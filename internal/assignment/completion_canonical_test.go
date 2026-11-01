package assignment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentresult"
)

func TestCompleteTaskCanonicalResultAcceptsAndReplaysOriginalReceipts(t *testing.T) {
	fixture, statePath, journalPath, record, sourceRef, sourceBytes := readyCanonicalResultFixture(t)
	message := canonicalResultMessage(t, fixture, record, "msg-s6-result-first", "Implemented the reviewed behavior", []string{sourceRef})
	messagePath := writeRegistrationMessage(t, fixture.root, "canonical-result-first.json", message)

	first, firstReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath,
		OccurredAt: time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CompleteTaskWithReceipt: %v", err)
	}
	if firstReceipt.ResultRef == "" || firstReceipt.ResultRevision != 1 || firstReceipt.Replayed || !firstReceipt.Current {
		t.Fatalf("first Result receipt = %+v", firstReceipt)
	}
	if first.Revision != 13 {
		t.Fatalf("first Result revision = %d, want one Writer revision after AgentBegin", first.Revision)
	}
	assertCanonicalResultProjection(t, fixture.root, first, record, firstReceipt, sourceRef, sourceBytes, "submitted")
	beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)

	replayed, replayReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("replay same message and answer: %v", err)
	}
	if replayed.Revision != first.Revision || !replayReceipt.Replayed || !replayReceipt.Current || replayReceipt.ResultRef != firstReceipt.ResultRef {
		t.Fatalf("same-message replay changed or lost original receipt: revision=%d receipt=%+v", replayed.Revision, replayReceipt)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)

	changedAnswer := canonicalResultMessage(t, fixture, record, "msg-s6-result-first", "A changed answer under the original key", []string{sourceRef})
	changedPath := writeRegistrationMessage(t, fixture.root, "canonical-result-changed-key.json", changedAnswer)
	if _, _, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: changedPath,
	}); err == nil || !strings.Contains(err.Error(), "different answer") {
		t.Fatalf("same message_id with changed answer error = %v", err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)

	sameAnswerNewKey := canonicalResultMessage(t, fixture, record, "msg-s6-result-retry", "Implemented the reviewed behavior", []string{sourceRef})
	sameAnswerPath := writeRegistrationMessage(t, fixture.root, "canonical-result-same-answer.json", sameAnswerNewKey)
	sameAnswerSnapshot, sameAnswerReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: sameAnswerPath,
	})
	if err != nil {
		t.Fatalf("same answer under a new message_id: %v", err)
	}
	if sameAnswerSnapshot.Revision != first.Revision+1 || !sameAnswerReceipt.Replayed || sameAnswerReceipt.ResultRef != firstReceipt.ResultRef {
		t.Fatalf("same-answer replay did not return the original Result: revision=%d receipt=%+v", sameAnswerSnapshot.Revision, sameAnswerReceipt)
	}
	// A new submission key is durably bound in the original Writer, while
	// the already accepted Result and its current pointers stay unchanged.
	assertCanonicalResultProjection(t, fixture.root, sameAnswerSnapshot, record, sameAnswerReceipt, sourceRef, sourceBytes, "submitted")

	ackMessagePath := writeRegistrationMessage(t, fixture.root, "canonical-result-prior-ack.json", map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_ack",
		"message_id": "msg-s6-result-prior-ack", "correlation_id": "corr-s6-result-prior-ack",
		"runtime_id": record.RuntimeID, "agent_id": record.OwnerAgentID,
		"agent_definition_ref": record.AgentDefinition.Ref, "task_id": record.Task.ID,
		"bug_id": nil, "team_id": record.TeamID, "occurred_at": "2026-10-14T14:00:30Z",
	})
	acknowledged, err := AdvanceAgent(fixture.root, statePath, journalPath, AgentEventRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, Event: "completion_acknowledged", MessagePath: ackMessagePath,
	})
	if err != nil {
		t.Fatalf("record prior Main acknowledgement: %v", err)
	}
	priorAckAgent := registrationAgent(acknowledged.State, record.OwnerAgentID)
	if priorAckAgent["state"] != "done" || priorAckAgent["completion_acknowledged_ref"] == nil {
		t.Fatalf("fixture did not record a prior acknowledgement without dispatch config: %v", priorAckAgent)
	}

	secondMessage := canonicalResultMessage(t, fixture, record, "msg-s6-result-second", "Implemented and checked the reviewed behavior", []string{sourceRef})
	secondPath := writeRegistrationMessage(t, fixture.root, "canonical-result-second.json", secondMessage)
	second, secondReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: secondPath,
		OccurredAt: time.Date(2026, 10, 14, 14, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("new answer and message_id: %v", err)
	}
	if secondReceipt.ResultRevision != 2 || secondReceipt.ResultRef == firstReceipt.ResultRef || secondReceipt.Replayed || !secondReceipt.Current || second.Revision != acknowledged.Revision+1 {
		t.Fatalf("second Result was not accepted as the next current revision: revision=%d receipt=%+v", second.Revision, secondReceipt)
	}
	secondAgent := registrationAgent(second.State, record.OwnerAgentID)
	if secondAgent["state"] != "reported" || secondAgent["completion_acknowledged_ref"] != nil || secondAgent["completion_acknowledged_reports"] != nil {
		t.Fatalf("new canonical Result retained old Main acknowledgement identity: %v", secondAgent)
	}
	assertCanonicalResultProjection(t, fixture.root, second, record, secondReceipt, sourceRef, sourceBytes, "submitted")

	beforeHistoricalState, beforeHistoricalJournal := registrationPairBytes(t, statePath, journalPath)
	historical, historicalReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("replay historical Result: %v", err)
	}
	if historical.Revision != second.Revision || !historicalReceipt.Replayed || historicalReceipt.Current || historicalReceipt.ResultRef != firstReceipt.ResultRef {
		t.Fatalf("historical replay moved current Result pointers: revision=%d receipt=%+v", historical.Revision, historicalReceipt)
	}
	finalState, finalJournal := registrationPairBytes(t, statePath, journalPath)
	if !bytes.Equal(beforeHistoricalState, finalState) || !bytes.Equal(beforeHistoricalJournal, finalJournal) {
		t.Fatal("historical Result replay wrote a new Runtime revision or journal event")
	}
}

