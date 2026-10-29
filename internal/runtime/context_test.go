package runtime_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestStoreContextCancelsLockWaitWithoutRefreshingBudget(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := filepath.Join(root, "state.json"), filepath.Join(root, "events.jsonl")
	writeState(t, statePath, 1)
	release, err := filelock.Acquire(context.Background(), statePath+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if _, err := runtime.NewStore(statePath, journalPath).WithContext(ctx).Snapshot(); !errors.Is(err, context.Canceled) {
		t.Fatalf("lock wait ignored the request cancellation: %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("lock started a new five-second wait")
	}
}

func TestStoreContextCancellationBeforePendingLeavesOriginalPair(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := filepath.Join(root, "state.json"), filepath.Join(root, "events.jsonl")
	writeState(t, statePath, 1)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ctx, spans := metrics.WithCallTimings(ctx)
	store := testWriter(statePath, journalPath)
	_, err = store.WithContext(ctx).Update(1, runtime.Mutation{EventID: "evt-cancel", TransitionID: "TR-TEST", Event: "test_transition", Actor: "orchestrator", IdempotencyKey: "cancel-before-pending", Apply: func(state map[string]any) error {
		state["updated_at"] = "2026-06-20T00:00:01Z"
		cancel()
		return nil
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled candidate committed: %v", err)
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("cancellation changed the original state")
	}
	for _, p := range []string{journalPath, statePath + ".commit-pending.json"} {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			t.Fatalf("cancellation published %s", p)
		}
	}
	if _, err := store.Snapshot(); err != nil {
		t.Fatalf("context copy changed the original Store: %v", err)
	}
	if spans.Snapshot()["runtime/lock_hold"].Count != 1 {
		t.Fatal("completed lock span was lost")
	}
}
