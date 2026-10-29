package investigation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9RepairPlanReportOriginalWriterConcurrentSamePacketAndReplay(t *testing.T) {
	fixture, _, owners := prepareS9RepairPlanReportHookFixture(t)
	owner := owners[0]
	packet := issueS9RecoveryTestPacket(t, fixture, owner.agentID)
	answerPath := writeS9RepairPlanReportAnswer(t, fixture.root, packet, "One exact pre-execution report wins the concurrent original Writer race.")
	before := currentS9Snapshot(t, fixture)
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
			outcomes[index].snapshot, _, outcomes[index].err = submitS9RecoveryTestPacket(fixture, before.Revision, packet, answerPath, owner.agentID)
		}(i)
	}
	close(start)
	workers.Wait()
	for i, result := range outcomes {
		if result.err != nil {
			t.Logf("concurrent S9 submit %d returned a race diagnostic; exact retry must reconcile it: %v", i, result.err)
		}
	}

	accepted := currentS9Snapshot(t, fixture)
	if accepted.Revision != before.Revision+1 {
		t.Fatalf("concurrent S9 submissions advanced Runtime to revision %d, want exactly one commit after %d", accepted.Revision, before.Revision)
	}
	receipt, found, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, accepted.State, packet.ID, owner.agentID)
	if err != nil || !found {
		t.Fatalf("verify original accepted S9 report receipt: found=%v err=%v", found, err)
	}
	assertS9SingleOriginalReportCommit(t, fixture, accepted.State, packet, receipt)
	if bytes.Equal(beforeState, mustRead(t, fixture.statePath)) || bytes.Equal(beforeJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("concurrent S9 submits returned without an original Runtime commit")
	}

	// A same-answer retry with a stale expected revision resolves to the exact
	// original report and must not append a second PTR-BUG-10 event or pointer.
	committedState, committedJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	replayed, report, err := submitS9RecoveryTestPacket(fixture, before.Revision, packet, answerPath, owner.agentID)
	if err != nil {
		t.Fatalf("exact S9 retry after the concurrent original Writer: %v", err)
	}
	if replayed.Revision != accepted.Revision || report.ReportID != packet.ReportID {
		t.Fatalf("S9 replay returned revision/report = %d/%q, want %d/%q", replayed.Revision, report.ReportID, accepted.Revision, packet.ReportID)
	}
	if !bytes.Equal(committedState, mustRead(t, fixture.statePath)) || !bytes.Equal(committedJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("exact S9 replay changed the committed Runtime state or journal")
	}
	assertS9SingleOriginalReportCommit(t, fixture, replayed.State, packet, receipt)
}