func TestCompleteTaskCanonicalResultKeepsActualAnswerAndRejectsEmptyDraft(t *testing.T) {
	t.Run("blank professional draft", func(t *testing.T) {
		fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
		message := canonicalResultMessage(t, fixture, record, "msg-s6-result-blank", "", []string{sourceRef})
		path := writeRegistrationMessage(t, fixture.root, "canonical-result-blank.json", message)
		beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
		if _, _, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
			ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: path,
		}); err == nil || !strings.Contains(err.Error(), "completion message schema") {
			t.Fatalf("blank S6 Result draft error = %v", err)
		}
		assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)
		assertNoCanonicalResultArtifacts(t, fixture.root, record)
	})

	for _, tc := range []struct {
		name, ref string
		extra     bool
	}{
		{"ordinary source reference", ".claude/loop-state.json", false},
		{"unrelated additional fields", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
			if tc.ref != "" {
				sourceRef = tc.ref
			}
			message := canonicalResultMessage(t, fixture, record, "msg-s6-result-professional", "The professional answer names a source", []string{sourceRef})
			if tc.extra {
				message["checks"] = []any{map[string]any{"name": "unit", "result": "pass"}}
			}
			path := writeRegistrationMessage(t, fixture.root, "canonical-result-professional.json", message)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
				ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: path,
			})
			if err != nil || receipt.ResultRef == "" {
				t.Fatalf("actual professional answer not received: %+v %v", receipt, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("author answer changed: %v", err)
			}
		})
	}
}

