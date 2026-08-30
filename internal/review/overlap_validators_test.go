package review

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// §14.1 overlap / cold-start overload validator tests.
//
// These tests exercise the deterministic mechanical gates added to
// ValidatePlan; they construct plan shapes directly and never touch the
// shared fixture, so the rules stay readable. The shape was pre-published in
// plan.go comments and the brief requires the rejection messages name both
// the gap and the next action.
// ---------------------------------------------------------------------------

func planWithClaimsAndAssignments(claims []Claim, assignments []PlanAssignment, e2eState string) *Plan {
	workspace := "e2e-workspace/plan-t-overlap"
	var workspaceRef *string
	if e2eState == "cold_start" {
		workspaceRef = &workspace
	}
	// Ensure the §4.2 zero-claim guard doesn't pre-empt the validator under
	// test. The helper adds delivery + qa stubs unless the test already
	// supplied its own required claims for those lenses, so ValidatePlan
	// always reaches the overlap / overload branches the test targets.
	if !lensHasRequired(claims, "delivery") {
		claims = append(claims, Claim{
			ClaimID: "claim-dv-stub", Lens: "delivery", Target: "internal/example",
			Method: "traceability", Oracle: "REQ covered",
			Applicability: "required", SourceRefs: []string{"REQ-1"},
		})
		assignments = append(assignments, PlanAssignment{
			AssignmentID: "assignment-dv-stub", Lens: "delivery",
			ClaimIDs:           []string{"claim-dv-stub"},
			NonOverlapBoundary: "owns traceability stub", ExecutionWave: "static",
		})
	}
	if !lensHasRequired(claims, "qa") {
		claims = append(claims, Claim{
			ClaimID: "claim-qa-stub", Lens: "qa", Target: "internal/example",
			Method: "code review", Oracle: "stub",
			Applicability: "required", SourceRefs: []string{"REQ-1"},
		})
		assignments = append(assignments, PlanAssignment{
			AssignmentID: "assignment-qa-stub", Lens: "qa",
			ClaimIDs:           []string{"claim-qa-stub"},
			NonOverlapBoundary: "owns code-quality stub", ExecutionWave: "static",
		})
	}
	return &Plan{
		SchemaVersion:                 "1.0.0",
		ReviewPlanID:                  "review-plan-overlap",
		ReviewRound:                   1,
		BaselineGeneration:            1,
		FrozenSubjects:                []FrozenSubject{{Path: "internal/example/service.go", SHA256: strings.Repeat("1", 64)}},
		Claims:                        claims,
		Assignments:                   assignments,
		E2ECoverageState:              e2eState,
		VerificationArtifactWorkspace: workspaceRef,
		DispatchCapacityPolicy:        "coverage_complete",
		CreatedBy:                     "test",
		CreatedAt:                     "2026-08-23T00:00:00Z",
	}
}

func lensHasRequired(claims []Claim, lens string) bool {
	for _, claim := range claims {
		if claim.Lens == lens && claim.Applicability == "required" {
			return true
		}
	}
	return false
}

// §14.1: 三个 QA Agents 都执行"全量代码 review"——overlap validator 拒绝，
// 要求 focus/non-overlap/oracle 区分或合并。
func TestValidatePlanRejectsDuplicatedGenericReview(t *testing.T) {
	target := "internal/example"
	method := "code review"
	oracle := "no dropped error"
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-qa-a1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-a2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-b1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-b2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-c1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-c2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-na", Lens: "e2e", Target: "n/a", Applicability: "not_applicable",
				NARationale: "pure internal change", NAChecklistID: "REQ-1#ui_impact", SourceRefs: []string{"REQ-1#ui"}},
		},
		[]PlanAssignment{
			// Identical non_overlap_boundary on all three — the validator's
			// only escape hatch (mutually distinct boundaries) is closed,
			// so the duplication must be rejected.
			{AssignmentID: "assignment-qa-a", Lens: "qa", ClaimIDs: []string{"claim-qa-a1", "claim-qa-a2"},
				NonOverlapBoundary: "owns the whole module end to end", ExecutionWave: "static"},
			{AssignmentID: "assignment-qa-b", Lens: "qa", ClaimIDs: []string{"claim-qa-b1", "claim-qa-b2"},
				NonOverlapBoundary: "owns the whole module end to end", ExecutionWave: "static"},
			{AssignmentID: "assignment-qa-c", Lens: "qa", ClaimIDs: []string{"claim-qa-c1", "claim-qa-c2"},
				NonOverlapBoundary: "owns the whole module end to end", ExecutionWave: "static"},
		},
		"not_applicable",
	)
	err := ValidatePlan(plan)
	if err == nil {
		t.Fatal("three same-lens Assignments with identical targets/methods/oracles AND identical non_overlap_boundary must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "duplicated generic review") {
		t.Fatalf("rejection must name the gap, got %v", err)
	}
	if !strings.Contains(msg, "non_overlap_boundary") {
		t.Fatalf("rejection must point at the next action (write distinct non_overlap_boundary), got %v", err)
	}
	if !strings.Contains(msg, "Merge") {
		t.Fatalf("rejection must offer merge as the other next action, got %v", err)
	}
}

