package review

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanChecksAggregateIndependentProblemsAndSkipBrokenIdentity(t *testing.T) {
	p := loadFixturePlan(t)
	p.DispatchCapacityPolicy = "queue_only"
	p.Claims[0].Oracle = "TODO(planner) oracle"
	p.Claims[1].Method = "PLANNER-REFINE method"
	p.Claims = append(p.Claims, p.Claims[0])
	err := ValidatePlan(p)
	var author *PlanAnswerError
	var report *PlanChecksError
	if !errors.As(err, &author) || !errors.As(err, &report) {
		t.Fatalf("missing original author/check diagnostics: %v", err)
	}
	for _, expected := range []string{"S7_DISPATCH_POLICY", "oracle", "method", "duplicate claim_id", "not_checked"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("missing independent feedback %s: %v", expected, err)
		}
	}
	for _, c := range report.Checks {
		if c.Name == "coverage_and_assignments" && (c.Status != "not_checked" || c.Err != nil) {
			t.Fatalf("broken Claim map produced false coverage: %+v", c)
		}
	}
}

func TestPlanChecksKeepIndependentE2EAndAssignmentFailures(t *testing.T) {
	p := loadFixturePlan(t)
	p.Assignments[0].ExecutionWave = "behavior"
	p.E2ECoverageState = "cold_start"
	p.VerificationArtifactWorkspace = nil
	err := ValidatePlan(p)
	if err == nil || !strings.Contains(err.Error(), "white-box") || !strings.Contains(err.Error(), "S7_E2E_WORKSPACE_MISSING") {
		t.Fatalf("independent failures were serialized into repeated saves: %v", err)
	}
}