func TestS9RepairPlanReportSealedPendingRecoveryAndReadOnlyReceipt(t *testing.T) {
	fixture, _, owners := prepareS9RepairPlanReportHookFixture(t)
	owner := owners[0]
	packet := issueS9RecoveryTestPacket(t, fixture, owner.agentID)
	answerPath := writeS9RepairPlanReportAnswer(t, fixture.root, packet, "The exact original PlanReport and PTR-BUG-10 pair survives pending recovery.")
	before := currentS9Snapshot(t, fixture)
	beforeState, beforeJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	accepted, _, err := submitS9RecoveryTestPacket(fixture, before.Revision, packet, answerPath, owner.agentID)
	if err != nil {
		t.Fatalf("accept the actual original S9 PlanReport before constructing its recovery fixture: %v", err)
	}
	acceptedReceipt, found, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, accepted.State, packet.ID, owner.agentID)
	if err != nil || !found {
		t.Fatalf("verify actual accepted S9 receipt before pending fixture: found=%v err=%v", found, err)
	}
	reportPath := filepath.Join(fixture.root, filepath.FromSlash(acceptedReceipt.ReportRef))
	reportBytes := mustRead(t, reportPath)
	afterState, afterJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	marker := buildS8SealedCommitMarker(t, beforeState, beforeJournal, afterState, afterJournal)

	// This is a mechanism fixture assembled from the actual accepted Runtime
	// pair and its final journal event. It models the durable marker boundary;
	// it does not kill a live Writer process.
	if err := os.WriteFile(fixture.statePath, beforeState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.journalPath, beforeJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	markerPath := fixture.statePath + ".commit-pending.json"
	writeInvestigationJSON(t, markerPath, marker)
	markerBytes := mustRead(t, markerPath)

	// The report is not referenced by the restored pre-commit pair. The
	// Runtime's original cleanup guard must still preserve it while the sealed
	// marker can publish a candidate that references that exact SHA.
	cleanupStore := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	removed, err := cleanupStore.RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
		ExpectedRevision: before.Revision, ArtifactPath: acceptedReceipt.ReportRef, ArtifactSHA256: workpacket.Digest(reportBytes),
	})
	if err != nil || removed {
		t.Fatalf("pending original S9 commit cleanup removed report=%v err=%v", removed, err)
	}
	if !bytes.Equal(reportBytes, mustRead(t, reportPath)) || !bytes.Equal(markerBytes, mustRead(t, markerPath)) {
		t.Fatal("pending cleanup changed the marker-bound S9 report or commit marker")
	}

	// RecoverPendingOperations installs the marker's exact original pair. The
	// helper validates the actual event/state hashes but does not simulate a
	// hard process termination.
	writer := runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	recovered, err := writer.RecoverPendingOperations()
	if err != nil || !recovered {
		t.Fatalf("recover actual sealed S9 original pair: recovered=%v err=%v", recovered, err)
	}
	if !bytes.Equal(afterState, mustRead(t, fixture.statePath)) || !bytes.Equal(afterJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("S9 recovery changed the original accepted Runtime pair or appended another event")
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered S9 commit marker remains: %v", err)
	}
	recoveredReceipt, found, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, currentS9State(t, fixture), packet.ID, owner.agentID)
	if err != nil || !found || !reflect.DeepEqual(recoveredReceipt, acceptedReceipt) {
		t.Fatalf("recovered exact S9 receipt: found=%v receipt=%+v err=%v", found, recoveredReceipt, err)
	}
	assertS9SingleOriginalReportCommit(t, fixture, currentS9State(t, fixture), packet, recoveredReceipt)

	// The read-only receipt is backed by the retained report/journal closure and
	// remains queryable after the authoring file disappears. A save replay
	// still requires the exact current author file and must leave Runtime alone.
	if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(answerPath))); err != nil {
		t.Fatalf("remove original S9 author answer after acceptance: %v", err)
	}
	if got, found, err := runtime.VerifiedRepairPlanReportReceipt(fixture.root, currentS9State(t, fixture), packet.ID, owner.agentID); err != nil || !found || !reflect.DeepEqual(got, recoveredReceipt) {
		t.Fatalf("read-only original S9 receipt depended on author file: found=%v err=%v", found, err)
	}
	stateBeforeMissingAuthor, journalBeforeMissingAuthor := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	if _, _, err := submitS9RecoveryTestPacket(fixture, before.Revision, packet, answerPath, owner.agentID); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("S9 save replay accepted a missing author answer or returned the wrong error: %v", err)
	}
	if !bytes.Equal(stateBeforeMissingAuthor, mustRead(t, fixture.statePath)) || !bytes.Equal(journalBeforeMissingAuthor, mustRead(t, fixture.journalPath)) {
		t.Fatal("missing-author-file S9 save changed the recovered Runtime pair")
	}
}

