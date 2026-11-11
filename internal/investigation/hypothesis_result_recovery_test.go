package investigation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS8ConcurrentOriginalCaseSubmitIsExactlyOnce(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	runS8AgentBegin(t, fixture, owner)
	snapshot := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, snapshot.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue exact S8 packet before concurrent submit: packets=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, fixture.root, packet, "Concurrent original writers observe one exact authored answer.")
	before := currentS8Snapshot(t, fixture)
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)

	type outcome struct {
		snapshot runtime.Snapshot
		err      error
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	outcomes := make([]outcome, 2)
	for i := range outcomes {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			outcomes[index].snapshot, outcomes[index].err = submitS8Original(fixture, packet, answerPath, owner.ownerID)
		}(i)
	}
	close(start)
	workers.Wait()

	directlyAccepted := 0
	for i, result := range outcomes {
		if result.err == nil {
			directlyAccepted++
			t.Logf("concurrent exact S8 submit %d accepted at Runtime revision %d", i, result.snapshot.Revision)
			continue
		}
		// A concurrent writer may see the original CAS advance or may lose
		// the shared immutable Case filename race. Preserve that diagnostic;
		// exact retry below must recover the same original receipt.
		t.Logf("concurrent exact S8 submit %d returned a retryable race outcome: %v", i, result.err)
	}
	accepted := currentS8Snapshot(t, fixture)
	if accepted.Revision != before.Revision+1 {
		t.Fatalf("concurrent packet submissions advanced Runtime to %d, want exactly one revision after %d", accepted.Revision, before.Revision)
	}
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found {
		t.Fatalf("verify one concurrent original Case receipt: found=%v err=%v", found, err)
	}
	assertS8SingleAcceptedResult(t, fixture, receipt, packet)

	// Reconcile both contenders through the original consumer. Exact replay is
	// a source-backed receipt lookup and must not create a second Case revision.
	stateBeforeRetry, journalBeforeRetry := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replayed, err := submitS8Original(fixture, packet, answerPath, owner.ownerID)
	if err != nil {
		t.Fatalf("exact S8 retry after concurrent submit: direct accepted=%d err=%v: %v", directlyAccepted, outcomes, err)
	}
	if replayed.Revision != accepted.Revision || !bytes.Equal(stateBeforeRetry, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeRetry, mustRead(t, fixture.journalPath)) {
		t.Fatal("exact concurrent S8 retry changed the committed Runtime pair")
	}
	assertS8SingleAcceptedResult(t, fixture, receipt, packet)
	if bytes.Equal(beforeState, stateBeforeRetry) || bytes.Equal(beforeJournal, journalBeforeRetry) {
		t.Fatal("concurrent S8 writers returned receipt success without an original Case Runtime commit")
	}
}

