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
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// TestS9RepairResultCandidateConcurrentReceiptAndSealedPendingRecovery uses
// the original registered S9 packet, Worker checkout, answer, and candidate
// produced by the accepted Hook fixture. It reconstructs the actual pre-CAS
// pair to exercise two original receivers racing from that same revision,
// then builds a sealed pending marker from the resulting actual CAS bytes.
// This is a recovery mechanism fixture, not a simulated hard process kill.
func TestS9RepairResultCandidateConcurrentReceiptAndSealedPendingRecovery(t *testing.T) {
	prepared := prepareAcceptedS9RepairResultCandidate(t)
	fixture, owner, packet := prepared.fixture, prepared.owner, prepared.packet
	if fixture == nil || packet.ID == "" || prepared.candidateReceipt.CandidateRef == "" {
		t.Fatalf("accepted S9 candidate fixture is incomplete: %+v", prepared)
	}
	if len(prepared.candidateBeforeState) == 0 || len(prepared.candidateBeforeJournal) == 0 || len(prepared.acceptedState) == 0 || len(prepared.acceptedJournal) == 0 {
		t.Fatal("S9 candidate fixture omitted exact before/after Runtime pair bytes")
	}
	if !bytes.Equal(prepared.acceptedState, mustRead(t, fixture.statePath)) || !bytes.Equal(prepared.acceptedJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("accepted S9 fixture pair changed before candidate recovery setup")
	}

	// Both callers receive the same original packet, author file and registered
	// Worker root, and both use the exact pre-candidate revision. The immutable
	// candidate file is the byte-identical artifact already produced by the
	// original accepted receiver and is reused by CreateWorkspaceCandidate.
	if err := os.WriteFile(fixture.statePath, prepared.candidateBeforeState, 0o600); err != nil {
		t.Fatalf("restore exact original pre-candidate Runtime state for concurrency fixture: %v", err)
	}
	if err := os.WriteFile(fixture.journalPath, prepared.candidateBeforeJournal, 0o600); err != nil {
		t.Fatalf("restore exact original pre-candidate Runtime journal for concurrency fixture: %v", err)
	}
	concurrentBeforeState, concurrentBeforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	beforeSnapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil || beforeSnapshot.Revision != prepared.issueSnapshot.Revision {
		t.Fatalf("read original same-revision candidate race baseline: revision=%d issue=%d err=%v", beforeSnapshot.Revision, prepared.issueSnapshot.Revision, err)
	}

	request := s9CandidateAcceptRequest(prepared)
	type receiveResult struct {
		snapshot  runtime.Snapshot
		candidate repair.WorkspaceCandidate
		ref       repair.ArtifactRef
		err       error
	}
	start := make(chan struct{})
	results := make(chan receiveResult, 2)
	var callers sync.WaitGroup
	for i := 0; i < 2; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			snapshot, candidate, ref, receiveErr := repair.AcceptIssuedRepairResultCandidate(
				context.Background(), fixture.root, fixture.statePath, fixture.journalPath, request,
			)
			results <- receiveResult{snapshot: snapshot, candidate: candidate, ref: ref, err: receiveErr}
		}()
	}
	close(start)
	callers.Wait()
	close(results)
	concurrentResults := make([]receiveResult, 0, 2)
	for result := range results {
		concurrentResults = append(concurrentResults, result)
	}
	if len(concurrentResults) != 2 {
		t.Fatalf("same-packet concurrent candidate receivers returned %d results", len(concurrentResults))
	}
	successful := make([]receiveResult, 0, 2)
	busyRetries := 0
	for i, result := range concurrentResults {
		if result.err != nil {
			if errors.Is(result.err, integration.ErrIntegrationBusy) {
				busyRetries++
				continue
			}
			t.Fatalf("original concurrent candidate receiver %d failed outside the bounded workspace lock contract: %v", i, result.err)
		}
		if result.snapshot.Revision != prepared.issueSnapshot.Revision+1 || result.ref.Path != prepared.candidateReceipt.CandidateRef || result.ref.SHA256 != prepared.candidateReceipt.CandidateSHA256 || result.candidate.Provenance == nil || result.candidate.Provenance.PacketID != packet.ID {
			t.Fatalf("concurrent receiver %d did not return the one original accepted candidate: revision=%d ref=%+v candidate=%+v", i, result.snapshot.Revision, result.ref, result.candidate.Provenance)
		}
		successful = append(successful, result)
	}
	if len(successful) == 0 {
		t.Fatal("both original concurrent candidate receivers failed; want at least one committed or exact-replayed receipt")
	}
	if len(successful) == 2 && (successful[0].snapshot.Revision != successful[1].snapshot.Revision || successful[0].ref != successful[1].ref) {
		t.Fatalf("concurrent exact retry returned different candidate receipt identities: first=%+v second=%+v", successful[0].ref, successful[1].ref)
	}
	if busyRetries > 0 {
		// The original workspace lock may return its bounded busy sentinel to
		// one concurrent caller. After all callers finish, that same request
		// must replay the durable receipt without another Runtime write.
		beforeRetryState, beforeRetryJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
		for i := 0; i < busyRetries; i++ {
			retriedSnapshot, retriedCandidate, retriedRef, retryErr := repair.AcceptIssuedRepairResultCandidate(
				context.Background(), fixture.root, fixture.statePath, fixture.journalPath, request,
			)
			if retryErr != nil {
				t.Fatalf("retry exact S9 candidate request after bounded workspace-busy response: %v", retryErr)
			}
			if retriedSnapshot.Revision != successful[0].snapshot.Revision || retriedRef != successful[0].ref || retriedCandidate.Provenance == nil || retriedCandidate.Provenance.PacketID != packet.ID {
				t.Fatalf("bounded-busy exact retry changed the accepted candidate identity: snapshot=%d ref=%+v candidate=%+v", retriedSnapshot.Revision, retriedRef, retriedCandidate.Provenance)
			}
		}
		if !bytes.Equal(beforeRetryState, mustRead(t, fixture.statePath)) || !bytes.Equal(beforeRetryJournal, mustRead(t, fixture.journalPath)) {
			t.Fatal("exact retries after bounded workspace-busy responses changed the accepted Runtime pair")
		}
	}
	concurrentAcceptedState, concurrentAcceptedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if bytes.Equal(concurrentBeforeState, concurrentAcceptedState) || bytes.Equal(concurrentBeforeJournal, concurrentAcceptedJournal) {
		t.Fatal("concurrent candidate receiver pair did not contain one original Runtime commit")
	}
	concurrentReceipt, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, successful[0].snapshot.State, packet.ID, owner.agentID)
	if err != nil || !found || concurrentReceipt.CandidateRef != successful[0].ref.Path || concurrentReceipt.CandidateSHA256 != successful[0].ref.SHA256 {
		t.Fatalf("read exact candidate receipt after same-packet concurrent receive: found=%t receipt=%+v err=%v", found, concurrentReceipt, err)
	}
	assertS9CandidateAcceptedEventOnce(t, concurrentAcceptedJournal, concurrentReceipt, packet)
	assertS9CandidateWorkspacePointer(t, successful[0].snapshot.State, prepared.execution, concurrentReceipt)

	// The pending marker is assembled from actual before/after pairs emitted by
	// the original receiver. Its retained-transition setting matches the
	// original WORKSPACE Mutation's RetainLastTransition=true.
	marker := buildS8SealedCommitMarker(t, concurrentBeforeState, concurrentBeforeJournal, concurrentAcceptedState, concurrentAcceptedJournal)
	marker["retain_last_transition"] = true
	if err := os.WriteFile(fixture.statePath, concurrentBeforeState, 0o600); err != nil {
		t.Fatalf("restore exact pre-candidate pair before pending recovery fixture: %v", err)
	}
	if err := os.WriteFile(fixture.journalPath, concurrentBeforeJournal, 0o600); err != nil {
		t.Fatalf("restore exact pre-candidate journal before pending recovery fixture: %v", err)
	}
	markerPath := fixture.statePath + ".commit-pending.json"
	writeInvestigationJSON(t, markerPath, marker)
	markerBytes := mustRead(t, markerPath)
	if readOnly, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot(); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
		t.Fatalf("read-only Runtime Store accepted/repaired pending S9 candidate pair: snapshot=%+v err=%v", readOnly, err)
	}
	assertS9RecoveryPairUnchanged(t, fixture, concurrentBeforeState, concurrentBeforeJournal, "read-only pending S9 candidate observation")
	if !bytes.Equal(markerBytes, mustRead(t, markerPath)) {
		t.Fatal("read-only Runtime observation changed the sealed S9 candidate marker")
	}

	cleanupStore := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	removed, err := cleanupStore.RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
		ExpectedRevision: beforeSnapshot.Revision,
		ArtifactPath:     concurrentReceipt.CandidateRef,
		ArtifactSHA256:   concurrentReceipt.CandidateSHA256,
	})
	if err != nil || removed {
		t.Fatalf("pending marker cleanup removed the exact marker-bound S9 candidate: removed=%t err=%v", removed, err)
	}
	candidatePath := filepath.Join(fixture.root, filepath.FromSlash(concurrentReceipt.CandidateRef))
	candidateBytes := mustRead(t, candidatePath)
	if workpacket.Digest(candidateBytes) != concurrentReceipt.CandidateSHA256 {
		t.Fatal("marker-bound S9 candidate bytes do not match the original receipt SHA")
	}
	if !bytes.Equal(markerBytes, mustRead(t, markerPath)) {
		t.Fatal("pending artifact cleanup changed the sealed S9 candidate marker")
	}

	// Invoke the original receiver as the mutation-capable recovery entrypoint.
	// It must install the exact marker pair and return the accepted receipt
	// without appending another WORKSPACE event.
	recoveredSnapshot, recoveredCandidate, recoveredRef, err := repair.AcceptIssuedRepairResultCandidate(
		context.Background(), fixture.root, fixture.statePath, fixture.journalPath, request,
	)
	if err != nil {
		t.Fatalf("original S9 candidate receiver did not recover and replay the sealed pair: %v", err)
	}
	if recoveredSnapshot.Revision != successful[0].snapshot.Revision || recoveredRef != successful[0].ref || recoveredCandidate.Provenance == nil || recoveredCandidate.Provenance.PacketID != packet.ID {
		t.Fatalf("sealed candidate recovery returned a different accepted identity: snapshot=%d ref=%+v candidate=%+v", recoveredSnapshot.Revision, recoveredRef, recoveredCandidate.Provenance)
	}
	if !bytes.Equal(concurrentAcceptedState, mustRead(t, fixture.statePath)) || !bytes.Equal(concurrentAcceptedJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("sealed candidate recovery changed the exact accepted Runtime pair or appended another event")
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered S9 candidate marker remains: %v", err)
	}
	recoveredReceipt, found, err := runtime.VerifiedRepairResultCandidateReceipt(fixture.root, recoveredSnapshot.State, packet.ID, owner.agentID)
	if err != nil || !found || !reflect.DeepEqual(recoveredReceipt, concurrentReceipt) {
		t.Fatalf("original candidate receipt changed after sealed recovery: found=%t receipt=%+v err=%v", found, recoveredReceipt, err)
	}
	assertS9CandidateAcceptedEventOnce(t, mustRead(t, fixture.journalPath), recoveredReceipt, packet)
	assertS9CandidateWorkspacePointer(t, recoveredSnapshot.State, prepared.execution, recoveredReceipt)
	if !bytes.Equal(candidateBytes, mustRead(t, candidatePath)) {
		t.Fatal("sealed candidate recovery altered the original immutable candidate bytes")
	}
	if code := runS9HookEvent(t, fixture.root, "SubagentStop", owner.agentID, prepared.workerRoot, prepared.execution.PlatformSessionID); code != 0 {
		t.Fatal("original S9 Worker Stop remained blocked after the recovered exact candidate receipt")
	}
}

