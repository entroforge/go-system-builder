package runtime_test

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// RC-10 observability baseline: missing journal is empty tail, threshold
// is 10000, and over-threshold journals still report the right counts.
func TestJournalNeedsRotation(t *testing.T) {
	t.Run("missing journal is empty tail", func(t *testing.T) {
		needs, count, err := runtime.JournalNeedsRotation(filepath.Join(t.TempDir(), "journal.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if needs || count != 0 {
			t.Fatalf("missing journal must read as empty: needs=%v count=%d", needs, count)
		}
	})

	t.Run("under threshold", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"a\":2}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		needs, count, err := runtime.JournalNeedsRotation(path)
		if err != nil {
			t.Fatal(err)
		}
		if needs || count != 2 {
			t.Fatalf("two-line journal: needs=%v count=%d", needs, count)
		}
	})

	t.Run("over threshold", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		var b strings.Builder
		for i := 0; i < 10001; i++ {
			b.WriteString("{\"a\":1}\n")
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		needs, count, err := runtime.JournalNeedsRotation(path)
		if err != nil {
			t.Fatal(err)
		}
		if !needs || count != 10001 {
			t.Fatalf("10001-line journal: needs=%v count=%d", needs, count)
		}
	})

	t.Run("threshold is 10000", func(t *testing.T) {
		if runtime.JournalRotationThreshold != 10000 {
			t.Fatalf("journal rotation threshold changed: %d", runtime.JournalRotationThreshold)
		}
	})
}

// RC-13 L5 acceptance: extractArchiveSeq must treat non-numeric archive
// names as a non-mergeable sort key (math.MaxInt), not as 0 (which would
// place malformed files at the front of the merge order).
func TestExtractArchiveSeqNonNumericReturnsMaxInt(t *testing.T) {
	if got := runtime.ExtractArchiveSeq("loop-events.jsonl.archive.bad.jsonl"); got != math.MaxInt {
		t.Fatalf("extractArchiveSeq bad-name = %d, want %d (math.MaxInt)", got, math.MaxInt)
	}
	if got := runtime.ExtractArchiveSeq("loop-events.jsonl.archive.42.jsonl"); got != 42 {
		t.Fatalf("extractArchiveSeq good-name = %d, want 42", got)
	}
	if got := runtime.ExtractArchiveSeq("no-archive-marker.jsonl"); got != math.MaxInt {
		t.Fatalf("extractArchiveSeq no-marker = %d, want %d", got, math.MaxInt)
	}
}

