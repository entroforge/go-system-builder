package review_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestCleanRoundAfterOriginalAcceptedPlanRevisionRetainsHistory(t *testing.T) {
	root, statePath, journalPath, final := review.MovedClaimRevisionFixtureForTest(t)
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	for _, raw := range final.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == "review-result-moved-old" {
			old = entry
		}
	}
	if old == nil || old["status"] != "invalid" || old["invalidation_rule"] != "review_plan_revision" {
		t.Fatalf("fixture lacks the original Writer's explicit supersession: %+v", old)
	}
	oldBytes, err := os.ReadFile(filepath.Join(root, old["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if !review.RequiredResponsibilitiesComplete(final.State) {
		t.Fatal("original current responsibilities were not complete")
	}
	after, err := os.ReadFile(filepath.Join(root, old["path"].(string)))
	if err != nil || !bytes.Equal(after, oldBytes) || old["status"] != "invalid" {
		t.Fatalf("qualification rewrote historical Result: %v", err)
	}
	for path, want := range map[string][]byte{statePath: beforeState, journalPath: beforeJournal} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, want) {
			t.Fatalf("pure CleanRound evaluation mutated %s: %v", path, err)
		}
	}
	// Use the actual original transition, not just the status label. This is
	// the existing noncanonical typed fixture; canonical product guards retain
	// their separate source proofs and are not bypassed by the qualification.
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-009", Actor: "orchestrator", ExpectedRevision: final.Revision,
		Files: fileview.Disk{Root: root},
	})
	if err != nil || next.State["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatalf("original clean-round exit cannot consume accepted revision results: %v", err)
	}
}
