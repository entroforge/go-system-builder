package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/processtree"
)

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
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return runBudgetedHook(ctx, executable, args[1:], stdin, stdout, stderr)
}

func runBudgetedHook(ctx context.Context, executable string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
			fmt.Fprintln(stderr, "Hook input unavailable or exceeds 4 MiB; retry with valid platform input")
			return 2
		}
		data = in.data
	case <-ctx.Done():
		fmt.Fprintln(stderr, "Hook input deadline exceeded; tool blocked")
		return 2
	}
	var input policy.Input
	decodeErr := json.Unmarshal(data, &input)
	fail := func(err error) int {
		fmt.Fprintf(stderr, "Harness Hook did not complete within its internal budget: %v; preserve Runtime and retry after checking the external terminal.\n", err)
		if decodeErr != nil || policy.UnavailableDecision(input, err).Decision == "deny" {
			return 2
		}
		return 0
	}
	if decodeErr != nil {
		return fail(decodeErr)
	}
	var out, errout bytes.Buffer
	cmd := exec.CommandContext(ctx, executable, append([]string{"__hook-worker"}, args...)...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &out
	cmd.Stderr = &errout
	err := processtree.Run(cmd)
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			// Exit 1 is a non-blocking platform error. Convert incomplete mutable
			// evaluations to a blocking outcome, while retaining native deny feedback.
			if exit.ExitCode() != 2 {
				return fail(err)
			}
			if _, writeErr := stdout.Write(out.Bytes()); writeErr != nil {
				return fail(writeErr)
			}
			_, _ = stderr.Write(errout.Bytes())
			return 2
		}
		return fail(err)
	}
	if _, writeErr := stdout.Write(out.Bytes()); writeErr != nil {
		return fail(writeErr)
	}
	_, _ = stderr.Write(errout.Bytes())
	return 0
}
