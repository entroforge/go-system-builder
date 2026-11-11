package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
)

func TestS7NaturalGuideAcceptsTaskWithoutDigest(t *testing.T) {
	for _, value := range []any{nil, "obsolete-wrong", 42, map[string]any{"legacy": true}} {
		root := worktreeProjectRoot(t)
		state := stageSnapshot(t, root).State
		state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-nohash-guide", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
		taskPath := "docs/dev/tasks/TASK-101.md"
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(taskPath)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, taskPath), []byte("# TASK-101\n> Module current truth: `docs/design/prototypes/example/`\n"), 0644); err != nil {
			t.Fatal(err)
		}
		casePath := "docs/design/prototypes/example/cases.json"
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(casePath)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, casePath), []byte(`{"module":"example","cases":[{"id":"CASE-001","title":"actual example","required":true,"browser_required":true}]}`), 0644); err != nil {
			t.Fatal(err)
		}
		task := map[string]any{"id": "TASK-101", "kind": "task", "path": taskPath, "version": "v1", "status": "locked", "generation": 1}
		if value != nil {
			task["sha256"] = value
		}
		state["documents"] = []any{task}
		delete(state["bound_req"].(map[string]any), "metadata")
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		planPath := review.PlanQuestionnairePath(root, state)
		entryQuestionnaireHook(t, root, "PreToolUse", planPath)
		draft, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("natural event did not create original Plan for %#v: %v", value, err)
		}
		if !strings.Contains(string(draft), "docs/design/prototypes/example/cases.json#CASE-001") {
			t.Fatalf("readable original CASE lost for %#v: %s", value, draft)
		}
		guide, err := os.ReadFile(strings.TrimSuffix(planPath, ".json") + ".md")
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"frozen TASK", "must restore", "re-draft", "Main must decide e2e_coverage_state"} {
			if strings.Contains(string(guide), forbidden) {
				t.Fatalf("natural guide requires %q for old SHA %#v: %s", forbidden, value, guide)
			}
		}
	}
}
