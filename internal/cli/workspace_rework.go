package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type reworkReceipt struct {
	Execution  workspace.Execution    `json:"execution"`
	Checkpoint integration.Checkpoint `json:"checkpoint"`
	Reason     string                 `json:"reason"`
}

func runWorkspaceRework(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace rework", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "bound Main root")
	id := fs.String("assignment", "", "failed assignment")
	owner := fs.String("agent", "", "registered owner")
	reason := fs.String("reason", "", "required code correction")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if *reason == "" {
		return fail(fmt.Errorf("rework requires a correction reason"))
	}
	if err := workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		return fail(err)
	}
	defer release()
	writer := runtime.NewWriter(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{})
	snap, err := writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	b, err := workspace.Decode(snap.State)
	if err != nil {
		return fail(err)
	}
	if b == nil {
		return fail(fmt.Errorf("bind Main first"))
	}
	if err = b.Validate(ctx, *root); err != nil {
		return fail(err)
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State))
	if !ok || e.AgentID != *owner {
		return fail(fmt.Errorf("rework owner/assignment mismatch"))
	}
	if err = b.ValidateExecution(ctx, e, false); err != nil {
		return fail(err)
	}
	if err = b.ValidateInputs(e); err != nil {
		return fail(err)
	}
	loaded, err := hookctx.LoadFull(*root, *owner)
	if err != nil {
		return fail(err)
	}
	var a *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		if loaded.Assignments[i].AssignmentID == *id && loaded.Assignments[i].OwnerAgentID == *owner {
			a = &loaded.Assignments[i]
		}
	}
	if a == nil {
		return fail(fmt.Errorf("rework assignment is unregistered"))
	}
	lifecycle, _ := snap.State["lifecycle"].(map[string]any)
	if lifecycle["state"] == "bug_resolution" {
		if _, err = repair.ResolveWorkspaceAuthority(*root, snap.State, *id, *owner); err != nil {
			return fail(err)
		}
	} else if lifecycle["state"] != "building" {
		return fail(fmt.Errorf("rework requires building or active repair authority"))
	}
	cpPath := integration.CheckpointPath(*root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)
	cp, found, err := integration.DefaultCheckpointStore().Load(cpPath)
	if err != nil {
		return fail(err)
	}
	if !found && e.Status == "ready" && e.ReworkRef != "" && e.DeliveryRef == "" && loaded.PolicyContext.Agent != nil && loaded.PolicyContext.Agent.State == "working" {
		return encodeJSON(stdout, map[string]any{"execution": e, "rework_receipt": e.ReworkRef, "reused": true, "next": "resume Worker correction and submit a new report"})
	}
	persist := func(message string, apply func(map[string]any) error) error {
		key := fmt.Sprintf("workspace-rework-r%d", snap.Revision+1)
		next, err := writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE-REWORK", Event: "integration_rework_requested", Actor: "main", RuntimeID: e.RuntimeID, IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: message, Apply: func(state map[string]any) error {
			if apply != nil {
				if err := apply(state); err != nil {
					return err
				}
			}
			state["workspace"] = workspace.Encode(b)
			return nil
		}})
		if err == nil {
			snap = next
		}
		return err
	}
	var receipt reworkReceipt
	if e.Status == "preparing" && e.ReworkRef != "" {
		data, err := os.ReadFile(filepath.Join(*root, filepath.FromSlash(e.ReworkRef)))
		if err != nil {
			return fail(err)
		}
		if err = json.Unmarshal(data, &receipt); err != nil {
			return fail(err)
		}
		if receipt.Execution.AssignmentID != e.AssignmentID || receipt.Execution.Generation != e.Generation || receipt.Execution.RuntimeID != e.RuntimeID {
			return fail(fmt.Errorf("rework receipt identity mismatch"))
		}
	} else {
		if e.Status != "ready" || !found || (cp.State != integration.StatePreserved && cp.State != integration.StateBlocked) || cp.TestedHead != "" || a.CompletionAckRef != "" {
			return fail(fmt.Errorf("rework requires a failed, unverified integration checkpoint; verified or acknowledged delivery cannot be reopened"))
		}
		if cp.WorktreePath != e.Path || cp.SourceBranch != e.Branch || cp.TargetBranch != e.TargetBranch {
			return fail(fmt.Errorf("failed checkpoint coordinates differ"))
		}
		receipt = reworkReceipt{e, cp, *reason}
		data, _ := json.MarshalIndent(receipt, "", "  ")
		hash := sha256.Sum256(data)
		e.ReworkRef = filepath.ToSlash(filepath.Join(filepath.Dir(cpPath), "rework", hex.EncodeToString(hash[:])+".json"))
		e.ReworkRef, _ = filepath.Rel(*root, e.ReworkRef)
		path := filepath.Join(*root, e.ReworkRef)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return fail(err)
		}
		if old, err := os.ReadFile(path); err == nil {
			if string(old) != string(data) {
				return fail(fmt.Errorf("rework receipt collision"))
			}
		} else if !os.IsNotExist(err) {
			return fail(err)
		} else {
			f, err := os.CreateTemp(filepath.Dir(path), ".rework-*")
			if err != nil {
				return fail(err)
			}
			defer os.Remove(f.Name())
			_, writeErr := f.Write(data)
			closeErr := f.Close()
			if writeErr != nil {
				return fail(writeErr)
			}
			if closeErr != nil {
				return fail(closeErr)
			}
			if err = os.Link(f.Name(), path); err != nil {
				return fail(err)
			}
		}
		catalog, err := transition.LoadCatalog(*root)
		if err != nil {
			return fail(err)
		}
		e.Status = "preparing"
		e.DeliveryRef = ""
		e.DeliverySHA256 = ""
		b.Executions[*id] = e
		if err = persist("preserve failed delivery and explicitly reopen original scope: "+*reason, func(state map[string]any) error {
			return assignment.ApplyIntegrationRework(state, catalog, *owner, a.TaskID, e.ReworkRef, *reason)
		}); err != nil {
			return fail(err)
		}
	}
	if found {
		if cp.Revision != receipt.Checkpoint.Revision || cp.SourceHead != receipt.Checkpoint.SourceHead || cp.State != receipt.Checkpoint.State {
			return fail(fmt.Errorf("checkpoint changed during rework recovery"))
		}
		if err = os.Rename(cpPath, filepath.Join(*root, e.ReworkRef)+".checkpoint.json"); err != nil {
			return fail(err)
		}
	}
	e.Status = "ready"
	b.Executions[*id] = e
	if err = persist("failed checkpoint archived; Worker may correct scope and submit a new report/candidate", nil); err != nil {
		return fail(err)
	}
	return encodeJSON(stdout, map[string]any{"execution": e, "rework_receipt": e.ReworkRef, "next": "resume Worker, correct the original scope, commit and report again; S9 requires a new immutable candidate"})
}
