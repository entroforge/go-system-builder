package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func runWorkspaceDelivery(args []string, stdout, stderr io.Writer) int {
	return runWorkspaceDeliveryContext(context.Background(), args, stdout, stderr)
}

func runWorkspaceDeliveryContext(parent context.Context, args []string, stdout, stderr io.Writer) int {
	verb := args[0]
	fs := flag.NewFlagSet("runtime workspace "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "Main or registered Worker root")
	id := fs.String("assignment", "", "registered assignment")
	agent := fs.String("agent", "", "registered owner")
	var checkCommands stringListFlag
	fs.Var(&checkCommands, "check-command", "integrate: necessary Main check command; repeat for multiple checks")
	retry := fs.Bool("retry-preserved", false, "retry the same immutable candidate after resolving its recorded failure")
	if err := parseWorkspaceFlags(fs, args[1:]); err != nil {
		return 2
	}
	acceptedFinal := false
	fail := func(err error) int {
		if acceptedFinal {
			fmt.Fprint(stderr, "Original Final remains accepted; Main preserves completed integration/checks and resumes the original ACK/cleanup independently of the current repair responsibility. ")
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	if verb == "integrate" {
		absoluteRoot, err := filepath.Abs(*root)
		if err != nil {
			return fail(err)
		}
		*root = absoluteRoot
	}
	if verb != "integrate" && len(checkCommands) > 0 {
		return fail(fmt.Errorf("check-command is an integrate-only Main work parameter"))
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	defer cancel()
	ctx, release, err := integration.LockWorkspace(ctx, *root)
	if err != nil {
		return fail(err)
	}
	defer release()
	statePath, journalPath := filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")
	writer := runtime.NewWriter(statePath, journalPath, *root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snap, err := writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	data, err := json.Marshal(snap.State["workspace"])
	if err != nil {
		return fail(err)
	}
	registry, err := workspace.DecodeRegistry(data)
	if err != nil {
		return fail(err)
	}
	b := &registry
	e, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State))
	if !ok || e.AgentID != *agent || (e.Status != "ready" && e.Status != "complete") {
		return fail(fmt.Errorf("delivery requires the registered execution and owner"))
	}
	if e.Purpose == workspace.DocumentReviewPurpose {
		return fail(fmt.Errorf("S5 document review has no product delivery/integration; use the answer receipt"))
	}
	if err = workspace.RequireMain(*root); err != nil {
		return fail(err)
	}
	// Explain the original reception boundary before opening a candidate path.
	// Execution registration alone does not establish any delivery fact.
	if e.DeliveryRef == "" {
		handoff := s9HandoffForExecution(ctx, *root, snap, e)
		return fail(fmt.Errorf("%s %s", handoff.summary(*root), s9UnreceivedActionContext(ctx, *root, handoff)))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	ref := repair.ArtifactRef{Path: e.DeliveryRef}
	data, err = workpacket.ReadFile(*root, ref.Path, 20<<20)
	if err != nil {
		return fail(fmt.Errorf("original candidate source %s for owner=%s assignment=%s is unavailable; Main restores this exact source and preserves accepted work: %w", ref.Path, e.AgentID, e.AssignmentID, err))
	}
	candidate, err := repair.DecodeWorkspaceCandidate(data)
	if err != nil {
		return fail(fmt.Errorf("original candidate source %s owner=%s assignment=%s is invalid; Main restores this source: %w", ref.Path, e.AgentID, e.AssignmentID, err))
	}
	if candidate.RuntimeID != e.RuntimeID || candidate.BaselineGeneration != e.BaselineGeneration || candidate.ExecutionGeneration != e.Generation || candidate.AssignmentID != e.AssignmentID {
		return fail(fmt.Errorf("candidate execution identity mismatch"))
	}
	typedPacket, typedResult, err := currentRepairResultPacketForDeliveryContext(ctx, *root, snap.State, e, candidate)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	resultRef, registered, err := repair.RegisteredWorkspaceResultContext(ctx, *root, snap.State, candidate)
	if err != nil {
		return fail(err)
	}
	if typedResult && !registered {
		complete, completeErr := repair.RepairResultWorkCompleteWithContext(ctx, *root, snap.State, typedPacket.ID, e.AgentID)
		if completeErr != nil {
			return fail(fmt.Errorf("original candidate acceptance is preserved; current professional completion is unavailable: %w", completeErr))
		}
		if !complete {
			return fail(fmt.Errorf("original candidate is accepted for owner=%s assignment=%s packet=%s; actual professional work remains unfinished. Main follows the recorded professional omissions and original rework path; do not repeat accepted content or integrate it as completed", e.AgentID, e.AssignmentID, typedPacket.ID))
		}
	}
	acceptedFinal = registered
	if !typedResult {
		return fail(fmt.Errorf("original S9 result packet is unavailable for the current responsibility"))
	}
	a := &hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID,
		TaskID: typedPacket.Binding.Sources.TaskID, WorktreePath: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch}
	deferredNative := e.Purpose == workspace.NativeDeliveryPurpose && e.PlatformSessionID == ""
	cpPath := workspaceExecutionCheckpointPath(*root, e)
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(cpPath)
	if err != nil {
		return fail(err)
	}
	if found && (cp.WorktreePath != e.Path || cp.TargetBranch != e.TargetBranch || cp.BaselineGeneration != e.BaselineGeneration) {
		return fail(fmt.Errorf("candidate/checkpoint coordinates differ"))
	}
	if registered && !found {
		return fail(fmt.Errorf("registered candidate result has no integration checkpoint"))
	}
	// A resumable checkpoint is the fact source for this responsibility. It is
	// a lifecycle fact about the recorded work, not a revision, round or digest
	// comparison, and it stays valid after the Worker checkout is reclaimed.
	resumable := found && (cp.State == integration.StateReady || cp.State == integration.StateMerged || cp.State == integration.StateVerified || cp.State == integration.StateAcknowledged || cp.State == integration.StateCleanupPending || cp.State == integration.StateComplete)
	if deferredNative && !registered {
		// Main resolves its actual checkout only when integration begins.
		actual, bindErr := workspace.New(ctx, *root)
		if bindErr != nil {
			return fail(bindErr)
		}
		if actual.MainRoot != b.MainRoot || actual.Branch != b.Branch {
			return fail(fmt.Errorf("Main checkout differs from its bound development workspace"))
		}
		b.CommonDir = actual.CommonDir
		if !resumable {
			// Actual source inspection begins with Main's integration responsibility,
			// after the professional Result has already been accepted. A reclaimed
			// Worker checkout has no branch to report, so a resumable checkpoint
			// never asks it for one.
			e.Branch, err = workspace.Git(ctx, e.Path, "branch", "--show-current")
			if err != nil {
				return fail(err)
			}
			a.Branch = e.Branch
		}
	}
	mainCommands, err := workspaceMainCheckCommandsForResponsibility(ctx, e.Checks, checkCommands, cp, fmt.Sprintf("owner=%s assignment=%s packet=%s", e.AgentID, e.AssignmentID, typedPacket.ID))
	if err != nil {
		return fail(err)
	}
	// Main selects its actual checks here. The Worker execution remains the
	// original work definition; these commands are not frozen from prechecks.
	mainExecution := e
	mainExecution.Checks = mainCommands
	if found && (cp.State == integration.StateVerified || cp.State == integration.StateAcknowledged || cp.State == integration.StateCleanupPending || cp.State == integration.StateComplete) {
		if _, err := verifiedDomainChecks(*root, mainExecution, cp); err != nil {
			return fail(fmt.Errorf("selected Main checks have not passed at the checkpoint tested HEAD: %w", err))
		}
	}
	if registered {
		cp, err = acknowledgeAcceptedRepairResult(ctx, *root, snap, typedPacket, e, cp)
		if err != nil {
			return fail(err)
		}
	}
	config := integration.IntegrateConfig{Root: *root, GitRoot: *root, RuntimeID: e.RuntimeID, RequiredChecks: mainCommands, CheckRunner: integration.CommandCheckRunner}
	// Cleanup is independent of original receipt, merge and Main checks. The
	// existing cleanup observes actual worktree status and preserves unknown
	// or private material; it does not build a digest-based deletion proof.

	var inspected integration.Inspection
	if resumable {
		// The recorded checkpoint is the fact source for this responsibility.
		// A live inspection and its checks are not repeated here.
		inspected = integration.InspectionFromCheckpoint(cp)

	} else {
		if err = b.CleanInputs(ctx); err != nil {
			return fail(err)
		}
		a.WorktreePath, a.Branch, a.TargetBranch = e.Path, e.Branch, e.TargetBranch
		// S9's original Result has already been received by its own consumer;
		// S6's separate completion-report file is not part of this responsibility.
		inspectCfg := integration.InspectConfig{SkipCompletionCheck: true}

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
		snap, err = writer.Snapshot()
		if err != nil {
			return fail(err)
		}
		candidate.Proposal.Checks, err = verifiedDomainChecks(*root, mainExecution, cp)
		if err != nil {
			return fail(err)
		}
		proof, proofErr := repairResultMainProofContext(ctx, *root, e, cp)
		if proofErr != nil {
			return fail(proofErr)
		}
		var result repair.RepairResult
		snap, result, resultRef, err = repair.SubmitIssuedRepairResult(ctx, *root, statePath, journalPath, repair.SubmitIssuedRepairResultRequest{
			ExpectedRevision: snap.Revision,
			PacketID:         typedPacket.ID,
			OwnerAgentID:     e.AgentID,
			MainProof:        proof,
			Checkpoint:       &cp,
			MainChecks:       candidate.Proposal.Checks,
			OccurredAt:       time.Now().UTC(),
		})
		if err != nil {
			return fail(err)
		}
		acceptedFinal = true
		if result.Result != "pass" {
			return fail(fmt.Errorf("domain result %s is %s; preserve Worker and follow the recorded repair recovery route", result.ResultID, result.Result))
		}
	}

	cp, err = acknowledgeAcceptedRepairResult(ctx, *root, snap, typedPacket, e, cp)
	if err != nil {
		return fail(err)
	}
	// Final reception unlocks its original successor independently of resource
	// retirement. Prepare once through the original idempotent issuer before
	// cleanup can return pending; never reach this boundary after failed checks.
	snap, err = writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	impactNext := ""
	if stringValue(repairPointerCLI(snap.State)["status"]) == "impact_reconciliation" {
		impactPath, impactErr := prepareRepairImpactQuestionnaire(*root, snap.State)
		if impactErr != nil {
			return fail(fmt.Errorf("Main integration recorded; ChangeImpact preparation: %w. Restore the named source/filesystem failure, then use %s; preserve accepted Final Results and completed integration", impactErr, repairImpactPreparationRecovery(*root)))
		}
		impactNext = "Main must complete and save ChangeImpact at " + impactPath + "."
		fmt.Fprintln(stderr, impactNext)
	}
	finished, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: integration.InspectionFromCheckpoint(cp), Acknowledge: true, Cleanup: true}, config)
	if err != nil {
		return fail(err)
	}
	snap, err = persistExecutionCompletionContext(ctx, *root, snap, e.AssignmentID)
	if err != nil {
		return fail(err)
	}
	snap, err = writer.Snapshot()
	if err != nil {
		return fail(err)
	}
	pointer := repairPointerCLI(snap.State)
	next := stringValue(pointer["next_action"])
	if next == "" {
		next = "Main integration recorded; current repair status: " + stringValue(pointer["status"]) + ". Continue the remaining repair or test work and Main checks."
	}
	if impactNext != "" {
		next = impactNext
	}
	return encodeJSON(stdout, map[string]any{"checkpoint": finished.Checkpoint, "result_ref": resultRef, "revision": snap.Revision, "next": next})
}