func TestS8OriginalCaseReceiptRecoversSealedPendingCommit(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	runS8AgentBegin(t, fixture, owner)
	initial := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, initial.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue exact S8 packet before recovery fixture: packets=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, fixture.root, packet, "The durable Case receipt survives a sealed commit recovery.")
	before := currentS8Snapshot(t, fixture)
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	accepted, err := submitS8Original(fixture, packet, answerPath, owner.ownerID)
	if err != nil {
		t.Fatalf("accept actual S8 answer before pending recovery fixture: %v", err)
	}
	acceptedReceipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found {
		t.Fatalf("verify accepted S8 Case receipt before simulating interruption: found=%v err=%v", found, err)
	}
	casePath := filepath.Join(fixture.root, filepath.FromSlash(acceptedReceipt.SuccessorCaseRef))
	caseBytes := mustRead(t, casePath)
	afterState, afterJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	marker := buildS8SealedCommitMarker(t, beforeState, beforeJournal, afterState, afterJournal)

	// Simulate the exact recoverable crash point: the Case file, final candidate
	// state/event and their original marker are durable, while the active pair
	// has been restored to the pre-commit generation.
	if err := os.WriteFile(fixture.statePath, beforeState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.journalPath, beforeJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	markerPath := fixture.statePath + ".commit-pending.json"
	writeInvestigationJSON(t, markerPath, marker)
	markerBytes := mustRead(t, markerPath)

	writer := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	removed, err := writer.RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
		ExpectedRevision: before.Revision, ArtifactPath: acceptedReceipt.SuccessorCaseRef, ArtifactBytes: caseBytes,
	})
	if err != nil || removed {
		t.Fatalf("pending Runtime commit cleanup removed a possibly reachable S8 Case artifact: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(casePath); err != nil {
		t.Fatalf("pending commit failed to preserve the sealed S8 Case artifact: %v", err)
	}
	if !bytes.Equal(caseBytes, mustRead(t, casePath)) {
		t.Fatal("pending cleanup changed the sealed S8 Case bytes")
	}
	if !bytes.Equal(markerBytes, mustRead(t, markerPath)) {
		t.Fatal("pending artifact cleanup changed the sealed Runtime commit marker")
	}

	// The original S8 consumer is a mutation-capable recovery entry point. It
	// repairs only the marker-bound state/journal pair, then returns the exact
	// accepted receipt without writing another Case or journal event.
	recovered, err := submitS8Original(fixture, packet, answerPath, owner.ownerID)
	if err != nil {
		t.Fatalf("recover sealed S8 Case commit and exact-replay answer: %v", err)
	}
	if recovered.Revision != accepted.Revision || !bytes.Equal(afterState, mustRead(t, fixture.statePath)) || !bytes.Equal(afterJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("S8 pending recovery/replay differs from the exact sealed original state/journal pair")
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered S8 commit marker remains: %v", err)
	}
	if !bytes.Equal(caseBytes, mustRead(t, casePath)) {
		t.Fatal("S8 pending recovery altered the original immutable Case receipt")
	}
	recoveredReceipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, recovered.State, packet.ID, owner.ownerID)
	if err != nil || !found || recoveredReceipt.AcceptedEventID != acceptedReceipt.AcceptedEventID {
		t.Fatalf("original S8 receipt was not queryable after pending recovery: found=%v receipt=%+v err=%v", found, recoveredReceipt, err)
	}
	assertS8SingleAcceptedResult(t, fixture, recoveredReceipt, packet)

	// Receipt reconstruction is based on the original Case/journal proof, not
	// the author's source file. A save replay still requires those exact bytes.
	if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(answerPath))); err != nil {
		t.Fatalf("remove author file after acceptance: %v", err)
	}
	if _, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, recovered.State, packet.ID, owner.ownerID); err != nil || !found {
		t.Fatalf("read-only original S8 receipt depended on the author's answer file: found=%v err=%v", found, err)
	}
	stateBeforeMissingAnswer, journalBeforeMissingAnswer := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if replay, err := submitS8Original(fixture, packet, answerPath, owner.ownerID); err != nil || replay.Revision != recovered.Revision {
		t.Fatalf("accepted S8 receipt could not replay without the private answer: revision=%d error=%v", replay.Revision, err)
	}
	if !bytes.Equal(stateBeforeMissingAnswer, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeMissingAnswer, mustRead(t, fixture.journalPath)) {
		t.Fatal("missing-author-file save changed the recovered Runtime pair")
	}
}

// G18 §1/§1.2/§1.3 reverse assertion: the three InvestigationCase writes of
// one Case — the ingest, a Case revision and the accepted hypothesis result —
// must own three different event identities, and the identity must be built
// from the Case's own domain revision instead of the Runtime write count.
// The Runtime revision is far ahead of the Case revision in this fixture, so
// an identity derived from the Runtime write count could not satisfy these
// assertions.
func TestS8CaseEventIdentitiesSeparateIngestRevisionAndAcceptance(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	runS8AgentBegin(t, fixture, owner)
	initial := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, initial.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue exact S8 packet for the identity check: packets=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, fixture.root, packet, "The accepted answer names one original responsibility.")
	accepted, err := submitS8Original(fixture, packet, answerPath, owner.ownerID)
	if err != nil {
		t.Fatalf("accept the original S8 answer: %v", err)
	}
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found {
		t.Fatalf("verify the accepted S8 Case receipt: found=%v err=%v", found, err)
	}

	caseID := packet.Binding.CaseID
	pointer, _ := accepted.State["review"].(map[string]any)["investigation"].(map[string]any)
	caseRevision := s8TestInt(pointer["revision"])
	if caseRevision < 2 {
		t.Fatalf("Case domain revision = %d, want the revision above the ingest revision", caseRevision)
	}
	if accepted.Revision == caseRevision {
		t.Fatalf("fixture does not separate the Runtime write count %d from the Case domain revision %d", accepted.Revision, caseRevision)
	}

	ingestID, acceptanceID, acceptanceKey := "", "", ""
	revisionIDs := map[string]bool{}
	for _, event := range s8CommittedEvents(t, fixture) {
		eventID, _ := event["event_id"].(string)
		switch {
		case strings.HasPrefix(eventID, "evt-investigation-case-ingested-"):
			if ingestID != "" {
				t.Fatalf("one Case produced two ingest identities: %q and %q", ingestID, eventID)
			}
			ingestID = eventID
		case strings.HasPrefix(eventID, "evt-investigation-case-revision-"):
			revisionIDs[eventID] = true
			if eventID == receipt.AcceptedEventID {
				acceptanceID = eventID
				acceptanceKey, _ = event["idempotency_key"].(string)
			}
		}
	}
	if ingestID != "evt-investigation-case-ingested-"+caseID {
		t.Fatalf("ingest identity = %q, want the ingest identity of Case %s", ingestID, caseID)
	}
	// The receipt identity is the identity of the one committed journal event
	// that accepted this result.
	if acceptanceID == "" || acceptanceID != receipt.AcceptedEventID {
		t.Fatalf("accepted receipt identity %q is not the identity of a committed Case revision event", receipt.AcceptedEventID)
	}
	if want := fmt.Sprintf("evt-investigation-case-revision-%s-r%d", caseID, caseRevision); acceptanceID != want {
		t.Fatalf("acceptance identity = %q, want the Case domain revision identity %q", acceptanceID, want)
	}
	if want := fmt.Sprintf("runtime:investigation-case-revision:%s:%d", caseID, caseRevision); acceptanceKey != want {
		t.Fatalf("acceptance idempotency key = %q, want %q", acceptanceKey, want)
	}
	if revisionIDs[ingestID] {
		t.Fatalf("the ingest and the accepted result share one event identity: %q", ingestID)
	}
	if len(revisionIDs) < 2 {
		t.Fatalf("Case revision identities = %v, want the hypothesis registration and the accepted result", revisionIDs)
	}
}

