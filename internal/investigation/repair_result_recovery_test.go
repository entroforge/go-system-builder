package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS9RepairResultFinalCASSealedPendingRecoveryBeforeOriginalAckCleanup(t *testing.T) {
	prepared := prepareAcceptedS9RepairResultCandidate(t)
	fixture, owner, packet := prepared.fixture, prepared.owner, prepared.packet
	if fixture == nil || packet.ID == "" || prepared.candidateReceipt.CandidateRef == "" {
		t.Fatalf("accepted S9 candidate fixture is incomplete: %+v", prepared)
	}

	var acceptedResult runtime.RepairResultReceipt
	var acceptedResultBytes, finalStateBytes, finalJournalBytes []byte
	var beforeResultStateBytes, beforeResultJournalBytes, resultPendingMarkerBytes []byte
	var finalResultRef repair.ArtifactRef
	var acceptedRevision int

	withS9VerifiedMainCheckpoint(t, fixture, owner, packet, func(ctx context.Context, snapshot runtime.Snapshot, execution workspace.Execution, checkpoint integration.Checkpoint, _ repair.ArtifactRef) {
		if snapshot.Revision != prepared.acceptedSnapshot.Revision {
			t.Fatalf("Runtime revision changed between the accepted candidate and final Result: current=%d accepted=%d", snapshot.Revision, prepared.acceptedSnapshot.Revision)
		}
		proof := s9MainProofFromVerifiedCheckpoint(t, fixture.root, execution, checkpoint)
		beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)

		after, result, resultRef, err := repair.SubmitIssuedRepairResult(ctx, fixture.root, fixture.statePath, fixture.journalPath, repair.SubmitIssuedRepairResultRequest{
			ExpectedRevision: snapshot.Revision,
			PacketID:         packet.ID,
			OwnerAgentID:     owner.agentID,
			MainProof:        proof,
			OccurredAt:       time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("original typed S9 Result writer after real Main verification: %v", err)
		}
		if after.Revision != snapshot.Revision+1 || result.ResultID != packet.ResultID || result.Result != "pass" || result.Verdict != "completed" || result.Receipt == nil {
			t.Fatalf("original typed S9 final Result commit = revision %d, result=%+v; want exactly one completed Result", after.Revision, result)
		}
		finalStateBytes, finalJournalBytes = mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
		if bytes.Equal(beforeState, finalStateBytes) || bytes.Equal(beforeJournal, finalJournalBytes) {
			t.Fatal("original Result submit did not commit a new Runtime state/journal pair")
		}
		receipt, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, after.State, packet.ID, owner.agentID)
		if err != nil || !found || !receipt.WorkComplete || receipt.ResultRef != resultRef.Path || receipt.ResultID != packet.ResultID {
			t.Fatalf("read exact original final S9 Result receipt: found=%t receipt=%+v err=%v", found, receipt, err)
		}
		acceptedResult = receipt
		acceptedResultBytes = mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(acceptedResult.ResultRef)))
		if workpacket.Digest(acceptedResultBytes) != resultRef.SHA256 {
			t.Fatal("original S9 final Result bytes differ from the accepted Runtime ref SHA")
		}
		assertS9FinalResultEventOccursOnce(t, finalJournalBytes, acceptedResult, packet.Binding.Sources.RuntimeID, packet.Binding.Sources.BaselineGeneration)
		acceptedRevision = after.Revision
		finalResultRef = resultRef

		// Build a real sealed-pair marker from the original before/after CAS
		// outputs. The Result artifact itself remains on disk while the Runtime
		// pair is restored to its pre-Result revision.
		marker := buildS8SealedCommitMarker(t, beforeState, beforeJournal, finalStateBytes, finalJournalBytes)
		if err := os.WriteFile(fixture.statePath, beforeState, 0o600); err != nil {
			t.Fatalf("restore exact pre-Result Runtime state: %v", err)
		}
		if err := os.WriteFile(fixture.journalPath, beforeJournal, 0o600); err != nil {
			t.Fatalf("restore exact pre-Result Runtime journal: %v", err)
		}
		markerPath := fixture.statePath + ".commit-pending.json"
		writeInvestigationJSON(t, markerPath, marker)
		markerBytes := mustRead(t, markerPath)
		beforeResultStateBytes = append([]byte(nil), beforeState...)
		beforeResultJournalBytes = append([]byte(nil), beforeJournal...)
		resultPendingMarkerBytes = append([]byte(nil), markerBytes...)

		// Receipt is a read-only observation path. With an actual sealed pending
		// pair it must report the recovery boundary and leave every byte intact.
		var receiptStdout, receiptStderr bytes.Buffer
		if code := cli.Run([]string{"runtime", "stage-answer", "receipt", "--root", fixture.root, "--agent", owner.agentID, "--packet", packet.ID}, nil, &receiptStdout, &receiptStderr); code == 0 || !strings.Contains(receiptStderr.String(), runtime.ErrPendingRuntimeOperation.Error()) {
			t.Fatalf("public S9 receipt must fail with the pending-operation diagnostic: code=%d stderr=%s stdout=%s", code, receiptStderr.String(), receiptStdout.String())
		}
		assertS9RecoveryPairUnchanged(t, fixture, beforeState, beforeJournal, "public read-only S9 receipt during pending Result recovery")
		if !bytes.Equal(markerBytes, mustRead(t, markerPath)) {
			t.Fatal("public read-only S9 receipt changed the pending Result marker")
		}

		readOnly, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
		if !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
			t.Fatalf("read-only Runtime snapshot accepted or repaired sealed pending Result pair: snapshot=%+v err=%v", readOnly, err)
		}
		assertS9RecoveryPairUnchanged(t, fixture, beforeState, beforeJournal, "read-only pending Result observation")
		if !bytes.Equal(markerBytes, mustRead(t, markerPath)) {
			t.Fatal("read-only Runtime observation changed the original sealed Result marker")
		}

		writer := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
		removed, err := writer.RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
			ExpectedRevision: snapshot.Revision,
			ArtifactPath:     finalResultRef.Path,
			ArtifactSHA256:   finalResultRef.SHA256,
		})
		if err != nil || removed {
			t.Fatalf("pending marker cleanup removed the unreferenced-but-marker-bound S9 Result: removed=%t err=%v", removed, err)
		}
		if !bytes.Equal(acceptedResultBytes, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(finalResultRef.Path)))) {
			t.Fatal("pending cleanup changed the marker-bound S9 Result artifact")
		}

		recovered, err := writer.RecoverPendingOperations()
		if err != nil || !recovered {
			t.Fatalf("original Runtime Writer did not recover the sealed S9 Result pair: recovered=%t err=%v", recovered, err)
		}
		recoveredSnapshot, err := writer.Snapshot()
		if err != nil || recoveredSnapshot.Revision != acceptedRevision {
			t.Fatalf("read recovered original S9 Result snapshot: revision=%d want=%d err=%v", recoveredSnapshot.Revision, acceptedRevision, err)
		}
		if !bytes.Equal(finalStateBytes, mustRead(t, fixture.statePath)) || !bytes.Equal(finalJournalBytes, mustRead(t, fixture.journalPath)) {
			t.Fatal("original Writer recovery did not install the exact sealed S9 Result pair")
		}
		if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovered S9 final Result marker remains: %v", err)
		}
		recoveredReceipt, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, recoveredSnapshot.State, packet.ID, owner.agentID)
		if err != nil || !found || !reflect.DeepEqual(recoveredReceipt, acceptedResult) {
			t.Fatalf("verify unique recovered original S9 Result receipt: found=%t receipt=%+v err=%v", found, recoveredReceipt, err)
		}
		assertS9FinalResultEventOccursOnce(t, mustRead(t, fixture.journalPath), recoveredReceipt, packet.Binding.Sources.RuntimeID, packet.Binding.Sources.BaselineGeneration)
		if !bytes.Equal(acceptedResultBytes, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(recoveredReceipt.ResultRef)))) {
			t.Fatal("original Result recovery changed the accepted immutable Result bytes")
		}

	})

	// Acceptance freezes the exact Main facts. A real later contribution must
	// survive sealed-pair recovery and cleanup without rerunning those checks,
	// rewriting the accepted Final, or resetting the isolated Main target.
	checkpointPath := integration.DefaultCheckpointStore().Path(fixture.root, packet.Binding.Sources.RuntimeID, packet.Binding.Sources.BaselineGeneration, prepared.execution.AssignmentID)
	checkpointBytesBefore := mustRead(t, checkpointPath)
	var verifiedCheckpoint integration.Checkpoint
	if err := json.Unmarshal(checkpointBytesBefore, &verifiedCheckpoint); err != nil || verifiedCheckpoint.State != integration.StateVerified || verifiedCheckpoint.TestedHead == "" {
		t.Fatalf("load original verified S9 checkpoint: checkpoint=%+v err=%v", verifiedCheckpoint, err)
	}
	laterPath := filepath.Join(fixture.root, "internal/api/later-recovery.go")
	if err := os.WriteFile(laterPath, []byte("package api\n\nconst LaterRecoveryContribution = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), fixture.root, "add", "internal/api/later-recovery.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), fixture.root, "commit", "-m", "Later synthetic Main contribution before Final recovery"); err != nil {
		t.Fatal(err)
	}
	targetAdvanced, err := workspace.Git(context.Background(), fixture.root, "rev-parse", verifiedCheckpoint.TargetBranch)
	if err != nil || targetAdvanced == verifiedCheckpoint.TestedHead {
		t.Fatalf("later contribution did not advance the verified target: head=%q tested=%q err=%v", targetAdvanced, verifiedCheckpoint.TestedHead, err)
	}

	// The public original integrate command must recover the sealed pair itself.
	// Reinstall the exact before-pair and marker after the explicit Writer
	// contrast, without touching the verified checkpoint or accepted sources.
	if err := os.WriteFile(fixture.statePath, beforeResultStateBytes, 0o600); err != nil {
		t.Fatalf("restore original pre-Result Runtime state for public integration recovery: %v", err)
	}
	if err := os.WriteFile(fixture.journalPath, beforeResultJournalBytes, 0o600); err != nil {
		t.Fatalf("restore original pre-Result Runtime journal for public integration recovery: %v", err)
	}
	markerPath := fixture.statePath + ".commit-pending.json"
	if err := os.WriteFile(markerPath, resultPendingMarkerBytes, 0o600); err != nil {
		t.Fatalf("restore the exact sealed Result marker for public integration recovery: %v", err)
	}
	assertS9RecoveryPairUnchanged(t, fixture, beforeResultStateBytes, beforeResultJournalBytes, "reinstall original pre-Result pair before public recovery")
	if !bytes.Equal(resultPendingMarkerBytes, mustRead(t, markerPath)) {
		t.Fatal("reinstalled public recovery marker differs from the original sealed Result marker")
	}

	// The public original command's mutation-capable Runtime snapshot recovers
	// the sealed final Result pair, then performs the ordinary ACK and cleanup.
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "workspace", "integrate", "--root", fixture.root, "--assignment", owner.manifestAssignmentID, "--agent", owner.agentID}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("original S9 ACK/cleanup after sealed Result recovery: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	finalSnapshot := currentS9Snapshot(t, fixture)
	finalReceipt, found, err := runtime.VerifiedRepairResultReceipt(fixture.root, finalSnapshot.State, packet.ID, owner.agentID)
	if err != nil || !found || !reflect.DeepEqual(finalReceipt, acceptedResult) {
		t.Fatalf("original ACK/cleanup changed the recovered S9 Result receipt: found=%t receipt=%+v err=%v", found, finalReceipt, err)
	}
	if finalSnapshot.Revision < acceptedRevision {
		t.Fatalf("original ACK/cleanup regressed Runtime revision from %d to %d", acceptedRevision, finalSnapshot.Revision)
	}
	assertS9FinalResultEventOccursOnce(t, mustRead(t, fixture.journalPath), finalReceipt, packet.Binding.Sources.RuntimeID, packet.Binding.Sources.BaselineGeneration)
	if !bytes.Equal(acceptedResultBytes, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(finalReceipt.ResultRef)))) {
		t.Fatal("public pending recovery and ACK/cleanup changed the immutable S9 Result bytes")
	}
	checkpoint, found, err := integration.DefaultCheckpointStore().Load(checkpointPath)
	if err != nil || !found || checkpoint.State != integration.StateComplete {
		t.Fatalf("original ACK/cleanup did not complete the S9 Main checkpoint: found=%t checkpoint=%+v err=%v", found, checkpoint, err)
	}
	checkpointRelative, err := filepath.Rel(fixture.root, checkpointPath)
	if err != nil || !filepath.IsLocal(checkpointRelative) {
		t.Fatalf("resolve completed S9 checkpoint path: path=%s err=%v", checkpointPath, err)
	}
	checkpointBytes, err := workpacket.ReadFile(fixture.root, filepath.ToSlash(checkpointRelative), 20<<20)
	if err != nil {
		t.Fatalf("read completed original S9 checkpoint proof: %v", err)
	}
	mainFacts, err := runtime.ProjectRepairResultMainFacts(checkpointBytes)
	if err != nil || !bytes.Equal(mainFacts, acceptedResult.MainProof.VerifiedFacts) {
		t.Fatalf("public pending recovery and ACK/cleanup changed immutable S9 Main facts: err=%v", err)
	}
	if head, err := workspace.Git(context.Background(), fixture.root, "rev-parse", verifiedCheckpoint.TargetBranch); err != nil || head != targetAdvanced {
		t.Fatalf("accepted Final recovery lost the later Main contribution: head=%s want=%s err=%v", head, targetAdvanced, err)
	}
	registry, err := workspace.Decode(finalSnapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original S9 Workspace registry after cleanup: %v", err)
	}
	finalExecution, found := registry.Executions[owner.manifestAssignmentID]
	if !found || finalExecution.Status != "complete" {
		t.Fatalf("original S9 ACK/cleanup did not complete the registered Worker execution: found=%t execution=%+v", found, finalExecution)
	}
	if finalReceipt.ResultRef != finalResultRef.Path || finalReceipt.ResultID != packet.ResultID {
		t.Fatalf("final S9 receipt changed Result identity after ACK/cleanup: %+v", finalReceipt)
	}
	if _, err := os.Stat(prepared.workerRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original ACK/cleanup did not remove the completed Worker checkout: %v", err)
	}
}

