package review

import (
	"errors"
	"fmt"
	"strings"
)

// PlanAnswerError marks only a failure in the author's plan content. Source,
// Runtime and Writer failures remain unmarked and belong to Main preparation.
type PlanAnswerError struct{ Err error }

func (e *PlanAnswerError) Error() string { return e.Err.Error() }
func (e *PlanAnswerError) Unwrap() error { return e.Err }

// PlanCheck is an ephemeral diagnostic, never another plan or Runtime state.
type PlanCheck struct {
	Name   string
	Status string
	Reason string
	Err    error
}

type PlanChecksError struct{ Checks []PlanCheck }

func (e *PlanChecksError) Error() string {
	var parts []string
	for _, c := range e.Checks {
		if c.Status == "failed" {
			parts = append(parts, c.Name+" [failed, owner=Main/plan]: "+c.Err.Error())
		} else if c.Status == "not_checked" {
			parts = append(parts, c.Name+" [not_checked]: "+c.Reason)
		}
	}
	return strings.Join(parts, "\n")
}

func (e *PlanChecksError) Unwrap() []error {
	var errs []error
	for _, c := range e.Checks {
		if c.Err != nil {
			errs = append(errs, c.Err)
		}
	}
	return errs
}

// ValidatePlan runs independent existing checks once. Identity-dependent
// checks never interpret a duplicate or unresolved Claim map.
func ValidatePlan(plan *Plan) error {
	if plan == nil {
		return &PlanAnswerError{fmt.Errorf("ReviewPlan is missing")}
	}
	var checks []PlanCheck
	run := func(name string, fn func() error) bool {
		c := PlanCheck{Name: name, Status: "passed", Err: fn()}
		if c.Err != nil {
			c.Status = "failed"
		}
		checks = append(checks, c)
		return c.Err == nil
	}
	run("dispatch_policy", func() error { return validatePlanPolicy(plan) })
	run("professional_content", func() error { return rejectPlannerPlaceholders(plan) })
	run("evidence_requirements", func() error { return validatePlanEvidenceRequirements(plan) })
	validIdentity := run("identities_and_references", func() error { return validatePlanIdentity(plan) })
	dependent := []struct {
		name string
		fn   func() error
	}{
		{"coverage_and_assignments", func() error { return validatePlanCoverage(plan) }},
		{"e2e_arrangement", func() error { return validatePlanE2EState(plan) }},
		{"assignment_overlap", func() error { return validateAssignmentOverlap(plan, planClaimMap(plan)) }},
		{"cold_start_load", func() error { return validateColdStartE2EOverload(plan, planClaimMap(plan)) }},
	}
	for _, c := range dependent {
		if validIdentity {
			run(c.name, c.fn)
		} else {
			checks = append(checks, PlanCheck{Name: c.name, Status: "not_checked", Reason: "resolve identities_and_references first"})
		}
	}
	for _, c := range checks {
		if c.Err != nil {
			checks = append(checks, PlanCheck{Name: "source_baseline_and_commit", Status: "not_checked", Reason: "plan content is invalid; original registration retains source checks and CAS"})
			return &PlanAnswerError{&PlanChecksError{Checks: checks}}
		}
	}
	return nil
}

func planClaimMap(plan *Plan) map[string]Claim {
	claims := make(map[string]Claim, len(plan.Claims))
	for _, claim := range plan.Claims {
		claims[claim.ClaimID] = claim
	}
	return claims
}

func validatePlanIdentity(plan *Plan) error {
	claims := map[string]bool{}
	assignments := map[string]bool{}
	var failures []error
	for _, c := range plan.Claims {
		if claims[c.ClaimID] {
			failures = append(failures, fmt.Errorf("duplicate claim_id %s", c.ClaimID))
		}
		claims[c.ClaimID] = true
	}
	for _, a := range plan.Assignments {
		if assignments[a.AssignmentID] {
			failures = append(failures, fmt.Errorf("duplicate assignment_id %s", a.AssignmentID))
		}
		assignments[a.AssignmentID] = true
		for _, id := range a.ClaimIDs {
			if !claims[id] {
				failures = append(failures, fmt.Errorf("assignment %s references unknown claim %s", a.AssignmentID, id))
			}
		}
	}
	for _, c := range plan.Claims {
		for _, id := range c.DependsOn {
			if !claims[id] {
				failures = append(failures, fmt.Errorf("claim %s depends on unknown claim %s", c.ClaimID, id))
			}
		}
	}
	return errors.Join(failures...)
}