// submitS8Original states the expectation as "whatever the current value is"
// (G18 §3.2: the call point no longer asserts a Runtime write count) and lets
// the Writer resolve the current value.
func submitS8Original(fixture *intakeFixture, packet workpacket.HypothesisResultPacket, answerPath, owner string) (runtime.Snapshot, error) {
	return investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.IssuedHypothesisResultRequest{
		ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: answerPath, OwnerAgentID: owner, SourceRoot: fixture.root,
		AuthorizeSource: func(root string, _ map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("unexpected S8 source root %q", root)
			}
			return nil
		},
	})
}

func currentS8Snapshot(t *testing.T, fixture *intakeFixture) runtime.Snapshot {
	t.Helper()
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertS8SingleAcceptedResult(t *testing.T, fixture *intakeFixture, receipt investigation.HypothesisResultReceipt, packet workpacket.HypothesisResultPacket) {
	t.Helper()
	caseDoc := readCaseDocument(t, fixture.root, receipt.SuccessorCaseRef)
	matched := 0
	for _, raw := range caseDoc["hypothesis_results"].([]any) {
		row, _ := raw.(map[string]any)
		resultReceipt, _ := row["receipt"].(map[string]any)
		if resultReceipt["packet_id"] == receipt.PacketID && row["assignment_id"] == receipt.AssignmentID && row["hypothesis_id"] == packet.Binding.HypothesisID {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("original Case contains %d result rows for S8 packet %s, want exactly one", matched, receipt.PacketID)
	}
	events := s8CommittedEvents(t, fixture)
	committed := 0
	for _, event := range events {
		if event["event_id"] == receipt.AcceptedEventID {
			committed++
		}
	}
	if committed != 1 {
		t.Fatalf("Runtime journal contains %d original Case commit events for packet %s, want exactly one", committed, receipt.PacketID)
	}
}

func buildS8SealedCommitMarker(t *testing.T, beforeState, beforeJournal, afterState, afterJournal []byte) map[string]any {
	t.Helper()
	before, beforeCanonical := decodeCanonicalS8State(t, beforeState)
	after, afterCanonical := decodeCanonicalS8State(t, afterState)
	if !bytes.Equal(beforeCanonical, beforeState) || !bytes.Equal(afterCanonical, afterState) {
		t.Fatal("sealed S8 commit fixture requires the exact original Runtime state serializer")
	}
	lines := bytes.Split(bytes.TrimSpace(afterJournal), []byte("\n"))
	if len(lines) == 0 || len(lines[len(lines)-1]) == 0 {
		t.Fatal("sealed S8 commit has no final journal event")
	}
	var event map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &event); err != nil {
		t.Fatalf("decode original S8 committed event: %v", err)
	}
	eventBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	eventLine := append(eventBytes, '\n')
	actualEventLine := append(append([]byte(nil), lines[len(lines)-1]...), '\n')
	if !bytes.Equal(eventLine, actualEventLine) {
		t.Fatal("original S8 journal event does not match Runtime's sealed event serializer")
	}
	requestID, _ := event["request_id"].(string)
	idempotencyKey, _ := event["idempotency_key"].(string)
	if requestID == "" || idempotencyKey == "" {
		t.Fatal("original S8 commit event lacks its request/idempotency identity")
	}
	return map[string]any{
		"schema_version":         "1.0.0",
		"previous_revision":      s8TestInt(before["revision"]),
		"request_id":             requestID,
		"idempotency_key":        idempotencyKey,
		"retain_last_transition": false,
		"state":                  after,
		"journal_event":          event,
	}
}

func decodeCanonicalS8State(t *testing.T, data []byte) (map[string]any, []byte) {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode original S8 Runtime state: %v", err)
	}
	canonical, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return state, append(canonical, '\n')
}
