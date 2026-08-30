package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// §14.1 plan-validator matrix: the coverage rules that gate registration
// beyond the exact-set partition checks already covered by review_test.go.
// ---------------------------------------------------------------------------

// loadFixturePlan returns a fresh decoded copy of the shared fixture plan.
func loadFixturePlan(t *testing.T) *Plan {
	t.Helper()
	root := t.TempDir()
	data, err := os.ReadFile(writePlanFile(t, root))
	if err != nil {
		t.Fatal(err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	return &plan
}

func dropClaims(plan *Plan, ids ...string) {
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	kept := plan.Claims[:0]
	for _, claim := range plan.Claims {
		if !drop[claim.ClaimID] {
			kept = append(kept, claim)
		}
	}
	plan.Claims = kept
	assignments := plan.Assignments[:0]
	for _, assignment := range plan.Assignments {
		claimIDs := assignment.ClaimIDs[:0]
		for _, claimID := range assignment.ClaimIDs {
			if !drop[claimID] {
				claimIDs = append(claimIDs, claimID)
			}
		}
		assignment.ClaimIDs = claimIDs
		assignments = append(assignments, assignment)
	}
	plan.Assignments = assignments
}

// §14.1: Main 临时把 capacity policy 改成 bounded_flow —— revision/schema
// gate 拒绝；S7 policy 固定为 coverage_complete。
func TestValidatePlanRejectsNonCoverageCompletePolicy(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.DispatchCapacityPolicy = "bounded_flow"
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "coverage_complete") {
		t.Fatalf("bounded_flow policy must be rejected, got %v", err)
	}
}

// §14.1: 有产品实现变化却 DV 或 QA 为 N=0 —— 拒绝；纯文档例外需
// coverage_justification 证明。
func TestValidatePlanRejectsZeroClaimLensWithoutJustification(t *testing.T) {
	// delivery N=0.
	plan := loadFixturePlan(t)
	dropClaims(plan, "claim-dv-1")
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "zero required delivery Claims") {
		t.Fatalf("zero DV claims must be rejected, got %v", err)
	}
	// Docs-only exception: a non-empty coverage_justification legalizes it.
	plan = loadFixturePlan(t)
	dropClaims(plan, "claim-dv-1")
	justification := "pure docs change: TASK-101 touches only docs/reports; no product behavior moved (source: change_impact)"
	plan.CoverageJustification = &justification
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("justified docs-only DV N=0 must pass, got %v", err)
	}
	// qa N=0.
	plan = loadFixturePlan(t)
	dropClaims(plan, "claim-qa-1", "claim-qa-2")
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "zero required qa Claims") {
		t.Fatalf("zero QA claims must be rejected, got %v", err)
	}
	// An empty/whitespace justification is not a justification.
	plan = loadFixturePlan(t)
	dropClaims(plan, "claim-qa-1", "claim-qa-2")
	blank := "   "
	plan.CoverageJustification = &blank
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("blank coverage_justification must not legalize a zero-claim lens")
	}
}

// §14.1: E2E cold_start —— 必须声明隔离写面且至少有一个 required e2e
// Claim；blank matrix 不得压成一个 generic Agent。
func TestValidatePlanColdStartRequirements(t *testing.T) {
	// cold_start without a workspace is rejected.
	plan := loadFixturePlan(t)
	plan.E2ECoverageState = "cold_start"
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "verification_artifact_workspace") {
		t.Fatalf("cold_start without workspace must be rejected, got %v", err)
	}
	// Workspace declared but every e2e claim is N/A: cold start still
	// requires at least one required e2e Claim.
	plan = loadFixturePlan(t)
	plan.E2ECoverageState = "cold_start"
	workspace := "e2e-workspace/plan-t-1"
	plan.VerificationArtifactWorkspace = &workspace
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "at least one required e2e Claim") {
		t.Fatalf("cold_start without a required e2e claim must be rejected, got %v", err)
	}
	// Promote the e2e claim to required and dispatch it: the matrix closes.
	plan = loadFixturePlan(t)
	plan.E2ECoverageState = "cold_start"
	plan.VerificationArtifactWorkspace = &workspace
	for i := range plan.Claims {
		if plan.Claims[i].ClaimID == "claim-e2e-na" {
			plan.Claims[i].Applicability = "required"
			plan.Claims[i].NARationale = ""
			plan.Claims[i].Target = "user login flow"
		}
	}
	plan.Assignments = append(plan.Assignments, PlanAssignment{
		AssignmentID: "assignment-e2e-1", Lens: "e2e", ClaimIDs: []string{"claim-e2e-na"},
		NonOverlapBoundary: "owns the declared flows", ExecutionWave: "behavior",
	})
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("closed cold-start matrix must pass, got %v", err)
	}
}

