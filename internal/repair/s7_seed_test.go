package repair

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/buildsurface"
	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestCanonicalS9SeedKeepsFullMachineSurfaceAndPlannerDecisionsOpen(t *testing.T) {
	baseline := canonicalS9SeedBaseline(t)
	plan := canonicalS9SeedDraft(baseline)
	err := prepareCanonicalS7ReviewPlanSeed(&plan, baseline, 3, ChangeImpact{ChangedArtifacts: []ArtifactRef{{Path: "src/changed.go", SHA256: strings.Repeat("b", 64)}}}, ContractRef{Path: ".claude/review/repair/contracts/contract.json", SHA256: strings.Repeat("c", 64)}, ".claude/review/repair/change-impact.json")
	if err != nil {
		t.Fatalf("prepareCanonicalS7ReviewPlanSeed: %v", err)
	}
	if plan.ReviewPlanID != "review-plan-s9-round-3" || plan.ReviewRound != 3 || plan.ProductBaseline == nil || plan.ProductBaseline.Digest != baseline.Digest {
		t.Fatalf("canonical Plan binding = %#v", plan)
	}
	if len(plan.FrozenSubjects) != 2 || plan.FrozenSubjects[0].Path != "docs/TASK-1.md" || plan.FrozenSubjects[1].Path != "src/changed.go" {
		t.Fatalf("frozen subjects must include exact TASK and changed product path: %#v", plan.FrozenSubjects)
	}
	if len(plan.CoverageInventory) != 1 || plan.CoverageInventory[0].SourceRef != "src/changed.go" {
		t.Fatalf("coverage inventory must preserve complete canonical product paths: %#v", plan.CoverageInventory)
	}
	if len(plan.Claims) != 8 || len(plan.Assignments) != 8 {
		t.Fatalf("expected delivery, six QA and one unresolved E2E Claim/Assignment; got claims=%d assignments=%d", len(plan.Claims), len(plan.Assignments))
	}
	if plan.E2ECoverageState != "cold_start" || plan.VerificationArtifactWorkspace == nil || *plan.VerificationArtifactWorkspace != "e2e-workspace/review-plan-s9-round-3" {
		t.Fatalf("ui_impact-derived N/A must remain an unresolved Planner E2E decision: %#v", plan)
	}
	for _, claim := range plan.Claims {
		if !strings.Contains(claim.Assertion, "PLANNER-REFINE") || !strings.Contains(claim.Oracle, "PLANNER-REFINE") || !strings.Contains(claim.Method, "PLANNER-REFINE") {
			t.Errorf("claim %s contains a non-placeholder professional field: %#v", claim.ClaimID, claim)
		}
		if claim.Lens == "qa" && (!containsSeedRef(claim.SourceRefs, "src/changed.go") || !containsSeedRef(claim.SourceRefs, ".claude/review/repair/change-impact.json")) {
			t.Errorf("QA claim %s lost change-impact sources: %#v", claim.ClaimID, claim.SourceRefs)
		}
		if claim.Lens == "delivery" && !containsSeedRef(claim.SourceRefs, ".claude/review/repair/contracts/contract.json") {
			t.Errorf("delivery claim %s lost RepairContract source: %#v", claim.ClaimID, claim.SourceRefs)
		}
		if claim.Lens == "e2e" && claim.Applicability != "required" {
			t.Errorf("E2E applicability was decided by the seed instead of remaining unresolved: %#v", claim)
		}
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", data); err != nil {
		t.Fatalf("canonical S9 seed must be schema-shaped before ordinary Plan refinement: %v", err)
	}
	if err := reviewpkg.ValidatePlan(&plan); err == nil || !strings.Contains(err.Error(), "PLANNER-REFINE") {
		t.Fatalf("unrefined canonical seed must not cross normal Plan registration: %v", err)
	}
}

func TestCanonicalS9SeedRejectsChangeImpactOutsidePinnedProductPaths(t *testing.T) {
	baseline := canonicalS9SeedBaseline(t)
	plan := canonicalS9SeedDraft(baseline)
	err := prepareCanonicalS7ReviewPlanSeed(&plan, baseline, 3, ChangeImpact{ChangedArtifacts: []ArtifactRef{{Path: "src/other.go", SHA256: strings.Repeat("b", 64)}}}, ContractRef{Path: "contract.json", SHA256: strings.Repeat("c", 64)}, "impact.json")
	if err == nil || !strings.Contains(err.Error(), "not an exact path/hash") {
		t.Fatalf("out-of-baseline change-impact path must fail closed, got %v", err)
	}
}

