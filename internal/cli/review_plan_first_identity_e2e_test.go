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

func TestMainCurrentPlanFirstNaturalReceptionUsesEntryIdentity(t *testing.T) {
	for _, value := range []string{"missing", "old"} {
		t.Run(value, func(t *testing.T) {
			root := worktreeProjectRoot(t)
			state := stageSnapshot(t, root).State
			const id = "review-plan-first-original-entry"
			state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": id, "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			path := review.PlanQuestionnairePath(root, state)
			s7MainNaturalPre(t, root)
			draftBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			professionalPath := minimalReviewPlan(t, t.TempDir(), "review-plan-other")
			professionalBytes, err := os.ReadFile(professionalPath)
			if err != nil {
				t.Fatal(err)
			}
			var draft, professional map[string]any
			if err := json.Unmarshal(draftBytes, &draft); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(professionalBytes, &professional); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"claims", "assignments", "e2e_coverage_state"} {
				draft[field] = professional[field]
			}
			if value == "missing" {
				delete(draft, "review_plan_id")
			} else {
				draft["review_plan_id"] = "review-plan-old-entry"
			}
			write := func() []byte {
				t.Helper()
				data, err := json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				return data
			}
			original := write()
			out := entryQuestionnaireHook(t, root, "PostToolUse", path)
			if !strings.Contains(out, "review-plan: registered "+id) {
				t.Fatalf("first original reception: %s", out)
			}
			if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
				t.Fatal("reception changed Main's original file")
			}
			ptr := review.PlanPointerFromState(stageSnapshot(t, root).State)
			if ptr == nil || ptr.PlanID != id {
				t.Fatalf("incorrect entry projection: %+v", ptr)
			}
			entryQuestionnaireHook(t, root, "PostToolUse", path)
			if got := s7ReviewPlanRegistrations(t, root); got != 1 {
				t.Fatalf("same answer registered %d times", got)
			}
			draft["claims"].([]any)[0].(map[string]any)["assertion"] = "new actual judgment"
			revised := write()
			out = entryQuestionnaireHook(t, root, "PostToolUse", path)
			if !strings.Contains(out, "review-plan: revised "+id) {
				t.Fatalf("natural revision: %s", out)
			}
			accepted, _, err := review.LoadPlan(root, stageSnapshot(t, root).State)
			if err != nil || accepted.Claims[0].Assertion != "new actual judgment" {
				t.Fatalf("revised professional content: %+v %v", accepted, err)
			}
			if got, _ := os.ReadFile(path); !bytes.Equal(got, revised) {
				t.Fatal("revision changed Main's original file")
			}
		})
	}
}