func TestValidatePlanRejectsRegressionWorkspaceAndMissingE2EClaim(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.E2ECoverageState = "regression_available"
	workspace := "e2e-workspace/should-not-be-used"
	plan.VerificationArtifactWorkspace = &workspace
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "only valid when e2e_coverage_state=cold_start") {
		t.Fatalf("regression_available must not declare an authoring workspace, got %v", err)
	}

	plan = loadFixturePlan(t)
	plan.E2ECoverageState = "regression_available"
	dropClaims(plan, "claim-e2e-na")
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "at least one required e2e Claim") {
		t.Fatalf("regression_available without an executable E2E Claim must be rejected, got %v", err)
	}
}

func TestValidatePlanRegressionRequiresAssetFingerprints(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.E2ECoverageState = "regression_available"
	dropClaims(plan, "claim-e2e-na")
	plan.Claims = append(plan.Claims, Claim{
		ClaimID: "claim-e2e-1", Lens: "e2e", Target: "settings save flow",
		Assertion: "flow behaves as declared", Oracle: "browser oracle", Method: "real browser",
		Applicability: "required", SourceRefs: []string{"REQ-001#settings"}, FocusKey: "settings-save",
	})
	plan.Assignments = append(plan.Assignments, PlanAssignment{
		AssignmentID: "assignment-e2e-1", Lens: "e2e", ClaimIDs: []string{"claim-e2e-1"},
		NonOverlapBoundary: "owns settings save", ExecutionWave: "behavior",
	})
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "S7_E2E_ASSET_FINGERPRINT") {
		t.Fatalf("regression plan without asset fingerprints must be rejected, got %v", err)
	}
	plan.E2EAssets = []E2EAsset{{
		AssetID: "asset-settings-save", CaseRef: "CASE-001", Path: "e2e/settings-save.spec.ts",
		SHA256: strings.Repeat("a", 64),
	}}
	// S7-7 (RC-07): a fingerprint alone is not enough — the asset must also
	// declare the selector/route/environment executability surface.
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "selector/route/environment") {
		t.Fatalf("regression asset without selector/route/environment fingerprint must be rejected, got %v", err)
	}
	plan.E2EAssets[0].SelectorRef = "testid:save-button"
	plan.E2EAssets[0].RouteRef = "settings/save"
	plan.E2EAssets[0].Environment = "chromium/localhost:3000/profile=default"
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("regression plan with a complete asset fingerprint must pass structural validation: %v", err)
	}
}

func TestValidatePlanRejectsPlannerTODOPlaceholder(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.Claims[0].Oracle = "TODO(planner): replace with a concrete oracle"
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "TODO(planner)") {
		t.Fatalf("planner TODO placeholder must be rejected at the plan gate, got %v", err)
	}
}

func TestValidatePlanRejectsUnknownEvidenceRequirement(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.Claims[0].RequiredEvidence = []string{"made_up_evidence_kind"}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "S7_PLAN_EVIDENCE_KIND") {
		t.Fatalf("unknown required evidence kind must be rejected with a recovery diagnostic, got %v", err)
	}
}

// §14.1: E2E 不适用 —— 必须有 impact/source/rationale；没有任何 N/A e2e
// Claim 的 not_applicable 结论被拒绝。
func TestValidatePlanNotApplicableRequiresNAClaim(t *testing.T) {
	plan := loadFixturePlan(t)
	dropClaims(plan, "claim-e2e-na")
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "applicability=not_applicable") {
		t.Fatalf("not_applicable without an N/A e2e claim must be rejected, got %v", err)
	}
}

// §14.1: ReviewPlan 漏掉 Closing Contract obligation —— 每个
// current-generation TASK 必须出现在至少一个 Claim 的 source_refs，
// 缺口在注册时拒绝并指出缺失 source。
func TestRegisterPlanRejectsTaskCoverageGap(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["documents"] = []any{
		map[string]any{
			"id": "TASK-101", "kind": "task", "path": "docs/tasks/TASK-101.md",
			"version": "v1.0.0", "sha256": strings.Repeat("2", 64),
			"status": "locked", "generation": 1,
		},
	}
	statePath, journalPath := writeState(t, root, state)

	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: writePlanFile(t, root),
	})
	if err == nil || !strings.Contains(err.Error(), "TASK-101") {
		t.Fatalf("plan dropping TASK-101 must be rejected with the missing source named, got %v", err)
	}

	// Add TASK-101 to a claim's source_refs: the coverage diff closes and
	// registration succeeds.
	planPath := writePlanFile(t, root)
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	for _, raw := range body["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-dv-1" {
			claim["source_refs"] = append(claim["source_refs"].([]any), "TASK-101")
		}
	}
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: planPath,
	})
	if err != nil {
		t.Fatalf("plan covering TASK-101 must register, got %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr == nil || ptr.Status != "running" {
		t.Fatalf("plan pointer wrong after coverage fix: %+v", ptr)
	}
}

