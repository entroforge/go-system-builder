package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
)

func progressFixture() ProgressFacts {
	return ProgressFacts{Binding: "runtime/g1/S5/assignment", Owner: "reviewer", Session: "session", Semantic: "original answer null", Gap: "/answer/conclusion: required", RecoveryOwner: "reviewer", Known: true, Sources: map[string]string{"/necessary": "digest"}}
}
func observeProgress(t *testing.T, root string, f ProgressFacts, e ProgressEvent) *ProgressNotice {
	t.Helper()
	// These cases test counting/reset semantics, not scheduler latency. The
	// public 25ms best-effort lock bound is tested separately under contention.
	n, _, _, err := observeWorkProgress(root, f, e, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func TestWorkProgressRepeatOnceAndMeaningfulReset(t *testing.T) {
	root := t.TempDir()
	f := progressFixture()
	event := ProgressEvent{Action: "status", Known: true}
	for i := 0; i < ProgressRepeatThreshold; i++ {
		if n := observeProgress(t, root, f, event); n != nil {
			t.Fatal("early diagnosis", n)
		}
	}
	n := observeProgress(t, root, f, event)
	if n == nil || n.Authorizes || n.Repeats != 5 || n.RecoveryOwner != "reviewer" {
		t.Fatal(n)
	}
	for i := 0; i < 10; i++ {
		if n := observeProgress(t, root, f, event); n != nil {
			t.Fatal("duplicate diagnosis")
		}
	}
	f.Semantic = "original answer pass"
	for i := 0; i < 5; i++ {
		if n := observeProgress(t, root, f, event); n != nil {
			t.Fatal("new professional fact did not reset")
		}
	}
	if n := observeProgress(t, root, f, event); n == nil {
		t.Fatal("new fact permanently disabled diagnosis")
	}
}
func TestWorkProgressNewNecessaryRangeUnknownRunningWaiting(t *testing.T) {
	root := t.TempDir()
	f := progressFixture()
	read := ProgressEvent{Action: "Read", Known: true, Read: &ProgressRead{Path: "/necessary", Digest: "digest", Range: "1:10"}}
	observeProgress(t, root, f, read)
	for i := 0; i < 4; i++ {
		observeProgress(t, root, f, read)
	}
	read.Read.Range = "11:10"
	if n := observeProgress(t, root, f, read); n != nil {
		t.Fatal("new necessary chapter falsely stagnant")
	}
	unknown := ProgressEvent{Action: "Read", Known: true}
	for i := 0; i < 20; i++ {
		if n := observeProgress(t, root, f, unknown); n != nil {
			t.Fatal("unknown read extent diagnosed")
		}
	}
	for _, kind := range []string{"running", "waiting", "complete"} {
		copy := f
		copy.Running = kind == "running"
		copy.Waiting = kind == "waiting"
		copy.Complete = kind == "complete"
		for i := 0; i < 20; i++ {
			if n := observeProgress(t, root, copy, ProgressEvent{Action: "status", Known: true}); n != nil {
				t.Fatal("external wait or live execution diagnosed", kind)
			}
		}
	}
}
func TestWorkProgressBoundedRangeSaturationAndDamagedCache(t *testing.T) {
	root := t.TempDir()
	f := progressFixture()
	for i := 0; i < 65; i++ {
		observeProgress(t, root, f, ProgressEvent{Action: "Read", Known: true, Read: &ProgressRead{Path: "/necessary", Digest: "digest", Range: fmt.Sprint(i)}})
	}
	for i := 0; i < 10; i++ {
		if n := observeProgress(t, root, f, ProgressEvent{Action: "status", Known: true}); n != nil {
			t.Fatal("saturated coverage diagnosed")
		}
	}
	path := filepath.Join(root, ".claude/hook-notices/work-progress", progressHash(f.Owner+"\x00"+f.Session)+".json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r progressRecord
	json.Unmarshal(bytes, &r)
	if len(r.Reads) != 64 || !r.Saturated || r.Coverage != "saturated" {
		t.Fatal(r)
	}
	if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if n, _, _, err := ObserveWorkProgress(root, f, ProgressEvent{Action: "status", Known: true}); err == nil || n != nil {
		t.Fatal("damaged diagnostic gained authority", n, err)
	}
	if bytes, _ := os.ReadFile(path); string(bytes) != "damaged" {
		t.Fatal("damaged cache overwritten")
	}
}
func TestWorkProgressConcurrentOnceAndLockBound(t *testing.T) {
	root := t.TempDir()
	facts := map[string]string{
		".claude/loop-state.json":   "original Runtime",
		".claude/loop-events.jsonl": "original journal",
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, content := range facts {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := progressFixture()
	observeProgress(t, root, f, ProgressEvent{Action: "status", Known: true})
	var wg sync.WaitGroup
	var mu sync.Mutex
	notices := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, _, _, err := ObserveWorkProgress(root, f, ProgressEvent{Action: "status", Known: true})
			if err != nil {
				return
			}
			if n != nil {
				mu.Lock()
				notices++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// The bounded diagnostic lock may drop observations under contention.
	// Sequential threshold behavior is covered above; concurrency must not
	// duplicate notices or turn missing telemetry into a required retry.
	if notices > 1 {
		t.Fatalf("concurrent notice count=%d", notices)
	}
	lock := filepath.Join(root, ".claude/hook-notices/work-progress", progressHash(f.Owner+"\x00"+f.Session)+".json.lck")
	release, err := filelock.Acquire(context.Background(), lock)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	started := time.Now()
	if n, _, _, err := ObserveWorkProgress(root, f, ProgressEvent{Action: "status", Known: true}); err == nil || n != nil {
		t.Fatal("busy cache bypassed bounded lock", n, err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("busy diagnostic blocked for %s", elapsed)
	}
	for path, content := range facts {
		if data, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(data) != content {
			t.Fatalf("diagnostic changed engineering facts at %s: %v", path, err)
		}
	}
}

func TestWorkProgressReportsOnlyResponsibilityFactChanges(t *testing.T) {
	root := t.TempDir()
	facts := progressFixture()
	event := ProgressEvent{Action: "status", Known: true}
	for i, want := range []bool{true, false} {
		_, known, changed, err := observeWorkProgress(root, facts, event, time.Second)
		if err != nil || !known || changed != want {
			t.Fatalf("observation %d: known=%v changed=%v err=%v", i, known, changed, err)
		}
	}
	read := ProgressEvent{Action: "Read", Known: true, Read: &ProgressRead{Path: "/necessary", Digest: "digest", Range: "1:2"}}
	if _, known, changed, err := observeWorkProgress(root, facts, read, time.Second); err != nil || !known || changed {
		t.Fatalf("new read range became a changed answer/source: %v %v %v", known, changed, err)
	}
	facts.Semantic = "actual changed professional answer"
	if _, known, changed, err := observeWorkProgress(root, facts, event, time.Second); err != nil || !known || !changed {
		t.Fatalf("changed fact lost: %v %v %v", known, changed, err)
	}
	facts.Known = false
	if _, known, _, err := ObserveWorkProgress(root, facts, event); err != nil || known {
		t.Fatalf("unknown facts became known: %v %v", known, err)
	}
}
