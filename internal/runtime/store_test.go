package runtime_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestStoreUpdateUsesRevisionCASAndAppendsJournal(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := runtime.NewStore(statePath, journalPath)
	next, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-2",
		TransitionID:   "TR-TEST",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:01Z"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if next.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", next.Revision)
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected journal event")
	}
}

func TestStoreRetainLastTransitionSkipsLastTransitionOverwrite(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := runtime.NewStore(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-tr008",
		TransitionID:   "TR-008",
		Event:          "transition_committed",
		Actor:          "hook",
		IdempotencyKey: "runtime:TR-008:1",
	}); err != nil {
		t.Fatal(err)
	}

	next, err := store.Update(2, runtime.Mutation{
		EventID:              "evt-milestone-refreshed-r3",
		TransitionID:         "MILESTONE-REFRESH",
		Event:                "milestone_refreshed",
		JournalEvent:         "milestone_refreshed",
		JournalOutcome:       "refreshed",
		RetainLastTransition: true,
		Actor:                "hook",
		IdempotencyKey:       "runtime:milestone:refresh",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-07-31T00:00:02Z"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("retain-last update failed: %v", err)
	}
	if next.Revision != 3 {
		t.Fatalf("expected revision 3, got %d", next.Revision)
	}
	last, ok := next.State["last_transition"].(map[string]any)
	if !ok || last == nil {
		t.Fatal("last_transition must remain after retain-last mutation")
	}
	if last["transition_id"] != "TR-008" {
		t.Fatalf("last_transition.transition_id=%v, want TR-008", last["transition_id"])
	}
	if last["event_id"] != "evt-tr008" {
		t.Fatalf("last_transition.event_id=%v, want evt-tr008", last["event_id"])
	}

	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 journal lines, got %d", len(lines))
	}
	var refresh map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &refresh); err != nil {
		t.Fatal(err)
	}
	if refresh["event"] != "milestone_refreshed" {
		t.Fatalf("journal event=%v, want milestone_refreshed", refresh["event"])
	}
	if refresh["outcome"] != "refreshed" {
		t.Fatalf("journal outcome=%v, want refreshed", refresh["outcome"])
	}
}

func TestStoreSerializesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	store := runtime.NewStore(statePath, journalPath)

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := store.Update(1, runtime.Mutation{
				EventID:        "evt-concurrent-" + string(rune('a'+index)),
				TransitionID:   "TR-TEST",
				Event:          "test_transition",
				Actor:          "orchestrator",
				IdempotencyKey: "runtime:TR-TEST:1:" + string(rune('a'+index)),
			})
			results <- err
		}(index)
	}
	wg.Wait()
	close(results)

	successes, stale := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, runtime.ErrStaleRevision):
			stale++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("expected one success and one stale result, got success=%d stale=%d", successes, stale)
	}
}

func TestStoreReconcilesMissingLastJournalEvent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	store := runtime.NewStore(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-reconcile",
		TransitionID:   "TR-TEST",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	reconciled, err := store.Reconcile()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !reconciled {
		t.Fatal("expected missing event to be reconciled")
	}
	file, err := os.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("expected reconciled journal line")
	}
	var event map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["event_id"] != "evt-reconcile" {
		t.Fatalf("unexpected reconciled event: %v", event)
	}
}

func TestStoreRejectsStaleRevisionWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 2)
	before, _ := os.ReadFile(statePath)

	store := runtime.NewStore(statePath, journalPath)
	_, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-stale",
		TransitionID:   "TR-TEST",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
	})
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("expected ErrStaleRevision, got %v", err)
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("stale update mutated state")
	}
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale update should not create journal, got %v", err)
	}
}

