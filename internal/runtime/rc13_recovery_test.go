package runtime_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// RC-13 contract: a journal rotation marker that exists on disk at
// recovery time but is not declared in the plan's SourcePendingSHA256
// must be rejected as input drift. The plan must declare every Runtime
// pending marker (including rotation) it expects to retire.
func TestApplyRecoveryRejectsUndeclaredRotationMarker(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged-state\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))

	rotationMarkerPath := statePath + ".journal-rotation-pending.json"
	rotationMarker := []byte(`{"schema_version":"1.0.0","archived_file":"/tmp/x","archived_sha256":"` + strings.Repeat("0", 64) + `","archived_count":1,"tail_sequence":1,"tail_event_id":"evt-x","started_at":"2026-08-29T00:00:00Z"}`)
	writeRecoveryFile(t, rotationMarkerPath, rotationMarker)

	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-rc13-undeclared", "plan-sha-rc13-undeclared")
	// Intentionally do NOT declare SourcePendingSHA256 for the rotation marker.
	_, err := runtime.ApplyRecovery(req)
	if err == nil {
		t.Fatal("ApplyRecovery must reject undeclared Runtime pending marker")
	}
	if !errors.Is(err, runtime.ErrRecoveryInputDrift) {
		t.Fatalf("error must be ErrRecoveryInputDrift, got %v", err)
	}
}
