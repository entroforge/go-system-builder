package runtime_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestStoreUpdateUsesRevisionCASAndAppendsJournal(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
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

	store := testWriter(statePath, journalPath)
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
	store := testWriter(statePath, journalPath)

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
	prepareMissingReconcileTarget(t, statePath, journalPath)
	store := testWriter(statePath, journalPath)

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
	var event map[string]any
	for scanner.Scan() {
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	if event == nil {
		t.Fatal("expected reconciled journal line")
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

	store := testWriter(statePath, journalPath)
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

func TestStoreValidationFailureLeavesStateJournalAndRevisionUnchanged(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	if err := os.WriteFile(journalPath, []byte("journal-before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	store := testWriter(statePath, journalPath)
	_, err = store.Update(1, runtime.Mutation{
		EventID:        "evt-invalid-candidate",
		TransitionID:   "TR-TEST",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1:invalid",
		Apply: func(state map[string]any) error {
			state["unknown_runtime_field"] = true
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected schema validation failure")
	}

	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatal("validation failure changed state bytes")
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatal("validation failure changed journal bytes")
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 {
		t.Fatalf("validation failure changed revision: got %d", snapshot.Revision)
	}
}

func TestStoreRolloverRejectsNonCanonicalFreshState(t *testing.T) {
	dir := t.TempDir()
	store := testWriter(filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl"))
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

	store := testWriter(statePath, journalPath)
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
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-test"
	state["revision"] = revision
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	state["last_transition"] = nil
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	definitionDir := filepath.Join(filepath.Dir(path), "docs", "control")
	if err := os.MkdirAll(definitionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(definitionDir, "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Maintenance must preserve document, evidence and REQ attestations while
// refreshing Harness metadata without a revision bump or a new journal entry.