// Final reception is the S9 domain acknowledgement. Complete that original
// checkpoint step after its Writer commit, including recovery between the two
// writes. Keep the accepted verified facts; later Main work has its own checks.
func acknowledgeAcceptedRepairResult(ctx context.Context, root string, snapshot runtime.Snapshot, packet workpacket.RepairResultPacket, execution workspace.Execution, checkpoint integration.Checkpoint) (integration.Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return checkpoint, err
	}
	if checkpoint.State != integration.StateVerified {
		return checkpoint, nil
	}
	if _, found, err := runtime.VerifiedRepairResultReceiptContext(ctx, root, snapshot.State, packet.ID, execution.AgentID); err != nil || !found {
		return checkpoint, fmt.Errorf("S9 Final acknowledgement requires its original accepted Result and unchanged Main proof; Main must inspect the original receipt and checkpoint without resubmitting Worker work: found=%t: %v", found, err)
	}
	// A rewritten target is a current Main problem. Detect it before the
	// generic failure path could retire the accepted checkpoint's proof.
	if _, err := integration.RecoveryBaseForCheckpoint(ctx, root, checkpoint); err != nil {
		return checkpoint, fmt.Errorf("S9 Final remains accepted; Main must reconcile the original merge with its target before cleanup: %w", err)
	}
	next := checkpoint
	next.State = integration.StateAcknowledged
	return integration.DefaultCheckpointStore().CompareAndSwap(workspaceExecutionCheckpointPath(root, execution), checkpoint, next)
}

