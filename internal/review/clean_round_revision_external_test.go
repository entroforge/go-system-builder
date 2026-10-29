package review_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/verification"
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
	if clean := verification.EvaluateCleanRound(final.State); !clean.Passed {
		t.Fatalf("fresh original accepted results remain vetoed by explicitly superseded history: %v", clean.Reasons)
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
	for _, tc := range []struct {
		name string
		edit func(map[string]any, map[string]any)
	}{
		{"missing accepted revision marker", func(_ map[string]any, e map[string]any) { e["invalidated_by"] = nil }},
		{"other plan", func(_ map[string]any, e map[string]any) { e["invalidated_by"] = "other-plan@r2" }},
		{"future revision", func(s map[string]any, e map[string]any) {
			e["invalidated_by"] = review.PlanPointerFromState(s).PlanID + "@r99"
		}},
		{"ordinary invalidation", func(_ map[string]any, e map[string]any) { e["invalidation_rule"] = "source_changed" }},
		{"wrong reason", func(_ map[string]any, e map[string]any) { e["invalidation_reason"] = "product_changed" }},
		{"different generation", func(_ map[string]any, e map[string]any) { e["baseline_generation"] = float64(99) }},
		{"no revision accepted", func(s map[string]any, _ map[string]any) {
			s["review"].(map[string]any)["plan"].(map[string]any)["revision"] = float64(1)
		}},
		{"current Claim still uses invalid Result", func(s map[string]any, e map[string]any) {
			s["review"].(map[string]any)["claims"].(map[string]any)["claim-dv-1"].(map[string]any)["result_id"] = e["id"]
		}},
		{"current Assignment still uses invalid Result", func(s map[string]any, e map[string]any) {
			s["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-2"].(map[string]any)["result_ref"] = e["path"]
		}},
		{"current receipt still uses invalid Result", func(s map[string]any, e map[string]any) {
			s["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-2"].(map[string]any)["result_receipt"].(map[string]any)["result_id"] = e["id"]
		}},
		{"invalid Finding", func(_ map[string]any, e map[string]any) { e["kind"] = "finding" }},
		{"invalid batch", func(_ map[string]any, e map[string]any) { e["kind"] = "observation_batch" }},
		{"invalid clean snapshot", func(_ map[string]any, e map[string]any) { e["kind"] = "clean_round" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(final.State)
			var state map[string]any
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			for _, raw := range state["evidence"].([]any) {
				entry := raw.(map[string]any)
				if entry["id"] == old["id"] {
					tc.edit(state, entry)
				}
			}
			clean := verification.EvaluateCleanRound(state)
			for _, check := range clean.Checks {
				if check.Name == "no_invalidated_pass_evidence" && !check.Passed {
					return
				}
			}
			t.Fatalf("unresolved invalid evidence was borrowed as superseded: %+v", clean)
		})
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
	cleanRef := ""
	for _, raw := range final.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["kind"] == "clean_round" && entry["status"] == "valid" {
			cleanRef, _ = entry["id"].(string)
		}
	}
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "TR-009", Actor: "orchestrator", ExpectedRevision: final.Revision,
		Files: fileview.Disk{Root: root}, Evidence: map[string]string{"clean_round_record": cleanRef},
	})
	if err != nil || next.State["lifecycle"].(map[string]any)["state"] != "acceptance" {
		t.Fatalf("original clean-round exit cannot consume accepted revision results: %v", err)
	}
}
