package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRevisionComparesOnlyOriginalResponsibility(t *testing.T) {
	root := t.TempDir()
	initial := Plan{
		ReviewPlanID: "review-plan-original", BaselineGeneration: 1,
		Claims: []Claim{
			{ClaimID: "claim-one", Lens: "delivery", Target: "one", Assertion: "one is covered"},
			{ClaimID: "claim-two", Lens: "qa", Target: "two", Assertion: "two is covered"},
		},
		Assignments: []PlanAssignment{
			{AssignmentID: "assignment-one", Lens: "delivery", ClaimIDs: []string{"claim-one"}},
			{AssignmentID: "assignment-two", Lens: "qa", ClaimIDs: []string{"claim-two"}},
		},
		FrozenSubjects: []FrozenSubject{{Path: "one", Kind: "task"}},
	}
	writePlan := func(name string, p Plan) string {
		t.Helper()
		rel := boundedReviewArtifactPath([]string{".claude", "review", "plans"}, name)
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return rel
	}
	writePlan(initial.ReviewPlanID, initial)
	next := initial
	next.Claims = append([]Claim(nil), initial.Claims...)
	next.FrozenSubjects = append([]FrozenSubject(nil), initial.FrozenSubjects...)
	next.Claims[1].Assertion = "changed judgment for the other responsibility"
	currentPath := writePlan(initial.ReviewPlanID+"-r2", next)
	state := map[string]any{"review": map[string]any{"plan": map[string]any{
		"plan_id": initial.ReviewPlanID, "path": currentPath, "revision": 2, "status": "running",
	}}}
	unchanged, err := ManifestAssignmentUnchangedSince(root, state, "assignment-one", 1)
	if err != nil || !unchanged {
		t.Fatalf("unaffected responsibility was not preserved: unchanged=%t err=%v", unchanged, err)
	}
	unchanged, err = ManifestAssignmentUnchangedSince(root, state, "assignment-two", 1)
	if err != nil || unchanged {
		t.Fatalf("changed assertion with same Claim ID was reused: unchanged=%t err=%v", unchanged, err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(boundedReviewArtifactPath([]string{".claude", "review", "plans"}, initial.ReviewPlanID)))); err != nil {
		t.Fatal(err)
	}
	unchanged, err = ManifestAssignmentUnchangedSince(root, state, "assignment-one", 1)
	if err != nil || unchanged {
		t.Fatalf("missing original responsibility should remain unknown: unchanged=%t err=%v", unchanged, err)
	}
}
