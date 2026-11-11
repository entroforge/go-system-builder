package review

import (
	"encoding/json"
	"os"
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

// Partially expressed planning remains available to Main for completion.
func TestValidatePlanAcceptsProfessionalCapacityPolicy(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.DispatchCapacityPolicy = "bounded_flow"
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("capacity policy narrowed Plan reception: %v", err)
	}
}

func TestValidatePlanAcceptsUnfinishedLensCoverage(t *testing.T) {
	for _, ids := range [][]string{{"claim-dv-1"}, {"claim-qa-1", "claim-qa-2"}} {
		plan := loadFixturePlan(t)
		dropClaims(plan, ids...)
		if err := ValidatePlan(plan); err != nil {
			t.Fatalf("uncompleted lens blocked Plan reception: %v", err)
		}
	}
}

func TestValidatePlanAcceptsColdStartWithoutInventedWorkspace(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.E2ECoverageState = "cold_start"
	plan.VerificationArtifactWorkspace = nil
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("cold-start Plan required invented workspace or evidence: %v", err)
	}
}

func TestValidatePlanAcceptsRegressionWorkspaceAndUnfinishedE2E(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.E2ECoverageState = "regression_available"
	workspace := "e2e-workspace/available"
	plan.VerificationArtifactWorkspace = &workspace
	dropClaims(plan, "claim-e2e-na")
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("available workspace or unfinished E2E blocked reception: %v", err)
	}
}

func TestValidatePlanAcceptsActualRegressionAssetWithoutFingerprint(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.E2ECoverageState = "regression_available"
	plan.E2EAssets = []E2EAsset{{CaseRef: "CASE-001", Path: "e2e/settings-save.spec.ts"}}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("actual reusable path required a machine fingerprint: %v", err)
	}
	if plan.E2EAssets[0].Path != "e2e/settings-save.spec.ts" {
		t.Fatal("professional asset path was lost")
	}
}

func TestValidatePlanAcceptsUnfinishedNAReasoning(t *testing.T) {
	plan := loadFixturePlan(t)
	dropClaims(plan, "claim-e2e-na")
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("uncompleted applicability blocked Plan reception: %v", err)
	}
}

// TASKs from earlier generations do not constrain the current round's plan.
func TestRegisterPlanIgnoresPriorGenerationTasks(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["documents"] = []any{
		map[string]any{
			"id": "TASK-099", "kind": "task", "path": "docs/dev/tasks/TASK-099.md",
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
