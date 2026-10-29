package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/processtree"
)

const hookProcessBudget = 8 * time.Second

// S4 reception and successor preparation each have a bounded 12s operation.
// Leave time to report their result before the native process/platform limits.
const s4HookProcessBudget = 28 * time.Second

func hookBudget(root string, input policy.Input) time.Duration {
	if input.Event != "PostToolUse" || input.Error != "" || !mainQuestionnaireViewer(input) {
		return hookProcessBudget
	}
	if failed, _ := input.ToolResponse["is_error"].(bool); failed {
		return hookProcessBudget
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if path, saved := savedQuestionnairePath(input); saved {
		rel, err := filepath.Rel(root, path)
		if err == nil && strings.HasPrefix(filepath.ToSlash(rel), ".claude/submissions/") && strings.Contains(filepath.ToSlash(rel), "/S4/") {
			return s4HookProcessBudget
		}
	}
	// A script need not advertise a file path, and a successful Post can resume
	// an existing S4 pending reception. This bounded read only selects time;
	// the original receiver still validates ownership, issuance and all inputs.
	statePath := filepath.Join(root, ".claude/loop-state.json")
	info, err := os.Lstat(statePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return hookProcessBudget
	}
	f, err := os.Open(statePath)
	if err != nil {
		return hookProcessBudget
	}
	defer f.Close()
	var state struct {
		Lifecycle struct {
			State string `json:"state"`
			Phase string `json:"phase"`
		} `json:"lifecycle"`
	}
	if json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&state) == nil && state.Lifecycle.State == "planning" && state.Lifecycle.Phase == "tasks" {
		return s4HookProcessBudget
	}
	return hookProcessBudget
}

func hookRootForBudget(args []string) string {
	flags := flag.NewFlagSet("hook-budget", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "")
	flags.String("event", "", "")
	flags.String("progress-mode", "shadow", "")
	if flags.Parse(args) != nil {
		return "."
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return *root
	}
	return abs
}

// RunWithHookBudget is the native entrypoint. The outer process owns a deadline
// independent of nested Runtime, Git and metric waits. Buffer decisions until
// the worker exits; a timed-out partial allow can never reach Claude.
func RunWithHookBudget(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "__hook-worker" {
		return Run(append([]string{"hook"}, args[1:]...), stdin, stdout, stderr)
	}
	if len(args) == 0 || args[0] != "hook" {
		return Run(args, stdin, stdout, stderr)
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return runBudgetedHook(context.Background(), executable, args[1:], stdin, stdout, stderr)
}

func runBudgetedHook(ctx context.Context, executable string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	started := time.Now()
	root := hookRootForBudget(args)
	recordFailure := func(lifetime context.Context, event, outcome string) {
		if errors.Is(lifetime.Err(), context.DeadlineExceeded) {
			outcome = "deadline"
		} else if errors.Is(lifetime.Err(), context.Canceled) {
			outcome = "canceled"
		}
		deadline, _ := lifetime.Deadline()
		budget := max(int64(0), deadline.Sub(started).Milliseconds())
		// Bounded-size publication through the existing spool, without audit
		// history reads or audit/summary lock waits.
		if err := metrics.ObserveHookTransportFailure(root, event, outcome, time.Since(started).Milliseconds(), budget); err != nil {
			fmt.Fprintf(stderr, "Hook transport diagnostic could not be persisted: %v\n", err)
		}
	}
	inputContext, inputCancel := context.WithTimeout(ctx, hookProcessBudget)
	defer inputCancel()
	type inputResult struct {
		data []byte
		err  error
	}
	inputs := make(chan inputResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(stdin, 4*1024*1024+1))
		inputs <- inputResult{data, err}
	}()
	var data []byte
	select {
	case in := <-inputs:
		if in.err != nil || len(in.data) > 4*1024*1024 {
			recordFailure(inputContext, "unknown", "input")
			fmt.Fprintln(stderr, "Hook input unavailable or exceeds 4 MiB; graph progress was not updated")
			return 1
		}
		data = in.data
	case <-inputContext.Done():
		recordFailure(inputContext, "unknown", "input")
		fmt.Fprintln(stderr, "Hook input deadline exceeded; graph progress was not updated")
		return 1
	}
	var input policy.Input
	decodeErr := json.Unmarshal(data, &input)
	ctx, cancel := context.WithDeadline(ctx, started.Add(hookBudget(root, input)))
	defer cancel()
	fail := func(err error, outcome string) int {
		recordFailure(ctx, input.Event, outcome)
		reason := "Harness Hook did not complete"
		if ctx.Err() != nil {
			reason += " within its internal budget"
		}
		fmt.Fprintf(stderr, "%s: %v. Hook transport is unfinished for %s; timeout/cancellation is not a professional content verdict. The original receipt and current responsibility remain authoritative. A missing response does not prove no commit; preserve professional answers and do not repeat accepted work. Framework recovery must use the original attempt, without changing answer fields, deleting evidence or inventing another artifact ID.\n", reason, err, input.Event)
		if input.Event == "PreToolUse" {
			return 0
		}
		if decodeErr != nil || policy.UnavailableDecision(input, err).Decision == "deny" {
			return 2
		}
		return 0
	}
	if decodeErr != nil {
		return fail(decodeErr, "input")
	}
	var out, errout bytes.Buffer
	cmd := exec.CommandContext(ctx, executable, append([]string{"__hook-worker"}, args...)...)
	deadline, _ := ctx.Deadline()
	// Carry the parent's absolute limit through process startup; the child must
	// not start a fresh eight-second clock after input and scheduling delays.
	cmd.Env = append(os.Environ(), hookDeadlineEnvironment+"="+deadline.UTC().Format(time.RFC3339Nano))
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := processtree.Run(cmd)
	if ctx.Err() != nil {
		// Keep the original receiver's specific responsibility and committed
		// facts even when the transport expires. Partial stdout still cannot
		// establish acceptance or a completed platform decision.
		_, _ = stderr.Write(errout.Bytes())
		return fail(ctx.Err(), "worker")
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			// Exit 1 is a non-blocking platform error. Convert incomplete mutable
			// evaluations to feedback, while retaining Stop/Idle continuation.
			if exit.ExitCode() != 2 {
				_, _ = stderr.Write(errout.Bytes())
				return fail(err, "worker")
			}
			if _, writeErr := stdout.Write(out.Bytes()); writeErr != nil {
				return fail(writeErr, "delivery")
			}
			_, _ = stderr.Write(errout.Bytes())
			if input.Event == "PreToolUse" {
				return 0
			}
			return 2
		}
		return fail(err, "worker")
	}
	if _, writeErr := stdout.Write(out.Bytes()); writeErr != nil {
		return fail(writeErr, "delivery")
	}
	_, _ = stderr.Write(errout.Bytes())
	return 0
}
