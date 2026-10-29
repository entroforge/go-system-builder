package integration

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

// TestCheckpointStoreCASStale verifies that CompareAndSwap refuses to
// overwrite a checkpoint whose Revision does not match.
func TestCheckpointStoreCASStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	store := newTestStore(t)

	first, err := store.CompareAndSwap(path, Checkpoint{Revision: 0}, Checkpoint{
		AssignmentID: "a-1",
		State:        StateReady,
	})
	if err != nil {
		t.Fatalf("first CAS: %v", err)
	}
	if first.Revision != 1 {
		t.Fatalf("first CAS revision: got %d want 1", first.Revision)
	}

	// Stale second write: current.Revision is 0 but on-disk Revision is 1.
	_, err = store.CompareAndSwap(path, Checkpoint{Revision: 0}, Checkpoint{
		AssignmentID: "a-1",
		State:        StateMerged,
	})
	if err == nil {
		t.Fatal("expected stale error")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale error, got %v", err)
	}
}

// TestCheckpointStoreIdempotentKey verifies the canonical key shape.
func TestCheckpointStoreIdempotentKey(t *testing.T) {
	k := IdempotencyKey("a", "b", "c", 1)
	if !strings.HasPrefix(k, "ckpt:") {
		t.Fatalf("key must start with ckpt: prefix, got %q", k)
	}
	if len(k) != len("ckpt:")+16 {
		t.Fatalf("key length unexpected: %q", k)
	}
}

// TestCheckpointStoreForceWrite verifies that ForceWrite bumps Revision
// even after a CAS-stale write so failure paths still produce a durable
// record.
func TestCheckpointStoreForceWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	store := newTestStore(t)

	if _, err := store.ForceWrite(path, Checkpoint{State: StatePreserved}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ForceWrite(path, Checkpoint{State: StatePreserved, FailureReason: "boom"}); err != nil {
		t.Fatal(err)
	}
	cp, found, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected checkpoint on disk")
	}
	if cp.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", cp.Revision)
	}
	if cp.FailureReason != "boom" {
		t.Fatalf("expected failure reason preserved, got %q", cp.FailureReason)
	}
}

// TestCheckpointStorePath is the canonical path test for the BUG-039-05
// "checkpoint evidence directory" layout.
func TestCheckpointStorePath(t *testing.T) {
	store := DefaultCheckpointStore()
	p := store.Path("/repo", "loop-REQ-039", 1, "assignment-x")
	want := "/repo/.claude/evidence/loop-REQ-039/g1/worktree/assignment-x/checkpoint.json"
	if p != want {
		t.Fatalf("path: got %q want %q", p, want)
	}
}

func TestCheckpointStorePathForRecordPreservesR1AndSeparatesRevisions(t *testing.T) {
	store := DefaultCheckpointStore()
	ref := assignmentrecord.RecordReference{
		RuntimeID: "loop-REQ-039", BaselineGeneration: 3,
		AssignmentID: "assignment-core", Revision: 1,
	}
	r1 := filepath.Join("/repo", ".claude/evidence/loop-REQ-039/g3/worktree/assignment-core/checkpoint.json")
	if got := store.PathForRecord("/repo", ref); got != r1 {
		t.Fatalf("r1 Record path = %q, want legacy path %q", got, r1)
	}
	if got := CheckpointPathForRecord("/repo", ref); got != r1 {
		t.Fatalf("public r1 Record path = %q, want %q", got, r1)
	}
	ref.Revision = 2
	r2 := filepath.Join("/repo", ".claude/evidence/loop-REQ-039/g3/worktree/assignment-core/a2/checkpoint.json")
	if got := store.PathForRecord("/repo", ref); got != r2 {
		t.Fatalf("r2 Record path = %q, want revision slot %q", got, r2)
	}
	ref.Revision = 3
	r3 := filepath.Join("/repo", ".claude/evidence/loop-REQ-039/g3/worktree/assignment-core/a3/checkpoint.json")
	if got := store.PathForRecord("/repo", ref); got != r3 {
		t.Fatalf("r3 Record path = %q, want revision slot %q", got, r3)
	}
	if got := store.PathForRecord("/repo", assignmentrecord.RecordReference{RuntimeID: "../escape", BaselineGeneration: 3, AssignmentID: "assignment-core", Revision: 2}); got != "" {
		t.Fatalf("invalid Record coordinate path = %q, want empty", got)
	}
}

// newTestStore returns a CheckpointStore with a fixed clock so tests
// can compare UpdatedAt deterministically.
func newTestStore(t *testing.T) *CheckpointStore {
	t.Helper()
	fixed := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	return &CheckpointStore{
		Clock:     func() time.Time { return fixed },
		NowString: func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) },
	}
}

// guard: CAS must be safe under concurrent callers (one wins, one gets
// ErrCASStale). This is a basic regression test for the optimistic lock.
func TestCheckpointStoreCASConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")
	store := newTestStore(t)

	cp := Checkpoint{State: StateReady}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.CompareAndSwap(path, cp, Checkpoint{State: StateMerged})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	stales := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case strings.Contains(err.Error(), "stale"):
			stales++
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if successes != 1 || stales != 1 {
		t.Fatalf("want 1 success + 1 stale, got %d/%d", successes, stales)
	}
}
