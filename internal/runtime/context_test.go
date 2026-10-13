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

func TestRuntimeContextBoundsLocksAndPreservesPair(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{true: "legacy_sentinel", false: "process_lock"}[legacy], func(t *testing.T) {
			root := t.TempDir()
			sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
			writeState(t, sp, 1)
			before := mustRead(t, sp)
			if legacy {
				if err := os.WriteFile(sp+".lock", []byte("live owner"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				release, err := filelock.Acquire(context.Background(), sp+".lock.process")
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			}
			base := testWriter(sp, jp)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			ctx = metrics.WithTiming(ctx)
			store := base.WithContext(ctx)
			started := time.Now()
			mutation := artifactMutation(".claude/evidence/cancelled.json")
			mutation.Artifacts = nil // Isolate lock budget from filesystem staging duration.
			_, err := store.Update(1, mutation)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("got %v", err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("Runtime reset the parent deadline")
			}
			_, err = store.Snapshot()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("later call renewed its budget: %v", err)
			}
			if !bytes.Equal(before, mustRead(t, sp)) {
				t.Fatal("timeout changed state")
			}
			if data, _ := os.ReadFile(jp); len(data) > 0 {
				t.Fatal("timeout appended journal")
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/evidence/cancelled.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("published on timeout")
			}
			staging, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
			if len(staging) > 0 {
				t.Fatal("pre-pending staging leaked")
			}
			timing := metrics.ReadTiming(ctx)
			if timing["runtime_lock_wait"].Calls != 2 || timing["runtime_lock_hold"].Calls != 0 {
				t.Fatalf("unobserved lock hold fabricated: %#v", timing)
			}
		})
	}
}

func TestRuntimeCancellationInApplyDoesNotBeginCommit(t *testing.T) {
	root := t.TempDir()
	sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, sp, 1)
	before := mustRead(t, sp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := artifactMutation(".claude/evidence/cancelled.json")
	apply := m.Apply
	m.Apply = func(state map[string]any) error {
		if err := apply(state); err != nil {
			return err
		}
		cancel()
		return nil
	}
	base := testWriter(sp, jp)
	_, err := base.WithContext(ctx).Update(1, m)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled proposal committed: %v", err)
	}
	if !bytes.Equal(before, mustRead(t, sp)) {
		t.Fatal("cancelled Apply changed state")
	}
	if _, err := os.Stat(sp + ".commit-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created pending: %v", err)
	}
	// WithContext must not poison other operations on the original Store.
	if _, err := base.UpdateCurrent(artifactMutation(".claude/evidence/cancelled.json")); err != nil {
		t.Fatalf("independent retry failed: %v", err)
	}
}

func TestRuntimeUpdateCurrentUsesOneLockedStateForApply(t *testing.T) {
	root := t.TempDir()
	sp, jp := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, sp, 1)
	ctx := metrics.WithTiming(context.Background())
	got, err := testWriter(sp, jp).WithContext(ctx).UpdateCurrent(runtime.Mutation{
		EventID: "evt-context", TransitionID: "CONTEXT", Event: "context_test", Actor: "orchestrator", IdempotencyKey: "context-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 {
		t.Fatalf("revision %d", got.Revision)
	}
	spans := metrics.ReadTiming(ctx)
	// Recovery checks the binding once, then Apply reads the locked state.
	if spans["runtime_state_read"].Calls != 2 || spans["runtime_journal_scan"].Calls != 1 {
		t.Fatalf("unexpected duplicate reads: %#v", spans)
	}
}

func TestCancelledRecoveryRetainsPendingAndStagingForRetry(t *testing.T) {
	root, sp, jp, _, _ := artifactRecoveryFixture(t, "partial_publish")
	marker := sp + ".commit-pending.json"
	stateBefore, markerBefore := mustRead(t, sp), mustRead(t, marker)
	stagingBefore, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := testWriter(sp, jp)
	if _, err := store.WithContext(ctx).RecoverPendingOperations(); !errors.Is(err, context.Canceled) {
		t.Fatalf("recovery ignored cancellation: %v", err)
	}
	if !bytes.Equal(stateBefore, mustRead(t, sp)) || !bytes.Equal(markerBefore, mustRead(t, marker)) {
		t.Fatal("cancelled recovery changed durable pair")
	}
	for _, p := range stagingBefore {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("cancelled recovery removed staging", err)
		}
	}
	if recovered, err := store.RecoverPendingOperations(); err != nil || !recovered {
		t.Fatalf("retry recovery: %v %v", recovered, err)
	}
}