// withS9VerifiedMainCheckpoint leaves the actual accepted candidate in the
// original Runtime and drives the native Inspect/Integrate APIs through the
// verified checkpoint boundary. The callback runs under the same reentrant
// Main workspace lock so SubmitIssuedRepairResult observes the same source
// ordering as runtime workspace integrate.
func withS9VerifiedMainCheckpoint(
	t *testing.T,
	fixture *intakeFixture,
	owner s9HookOwner,
	packet workpacket.RepairResultPacket,
	accept func(context.Context, runtime.Snapshot, workspace.Execution, integration.Checkpoint, repair.ArtifactRef),
) {
	t.Helper()

	ctx, release, err := integration.LockWorkspace(context.Background(), fixture.root)
	if err != nil {
		t.Fatalf("lock original S9 Main workspace: %v", err)
	}
	defer release()

	statePath := filepath.Join(fixture.root, ".claude/loop-state.json")
	journalPath := filepath.Join(fixture.root, ".claude/loop-events.jsonl")
	writer := runtime.NewWriter(statePath, journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	snapshot, err := writer.Snapshot()
	if err != nil {
		t.Fatalf("read original S9 Runtime before Main inspection: %v", err)
	}
	registeredPacket, found, err := runtime.FindRepairResultPacketContext(runtime.WithPacketBodyRoot(t.Context(), fixture.root), snapshot.State, packet.ID)
	if err != nil || !found {
		t.Fatalf("read original issued S9 RepairResult packet: found=%t err=%v", found, err)
	}
	if err := runtime.VerifyRepairResultPacketCurrent(fixture.root, snapshot.State, registeredPacket); err != nil {
		t.Fatalf("verify current original S9 RepairResult packet: %v", err)
	}
	if got, err := workpacket.RepairResultPacketDigest(registeredPacket); err != nil {
		t.Fatalf("digest original S9 packet: %v", err)
	} else if want, err := workpacket.RepairResultPacketDigest(packet); err != nil || got != want {
		t.Fatalf("current S9 packet differs from the accepted test packet: digest=%s want=%s err=%v", got, want, err)
	}

	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original S9 Workspace registry: %v", err)
	}
	if err := registry.Validate(ctx, fixture.root); err != nil {
		t.Fatalf("validate original S9 Main workspace binding: %v", err)
	}
	execution, ok := registry.Execution(owner.manifestAssignmentID, workspace.RuntimeID(snapshot.State), workspace.Generation(snapshot.State))
	if !ok || execution.AgentID != owner.agentID || execution.Status != "ready" || execution.CheckLocation != "main" {
		t.Fatalf("read exact original S9 Main-check execution: found=%t execution=%+v", ok, execution)
	}
	if err := registry.CleanInputs(ctx); err != nil {
		t.Fatalf("validate original S9 frozen inputs before Main integration: %v", err)
	}
	if err := registry.ValidateExecution(ctx, execution, false); err != nil {
		t.Fatalf("validate original S9 registered execution before Main integration: %v", err)
	}

	candidateReceipt, accepted, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, snapshot.State, packet.ID, owner.agentID)
	if err != nil || !accepted {
		t.Fatalf("read exact original S9 accepted candidate receipt: accepted=%t err=%v", accepted, err)
	}
	candidateRef := repair.ArtifactRef{Path: candidateReceipt.CandidateRef, SHA256: candidateReceipt.CandidateSHA256}
	candidate, err := repair.ReadWorkspaceCandidate(fixture.root, candidateRef)
	if err != nil || candidate.Provenance == nil || candidate.Provenance.PacketID != packet.ID {
		t.Fatalf("read exact immutable S9 Worker candidate: provenance=%+v err=%v", candidate.Provenance, err)
	}
	if _, err := repair.ValidateWorkspaceCandidate(fixture.root, snapshot.State, execution, candidateRef); err != nil {
		t.Fatalf("revalidate original S9 candidate before Main integration: %v", err)
	}

	loaded, err := hookctx.LoadFull(fixture.root, execution.AgentID)
	if err != nil {
		t.Fatalf("load original S9 assignment projection for Main integration: %v", err)
	}
	var assignment *hookctx.AssignmentContext
	for i := range loaded.Assignments {
		if loaded.Assignments[i].AssignmentID == execution.AssignmentID && loaded.Assignments[i].OwnerAgentID == execution.AgentID {
			assignment = &loaded.Assignments[i]
			break
		}
	}
	if assignment == nil {
		t.Fatalf("original S9 assignment %s is no longer registered for %s", execution.AssignmentID, execution.AgentID)
	}
	assignment.WorktreePath = execution.Path
	assignment.Branch = execution.Branch
	assignment.TargetBranch = execution.TargetBranch
	assignment.WritePaths = append([]string{}, execution.WritePaths...)
	lockedArtifacts := make([]string, 0, len(loaded.PolicyContext.LockedArtifacts))
	for _, artifact := range loaded.PolicyContext.LockedArtifacts {
		lockedArtifacts = append(lockedArtifacts, artifact.Path)
	}

	inspectConfig := integration.InspectConfig{
		LockedArtifacts: lockedArtifacts,
		ValidateDelivery: func(_ context.Context, inspection integration.Inspection) error {
			if inspection.SourceHead != candidate.SourceHead {
				return fmt.Errorf("S9 candidate source commit changed: inspected=%s candidate=%s", inspection.SourceHead, candidate.SourceHead)
			}
			_, err := repair.ValidateWorkspaceCandidate(fixture.root, snapshot.State, execution, candidateRef)
			return err
		},
	}
	inspection, err := integration.Inspect(ctx, integration.InspectRequest{
		Root:               fixture.root,
		Assignment:         *assignment,
		TargetBranch:       execution.TargetBranch,
		BaselineGeneration: execution.BaselineGeneration,
		RuntimeID:          execution.RuntimeID,
	}, inspectConfig)
	if err != nil || !inspection.Ready {
		t.Fatalf("original S9 Inspect rejected the accepted candidate: ready=%t blockers=%v err=%v", inspection.Ready, inspection.Blockers, err)
	}

	// Main chooses an actual fixture check; professional red_checks are not
	// execution configuration and no longer create checks implicitly.
	mainChecks := append([]string(nil), execution.Checks...)
	checkRunner := integration.ProfileCheckRunner(execution.CheckProfiles, checkexec.Binding{
		RuntimeID: execution.RuntimeID, Generation: execution.BaselineGeneration,
		AssignmentID: execution.AssignmentID, Owner: execution.AgentID,
		ExecutionGeneration: execution.Generation, Responsibility: "main_required_integration",
	})
	if len(mainChecks) == 0 {
		mainChecks = []string{"test -f internal/api/payload.go"}
		checkRunner = integration.CommandCheckRunner
	}
	integrateConfig := integration.IntegrateConfig{
		Root:           fixture.root,
		GitRoot:        fixture.root,
		RuntimeID:      execution.RuntimeID,
		RequiredChecks: mainChecks,
		CheckRunner:    checkRunner,
	}
	merged, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, integrateConfig)
	if err != nil || merged.Checkpoint.State != integration.StateVerified {
		t.Fatalf("original S9 Main integration did not stop at verified: checkpoint=%+v err=%v", merged.Checkpoint, err)
	}
	if merged.Checkpoint.AssignmentID != execution.AssignmentID || merged.Checkpoint.BaselineGeneration != execution.BaselineGeneration || merged.Checkpoint.WorktreePath != execution.Path || merged.Checkpoint.TestedHead == "" {
		t.Fatalf("verified S9 Main checkpoint lost its original execution coordinates: %+v", merged.Checkpoint)
	}
	checkpointPath := integration.DefaultCheckpointStore().Path(fixture.root, execution.RuntimeID, execution.BaselineGeneration, execution.AssignmentID)
	checkpoint, found, err := integration.DefaultCheckpointStore().Load(checkpointPath)
	if err != nil || !found || checkpoint.State != integration.StateVerified || checkpoint.TestedHead != merged.Checkpoint.TestedHead {
		t.Fatalf("load actual verified S9 Main checkpoint: found=%t checkpoint=%+v err=%v", found, checkpoint, err)
	}

	// Integrate changes only its original checkpoint here. Refresh Runtime
	// through the same mutation-capable reader used by the CLI before the Result
	// CAS, then leave ACK and cleanup to the public original command.
	snapshot, err = writer.Snapshot()
	if err != nil {
		t.Fatalf("refresh original Runtime before S9 Result submit: %v", err)
	}
	accept(ctx, snapshot, execution, checkpoint, candidateRef)
}