// §14.1: 不同 oracle 的独立视角保留——overlap validator 看到不同 oracle
// 立即放行，绝不以"读了相同文件"为由合并。
func TestValidatePlanAllowsSameReadSetWithDifferentOracle(t *testing.T) {
	target := "internal/example"
	method := "code review"
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-qa-1", Lens: "qa", Target: target, Method: method, Oracle: "no dropped error", Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-2", Lens: "qa", Target: target, Method: method, Oracle: "no orphan transition", Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-3", Lens: "qa", Target: target, Method: method, Oracle: "no unsafe permission", Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-4", Lens: "qa", Target: target, Method: method, Oracle: "no resource leak", Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-na", Lens: "e2e", Target: "n/a", Applicability: "not_applicable",
				NARationale: "pure internal change", NAChecklistID: "REQ-1#ui_impact", SourceRefs: []string{"REQ-1#ui"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-qa-1", Lens: "qa", ClaimIDs: []string{"claim-qa-1", "claim-qa-2"},
				NonOverlapBoundary: "error/state oracle", ExecutionWave: "static"},
			{AssignmentID: "assignment-qa-2", Lens: "qa", ClaimIDs: []string{"claim-qa-3", "claim-qa-4"},
				NonOverlapBoundary: "security/resource oracle", ExecutionWave: "static"},
		},
		"not_applicable",
	)
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("different oracles must be preserved, got %v", err)
	}
}

// §14.1: 双方都写了互不相同的 non_overlap_boundary，且各自拥有不相交的
// focus 维度（target+method 集合相同但分区真实存在）——放行。S7-6/RC-07：
// 纯 prose 边界不再是逃生门。
func TestValidatePlanAllowsDistinctNonOverlapBoundaries(t *testing.T) {
	target := "internal/example"
	method := "code review"
	oracle := "no dropped error"
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-qa-a1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", FocusKey: "error-propagation", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-a2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", FocusKey: "error-propagation", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-b1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", FocusKey: "state-machine", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-b2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", FocusKey: "state-machine", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-na", Lens: "e2e", Target: "n/a", Applicability: "not_applicable",
				NARationale: "pure internal change", NAChecklistID: "REQ-1#ui_impact", SourceRefs: []string{"REQ-1#ui"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-qa-a", Lens: "qa", ClaimIDs: []string{"claim-qa-a1", "claim-qa-a2"},
				FocusKeys:          []string{"error-propagation"},
				NonOverlapBoundary: "owns error propagation logic", ExecutionWave: "static"},
			{AssignmentID: "assignment-qa-b", Lens: "qa", ClaimIDs: []string{"claim-qa-b1", "claim-qa-b2"},
				FocusKeys:          []string{"state-machine"},
				NonOverlapBoundary: "owns state-machine transitions", ExecutionWave: "static"},
		},
		"not_applicable",
	)
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("mutually distinct non_overlap_boundary with a real focus partition must release the pair, got %v", err)
	}
}

