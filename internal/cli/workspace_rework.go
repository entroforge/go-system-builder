package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
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
	if lifecycle["state"] != "building" {
		return fail(fmt.Errorf("rework requires building"))
	}
	cpPath := workspaceExecutionCheckpointPath(*root, e)
	cp, found, err := integration.DefaultCheckpointStore().Load(cpPath)
	if err != nil {
		return fail(err)
	}
	if !found && e.Status == "ready" && e.ReworkRef != "" && e.DeliveryRef == "" && loaded.PolicyContext.Agent != nil && loaded.PolicyContext.Agent.LifecycleActivity() == "working" {
		return encodeJSON(stdout, map[string]any{"execution": e, "rework_receipt": e.ReworkRef, "reused": true, "next": "resume Worker correction and submit a new report"})
	}
	persist := func(message string, apply func(map[string]any) error) error {
		// The responsibility is the Worker slot; the intent is this rework
		// round, named by the immutable rework receipt it bound and the
		// execution status the round reaches. A round that is interrupted
		// after its checkpoint was archived and then resumed reaches "ready"
		// more than once, so the journal anchor — a record identity, not the
		// Runtime write count — separates those writes (G18 §1.2/§1.3).
		intent := e.Status + "-" + domainEventToken(e.ReworkRef)
		eventID, idempotencyKey := workspaceEventIdentity(e.RuntimeID, "rework", e.AssignmentID, e.AgentID, e.Generation, intent, domainEventToken(journalEventAnchor(snap.State)))
		// Bind both the assignment slot and the exact immutable rework receipt.
		// The event actor is Main, so the slot ID is required to distinguish
		// sibling assignments when an original typed packet is revalidated.
		evidenceIDs := []string{e.AssignmentID}
		if e.ReworkRef != "" {
			evidenceIDs = append(evidenceIDs, e.ReworkRef)
		}
		next, err := writer.Update(snap.Revision, runtime.Mutation{EventID: eventID, TransitionID: "WORKSPACE-REWORK", Event: "integration_rework_requested", Actor: "main", RuntimeID: e.RuntimeID, BaselineGeneration: workspace.Generation(snap.State), IdempotencyKey: idempotencyKey, EvidenceIDs: evidenceIDs, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: message, Apply: func(state map[string]any) error {
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
		var stored struct {
			Execution  json.RawMessage `json:"execution"`
			Checkpoint json.RawMessage `json:"checkpoint"`
			Reason     string          `json:"reason"`
		}
		if err = json.Unmarshal(data, &stored); err != nil {
			return fail(err)
		}
		if len(stored.Checkpoint) != 0 {
			if receipt.Checkpoint, err = integration.DecodeCheckpoint(stored.Checkpoint); err != nil {
				return fail(err)
			}
		}
		receipt.Reason = stored.Reason
		if receipt.Execution, err = workspace.DecodeExecution(stored.Execution); err != nil {
			return fail(err)
		}
		if receipt.Execution.AssignmentID != e.AssignmentID || receipt.Execution.Generation != e.Generation || receipt.Execution.RuntimeID != e.RuntimeID {
			return fail(fmt.Errorf("rework receipt identity mismatch"))
		}
	} else {
		if e.Status != "ready" || !found || (cp.State != integration.StatePreserved && cp.State != integration.StateBlocked) || a.CompletionAckRef != "" || cp.VerifiedAt != "" {
			return fail(fmt.Errorf("rework requires a failed, unverified integration checkpoint; verified or acknowledged delivery cannot be reopened"))
		}
		if cp.ResumeState != integration.StatePending && cp.ResumeState != integration.StateMerged {
			return fail(fmt.Errorf("previous integration outcome is unknown; preserve the checkpoint for Main to inspect"))
		}
		if cp.WorktreePath != e.Path || cp.SourceBranch != e.Branch || cp.TargetBranch != e.TargetBranch {
			return fail(fmt.Errorf("failed checkpoint coordinates differ"))
		}
		if a.AssignmentRecord != nil {
			if a.RoleFamily == "" {
				return fail(fmt.Errorf("canonical S6 rework cannot resolve the original Builder role for %s; preserve the checkpoint and inspect its registered responsibility", a.AssignmentID))
			}
			return fail(fmt.Errorf(
				"canonical S6 Assignment rework follows its original current responsibility; use `loop-harness s6 prepare --root %q --task %s --role %s --revise-assignment %s`",
				*root, a.TaskID, a.RoleFamily, a.AssignmentID,
			))
		}
		receipt = reworkReceipt{e, cp, *reason}
		data, _ := json.MarshalIndent(receipt, "", "  ")
		path, err := reworkTransactionPath(filepath.Join(filepath.Dir(cpPath), "rework"), data)
		if err != nil {
			return fail(err)
		}
		e.ReworkRef = filepath.ToSlash(path)
		e.ReworkRef, _ = filepath.Rel(*root, e.ReworkRef)
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
		b.Executions[*id] = e
		if err = persist("preserve failed delivery and explicitly reopen original scope: "+*reason, func(state map[string]any) error {
			return assignment.ApplyIntegrationRework(state, catalog, *owner, a.TaskID, e.ReworkRef, *reason)
		}); err != nil {
			return fail(err)
		}
	}
	archive := filepath.Join(*root, e.ReworkRef) + ".checkpoint.json"
	if found {
		if !sameReworkCheckpoint(cp, receipt.Checkpoint) {
			return fail(fmt.Errorf("checkpoint changed during rework recovery"))
		}
		if err = os.Link(cpPath, archive); err != nil && !os.IsExist(err) {
			return fail(err)
		}
		archived, ok, err := integration.DefaultCheckpointStore().Load(archive)
		if err != nil || !ok || !sameReworkCheckpoint(archived, receipt.Checkpoint) {
			return fail(fmt.Errorf("archived checkpoint differs from the original rework transaction"))
		}
		if err = os.Remove(cpPath); err != nil {
			return fail(err)
		}
	} else {
		archived, ok, err := integration.DefaultCheckpointStore().Load(archive)
		if err != nil || !ok || !sameReworkCheckpoint(archived, receipt.Checkpoint) {
			return fail(fmt.Errorf("original rework checkpoint archive is unavailable; preserve the transaction"))
		}
	}
	e.Status = "ready"
	b.Executions[*id] = e
	if err = persist("failed checkpoint archived; Worker may correct scope and submit a new report/candidate", nil); err != nil {
		return fail(err)
	}
	return encodeJSON(stdout, map[string]any{"execution": e, "rework_receipt": e.ReworkRef, "next": "resume Worker, correct the original scope, commit and report again; S9 requires a new immutable candidate"})
}

func sameReworkCheckpoint(a, b integration.Checkpoint) bool {
	return a.Revision == b.Revision && a.State == b.State && a.ResumeState == b.ResumeState &&
		a.AssignmentID == b.AssignmentID && a.BaselineGeneration == b.BaselineGeneration &&
		a.WorktreePath == b.WorktreePath && a.SourceBranch == b.SourceBranch && a.TargetBranch == b.TargetBranch
}

// Allocate under the workspace lock. An unpublished Runtime transition can
// reuse its already-written intent; an archived prior attempt never aliases it.
func reworkTransactionPath(dir string, payload []byte) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	max := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "operation-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "operation-"), ".json"))
		if err != nil || n < 1 {
			continue
		}
		if n > max {
			max = n
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path + ".checkpoint.json"); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if string(old) == string(payload) {
			return path, nil
		}
	}
	return filepath.Join(dir, fmt.Sprintf("operation-%d.json", max+1)), nil
}