// s9CandidateAcceptRequest supplies a fixture-local trusted-host callback.
// It verifies the frozen Worker owner/path/session tuple from the original
// Runtime registry; it is not a live Claude host identity assertion.
func s9CandidateAcceptRequest(prepared s9AcceptedRepairResultFixture) repair.AcceptIssuedRepairResultCandidateRequest {
	return repair.AcceptIssuedRepairResultCandidateRequest{
		ExpectedRevision: prepared.issueSnapshot.Revision,
		PacketID:         prepared.packet.ID,
		AnswerPath:       prepared.answerPath,
		OwnerAgentID:     prepared.owner.agentID,
		SourceRoot:       prepared.workerRoot,
		OccurredAt:       time.Now().UTC(),
		AuthorizeSource: func(root string, state map[string]any) error {
			if root != prepared.fixture.root || state["runtime_id"] != prepared.packet.Binding.Sources.RuntimeID {
				return fmt.Errorf("candidate fixture source authority differs from the registered Runtime")
			}
			registry, err := workspace.Decode(state)
			if err != nil {
				return fmt.Errorf("decode candidate fixture's registered Worker source: %w", err)
			}
			if registry == nil {
				return fmt.Errorf("candidate fixture's registered Worker source is missing")
			}
			current, ok := registry.Execution(prepared.execution.AssignmentID, prepared.execution.RuntimeID, prepared.execution.BaselineGeneration)
			if !ok || current.AgentID != prepared.owner.agentID || current.Path != prepared.workerRoot || current.PlatformSessionID != prepared.execution.PlatformSessionID {
				return fmt.Errorf("candidate fixture source authority differs from the exact registered Worker owner/path/session")
			}
			return nil
		},
	}
}

