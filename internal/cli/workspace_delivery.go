package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceDelivery(args []string, stdout, stderr io.Writer) int {
	verb := args[0]
	fs := flag.NewFlagSet("runtime workspace "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "Main or registered Worker root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	retry := fs.Bool("retry-preserved", false, "retry the same immutable candidate after resolving its recorded failure")
	if err := parseWorkspaceFlags(fs, args[1:]); err != nil {
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		return fail(err)
	}
	defer release()
	statePath, journalPath := filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")
	writer := runtime.NewWriter(statePath, journalPath, *root, semantic.RuntimeCandidateValidator{})
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
	if !ok || e.AgentID != *agent || (e.Status != "ready" && !(verb == "integrate" && e.Status == "complete")) {
		return fail(fmt.Errorf("delivery requires the registered execution and owner"))
	}
	loaded, err := hookctx.LoadFull(*root, e.AgentID)
	if err != nil {
		return fail(err)
	}
	var a *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		if loaded.Assignments[i].AssignmentID == e.AssignmentID && loaded.Assignments[i].OwnerAgentID == e.AgentID {
			a = &loaded.Assignments[i]
			break
		}
	}
	if a == nil {
		return fail(fmt.Errorf("delivery assignment is no longer registered"))
	}
	if verb == "deliver" {
		if loaded.PolicyContext.Agent == nil || loaded.PolicyContext.Agent.State != "reported" {
			return fail(fmt.Errorf("record the genuine Worker completion_report before proposing S9 delivery; the final domain RepairResult is submitted only after merge"))
		}
		if err = b.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		if err = b.ValidateInputs(e); err != nil {
			return fail(err)
		}
		path := filepath.Join(e.Path, ".claude/submissions/repair-result.json")
		physical, err := workspace.Canonical(path)
		if err != nil || physical != path {
			return fail(fmt.Errorf("candidate proposal must be a local Worker submission"))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		var proposal repair.RepairResultRequest
		if err = json.Unmarshal(data, &proposal); err != nil {
			return fail(err)
		}
		candidate, ref, err := repair.CreateWorkspaceCandidate(ctx, *root, snap.State, e, proposal)
		if err != nil {
			return fail(err)
		}
		if e.DeliveryRef != "" && (e.DeliveryRef != ref.Path || e.DeliverySHA256 != ref.SHA256) {
			return fail(fmt.Errorf("execution already has an immutable delivery; preserve it for explicit recovery"))
		}
		if e.DeliveryRef == "" {
			e.DeliveryRef, e.DeliverySHA256 = ref.Path, ref.SHA256
			b.Executions[e.AssignmentID] = e
			key := fmt.Sprintf("workspace-delivery-r%d", snap.Revision+1)
			snap, err = writer.Update(snap.Revision, runtime.Mutation{EventID: key, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: e.AgentID, RuntimeID: e.RuntimeID, IdempotencyKey: key, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Message: "immutable S9 candidate proposed; no final result or PASS registered", Apply: func(state map[string]any) error { state["workspace"] = workspace.Encode(b); return nil }})
			if err != nil {
				return fail(err)
			}
		}
		return encodeJSON(stdout, map[string]any{"candidate": candidate, "artifact_ref": ref, "revision": snap.Revision, "next_command_argv": []string{"loop-harness", "runtime", "workspace", "integrate", "--root", *root, "--assignment", e.AssignmentID, "--agent", e.AgentID}})
	}
	if err = workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	ref := repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256}
	candidate, err := repair.ReadWorkspaceCandidate(*root, ref)
	if err != nil {
		return fail(err)
	}
	if candidate.RuntimeID != e.RuntimeID || candidate.BaselineGeneration != e.BaselineGeneration || candidate.ExecutionGeneration != e.Generation || candidate.AssignmentID != e.AssignmentID || candidate.BaseCommit != e.BaseCommit {
		return fail(fmt.Errorf("candidate execution identity mismatch"))
	}
	resultRef, registered, err := repair.RegisteredWorkspaceResult(*root, snap.State, candidate)
	if err != nil {
		return fail(err)
	}
	if !registered {
		if _, err = repair.ValidateWorkspaceCandidate(*root, snap.State, e, ref); err != nil {
			return fail(err)
		}
	}
	cpPath := integration.CheckpointPath(*root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)
	cp, found, err := integration.DefaultCheckpointStore().Load(cpPath)
	if err != nil {
		return fail(err)
	}
	if found && ((cp.SourceHead != "" && cp.SourceHead != candidate.SourceHead) || cp.WorktreePath != e.Path || cp.TargetBranch != e.TargetBranch || cp.BaselineGeneration != e.BaselineGeneration) {
		return fail(fmt.Errorf("candidate/checkpoint coordinates differ"))
	}
	if registered && !found {
		return fail(fmt.Errorf("registered candidate result has no integration checkpoint"))
	}
	config := integration.IntegrateConfig{Root: *root, GitRoot: *root, RuntimeID: e.RuntimeID, RequiredChecks: e.Checks, CheckRunner: integration.CommandCheckRunner}
	var inspected integration.Inspection
	if found && (cp.State == integration.StateReady || cp.State == integration.StateMerged || cp.State == integration.StateVerified || cp.State == integration.StateAcknowledged || cp.State == integration.StateCleanupPending || cp.State == integration.StateComplete) {
		inspected, err = integration.RefreshCompletionBinding(*root, e.RuntimeID, a.CompletionRef, integration.InspectionFromCheckpoint(cp))
		if err != nil {
			return fail(err)
		}
	} else {
		if err = b.CleanInputs(ctx); err != nil {
			return fail(err)
		}
		if err = b.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		a.WorktreePath, a.Branch, a.TargetBranch = e.Path, e.Branch, e.TargetBranch
		a.WritePaths = e.WritePaths
		locked := []string{}
		for _, v := range loaded.PolicyContext.LockedArtifacts {
			locked = append(locked, v.Path)
		}
		inspectCfg := integration.InspectConfig{LockedArtifacts: locked, ValidateDelivery: func(ctx context.Context, in integration.Inspection) error {
			if in.SourceHead != candidate.SourceHead {
				return fmt.Errorf("candidate source commit changed")
			}
			_, err := repair.ValidateWorkspaceCandidate(*root, snap.State, e, ref)
			return err
		}}
		if found && cp.MergeCommit != "" {
			inspectCfg.RecoveryBase, err = integration.RecoveryBaseForCheckpoint(ctx, *root, cp)
			if err != nil {
				return fail(err)
			}
		}
		inspected, err = integration.Inspect(ctx, integration.InspectRequest{Root: *root, Assignment: *a, TargetBranch: e.TargetBranch, BaselineGeneration: e.BaselineGeneration, RuntimeID: e.RuntimeID}, inspectCfg)
		if err != nil {
			return fail(err)
		}
	}
	merged, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspected, RetryPreserved: *retry}, config)
	if err != nil {
		return fail(err)
	}
	cp = merged.Checkpoint
	if cp.State != integration.StateVerified && cp.State != integration.StateAcknowledged && cp.State != integration.StateCleanupPending && cp.State != integration.StateComplete {
		return fail(fmt.Errorf("candidate is not verified: %s %s", cp.State, cp.FailureReason))
	}
	if !registered {
		head, err := workspace.Git(ctx, *root, "rev-parse", "HEAD")
		if err != nil || head != cp.TestedHead {
			return fail(fmt.Errorf("Main moved after candidate verification"))
		}
		snap, err = writer.Snapshot()
		if err != nil {
			return fail(err)
		}
		candidate.Proposal.Checks, err = verifiedDomainChecks(*root, e, cp)
		if err != nil {
			return fail(err)
		}
		var result repair.RepairResult
		snap, result, resultRef, err = repair.SubmitRepairResultToRuntime(*root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: snap.Revision, Actor: e.AgentID}, Result: candidate.Proposal})
		if err != nil {
			return fail(err)
		}
		if result.Result != "pass" {
			return fail(fmt.Errorf("domain result %s is %s; preserve Worker and follow the recorded repair recovery route", result.ResultID, result.Result))
		}
	}
	if a.CompletionAckRef == "" {
		for _, other := range b.Executions {
			if other.AgentID != e.AgentID || other.AssignmentID == e.AssignmentID || other.DeliveryRef == "" || other.RuntimeID != e.RuntimeID || other.BaselineGeneration != e.BaselineGeneration {
				continue
			}
			c, err := repair.ReadWorkspaceCandidate(*root, repair.ArtifactRef{Path: other.DeliveryRef, SHA256: other.DeliverySHA256})
			if err != nil {
				return fail(err)
			}
			if _, done, err := repair.RegisteredWorkspaceResult(*root, snap.State, c); err != nil {
				return fail(err)
			} else if !done {
				return fail(fmt.Errorf("register verified domain result for %s before acknowledging shared owner", other.AssignmentID))
			}
		}
		snap, err = acknowledgeVerifiedIntegration(*root, snap, a, cp)
		if err != nil {
			return fail(err)
		}
	}
	finished, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: integration.InspectionFromCheckpoint(cp), Acknowledge: true, Cleanup: true}, config)
	if err != nil {
		return fail(err)
	}
	snap, err = persistExecutionCompletion(*root, snap, e.AssignmentID)
	if err != nil {
		return fail(err)
	}
	return encodeJSON(stdout, map[string]any{"checkpoint": finished.Checkpoint, "result_ref": resultRef, "revision": snap.Revision, "next": "continue independent targeted verification and a fresh S7; human release remains required"})
}
