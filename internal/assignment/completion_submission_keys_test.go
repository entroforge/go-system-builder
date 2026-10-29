package assignment

import (
	"bytes"
	"encoding/json"
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
	var previous map[string]any
	if err := json.Unmarshal(beforeState, &previous); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(journalBytes, beforeJournal)), &event); err != nil {
		t.Fatal(err)
	}
	previousJSON, _ := json.MarshalIndent(previous, "", "  ")
	desiredJSON, _ := json.MarshalIndent(aliased.State, "", "  ")
	eventJSON, _ := json.Marshal(event)
	pending, _ := json.Marshal(map[string]any{
		"schema_version": "1.0.0", "previous_revision": first.Revision,
		"previous_state_sha256": registrationSHA(append(previousJSON, '\n')),
		"state_sha256":          registrationSHA(append(desiredJSON, '\n')),
		"journal_event_sha256":  registrationSHA(append(eventJSON, '\n')),
		"request_id":            event["request_id"], "idempotency_key": event["idempotency_key"],
		"retain_last_transition": true, "state": aliased.State, "journal_event": event,
	})
	mustWriteCanonicalRecovery(t, statePath, beforeState)
	mustWriteCanonicalRecovery(t, journalPath, beforeJournal)
	mustWriteCanonicalRecovery(t, statePath+".commit-pending.json", pending)
	writer := loopruntime.NewWriter(statePath, journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
	if recovered, err := writer.RecoverPendingOperations(); err != nil || !recovered {
		t.Fatalf("alias pending recovery: %t %v", recovered, err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, stateBytes, journalBytes)
	// A fresh call creates a new Store; no in-memory replay cache participates.
	replayed, again, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath})
	if err != nil || replayed.Revision != aliased.Revision || again.ResultRef != primary.ResultRef || !again.Replayed {
		t.Fatalf("persisted alias replay: %+v %v", again, err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, stateBytes, journalBytes)
	aliasBody["summary"] = "A changed answer under the already received alias"
	aliasPath = writeRegistrationMessage(t, fixture.root, "alias-conflict-result.json", aliasBody)
	if _, _, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: aliasPath}); err == nil || !strings.Contains(err.Error(), "different AnswerDigest") {
		t.Fatalf("reused alias with changed Answer must be rejected: %v", err)
	}
	assertRegistrationPairUnchanged(t, statePath, journalPath, stateBytes, journalBytes)
}