// §14.1 / S7-6 (RC-07): 互不相同的 non_overlap_boundary 只有 prose、
// target+method 集合完全一致且无任何结构分区——拒收，prose 不能替代分区。
func TestValidatePlanRejectsProseOnlyNonOverlapBoundary(t *testing.T) {
	target := "internal/example"
	method := "code review"
	oracle := "no dropped error"
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-qa-a1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-a2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-b1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-b2", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-na", Lens: "e2e", Target: "n/a", Applicability: "not_applicable",
				NARationale: "pure internal change", NAChecklistID: "REQ-1#ui_impact", SourceRefs: []string{"REQ-1#ui"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-qa-a", Lens: "qa", ClaimIDs: []string{"claim-qa-a1", "claim-qa-a2"},
				NonOverlapBoundary: "owns error propagation logic", ExecutionWave: "static"},
			{AssignmentID: "assignment-qa-b", Lens: "qa", ClaimIDs: []string{"claim-qa-b1", "claim-qa-b2"},
				NonOverlapBoundary: "owns state-machine transitions", ExecutionWave: "static"},
		},
		"not_applicable",
	)
	err := ValidatePlan(plan)
	if err == nil {
		t.Fatal("prose-only non_overlap_boundary over an identical target+method set must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "non_overlap_boundary is prose, not a partition") {
		t.Fatalf("rejection must name the prose-escape gap, got %v", err)
	}
	if !strings.Contains(msg, "focus_key") || !strings.Contains(msg, "merge") {
		t.Fatalf("rejection must point at the structural split or merge, got %v", err)
	}
}

// §14.1: 不同 lens/persona 即使 read set 相同也不合并。
func TestValidatePlanAllowsSameReadSetAcrossLenses(t *testing.T) {
	target := "internal/example"
	method := "code review"
	oracle := "no dropped error"
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-dv-1", Lens: "delivery", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-qa-1", Lens: "qa", Target: target, Method: method, Oracle: oracle, Applicability: "required", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-na", Lens: "e2e", Target: "n/a", Applicability: "not_applicable",
				NARationale: "pure internal change", NAChecklistID: "REQ-1#ui_impact", SourceRefs: []string{"REQ-1#ui"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-dv-1", Lens: "delivery", ClaimIDs: []string{"claim-dv-1"},
				NonOverlapBoundary: "owns traceability", ExecutionWave: "static"},
			{AssignmentID: "assignment-qa-1", Lens: "qa", ClaimIDs: []string{"claim-qa-1"},
				NonOverlapBoundary: "owns code-quality", ExecutionWave: "static"},
		},
		"not_applicable",
	)
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("different lenses must never merge, got %v", err)
	}
}

// §14.1: E2E cold_start，多 persona/入口/状态/负向路径却只生成一个全需求
// Assignment——overload validator 拒绝并要求先扩 coverage matrix。
func TestValidatePlanRejectsColdStartSingleAssignmentOverload(t *testing.T) {
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-e2e-admin-positive", Lens: "e2e", Target: "admin/positive", Method: "browser",
				Oracle: "admin creates order successfully", Applicability: "required",
				FocusKey:   "persona=admin|flow=order-create|sign=positive",
				SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-user-negative", Lens: "e2e", Target: "user/negative", Method: "browser",
				Oracle: "user sees rejected state on invalid input", Applicability: "required",
				FocusKey:   "persona=user|flow=order-create|sign=negative",
				SourceRefs: []string{"REQ-1"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-e2e-all", Lens: "e2e",
				ClaimIDs:           []string{"claim-e2e-admin-positive", "claim-e2e-user-negative"},
				NonOverlapBoundary: "owns every e2e flow in one pass", ExecutionWave: "behavior"},
		},
		"cold_start",
	)
	err := ValidatePlan(plan)
	if err == nil {
		t.Fatal("cold-start single e2e Assignment spanning two focus dimensions must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cold_start") {
		t.Fatalf("rejection must name the cold-start state, got %v", err)
	}
	if !strings.Contains(msg, "persona=admin|flow=order-create|sign=positive") &&
		!strings.Contains(msg, "persona=user|flow=order-create|sign=negative") {
		t.Fatalf("rejection must list the conflicting focus dimensions, got %v", err)
	}
	if !strings.Contains(msg, "Expand the coverage matrix") {
		t.Fatalf("rejection must point at the next action (expand coverage matrix), got %v", err)
	}
}

