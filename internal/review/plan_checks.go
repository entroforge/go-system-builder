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

// ValidatePlan checks the observable draft and unambiguous original
// responsibility relationships. Professional coverage remains Main's work.
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
	run("professional_content", func() error { return requireSomeProfessionalPlanContent(plan) })
	run("identities_and_references", func() error { return validatePlanIdentity(plan) })
	for _, c := range checks {
		if c.Err != nil {
			return &PlanAnswerError{&PlanChecksError{Checks: checks}}
		}
	}
	return nil
}

// Only an entirely unfilled generated draft remains a draft. A real
// professional statement is receivable even when another item is undecided.
func requireSomeProfessionalPlanContent(plan *Plan) error {
	if HasProfessionalPlanContent(plan) {
		return nil
	}
	return &PlanAnswerError{fmt.Errorf("ReviewPlan has no professional Claim content yet; preserve the original draft")}
}

// HasProfessionalPlanContent tests whether the original contains any actual
// professional expression. Treat only the exact bare placeholder tokens as
// empty; words inside an author's sentence are never a rejection criterion.
func HasProfessionalPlanContent(plan *Plan) bool {
	if plan == nil {
		return false
	}
	expressed := func(value string) bool {
		value = strings.TrimSpace(value)
		switch value {
		case "", "TODO(planner)", "PLANNER-REFINE", "TODO(planner): select target", "TODO(planner): decide", "PLANNER-REFINE: decide":
			return false
		default:
			return true
		}
	}
	for _, claim := range plan.Claims {
		for _, value := range []string{claim.Target, claim.Assertion, claim.Oracle, claim.Method, claim.NARationale, claim.FocusKey} {
			if expressed(value) {
				return true
			}
		}
		for _, value := range append(append([]string(nil), claim.SourceRefs...), claim.RequiredEvidence...) {
			if expressed(value) {
				return true
			}
		}
	}
	for _, assignment := range plan.Assignments {
		if expressed(assignment.NonOverlapBoundary) {
			return true
		}
	}
	if plan.ChangeImpact != nil && expressed(plan.ChangeImpact.Summary) {
		return true
	}
	return plan.CoverageJustification != nil && expressed(*plan.CoverageJustification)
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
	owners := map[string]string{}
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
			if owner := owners[id]; owner != "" && owner != a.AssignmentID {
				failures = append(failures, fmt.Errorf("claim %s is assigned to both %s and %s", id, owner, a.AssignmentID))
			}
			owners[id] = a.AssignmentID
		}
	}
	return errors.Join(failures...)
}