func TestStoreRolloverRejectsNonCanonicalFreshState(t *testing.T) {
	dir := t.TempDir()
	store := runtime.NewStore(filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl"))
	_, err := store.Rollover(
		map[string]any{"runtime_id": "loop-inactive", "revision": 0},
		filepath.Join(dir, "archive"),
		runtime.RolloverApproval{ApprovedBy: "release-owner", EvidenceID: "ev-approval"},
		time.Now().UTC(),
	)
	if err == nil || !strings.Contains(err.Error(), "validate fresh runtime") {
		t.Fatalf("rollover error = %v, want non-canonical fresh-state rejection", err)
	}
}

func TestStoreRecoversExpiredLock(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	lockPath := statePath + ".lock"
	writeState(t, statePath, 1)

	if err := os.WriteFile(lockPath, []byte(`{"pid":999999,"created_at":"2026-06-20T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, expired, expired); err != nil {
		t.Fatal(err)
	}

	store := runtime.NewStore(statePath, journalPath)
	next, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-after-expired-lock",
		TransitionID:   "TR-TEST",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
	})
	if err != nil {
		t.Fatalf("expected expired lock recovery, got %v", err)
	}
	if next.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", next.Revision)
	}
}

func writeState(t *testing.T, path string, revision int) {
	t.Helper()
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   revision,
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": revision,
			"last_event_id": nil,
		},
		"last_transition": nil,
		"updated_at":      "2026-06-20T00:00:00Z",
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStoreRefreshFingerprintsRecomputesSHA covers BUG-004 repair: the
// fingerprint refresh command must rewrite stale SHA256 hashes in the runtime
// state's documents / evidence / bound_req entries without bumping the
// revision or appending to the journal.
func TestStoreRefreshFingerprintsRecomputesSHA(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")

	// Two real document files on disk.
	docPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(docPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	evPath := filepath.Join(dir, "evidence.md")
	if err := os.WriteFile(evPath, []byte("evidence body"), 0o644); err != nil {
		t.Fatal(err)
	}

	// State pre-populated with stale hashes (wrong on purpose).
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   3,
		"journal":    map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 3, "last_event_id": "evt-3"},
		"last_transition": map[string]any{
			"event_id": "evt-3", "sequence": 3, "transition_id": "TR-X", "event": "x", "actor": "user",
			"from":              map[string]any{"state": "a", "phase": nil},
			"to":                map[string]any{"state": "b", "phase": nil},
			"expected_revision": 3, "committed_revision": 3, "idempotency_key": "k",
			"evidence_ids": []string{}, "occurred_at": "2026-06-20T00:00:00Z",
		},
		"updated_at": "2026-06-20T00:00:00Z",
		"documents": []any{
			map[string]any{"id": "DOC-1", "kind": "task", "path": "doc.md", "version": "v1", "sha256": "stale", "status": "locked", "generation": 1},
		},
		"evidence": []any{
			map[string]any{"id": "EV-1", "kind": "document_review", "path": "evidence.md", "sha256": "stale", "status": "valid", "baseline_generation": 1, "review_round": 1, "produced_by": []string{"a"}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": nil, "scope_refs": []string{}},
		},
		"bound_req": map[string]any{
			"id": "REQ-001", "path": "doc.md", "version": "v1", "sha256": "stale",
			"status": "locked", "approved_by": "u", "approved_at": "2026-06-20T00:00:00Z",
		},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	store := runtime.NewStore(statePath, journalPath)
	result, err := store.RefreshFingerprints(dir)
	if err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}
	if len(result.Updated) != 3 {
		t.Fatalf("expected 3 updated entries, got %d (%v)", len(result.Updated), result.Updated)
	}
	if len(result.Unchanged) != 0 {
		t.Fatalf("expected 0 unchanged, got %d (%v)", len(result.Unchanged), result.Unchanged)
	}

	// Confirm revision did NOT change.
	raw, _ := os.ReadFile(statePath)
	var after map[string]any
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if after["revision"].(float64) != 3 {
		t.Fatalf("revision must not change; got %v", after["revision"])
	}

	// Confirm journal was NOT appended.
	if _, err := os.Stat(journalPath); err == nil {
		t.Fatal("journal file must not exist after fingerprint refresh")
	}

	// Re-run; now everything should report Unchanged.
	result2, err := store.RefreshFingerprints(dir)
	if err != nil {
		t.Fatalf("second RefreshFingerprints: %v", err)
	}
	if len(result2.Updated) != 0 || len(result2.Unchanged) != 3 {
		t.Fatalf("expected 0 updated / 3 unchanged on second pass; got updated=%d unchanged=%d", len(result2.Updated), len(result2.Unchanged))
	}
}

func TestStoreRefreshFingerprintsSynchronizesDefinitionVersion(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	definitionPath := filepath.Join(dir, "loop-definition.json")
	definition := []byte(`{"schema_version":"1.3.0","definition_id":"test"}`)
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	definitionHash := sha256.Sum256(definition)
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   8,
		"definition": map[string]any{
			"path":    "loop-definition.json",
			"version": "1.2.0",
			"sha256":  fmt.Sprintf("%x", definitionHash),
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.NewStore(statePath, filepath.Join(dir, "loop-events.jsonl")).RefreshFingerprints(dir); err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}

	updated, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if err := json.Unmarshal(updated, &after); err != nil {
		t.Fatal(err)
	}
	definitionState := after["definition"].(map[string]any)
	if definitionState["version"] != "1.3.0" {
		t.Fatalf("definition version was not synchronized: got %v", definitionState["version"])
	}
	if after["revision"].(float64) != 8 {
		t.Fatalf("fingerprint refresh must not change revision: got %v", after["revision"])
	}
}

// TestStoreRefreshFingerprintsReportsMissing covers the case where a document
// path in state no longer exists on disk; the entry should be reported in
// Missing rather than silently updated to an empty hash.
func TestStoreRefreshFingerprintsReportsMissing(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")

	state := map[string]any{
		"runtime_id":      "loop-test",
		"revision":        0,
		"journal":         map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil},
		"last_transition": nil,
		"updated_at":      "2026-06-20T00:00:00Z",
		"documents": []any{
			map[string]any{"id": "GONE", "kind": "task", "path": "does-not-exist.md", "version": "v1", "sha256": "stale", "status": "locked", "generation": 1},
		},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	store := runtime.NewStore(statePath, journalPath)
	result, err := store.RefreshFingerprints(dir)
	if err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "does-not-exist.md" {
		t.Fatalf("expected Missing=[does-not-exist.md], got %v", result.Missing)
	}
	if len(result.Updated) != 0 {
		t.Fatalf("expected no updates, got %v", result.Updated)
	}
}

func TestStoreRefreshFingerprintsUpdatesTaskEntityHash(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	taskPath := filepath.Join(dir, "TASK-001.md")
	if err := os.WriteFile(taskPath, []byte("updated task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"revision": 1,
		"entities": map[string]any{
			"tasks": []any{map[string]any{
				"id": "TASK-001", "path": "TASK-001.md", "sha256": "stale", "state": "reviewed",
			}},
		},
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runtime.NewStore(statePath, filepath.Join(dir, "loop-events.jsonl")).RefreshFingerprints(dir)
	if err != nil {
		t.Fatalf("RefreshFingerprints: %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "TASK-001.md" {
		t.Fatalf("expected task entity update, got %#v", result)
	}
	updated, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if err := json.Unmarshal(updated, &after); err != nil {
		t.Fatal(err)
	}
	task := after["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	if task["sha256"] == "stale" || task["sha256"] == "" {
		t.Fatalf("task hash was not refreshed: %#v", task)
	}
}