func TestS9PublicStageAnswerIssueRecoversSealedPendingPacketIssuance(t *testing.T) {
	fixture, _, owners := prepareS9RepairPlanReportHookFixture(t)
	if len(owners) != 2 {
		t.Fatalf("fixture Builder owners=%d, want two", len(owners))
	}

	initial := currentS9Snapshot(t, fixture)
	firstPackets, err := repair.IssueRepairPlanReportPackets(fixture.root, fixture.statePath, fixture.journalPath, initial.Revision, owners[0].agentID)
	if err != nil || len(firstPackets) != 1 {
		t.Fatalf("issue the first exact S9 packet before the pending fixture: packets=%d err=%v", len(firstPackets), err)
	}
	firstPacket := firstPackets[0]
	firstAnswerPath := writeS9RepairPlanReportAnswer(t, fixture.root, firstPacket, "Preserve this authored first-Builder answer across public issue recovery.")
	afterFirst := currentS9Snapshot(t, fixture)
	firstState, firstJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)

	secondPackets, err := repair.IssueRepairPlanReportPackets(fixture.root, fixture.statePath, fixture.journalPath, afterFirst.Revision, owners[1].agentID)
	if err != nil || len(secondPackets) != 1 {
		t.Fatalf("issue the second exact S9 packet before the pending fixture: packets=%d err=%v", len(secondPackets), err)
	}
	secondPacket := secondPackets[0]
	secondAnswerPath := writeS9RepairPlanReportAnswer(t, fixture.root, secondPacket, "Preserve this authored second-Builder answer across public issue recovery.")
	afterSecond := currentS9Snapshot(t, fixture)
	if afterSecond.Revision != afterFirst.Revision+1 {
		t.Fatalf("second S9 packet issue advanced revision to %d, want exactly %d", afterSecond.Revision, afterFirst.Revision+1)
	}
	secondState, secondJournal := mustRead(t, fixture.statePath), mustRead(t, fixture.journalPath)
	marker := buildS8SealedCommitMarker(t, firstState, firstJournal, secondState, secondJournal)
	// PACKET-ISSUE retains the previous last_transition in the candidate state.
	marker["retain_last_transition"] = true

	// Restore the exact pair before the second issuance, leaving the already
	// issued second packet, journal event, and both authored answer files bound
	// by the actual original commit marker.
	if err := os.WriteFile(fixture.statePath, firstState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.journalPath, firstJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	markerPath := fixture.statePath + ".commit-pending.json"
	writeInvestigationJSON(t, markerPath, marker)
	firstAnswerBytes := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(firstAnswerPath)))
	secondAnswerBytes := mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(secondAnswerPath)))

	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("public original `runtime stage-answer issue` pending recovery: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var issued []workpacket.RepairPlanReportPacket
	if err := json.Unmarshal(stdout.Bytes(), &issued); err != nil {
		t.Fatalf("decode public S9 issue replay: %v; stdout=%s", err, stdout.String())
	}
	if len(issued) != 2 {
		t.Fatalf("public S9 issue recovery returned %d packets, want both registered Builders: %+v", len(issued), issued)
	}
	issuedByID := map[string]workpacket.RepairPlanReportPacket{}
	for _, packet := range issued {
		issuedByID[packet.ID] = packet
	}
	for _, expected := range []workpacket.RepairPlanReportPacket{firstPacket, secondPacket} {
		got, ok := issuedByID[expected.ID]
		if !ok || got.AnswerPath != expected.AnswerPath || got.ReportID != expected.ReportID {
			t.Fatalf("public S9 issue replay did not return exact original packet %s: found=%v got=%+v", expected.ID, ok, got)
		}
	}
	if !bytes.Equal(secondState, mustRead(t, fixture.statePath)) || !bytes.Equal(secondJournal, mustRead(t, fixture.journalPath)) {
		t.Fatal("public S9 issue recovery did not restore the exact marker-bound Runtime pair")
	}
	if !bytes.Equal(firstAnswerBytes, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(firstAnswerPath)))) ||
		!bytes.Equal(secondAnswerBytes, mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(secondAnswerPath)))) {
		t.Fatal("public S9 issue replay overwrote an author-edited answer file")
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("public S9 issue recovery left the original commit marker: %v", err)
	}

	journalLines := bytes.Split(bytes.TrimSpace(secondJournal), []byte("\n"))
	for _, packet := range []workpacket.RepairPlanReportPacket{firstPacket, secondPacket} {
		count := 0
		for _, line := range journalLines {
			var event map[string]any
			if err := json.Unmarshal(line, &event); err != nil {
				t.Fatalf("decode S9 PACKET-ISSUE event: %v", err)
			}
			if event["transition_id"] == "PACKET-ISSUE" && event["event_id"] == packet.IssuanceEventID && event["idempotency_key"] == "packet:"+packet.ID {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("original S9 issue journal has %d exact PACKET-ISSUE events for %s, want one", count, packet.ID)
		}
	}
}

