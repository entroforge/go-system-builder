package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type s9FinalSubmitInput struct {
	snapshot     runtime.Snapshot
	execution    workspace.Execution
	checkpoint   integration.Checkpoint
	proof        runtime.RepairResultMainProof
	candidateRef repair.ArtifactRef
}

type s9FinalSubmitOutcome struct {
	snapshot runtime.Snapshot
	result   repair.RepairResult
	ref      repair.ArtifactRef
	err      error
}

func TestS9RepairResultFinalConcurrentSubmitExactRetry(t *testing.T) {
	prepared := prepareAcceptedS9RepairResultCandidate(t)
	input := captureS9FinalSubmitInput(t, prepared)
	if input.snapshot.Revision != prepared.acceptedSnapshot.Revision {
		t.Fatalf("Runtime advanced after candidate acceptance: current=%d accepted=%d", input.snapshot.Revision, prepared.acceptedSnapshot.Revision)
	}
	beforeState := mustRead(t, prepared.fixture.statePath)
	beforeJournal := mustRead(t, prepared.fixture.journalPath)
	request := repair.SubmitIssuedRepairResultRequest{
		ExpectedRevision: input.snapshot.Revision,
		PacketID:         prepared.packet.ID,
		OwnerAgentID:     prepared.owner.agentID,
		MainProof:        input.proof,
		OccurredAt:       time.Now().UTC(),
	}

	start := make(chan struct{})
	outcomes := make(chan s9FinalSubmitOutcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			snapshot, result, ref, err := repair.SubmitIssuedRepairResult(
				context.Background(), prepared.fixture.root, prepared.fixture.statePath, prepared.fixture.journalPath, request,
			)
			outcomes <- s9FinalSubmitOutcome{snapshot: snapshot, result: result, ref: ref, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)

	var accepted *s9FinalSubmitOutcome
	var retryRequests []repair.SubmitIssuedRepairResultRequest
	for outcome := range outcomes {
		if outcome.err == nil {
			if accepted == nil {
				copy := outcome
				accepted = &copy
			} else if outcome.ref != accepted.ref || !reflect.DeepEqual(outcome.result, accepted.result) {
				t.Fatalf("concurrent exact S9 Result calls returned different accepted artifacts: first=%+v second=%+v", *accepted, outcome)
			}
			continue
		}
		if !errors.Is(outcome.err, integration.ErrIntegrationBusy) && !errors.Is(outcome.err, runtime.ErrStaleRevision) {
			t.Fatalf("concurrent original S9 Result submit returned an unexpected error: %v", outcome.err)
		}
		retryRequests = append(retryRequests, request)
	}
	if accepted == nil {
		t.Fatal("neither concurrent original S9 Result submit committed or replayed successfully")
	}
	if len(retryRequests) == 0 {
		// Even when both racing calls reached the idempotent accepted branch,
		// keep an explicit post-race exact replay assertion.
		retryRequests = append(retryRequests, request)
	}
	if accepted.snapshot.Revision != input.snapshot.Revision+1 {
		t.Fatalf("concurrent S9 Result submit revision=%d, want one CAS from %d", accepted.snapshot.Revision, input.snapshot.Revision)
	}

	acceptedState := mustRead(t, prepared.fixture.statePath)
	acceptedJournal := mustRead(t, prepared.fixture.journalPath)
	acceptedBytes := readS9FinalResultBytes(t, prepared.fixture.root, accepted.ref)
	if bytes.Equal(beforeState, acceptedState) || bytes.Equal(beforeJournal, acceptedJournal) {
		t.Fatal("winning original S9 Result did not commit both Runtime state and journal")
	}
	if accepted.result.Receipt == nil {
		t.Fatal("winning original S9 Result omitted its typed final receipt")
	}
	assertOneS9FinalSubmitEvent(t, acceptedJournal)
	assertS9FinalResultEventOccursOnce(t, acceptedJournal, *accepted.result.Receipt, prepared.packet.Binding.Sources.RuntimeID, prepared.packet.Binding.Sources.BaselineGeneration)

	finalSnapshot := currentS9Snapshot(t, prepared.fixture)
	finalReceipt, found, err := runtime.VerifiedRepairResultReceipt(prepared.fixture.root, finalSnapshot.State, prepared.packet.ID, prepared.owner.agentID)
	if err != nil || !found || finalReceipt.ResultRef != accepted.ref.Path || finalReceipt.ResultID != prepared.packet.ResultID || !finalReceipt.WorkComplete {
		t.Fatalf("verify unique original S9 final Result after concurrent submission: found=%t receipt=%+v err=%v", found, finalReceipt, err)
	}
	if finalReceipt.AcceptedAfterRuntimeRevision != finalSnapshot.Revision {
		t.Fatalf("S9 final receipt revision=%d, Runtime revision=%d", finalReceipt.AcceptedAfterRuntimeRevision, finalSnapshot.Revision)
	}
	if workpacket.Digest(acceptedBytes) != accepted.ref.SHA256 {
		t.Fatal("accepted S9 Result artifact does not match its original immutable ref")
	}

	for _, retry := range retryRequests {
		stateBeforeReplay := mustRead(t, prepared.fixture.statePath)
		journalBeforeReplay := mustRead(t, prepared.fixture.journalPath)
		artifactBeforeReplay := mustRead(t, filepath.Join(prepared.fixture.root, filepath.FromSlash(accepted.ref.Path)))
		snapshot, result, ref, err := repair.SubmitIssuedRepairResult(
			context.Background(), prepared.fixture.root, prepared.fixture.statePath, prepared.fixture.journalPath, retry,
		)
		if err != nil || ref != accepted.ref || !reflect.DeepEqual(result, accepted.result) {
			t.Fatalf("exact retry of concurrent original S9 Result: ref=%+v want=%+v resultEqual=%t err=%v", ref, accepted.ref, reflect.DeepEqual(result, accepted.result), err)
		}
		if snapshot.Revision != finalSnapshot.Revision {
			t.Fatalf("exact S9 Result retry changed Runtime revision from %d to %d", finalSnapshot.Revision, snapshot.Revision)
		}
		if !bytes.Equal(stateBeforeReplay, mustRead(t, prepared.fixture.statePath)) || !bytes.Equal(journalBeforeReplay, mustRead(t, prepared.fixture.journalPath)) || !bytes.Equal(artifactBeforeReplay, mustRead(t, filepath.Join(prepared.fixture.root, filepath.FromSlash(accepted.ref.Path)))) {
			t.Fatal("exact S9 Result retry changed Runtime state, journal, or immutable artifact bytes")
		}
	}
	if !bytes.Equal(acceptedState, mustRead(t, prepared.fixture.statePath)) || !bytes.Equal(acceptedJournal, mustRead(t, prepared.fixture.journalPath)) || !bytes.Equal(acceptedBytes, readS9FinalResultBytes(t, prepared.fixture.root, accepted.ref)) {
		t.Fatal("concurrent exact S9 Result retries changed accepted state, journal, or artifact bytes")
	}
}

func TestS9RepairResultStagingRebuildAfterOriginalSiblingWorkspaceCAS(t *testing.T) {
	prepared := prepareAcceptedS9RepairResultCandidate(t)
	input := captureS9FinalSubmitInput(t, prepared)
	if input.snapshot.Revision != prepared.acceptedSnapshot.Revision {
		t.Fatalf("Runtime advanced after candidate acceptance: current=%d accepted=%d", input.snapshot.Revision, prepared.acceptedSnapshot.Revision)
	}
	request := repair.SubmitIssuedRepairResultRequest{
		ExpectedRevision: input.snapshot.Revision,
		PacketID:         prepared.packet.ID,
		OwnerAgentID:     prepared.owner.agentID,
		MainProof:        input.proof,
		OccurredAt:       time.Now().UTC(),
	}
	beforeState, beforeJournal := mustRead(t, prepared.fixture.statePath), mustRead(t, prepared.fixture.journalPath)
	firstSnapshot, firstResult, firstRef, err := repair.SubmitIssuedRepairResult(
		context.Background(), prepared.fixture.root, prepared.fixture.statePath, prepared.fixture.journalPath, request,
	)
	if err != nil {
		t.Fatalf("first original S9 Result CAS for staging fixture: %v", err)
	}
	if firstResult.Receipt == nil {
		t.Fatal("first original S9 Result omitted its typed receipt")
	}
	firstJournal := mustRead(t, prepared.fixture.journalPath)
	firstBytes := readS9FinalResultBytes(t, prepared.fixture.root, firstRef)
	firstReceipt, found, err := runtime.VerifiedRepairResultReceipt(prepared.fixture.root, firstSnapshot.State, prepared.packet.ID, prepared.owner.agentID)
	if err != nil || !found || firstReceipt.ResultRef != firstRef.Path || firstReceipt.AcceptedAfterRuntimeRevision != firstSnapshot.Revision {
		t.Fatalf("verify initial original S9 Result before staging simulation: found=%t receipt=%+v err=%v", found, firstReceipt, err)
	}
	assertOneS9FinalSubmitEvent(t, firstJournal)

	// This creates an actual immutable Result artifact, then restores the exact
	// pre-CAS pair to model an unaccepted staged artifact. The later sibling
	// operation is still an original public Writer action.
	if err := os.WriteFile(prepared.fixture.statePath, beforeState, 0o600); err != nil {
		t.Fatalf("restore exact original pre-Result Runtime state: %v", err)
	}
	if err := os.WriteFile(prepared.fixture.journalPath, beforeJournal, 0o600); err != nil {
		t.Fatalf("restore exact original pre-Result Runtime journal: %v", err)
	}
	if !bytes.Equal(firstBytes, readS9FinalResultBytes(t, prepared.fixture.root, firstRef)) {
		t.Fatal("restoring the pre-Result pair removed or changed the unaccepted staged artifact")
	}
	if snapshot := currentS9Snapshot(t, prepared.fixture); snapshot.Revision != input.snapshot.Revision {
		t.Fatalf("restored pre-Result Runtime revision=%d, want %d", snapshot.Revision, input.snapshot.Revision)
	}

	siblingAssignmentID, siblingOwnerID := s9UnpreparedSiblingManifestAssignment(t, prepared)
	checkpointPath := integration.DefaultCheckpointStore().Path(
		prepared.fixture.root, input.execution.RuntimeID, input.execution.BaselineGeneration, input.execution.AssignmentID,
	)
	checkpointBefore := mustRead(t, checkpointPath)
	var checkpoint integration.Checkpoint
	if err := json.Unmarshal(checkpointBefore, &checkpoint); err != nil || checkpoint.State != integration.StateVerified || checkpoint.TestedHead == "" {
		t.Fatalf("read actual verified Main checkpoint before sibling prepare: checkpoint=%+v err=%v", checkpoint, err)
	}
	mainHeadBefore, err := workspace.Git(context.Background(), prepared.fixture.root, "rev-parse", checkpoint.TargetBranch)
	if err != nil || mainHeadBefore != checkpoint.TestedHead {
		t.Fatalf("Main target before sibling prepare=%q tested=%q err=%v", mainHeadBefore, checkpoint.TestedHead, err)
	}

	preparedBytes := runS9CommandOutput(t, prepared.fixture.root,
		"runtime", "workspace", "prepare", "--root", prepared.fixture.root,
		"--assignment", siblingAssignmentID, "--agent", siblingOwnerID, "--check-location", "main",
	)
	var siblingExecution workspace.Execution
	var response struct {
		Execution workspace.Execution `json:"execution"`
	}
	if err := json.Unmarshal(preparedBytes, &response); err != nil {
		t.Fatal(err)
	}
	siblingExecution = response.Execution
	if siblingExecution.AssignmentID != siblingAssignmentID || siblingExecution.AgentID != siblingOwnerID || siblingExecution.Status != "ready" || siblingExecution.CheckLocation != "main" || siblingExecution.BootstrapSHA256 == "" {
		t.Fatalf("original sibling workspace prepare did not publish its real ready execution: execution=%+v output=%s", siblingExecution, preparedBytes)
	}
	postPrepare := currentS9Snapshot(t, prepared.fixture)
	if postPrepare.Revision <= input.snapshot.Revision {
		t.Fatalf("legal original sibling prepare did not advance Runtime from %d (now %d)", input.snapshot.Revision, postPrepare.Revision)
	}
	registry, err := workspace.Decode(postPrepare.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original Workspace registry after sibling prepare: %v", err)
	}
	registeredSibling, found := registry.Execution(siblingAssignmentID, input.execution.RuntimeID, input.execution.BaselineGeneration)
	if !found || registeredSibling.AgentID != siblingOwnerID || registeredSibling.Status != "ready" || registeredSibling.BootstrapSHA256 != siblingExecution.BootstrapSHA256 {
		t.Fatalf("sibling prepare result is not retained in the original Runtime registry: found=%t execution=%+v", found, registeredSibling)
	}
	mainHeadAfter, err := workspace.Git(context.Background(), prepared.fixture.root, "rev-parse", checkpoint.TargetBranch)
	if err != nil || mainHeadAfter != mainHeadBefore || !bytes.Equal(checkpointBefore, mustRead(t, checkpointPath)) {
		t.Fatalf("sibling prepare changed the verified Main target/checkpoint: before=%q after=%q checkpointEqual=%t err=%v", mainHeadBefore, mainHeadAfter, bytes.Equal(checkpointBefore, mustRead(t, checkpointPath)), err)
	}
	stagedBeforeRetry := readS9FinalResultBytes(t, prepared.fixture.root, firstRef)
	if !bytes.Equal(stagedBeforeRetry, firstBytes) {
		t.Fatal("sibling prepare changed the unaccepted original Result staging artifact")
	}

	// The exact old request must remain stale and must not clean, rewrite, or
	// otherwise claim the orphan staging artifact.
	staleState, staleJournal := mustRead(t, prepared.fixture.statePath), mustRead(t, prepared.fixture.journalPath)
	_, _, _, err = repair.SubmitIssuedRepairResult(
		context.Background(), prepared.fixture.root, prepared.fixture.statePath, prepared.fixture.journalPath, request,
	)
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("old S9 final request after sibling CAS must fail its original expected revision, got %v", err)
	}
	if !bytes.Equal(staleState, mustRead(t, prepared.fixture.statePath)) || !bytes.Equal(staleJournal, mustRead(t, prepared.fixture.journalPath)) || !bytes.Equal(firstBytes, readS9FinalResultBytes(t, prepared.fixture.root, firstRef)) {
		t.Fatal("stale old S9 request changed Runtime pair or the original unaccepted staging bytes")
	}

	// Only the CAS coordinate is refreshed. The exact accepted packet, owner,
	// authored Result identity, source/candidate and Main proof remain unchanged.
	refreshedRequest := request
	refreshedRequest.ExpectedRevision = postPrepare.Revision
	if refreshedRequest.PacketID != request.PacketID || refreshedRequest.OwnerAgentID != request.OwnerAgentID || !reflect.DeepEqual(refreshedRequest.MainProof, request.MainProof) || refreshedRequest.OccurredAt != request.OccurredAt {
		t.Fatal("fresh retry altered immutable S9 request identity beyond the Runtime CAS coordinate")
	}
	finalSnapshot, finalResult, finalRef, err := repair.SubmitIssuedRepairResult(
		context.Background(), prepared.fixture.root, prepared.fixture.statePath, prepared.fixture.journalPath, refreshedRequest,
	)
	if err != nil {
		t.Fatalf("original S9 Result writer did not safely rebuild stale predicted receipt after sibling CAS: %v", err)
	}
	if finalResult.Receipt == nil {
		t.Fatal("rebuilt original S9 Result omitted its typed receipt")
	}
	if finalSnapshot.Revision != postPrepare.Revision+1 || finalRef.Path != firstRef.Path || finalRef.SHA256 == firstRef.SHA256 || finalResult.ResultID != firstResult.ResultID || finalResult.ResultID != prepared.packet.ResultID {
		t.Fatalf("rebuilt S9 Result identity/artifact/revision = rev %d ref %+v result=%s; prior ref=%+v result=%s", finalSnapshot.Revision, finalRef, finalResult.ResultID, firstRef, firstResult.ResultID)
	}
	firstResultWithoutReceipt, finalResultWithoutReceipt := firstResult, finalResult
	firstResultWithoutReceipt.Receipt = nil
	finalResultWithoutReceipt.Receipt = nil
	if !reflect.DeepEqual(firstResultWithoutReceipt, finalResultWithoutReceipt) {
		t.Fatalf("sibling-CAS retry changed accepted professional/source Result content outside predicted receipt coordinates: first=%+v final=%+v", firstResultWithoutReceipt, finalResultWithoutReceipt)
	}
	if !reflect.DeepEqual(firstResult.Receipt.Provenance, finalResult.Receipt.Provenance) || !reflect.DeepEqual(firstResult.Receipt.MainProof, finalResult.Receipt.MainProof) || finalResult.Receipt.AcceptedAfterRuntimeRevision != finalSnapshot.Revision {
		t.Fatalf("rebuilt S9 receipt changed accepted Worker/Main facts or retained wrong revision: first=%+v final=%+v", firstResult.Receipt, finalResult.Receipt)
	}
	finalBytes := readS9FinalResultBytes(t, prepared.fixture.root, finalRef)
	if workpacket.Digest(finalBytes) != finalRef.SHA256 || bytes.Equal(finalBytes, firstBytes) {
		t.Fatal("rebuilt final S9 Result bytes do not reflect the refreshed immutable receipt coordinates")
	}
	finalReceipt, found, err := runtime.VerifiedRepairResultReceipt(prepared.fixture.root, finalSnapshot.State, prepared.packet.ID, prepared.owner.agentID)
	if err != nil || !found || finalReceipt.ResultRef != finalRef.Path || finalReceipt.ResultID != prepared.packet.ResultID || !finalReceipt.WorkComplete || !reflect.DeepEqual(finalReceipt.Provenance, firstReceipt.Provenance) || !reflect.DeepEqual(finalReceipt.MainProof, firstReceipt.MainProof) {
		t.Fatalf("verify rebuilt original S9 receipt: found=%t receipt=%+v err=%v", found, finalReceipt, err)
	}
	assertOneS9FinalSubmitEvent(t, mustRead(t, prepared.fixture.journalPath))
	assertS9FinalResultEventOccursOnce(t, mustRead(t, prepared.fixture.journalPath), finalReceipt, prepared.packet.Binding.Sources.RuntimeID, prepared.packet.Binding.Sources.BaselineGeneration)
	if finalReceipt.AcceptedSequence <= firstReceipt.AcceptedSequence || finalReceipt.AcceptedAfterRuntimeRevision != finalSnapshot.Revision {
		t.Fatalf("rebuilt Result did not predict the post-sibling CAS journal coordinates: initial=%+v final=%+v snapshot=%d", firstReceipt, finalReceipt, finalSnapshot.Revision)
	}
	finalRegistry, err := workspace.Decode(finalSnapshot.State)
	if err != nil || finalRegistry == nil {
		t.Fatalf("decode Workspace registry after rebuilt final Result: %v", err)
	}
	retainedSibling, found := finalRegistry.Execution(siblingAssignmentID, input.execution.RuntimeID, input.execution.BaselineGeneration)
	if !found || retainedSibling.AgentID != siblingOwnerID || retainedSibling.Status != "ready" || retainedSibling.Path != registeredSibling.Path || retainedSibling.BootstrapSHA256 != registeredSibling.BootstrapSHA256 {
		t.Fatalf("final S9 Result CAS discarded or rewrote sibling execution: found=%t before=%+v after=%+v", found, registeredSibling, retainedSibling)
	}
}

