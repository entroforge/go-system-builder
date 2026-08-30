// draft_placeholder_test.go proves the disclosure fixes applied to
// the QA claim scaffolding in DraftPlan (L3-S7 §4.2 planner assist).
// Two contracts are guarded:
//
//  1. When current-generation completion envelopes carry changed
//     paths, the QA claim `target` is the real changed-surface string
//     (or the frozen-subject fallback). The historical placeholder
//     "the current change surface" must not appear anywhere in the
//     emitted plan: a reviewer reading the draft must know what to
//     review without first re-running the planning pass.
//
//  2. When no completion envelopes and no frozen subjects exist
//     (a sandbox/test/demo state), every QA claim's `target` and
//     `method` carry an explicit TODO marker, and a planner note
//     makes the gap visible. Registration-time validators reject
//     a fake target that names nothing, so surfacing the placeholder
//     at draft time keeps the planner from being surprised by the
//     register-time rejection.
package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestDraftPlanForRootProjectsCanonicalCompletionEnvelope(t *testing.T) {
	root := t.TempDir()
	state := baseDraftState(t)
	state["documents"] = []any{taskFixture("TASK-1", "internal/example/service.go")}
	changedPath := filepath.Join(root, "internal", "api", "handler.go")
	if err := os.MkdirAll(filepath.Dir(changedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changedPath, []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	completionRel := filepath.ToSlash(filepath.Join(".claude", "evidence", "completion.json"))
	completion := []byte(`{"kind":"completion_report","changed_paths":["internal/api/handler.go"],"reviewed_paths":[]}` + "\n")
	completionPath := filepath.Join(root, filepath.FromSlash(completionRel))
	if err := os.MkdirAll(filepath.Dir(completionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completionPath, completion, 0o644); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{map[string]any{
		"id": "completion-1", "kind": "completion_report", "path": completionRel,
		"sha256": sha256Of(completion), "status": "valid", "baseline_generation": 1,
		"scope_refs": []any{},
	}}

	plan, notes := DraftPlanForRoot(root, state, 1)
	if plan == nil {
		t.Fatal("DraftPlanForRoot returned nil plan")
	}
	for _, note := range notes {
		if strings.Contains(note, "baseline projection") {
			t.Fatalf("unexpected baseline projection note: %v", notes)
		}
	}
	if len(plan.CoverageInventory) != 1 || plan.CoverageInventory[0].SourceRef != "internal/api/handler.go" {
		t.Fatalf("coverage_inventory = %+v, want canonical changed path", plan.CoverageInventory)
	}
	foundFrozen := false
	for _, subject := range plan.FrozenSubjects {
		if subject.Path == "internal/api/handler.go" {
			foundFrozen = true
			break
		}
	}
	if !foundFrozen {
		t.Fatalf("frozen_subjects = %+v, want canonical changed path", plan.FrozenSubjects)
	}
	if plan.ChangeImpact == nil || len(plan.ChangeImpact.SourceRefs) != 1 || plan.ChangeImpact.SourceRefs[0] != "internal/api/handler.go" {
		t.Fatalf("change_impact = %+v, want canonical changed path", plan.ChangeImpact)
	}
}

func TestDraftPlanNotApplicableCoverageJustificationMarshalsAsNull(t *testing.T) {
	root := t.TempDir()
	plan, _ := DraftPlanForRoot(root, baseDraftState(t), 1)
	if plan.CoverageJustification != nil {
		t.Fatalf("not-applicable draft coverage_justification = %v, want nil", *plan.CoverageJustification)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	if value, present := encoded["coverage_justification"]; !present || value != nil {
		t.Fatalf("coverage_justification = %#v, want explicit JSON null", value)
	}
}

// baseDraftState returns a schema-valid runtime payload ready for
// DraftPlan to consume. The caller patches documents/evidence per
// scenario; lifecycle and review sections are inert stubs because
// DraftPlan only walks the data it pulls from those blocks.
func baseDraftState(t *testing.T) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	// DraftPlan reads bound_req.metadata.ui_impact for the E2E lens;
	// the example asset uses ui_impact=none which keeps the e2e
	// branch deterministic without re-flowing the case.
	return state
}

func completionEnvelope(scopeRefs []string) map[string]any {
	return map[string]any{
		"kind":                "completion_report",
		"baseline_generation": 1,
		"scope_refs":          anySlice(scopeRefs),
	}
}

func anySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func taskFixture(id, path string) map[string]any {
	return map[string]any{
		"kind":       "task",
		"id":         id,
		"path":       path,
		"generation": 1,
		"sha256":     strings.Repeat("a", 64),
	}
}

// TestDraftPlanQAClaimsUseRealChangedSurface drives the happy path:
// current-generation completion envelopes carry the real changed
// scope, so the QA claim target must enumerate the paths in
// deterministic order (sorted) and the literal placeholder must not
// leak through.
func TestDraftPlanQAClaimsUseRealChangedSurface(t *testing.T) {
	state := baseDraftState(t)
	state["documents"] = []any{taskFixture("TASK-1", "internal/example/service.go")}
	state["evidence"] = []any{
		completionEnvelope([]string{"internal/cli/foo.go", "internal/review/bar.go"}),
		completionEnvelope([]string{"internal/review/bar.go"}), // duplicate
	}
	plan, notes := DraftPlan(state, 1)
	if plan == nil {
		t.Fatal("DraftPlan returned a nil plan")
	}
	qaCount := 0
	for _, claim := range plan.Claims {
		if claim.Lens != "qa" {
			continue
		}
		qaCount++
		if claim.Target == "the current change surface" {
			t.Errorf("QA claim %s leaked the placeholder target", claim.ClaimID)
		}
		// Sorted deterministic surface; both paths must be present.
		for _, want := range []string{"internal/cli/foo.go", "internal/review/bar.go"} {
			if !strings.Contains(claim.Target, want) {
				t.Errorf("QA claim %s missing %q in target %q", claim.ClaimID, want, claim.Target)
			}
		}
	}
	if qaCount == 0 {
		t.Fatal("QA static claims were not scaffolded")
	}
	for _, note := range notes {
		if strings.Contains(note, "QA claim `target` is a TODO marker") {
			t.Errorf("planner note falsely flags a real surface as a TODO marker: %q", note)
		}
	}
}

func TestDraftPlanSplitsQABaselineIntoIndependentAssignments(t *testing.T) {
	state := baseDraftState(t)
	state["documents"] = []any{taskFixture("TASK-1", "internal/example/service.go")}
	plan, _ := DraftPlan(state, 1)
	want := map[string]bool{
		"design-boundary":    false,
		"pattern-idiom-fit":  false,
		"logic-state-error":  false,
		"maintainability":    false,
		"testability-oracle": false,
		"debt-operability":   false,
	}
	assignments := 0
	for _, assignment := range plan.Assignments {
		if assignment.Lens != "qa" {
			continue
		}
		assignments++
		if len(assignment.ClaimIDs) != 1 || len(assignment.FocusKeys) != 1 {
			t.Fatalf("QA assignment must own exactly one focus Claim, got %+v", assignment)
		}
		claim := planClaimByID(plan, assignment.ClaimIDs[0])
		if claim == nil {
			t.Fatalf("QA assignment must reference an existing Claim, assignment=%+v", assignment)
		}
		if _, ok := want[claim.FocusKey]; !ok || assignment.FocusKeys[0] != claim.FocusKey {
			t.Fatalf("QA assignment focus must match one baseline Claim, assignment=%+v claim=%+v", assignment, claim)
		}
		want[claim.FocusKey] = true
	}
	if assignments != len(want) {
		t.Fatalf("DraftPlan must produce one independently dispatchable QA assignment per baseline focus, got %d", assignments)
	}
	for focus, seen := range want {
		if !seen {
			t.Errorf("missing QA baseline focus %q", focus)
		}
	}
}

func planClaimByID(plan *Plan, id string) *Claim {
	for i := range plan.Claims {
		if plan.Claims[i].ClaimID == id {
			return &plan.Claims[i]
		}
	}
	return nil
}

// TestDraftPlanQAClaimsFallbackToFrozenSubjects proves the
// intermediate branch: completion envelopes are absent (sandbox
// state), but the runtime has fingerprinted frozen subjects. The QA
// target must fall back to the frozen-subject paths — the reviewer
// is given real pointer text rather than a placeholder.
func TestDraftPlanQAClaimsFallbackToFrozenSubjects(t *testing.T) {
	state := baseDraftState(t)
	state["documents"] = []any{taskFixture("TASK-1", "internal/example/service.go")}
	state["evidence"] = []any{} // no completion envelopes
	// Round=1 plan must reference a fingerprinted frozen subject;
	// inject one via DraftPlan's normal documents walk by also pinning
	// a TASK above (its path becomes a frozen subject).
	plan, notes := DraftPlan(state, 1)
	if plan == nil {
		t.Fatal("DraftPlan returned a nil plan")
	}
	if len(plan.FrozenSubjects) == 0 {
		t.Fatal("expected frozen subjects from TASK documents; got none")
	}
	qaTargets := []string{}
	for _, claim := range plan.Claims {
		if claim.Lens != "qa" {
			continue
		}
		qaTargets = append(qaTargets, claim.Target)
	}
	if len(qaTargets) == 0 {
		t.Fatal("QA static claims were not scaffolded")
	}
	// Sorted, real path content must show up; no fabricated placeholder.
	for _, target := range qaTargets {
		if target == "the current change surface" {
			t.Errorf("QA target leaked the historical placeholder: %q", target)
		}
		if !strings.Contains(target, "internal/example/service.go") {
			t.Errorf("QA target %q missing the frozen-subject fallback path", target)
		}
	}
	// No TODO-marker note because we DID derive a real surface.
	for _, note := range notes {
		if strings.Contains(note, "QA claim `target` is a TODO marker") {
			t.Errorf("planner note falsely flags the frozen-subject fallback as a TODO marker: %q", note)
		}
	}
}

// TestDraftPlanQAClaimsEmitTODOWhenSurfaceIsUnknown covers the
// disclosure contract: when both completion envelopes and frozen
// subjects are empty, every QA claim's target and method carry an
// explicit TODO marker, and the planner note names the registration-
// time validator so the planner is not surprised by the rejection.
func TestDraftPlanQAClaimsEmitTODOWhenSurfaceIsUnknown(t *testing.T) {
	state := baseDraftState(t)
	state["documents"] = []any{} // no TASK docs, no frozen subjects
	state["evidence"] = []any{}  // no completion envelopes
	plan, notes := DraftPlan(state, 1)
	if plan == nil {
		t.Fatal("DraftPlan returned a nil plan")
	}
	qaFound := 0
	planJSON, _ := json.Marshal(plan)
	for _, claim := range plan.Claims {
		if claim.Lens != "qa" {
			continue
		}
		qaFound++
		if !strings.Contains(claim.Target, "TODO(planner)") {
			t.Errorf("QA claim %s target %q lacks a TODO marker", claim.ClaimID, claim.Target)
		}
		if !strings.Contains(claim.Method, "TODO(planner)") {
			t.Errorf("QA claim %s method %q lacks a TODO marker", claim.ClaimID, claim.Method)
		}
	}
	if qaFound == 0 {
		t.Fatalf("QA claims missing from draft; plan JSON:\n%s", string(planJSON))
	}
	// Sorted deterministic note: the disclosure MUST name the
	// registration-time consequence (the registration gate is what
	// the planner is trying to pass next).
	want := []string{"QA claim `target` is a TODO marker", "registration"}
	haveNote := false
	for _, note := range notes {
		ok := true
		for _, w := range want {
			if !strings.Contains(note, w) {
				ok = false
			}
		}
		if ok {
			haveNote = true
			break
		}
	}
	if !haveNote {
		t.Errorf("planner note disclosing the registration-time rejection not found; notes=%v", notes)
	}
	// Sanity: the literal fictional placeholder must not be used.
	planStr := string(planJSON)
	if strings.Contains(planStr, "the current change surface") {
		t.Errorf("plan JSON still contains the historical placeholder target")
	}
}

// qaChangeSurface is exercised above only via the public DraftPlan;
// the helper itself has its own deterministic-order contract:
// callers (currently DraftPlan) pre-sort changedList; the helper
// does not re-sort. This test enforces the caller-side contract by
// passing a sorted input and asserting the output preserves it.
func TestQAChangeSurfacePreservesSortedChangedPaths(t *testing.T) {
	got, placeholder := qaChangeSurface([]string{"a.go", "b.go", "c.go"}, nil)
	if placeholder {
		t.Fatal("expected a non-placeholder surface")
	}
	want := "a.go, b.go, c.go"
	if got != want {
		t.Fatalf("qaChangeSurface = %q, want %q (preserves sorted input)", got, want)
	}
}

func TestQAChangeSurfaceFallsBackToFrozenSubjects(t *testing.T) {
	frozen := []FrozenSubject{
		{Path: "internal/cli/x.go", Kind: "task"},
		{Path: "internal/review/y.go", Kind: "task"},
	}
	got, placeholder := qaChangeSurface(nil, frozen)
	if placeholder {
		t.Fatal("expected a real surface from frozen subjects, not a placeholder")
	}
	// Sorted, deduplicated, comma-separated.
	paths := strings.Split(got, ", ")
	sortedCopy := append([]string(nil), paths...)
	sort.Strings(sortedCopy)
	if !equalStrings(paths, sortedCopy) {
		t.Errorf("qaChangeSurface not sorted: %v", paths)
	}
}

func TestQAChangeSurfacePlaceholderWhenBothEmpty(t *testing.T) {
	got, placeholder := qaChangeSurface(nil, nil)
	if !placeholder {
		t.Fatalf("expected placeholder flag when both changed and frozen are empty; got %q", got)
	}
	if !strings.Contains(got, "TODO(planner)") {
		t.Errorf("placeholder surface lacks a TODO marker: %q", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
