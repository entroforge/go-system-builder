package runtime_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestReadOnlySnapshotReportsPendingRolloverWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	if err := os.WriteFile(journalPath, []byte("old-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fresh := freshInactiveState(t)
	archiveDir := filepath.Join(dir, "archive", "loop-test-r7")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveState := mustJSON(t, fresh)
	archiveJournal := []byte("archived-journal\n")
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-state.json"), archiveState, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-events.jsonl"), archiveJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, archiveJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	markerPath := statePath + ".rollover-pending.json"
	marker := map[string]any{
		"schema_version": "1.0.0",
		"fresh_state":    fresh,
		"record": map[string]any{
			"archive_dir":            archiveDir,
			"runtime_id":             "loop-test",
			"revision":               7,
			"archive_state_sha256":   sha256HexForTest(archiveState),
			"archive_journal_sha256": sha256HexForTest(archiveJournal),
		},
		"approval":    map[string]any{"approved_by": "release-owner", "evidence_id": "ev-approval"},
		"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := os.WriteFile(markerPath, mustJSON(t, marker), 0o644); err != nil {
		t.Fatal(err)
	}

	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	markerBefore := mustRead(t, markerPath)
	_, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("Snapshot error = %v, want diagnostic pending-operation error", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("read-only Snapshot changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("read-only Snapshot changed journal")
	}
	if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
		t.Fatal("read-only Snapshot changed pending marker")
	}
}

