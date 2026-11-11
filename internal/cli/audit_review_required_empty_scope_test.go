package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

// A real re-review decision may concern the adequacy of previous checks,
// without any asserted product change or frozen subjects. The S10 receiver,
// decision writer, TR-016 and S7 preparer are the normal production path.
func TestS10ReviewRequiredWithoutProductChangesEntersNewS7(t *testing.T) {
	const oldPlan = "review-plan-s10-without-product-change"
	root, answer := s10QuestionnaireHookRootWithIdentity(t, oldPlan)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before := readS10QuestionnaireState(t, statePath)
	oldStatus := make(map[string]any)
	for _, raw := range before["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		oldStatus[row["id"].(string)] = row["status"]
	}
	ptr := before["review"].(map[string]any)["plan"].(map[string]any)
	planPath := ptr["path"].(string)
	plan := readS10QuestionnaireState(t, filepath.Join(root, planPath))
	plan["frozen_subjects"] = []any{}
	plan["coverage_inventory"] = []any{}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	writeS10QuestionnaireFile(t, root, planPath, data)
	fillS10QuestionnaireAnswer(t, answer, "acceptance", "review_required")

	out := saveS10ReviewAnswer(t, root, answer)
	after := readS10QuestionnaireState(t, statePath)
	if after["lifecycle"].(map[string]any)["state"] != "verification" || !strings.Contains(out, "TR-016") {
		t.Fatalf("actual S10 re-review decision did not enter S7: %s", out)
	}
	if !hasS10QuestionnaireEvidence(root, after, "acceptance", "review_required") {
		t.Fatal("the original S10 decision was not retained")
	}
	entry := after["review"].(map[string]any)["round_entry"].(map[string]any)
	if entry["plan_id"] == "" || entry["plan_id"] == oldPlan {
		t.Fatalf("new S7 must own a distinct ReviewPlan identity: %#v", entry)
	}
	oldPointer := review.PlanPointerFromState(after)
	if oldPointer == nil || reviewmeta.CompareIdentity(oldPointer.Identity(), reviewmeta.FromReview(after["review"].(map[string]any))) != reviewmeta.Different {
		t.Fatal("new S7 responsibility must not use the old accepted Plan identity")
	}
	newPath := review.PlanQuestionnairePath(root, after)
	if newPath == "" || !strings.Contains(out, newPath) {
		t.Fatalf("new S7 original questionnaire was not prepared: %q; output=%s", newPath, out)
	}
	// A normal next tool event must not turn the old Plan's accepted results
	// into a passing conclusion for the newly opened responsibility.
	callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	current := readS10QuestionnaireState(t, statePath)
	if current["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatalf("old results advanced the new S7 without its own review: %#v", current["lifecycle"])
	}
	for _, raw := range after["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if prior, exists := oldStatus[row["id"].(string)]; exists && row["status"] != prior {
			t.Fatalf("re-review without product change invalidated previous evidence %s: %v -> %v", row["id"], prior, row["status"])
		}
		if row["kind"] == "change_impact" {
			scopes, _ := row["scope_refs"].([]any)
			if len(scopes) != 0 {
				t.Fatalf("re-review without product change fabricated affected paths: %#v", scopes)
			}
		}
	}
}

func TestS10ReviewRequiredWithoutPathsNaturalScriptSaveEntersS7(t *testing.T) {
	root, answer := s10QuestionnaireHookRootWithIdentity(t, "review-plan-before-script-recheck")
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before := readS10QuestionnaireState(t, statePath)
	planRef := before["review"].(map[string]any)["plan"].(map[string]any)["path"].(string)
	plan := readS10QuestionnaireState(t, filepath.Join(root, planRef))
	plan["frozen_subjects"] = []any{}
	plan["coverage_inventory"] = []any{}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	writeS10QuestionnaireFile(t, root, planRef, data)
	fillS10QuestionnaireAnswer(t, answer, "acceptance", "review_required")
	// This event does not identify the answer file in its tool arguments.
	out := callS10QuestionnaireHook(t, root, map[string]any{
		"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Bash",
		"tool_input": map[string]any{"command": "script completed"}, "tool_response": map[string]any{"exit_code": 0},
	})
	state := readS10QuestionnaireState(t, statePath)
	if state["lifecycle"].(map[string]any)["state"] != "verification" || !strings.Contains(out, "TR-016") {
		t.Fatalf("natural script save did not receive the re-review decision and enter S7: %s", out)
	}
	if !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") {
		t.Fatal("the original S10 decision was not retained")
	}
}
