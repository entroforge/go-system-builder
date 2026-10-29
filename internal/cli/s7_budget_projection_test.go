package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The receipt rows are original accepted-state fixtures for this read-only
// projection check, not author-built budget decisions or S10 reception tests.
func TestS7BudgetWaitRequiresActualOriginalRequestForAnotherRound(t *testing.T) {
	for _, scenario := range []string{"acceptance-unanswered", "acceptance-pass", "acceptance-review-required", "missing-receipt", "changed-receipt", "wrong-runtime", "draining-S7"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			state := map[string]any{
				"runtime_id": "budget-runtime", "baseline": map[string]any{"generation": 1},
				"lifecycle": map[string]any{"state": "acceptance", "phase": nil}, "review": map[string]any{"round": 5},
				"configuration": map[string]any{"repair": map[string]any{"max_full_review_rounds": 5}}, "evidence": []any{},
			}
			if scenario == "draining-S7" {
				state["lifecycle"] = map[string]any{"state": "verification", "phase": "running"}
			}
			if scenario != "acceptance-unanswered" {
				conclusion := "review_required"
				if scenario == "acceptance-pass" {
					conclusion = "pass"
				}
				source := map[string]any{"runtime_id": "budget-runtime", "baseline_generation": 1, "review_round": 5, "conclusion": conclusion}
				if scenario == "wrong-runtime" {
					source["runtime_id"] = "other-runtime"
				}
				data, _ := json.Marshal(source)
				if err := os.WriteFile(filepath.Join(root, "accepted.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				state["evidence"] = []any{map[string]any{"id": "accepted-report", "kind": "acceptance", "status": "valid", "path": "accepted.json", "sha256": workpacket.Digest(data), "baseline_generation": 1, "review_round": 5, "invalidated_by": nil}}
				if scenario == "changed-receipt" {
					if err := os.WriteFile(filepath.Join(root, "accepted.json"), []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if scenario == "missing-receipt" {
					if err := os.Remove(filepath.Join(root, "accepted.json")); err != nil {
						t.Fatal(err)
					}
				}
			}
			want := scenario == "acceptance-review-required"
			if got := s7BudgetGateRequired(state, root); got != want {
				t.Fatalf("budget wait=%v want=%v for %s", got, want, scenario)
			}
			context := stageS7BudgetDecisionQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
			if scenario == "missing-receipt" || scenario == "changed-receipt" || scenario == "wrong-runtime" {
				if !strings.Contains(context, "Main/system") || !strings.Contains(context, "accepted.json") {
					t.Fatalf("original source failure lost its exact system recovery: %s", context)
				}
			}
			if paths, _ := filepath.Glob(filepath.Join(root, ".claude", "decisions", "*.application.json")); len(paths) != 0 {
				t.Fatalf("read-only projection created human applications: %v", paths)
			}
		})
	}
}