func TestValidateCoverageInventoryRequiresChangedSurfaceFrozenSubject(t *testing.T) {
	root := t.TempDir()
	changedRel := "internal/api/handler.go"
	changedPath := filepath.Join(root, filepath.FromSlash(changedRel))
	if err := os.MkdirAll(filepath.Dir(changedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	changedBytes := []byte("package api\n")
	if err := os.WriteFile(changedPath, changedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	completionRel := ".claude/evidence/completion.json"
	completionBytes := []byte(`{"kind":"completion_report","changed_paths":["internal/api/handler.go"]}` + "\n")
	completionPath := filepath.Join(root, filepath.FromSlash(completionRel))
	if err := os.MkdirAll(filepath.Dir(completionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completionPath, completionBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state := baseDraftState(t)
	state["evidence"] = []any{map[string]any{
		"id": "completion-1", "kind": "completion_report", "path": completionRel,
		"sha256": sha256Of(completionBytes), "status": "valid", "baseline_generation": 1,
		"scope_refs": []any{},
	}}
	plan := &Plan{
		CoverageInventory: []CoverageItem{{
			ID: "surface:" + changedRel, Kind: "changed_surface", SourceRef: changedRel,
			Target: changedRel, Lens: "qa",
		}},
		Claims: []Claim{{SourceRefs: []string{changedRel}}},
	}
	if err := validateCoverageInventory(root, state, plan); err == nil || !strings.Contains(err.Error(), "frozen_subjects") {
		t.Fatalf("changed surface without a frozen subject must be rejected with recovery guidance, got %v", err)
	}
	plan.FrozenSubjects = []FrozenSubject{{Path: changedRel, SHA256: sha256Of(changedBytes), Kind: "changed_surface"}}
	if err := validateCoverageInventory(root, state, plan); err != nil {
		t.Fatalf("a SHA-pinned changed surface frozen subject should close the gate, got %v", err)
	}
}

func TestValidateRepairRoundBaselineRequiresChangedArtifactsInFrozenSubjects(t *testing.T) {
	root := t.TempDir()
	changedRel := "internal/api/handler.go"
	changedBytes := []byte("package api\n")
	changedPath := filepath.Join(root, filepath.FromSlash(changedRel))
	if err := os.MkdirAll(filepath.Dir(changedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changedPath, changedBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	impactRel := ".claude/evidence/impact.json"
	impactValue := map[string]any{
		"schema_version": "1.0.0", "record_type": "change_impact", "impact_id": "impact-BUG-001-attempt-1",
		"runtime_id": "loop-REQ-TEST", "req_id": "REQ-001", "baseline_generation": 1,
		"source_bug_ids": []string{"BUG-001"}, "change_types": []string{"implementation"},
		"changed_artifacts": []any{map[string]any{"id": "handler", "path": changedRel, "sha256": sha256Of(changedBytes)}},
		"decisions": []any{map[string]any{
			"source_id": "handler", "target_id": "evidence-verification", "relation": "implements repair",
			"rule_id": "IM-IMPLEMENTATION-MODULE", "decision": "invalidate", "responsibility_id": "VER-MODULE-COMPLETE",
			"scope": []string{"internal/api/handler.go"}, "rationale": "repair changed the implementation", "recovery_evidence": []string{"BUG-001"},
		}},
		"escalation_level": "review_round", "invalidated_evidence_ids": []string{"ev-old"},
		"superseded_evidence_ids": []string{}, "retained_evidence_ids": []string{},
		"required_reverification_ids": []string{"reverify-BUG-001"}, "analyzed_by": "orchestrator", "analyzed_at": "2026-08-25T00:00:00Z",
	}
	impactBytes, err := json.Marshal(impactValue)
	if err != nil {
		t.Fatal(err)
	}
	impactPath := filepath.Join(root, filepath.FromSlash(impactRel))
	if err := os.MkdirAll(filepath.Dir(impactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(impactPath, impactBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	state := baseDraftState(t)
	state["runtime_id"] = "loop-REQ-TEST"
	state["baseline"].(map[string]any)["generation"] = float64(1)
	state["review"] = map[string]any{
		"round": 2.0, "clean_round": nil,
		"round_entry": map[string]any{
			"transition_id":       "TR-012",
			"round":               2.0,
			"baseline_generation": 1.0,
			"change_impact_ref":   "ev-impact-1",
		},
	}
	state["evidence"] = []any{map[string]any{
		"id": "ev-impact-1", "kind": "change_impact", "path": impactRel,
		"sha256": sha256Of(impactBytes), "status": "valid", "baseline_generation": 1,
		"review_round": nil, "produced_by": []any{"orchestrator"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": "orchestrator", "scope_refs": []any{},
	}}

	plan := &Plan{
		ReviewRound:        2,
		BaselineGeneration: 1,
		ChangeImpact:       &ChangeImpact{SourceRefs: []string{"ev-impact-1"}},
		FrozenSubjects:     []FrozenSubject{{Path: "internal/other.go", SHA256: strings.Repeat("a", 64)}},
		CoverageInventory:  []CoverageItem{{ID: "surface:" + changedRel, Kind: "changed_surface", SourceRef: changedRel, Target: changedRel, Lens: "qa"}},
		Claims:             []Claim{{SourceRefs: []string{changedRel}}},
	}
	if err := validateRepairRoundBaseline(root, state, plan); err == nil || !strings.Contains(err.Error(), changedRel) {
		t.Fatalf("repair round without changed artifact frozen subject must be rejected, got %v", err)
	}

	plan.FrozenSubjects = append(plan.FrozenSubjects, FrozenSubject{Path: changedRel, SHA256: sha256Of(changedBytes)})
	if err := validateRepairRoundBaseline(root, state, plan); err != nil {
		t.Fatalf("complete repair baseline should pass: %v", err)
	}
}

func TestRegisterPlanEnforcesTR012RepairBaselineBinding(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["review"] = map[string]any{
		"round": 2.0, "clean_round": nil,
		"round_entry": map[string]any{
			"transition_id": "TR-012", "round": 2.0, "baseline_generation": 1.0,
			"change_impact_ref": "ev-impact-1",
		},
	}

	planPath := writePlanFile(t, root)
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var planBody map[string]any
	if err := json.Unmarshal(planData, &planBody); err != nil {
		t.Fatal(err)
	}
	planBody["review_round"] = 2
	planBody["change_impact"] = map[string]any{"source_refs": []string{"ev-impact-1"}}
	planBody["coverage_inventory"] = []any{map[string]any{
		"id": "surface:internal/example/service.go", "kind": "changed_surface",
		"source_ref": "internal/example/service.go", "target": "internal/example/service.go", "lens": "qa",
	}}
	for _, raw := range planBody["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["source_refs"] = append(claim["source_refs"].([]any), "internal/example/service.go")
		}
	}
	updatedPlan, err := json.MarshalIndent(planBody, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(updatedPlan, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	impactRel := ".claude/evidence/impact.json"
	impactValue := map[string]any{
		"schema_version": "1.0.0", "record_type": "change_impact", "impact_id": "impact-BUG-001-attempt-1",
		"runtime_id": "loop-REQ-TEST", "req_id": "REQ-001", "baseline_generation": 1,
		"source_bug_ids": []string{"BUG-001"}, "change_types": []string{"implementation"},
		"changed_artifacts": []any{map[string]any{"id": "handler", "path": "internal/example/service.go", "sha256": sha256Of([]byte("fixture baseline"))}},
		"decisions": []any{map[string]any{
			"source_id": "handler", "target_id": "evidence-verification", "relation": "implements repair",
			"rule_id": "IM-IMPLEMENTATION-MODULE", "decision": "invalidate", "responsibility_id": "VER-MODULE-COMPLETE",
			"scope": []string{"internal/example/service.go"}, "rationale": "repair changed the implementation", "recovery_evidence": []string{"BUG-001"},
		}},
		"escalation_level": "review_round", "invalidated_evidence_ids": []string{"ev-old"},
		"superseded_evidence_ids": []string{}, "retained_evidence_ids": []string{},
		"required_reverification_ids": []string{"reverify-BUG-001"}, "analyzed_by": "orchestrator", "analyzed_at": "2026-08-25T00:00:00Z",
	}
	impactBytes, err := json.Marshal(impactValue)
	if err != nil {
		t.Fatal(err)
	}
	impactPath := filepath.Join(root, filepath.FromSlash(impactRel))
	if err := os.MkdirAll(filepath.Dir(impactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(impactPath, impactBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{map[string]any{
		"id": "ev-impact-1", "kind": "change_impact", "path": impactRel,
		"sha256": sha256Of(impactBytes), "status": "valid", "baseline_generation": 1,
		"review_round": nil, "produced_by": []any{"orchestrator"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": "orchestrator", "scope_refs": []any{},
	}}
	statePath, journalPath := writeState(t, root, state)
	if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath}); err != nil {
		t.Fatalf("TR-012 plan with complete repair baseline must register: %v", err)
	}

	var incomplete map[string]any
	if err := json.Unmarshal(updatedPlan, &incomplete); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(root, "internal", "other.go")
	if err := os.MkdirAll(filepath.Dir(otherPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	incomplete["frozen_subjects"] = []any{map[string]any{"path": "internal/other.go", "sha256": sha256Of([]byte("package other\n"))}}
	incompleteBytes, err := json.MarshalIndent(incomplete, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	incompletePath := filepath.Join(root, "plan-incomplete.json")
	if err := os.WriteFile(incompletePath, append(incompleteBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use a fresh state because the successful registration above owns the
	// round; the rejection assertion is about the same registration gate.
	freshState := baseVerificationState()
	freshState["review"] = state["review"]
	freshState["evidence"] = state["evidence"]
	freshStatePath, freshJournalPath := writeState(t, root, freshState)
	if _, err := RegisterPlan(root, freshStatePath, freshJournalPath, PlanRequest{ExpectedRevision: 1, PlanPath: incompletePath}); err == nil || !strings.Contains(err.Error(), "S7_REPAIR_BASELINE_COVERAGE") {
		t.Fatalf("TR-012 plan missing repaired frozen subject must be rejected, got %v", err)
	}
}

// TASKs from earlier generations do not constrain the current round's plan.
func TestRegisterPlanIgnoresPriorGenerationTasks(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["documents"] = []any{
		map[string]any{
			"id": "TASK-099", "kind": "task", "path": "docs/tasks/TASK-099.md",
			"version": "v1.0.0", "sha256": strings.Repeat("3", 64),
			"status": "locked", "generation": 0,
		},
	}
	statePath, journalPath := writeState(t, root, state)
	if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: writePlanFile(t, root),
	}); err != nil {
		t.Fatalf("prior-generation TASK must not gate registration, got %v", err)
	}
}

func TestRegisterPlanRejectsChangedSurfaceCoverageGap(t *testing.T) {
	root := t.TempDir()
	// RC-16: the S7 projection verifies the completion envelope against disk
	// (a registered artifact path must exist and match its sha256), so the
	// fixture writes the real envelope instead of relying on the removed
	// missing-artifact scope_refs fallback.
	envelopeRel := ".claude/evidence/completion-surface.json"
	envelopeAbs := filepath.Join(root, filepath.FromSlash(envelopeRel))
	if err := os.MkdirAll(filepath.Dir(envelopeAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	envelope := []byte(`{"kind":"completion_report","changed_paths":["internal/example/service.go"]}`)
	if err := os.WriteFile(envelopeAbs, envelope, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(envelope)
	state := baseVerificationState()
	state["evidence"] = []any{map[string]any{
		"id":                  "ev-completion-surface-1",
		"kind":                "completion_report",
		"path":                envelopeRel,
		"sha256":              hex.EncodeToString(sum[:]),
		"status":              "valid",
		"baseline_generation": 1,
		"review_round":        nil,
		"produced_by":         []any{"agent-builder-1"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "BUILD-WORK-PACKAGE",
		"scope_refs":          []any{"internal/example/service.go"},
	}}
	statePath, journalPath := writeState(t, root, state)
	planPath := writePlanFile(t, root)
	if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: planPath,
	}); err == nil || !strings.Contains(err.Error(), "S7_PLAN_SURFACE_COVERAGE") || !strings.Contains(err.Error(), "internal/example/service.go") {
		t.Fatalf("changed surface without inventory/Claim must be rejected with a repair diagnostic, got %v", err)
	}

	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	body["coverage_inventory"] = []any{map[string]any{
		"id": "surface:internal/example/service.go", "kind": "changed_surface",
		"source_ref": "internal/example/service.go", "target": "internal/example/service.go", "lens": "qa",
	}}
	for _, raw := range body["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["source_refs"] = append(claim["source_refs"].([]any), "internal/example/service.go")
		}
	}
	updated, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{
		ExpectedRevision: 1, PlanPath: planPath,
	}); err != nil {
		t.Fatalf("complete changed-surface inventory must register: %v", err)
	}
}