// currentRepairResultPacketForDelivery identifies the packet for the original
// accepted S9 result using its existing responsibility association. A
// candidate without original provenance is resolved by the typed selector: a
// registered typed S9 packet for this assignment stays typed and is refused
// explicitly instead of being answered by an older fallback.
func currentRepairResultPacketForDelivery(root string, state map[string]any, execution workspace.Execution, candidate repair.WorkspaceCandidate) (workpacket.RepairResultPacket, bool, error) {
	return currentRepairResultPacketForDeliveryContext(context.Background(), root, state, execution, candidate)
}

func currentRepairResultPacketForDeliveryContext(ctx context.Context, root string, state map[string]any, execution workspace.Execution, candidate repair.WorkspaceCandidate) (workpacket.RepairResultPacket, bool, error) {
	if err := ctx.Err(); err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	if candidate.Provenance == nil {
		// The original candidate carries no provenance. Ask the typed selector
		// whether this assignment and owner still own a registered typed S9
		// packet before answering for the current responsibility.
		packet, typed, err := typedRepairResultPacketForAssignmentContext(ctx, root, state, execution.AssignmentID, execution.AgentID)
		if err != nil {
			return packet, true, fmt.Errorf("typed S9 candidate original provenance is unavailable; preserve registered source %s and restore original packet binding; no legacy fallback: %w", execution.DeliveryRef, err)
		}
		if typed {
			// This responsibility owns a typed S9 packet, so its original
			// provenance is a named gap, not an absent responsibility.
			return packet, true, fmt.Errorf("typed S9 candidate original provenance is unavailable; preserve registered source %s and restore original packet binding; no legacy fallback", execution.DeliveryRef)
		}
		// No typed S9 responsibility is registered for this assignment. The
		// caller reports the unavailable original packet for the current
		// responsibility.
		return workpacket.RepairResultPacket{}, false, nil
	}
	packet, found, err := runtime.FindRepairResultPacketContext(runtime.WithPacketBodyRoot(ctx, root), state, candidate.Provenance.PacketID)
	if err != nil || !found {
		return workpacket.RepairResultPacket{}, false, fmt.Errorf("current candidate packet is unavailable: %v", err)
	}
	selected, err := runtime.RepairResultExecution(state, packet)
	if err != nil || selected["assignment_id"] != execution.AssignmentID {
		return packet, false, fmt.Errorf("candidate original registered execution differs: %v", err)
	}
	b := packet.Binding.Sources
	pointer := repairPointerCLI(state)
	if !repairResultCandidateMatchesPacketExecution(candidate, packet, execution) ||
		pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef ||
		candidate.Proposal.AssignmentID != b.RepairAssignmentID ||
		stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != execution.AgentID {
		return packet, false, fmt.Errorf("candidate does not correspond to the current execution attempt")
	}
	return packet, true, nil
}

