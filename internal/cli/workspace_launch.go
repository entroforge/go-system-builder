package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceLaunch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace launch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "bound Main root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	resume := fs.Bool("resume", false, "resume the exact recorded Claude session")
	prompt := fs.String("prompt", "Continue your registered assignment, preserve Main, and report evidence through the Harness.", "Worker instruction")
	timeout := fs.Duration("timeout", 30*time.Minute, "maximum Worker process duration")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *timeout <= 0 {
		return fail(fmt.Errorf("positive launch timeout required"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, *timeout)
	defer cancel()
	locked, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		return fail(fmt.Errorf("lock Main workspace for original Worker launch: %w", err))
	}
	// Release the Git lock before the Worker starts; its adapters need that lock.
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	writer := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snap, err := writer.Snapshot()
	if err != nil {
		return fail(fmt.Errorf("read Runtime for original Worker launch: %w", err))
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("bind Main first"))
	}
	if err = b.Validate(locked, *root); err != nil {
		return fail(err)
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State))
	if !ok || e.AgentID != *agent || e.Status != "ready" || e.BootstrapSHA256 == "" {
		return fail(fmt.Errorf("launch requires current bootstrapped Worker owner"))
	}
	if e.Purpose == workspace.DocumentReviewPurpose {
		p, err := workspace.DocumentReviewPacket(snap.State, e)
		if err != nil {
			return fail(err)
		}
		life, _ := snap.State["lifecycle"].(map[string]any)
		if life["state"] != "document_verification" || stageAnswerEntry(life["phase_revision"]) != p.Binding.StageEntry {
			return fail(fmt.Errorf("S5 review origin changed; restore the original receipt or rework dispatch"))
		}
		if err := b.ValidateDocumentReviewView(e, p); err != nil {
			return fail(err)
		}
	}
	if err = b.PreflightChecks(ctx, e, e.Path); err != nil {
		return fail(fmt.Errorf("prepare original Worker checks for %s: %w", e.AssignmentID, err))
	}
	loaded, err := hookctx.LoadFull(*root, e.AgentID)
	if err != nil {
		return fail(fmt.Errorf("read original Worker context for %s: %w", e.AssignmentID, err))
	}
	if loaded.PolicyContext.Agent == nil {
		return fail(fmt.Errorf("Worker owner is not registered"))
	}
	role := ""
	for _, a := range loaded.Assignments {
		if a.AssignmentID == e.AssignmentID && a.OwnerAgentID == e.AgentID {
			role = a.RoleFamily
		}
	}
	if role == "" {
		return fail(fmt.Errorf("Worker owner has no role family"))
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return fail(err)
	}
	if err := b.ValidateExecution(locked, e, false); err != nil {
		return fail(err)
	}
	if err := b.ValidateInputs(e); err != nil {
		return fail(err)
	}
	if info, err := os.Stat(filepath.Join(e.Path, ".claude/agents", role+".md")); err != nil || !info.Mode().IsRegular() {
		return fail(fmt.Errorf("installed role %s is unavailable", role))
	}
	leaseCtx, leaseCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	lease, err := filelock.Acquire(leaseCtx, filepath.Join(*root, ".claude/workspace-launch", e.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", e.BaselineGeneration, e.AssignmentID, e.Generation)))
	leaseCancel()
	if err != nil {
		return fail(fmt.Errorf("acquire original Worker launch lease for %s: %w", e.AssignmentID, err))
	}
	defer lease()
	if e.PlatformSessionID != "" && !*resume {
		return fail(fmt.Errorf("session %s already reserved; use --resume; if Claude never created it, preserve the launch receipt for explicit execution replacement", e.PlatformSessionID))
	}
	if e.PlatformSessionID == "" {
		if *resume {
			return fail(fmt.Errorf("no recorded session to resume"))
		}
		e.PlatformSessionID, err = workspace.NewSessionID()
		if err != nil {
			return fail(fmt.Errorf("create original Worker session ID for %s: %w", e.AssignmentID, err))
		}
		b.Executions[e.AssignmentID] = e
		key := fmt.Sprintf("workspace-launch-r%d", snap.Revision+1)
		_, err = writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: e.RuntimeID, IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: "reserve Worker platform session before child launch", Apply: func(state map[string]any) error { state["workspace"] = workspace.Encode(b); return nil }})
		if err != nil {
			return fail(fmt.Errorf("reserve original Worker session for %s: %w", e.AssignmentID, err))
		}
	}
	release()
	released = true
	dir := filepath.Join(*root, ".claude/workspace-launch", e.PlatformSessionID)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return fail(err)
	}
	log, err := os.CreateTemp(dir, "attempt-*.log")
	if err != nil {
		return fail(fmt.Errorf("create original Worker launch log for %s: %w", e.AssignmentID, err))
	}
	defer log.Close()
	started := time.Now().UTC()
	err = b.Launch(ctx, e, "claude", role, *prompt, *resume, io.MultiWriter(stdout, log), io.MultiWriter(stderr, log))
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	receipt := map[string]any{"execution": e, "started_at": started, "finished_at": time.Now().UTC(), "resume": *resume, "error": detail, "log": log.Name(), "completed_assignment": false}
	data, encodeErr := json.MarshalIndent(receipt, "", "  ")
	if encodeErr != nil {
		return fail(encodeErr)
	}
	if writeErr := os.WriteFile(log.Name()+".json", data, 0600); writeErr != nil {
		return fail(writeErr)
	}
	if err != nil {
		return fail(fmt.Errorf("run original Worker process for %s: %w", e.AssignmentID, err))
	}
	return 0
}
