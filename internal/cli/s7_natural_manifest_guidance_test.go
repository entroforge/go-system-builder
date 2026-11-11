package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/review"
)

func TestS7NaturalManifestGuidanceUsesOriginalPlanWithoutRound(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "review": map[string]any{
		"round_entry": map[string]any{"plan_id": "current-plan/a"},
		"plan":        map[string]any{"plan_id": "current-plan/a"},
	}}
	first := s7ManifestPreparationHint(root, state, "assignment-qa")
	if !strings.Contains(first, "manifest-draft") || strings.Contains(first, "restore the current ReviewPlan") || strings.Contains(first, "-r0-") {
		t.Fatalf("missing natural no-round preparation: %s", first)
	}
	other := map[string]any{"baseline": map[string]any{"generation": 1}, "review": map[string]any{
		"round_entry": map[string]any{"plan_id": "current-plan/b"},
		"plan":        map[string]any{"plan_id": "current-plan/b"},
	}}
	second := s7ManifestPreparationHint(root, other, "assignment-qa")
	if first == second {
		t.Fatalf("distinct plans reused the same preparation path: %s", first)
	}
	dir := s7ManifestIdentityDir(filepath.Join(root, ".claude/workgroups/plans"), "current-plan/a")
	dir = s7ManifestIdentityDir(filepath.Join(dir, "assignments"), "assignment-qa")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte("unknown original"), 0600); err != nil {
		t.Fatal(err)
	}
	hint := s7ManifestPreparationHint(root, state, "assignment-qa")
	if !strings.Contains(hint, filepath.Join(dir, "manifest-2.json")) || !strings.Contains(hint, "manifest-draft") {
		t.Fatalf("unknown file blocked current preparation: %s", hint)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "unknown original" {
		t.Fatalf("unknown file changed: %q %v", data, err)
	}
}

func TestS7NaturalManifestGuidanceReusesCurrentPlanOnly(t *testing.T) {
	root := t.TempDir()
	makeState := func(plan string) map[string]any {
		return map[string]any{"runtime_id": "runtime-current", "baseline": map[string]any{"generation": 1}, "review": map[string]any{
			"round_entry": map[string]any{"plan_id": plan}, "plan": map[string]any{"plan_id": plan},
		}}
	}
	state := makeState("plan-one")
	dir := s7ManifestIdentityDir(filepath.Join(root, ".claude/workgroups/plans"), "plan-one")
	dir = s7ManifestIdentityDir(filepath.Join(dir, "assignments"), "assignment-qa")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	draft := review.ManifestDraft{ManifestID: review.ManifestIDForPlan("plan-one", "assignment-qa"), RuntimeID: "runtime-current", BaselineGeneration: 1,
		Assignments: []review.ManifestAssignment{{AssignmentID: "assignment-qa"}},
		Documents:   []review.ManifestDocument{{ID: "TASK-001-01", Path: "docs/task.md"}}}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	hint := s7ManifestPreparationHint(root, state, "assignment-qa")
	if !strings.Contains(hint, "preserve and read existing manifest") || !strings.Contains(hint, "register-workgroup") || strings.Contains(hint, "manifest-2.json") {
		t.Fatalf("same responsibility was not reused: %s", hint)
	}
	if _, err := readS7ExistingManifest(root, path, makeState("plan-two"), "assignment-qa"); err == nil {
		t.Fatal("a different plan accepted the first plan's manifest")
	}
	revised := makeState("plan-one")
	revised["review"].(map[string]any)["assignments"] = map[string]any{"assignment-qa": map[string]any{"claim_ids": []any{"new-claim"}, "lens": "qa"}}
	if next := s7ManifestPreparationHint(root, revised, "assignment-qa"); !strings.Contains(next, filepath.Join(dir, "manifest-2.json")) {
		t.Fatalf("changed Claim set reused the old draft: %s", next)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(data) {
		t.Fatalf("revised plan altered the original draft: %q %v", after, err)
	}
}

func TestS7NaturalPlanGuideProvidesOptionalAssetShape(t *testing.T) {
	guide := reviewPlanGuide(nil)
	for _, want := range []string{"e2e_assets", "case_ref", "path", "selector_ref", "route_ref", "environment", "omit the list", "workgroup registration"} {
		if !strings.Contains(guide, want) {
			t.Fatalf("guide is missing %q: %s", want, guide)
		}
	}
	for _, forbidden := range []string{"review-plan.schema.json", "must select", "round >=", "asset IDs/digests are optional"} {
		if strings.Contains(guide, forbidden) {
			t.Fatalf("guide retained obsolete instruction %q: %s", forbidden, guide)
		}
	}
}

func TestS7ManifestReuseFollowsOriginalProfessionalRevision(t *testing.T) {
	root := t.TempDir()
	old := review.Plan{ReviewPlanID: "review-plan-one", BaselineGeneration: 1,
		Claims: []review.Claim{
			{ClaimID: "claim-qa", Lens: "qa", Target: "x", Assertion: "old judgment"},
			{ClaimID: "claim-dv", Lens: "delivery", Target: "y", Assertion: "delivery judgment"},
		},
		Assignments: []review.PlanAssignment{
			{AssignmentID: "assignment-qa", Lens: "qa", ClaimIDs: []string{"claim-qa"}},
			{AssignmentID: "assignment-dv", Lens: "delivery", ClaimIDs: []string{"claim-dv"}},
		},
	}
	plans := filepath.Join(root, ".claude/review/plans")
	if err := os.MkdirAll(plans, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(plans, "review-plan-one.json"), old)
	current := old
	current.Claims = append([]review.Claim(nil), old.Claims...)
	current.Claims[1].Assertion = "other responsibility changed"
	currentPath := filepath.Join(plans, "review-plan-one-r2.json")
	write(currentPath, current)
	state := map[string]any{"runtime_id": "loop-REQ-001", "baseline": map[string]any{"generation": 1}, "review": map[string]any{
		"round_entry": map[string]any{"plan_id": "review-plan-one"},
		"plan":        map[string]any{"plan_id": "review-plan-one", "path": currentPath, "revision": 2, "status": "running"},
		"assignments": map[string]any{"assignment-qa": map[string]any{"status": "planned", "lens": "qa", "claim_ids": []any{"claim-qa"}}},
	}}
	dir := s7ManifestIdentityDir(filepath.Join(root, ".claude/workgroups/plans"), "review-plan-one")
	dir = s7ManifestIdentityDir(filepath.Join(dir, "assignments"), "assignment-qa")
	path := filepath.Join(dir, "manifest.json")
	draft := review.ManifestDraft{ManifestID: review.ManifestIDForPlan("review-plan-one", "assignment-qa"), RuntimeID: "loop-REQ-001", BaselineGeneration: 1,
		WorkgroupKind: "qa", Assignments: []review.ManifestAssignment{{AssignmentID: "assignment-qa", ClaimIDs: []string{"claim-qa"}}}}
	write(path, draft)
	if hint := s7ManifestPreparationHint(root, state, "assignment-qa"); !strings.Contains(hint, "preserve and read existing manifest") {
		t.Fatalf("unaffected original draft not reused: %s", hint)
	}
	current.Claims[0].Assertion = "changed judgment with same Claim ID"
	write(currentPath, current)
	if hint := s7ManifestPreparationHint(root, state, "assignment-qa"); !strings.Contains(hint, filepath.Join(dir, "manifest-2.json")) {
		t.Fatalf("changed professional content reused the old draft: %s", hint)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("original draft was lost: %v", err)
	}
}
