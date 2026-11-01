package repair_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9HandoffEntryIdentityReplayAndConcurrentRecovery(t *testing.T) {
	root, statePath, journalPath, ref := overBudgetHandoffFixture(t)
	before, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := reviewmeta.NewPlanID(before.State["runtime_id"].(string), fmt.Sprintf("evt-s9-handoff-r%d", before.Revision+1))
	request := repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, Handoff: ref}
	const n = 4
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := repair.CommitRepairHandoff(root, statePath, journalPath, request)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	after, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision+1 {
		t.Fatalf("duplicate handoff commits: before=%d after=%d", before.Revision, after.Revision)
	}
	review := after.State["review"].(map[string]any)
	if fmt.Sprint(review["round"]) != fmt.Sprint(before.State["review"].(map[string]any)["round"]) {
		t.Fatalf("historical round changed: %v", review["round"])
	}
	entry := review["round_entry"].(map[string]any)
	if entry["plan_id"] != want {
		t.Fatalf("entry identity=%v want=%s", entry, want)
	}
	if _, ok := entry["round"]; ok {
		t.Fatalf("new entry emits number: %v", entry)
	}
	seedPath := filepath.Join(root, filepath.FromSlash(entry["review_plan_seed_ref"].(string)))
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repair.CommitRepairHandoff(root, statePath, journalPath, request); err != nil {
		t.Fatal(err)
	}
	replay, err := os.ReadFile(seedPath)
	if err != nil || string(seed) != string(replay) {
		t.Fatalf("seed changed on replay: %v", err)
	}
	request.ExpectedRevision = before.Revision
	if _, err = repair.CommitRepairHandoff(root, statePath, journalPath, request); !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("explicit stale CAS=%v", err)
	}
}
