package assignment

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// Reconstruct original Store durable boundaries from its actual accepted
// state/event. This exercises pending recovery, not a live process crash.
func TestCompleteTaskCanonicalResultPendingRecoveryRetainsOriginalArtifacts(t *testing.T) {
	for _, boundary := range []string{"before state", "after state", "after journal"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
			beforeState, beforeJournal := registrationPairBytes(t, statePath, journalPath)
			message := canonicalResultMessage(t, fixture, record, "msg-s6-pending-result", "Delivered behavior with original evidence", []string{sourceRef})
			messagePath := writeRegistrationMessage(t, fixture.root, "canonical-pending-result.json", message)
			accepted, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath})
			if err != nil {
				t.Fatal(err)
			}
			afterState, afterJournal := registrationPairBytes(t, statePath, journalPath)
			var previous map[string]any
			if err := json.Unmarshal(beforeState, &previous); err != nil {
				t.Fatal(err)
			}
			lastLine := bytes.TrimSpace(bytes.TrimPrefix(afterJournal, beforeJournal))
			var event map[string]any
			if err := json.Unmarshal(lastLine, &event); err != nil {
				t.Fatal(err)
			}
			previousJSON, _ := json.MarshalIndent(previous, "", "  ")
			desiredJSON, _ := json.MarshalIndent(accepted.State, "", "  ")
			eventJSON, _ := json.Marshal(event)
			pending := map[string]any{
				"schema_version": "1.0.0", "previous_revision": accepted.Revision - 1,
				"previous_state_sha256": registrationSHA(append(previousJSON, '\n')), "state_sha256": registrationSHA(append(desiredJSON, '\n')),
				"journal_event_sha256": registrationSHA(append(eventJSON, '\n')),
				"request_id":           event["request_id"], "idempotency_key": event["idempotency_key"],
				"retain_last_transition": false, "state": accepted.State, "journal_event": event,
			}
			pendingBytes, _ := json.Marshal(pending)
			marker := statePath + ".commit-pending.json"
			if boundary == "before state" {
				mustWriteCanonicalRecovery(t, statePath, beforeState)
			}
			if boundary != "after journal" {
				mustWriteCanonicalRecovery(t, journalPath, beforeJournal)
			}
			mustWriteCanonicalRecovery(t, marker, pendingBytes)
			reader := loopruntime.NewStore(statePath, journalPath)
			if _, err := reader.Snapshot(); !errors.Is(err, loopruntime.ErrPendingRuntimeOperation) {
				t.Fatalf("readonly Snapshot pending error = %v", err)
			}
			writer := loopruntime.NewWriter(statePath, journalPath, fixture.root, semantic.RuntimeCandidateValidator{})
			removed, err := writer.RemoveUnreferencedArtifact(loopruntime.ArtifactCleanupRequest{ExpectedRevision: accepted.Revision - 1, ArtifactPath: receipt.ResultRef, ArtifactSHA256: receipt.ResultSHA256})
			if err != nil || removed {
				t.Fatalf("pending artifact cleanup removed=%t err=%v", removed, err)
			}
			if recovered, err := writer.RecoverPendingOperations(); err != nil || !recovered {
				t.Fatalf("original Store recovery = %t, %v", recovered, err)
			}
			stateBytes, journalBytes := registrationPairBytes(t, statePath, journalPath)
			if !bytes.Equal(stateBytes, afterState) || !bytes.Equal(journalBytes, afterJournal) {
				t.Fatal("recovery changed the original accepted pair or appended duplicate event")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("pending marker remains: %v", err)
			}
			read := func(ref string) ([]byte, error) {
				return os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(ref)))
			}
			if _, err := assignmentresult.LoadAccepted(accepted.State, record, receipt.ResultRef, read); err != nil {
				t.Fatalf("recovered Result/source bytes: %v", err)
			}
			if _, replay, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath}); err != nil || !replay.Replayed || replay.ResultRef != receipt.ResultRef {
				t.Fatalf("recovered original receipt replay = %+v, %v", replay, err)
			}
			assertRegistrationPairUnchanged(t, statePath, journalPath, afterState, afterJournal)
		})
	}
}

func mustWriteCanonicalRecovery(t *testing.T, file string, body []byte) {
	t.Helper()
	if err := os.WriteFile(file, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
