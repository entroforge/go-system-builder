package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
)

func TestMainReviewPlanIdentityNaturalReceptionAndReplay(t *testing.T) {
	for _, identity := range []string{"legacy", "plan"} {
		t.Run(identity, func(t *testing.T) {
			root := worktreeProjectRoot(t)
			const planID = "review-plan-natural-original"
			statePath := filepath.Join(root, ".claude/loop-state.json")
			state := stageSnapshot(t, root).State
			if identity == "plan" {
				// Retain the historical number while entering a new responsibility.
				state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": planID, "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
				data, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(statePath, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			path := review.PlanQuestionnairePath(root, state)
			out := s7MainNaturalPre(t, root)
			if !strings.Contains(out, "S7 Main ReviewPlan questionnaire: "+path) {
				t.Fatalf("missing original planning duty: %s", out)
			}
			draft, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out = s7MainNaturalPre(t, root)
			if strings.Contains(out, "review-plan: registered") {
				t.Fatalf("unfinished draft was registered: %s", out)
			}
			if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, draft) {
				t.Fatalf("ordinary observation changed unfinished draft: %v", err)
			}
			// This fixture explicitly retains the professional E2E N/A answer.
			authored := minimalReviewPlan(t, t.TempDir(), planID)
			data, err := os.ReadFile(authored)
			if err != nil {
				t.Fatal(err)
			}
			if identity == "plan" {
				var answer map[string]any
				if err := json.Unmarshal(data, &answer); err != nil {
					t.Fatal(err)
				}
				answer["schema_version"] = "1.1.0"
				delete(answer, "review_round")
				data, err = json.Marshal(answer)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			out = s7MainNaturalPre(t, root)
			for _, expected := range []string{"questionnaire observed at " + path, "review-plan: registered " + planID, "prepares and registers", "manifest-draft"} {
				if !strings.Contains(out, expected) {
					t.Fatalf("original consumer omitted %q: %s", expected, out)
				}
			}
			ptr := review.PlanPointerFromState(stageSnapshot(t, root).State)
			if ptr == nil || ptr.PlanID != planID || s7ReviewPlanRegistrations(t, root) != 1 {
				t.Fatalf("original registration missing: %+v", ptr)
			}
			for i := 0; i < 2; i++ {
				out = s7MainNaturalPre(t, root)
				for _, stale := range []string{"S7 Main ReviewPlan questionnaire:", "questionnaire observed at", "review-plan: registered", "already registered", "not prepared"} {
					if strings.Contains(out, stale) {
						t.Fatalf("accepted Plan repeated planning duty %q: %s", stale, out)
					}
				}
				if got := s7ReviewPlanRegistrations(t, root); got != 1 {
					t.Fatalf("ordinary observation repeated registration: %d", got)
				}
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
					t.Fatalf("ordinary observation changed professional answer: %v", err)
				}
			}
		})
	}
}
