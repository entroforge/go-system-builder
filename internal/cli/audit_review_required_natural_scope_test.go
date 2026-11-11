package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
)

// The upstream clean review is the existing synthetic S10 fixture; this test
// exercises the real S10 answer, impact consumer, TR-016 and new S7 preparer.
func TestS10ReviewRequiredNewPlanWithoutFrozenSubject(t *testing.T) {
	const originalPlanID = "review-plan-natural-s10"
	const changedPath = "internal/service.go"
	root, answer := s10QuestionnaireHookRootWithIdentity(t, originalPlanID)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	state := readS10QuestionnaireState(t, statePath)
	pointer := state["review"].(map[string]any)["plan"].(map[string]any)
	planRef := pointer["path"].(string)
	original := readS10QuestionnaireState(t, filepath.Join(root, planRef))
	original["schema_version"] = "1.1.0"
	original["frozen_subjects"] = []any{}
	original["coverage_inventory"] = []any{map[string]any{
		"id": "change-service", "kind": "post_repair_change", "source_ref": changedPath,
		"target": changedPath, "lens": "qa",
	}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	writeS10QuestionnaireFile(t, root, planRef, data)
	pointer["sha256"] = sha256Hex(data)
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeS10QuestionnaireFile(t, root, ".claude/loop-state.json", data)
	fillS10QuestionnaireAnswer(t, answer, "acceptance", "review_required")

	out := saveS10ReviewAnswer(t, root, answer)
	state = readS10QuestionnaireState(t, statePath)
	lifecycle := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "verification" || !strings.Contains(out, "TR-016") {
		t.Fatalf("received S10 decision did not enter new S7: state=%v output=%s", lifecycle, out)
	}
	if !hasS10QuestionnaireEvidence(root, state, "acceptance", "review_required") {
		t.Fatal("original S10 professional decision was not preserved")
	}
	foundScope := false
	for _, raw := range state["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] != "change_impact" {
			continue
		}
		scopes, _ := row["scope_refs"].([]any)
		for _, scope := range scopes {
			foundScope = foundScope || scope == changedPath
		}
	}
	if !foundScope {
		t.Fatalf("original changed path did not reach the TR-016 impact: %s", out)
	}
	path := review.PlanQuestionnairePath(root, state)
	if path == "" || !strings.Contains(out, path) {
		t.Fatalf("Main did not receive the exact new ReviewPlan path: %q; output=%s", path, out)
	}
	newPlan, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("new ReviewPlan not prepared: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(newPlan, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["review_plan_id"] == originalPlanID {
		t.Fatal("new verification reused the previous Plan identity")
	}
	if !strings.Contains(string(newPlan), changedPath) {
		t.Fatalf("new ReviewPlan lost changed-path review guidance: %s", newPlan)
	}
}