func s9MainProofFromVerifiedCheckpoint(t *testing.T, root string, execution workspace.Execution, checkpoint integration.Checkpoint) runtime.RepairResultMainProof {
	t.Helper()
	if checkpoint.State != integration.StateVerified {
		t.Fatalf("S9 Main proof requires an actually verified checkpoint, got %q", checkpoint.State)
	}
	path := integration.DefaultCheckpointStore().Path(root, execution.RuntimeID, execution.BaselineGeneration, execution.AssignmentID)
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		t.Fatalf("resolve original S9 checkpoint slot: path=%s err=%v", path, err)
	}
	checkpointBytes, err := workpacket.ReadFile(root, filepath.ToSlash(relative), 20<<20)
	if err != nil {
		t.Fatalf("read bounded original S9 verified checkpoint: %v", err)
	}
	var persisted integration.Checkpoint
	if err := json.Unmarshal(checkpointBytes, &persisted); err != nil || persisted.State != integration.StateVerified || persisted.AssignmentID != execution.AssignmentID || persisted.BaselineGeneration != execution.BaselineGeneration {
		t.Fatalf("original S9 checkpoint bytes do not bind the verified execution: checkpoint=%+v err=%v", persisted, err)
	}
	facts, err := runtime.ProjectRepairResultMainFacts(checkpointBytes)
	if err != nil {
		t.Fatalf("project immutable original S9 Main facts: %v", err)
	}
	return runtime.RepairResultMainProof{
		CheckpointRef:       filepath.ToSlash(relative),
		CheckpointSHA256:    workpacket.Digest(checkpointBytes),
		VerifiedFacts:       facts,
		VerifiedFactsSHA256: workpacket.Digest(facts),
	}
}

func assertS9FinalResultEventOccursOnce(t *testing.T, journalBytes []byte, receipt runtime.RepairResultReceipt, runtimeID string, generation int) {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(journalBytes), []byte("\n"))
	count := 0
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode original S9 Result event: %v", err)
		}
		if event["event_id"] != receipt.AcceptedEventID {
			continue
		}
		count++
		if event["event"] != "transition_committed" || event["outcome"] != "committed" || event["transition_id"] != "S9-RESULT-SUBMIT" || event["idempotency_key"] != receipt.AcceptedIdempotencyKey || event["runtime_id"] != runtimeID || event["baseline_generation"] != float64(generation) {
			t.Fatalf("original S9 Result event differs from its receipt: %#v", event)
		}
	}
	if count != 1 {
		t.Fatalf("original Runtime journal contains %d S9 final Result events for %s, want exactly one", count, receipt.ResultID)
	}
}

func assertS9RecoveryPairUnchanged(t *testing.T, fixture *intakeFixture, state, journal []byte, reason string) {
	t.Helper()
	if !bytes.Equal(state, mustRead(t, fixture.statePath)) || !bytes.Equal(journal, mustRead(t, fixture.journalPath)) {
		t.Fatalf("%s changed the original Runtime pair", reason)
	}
}