func TestCompleteTaskCanonicalResultReplayKeepsOriginalProfessionalAnswer(t *testing.T) {
	fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
	message := canonicalResultMessage(t, fixture, record, "msg-s6-result-source-drift", "Implemented the reviewed behavior", []string{sourceRef})
	messagePath := writeRegistrationMessage(t, fixture.root, "canonical-result-source-drift.json", message)
	accepted, firstReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("accept Result: %v", err)
	}
	beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
	changedBytes := []byte("package pkg\n// source changed after acceptance\n")
	if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(sourceRef))); err != nil {
		t.Fatal(err)
	}
	replayed, replayReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("replay after live source cleanup: %v", err)
	}
	if replayed.Revision != accepted.Revision || !replayReceipt.Replayed || replayReceipt.ResultRef != firstReceipt.ResultRef {
		t.Fatalf("live source change rebound the original Result: revision=%d receipt=%+v", replayed.Revision, replayReceipt)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, beforeState, beforeJournal)

	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(sourceRef)), changedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	newAnswer := canonicalResultMessage(t, fixture, record, "msg-s6-result-new-fact", "A later answer reflects the changed source", []string{sourceRef})
	newAnswerPath := writeRegistrationMessage(t, fixture.root, "canonical-result-new-fact.json", newAnswer)
	newSnapshot, newReceipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: newAnswerPath,
	})
	if err != nil {
		t.Fatalf("accept new Result from changed live source: %v", err)
	}
	if newReceipt.ResultRevision != 2 || newReceipt.Replayed || newReceipt.ResultRef == firstReceipt.ResultRef || newSnapshot.Revision != accepted.Revision+1 {
		t.Fatalf("new facts did not create the next Result revision: revision=%d receipt=%+v", newSnapshot.Revision, newReceipt)
	}
	assertCanonicalResultProjection(t, fixture.root, newSnapshot, record, newReceipt, sourceRef, changedBytes, "submitted")

	var firstResult assignmentresult.AcceptedResult
	firstEnvelope, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(firstReceipt.ResultRef)))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(firstEnvelope, &firstResult); err != nil {
		t.Fatal(err)
	}
	if firstResult.Answer.Summary != "Implemented the reviewed behavior" ||
		len(firstResult.Answer.AssertionEvidence) != 1 ||
		!reflect.DeepEqual(firstResult.Answer.AssertionEvidence[0].EvidenceRefs, []string{sourceRef}) {
		t.Fatalf("the original professional answer was changed by the later Result: %+v", firstResult.Answer)
	}

}

func readyCanonicalResultFixture(t *testing.T) (recordRegistrationFixture, string, string, Record, string, []byte) {
	t.Helper()
	fixture, statePath, journalPath, record, _ := registerAgentBeginFixture(t, true)
	planPath := writeThinS6Plan(t, fixture, record, "msg-s6-result-plan", "corr-s6-result-plan")
	snapshot, outcome, err := AgentBegin(fixture.root, statePath, journalPath, AgentBeginRequest{
		ExpectedRevision: -1, AgentID: record.OwnerAgentID, PlanPath: planPath,
		OccurredAt: time.Date(2026, 10, 14, 13, 30, 0, 0, time.UTC),
	})
	if err != nil || !outcome.Chained || outcome.FinalState != "working" {
		t.Fatalf("AgentBegin before canonical Result: outcome=%+v err=%v", outcome, err)
	}
	if snapshot.Revision != 12 || registrationAgent(snapshot.State, record.OwnerAgentID)["state"] != "working" {
		t.Fatalf("AgentBegin fixture did not reach working: revision=%d agent=%v", snapshot.Revision, registrationAgent(snapshot.State, record.OwnerAgentID)["state"])
	}
	sourceRef := "src/pkg/result-proof.txt"
	sourceBytes := []byte("package pkg\n// current professional evidence\n")
	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(sourceRef)), sourceBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture, statePath, journalPath, record, sourceRef, sourceBytes
}

func canonicalResultMessage(t *testing.T, fixture recordRegistrationFixture, record Record, messageID, summary string, evidenceRefs []string) map[string]any {
	t.Helper()
	var evidence []any
	if summary != "" {
		evidence = []any{map[string]any{
			"assertion":     "TASK behavior is implemented",
			"oracle":        "the selected required check passes",
			"evidence_refs": evidenceRefs,
		}}
	}
	return map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report",
		"message_id": messageID, "correlation_id": "corr-" + strings.TrimPrefix(messageID, "msg-"),
		"runtime_id": record.RuntimeID, "baseline_generation": record.BaselineGeneration,
		"agent_id": record.OwnerAgentID, "agent_definition_ref": record.AgentDefinition.Ref,
		"task_id": record.Task.ID, "bug_id": nil, "team_id": record.TeamID,
		"occurred_at":   "2026-10-14T14:00:00Z",
		"assignment_id": record.AssignmentID, "assignment_revision": record.Revision,
		"assignment_record_contract": record.Contract,
		"summary":                    summary, "assertion_evidence": evidence,
		"risks_blockers": []string{}, "scope_deviations": []string{},
	}
}

