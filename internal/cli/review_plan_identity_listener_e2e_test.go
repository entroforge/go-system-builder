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

// TestMainReviewPlanIdentityNaturalReceptionAndReplay pins the Main ReviewPlan
// responsibility as the current ReviewPlan identity actually provides it.
//
// L3-S7 §7.1: the questionnaire path is generated along this ReviewPlan
// identity and does not embed a review number; the old
// `S7/r<round>/review-plan.json` runtime wiring is removed and no parallel
// path is kept. L4-state-transition-core §"原 ReviewPlan 身份及实际关联" and
// L4-revision-usage §"原 ReviewPlan 身份": the old `review.round` production,
// consumption and compatibility reads are deleted, so no round number is
// produced or accumulated.
//
// A Runtime that still carries only the removed numeric round identity has no
// ReviewPlan identity at all: it owns no questionnaire path and no planning
// duty. A Runtime that entered S7 for real carries the Plan identity written
// by the S7 entry transition (internal/transition/actions.go:250,263) and keeps
// the original reception and replay duty.
func TestMainReviewPlanIdentityNaturalReceptionAndReplay(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		root := worktreeProjectRoot(t)
		state := stageSnapshot(t, root).State
		facts, _ := state["review"].(map[string]any)
		if facts == nil {
			t.Fatal("review projection missing")
		}
		if _, ok := facts["round"]; !ok {
			t.Fatal("legacy round identity fixture lost review.round")
		}
		if _, ok := facts["round_entry"]; ok {
			t.Fatal("legacy round identity fixture must not carry a ReviewPlan identity")
		}
		// The questionnaire path is generated along the current ReviewPlan
		// identity. Without one there is no path at all, and the removed
		// round-based wiring must not be resurrected as a parallel path.
		if path := review.PlanQuestionnairePath(root, state); path != "" {
			t.Fatalf("round-only identity still produced a questionnaire path: %s", path)
		}
		out := s7MainNaturalPre(t, root)
		for _, absent := range []string{
			"S7 Main ReviewPlan questionnaire: ",
			"S7/r1/review-plan.json",
			"questionnaire observed at",
			"review-plan: registered",
		} {
			if strings.Contains(out, absent) {
				t.Fatalf("round-only identity still produced the removed planning duty %q: %s", absent, out)
			}
		}
		// The listener reports the real gap instead of guessing a path.
		if !strings.Contains(out, "S7 Main ReviewPlan questionnaire is unavailable: Main must restore the original current review identity and generation. No path was guessed.") {
			t.Fatalf("missing ReviewPlan identity was not reported truthfully: %s", out)
		}
		if !strings.Contains(out, "REQ authority root is not bound") {
			t.Fatalf("the actual control gap was not reported: %s", out)
		}
		// No questionnaire file was prepared and no Plan was registered.
		prepared := 0
		_ = filepath.WalkDir(filepath.Join(root, ".claude", "submissions"), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !entry.IsDir() && entry.Name() == "review-plan.json" {
				prepared++
			}
			return nil
		})
		if prepared != 0 {
			t.Fatalf("round-only identity prepared %d questionnaire files", prepared)
		}
		after := stageSnapshot(t, root).State
		if ptr := review.PlanPointerFromState(after); ptr != nil {
			t.Fatalf("round-only identity registered a ReviewPlan: %+v", ptr)
		}
		if afterFacts, _ := after["review"].(map[string]any); afterFacts != nil {
			if _, ok := afterFacts["round_entry"]; ok {
				t.Fatalf("ordinary observation manufactured a ReviewPlan identity: %+v", afterFacts["round_entry"])
			}
		}
		if got := s7ReviewPlanRegistrations(t, root); got != 0 {
			t.Fatalf("round-only identity registered %d ReviewPlans", got)
		}
	})

	t.Run("plan", func(t *testing.T) {
		root := worktreeProjectRoot(t)
		const planID = "review-plan-natural-original"
		statePath := filepath.Join(root, ".claude/loop-state.json")
		state := stageSnapshot(t, root).State
		// Every real S7 entry writes the current ReviewPlan identity
		// (internal/transition/actions.go:250,263). The fixture stamps that
		// identity the same way the transition does.
		state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": planID, "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
		seeded, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, seeded, 0600); err != nil {
			t.Fatal(err)
		}
		path := review.PlanQuestionnairePath(root, state)
		if path == "" {
			t.Fatal("current ReviewPlan identity produced no questionnaire path")
		}
		if !strings.HasSuffix(path, filepath.Join("S7", planID, "review-plan.json")) {
			t.Fatalf("questionnaire path is not generated along the ReviewPlan identity: %s", path)
		}
		if strings.Contains(path, string(filepath.Separator)+"r1"+string(filepath.Separator)) {
			t.Fatalf("questionnaire path returned to the removed round wiring: %s", path)
		}
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
		// The current identity schema carries no review round number.
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
