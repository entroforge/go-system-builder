package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// PreflightCheck records dependent checks explicitly instead of turning a
// missing prerequisite into either a cascade of guesses or an implicit pass.
type PreflightCheck struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Problems []string `json:"problems,omitempty"`
}

type PlanPreflight struct {
	Mode            string           `json:"mode"`
	ArtifactValid   bool             `json:"artifact_valid"`
	TransitionReady bool             `json:"transition_ready"`
	Checks          []PreflightCheck `json:"checks"`
}

// PreflightPlan performs no writes, creates no Store, and never acquires a
// Runtime lock. A state supplied by the caller is an observed snapshot only;
// registration must validate again in its commit transaction.
func PreflightPlan(root string, data []byte, state map[string]any) PlanPreflight {
	report := PlanPreflight{Mode: "author_only", Checks: []PreflightCheck{}}
	if state != nil {
		report.Mode = "runtime_snapshot"
	}
	add := func(name string, err error) {
		check := PreflightCheck{Name: name, Status: "passed"}
		if err != nil {
			check.Status = "failed"
			check.Problems = errorMessages(err)
		}
		report.Checks = append(report.Checks, check)
	}
	skip := func(name, reason string) {
		report.Checks = append(report.Checks, PreflightCheck{Name: name, Status: "skipped", Problems: []string{reason}})
	}
	err := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", data)
	add("schema", err)
	if err != nil {
		skip("semantics", "depends on a valid schema and artifact identity")
		skip("runtime_inputs", "depends on a valid schema and artifact identity")
		return report
	}
	var plan Plan
	if err = json.Unmarshal(data, &plan); err != nil {
		add("decode", err)
		return report
	}
	add("semantics", ValidatePlan(&plan))
	if err := validatePlanIdentity(&plan); err != nil {
		skip("coverage_graph", "depends on unique claim and assignment identities")
	}
	if state == nil {
		skip("runtime_inputs", "no Runtime supplied; file fingerprints, authority and round readiness are not verified")
	} else if err := ValidatePlanProtocolBinding(state, &plan); err != nil {
		add("protocol", err)
		skip("runtime_inputs", "depends on a compatible Runtime/artifact protocol")
	} else {
		add("protocol", nil)
		add("module_scope", ValidateModuleScope(root, state, &plan))
		add("frozen_subjects", verifyFrozenSubjects(root, &plan, state))
		add("regression_assets", verifyRegressionAssetFingerprints(root, &plan))
		add("task_coverage", ValidatePlanTaskCoverage(state, &plan))
		add("coverage_inventory", validateCoverageInventory(root, state, &plan))
		add("repair_baseline", validateRepairRoundBaseline(root, state, &plan))
		var coordinates []error
		if plan.ReviewRound != currentReviewRound(state) {
			coordinates = append(coordinates, fmt.Errorf("review_round %d does not match Runtime %d", plan.ReviewRound, currentReviewRound(state)))
		}
		if plan.BaselineGeneration != baselineGeneration(state) {
			coordinates = append(coordinates, fmt.Errorf("baseline_generation %d does not match Runtime %d", plan.BaselineGeneration, baselineGeneration(state)))
		}
		add("coordinates", errors.Join(coordinates...))
	}
	report.ArtifactValid = true
	for _, check := range report.Checks {
		if check.Status == "failed" {
			report.ArtifactValid = false
		}
	}
	return report
}

func errorMessages(err error) []string {
	if err == nil {
		return nil
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var messages []string
		for _, child := range group.Unwrap() {
			messages = append(messages, errorMessages(child)...)
		}
		return messages
	}
	return []string{err.Error()}
}

func validatePlanIdentity(plan *Plan) error {
	var problems []error
	claims, assignments := map[string]bool{}, map[string]bool{}
	for _, claim := range plan.Claims {
		if strings.TrimSpace(claim.ClaimID) == "" {
			problems = append(problems, fmt.Errorf("empty claim_id"))
		}
		if claims[claim.ClaimID] {
			problems = append(problems, fmt.Errorf("duplicate claim_id %s", claim.ClaimID))
		}
		claims[claim.ClaimID] = true
	}
	for _, assignment := range plan.Assignments {
		if strings.TrimSpace(assignment.AssignmentID) == "" {
			problems = append(problems, fmt.Errorf("empty assignment_id"))
		}
		if assignments[assignment.AssignmentID] {
			problems = append(problems, fmt.Errorf("duplicate assignment_id %s", assignment.AssignmentID))
		}
		assignments[assignment.AssignmentID] = true
	}
	return errors.Join(problems...)
}
