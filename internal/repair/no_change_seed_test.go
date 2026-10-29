package repair

import (
	"reflect"
	"strings"
	"testing"

	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
)

func TestNoChangeS9SeedPreservesEntireCanonicalProductBaseline(t *testing.T) {
	baseline := canonicalS9SeedBaseline(t)
	plan := canonicalS9SeedDraft(baseline)
	if err := prepareCanonicalS7ReviewPlanSeed(&plan, baseline, 3, ChangeImpact{ChangedArtifacts: []ArtifactRef{}}, ContractRef{Path: "contract.json", SHA256: strings.Repeat("c", 64)}, "impact.json"); err != nil {
		t.Fatal(err)
	}
	if plan.ProductBaseline == nil || !reflect.DeepEqual(*plan.ProductBaseline, baseline) || len(plan.FrozenSubjects) != 2 || len(plan.CoverageInventory) != 1 || plan.CoverageInventory[0].SourceRef != "src/changed.go" {
		t.Fatalf("empty S9 contribution narrowed the existing S6 product/TASK denominator: %+v", plan)
	}
	if len(plan.Claims) != 8 || len(plan.Assignments) != 8 || plan.E2ECoverageState != "cold_start" {
		t.Fatalf("no-change bypassed the original full S7 responsibilities: %+v", plan)
	}
	if err := reviewpkg.ValidatePlan(&plan); err == nil || !strings.Contains(err.Error(), "PLANNER-REFINE") {
		t.Fatalf("no-change seed became an accepted professional plan: %v", err)
	}
}