func assertCanonicalResultProjection(t *testing.T, root string, snapshot struct {
	Revision int
	State    map[string]any
}, record Record, receipt ResultAcceptanceReceipt, sourceRef string, _ []byte, conclusion string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(receipt.ResultRef)))
	if err != nil {
		t.Fatalf("read accepted Result %s: %v", receipt.ResultRef, err)
	}
	var result assignmentresult.AcceptedResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Conclusion != conclusion || result.ResultRevision != receipt.ResultRevision || result.AssignmentRecord != record.Reference() {
		t.Fatalf("canonical Result envelope = %+v", result)
	}
	if len(result.Answer.AssertionEvidence) != 1 || !reflect.DeepEqual(result.Answer.AssertionEvidence[0].EvidenceRefs, []string{sourceRef}) {
		t.Fatalf("original professional evidence references = %+v", result.Answer.AssertionEvidence)
	}

	if strings.Contains(string(data), `"status"`) || strings.Contains(string(data), `"checks"`) || strings.Contains(string(data), `"changed_paths"`) || strings.Contains(string(data), `"reviewed_paths"`) {
		t.Fatalf("canonical Result contains machine or legacy completion fields: %s", data)
	}
	agent := registrationAgent(snapshot.State, record.OwnerAgentID)
	entities, _ := snapshot.State["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	if agent["completion_reported_ref"] != receipt.ResultRef || len(tasks) != 1 || tasks[0].(map[string]any)["completion_report_ref"] != receipt.ResultRef {
		t.Fatalf("Agent/TASK current Result refs do not match receipt: agent=%v tasks=%v", agent, tasks)
	}
	items, _ := snapshot.State["evidence"].([]any)
	found := false
	for _, raw := range items {
		row, _ := raw.(map[string]any)
		if row["path"] == receipt.ResultRef {
			found = true
			if row["status"] != "valid" || len(row["scope_refs"].([]any)) != 0 {
				t.Fatalf("canonical Result evidence index row = %#v", row)
			}
		}
	}
	if !found {
		t.Fatal("canonical Result is not present in original Runtime evidence index")
	}
}

func assertNoCanonicalResultArtifacts(t *testing.T, root string, record Record) {
	t.Helper()
	ownerDir := filepath.Join(root, ".claude", "evidence", record.RuntimeID, "g1", "assignments", record.OwnerAgentID)
	if entries, err := os.ReadDir(ownerDir); err == nil && len(entries) != 0 {
		t.Fatalf("rejected canonical Result left staged artifacts: %v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("inspect canonical Result artifact directory: %v", err)
	}
}

func TestCanonicalResultWriterLeavesUnknownOccupiedCandidateAndRegistersOwnPath(t *testing.T) {
	const messageID = "msg-result-occupied-candidate"
	occurredAt := time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)
	// Build the exact envelope through the original Writer so the identical
	// occupancy case exercises real content rather than a made-up checksum.
	source, sourceState, sourceJournal, sourceRecord, sourceRef, _ := readyCanonicalResultFixture(t)
	sourceMessage := canonicalResultMessage(t, source, sourceRecord, messageID, "Implemented the behavior", []string{sourceRef})
	sourcePath := writeRegistrationMessage(t, source.root, "result-source.json", sourceMessage)
	_, issued, err := CompleteTaskWithReceipt(source.root, sourceState, sourceJournal, CompletionRequest{
		ExpectedRevision: -1, AgentID: sourceRecord.OwnerAgentID, MessagePath: sourcePath, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("create original result fixture: %v", err)
	}
	identical, err := os.ReadFile(filepath.Join(source.root, filepath.FromSlash(issued.ResultRef)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		occupant []byte
	}{
		{"different bytes", []byte("unknown content")},
		{"identical bytes", identical},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, statePath, journalPath, record, ref, sourceBytes := readyCanonicalResultFixture(t)
			message := canonicalResultMessage(t, fixture, record, messageID, "Implemented the behavior", []string{ref})
			messagePath := writeRegistrationMessage(t, fixture.root, "result-answer.json", message)
			candidate := canonicalResultArtifactPath(record, canonicalResultEvidenceID(record, 1, messageID))
			occupied := filepath.Join(fixture.root, filepath.FromSlash(candidate))
			if err := os.MkdirAll(filepath.Dir(occupied), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(occupied, tc.occupant, 0600); err != nil {
				t.Fatal(err)
			}
			request := CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath, OccurredAt: occurredAt}
			snapshot, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, request)
			if err != nil {
				t.Fatalf("accept with occupied candidate: %v", err)
			}
			if receipt.ResultRef == candidate || receipt.ResultRef == "" {
				t.Fatalf("Writer claimed unknown candidate: %+v", receipt)
			}
			if retained, err := os.ReadFile(occupied); err != nil || !bytes.Equal(retained, tc.occupant) {
				t.Fatalf("unknown candidate changed: %q %v", retained, err)
			}
			if tc.name == "identical bytes" {
				published, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(receipt.ResultRef)))
				if err != nil || !bytes.Equal(published, identical) {
					t.Fatalf("the occupied file did not actually match this Writer's envelope: %v", err)
				}
			}
			assertCanonicalResultProjection(t, fixture.root, snapshot, record, receipt, ref, sourceBytes, "submitted")
			replayed, again, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, request)
			if err != nil || !again.Replayed || again.ResultRef != receipt.ResultRef || replayed.Revision != snapshot.Revision {
				t.Fatalf("registered responsibility did not replay: %+v %v", again, err)
			}
		})
	}
}

