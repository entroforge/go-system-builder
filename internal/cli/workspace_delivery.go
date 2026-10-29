package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	var b *workspace.ExecutionRegistry
	if verb == "integrate" {
		data, err := json.Marshal(snap.State["workspace"])
		if err != nil {
			return fail(err)
		}
		if err := json.Unmarshal(data, &b); err != nil {
			return fail(err)
		}
	} else {
		b, err = workspace.Decode(snap.State)
		if err != nil {
			return fail(err)
		}
	}
	if b == nil {
		return fail(fmt.Errorf("bind Main first"))
	}
	if verb != "integrate" {
		if err := b.Validate(ctx, *root); err != nil {
			return fail(err)
		}
	}
	e, ok := b.Execution(*id, workspace.RuntimeID(snap.State), workspace.Generation(snap.State))
	if !ok || e.AgentID != *agent || (e.Status != "ready" && !(verb == "integrate" && e.Status == "complete")) {
		return fail(fmt.Errorf("delivery requires the registered execution and owner"))
	}
	if e.Purpose == workspace.DocumentReviewPurpose {
		return fail(fmt.Errorf("S5 document review has no product delivery/integration; use the answer receipt"))
	}
	var loaded *hookctx.LoadedContext
	var a *hookctx.AssignmentContext
	if verb != "integrate" {
		loaded, err = hookctx.LoadFull(*root, e.AgentID)
		if err != nil {
			return fail(err)
		}
		for i := range loaded.Assignments {
			if loaded.Assignments[i].AssignmentID == e.AssignmentID && loaded.Assignments[i].OwnerAgentID == e.AgentID {
				a = &loaded.Assignments[i]
				break
			}
		}
		if a == nil {
			return fail(fmt.Errorf("delivery assignment is no longer registered"))
		}
	}
	if verb == "deliver" {
		if loaded.PolicyContext.Agent == nil || loaded.PolicyContext.Agent.LifecycleActivity() != "reported" {
			return fail(fmt.Errorf("record the genuine Worker completion_report before proposing S9 delivery; the final domain RepairResult is submitted only after merge"))
		}
		if err = b.ValidateExecution(ctx, e, false); err != nil {
			return fail(err)
		}
		if err = b.ValidateInputs(e); err != nil {
			return fail(err)
		}
		submissionPaths, err := b.WorkerSubmissionPaths(e)
		if err != nil {
			return fail(err)
		}
		path := filepath.Join(e.Path, filepath.FromSlash(submissionPaths.RepairResult))
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
	// Explain the original reception boundary before opening a candidate path.
	// Execution registration alone does not establish any delivery fact.
	if e.DeliveryRef == "" || e.DeliverySHA256 == "" {
		handoff := s9HandoffForExecution(ctx, *root, snap, e)
		if e.DeliveryRef == "" && e.DeliverySHA256 == "" {
			return fail(fmt.Errorf("%s %s", handoff.summary(*root), s9UnreceivedActionContext(ctx, *root, handoff)))
		}
		return fail(fmt.Errorf("%s Original candidate binding is incomplete: delivery_ref=%q delivery_sha256=%q. Main restores this exact registered source and binding; no legacy fallback or replacement candidate is allowed", handoff.summary(*root), e.DeliveryRef, e.DeliverySHA256))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	ref := repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256}
	data, err := workpacket.ReadFile(*root, ref.Path, 20<<20)
	if err != nil {
		return fail(fmt.Errorf("original candidate source %s for owner=%s assignment=%s is unavailable; Main restores this exact source and preserves accepted work: %w", ref.Path, e.AgentID, e.AssignmentID, err))
	}
	if workpacket.Digest(data) != ref.SHA256 {
		return fail(fmt.Errorf("candidate bytes differ from the original registered delivery SHA256: %s owner=%s assignment=%s; Main restores the original source", ref.Path, e.AgentID, e.AssignmentID))
	}
	var candidate repair.WorkspaceCandidate
	if err := json.Unmarshal(data, &candidate); err != nil {
		return fail(fmt.Errorf("original candidate source %s owner=%s assignment=%s is invalid; Main restores this source: %w", ref.Path, e.AgentID, e.AssignmentID, err))
	}
	if candidate.RuntimeID != e.RuntimeID || candidate.BaselineGeneration != e.BaselineGeneration || candidate.ExecutionGeneration != e.Generation || candidate.AssignmentID != e.AssignmentID || candidate.BaseCommit != e.BaseCommit {
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
			return fail(fmt.Errorf("original candidate is accepted; actual professional work remains unfinished. Main follows the recorded professional omissions and original rework path; do not repeat accepted content or integrate it as completed"))
		}
	}
	acceptedFinal = registered
	if !typedResult {
		loaded, err = hookctx.LoadFull(*root, e.AgentID)
		if err != nil {
			return fail(err)
		}
		for i := range loaded.Assignments {
			if loaded.Assignments[i].AssignmentID == e.AssignmentID && loaded.Assignments[i].OwnerAgentID == e.AgentID {
				a = &loaded.Assignments[i]
				break
			}
		}
		if a == nil {
			return fail(fmt.Errorf("legacy delivery assignment is unavailable"))
		}
		if !registered {
			if _, err := repair.ValidateWorkspaceCandidate(*root, snap.State, e, ref); err != nil {
				return fail(err)
			}
		}
	} else {
		a = &hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID,
			TaskID: typedPacket.Binding.Sources.TaskID, WorktreePath: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch}
	}
	cpPath := workspaceExecutionCheckpointPath(*root, e)
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
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
	mainCommands, err := workspaceMainCheckCommandsContext(ctx, e.Checks, checkCommands, cp)
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
	if registered && typedResult {
		cp, err = acknowledgeAcceptedRepairResult(ctx, *root, snap, typedPacket, e, cp)
		if err != nil {
			return fail(err)
		}
	}
	config := integration.IntegrateConfig{Root: *root, GitRoot: *root, RuntimeID: e.RuntimeID, RequiredChecks: mainCommands, CheckRunner: integration.CommandCheckRunner}
	if typedResult && cp.State != integration.StateComplete {
		config.WorkerLogEvidence, err = repair.WorkspaceCandidateLogProjectionForPacket(ctx, *root, typedPacket, e, candidate)
		if err != nil {
			if !registered {
				return fail(err)
			}
			config.WorkerCleanupError = errors.Join(config.WorkerCleanupError, err)
		}
		config.WorkerPrivateEvidence, err = repair.NativeS9PrivateProjection(ctx, *root, typedPacket, e, candidate, registered)
		if err != nil {
			if !registered {
				return fail(err)
			}
			config.WorkerCleanupError = errors.Join(config.WorkerCleanupError, err)
		}
		privateArea, areaErr := workspaceNativePrivateArea(ctx, *root, typedPacket, e)
		if areaErr != nil {
			if !registered {
				return fail(areaErr)
			}
			config.WorkerCleanupError = errors.Join(config.WorkerCleanupError, areaErr)
		} else {
			config.WorkerPrivateArea = privateArea.Path
			config.WorkerPrivateBaseCommit = privateArea.BaseCommit
			config.WorkerPrivateWritePaths = privateArea.WritePaths
			config.WorkerPrivateBaselinePaths = privateArea.BaselinePaths
		}
		config.WorkerIgnoredEvidence, err = workspaceIgnoredOriginalProjection(ctx, *root, e.Path, typedPacket, candidate)
		if err != nil {
			if !registered {
				return fail(err)
			}
			config.WorkerCleanupError = errors.Join(config.WorkerCleanupError, err)
		}
	}
	var inspected integration.Inspection
	if found && (cp.State == integration.StateReady || cp.State == integration.StateMerged || cp.State == integration.StateVerified || cp.State == integration.StateAcknowledged || cp.State == integration.StateCleanupPending || cp.State == integration.StateComplete) {
		inspected = integration.InspectionFromCheckpoint(cp)
		if !typedResult {
			inspected, err = integration.RefreshCompletionBinding(*root, e.RuntimeID, a.CompletionRef, inspected)
			if err != nil {
				return fail(err)
			}
		}
	} else {
		if err = b.CleanInputs(ctx); err != nil {
			return fail(err)
		}
		if !typedResult {
			if err = b.ValidateExecution(ctx, e, false); err != nil {
				return fail(err)
			}
		}
		a.WorktreePath, a.Branch, a.TargetBranch = e.Path, e.Branch, e.TargetBranch
		locked := []string{}
		if !typedResult {
			a.WritePaths = e.WritePaths
			for _, v := range loaded.PolicyContext.LockedArtifacts {
				locked = append(locked, v.Path)
			}
		}
		inspectCfg := integration.InspectConfig{LockedArtifacts: locked, ValidateDelivery: func(ctx context.Context, in integration.Inspection) error {
			if in.SourceHead != candidate.SourceHead {
				return fmt.Errorf("candidate source commit changed")
			}
			if !typedResult {
				_, err := repair.ValidateWorkspaceCandidate(*root, snap.State, e, ref)
				return err
			}
			return nil
		}}
		if typedResult {
			inspectCfg.WorkerLogEvidence = config.WorkerLogEvidence
			inspectCfg.WorkerPrivateEvidence = config.WorkerPrivateEvidence
			inspectCfg.WorkerPrivateArea = config.WorkerPrivateArea
			inspectCfg.WorkerPrivateBaseCommit = config.WorkerPrivateBaseCommit
			inspectCfg.WorkerPrivateWritePaths = config.WorkerPrivateWritePaths
			inspectCfg.WorkerPrivateBaselinePaths = config.WorkerPrivateBaselinePaths
		}
		if typedResult && e.Purpose == workspace.NativeDeliveryPurpose && candidate.SourceHead == candidate.BaseCommit && len(candidate.Proposal.ChangedArtifacts) == 0 {
			inspectCfg.ReceivedSourceHead = candidate.SourceHead
		}
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
		candidate.Proposal.Checks, err = verifiedDomainChecks(*root, mainExecution, cp)
		if err != nil {
			return fail(err)
		}
		var result repair.RepairResult
		if typedResult {
			proof, proofErr := repairResultMainProofContext(ctx, *root, e, cp)
			if proofErr != nil {
				return fail(proofErr)
			}
			snap, result, resultRef, err = repair.SubmitIssuedRepairResult(ctx, *root, statePath, journalPath, repair.SubmitIssuedRepairResultRequest{
				ExpectedRevision: snap.Revision,
				PacketID:         typedPacket.ID,
				OwnerAgentID:     e.AgentID,
				MainProof:        proof,
				OccurredAt:       time.Now().UTC(),
			})
		} else {
			snap, result, resultRef, err = repair.SubmitRepairResultToRuntime(*root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: snap.Revision, Actor: e.AgentID}, Result: candidate.Proposal})
		}
		if err != nil {
			return fail(err)
		}
		acceptedFinal = true
		if result.Result != "pass" {
			return fail(fmt.Errorf("domain result %s is %s; preserve Worker and follow the recorded repair recovery route", result.ResultID, result.Result))
		}
	}

	if !typedResult && a.CompletionAckRef == "" {
		for _, other := range b.Executions {
			if other.AgentID != e.AgentID || other.AssignmentID == e.AssignmentID || other.DeliveryRef == "" || other.RuntimeID != e.RuntimeID || other.BaselineGeneration != e.BaselineGeneration {
				continue
			}
			c, err := repair.ReadWorkspaceCandidate(*root, repair.ArtifactRef{Path: other.DeliveryRef, SHA256: other.DeliverySHA256})
			if err != nil {
				return fail(err)
			}
			if _, done, err := repair.RegisteredWorkspaceResultContext(ctx, *root, snap.State, c); err != nil {
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
	if typedResult {
		cp, err = acknowledgeAcceptedRepairResult(ctx, *root, snap, typedPacket, e, cp)
		if err != nil {
			return fail(err)
		}
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

// currentRepairResultPacketForDelivery identifies an issued typed S9 packet
// from immutable candidate provenance. If no provenance exists, any issued
// packet for this Assignment still prevents fallback to the legacy Writer.
func currentRepairResultPacketForDelivery(root string, state map[string]any, execution workspace.Execution, candidate repair.WorkspaceCandidate) (workpacket.RepairResultPacket, bool, error) {
	return currentRepairResultPacketForDeliveryContext(context.Background(), root, state, execution, candidate)
}

func currentRepairResultPacketForDeliveryContext(ctx context.Context, root string, state map[string]any, execution workspace.Execution, candidate repair.WorkspaceCandidate) (workpacket.RepairResultPacket, bool, error) {
	if err := ctx.Err(); err != nil {
		return workpacket.RepairResultPacket{}, false, err
	}
	if candidate.Provenance == nil {
		packet, typed, err := typedRepairResultPacketForAssignmentContext(ctx, root, state, execution.AssignmentID, execution.AgentID)
		if err != nil {
			return packet, typed, err
		}
		if typed || execution.Purpose == workspace.NativeDeliveryPurpose {
			return packet, true, fmt.Errorf("typed S9 candidate original provenance is unavailable; preserve registered source %s and restore original packet binding; no legacy fallback", execution.DeliveryRef)
		}
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
	packetSHA, err := workpacket.RepairResultPacketDigest(packet)
	if err != nil {
		return packet, false, fmt.Errorf("candidate original packet differs: %w", err)
	}
	b := packet.Binding.Sources
	pointer := repairPointerCLI(state)
	if !repairResultCandidateMatchesPacketExecution(candidate, packet, execution, packetSHA) ||
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
func repairResultCandidateMatchesPacketExecution(candidate repair.WorkspaceCandidate, packet workpacket.RepairResultPacket, execution workspace.Execution, packetSHA string) bool {
	return candidate.Provenance != nil && packetSHA != "" && candidate.Provenance.PacketSHA256 == packetSHA &&
		runtime.ValidateRepairResultProvenanceBinding(packet, *candidate.Provenance) == nil &&
		candidate.RuntimeID == execution.RuntimeID && candidate.BaselineGeneration == execution.BaselineGeneration &&
		candidate.ExecutionGeneration == execution.Generation && candidate.AssignmentID == execution.AssignmentID &&
		candidate.BaseCommit == execution.BaseCommit && candidate.SourceHead == candidate.Provenance.SourceHead &&
		candidate.SessionID == packet.Binding.Sources.SessionID && candidate.PlanID == packet.Binding.Sources.PlanID &&
		candidate.ContractSHA256 == packet.Binding.Sources.ContractSHA256 &&
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
	checkpointHash := sha256.Sum256(data)
	facts, err := runtime.ProjectRepairResultMainFacts(data)
	if err != nil {
		return runtime.RepairResultMainProof{}, err
	}
	factsHash := sha256.Sum256(facts)
	if checkpoint.State != integration.StateVerified && checkpoint.State != integration.StateAcknowledged && checkpoint.State != integration.StateCleanupPending && checkpoint.State != integration.StateComplete {
		return runtime.RepairResultMainProof{}, fmt.Errorf("S9 Main checkpoint is not verified")
	}
	return runtime.RepairResultMainProof{
		CheckpointRef:       filepath.ToSlash(relative),
		CheckpointSHA256:    hex.EncodeToString(checkpointHash[:]),
		VerifiedFacts:       facts,
		VerifiedFactsSHA256: hex.EncodeToString(factsHash[:]),
	}, nil
}

// workspaceMainCheckCommands selects this Main operation's commands. Existing
// checkpoint records allow a completed operation to be inspected again without
// asking Main to re-enter commands it already executed.
func workspaceMainCheckCommands(configured, selected []string, cp integration.Checkpoint) ([]string, error) {
	return workspaceMainCheckCommandsContext(context.Background(), configured, selected, cp)
}
func workspaceMainCheckCommandsContext(ctx context.Context, configured, selected []string, cp integration.Checkpoint) ([]string, error) {
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
				Before  string `json:"head_before"`
				After   string `json:"head_after"`
				Exit    int    `json:"exit_code"`
			}
			if err := json.Unmarshal(data, &receipt); err != nil {
				return nil, err
			}
			if receipt.Status == "pass" && receipt.Exit == 0 && receipt.Before == cp.TestedHead && receipt.After == cp.TestedHead {
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
		return nil, fmt.Errorf("S9 Main check selection is unavailable: select necessary checks with --check-command for unfinished work; for an accepted Final, Main must recover its original checkpoint check receipts before cleanup, without resubmitting Worker work")
	}
	return out, nil
}