func captureS9FinalSubmitInput(t *testing.T, prepared s9AcceptedRepairResultFixture) s9FinalSubmitInput {
	t.Helper()
	var captured s9FinalSubmitInput
	withS9VerifiedMainCheckpoint(t, prepared.fixture, prepared.owner, prepared.packet,
		func(_ context.Context, snapshot runtime.Snapshot, execution workspace.Execution, checkpoint integration.Checkpoint, candidateRef repair.ArtifactRef) {
			if snapshot.Revision != prepared.acceptedSnapshot.Revision {
				t.Fatalf("Main verification observed Runtime revision %d, want candidate acceptance revision %d", snapshot.Revision, prepared.acceptedSnapshot.Revision)
			}
			captured = s9FinalSubmitInput{
				snapshot: snapshot, execution: execution, checkpoint: checkpoint,
				proof: s9MainProofFromVerifiedCheckpoint(t, prepared.fixture.root, execution, checkpoint), candidateRef: candidateRef,
			}
		},
	)
	if captured.snapshot.Revision == 0 || captured.proof.CheckpointRef == "" || captured.candidateRef.Path == "" {
		t.Fatalf("original Main verification did not provide a complete final-submit input: %+v", captured)
	}
	return captured
}

func s9UnpreparedSiblingManifestAssignment(t *testing.T, prepared s9AcceptedRepairResultFixture) (manifestAssignmentID, ownerAgentID string) {
	t.Helper()
	state := currentS9State(t, prepared.fixture)
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	planPath, _ := pointer["plan_ref"].(string)
	planSHA, _ := pointer["plan_sha256"].(string)
	if planPath == "" || planSHA == "" {
		t.Fatalf("current original S9 RepairPlan pointer is incomplete: %#v", pointer)
	}
	plan, err := repair.ValidateRepairPlan(prepared.fixture.root, repair.ArtifactRef{Path: planPath, SHA256: planSHA})
	if err != nil {
		t.Fatalf("read original S9 RepairPlan for sibling: %v", err)
	}
	owners, _ := pointer["assignment_owners"].(map[string]any)
	var sibling *repair.RepairAssignment
	for i := range plan.Assignments {
		if plan.Assignments[i].AssignmentID == prepared.packet.Binding.Sources.RepairAssignmentID {
			continue
		}
		if sibling != nil {
			t.Fatalf("expected exactly one sibling Assignment, found more than one in original plan %s", plan.PlanID)
		}
		copy := plan.Assignments[i]
		sibling = &copy
	}
	if sibling == nil {
		t.Fatalf("original S9 plan %s has no sibling Assignment to prepare", plan.PlanID)
	}
	ownerAgentID, _ = owners[sibling.AssignmentID].(string)
	if ownerAgentID == "" || ownerAgentID == prepared.owner.agentID {
		t.Fatalf("original sibling Assignment %s has no distinct registered owner: %#v", sibling.AssignmentID, owners)
	}
	loaded, err := hookctx.LoadFull(prepared.fixture.root, ownerAgentID)
	if err != nil {
		t.Fatalf("resolve original sibling owner %s through hook context: %v", ownerAgentID, err)
	}
	if loaded.PolicyContext.Agent.RepairAssignmentID != sibling.AssignmentID {
		t.Fatalf("original sibling owner projects RepairAssignment %q, want %q", loaded.PolicyContext.Agent.RepairAssignmentID, sibling.AssignmentID)
	}
	var rows []hookctx.AssignmentContext
	for _, row := range loaded.Assignments {
		if row.OwnerAgentID == ownerAgentID && row.AssignmentID != "" && row.ManifestRef != "" {
			rows = append(rows, row)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("original sibling owner %s has %d registered manifest Assignment rows, want one exact prepare target", ownerAgentID, len(rows))
	}
	return rows[0].AssignmentID, ownerAgentID
}

func readS9FinalResultBytes(t *testing.T, root string, ref repair.ArtifactRef) []byte {
	t.Helper()
	if ref.Path == "" || ref.SHA256 == "" || !filepath.IsLocal(filepath.FromSlash(ref.Path)) {
		t.Fatalf("invalid original S9 Result artifact reference: %+v", ref)
	}
	data := mustRead(t, filepath.Join(root, filepath.FromSlash(ref.Path)))
	if workpacket.Digest(data) != ref.SHA256 {
		t.Fatalf("original S9 Result artifact %s SHA differs from its immutable reference", ref.Path)
	}
	return data
}

func assertOneS9FinalSubmitEvent(t *testing.T, journalBytes []byte) {
	t.Helper()
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(journalBytes), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode original Runtime event while counting S9 final submits: %v", err)
		}
		if event["transition_id"] == "S9-RESULT-SUBMIT" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("original Runtime journal has %d S9-RESULT-SUBMIT events, want exactly one", count)
	}
}