func TestRemoveUnreferencedArtifactSkipsPendingOperation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	artifactRel := ".claude/review/plans/review-plan-test-r2.json"
	artifactPath := filepath.Join(dir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := []byte("staged plan")
	if err := os.WriteFile(artifactPath, artifact, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath+".commit-pending.json", []byte(`{"schema_version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
		ExpectedRevision: 1,
		ArtifactPath:     artifactRel,
		ArtifactSHA256:   sha256HexForTest(artifact),
	})
	if err != nil {
		t.Fatalf("RemoveUnreferencedArtifact: %v", err)
	}
	if removed {
		t.Fatal("pending runtime operation must prevent artifact deletion")
	}
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("staged artifact was removed: %v", err)
	}

	if err := os.Remove(statePath + ".commit-pending.json"); err != nil {
		t.Fatal(err)
	}
	removed, err = runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RemoveUnreferencedArtifact(runtime.ArtifactCleanupRequest{
		ExpectedRevision: 1,
		ArtifactPath:     artifactRel,
		ArtifactSHA256:   sha256HexForTest(artifact),
	})
	if err != nil {
		t.Fatalf("RemoveUnreferencedArtifact without pending operation: %v", err)
	}
	if !removed {
		t.Fatal("unreferenced artifact should be removed when the runtime is stable")
	}
}

func TestWriterSnapshotRecoversPendingRolloverWithValidation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	if err := os.WriteFile(journalPath, []byte("old-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePendingRolloverMarker(t, statePath, dir, freshInactiveState(t))

	store := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{})
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatalf("writer Snapshot recovery: %v", err)
	}
	if snapshot.Revision != 0 {
		t.Fatalf("recovered revision = %d, want 0", snapshot.Revision)
	}
	if _, err := os.Stat(statePath + ".rollover-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending rollover marker remains: %v", err)
	}
	journal := mustRead(t, journalPath)
	if len(journal) != 0 {
		t.Fatalf("recovered journal = %q, want empty", journal)
	}

	// A second explicit writer operation must be idempotent: recovery has
	// already cleared the marker and must not append another event.
	if _, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot(); err != nil {
		t.Fatalf("second writer Snapshot: %v", err)
	}
	if got := mustRead(t, journalPath); len(got) != 0 {
		t.Fatalf("second recovery changed fresh journal: %q", got)
	}
}

func TestRecoverPendingRolloverCompletesTargetStateSourceJournalCrashWindow(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	if err := os.WriteFile(journalPath, []byte("old-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := freshInactiveState(t)
	writePendingRolloverMarker(t, statePath, dir, fresh)
	sourceJournal := mustRead(t, journalPath)
	// Simulate a process crash after the fresh state replacement and before
	// the journal reset in recoverPendingRolloverLocked.
	writeJSONMapRuntimeTest(t, statePath, fresh)

	completed, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RecoverPendingOperations()
	if err != nil {
		t.Fatalf("RecoverPendingOperations() mixed rollover retry error = %v", err)
	}
	if !completed {
		t.Fatal("mixed rollover retry did not complete the pending operation")
	}
	if got := mustRead(t, journalPath); len(got) != 0 {
		t.Fatalf("mixed rollover retry journal = %q, want empty (source was %q)", got, sourceJournal)
	}
	if _, err := os.Stat(statePath + ".rollover-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollover marker remains after mixed-pair retry: %v", err)
	}
}

func TestWriterRejectsInvalidArchivedJournalBeforeRolloverReplacement(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	if err := os.WriteFile(journalPath, []byte("current-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := freshInactiveState(t)
	markerPath := writePendingRolloverMarkerWithArchive(t, statePath, dir, fresh, []byte(`{"event_id":"missing-schema-fields"}`+"\n"))
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	markerBefore := mustRead(t, markerPath)

	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(err.Error(), "journal") {
		t.Fatalf("recovery error = %v, want archived journal validation failure", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("invalid archived journal changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("invalid archived journal changed active journal")
	}
	if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
		t.Fatal("invalid archived journal changed rollover marker")
	}
}

func TestWriterRejectsEmptyArchivedJournalWithNonEmptyStateCursor(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	fresh := freshInactiveState(t)
	writePendingRolloverMarkerWithArchive(t, statePath, dir, fresh, nil)
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(err.Error(), "empty archived journal") {
		t.Fatalf("recovery error = %v, want empty archived journal cursor rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("invalid empty archive recovery changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("invalid empty archive recovery changed journal")
	}
}

func TestWriterRejectsArchivedJournalSequenceGapBeforeRolloverReplacement(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	if err := os.WriteFile(journalPath, []byte("current-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := freshInactiveState(t)
	archiveJournal := validArchiveJournal(t, "loop-test", 7, sourceJournalEventID("loop-test", 7))
	markerPath := writePendingRolloverMarkerWithArchive(t, statePath, dir, fresh, archiveJournal)
	marker := readJSONMapRuntimeTest(t, markerPath)
	record := marker["record"].(map[string]any)
	archiveDir := record["archive_dir"].(string)
	lines := strings.Split(string(archiveJournal), "\n")
	gapJournal := []byte(strings.Join(append(append([]string{}, lines[:1]...), lines[2:]...), "\n"))
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-events.jsonl"), gapJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	record["archive_journal_sha256"] = sha256HexForTest(gapJournal)
	marker["source_journal_sha256"] = sha256HexForTest(gapJournal)
	writeJSONMapRuntimeTest(t, markerPath, marker)
	if err := os.WriteFile(journalPath, gapJournal, 0o644); err != nil {
		t.Fatal(err)
	}

	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(err.Error(), "not contiguous") {
		t.Fatalf("sequence-gap recovery error = %v, want contiguous-sequence diagnostic", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("sequence-gap recovery changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("sequence-gap recovery changed active journal")
	}
}

func TestCommitRecoveryRejectsMarkerCoherenceMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "state journal sequence",
			mutate: func(marker map[string]any) {
				state := marker["state"].(map[string]any)
				journal := state["journal"].(map[string]any)
				journal["last_sequence"] = float64(journal["last_sequence"].(float64) + 1)
				marker["state_sha256"] = sha256HexForTest(mustJSON(t, state))
			},
		},
		{
			name: "event after revision",
			mutate: func(marker map[string]any) {
				event := marker["journal_event"].(map[string]any)
				event["after_revision"] = float64(event["after_revision"].(float64) + 1)
				marker["journal_event_sha256"] = sha256HexForTest(jsonLineForTest(event))
			},
		},
		{
			name: "last transition linkage",
			mutate: func(marker map[string]any) {
				state := marker["state"].(map[string]any)
				transition := state["last_transition"].(map[string]any)
				transition["idempotency_key"] = "conflicting-idempotency-key"
				marker["state_sha256"] = sha256HexForTest(mustJSON(t, state))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 1)
			if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(journalPath, 0o444); err != nil {
				t.Fatal(err)
			}
			store := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{})
			_, err := store.Update(1, runtime.Mutation{
				EventID: "evt-coherence", TransitionID: "TR-COHERENCE", Event: "test_transition",
				Actor: "orchestrator", IdempotencyKey: "runtime:coherence:1",
			})
			if err == nil {
				t.Fatal("expected injected journal append failure")
			}
			if err := os.Chmod(journalPath, 0o644); err != nil {
				t.Fatal(err)
			}
			markerPath := statePath + ".commit-pending.json"
			marker := readJSONMapRuntimeTest(t, markerPath)
			tt.mutate(marker)
			writeJSONMapRuntimeTest(t, markerPath, marker)
			stateBefore := mustRead(t, statePath)
			journalBefore := []byte{}
			markerBefore := mustRead(t, markerPath)

			_, err = runtime.NewWriter(statePath, filepath.Join(dir, "loop-events.jsonl"), dir, integrityTestValidator{}).Snapshot()
			if err == nil || !strings.Contains(err.Error(), "pending runtime commit") {
				t.Fatalf("recovery error = %v, want coherence failure", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("coherence failure changed state")
			}
			if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
				t.Fatal("coherence failure changed marker")
			}
			if got := mustRead(t, filepath.Join(dir, "loop-events.jsonl")); string(got) != string(journalBefore) {
				t.Fatal("coherence failure changed journal")
			}
		})
	}
}

func TestReadOnlyStoreReconcileFailsBeforeAppend(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	store := testWriter(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID: "evt-reconcile-read-only", TransitionID: "TR-RECONCILE", Event: "test_transition",
		Actor: "orchestrator", IdempotencyKey: "runtime:reconcile-read-only:1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	_, err := runtime.NewStore(statePath, journalPath).Reconcile()
	if !errors.Is(err, runtime.ErrCandidateValidatorRequired) || !strings.Contains(err.Error(), "mutation-capable writer") {
		t.Fatalf("NewStore.Reconcile error = %v, want mutation-capable writer failure", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("read-only Reconcile changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("read-only Reconcile appended journal")
	}
}

func TestApplyMutationRejectsInvalidExistingJournalBeforeStateWrite(t *testing.T) {
	tests := []struct {
		name          string
		journal       map[string]any
		stateSequence int
		stateEventID  string
		mutationID    string
	}{
		{
			name:          "duplicate event id",
			journal:       continuityTestEvent("loop-test", "evt-duplicate", 1, 0, 1),
			stateSequence: 1,
			stateEventID:  "evt-duplicate",
			mutationID:    "evt-duplicate",
		},
		{
			name:          "sequence gap",
			journal:       continuityTestEvent("loop-test", "evt-gap", 2, 1, 2),
			stateSequence: 2,
			stateEventID:  "evt-gap",
			mutationID:    "evt-new-gap",
		},
		{
			name:          "state cursor mismatch",
			journal:       continuityTestEvent("loop-test", "evt-tail", 1, 0, 1),
			stateSequence: 2,
			stateEventID:  "evt-state-tail",
			mutationID:    "evt-new-mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 1)
			state := readJSONMapRuntimeTest(t, statePath)
			state["journal"].(map[string]any)["last_sequence"] = tt.stateSequence
			state["journal"].(map[string]any)["last_event_id"] = tt.stateEventID
			writeJSONMapRuntimeTest(t, statePath, state)
			if err := os.WriteFile(journalPath, jsonLineForTest(tt.journal), 0o644); err != nil {
				t.Fatal(err)
			}
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)

			_, err := testWriter(statePath, journalPath).Update(1, runtime.Mutation{
				EventID: tt.mutationID, TransitionID: "TR-JOURNAL-PREFLIGHT", Event: "test_transition",
				Actor: "orchestrator", IdempotencyKey: "runtime:journal-preflight:" + tt.name,
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "journal") {
				t.Fatalf("Update error = %v, want journal preflight rejection", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("invalid existing journal changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("invalid existing journal changed journal")
			}
		})
	}
}

func TestStoreReconcileUsesDurableCommitMarkerForAppendRecovery(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	history := prepareMissingReconcileTarget(t, statePath, journalPath)
	store := testWriter(statePath, journalPath)
	if err := os.Chmod(journalPath, 0o444); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)

	if reconciled, err := store.Reconcile(); err == nil || reconciled {
		t.Fatalf("Reconcile result=(%v,%v), want append failure with pending marker", reconciled, err)
	}
	markerPath := statePath + ".commit-pending.json"
	markerBeforeRecovery := mustRead(t, markerPath)
	if len(markerBeforeRecovery) == 0 {
		t.Fatal("Reconcile append failure did not leave durable commit marker")
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("Reconcile append failure changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("Reconcile append failure changed journal")
	}

	if err := os.Chmod(journalPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot(); err != nil {
		t.Fatalf("writer recovery of reconcile marker: %v", err)
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reconcile commit marker remains after recovery: %v", err)
	}
	data := mustRead(t, journalPath)
	if !strings.Contains(string(data), "evt-reconcile") || strings.Count(string(data), "evt-reconcile") != 1 {
		t.Fatalf("recovered journal = %q, want one reconciled event", data)
	}
	if !strings.HasPrefix(string(data), string(history)) {
		t.Fatalf("recovered journal lost historical prefix: %q", data)
	}
}

func TestRolloverRecoveryRejectsMarkerCrossBindingBeforeWrites(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "record runtime differs from source runtime",
			mutate: func(marker map[string]any) {
				marker["record"].(map[string]any)["runtime_id"] = "loop-other"
			},
		},
		{
			name: "record revision differs from source revision",
			mutate: func(marker map[string]any) {
				record := marker["record"].(map[string]any)
				record["revision"] = record["revision"].(float64) + 1
			},
		},
		{
			name: "archive state hash differs from source hash",
			mutate: func(marker map[string]any) {
				marker["record"].(map[string]any)["archive_state_sha256"] = strings.Repeat("0", 64)
			},
		},
		{
			name: "archive journal hash differs from source hash",
			mutate: func(marker map[string]any) {
				marker["record"].(map[string]any)["archive_journal_sha256"] = strings.Repeat("0", 64)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 7)
			writePendingRolloverMarker(t, statePath, dir, freshInactiveState(t))
			markerPath := statePath + ".rollover-pending.json"
			marker := readJSONMapRuntimeTest(t, markerPath)
			tt.mutate(marker)
			writeJSONMapRuntimeTest(t, markerPath, marker)
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)
			markerBefore := mustRead(t, markerPath)

			_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "marker") {
				t.Fatalf("cross-binding recovery error = %v, want marker cross-binding diagnostic", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("cross-binding failure changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("cross-binding failure changed journal")
			}
			if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
				t.Fatal("cross-binding failure changed marker")
			}
		})
	}
}

func TestWriterRejectsArchivedJournalBeforeRevisionMismatch(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	writePendingRolloverMarker(t, statePath, dir, freshInactiveState(t))
	markerPath := statePath + ".rollover-pending.json"
	marker := readJSONMapRuntimeTest(t, markerPath)
	record := marker["record"].(map[string]any)
	archiveDir := record["archive_dir"].(string)
	archiveJournal := mustRead(t, filepath.Join(archiveDir, "loop-events.jsonl"))
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(archiveJournal)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, event)
	}
	lines[0]["before_revision"] = float64(1)
	var mutatedJournal []byte
	for _, event := range lines {
		mutatedJournal = append(mutatedJournal, jsonLineForTest(event)...)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-events.jsonl"), mutatedJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, mutatedJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	marker["source_journal_sha256"] = sha256HexForTest(mutatedJournal)
	record["archive_journal_sha256"] = sha256HexForTest(mutatedJournal)
	writeJSONMapRuntimeTest(t, markerPath, marker)
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	markerBefore := mustRead(t, markerPath)

	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "before_revision") {
		t.Fatalf("archive before_revision error = %v, want before_revision diagnostic", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("archive before_revision failure changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("archive before_revision failure changed journal")
	}
	if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
		t.Fatal("archive before_revision failure changed marker")
	}
}

func TestWriterRejectsValidatorThatOnlyRejectsEmptyProbe(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)
	store := runtime.NewWriter(statePath, journalPath, dir, emptyOnlyValidator{})
	_, err := store.Update(1, runtime.Mutation{
		EventID: "evt-invalid-validator", TransitionID: "TR-INVALID-VALIDATOR", Event: "test_transition",
		Actor: "orchestrator", IdempotencyKey: "runtime:invalid-validator:1",
		Apply: func(state map[string]any) error {
			state["lifecycle"].(map[string]any)["phase"] = "invalid_semantic_phase"
			return nil
		},
	})
	if err == nil || (!strings.Contains(err.Error(), "validator") && !strings.Contains(err.Error(), "semantic")) {
		t.Fatalf("Update error = %v, want validator or built-in semantic failure", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("invalid validator path changed state")
	}
}

func TestWriterRejectsDifferentSchemaValidSemanticInvalidStateWithPermissiveValidator(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)

	// This validator rejects the constructor's empty probe and the historical
	// fixed probe, but deliberately accepts another schema-valid invalid phase.
	// Runtime must own enough semantic validation that this adapter cannot turn
	// an invalid lifecycle candidate into a durable commit.
	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Update(1, runtime.Mutation{
		EventID:        "evt-runtime-core-semantic",
		TransitionID:   "TR-RUNTIME-CORE-SEMANTIC",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:core-semantic:1",
		Apply: func(state map[string]any) error {
			state["lifecycle"].(map[string]any)["phase"] = "another_invalid_phase"
			return nil
		},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "semantic") {
		t.Fatalf("Update error = %v, want built-in semantic rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("schema-valid semantic-invalid candidate changed state")
	}
	if got := mustRead(t, journalPath); len(got) != 0 {
		t.Fatal("schema-valid semantic-invalid candidate changed journal")
	}
}

func TestWriterRejectsMutationRuntimeIDDifferentFromState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)

	_, err := testWriter(statePath, journalPath).Update(1, runtime.Mutation{
		EventID:        "evt-cross-runtime",
		TransitionID:   "TR-CROSS-RUNTIME",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:cross-runtime:1",
		RuntimeID:      "loop-other-runtime",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "runtime_id") {
		t.Fatalf("Update error = %v, want runtime_id mismatch", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("cross-runtime mutation changed state")
	}
	if got := mustRead(t, journalPath); len(got) != 0 {
		t.Fatal("cross-runtime mutation changed journal")
	}
}

func TestCommitRecoveryRejectsExistingJournalFromDifferentRuntime(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	firstEvent := validArchiveJournal(t, "loop-test", 1, "evt-existing-runtime")
	state := readJSONMapRuntimeTest(t, statePath)
	state["revision"] = 1
	state["journal"].(map[string]any)["last_sequence"] = 1
	state["journal"].(map[string]any)["last_event_id"] = "evt-existing-runtime"
	writeJSONMapRuntimeTest(t, statePath, state)
	if err := os.WriteFile(journalPath, firstEvent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(journalPath, 0o444); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(statePath, journalPath)
	if _, err := writer.Update(1, runtime.Mutation{
		EventID:        "evt-pending-runtime",
		TransitionID:   "TR-PENDING-RUNTIME",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:pending-runtime:1",
	}); err == nil {
		t.Fatal("expected journal append failure")
	}
	if err := os.Chmod(journalPath, 0o644); err != nil {
		t.Fatal(err)
	}

	foreign := continuityTestEvent("loop-foreign-runtime", "evt-existing-runtime", 1, 0, 1)
	if err := os.WriteFile(journalPath, jsonLineForTest(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "runtime_id") {
		t.Fatalf("recovery error = %v, want existing journal runtime_id rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("cross-runtime journal recovery changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("cross-runtime journal recovery changed journal")
	}
}

func TestRecoverPendingOperationsCompletesValidCommitBeforeArtifactRecovery(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(journalPath, 0o444); err != nil {
		t.Fatal(err)
	}
	writer := testWriter(statePath, journalPath)
	if _, err := writer.Update(1, runtime.Mutation{
		EventID:        "evt-valid-pending-precedence",
		TransitionID:   "TR-VALID-PENDING",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:valid-pending:1",
	}); err == nil {
		t.Fatal("expected journal append interruption")
	}
	if err := os.Chmod(journalPath, 0o644); err != nil {
		t.Fatal(err)
	}

	completed, err := writer.RecoverPendingOperations()
	if err != nil {
		t.Fatalf("RecoverPendingOperations() error = %v", err)
	}
	if !completed {
		t.Fatal("valid pending commit was not selected before artifact recovery")
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("commit marker remains after exact recovery: %v", err)
	}
	journal := mustRead(t, journalPath)
	if !bytes.Contains(journal, []byte("evt-valid-pending-precedence")) {
		t.Fatalf("recovered journal lacks pending event: %s", journal)
	}
}

func TestRefreshFingerprintsRejectsInconsistentStateJournalPair(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	documentPath := filepath.Join(dir, "document.md")
	if err := os.WriteFile(documentPath, []byte("refresh candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeState(t, statePath, 1)
	state := readJSONMapRuntimeTest(t, statePath)
	state["documents"] = []any{map[string]any{
		"id": "DOC-INCONSISTENT", "kind": "task", "path": "document.md", "version": "v1",
		"sha256": "stale", "status": "locked", "generation": 1,
	}}
	state["journal"].(map[string]any)["last_sequence"] = 1
	state["journal"].(map[string]any)["last_event_id"] = "evt-missing-tail"
	writeJSONMapRuntimeTest(t, statePath, state)
	stateBefore := mustRead(t, statePath)

	_, err := testWriter(statePath, journalPath).RefreshFingerprints(dir)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "journal") {
		t.Fatalf("RefreshFingerprints error = %v, want state/journal coherence rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("inconsistent state/journal pair was fingerprint-refreshed")
	}
}

func TestPendingFingerprintRecoveryRejectsInconsistentStateJournalPair(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	documentPath := filepath.Join(dir, "document.md")
	if err := os.WriteFile(documentPath, []byte("pending refresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeState(t, statePath, 1)
	state := readJSONMapRuntimeTest(t, statePath)
	state["documents"] = []any{map[string]any{
		"id": "DOC-PENDING-INCONSISTENT", "kind": "task", "path": "document.md", "version": "v1",
		"sha256": "stale", "status": "locked", "generation": 1,
	}}
	state["journal"].(map[string]any)["last_sequence"] = 1
	state["journal"].(map[string]any)["last_event_id"] = "evt-missing-tail"
	writeJSONMapRuntimeTest(t, statePath, state)
	previousState := mustRead(t, statePath)
	previousHash := sha256HexForTest(previousState)

	pending := readJSONMapRuntimeTest(t, statePath)
	pending["documents"].([]any)[0].(map[string]any)["sha256"] = sha256HexForTest([]byte("pending refresh\n"))
	pendingBytes := mustJSON(t, pending)
	marker := map[string]any{
		"schema_version":        "1.0.0",
		"previous_state_sha256": previousHash,
		"previous_revision":     1,
		"state_sha256":          sha256HexForTest(pendingBytes),
		"state":                 pending,
	}
	markerPath := statePath + ".fingerprint-pending.json"
	writeJSONMapRuntimeTest(t, markerPath, marker)
	markerBefore := mustRead(t, markerPath)

	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "journal") {
		t.Fatalf("fingerprint recovery error = %v, want state/journal coherence rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(previousState) {
		t.Fatal("inconsistent state/journal pair changed during fingerprint recovery")
	}
	if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
		t.Fatal("fingerprint marker changed after coherence rejection")
	}
}

func TestCommitAppendFailureLeavesRecoverableMarkerAndWriterFinishesPair(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	beforeEvent := validArchiveJournal(t, "loop-test", 1, "evt-append-before")
	state := readJSONMapRuntimeTest(t, statePath)
	state["journal"].(map[string]any)["last_sequence"] = 1
	state["journal"].(map[string]any)["last_event_id"] = "evt-append-before"
	writeJSONMapRuntimeTest(t, statePath, state)
	if err := os.WriteFile(journalPath, beforeEvent, 0o644); err != nil {
		t.Fatal(err)
	}
	journalBeforeFailure := mustRead(t, journalPath)
	if err := os.Chmod(journalPath, 0o444); err != nil {
		t.Fatal(err)
	}

	store := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{})
	_, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-append-failure",
		TransitionID:   "TR-APPEND-FAILURE",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:append-failure:1",
	})
	if err == nil {
		t.Fatal("expected journal append failure")
	}
	if err := os.Chmod(journalPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); err != nil {
		t.Fatalf("append failure must leave commit marker: %v", err)
	}
	stateBeforeRead := mustRead(t, statePath)
	markerBeforeRead := mustRead(t, statePath+".commit-pending.json")
	if _, err := runtime.NewStore(statePath, journalPath).Snapshot(); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("read-only Snapshot error = %v, want pending commit diagnostic", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBeforeRead) {
		t.Fatal("read-only Snapshot changed state during pending commit")
	}
	if got := mustRead(t, statePath+".commit-pending.json"); string(got) != string(markerBeforeRead) {
		t.Fatal("read-only Snapshot changed commit marker")
	}
	stateAfterFailure := mustRead(t, statePath)
	var stateAfter map[string]any
	if err := json.Unmarshal(stateAfterFailure, &stateAfter); err != nil {
		t.Fatal(err)
	}
	if stateAfter["revision"] != float64(2) {
		t.Fatalf("state revision after append failure = %v, want 2", stateAfter["revision"])
	}

	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journalBeforeFailure, 0o644); err != nil {
		t.Fatal(err)
	}
	recovered, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err != nil {
		t.Fatalf("writer marker recovery: %v", err)
	}
	if recovered.Revision != 2 {
		t.Fatalf("recovered revision = %d, want 2", recovered.Revision)
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("commit marker remains after recovery: %v", err)
	}
	journal := mustRead(t, journalPath)
	if !strings.Contains(string(journal), "evt-append-failure") {
		t.Fatalf("recovered journal does not contain committed event: %q", journal)
	}
	if !strings.Contains(string(journal), `"after_revision":2`) {
		t.Fatalf("recovered journal does not anchor revision 2: %q", journal)
	}
	if _, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot(); err != nil {
		t.Fatalf("idempotent second writer recovery: %v", err)
	}
	if got := strings.Count(string(mustRead(t, journalPath)), "evt-append-failure"); got != 1 {
		t.Fatalf("second recovery duplicated journal event: count=%d", got)
	}
}

func TestCommitRecoveryRejectsConflictingExistingJournalEvent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(journalPath, 0o444); err != nil {
		t.Fatal(err)
	}
	store := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{})
	if _, err := store.Update(1, runtime.Mutation{
		EventID: "evt-conflicting-journal", TransitionID: "TR-CONFLICT", Event: "test_transition",
		Actor: "orchestrator", IdempotencyKey: "runtime:conflict:1",
	}); err == nil {
		t.Fatal("expected injected journal append failure")
	}
	if err := os.Chmod(journalPath, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := readJSONMapRuntimeTest(t, statePath+".commit-pending.json")
	conflict := marker["journal_event"].(map[string]any)
	conflict["message"] = "conflicting event body"
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	journal := jsonLineForTest(conflict)
	if err := os.WriteFile(journalPath, journal, 0o644); err != nil {
		t.Fatal(err)
	}
	stateBefore := mustRead(t, statePath)
	markerBefore := mustRead(t, statePath+".commit-pending.json")
	journalBefore := mustRead(t, journalPath)
	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(err.Error(), "conflicts with journal") {
		t.Fatalf("conflicting recovery error = %v, want conflict diagnostic", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("conflicting recovery changed state")
	}
	if got := mustRead(t, statePath+".commit-pending.json"); string(got) != string(markerBefore) {
		t.Fatal("conflicting recovery changed marker")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("conflicting recovery changed journal")
	}
}

func TestCommitRecoveryRejectsNonContinuousJournalTail(t *testing.T) {
	tests := []struct {
		name    string
		journal func(t *testing.T, pending map[string]any) []byte
	}{
		{
			name: "pending event is in the middle",
			journal: func(t *testing.T, pending map[string]any) []byte {
				event := pending["journal_event"].(map[string]any)
				journal := jsonLineForTest(continuityTestEvent("loop-test", "evt-before", 1, 0, 1))
				journal = append(journal, jsonLineForTest(event)...)
				return append(journal, jsonLineForTest(continuityTestEvent("loop-test", "evt-after", 3, 2, 3))...)
			},
		},
		{
			name: "journal sequence has a gap",
			journal: func(t *testing.T, pending map[string]any) []byte {
				journal := jsonLineForTest(continuityTestEvent("loop-test", "evt-before", 1, 0, 1))
				return append(journal, jsonLineForTest(continuityTestEvent("loop-test", "evt-gap", 3, 2, 3))...)
			},
		},
		{
			name: "journal tail is already another event",
			journal: func(t *testing.T, pending map[string]any) []byte {
				journal := jsonLineForTest(continuityTestEvent("loop-test", "evt-before", 1, 0, 1))
				return append(journal, jsonLineForTest(continuityTestEvent("loop-test", "evt-tail", 2, 1, 2))...)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 1)
			if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(journalPath, 0o444); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Update(1, runtime.Mutation{
				EventID: "evt-continuity", TransitionID: "TR-CONTINUITY", Event: "test_transition",
				Actor: "orchestrator", IdempotencyKey: "runtime:continuity:1",
			}); err == nil {
				t.Fatal("expected injected journal append failure")
			}
			if err := os.Chmod(journalPath, 0o644); err != nil {
				t.Fatal(err)
			}
			pending := readJSONMapRuntimeTest(t, statePath+".commit-pending.json")
			if err := os.WriteFile(journalPath, tt.journal(t, pending), 0o644); err != nil {
				t.Fatal(err)
			}
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)
			markerBefore := mustRead(t, statePath+".commit-pending.json")

			_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
			if err == nil || !strings.Contains(err.Error(), "journal") {
				t.Fatalf("continuity recovery error = %v, want fail-closed journal diagnostic", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("continuity failure changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("continuity failure changed journal")
			}
			if got := mustRead(t, statePath+".commit-pending.json"); string(got) != string(markerBefore) {
				t.Fatal("continuity failure changed marker")
			}
		})
	}
}

func TestNewJournalEventCarriesIdempotencyKey(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	const idempotencyKey = "runtime:new-event-idempotency:1"
	if _, err := testWriter(statePath, journalPath).Update(1, runtime.Mutation{
		EventID: "evt-new-event-idempotency", TransitionID: "TR-IDEMPOTENCY", Event: "test_transition",
		Actor: "orchestrator", IdempotencyKey: idempotencyKey,
	}); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	data := mustRead(t, journalPath)
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	if got := event["idempotency_key"]; got != idempotencyKey {
		t.Fatalf("journal idempotency_key = %#v, want %q", got, idempotencyKey)
	}
}

func TestRetainLastTransitionRecoveryRequiresIdempotencyCoherence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, marker map[string]any)
	}{
		{
			name: "marker idempotency",
			mutate: func(_ *testing.T, marker map[string]any) {
				marker["idempotency_key"] = "runtime:tampered-marker"
			},
		},
		{
			name: "journal idempotency",
			mutate: func(t *testing.T, marker map[string]any) {
				event := marker["journal_event"].(map[string]any)
				event["idempotency_key"] = "runtime:tampered-journal"
				marker["journal_event_sha256"] = sha256HexForTest(jsonLineForTest(event))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 1)
			writer := testWriter(statePath, journalPath)
			if _, err := writer.Update(1, runtime.Mutation{
				EventID: "evt-retain-before", TransitionID: "TR-RETAIN-BEFORE", Event: "test_transition",
				Actor: "orchestrator", IdempotencyKey: "runtime:retain:before",
			}); err != nil {
				t.Fatal(err)
			}
			journalBeforeFailure := mustRead(t, journalPath)
			if err := os.Chmod(journalPath, 0o444); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Update(2, runtime.Mutation{
				EventID: "evt-retain-recovery", TransitionID: "TR-RETAIN-RECOVERY", Event: "test_transition",
				Actor: "orchestrator", IdempotencyKey: "runtime:retain:recovery", RetainLastTransition: true,
			}); err == nil {
				t.Fatal("expected injected journal append failure")
			}
			if err := os.Chmod(journalPath, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(journalPath); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, journalBeforeFailure, 0o644); err != nil {
				t.Fatal(err)
			}
			markerPath := statePath + ".commit-pending.json"
			marker := readJSONMapRuntimeTest(t, markerPath)
			tt.mutate(t, marker)
			writeJSONMapRuntimeTest(t, markerPath, marker)
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)
			markerBefore := mustRead(t, markerPath)

			_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "idempotency") {
				t.Fatalf("retain recovery error = %v, want idempotency coherence diagnostic", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("idempotency failure changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("idempotency failure changed journal")
			}
			if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
				t.Fatal("idempotency failure changed marker")
			}
		})
	}
}

func TestWriterRejectsRolloverMarkerWithoutSourceBinding(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 7)
	if err := os.WriteFile(journalPath, []byte("old-journal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePendingRolloverMarker(t, statePath, dir, freshInactiveState(t))
	markerPath := statePath + ".rollover-pending.json"
	marker := readJSONMapRuntimeTest(t, markerPath)
	for _, field := range []string{"source_state_sha256", "source_journal_sha256", "source_runtime_id", "source_revision"} {
		delete(marker, field)
	}
	writeJSONMapRuntimeTest(t, markerPath, marker)
	stateBefore := mustRead(t, statePath)
	journalBefore := mustRead(t, journalPath)
	markerBefore := mustRead(t, markerPath)

	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "source") {
		t.Fatalf("source-less rollover recovery error = %v, want source-binding failure", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("source-less rollover changed state")
	}
	if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("source-less rollover changed journal")
	}
	if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
		t.Fatal("source-less rollover changed marker")
	}
}

func TestWriterRejectsRolloverSourceOrApprovalMismatch(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		mutate func(map[string]any)
	}{
		{
			name: "source hash",
			want: "marker",
			mutate: func(marker map[string]any) {
				marker["source_state_sha256"] = strings.Repeat("0", 64)
			},
		},
		{
			name: "approval evidence",
			want: "approval",
			mutate: func(marker map[string]any) {
				marker["approval"].(map[string]any)["evidence_id"] = "ev-forged"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 7)
			if err := os.WriteFile(journalPath, []byte("old-journal\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			writePendingRolloverMarker(t, statePath, dir, freshInactiveState(t))
			markerPath := statePath + ".rollover-pending.json"
			marker := readJSONMapRuntimeTest(t, markerPath)
			tt.mutate(marker)
			writeJSONMapRuntimeTest(t, markerPath, marker)
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)
			markerBefore := mustRead(t, markerPath)

			_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("rollover mismatch error = %v, want %s diagnostic", err, tt.want)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("rollover mismatch changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("rollover mismatch changed journal")
			}
			if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
				t.Fatal("rollover mismatch changed marker")
			}
		})
	}
}

func TestWriterRejectsRolloverArchiveRecordRuntimeOrRevisionMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "runtime_id",
			mutate: func(marker map[string]any) {
				marker["record"].(map[string]any)["runtime_id"] = "loop-other"
			},
		},
		{
			name: "revision",
			mutate: func(marker map[string]any) {
				record := marker["record"].(map[string]any)
				record["revision"] = float64(record["revision"].(float64) + 1)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 7)
			if err := os.WriteFile(journalPath, []byte("old-journal\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			writePendingRolloverMarker(t, statePath, dir, freshInactiveState(t))
			markerPath := statePath + ".rollover-pending.json"
			marker := readJSONMapRuntimeTest(t, markerPath)
			tt.mutate(marker)
			writeJSONMapRuntimeTest(t, markerPath, marker)
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)
			markerBefore := mustRead(t, markerPath)

			_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).Snapshot()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "marker") {
				t.Fatalf("archive coherence error = %v, want marker cross-binding diagnostic", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("archive coherence mismatch changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("archive coherence mismatch changed journal")
			}
			if got := mustRead(t, markerPath); string(got) != string(markerBefore) {
				t.Fatal("archive coherence mismatch changed marker")
			}
		})
	}
}

func TestWriterRejectsNoopCandidateValidator(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	store := runtime.NewWriter(statePath, journalPath, dir, noopIntegrityValidator{})
	_, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-noop-validator",
		TransitionID:   "TR-NOOP-VALIDATOR",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:noop-validator:1",
	})
	if err == nil || !strings.Contains(err.Error(), "validator") {
		t.Fatalf("Update error = %v, want validator capability rejection", err)
	}
}

func TestRefreshFingerprintsRejectsValidatorThatAcceptsInvalidCandidate(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	documentPath := filepath.Join(dir, "document.md")
	if err := os.WriteFile(documentPath, []byte("updated document\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeState(t, statePath, 1)
	state := readJSONMapRuntimeTest(t, statePath)
	state["documents"] = []any{map[string]any{
		"id": "DOC-REFRESH", "kind": "task", "path": "document.md", "version": "v1",
		"sha256": "stale", "status": "locked", "generation": 1,
	}}
	writeJSONMapRuntimeTest(t, statePath, state)
	stateBefore := mustRead(t, statePath)

	// This validator rejects only the constructor's empty probe. It accepts
	// schema-valid but semantically invalid candidates and must not be trusted
	// by a fingerprint refresh.
	store := runtime.NewWriter(statePath, journalPath, dir, emptyOnlyValidator{})
	if _, err := store.RefreshFingerprints(dir); err == nil || !strings.Contains(strings.ToLower(err.Error()), "validator") {
		t.Fatalf("RefreshFingerprints error = %v, want validator rejection", err)
	}
	if got := mustRead(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("invalid candidate was persisted by RefreshFingerprints")
	}
}

func TestApplyMutationRejectsStateJournalLastEventIDMismatch(t *testing.T) {
	tests := []struct {
		name          string
		stateSequence int
		stateEventID  any
		journal       []byte
	}{
		{
			name:          "non-empty journal tail",
			stateSequence: 1,
			stateEventID:  "evt-state-tail",
			journal:       jsonLineForTest(continuityTestEvent("loop-test", "evt-journal-tail", 1, 0, 1)),
		},
		{
			name:          "empty journal has non-null state cursor",
			stateSequence: 0,
			stateEventID:  "evt-forged-empty-tail",
			journal:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 1)
			state := readJSONMapRuntimeTest(t, statePath)
			state["journal"].(map[string]any)["last_sequence"] = tt.stateSequence
			state["journal"].(map[string]any)["last_event_id"] = tt.stateEventID
			writeJSONMapRuntimeTest(t, statePath, state)
			if err := os.WriteFile(journalPath, tt.journal, 0o644); err != nil {
				t.Fatal(err)
			}
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)

			_, err := testWriter(statePath, journalPath).Update(1, runtime.Mutation{
				EventID: "evt-new-cursor-check", TransitionID: "TR-CURSOR-CHECK", Event: "test_transition",
				Actor: "orchestrator", IdempotencyKey: "runtime:cursor-check:" + tt.name,
			})
			if err == nil || !strings.Contains(err.Error(), "last_event_id") {
				t.Fatalf("Update error = %v, want last_event_id mismatch rejection", err)
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("last_event_id mismatch changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("last_event_id mismatch changed journal")
			}
		})
	}
}

func TestApplyMutationRejectsApplyBoundaryTampering(t *testing.T) {
	tests := []struct {
		name  string
		apply func(map[string]any)
	}{
		{
			name: "runtime_id",
			apply: func(state map[string]any) {
				state["runtime_id"] = "loop-forged"
			},
		},
		{
			name: "journal last_sequence",
			apply: func(state map[string]any) {
				state["journal"].(map[string]any)["last_sequence"] = 99
			},
		},
		{
			name: "journal last_event_id",
			apply: func(state map[string]any) {
				state["journal"].(map[string]any)["last_event_id"] = "evt-forged"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "loop-state.json")
			journalPath := filepath.Join(dir, "loop-events.jsonl")
			writeState(t, statePath, 1)
			if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			stateBefore := mustRead(t, statePath)
			journalBefore := mustRead(t, journalPath)
			markerPath := statePath + ".commit-pending.json"

			_, err := testWriter(statePath, journalPath).Update(1, runtime.Mutation{
				EventID:        "evt-apply-boundary-" + tt.name,
				TransitionID:   "TR-APPLY-BOUNDARY",
				Event:          "test_transition",
				Actor:          "orchestrator",
				IdempotencyKey: "runtime:apply-boundary:" + tt.name,
				Apply: func(state map[string]any) error {
					tt.apply(state)
					return nil
				},
			})
			if err == nil {
				t.Fatal("Mutation.Apply tampering was accepted")
			}
			if got := mustRead(t, statePath); string(got) != string(stateBefore) {
				t.Fatal("Mutation.Apply tampering changed state")
			}
			if got := mustRead(t, journalPath); string(got) != string(journalBefore) {
				t.Fatal("Mutation.Apply tampering changed journal")
			}
			if _, statErr := os.Stat(markerPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("Mutation.Apply tampering left commit marker, stat error = %v", statErr)
			}
		})
	}
}

type integrityTestValidator struct{}

func (integrityTestValidator) ValidateCandidate(_ string, state map[string]any) error {
	if state == nil || state["runtime_id"] == nil {
		return errors.New("test validator rejects empty probe")
	}
	if lifecycle, ok := state["lifecycle"].(map[string]any); ok && lifecycle["phase"] == "invalid_semantic_phase" {
		return errors.New("test validator rejects semantic probe")
	}
	return nil
}

type noopIntegrityValidator struct{}

func (noopIntegrityValidator) ValidateCandidate(_ string, _ map[string]any) error { return nil }

type emptyOnlyValidator struct{}

func (emptyOnlyValidator) ValidateCandidate(_ string, state map[string]any) error {
	if len(state) == 0 {
		return errors.New("empty probe rejected")
	}
	return nil
}

func freshInactiveState(t *testing.T) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-inactive"
	state["revision"] = 0
	state["lifecycle"] = map[string]any{"state": "inactive", "phase": nil, "phase_revision": 0}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["last_transition"] = nil
	state["bound_req"] = nil
	state["pause"] = nil
	state["change"] = nil
	state["authorization"] = map[string]any{"mode": "none", "actor": "", "command": "", "occurred_at": "1970-01-01T00:00:00Z"}
	state["baseline"] = map[string]any{"generation": 0, "captured_at": nil}
	state["review"] = map[string]any{"round": 0, "clean_round": nil}
	state["documents"] = []any{}
	state["evidence"] = []any{}
	state["blockers"] = []any{}
	state["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}}
	return state
}

func writePendingRolloverMarker(t *testing.T, statePath, dir string, fresh map[string]any) {
	t.Helper()
	sourceRuntimeID, sourceRevision, sourceState, sourceJournal := prepareRolloverSourcePair(t, statePath)
	archiveDir := filepath.Join(dir, "archive", "loop-test-r1")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveState := sourceState
	archiveJournal := sourceJournal
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-state.json"), archiveState, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-events.jsonl"), archiveJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{
		"schema_version": "1.0.0",
		"fresh_state":    fresh,
		"record": map[string]any{
			"archive_dir": archiveDir, "runtime_id": sourceRuntimeID, "revision": sourceRevision,
			"archive_state_sha256": sha256HexForTest(archiveState), "archive_journal_sha256": sha256HexForTest(archiveJournal),
		},
		"approval":              map[string]any{"approved_by": "release-owner", "evidence_id": "ev-approval"},
		"source_state_sha256":   sha256HexForTest(sourceState),
		"source_journal_sha256": sha256HexForTest(archiveJournal),
		"source_runtime_id":     sourceRuntimeID,
		"source_revision":       sourceRevision,
	}
	if err := os.WriteFile(statePath+".rollover-pending.json", mustJSON(t, marker), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePendingRolloverMarkerWithArchive(t *testing.T, statePath, dir string, fresh map[string]any, archiveJournal []byte) string {
	t.Helper()
	sourceRuntimeID, sourceRevision, sourceState, _ := prepareRolloverSourcePair(t, statePath)
	archiveDir := filepath.Join(dir, "archive", "loop-test-r1")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveState := sourceState
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-state.json"), archiveState, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-events.jsonl"), archiveJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(filepath.Dir(statePath), "loop-events.jsonl")
	if err := os.WriteFile(journalPath, archiveJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	markerPath := statePath + ".rollover-pending.json"
	writeJSONMapRuntimeTest(t, markerPath, map[string]any{
		"schema_version": "1.0.0", "fresh_state": fresh,
		"record": map[string]any{
			"archive_dir": archiveDir, "runtime_id": sourceRuntimeID, "revision": sourceRevision,
			"archive_state_sha256": sha256HexForTest(archiveState), "archive_journal_sha256": sha256HexForTest(archiveJournal),
		},
		"approval":              map[string]any{"approved_by": "release-owner", "evidence_id": "ev-approval"},
		"source_state_sha256":   sha256HexForTest(sourceState),
		"source_journal_sha256": sha256HexForTest(archiveJournal),
		"source_runtime_id":     sourceRuntimeID,
		"source_revision":       sourceRevision,
	})
	return markerPath
}

func freshInactiveArchiveState(t *testing.T, runtimeID string, revision int, lastEventID string) []byte {
	t.Helper()
	data := freshInactiveState(t)
	data["runtime_id"] = runtimeID
	data["revision"] = revision
	data["journal"].(map[string]any)["last_sequence"] = revision
	data["journal"].(map[string]any)["last_event_id"] = lastEventID
	return mustJSON(t, data)
}

func validArchiveJournal(t *testing.T, runtimeID string, revision int, eventID string) []byte {
	t.Helper()
	var journal []byte
	for sequence := 1; sequence <= revision; sequence++ {
		currentEventID := fmt.Sprintf("%s-%d", eventID, sequence)
		if sequence == revision {
			currentEventID = eventID
		}
		event := continuityTestEvent(runtimeID, currentEventID, sequence, sequence-1, sequence)
		event["actor"] = map[string]any{"type": "system", "id": "archive-test"}
		event["request_id"] = "archive-test"
		event["from"] = map[string]any{"state": "inactive", "phase": nil}
		event["to"] = map[string]any{"state": "inactive", "phase": nil}
		event["message"] = "Archived test event."
		journal = append(journal, jsonLineForTest(event)...)
	}
	return journal
}

func prepareRolloverSourcePair(t *testing.T, statePath string) (string, int, []byte, []byte) {
	t.Helper()
	state := readJSONMapRuntimeTest(t, statePath)
	runtimeID, ok := state["runtime_id"].(string)
	if !ok || runtimeID == "" {
		t.Fatalf("source runtime_id = %#v", state["runtime_id"])
	}
	revision := int(state["revision"].(float64))
	eventID := sourceJournalEventID(runtimeID, revision)
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": revision}
	state["evidence"] = []any{map[string]any{
		"id": "ev-approval", "kind": "human_decision", "path": "docs/requirements/REQ-099.md",
		"sha256": strings.Repeat("0", 64), "status": "valid", "baseline_generation": 0,
		"review_round": nil, "produced_by": []any{"release-owner"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": nil,
		"scope_refs": []any{fmt.Sprintf("runtime_rollover:%s@%d", runtimeID, revision)},
	}}
	state["journal"].(map[string]any)["last_sequence"] = float64(revision)
	state["journal"].(map[string]any)["last_event_id"] = eventID
	writeJSONMapRuntimeTest(t, statePath, state)
	stateBytes := mustRead(t, statePath)
	journalBytes := validArchiveJournal(t, runtimeID, revision, eventID)
	journalPath := filepath.Join(filepath.Dir(statePath), "loop-events.jsonl")
	if err := os.WriteFile(journalPath, journalBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return runtimeID, revision, stateBytes, journalBytes
}

func sourceJournalEventID(runtimeID string, revision int) string {
	return fmt.Sprintf("evt-source-%s-r%d", runtimeID, revision)
}

func rolloverArchiveState(t *testing.T, runtimeID string, revision int, eventID string) []byte {
	t.Helper()
	state := freshInactiveState(t)
	state["runtime_id"] = runtimeID
	state["revision"] = revision
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": revision}
	state["journal"].(map[string]any)["last_sequence"] = revision
	state["journal"].(map[string]any)["last_event_id"] = eventID
	state["evidence"] = []any{map[string]any{
		"id": "ev-approval", "kind": "human_decision", "path": "docs/requirements/REQ-099.md",
		"sha256": strings.Repeat("0", 64), "status": "valid", "baseline_generation": 0,
		"review_round": nil, "produced_by": []any{"release-owner"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": nil,
		"scope_refs": []any{fmt.Sprintf("runtime_rollover:%s@%d", runtimeID, revision)},
	}}
	return mustJSON(t, state)
}

func continuityTestEvent(runtimeID, eventID string, sequence, beforeRevision, afterRevision int) map[string]any {
	return map[string]any{
		"schema_version": "1.0.0", "runtime_id": runtimeID, "event_id": eventID,
		"sequence": sequence, "event": "milestone_refreshed", "outcome": "refreshed",
		"actor": map[string]any{"type": "system", "id": "continuity-test"}, "request_id": "continuity-test",
		"baseline_generation": 1, "before_revision": beforeRevision, "after_revision": afterRevision,
		"from": map[string]any{"state": "planning", "phase": nil}, "to": map[string]any{"state": "planning", "phase": nil},
		"evidence_ids": []any{}, "message": "Continuity test event.", "occurred_at": "2026-08-13T00:00:00Z",
	}
}

func prepareMissingReconcileTarget(t *testing.T, statePath, journalPath string) []byte {
	t.Helper()
	writeState(t, statePath, 1)
	state := readJSONMapRuntimeTest(t, statePath)
	state["journal"].(map[string]any)["last_sequence"] = 1
	state["journal"].(map[string]any)["last_event_id"] = "evt-history"
	writeJSONMapRuntimeTest(t, statePath, state)
	history := jsonLineForTest(continuityTestEvent("loop-test", "evt-history", 1, 0, 1))
	if err := os.WriteFile(journalPath, history, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := testWriter(statePath, journalPath).Update(1, runtime.Mutation{
		EventID: "evt-reconcile", TransitionID: "TR-TEST", Event: "test_transition",
		Actor: "orchestrator", IdempotencyKey: "runtime:TR-TEST:1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, history, 0o644); err != nil {
		t.Fatal(err)
	}
	return history
}

func jsonLineForTest(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func readJSONMapRuntimeTest(t *testing.T, path string) map[string]any {
	t.Helper()
	data := mustRead(t, path)
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeJSONMapRuntimeTest(t *testing.T, path string, value map[string]any) {
	t.Helper()
	if err := os.WriteFile(path, mustJSON(t, value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sha256HexForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