// S7-6/RC-07: an empty focus_key is not a free pass. A cold-start plan whose
// required e2e Claims declare no focus_key at all previously bypassed the
// overload gate (dimensions < 2 read as "not overloaded"); now the missing
// dimension carrier IS the overload and registration is rejected.
func TestValidatePlanRejectsColdStartClaimsAllWithEmptyFocusKey(t *testing.T) {
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-e2e-flow-a", Lens: "e2e", Target: "flow a", Method: "browser",
				Oracle: "flow a completes", Applicability: "required",
				SourceRefs: []string{"REQ-1"}}, // no FocusKey
			{ClaimID: "claim-e2e-flow-b", Lens: "e2e", Target: "flow b", Method: "browser",
				Oracle: "flow b completes", Applicability: "required",
				SourceRefs: []string{"REQ-1"}}, // no FocusKey
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-e2e-all", Lens: "e2e",
				ClaimIDs:           []string{"claim-e2e-flow-a", "claim-e2e-flow-b"},
				NonOverlapBoundary: "owns every e2e flow in one pass", ExecutionWave: "behavior"},
		},
		"cold_start",
	)
	err := ValidatePlan(plan)
	if err == nil {
		t.Fatal("cold-start plan with all required e2e claims lacking focus_key must be rejected as overload")
	}
	msg := err.Error()
	if !strings.Contains(msg, "empty focus_key is overload") {
		t.Fatalf("rejection must name empty focus_key as overload (S7-6/RC-07), got %v", err)
	}
	if !strings.Contains(msg, "assignment-e2e-all") {
		t.Fatalf("rejection must name the overloaded assignment, got %v", err)
	}
}

// §14.1: 单一 persona 单一 flow cluster——放行。
func TestValidatePlanAllowsColdStartSingleDimension(t *testing.T) {
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-e2e-admin-positive-1", Lens: "e2e", Target: "admin/positive/1", Method: "browser",
				Oracle: "admin happy path step 1", Applicability: "required",
				FocusKey:   "persona=admin|flow=order",
				SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-admin-positive-2", Lens: "e2e", Target: "admin/positive/2", Method: "browser",
				Oracle: "admin happy path step 2", Applicability: "required",
				FocusKey:   "persona=admin|flow=order",
				SourceRefs: []string{"REQ-1"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-e2e-one", Lens: "e2e",
				ClaimIDs:           []string{"claim-e2e-admin-positive-1", "claim-e2e-admin-positive-2"},
				NonOverlapBoundary: "owns the admin order happy path", ExecutionWave: "behavior"},
		},
		"cold_start",
	)
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("single persona/flow cluster must be released, got %v", err)
	}
}

// §14.1: 已经把 E2E 拆成两个 Assignment——overload validator 不再触发。
func TestValidatePlanAllowsColdStartSplitAssignments(t *testing.T) {
	plan := planWithClaimsAndAssignments(
		[]Claim{
			{ClaimID: "claim-e2e-admin", Lens: "e2e", Target: "admin", Method: "browser",
				Oracle: "admin happy", Applicability: "required",
				FocusKey: "persona=admin", SourceRefs: []string{"REQ-1"}},
			{ClaimID: "claim-e2e-user", Lens: "e2e", Target: "user", Method: "browser",
				Oracle: "user happy", Applicability: "required",
				FocusKey: "persona=user", SourceRefs: []string{"REQ-1"}},
		},
		[]PlanAssignment{
			{AssignmentID: "assignment-e2e-admin", Lens: "e2e", ClaimIDs: []string{"claim-e2e-admin"},
				NonOverlapBoundary: "admin persona", ExecutionWave: "behavior"},
			{AssignmentID: "assignment-e2e-user", Lens: "e2e", ClaimIDs: []string{"claim-e2e-user"},
				NonOverlapBoundary: "user persona", ExecutionWave: "behavior"},
		},
		"cold_start",
	)
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("multiple E2E Assignments must release the overload validator, got %v", err)
	}
}