// RC-13 L5 acceptance: journalSegmentPaths must filter out non-numeric
// archives so they do not participate in the contiguous merge.
func TestJournalSegmentPathsFiltersNonNumericArchives(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	good50 := journalPath + ".archive.50.jsonl"
	good100 := journalPath + ".archive.100.jsonl"
	bad := journalPath + ".archive.bad.jsonl"
	for _, p := range []string{good50, good100, bad} {
		if err := os.WriteFile(p, []byte("ignored\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := runtime.JournalSegmentPaths(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 valid archives, got %d: %v", len(got), got)
	}
	if filepath.Base(got[0]) != filepath.Base(good50) {
		t.Fatalf("first archive should be sequence 50, got %s", filepath.Base(got[0]))
	}
	if filepath.Base(got[1]) != filepath.Base(good100) {
		t.Fatalf("second archive should be sequence 100, got %s", filepath.Base(got[1]))
	}
	for _, p := range got {
		if strings.Contains(p, "bad") {
			t.Fatalf("non-numeric archive leaked into merge: %s", p)
		}
	}
}

// RC-13 R-H1 acceptance: after a crash between marker write and active
// truncation, recovery must complete the rotation by slicing the active
// journal at pending.ArchivedCount and rewriting only the tail. The
// post-recovery journal must be contiguous across archive + active.
func TestRecoverPendingJournalRotationTruncatesActiveWithArchivedPrefix(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeStateForRotationTest(t, statePath, "loop-test", 100, "evt-arch-100")

	// Build an active journal with sequences 1..100 and an archive file
	// covering 1..99. The "crash" state is the marker written + archive
	// created + active NOT yet truncated.
	var archivedSeq []byte
	var activeSeq []byte
	for i := 1; i <= 99; i++ {
		archivedSeq = append(archivedSeq, rotationEventLine(t, "loop-test", i)...)
	}
	for i := 1; i <= 100; i++ {
		activeSeq = append(activeSeq, rotationEventLine(t, "loop-test", i)...)
	}
	archiveFile := journalPath + ".archive.99.jsonl"
	if err := os.WriteFile(archiveFile, archivedSeq, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, activeSeq, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{
		"schema_version":  "1.0.0",
		"archived_file":   archiveFile,
		"archived_sha256": sha256HexForTest(archivedSeq),
		"archived_count":  99,
		"tail_sequence":   100,
		"tail_event_id":   "evt-arch-100",
		"started_at":      "2026-08-29T00:00:00Z",
	}
	if err := os.WriteFile(statePath+".journal-rotation-pending.json", mustJSON(t, marker), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RecoverPendingOperations(); err != nil {
		t.Fatalf("RecoverPendingOperations: %v", err)
	}
	if _, err := os.Stat(statePath + ".journal-rotation-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rotation marker not cleared: %v", err)
	}
	// After recovery, active journal must contain only the tail event
	// (sequence 100). The archive file is left untouched.
	tail := mustRead(t, journalPath)
	if len(tail) == 0 || !strings.Contains(string(tail), `"sequence":100`) {
		t.Fatalf("active journal did not keep the tail event: %q", tail)
	}
	if strings.Contains(string(tail), `"sequence":99`) {
		t.Fatalf("active journal still contains the archived prefix: %q", tail)
	}
	// Inspecting the merged journal (archive + active) must remain
	// contiguous with TailSequence==100 and no duplicate event_ids.
	segments, err := runtime.JournalSegmentPaths(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 archive after recovery, got %d: %v", len(segments), segments)
	}
}

// RC-13 R-H1 regression: if the process crashes after the atomic active-file
// rewrite but before clearing the marker, recovery must recognize the
// already-truncated tail, validate the archive + tail pair, and clear the
// marker without dropping the tail or attempting a second truncation.
func TestRecoverPendingJournalRotationAcceptsAlreadyTruncatedActiveTail(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeStateForRotationTest(t, statePath, "loop-test", 100, "evt-arch-100")

	var archivedSeq []byte
	for i := 1; i <= 99; i++ {
		archivedSeq = append(archivedSeq, rotationEventLine(t, "loop-test", i)...)
	}
	archiveFile := journalPath + ".archive.99.jsonl"
	if err := os.WriteFile(archiveFile, archivedSeq, 0o644); err != nil {
		t.Fatal(err)
	}
	// This is the post-truncation, pre-marker-clear crash state.
	activeTail := rotationEventLine(t, "loop-test", 100)
	if err := os.WriteFile(journalPath, activeTail, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{
		"schema_version":  "1.0.0",
		"archived_file":   archiveFile,
		"archived_sha256": sha256HexForTest(archivedSeq),
		"archived_count":  99,
		"tail_sequence":   100,
		"tail_event_id":   "evt-arch-100",
		"started_at":      "2026-08-29T00:00:00Z",
	}
	markerPath := statePath + ".journal-rotation-pending.json"
	if err := os.WriteFile(markerPath, mustJSON(t, marker), 0o644); err != nil {
		t.Fatal(err)
	}

	completed, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RecoverPendingOperations()
	if err != nil {
		t.Fatalf("RecoverPendingOperations: %v", err)
	}
	if !completed {
		t.Fatal("already-truncated journal rotation should be reported as completed")
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rotation marker not cleared: %v", err)
	}
	if got := mustRead(t, journalPath); string(got) != string(activeTail) {
		t.Fatalf("active tail changed during idempotent recovery: %q", got)
	}

	completed, err = runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RecoverPendingOperations()
	if err != nil {
		t.Fatalf("second RecoverPendingOperations: %v", err)
	}
	if completed {
		t.Fatal("second recovery without a marker should be a no-op")
	}
}

// RC-13 R-M2 acceptance: when state.journal.last_sequence does not match
// the post-recovery tail, validateStateJournalPair must fail and the
// marker must remain on disk for `runtime reconcile` to inspect.
func TestRecoverPendingJournalRotationRetainsMarkerOnStateJournalMismatch(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	// State declares last_sequence=50 but the active journal tail is
	// sequence 100. Recovery must fail-closed and retain the marker.
	writeStateForRotationTest(t, statePath, "loop-test", 50, "evt-arch-50")

	var archivedSeq []byte
	var activeSeq []byte
	for i := 1; i <= 99; i++ {
		archivedSeq = append(archivedSeq, rotationEventLine(t, "loop-test", i)...)
	}
	for i := 1; i <= 100; i++ {
		activeSeq = append(activeSeq, rotationEventLine(t, "loop-test", i)...)
	}
	archiveFile := journalPath + ".archive.99.jsonl"
	if err := os.WriteFile(archiveFile, archivedSeq, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, activeSeq, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{
		"schema_version":  "1.0.0",
		"archived_file":   archiveFile,
		"archived_sha256": sha256HexForTest(archivedSeq),
		"archived_count":  99,
		"tail_sequence":   100,
		"tail_event_id":   "evt-arch-100",
		"started_at":      "2026-08-29T00:00:00Z",
	}
	if err := os.WriteFile(statePath+".journal-rotation-pending.json", mustJSON(t, marker), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runtime.NewWriter(statePath, journalPath, dir, integrityTestValidator{}).RecoverPendingOperations()
	if err == nil {
		t.Fatal("RecoverPendingOperations must fail-closed on state/journal mismatch")
	}
	if !strings.Contains(err.Error(), "state/journal mismatch") {
		t.Fatalf("error must mention state/journal mismatch: %v", err)
	}
	if _, statErr := os.Stat(statePath + ".journal-rotation-pending.json"); statErr != nil {
		t.Fatalf("rotation marker must be retained on fail-closed: %v", statErr)
	}
}

// RC-13 R-M3 acceptance: knownRecoverySourcePendingPaths must include
// the journal rotation marker so recovery plans can declare its SHA and
// the recovery apply can retire it alongside the other Runtime markers.
func TestKnownRecoverySourcePendingPathsIncludesRotationMarker(t *testing.T) {
	paths := runtime.KnownRecoverySourcePendingPaths("/state/loop-state.json")
	want := "/state/loop-state.json.journal-rotation-pending.json"
	found := false
	for _, p := range paths {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("journal rotation marker missing from known paths: %v", paths)
	}
}

// writeStateForRotationTest writes a minimal runtime state file with the
// given journal cursor. The shape is intentionally minimal — we only
// need runtime_id, revision, and journal fields for the rotation
// recovery to validate state/journal pair.
func writeStateForRotationTest(t *testing.T, path, runtimeID string, lastSequence int, lastEventID string) {
	t.Helper()
	state := map[string]any{
		"schema_version": "1.0.0",
		"runtime_id":     runtimeID,
		"revision":       lastSequence,
		"lifecycle":      map[string]any{"state": "inactive", "phase": nil, "phase_revision": lastSequence},
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": lastSequence,
			"last_event_id": lastEventID,
		},
		"last_transition": nil,
		"bound_req":       nil,
		"pause":           nil,
		"change":          nil,
		"authorization":   map[string]any{"mode": "none", "actor": "", "command": "", "occurred_at": "1970-01-01T00:00:00Z"},
		"baseline":        map[string]any{"generation": 1, "captured_at": "2026-08-29T00:00:00Z"},
		"review":          map[string]any{"round": 0, "clean_round": nil},
		"documents":       []any{},
		"evidence":        []any{},
		"blockers":        []any{},
		"entities":        map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rotationEventLine builds a single journal event JSONL line for the
// rotation recovery tests. The event_id encodes the sequence so the
// contiguous check can identify each event uniquely.
func rotationEventLine(t *testing.T, runtimeID string, sequence int) []byte {
	t.Helper()
	event := map[string]any{
		"schema_version":      "1.0.0",
		"runtime_id":          runtimeID,
		"event_id":            "evt-arch-" + strconv.Itoa(sequence),
		"sequence":            sequence,
		"event":               "milestone_refreshed",
		"outcome":             "refreshed",
		"actor":               map[string]any{"type": "system", "id": "rotation-test"},
		"request_id":          "rotation-test",
		"baseline_generation": 1,
		"before_revision":     sequence - 1,
		"after_revision":      sequence,
		"from":                map[string]any{"state": "inactive", "phase": nil},
		"to":                  map[string]any{"state": "inactive", "phase": nil},
		"evidence_ids":        []any{},
		"message":             "Rotation test event.",
		"occurred_at":         "2026-08-29T00:00:00Z",
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}