func assertS9CandidateAcceptedEventOnce(t *testing.T, journal []byte, receipt runtime.RepairResultCandidateReceipt, packet workpacket.RepairResultPacket) {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(journal), []byte("\n"))
	count := 0
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode original S9 candidate journal event: %v", err)
		}
		if event["idempotency_key"] != receipt.AcceptedIdempotencyKey {
			continue
		}
		count++
		if event["event_id"] != receipt.AcceptedEventID || event["transition_id"] != "WORKSPACE" || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["runtime_id"] != packet.Binding.Sources.RuntimeID || event["baseline_generation"] != float64(packet.Binding.Sources.BaselineGeneration) {
			t.Fatalf("original candidate acceptance event differs from its durable receipt: %#v", event)
		}
	}
	if count != 1 || receipt.AcceptedEventID != "evt-s9-issued-repair-candidate-"+packet.ID || receipt.AcceptedIdempotencyKey != "runtime:s9:issued-repair-candidate:"+packet.ID {
		t.Fatalf("Runtime has %d original WORKSPACE candidate events for packet %s, receipt=%+v", count, packet.ID, receipt)
	}
}

func assertS9CandidateWorkspacePointer(t *testing.T, state map[string]any, execution workspace.Execution, receipt runtime.RepairResultCandidateReceipt) {
	t.Helper()
	registry, err := workspace.Decode(state)
	if err != nil || registry == nil {
		t.Fatalf("decode original S9 Workspace candidate pointer: %v", err)
	}
	entry, found := registry.Executions[execution.AssignmentID]
	if !found || entry.AssignmentID != execution.AssignmentID || entry.DeliveryRef != receipt.CandidateRef || entry.DeliverySHA256 != receipt.CandidateSHA256 {
		t.Fatalf("original S9 Workspace delivery pointer differs from accepted candidate receipt: found=%t entry=%+v receipt=%+v", found, entry, receipt)
	}
}