func issueS9RecoveryTestPacket(t *testing.T, fixture *intakeFixture, owner string) workpacket.RepairPlanReportPacket {
	t.Helper()
	snapshot := currentS9Snapshot(t, fixture)
	packets, err := repair.IssueRepairPlanReportPackets(fixture.root, fixture.statePath, fixture.journalPath, snapshot.Revision, owner)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue one original S9 PlanReport packet for %s: packets=%d err=%v", owner, len(packets), err)
	}
	return packets[0]
}

func submitS9RecoveryTestPacket(fixture *intakeFixture, expectedRevision int, packet workpacket.RepairPlanReportPacket, answerPath, owner string) (runtime.Snapshot, repair.PlanReport, error) {
	return repair.SubmitIssuedRepairPlanReport(fixture.root, fixture.statePath, fixture.journalPath, repair.SubmitIssuedRepairPlanReportRequest{
		ExpectedRevision: expectedRevision,
		PacketID:         packet.ID,
		AnswerPath:       answerPath,
		OwnerAgentID:     owner,
		SourceRoot:       fixture.root,
		OccurredAt:       time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC),
		AuthorizeSource: func(root string, _ map[string]any) error {
			if root != fixture.root {
				return fmt.Errorf("unexpected S9 source authority root %q", root)
			}
			return nil
		},
	})
}

func assertS9SingleOriginalReportCommit(t *testing.T, fixture *intakeFixture, state map[string]any, packet workpacket.RepairPlanReportPacket, receipt runtime.RepairPlanReportReceipt) {
	t.Helper()
	if receipt.PacketID != packet.ID || receipt.ReportID != packet.ReportID || receipt.ReportRef != ".claude/review/repair/plan-reports/"+packet.ReportID+".json" || receipt.WorkComplete {
		t.Fatalf("original S9 report receipt identity/completion = %+v", receipt)
	}
	review, _ := state["review"].(map[string]any)
	repairState, _ := review["repair"].(map[string]any)
	if repairState["plan_report_ref"] != receipt.ReportRef {
		t.Fatalf("original S9 singular report pointer = %v, want %s", repairState["plan_report_ref"], receipt.ReportRef)
	}
	refs, _ := repairState["plan_report_refs"].([]any)
	refCount := 0
	var indexedSHA string
	for _, raw := range refs {
		ref, _ := raw.(map[string]any)
		if ref["path"] == receipt.ReportRef {
			refCount++
			indexedSHA, _ = ref["sha256"].(string)
		}
	}
	actualSHA := workpacket.Digest(mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(receipt.ReportRef))))
	if refCount != 1 || indexedSHA != actualSHA {
		t.Fatalf("original Runtime contains report path refs=%d SHA=%q for packet %s, want one ref with SHA %q", refCount, indexedSHA, packet.ID, actualSHA)
	}

	lines := bytes.Split(bytes.TrimSpace(mustRead(t, fixture.journalPath)), []byte("\n"))
	ptrBug10 := 0
	acceptedEvent := 0
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode original S9 Runtime event: %v", err)
		}
		if event["transition_id"] == "PTR-BUG-10" {
			ptrBug10++
			if event["event"] == "transition_committed" && event["event_id"] == receipt.AcceptedEventID && event["idempotency_key"] == receipt.AcceptedIdempotencyKey {
				acceptedEvent++
			}
		}
	}
	if ptrBug10 != 1 || acceptedEvent != 1 {
		t.Fatalf("original journal has PTR-BUG-10=%d matching accepted S9 event=%d, want exactly one each", ptrBug10, acceptedEvent)
	}
}
