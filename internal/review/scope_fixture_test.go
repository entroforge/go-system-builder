package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/testfixture"
)

func scopeReviewFixture(t *testing.T, root string, state, plan map[string]any) {
	t.Helper()
	testfixture.ReviewProject(t, root, state, plan)
	workspace, _ := plan["verification_artifact_workspace"].(string)
	prepared, err := prepareModuleScope(root, state, intField(plan["review_round"]), workspace)
	if err != nil {
		t.Fatal(err)
	}
	testfixture.ScopePlan(t, plan, prepared.scope)
	writeState(t, root, state)
}

func scopeReviewFixtureFile(t *testing.T, root string, plan map[string]any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	scopeReviewFixture(t, root, state, plan)
}
