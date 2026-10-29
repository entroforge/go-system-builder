package cli

import (
	"context"
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"io"
	"path/filepath"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "control or registered Worker root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	index := fs.Int("check", -1, "zero-based declared executable check index")
	ownedIndex := fs.Int("owned-check", -1, "zero-based frozen owner-run WorkerCheck index")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	checkSelected, ownedSelected := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "check":
			checkSelected = true
		case "owned-check":
			ownedSelected = true
		}
	})
	if checkSelected == ownedSelected {
		fmt.Fprintln(stderr, "select exactly one of --check or --owned-check")
		return 2
	}
	owned := ownedSelected
	if (owned && (*ownedIndex < 0 || checkSelected)) || (!owned && (*index < 0 || ownedSelected)) {
		fmt.Fprintln(stderr, "the selected check index must be nonnegative and the other flag must remain unset")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	snapshot, err := runtime.NewStore(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return fail(err)
	}
	b, err := workspace.Decode(snapshot.State)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("bind Main and prepare the Worker first"))
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(snapshot.State), workspace.Generation(snapshot.State))
	if !ok || e.AgentID != *agent || e.Status != "ready" || e.BootstrapSHA256 == "" {
		return fail(fmt.Errorf("check requires the current bootstrapped assignment and owner"))
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	switch lifecycle["state"] {
	case "building":
	case "bug_resolution":
		if _, err := repair.ResolveWorkspaceAuthority(*root, snapshot.State, e.AssignmentID, e.AgentID); err != nil {
			return fail(err)
		}
	default:
		return fail(fmt.Errorf("Worker check requires building or authorized S9 execution"))
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	authorized := false
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row["id"] == e.AgentID && (workstate.AgentActivity(row) == "working" || workstate.AgentActivity(row) == "reported") {
			authorized = true
		}
	}
	if !authorized {
		return fail(fmt.Errorf("Worker check requires its owner's active execution or reported delivery"))
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return fail(err)
	}
	if err := b.ValidateExecution(ctx, e, false); err != nil {
		return fail(err)
	}
	if err := b.ValidateInputs(e); err != nil {
		return fail(err)
	}
	if err := b.ValidateBootstrap(e); err != nil {
		return fail(err)
	}
	var attempt *checkexec.RunAttempt
	if !owned {
		attempt, err = b.BeginCheckRun(ctx, e, *index)
		if err != nil {
			return fail(err)
		}
		defer attempt.Close()
	}
	finalReceipt := ""
	var finalErr error
	if attempt != nil {
		defer func() {
			if err := attempt.Finish(finalReceipt, finalErr); err != nil {
				fmt.Fprintln(stderr, "check diagnostic finalization:", err)
			}
		}()
	}
	ctx, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		finalErr = err
		return fail(err)
	}
	defer release()
	// Waiting never turns an old snapshot into current authority.
	current, err := runtime.NewStore(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		finalErr = err
		return fail(err)
	}
	currentRegistry, err := workspace.Decode(current.State)
	if err != nil || currentRegistry == nil {
		finalErr = fmt.Errorf("check workspace changed while waiting")
		return fail(finalErr)
	}
	currentExecution, ok := currentRegistry.Execution(e.AssignmentID, workspace.RuntimeID(current.State), workspace.Generation(current.State))
	compare := currentExecution
	compare.ToolCheckReceipts = e.ToolCheckReceipts
	compare.OwnedCheckReceipts = e.OwnedCheckReceipts
	if !ok || !reflect.DeepEqual(compare, e) {
		finalErr = fmt.Errorf("check execution changed while waiting")
		return fail(finalErr)
	}
	currentLife, _ := current.State["lifecycle"].(map[string]any)
	if currentLife["state"] != lifecycle["state"] {
		finalErr = fmt.Errorf("check responsibility changed while waiting")
		return fail(finalErr)
	}
	if currentLife["state"] == "bug_resolution" {
		if _, err := repair.ResolveWorkspaceAuthority(*root, current.State, e.AssignmentID, e.AgentID); err != nil {
			finalErr = err
			return fail(err)
		}
	}
	currentEntities, _ := current.State["entities"].(map[string]any)
	currentAgents, _ := currentEntities["agents"].([]any)
	active := false
	for _, raw := range currentAgents {
		row, _ := raw.(map[string]any)
		if row["id"] == e.AgentID && (workstate.AgentActivity(row) == "working" || workstate.AgentActivity(row) == "reported") {
			active = true
		}
	}
	if !active {
		finalErr = fmt.Errorf("check owner stopped while waiting")
		return fail(finalErr)
	}
	b, e = currentRegistry, currentExecution
	ctx = checkexec.WithReuseGuard(ctx, checkexec.ReuseGuard{Fresh: currentLife["state"] == "bug_resolution"})
	if attempt != nil {
		if err := attempt.Start(); err != nil {
			finalErr = err
			return fail(err)
		}
	}
	var receipt string
	var captured *checkexec.ToolReceipt
	var ownedAttempt *workspace.OwnedCheckRunAttempt
	if owned {
		result, runErr := b.RunOwnedCheck(ctx, e, *ownedIndex, stdout)
		receipt, err = result.ReceiptPath, runErr
		ownedAttempt = result.Attempt
		if ownedAttempt != nil {
			defer ownedAttempt.Close()
		}
		if result.ToolReceipt.Path != "" && !result.Reused {
			captured = &result.ToolReceipt
		}
	} else {
		ctx = checkexec.WithReceiptCapture(ctx, func(ref checkexec.ToolReceipt) { captured = &ref })
		receipt, err = b.RunCheck(ctx, e, *index, stdout)
	}
	finalReceipt, finalErr = receipt, err
	if receipt != "" {
		fmt.Fprintln(stderr, "Worker check receipt:", receipt)
	}
	if captured != nil {
		var acceptErr error
		if owned {
			acceptErr = acceptWorkspaceOwnedCheckReceipt(*root, e, *ownedIndex, *captured)
		} else {
			acceptErr = acceptWorkspaceToolReceipt(*root, e, *captured)
		}
		if acceptErr != nil {
			finalErr = acceptErr
			if ownedAttempt != nil {
				_ = ownedAttempt.Finish(receipt, acceptErr)
			}
			return fail(acceptErr)
		}
	}
	if ownedAttempt != nil {
		if finishErr := ownedAttempt.Finish(receipt, err); finishErr != nil && err == nil {
			finalErr = finishErr
			return fail(finishErr)
		}
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

// Called only with the reference captured in this actual tool invocation.
// There is no CLI import path for a self-authored receipt.
func acceptWorkspaceToolReceipt(root string, execution workspace.Execution, ref checkexec.ToolReceipt) error {
	return acceptWorkspaceCheckReceipt(root, execution, -1, ref)
}

func acceptWorkspaceOwnedCheckReceipt(root string, execution workspace.Execution, index int, ref checkexec.ToolReceipt) error {
	return acceptWorkspaceCheckReceipt(root, execution, index, ref)
}

func acceptWorkspaceCheckReceipt(root string, execution workspace.Execution, ownedIndex int, ref checkexec.ToolReceipt) error {
	receive := func(state map[string]any) error {
		b, err := workspace.Decode(state)
		if err != nil || b == nil {
			return fmt.Errorf("tool receipt requires original workspace")
		}
		e, ok := b.Execution(execution.AssignmentID, workspace.RuntimeID(state), workspace.Generation(state))
		if !ok || e.AgentID != execution.AgentID || e.Generation != execution.Generation || e.Path != execution.Path || e.Status != "ready" || e.BootstrapSHA256 != execution.BootstrapSHA256 {
			return fmt.Errorf("tool receipt execution changed before reception")
		}
		compare := e
		compare.ToolCheckReceipts = execution.ToolCheckReceipts
		compare.OwnedCheckReceipts = execution.OwnedCheckReceipts
		if !reflect.DeepEqual(compare, execution) {
			return fmt.Errorf("tool receipt original execution/Record projection changed before reception")
		}
		if err := b.ValidateExecution(context.Background(), e, false); err != nil {
			return err
		}
		if err := b.ValidateInputs(e); err != nil {
			return err
		}
		if err := b.ValidateBootstrap(e); err != nil {
			return err
		}
		lifecycle, _ := state["lifecycle"].(map[string]any)
		if lifecycle["state"] == "bug_resolution" {
			if _, err := repair.ResolveWorkspaceAuthority(root, state, e.AssignmentID, e.AgentID); err != nil {
				return err
			}
		} else if lifecycle["state"] != "building" {
			return fmt.Errorf("tool receipt responsibility is no longer executing")
		}
		entities, _ := state["entities"].(map[string]any)
		agents, _ := entities["agents"].([]any)
		active := false
		for _, raw := range agents {
			row, _ := raw.(map[string]any)
			if row["id"] == e.AgentID && (workstate.AgentActivity(row) == "working" || workstate.AgentActivity(row) == "reported") {
				active = true
			}
		}
		if !active {
			return fmt.Errorf("tool receipt owner is no longer active")
		}
		if ownedIndex >= 0 {
			if err := b.AcceptOwnedCheckReceipt(&e, ownedIndex, ref); err != nil {
				return err
			}
		} else {
			if err := b.AcceptToolReceipt(&e, ref); err != nil {
				return err
			}
		}
		b.Executions[e.AssignmentID] = e
		state["workspace"] = workspace.Encode(b)
		return nil
	}

	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	for attempt := 0; attempt < 3; attempt++ {
		snap, err := writer.Snapshot()
		if err != nil {
			return err
		}
		if !workstate.SupportsAssignments(snap.State["schema_version"]) {
			return fmt.Errorf("tool receipt registration requires original 1.2 Runtime")
		}
		registered := false
		original, decodeErr := workspace.Decode(snap.State)
		if decodeErr != nil || original == nil {
			return fmt.Errorf("tool receipt requires original workspace")
		}
		currentExecution := original.Executions[execution.AssignmentID]
		if ownedIndex >= 0 {
			for _, previous := range currentExecution.OwnedCheckReceipts {
				if previous.Receipt.SHA256 != ref.SHA256 {
					continue
				}
				if previous.WorkerCheckIndex != ownedIndex || previous.Receipt != ref {
					return fmt.Errorf("owned receipt replay differs from original captured reference")
				}
				registered = true
			}
		} else {
			for _, previous := range currentExecution.ToolCheckReceipts {
				if previous.SHA256 == ref.SHA256 {
					if previous != ref {
						return fmt.Errorf("tool receipt replay differs from original captured reference")
					}
					registered = true
				}
			}
		}
		// Validate replay identity before the original Writer's idempotency lookup.
		if err := receive(snap.State); err != nil {
			return err
		}
		if registered {
			return nil
		}
		key, transition, event, message := "tool-check:"+ref.SHA256, "WORKSPACE-CHECK", "workspace_check_received", "record actual tool receipt; no professional acceptance or gate PASS"
		if ownedIndex >= 0 {
			key = fmt.Sprintf("owned-check:%d:%s", ownedIndex, ref.SHA256)
			transition, event = "WORKSPACE-OWNED-CHECK", "workspace_owned_check_received"
			message = "record actual frozen WorkerCheck receipt; no professional acceptance or gate PASS"
		}
		_, err = writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: transition, Event: event, Actor: execution.AgentID, RuntimeID: execution.RuntimeID, IdempotencyKey: key, RetainLastTransition: true, ProducerResponsibility: "owned_check", OccurredAt: time.Now().UTC(), Message: message, Apply: func(state map[string]any) error {
			return receive(state)
		}})
		if err == runtime.ErrStaleRevision {
			continue
		}
		return err
	}
	return runtime.ErrStaleRevision
}