// The registered execution is checked by the original Runtime projection at
// the caller. Share the original consumer's tuple predicate instead of keeping
// a second, weaker list of provenance fields before Git integration.
func repairResultCandidateMatchesPacketExecution(candidate repair.WorkspaceCandidate, packet workpacket.RepairResultPacket, execution workspace.Execution) bool {
	return candidate.Provenance != nil &&
		runtime.ValidateRepairResultProvenanceBinding(packet, *candidate.Provenance) == nil &&
		candidate.Provenance.ExecutionAssignmentID == execution.AssignmentID &&
		candidate.Provenance.ExecutionGeneration == execution.Generation &&
		candidate.Provenance.BaselineGeneration == execution.BaselineGeneration &&
		filepath.Clean(candidate.Provenance.ExecutionRoot) == filepath.Clean(execution.Path) &&
		candidate.Provenance.ReworkRef == execution.ReworkRef &&
		candidate.RuntimeID == execution.RuntimeID && candidate.BaselineGeneration == execution.BaselineGeneration &&
		candidate.ExecutionGeneration == execution.Generation && candidate.AssignmentID == execution.AssignmentID &&
		candidate.SessionID == packet.Binding.Sources.SessionID && candidate.PlanID == packet.Binding.Sources.PlanID &&
		candidate.Proposal.ResultID == packet.ResultID && candidate.Proposal.AssignmentID == packet.Binding.Sources.RepairAssignmentID &&
		candidate.Proposal.ProducerAgentID == execution.AgentID
}

