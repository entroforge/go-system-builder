package repair_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// revisionEventID matches the former Runtime-write-count handoff identity.
var revisionEventID = regexp.MustCompile(`^evt-s9-handoff-r[0-9]+$`)

// journalEventIDs counts the journal events per event id.
func journalEventIDs(t *testing.T, journalPath string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if id, ok := event["event_id"].(string); ok {
			ids[id]++
		}
	}
	return ids
}

func TestS9HandoffEntryIdentityReplayAndConcurrentRecovery(t *testing.T) {
	root, statePath, journalPath, ref := overBudgetHandoffFixture(t)
	before, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	pointer, _ := before.State["review"].(map[string]any)["repair"].(map[string]any)
	sessionID, _ := pointer["session_id"].(string)
	// G18: the handoff event identity is the repair session that owns the
	// handoff, not the Runtime write count. The derived S7 plan identity
	// therefore stays a stable function of the session.
	wantEventID := fmt.Sprintf("evt-s9-handoff-%s", sessionID)
	want := reviewmeta.NewPlanID(before.State["runtime_id"].(string), wantEventID)
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
	// G18 reverse assertion: the repeated handoff of the same session wrote
	// exactly one journal event, and its id carries the session identity
	// instead of a Runtime write count.
	handoffEvents := journalEventIDs(t, journalPath)
	if handoffEvents[wantEventID] != 1 {
		t.Fatalf("handoff event identity %q was not written exactly once: %v", wantEventID, handoffEvents)
	}
	for id := range handoffEvents {
		if revisionEventID.MatchString(id) {
			t.Fatalf("handoff event id still carries a Runtime write count: %q", id)
		}
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