func TestCanonicalResultWriterConcurrentSameAnswerUsesOneRegisteredPath(t *testing.T) {
	fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
	message := canonicalResultMessage(t, fixture, record, "msg-result-concurrent-issuance", "Implemented the behavior", []string{sourceRef})
	messagePath := writeRegistrationMessage(t, fixture.root, "result-concurrent-answer.json", message)
	request := CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath}
	type outcome struct {
		snapshot int
		receipt  ResultAcceptanceReceipt
		err      error
	}
	const concurrent = 8
	start, outcomes := make(chan struct{}), make(chan outcome, concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			<-start
			snapshot, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, request)
			outcomes <- outcome{snapshot.Revision, receipt, err}
		}()
	}
	close(start)
	first := <-outcomes
	if first.err != nil || first.receipt.ResultRef == "" {
		t.Fatalf("concurrent original Writer: %+v", first)
	}
	for i := 1; i < concurrent; i++ {
		next := <-outcomes
		if next.err != nil || first.receipt.ResultRef != next.receipt.ResultRef || first.snapshot != next.snapshot {
			t.Fatalf("same responsibility had multiple results: %+v %+v", first, next)
		}
	}
}

func TestCanonicalResultWriterConcurrentChangedAnswerAndWrongOwner(t *testing.T) {
	fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
	first := canonicalResultMessage(t, fixture, record, "msg-concurrent-conflict", "First professional answer", []string{sourceRef})
	second := canonicalResultMessage(t, fixture, record, "msg-concurrent-conflict", "Different professional answer", []string{sourceRef})
	firstPath := writeRegistrationMessage(t, fixture.root, "result-conflict-first.json", first)
	secondPath := writeRegistrationMessage(t, fixture.root, "result-conflict-second.json", second)
	type outcome struct {
		receipt ResultAcceptanceReceipt
		err     error
	}
	start := make(chan struct{})
	results := make(chan outcome, 3)
	for _, req := range []CompletionRequest{
		{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: firstPath},
		{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: secondPath},
		{ExpectedRevision: -1, AgentID: "another-owner", MessagePath: firstPath},
	} {
		go func(req CompletionRequest) {
			<-start
			_, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, req)
			results <- outcome{receipt: receipt, err: err}
		}(req)
	}
	close(start)
	accepted, different, wrongOwner := 0, 0, 0
	for i := 0; i < 3; i++ {
		result := <-results
		switch {
		case result.err == nil && result.receipt.ResultRef != "":
			accepted++
		case result.err != nil && strings.Contains(result.err.Error(), "different answer"):
			different++
		case result.err != nil && strings.Contains(result.err.Error(), "does not match Agent"):
			wrongOwner++
		default:
			t.Fatalf("unexpected concurrent outcome: %+v", result)
		}
	}
	if accepted != 1 || different != 1 || wrongOwner != 1 {
		t.Fatalf("conflicting answers or owners crossed responsibilities: accepted=%d different=%d wrongOwner=%d", accepted, different, wrongOwner)
	}
}

func TestCanonicalResultWriterSkipsOccupiedCandidateParent(t *testing.T) {
	fixture, statePath, journalPath, record, ref, _ := readyCanonicalResultFixture(t)
	const messageID = "msg-s6-occupied-parent"
	message := canonicalResultMessage(t, fixture, record, messageID, "Implemented the behavior", []string{ref})
	messagePath := writeRegistrationMessage(t, fixture.root, "result-parent-answer.json", message)
	candidate := canonicalResultArtifactPath(record, canonicalResultEvidenceID(record, 1, messageID))
	parent := filepath.Dir(filepath.Join(fixture.root, filepath.FromSlash(candidate)))
	if err := os.MkdirAll(filepath.Dir(parent), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("unknown parent file"), 0600); err != nil {
		t.Fatal(err)
	}
	_, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath})
	if err != nil || receipt.ResultRef == "" || receipt.ResultRef == candidate {
		t.Fatalf("reserve alternate Result: %+v %v", receipt, err)
	}
	if got, err := os.ReadFile(parent); err != nil || string(got) != "unknown parent file" {
		t.Fatalf("unknown parent changed: %q %v", got, err)
	}
}
