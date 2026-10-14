package review

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanPreflightAggregatesIndependentProblems(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.Claims[0].Oracle = "TODO(planner)"
	plan.Claims[1].Method = "PLANNER-REFINE"
	plan.Claims[0].RequiredEvidence = []string{"unknown-first", "unknown-second"}
	plan.Assignments[0].ClaimIDs = append(plan.Assignments[0].ClaimIDs, "missing-claim")
	plan.Assignments[1].ExecutionWave = "behavior"
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	report := PreflightPlan(t.TempDir(), data, nil)
	encoded, _ := json.Marshal(report)
	for _, want := range []string{"TODO(planner)", "PLANNER-REFINE", "unknown-first", "unknown-second", "missing-claim", "white-box"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("missing %q in %s", want, encoded)
		}
	}
	if report.ArtifactValid || report.TransitionReady || report.Mode != "author_only" {
		t.Fatalf("unexpected readiness: %+v", report)
	}
}

func TestPlanPreflightSkipsDependentChecksAfterIdentityFailure(t *testing.T) {
	plan := loadFixturePlan(t)
	plan.Claims = append(plan.Claims, plan.Claims[0])
	plan.Assignments = append(plan.Assignments, plan.Assignments[0])
	data, _ := json.Marshal(plan)
	report := PreflightPlan(t.TempDir(), data, nil)
	encoded, _ := json.Marshal(report)
	for _, want := range []string{"duplicate claim_id", "duplicate assignment_id", "coverage_graph", "skipped"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("missing %q: %s", want, encoded)
		}
	}
	if report.ArtifactValid {
		t.Fatal("duplicate identity passed")
	}
	for _, invalid := range []string{"{", "null", `{ "schema_version":"1.0.0" }`} {
		report = PreflightPlan(t.TempDir(), []byte(invalid), nil)
		if report.ArtifactValid || len(report.Checks) != 3 || report.Checks[1].Status != "skipped" {
			t.Fatalf("invalid syntax/schema evaluated: %+v", report)
		}
	}
}

func TestPlanPreflightDoesNotClaimTransitionReadiness(t *testing.T) {
	data, _ := json.Marshal(loadFixturePlan(t))
	report := PreflightPlan(t.TempDir(), data, nil)
	if !report.ArtifactValid || report.TransitionReady {
		t.Fatalf("author-only validity/readiness: %+v", report)
	}
}