func repairResultMainProof(root string, execution workspace.Execution, checkpoint integration.Checkpoint) (runtime.RepairResultMainProof, error) {
	return repairResultMainProofContext(context.Background(), root, execution, checkpoint)
}
func repairResultMainProofContext(ctx context.Context, root string, execution workspace.Execution, checkpoint integration.Checkpoint) (runtime.RepairResultMainProof, error) {
	if err := ctx.Err(); err != nil {
		return runtime.RepairResultMainProof{}, err
	}
	path := workspaceExecutionCheckpointPath(root, execution)
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return runtime.RepairResultMainProof{}, fmt.Errorf("S9 Main checkpoint escaped its registered slot")
	}
	data, err := workpacket.ReadFile(root, filepath.ToSlash(relative), 20<<20)
	if err != nil {
		return runtime.RepairResultMainProof{}, fmt.Errorf("read verified S9 Main checkpoint: %w", err)
	}
	facts, err := runtime.ProjectRepairResultMainFacts(data)
	if err != nil {
		return runtime.RepairResultMainProof{}, err
	}
	if checkpoint.State != integration.StateVerified && checkpoint.State != integration.StateAcknowledged && checkpoint.State != integration.StateCleanupPending && checkpoint.State != integration.StateComplete {
		return runtime.RepairResultMainProof{}, fmt.Errorf("S9 Main checkpoint is not verified")
	}
	return runtime.RepairResultMainProof{
		CheckpointRef: filepath.ToSlash(relative),
		VerifiedFacts: facts,
	}, nil
}

// workspaceMainCheckCommands selects this Main operation's commands. Existing
// checkpoint records allow a completed operation to be inspected again without
// asking Main to re-enter commands it already executed.
func workspaceMainCheckCommands(configured, selected []string, cp integration.Checkpoint) ([]string, error) {
	return workspaceMainCheckCommandsContext(context.Background(), configured, selected, cp)
}

func workspaceMainCheckCommandsContext(ctx context.Context, configured, selected []string, cp integration.Checkpoint) ([]string, error) {
	return workspaceMainCheckCommandsForResponsibility(ctx, configured, selected, cp, "")
}

// workspaceMainCheckCommandsForResponsibility names the exact original
// responsibility in its own diagnostic, so an unavailable Main check selection
// still identifies the assignment, agent and packet it belongs to.
func workspaceMainCheckCommandsForResponsibility(ctx context.Context, configured, selected []string, cp integration.Checkpoint, responsibility string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	commands := configured
	if len(selected) > 0 {
		commands = selected
	}
	if len(commands) == 0 && (cp.State == integration.StateVerified || cp.State == integration.StateAcknowledged || cp.State == integration.StateCleanupPending || cp.State == integration.StateComplete) {
		for _, path := range cp.CheckReceipts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var receipt struct {
				Command string `json:"command"`
				Status  string `json:"status"`
				Exit    *int   `json:"exit_code"`
			}
			if err := json.Unmarshal(data, &receipt); err != nil {
				return nil, err
			}
			if receipt.Status == "pass" && receipt.Exit != nil && *receipt.Exit == 0 {
				commands = append(commands, receipt.Command)
			}
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, command := range commands {
		if strings.TrimSpace(command) == "" || strings.HasPrefix(strings.TrimSpace(command), "locked:") {
			continue
		}
		if !seen[command] {
			out = append(out, command)
			seen[command] = true
		}
	}
	if len(out) == 0 {
		message := "S9 Main check selection is unavailable"
		if responsibility != "" {
			message += " for original responsibility " + responsibility
		}
		return nil, fmt.Errorf("%s: select necessary checks with --check-command for unfinished work; for an accepted Final, Main must recover its original checkpoint check receipts before cleanup, without resubmitting Worker work", message)
	}
	return out, nil
}
