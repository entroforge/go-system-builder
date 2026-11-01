package assignment

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestCompleteTaskCanonicalResultAliasIsDurableAndRejectsChangedAnswer(t *testing.T) {
	fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
	firstBody := canonicalResultMessage(t, fixture, record, "msg-result-primary", "Delivered the frozen professional answer", []string{sourceRef})
	primaryPath := writeRegistrationMessage(t, fixture.root, "primary-key-result.json", firstBody)
	first, primary, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: primaryPath})
	if err != nil {
		t.Fatal(err)
	}
	aliasBody := canonicalResultMessage(t, fixture, record, "msg-result-alias", "Delivered the frozen professional answer", []string{sourceRef})
	aliasPath := writeRegistrationMessage(t, fixture.root, "alias-key-result.json", aliasBody)
	beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
	aliased, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath})
	if err != nil {
		t.Fatal(err)
	}
	if aliased.Revision != first.Revision+1 || receipt.ResultRef != primary.ResultRef || receipt.ResultRevision != 1 || !receipt.Replayed || !receipt.Current {
		t.Fatalf("alias must bind the existing Result exactly once: revision=%d receipt=%+v", aliased.Revision, receipt)
	}
	keys, err := readResultSubmissionBindings(registrationAgent(aliased.State, record.OwnerAgentID))
	if err != nil || len(keys) != 1 || keys[0].SubmissionKey != "msg-result-alias" || keys[0].ResultRef != primary.ResultRef {
		t.Fatalf("original Writer alias index: %+v %v", keys, err)
	}
	stateBytes, journalBytes := registrationPairBytes(t, statePath, journalPath)
	// Reconstruct the original Writer's pending boundary before state publish.
	// An alias has no new Result assets and retains the original transition.

	var durableState map[string]any
	if err := json.Unmarshal(stateBytes, &durableState); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(journalBytes, beforeJournal)), &event); err != nil {
		t.Fatal(err)
	}
	pending, _ := json.Marshal(map[string]any{
		"schema_version": "1.0.0", "previous_revision": first.Revision,
		"request_id": event["request_id"], "idempotency_key": event["idempotency_key"],
		"retain_last_transition": true, "state": durableState, "journal_event": event,
	})
	mustWriteCanonicalRecovery(t, statePath, beforeState)
	mustWriteCanonicalRecovery(t, journalPath, beforeJournal)
	mustWriteCanonicalRecovery(t, statePath+".commit-pending.json", pending)
	writer := loopruntime.NewWriter(statePath, journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	if recovered, err := writer.RecoverPendingOperations(); err != nil || !recovered {
		t.Fatalf("alias pending recovery: %t %v", recovered, err)
	}
	recoveredState, recoveredJournal := registrationPairBytes(t, statePath, journalPath)
	var beforeValue, afterValue any
	if err := json.Unmarshal(stateBytes, &beforeValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recoveredState, &afterValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeValue, afterValue) {
		t.Fatalf("recovery changed the original accepted Runtime facts: before=%v after=%v", beforeValue, afterValue)
	}
	if !bytes.Equal(journalBytes, recoveredJournal) {
		t.Fatalf("recovery changed or duplicated the original journal event: before=%s after=%s", journalBytes, recoveredJournal)
	}
	stateBytes, journalBytes = recoveredState, recoveredJournal
	// A fresh call creates a new Store; no in-memory replay cache participates.
	replayed, again, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath})
	if err != nil || replayed.Revision != aliased.Revision || again.ResultRef != primary.ResultRef || !again.Replayed {
		t.Fatalf("persisted alias replay: %+v %v", again, err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, stateBytes, journalBytes)
	aliasBody["summary"] = "A changed answer under the already received alias"
	aliasPath = writeRegistrationMessage(t, fixture.root, "alias-conflict-result.json", aliasBody)
	if _, _, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath}); err == nil || !strings.Contains(err.Error(), "different answer") {
		t.Fatalf("reused alias with changed Answer must be rejected: %v", err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, stateBytes, journalBytes)
}