func canonicalS9SeedBaseline(t *testing.T) buildsurface.Snapshot {
	t.Helper()
	baseline := buildsurface.Snapshot{
		Contract:           buildsurface.Contract,
		RuntimeID:          "loop-s9-test",
		BaselineGeneration: 1,
		StartReceiptSHA256: strings.Repeat("a", 64),
		StartCommit:        strings.Repeat("1", 40),
		FinalCommit:        strings.Repeat("2", 40),
		ProjectRoot:        "/tmp/s9-seed-fixture",
		DevelopmentBranch:  "main",
		Tasks:              []buildsurface.Task{{ID: "TASK-1", Path: "docs/TASK-1.md", Version: "1.0.0", SHA256: strings.Repeat("a", 64)}},
		Contributions: []buildsurface.Contribution{{
			AssignmentRecord: assignmentRecordRefForSeed(), AssignmentRecordSHA256: strings.Repeat("b", 64),
			ResultRef: "result.json", ResultSHA256: strings.Repeat("c", 64), CheckpointRef: "checkpoint.json", CheckpointSHA256: strings.Repeat("d", 64),
			MergeCommit: strings.Repeat("3", 40), TestedHead: strings.Repeat("4", 40), MainFactsSHA256: strings.Repeat("e", 64),
		}},
		Paths: []buildsurface.Path{{Path: "src/changed.go", Status: "modified", SHA256: strings.Repeat("b", 64), BlobOID: strings.Repeat("5", 40)}},
	}
	digest, err := buildsurface.Digest(baseline)
	if err != nil {
		t.Fatal(err)
	}
	baseline.Digest = digest
	return baseline
}

func assignmentRecordRefForSeed() (ref assignmentrecord.RecordReference) {
	return assignmentrecord.RecordReference{RuntimeID: "loop-s9-test", BaselineGeneration: 1, AssignmentID: "assignment-s6-test", Revision: 1}
}

func canonicalS9SeedDraft(baseline buildsurface.Snapshot) reviewpkg.Plan {
	product := baseline
	plan := reviewpkg.Plan{
		SchemaVersion:          "1.0.0",
		ReviewPlanID:           "review-plan-draft-r3",
		ReviewRound:            3,
		BaselineGeneration:     1,
		ProductBaseline:        &product,
		CoverageInventory:      []reviewpkg.CoverageItem{{ID: "old", Kind: "changed_surface", SourceRef: "old", Target: "old", Lens: "qa"}},
		ChangeImpact:           &reviewpkg.ChangeImpact{SourceRefs: []string{"src/changed.go"}},
		E2ECoverageState:       "not_applicable",
		DispatchCapacityPolicy: "coverage_complete",
		CreatedBy:              "orchestrator", CreatedAt: "2026-10-14T00:00:00Z",
	}
	plan.Claims = append(plan.Claims, reviewpkg.Claim{
		ClaimID: "claim-dv-task-1", Lens: "delivery", Target: "docs/TASK-1.md", Assertion: "old", Oracle: "old", Method: "old", Applicability: "required", SourceRefs: []string{"TASK-1", "docs/TASK-1.md"},
	})
	plan.Assignments = append(plan.Assignments, reviewpkg.PlanAssignment{AssignmentID: "assignment-dv", Lens: "delivery", ClaimIDs: []string{"claim-dv-task-1"}, NonOverlapBoundary: "delivery traceability", ExecutionWave: "static"})
	for _, focus := range []string{"design-boundary", "pattern-idiom-fit", "logic-state-error", "maintainability", "testability-oracle", "debt-operability"} {
		claimID := "claim-qa-" + focus
		plan.Claims = append(plan.Claims, reviewpkg.Claim{
			ClaimID: claimID, Lens: "qa", Target: "src/changed.go", Assertion: "old", Oracle: "old", Method: "old", Applicability: "required", SourceRefs: []string{"src/changed.go"}, FocusKey: focus,
		})
		plan.Assignments = append(plan.Assignments, reviewpkg.PlanAssignment{AssignmentID: "assignment-qa-" + focus, Lens: "qa", ClaimIDs: []string{claimID}, FocusKeys: []string{focus}, NonOverlapBoundary: "one independent QA focus", ExecutionWave: "static"})
	}
	plan.Claims = append(plan.Claims, reviewpkg.Claim{
		ClaimID: "claim-e2e-na-1", Lens: "e2e", Target: "n/a", Assertion: "old", Oracle: "old", Method: "old", Applicability: "not_applicable", NARationale: "draft inferred no UI impact", NAChecklistID: "bound_req#ui_impact", SourceRefs: []string{"bound_req"},
	})
	return plan
}

func TestCanonicalS9SeedPreservesDeletedPreimageAndCoverage(t *testing.T) {
	baseline := canonicalS9SeedBaseline(t)
	baseline.Paths[0].Status = "deleted"
	baseline.Paths[0].PreimageCommit = strings.Repeat("6", 40)
	baseline.Paths[0].DeletedByCommit = strings.Repeat("7", 40)
	var err error
	baseline.Digest, err = buildsurface.Digest(baseline)
	if err != nil {
		t.Fatal(err)
	}
	plan := canonicalS9SeedDraft(baseline)
	if err := prepareCanonicalS7ReviewPlanSeed(&plan, baseline, 3, ChangeImpact{ChangedArtifacts: []ArtifactRef{{Path: "src/changed.go", SHA256: baseline.Paths[0].SHA256}}}, ContractRef{Path: "contract.json", SHA256: strings.Repeat("c", 64)}, "impact.json"); err != nil {
		t.Fatal(err)
	}
	if len(plan.FrozenSubjects) != 2 || plan.FrozenSubjects[1].Kind != "deleted" || plan.FrozenSubjects[1].SHA256 != baseline.Paths[0].SHA256 {
		t.Fatalf("fresh seed lost deletion preimage: %+v", plan.FrozenSubjects)
	}
	if len(plan.CoverageInventory) != 1 || plan.CoverageInventory[0].SourceRef != "src/changed.go" {
		t.Fatalf("fresh seed lost deletion coverage: %+v", plan.CoverageInventory)
	}
}
