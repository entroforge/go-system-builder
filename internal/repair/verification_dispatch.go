package repair

import (
	"errors"
	"fmt"
	"strings"
)

// RC-18 (S9-L3): independent-verification dispatch identity.
//
// RC-01/RC-15 bind a targeted reverification to two dispatched identities:
// the original repair assignment (owned by the repair owner O) and the
// performing verifier (owned by V, V != O). The verifier pool is
// {plan assignments} ∪ {their manifest aliases} ∪ {assignment_owners keys}
// ∪ {their aliases}. When a RepairContract declares a single repair unit
// (schema-legal: repair_units minItems=1) the plan holds exactly one
// assignment, so the pool contains only the repair owner's own identity and
// no registration path can introduce an independent verifier — the
// reverification can never be bound and S9 cannot reach its handoff.
//
// This file names the missing work item: a verification assignment
// synthesized from a plan repair assignment. It is registered through the
// ordinary `assignment.Register` path (workgroup_kind "builder" + owner
// binding), so the RC-01/RC-15 gate accepts it unchanged. Nothing in the
// identity gate is loosened here; the gate keeps rejecting every fabricated
// or self-owned verifier identity.
const (
	verificationAssignmentPrefix = "repair-assignment-verification-"
	manifestVerificationPrefix   = "assignment-s9-verification-"
)

// VerificationAssignmentID returns the plan-side identity of the independent
// verification assignment synthesized for a plan repair assignment. The
// spelling mirrors manifestAssignmentAlias: both spellings name the same work
// item and the Runtime resolves either one to the same owner.
func VerificationAssignmentID(originalAssignmentID string) string {
	return verificationAssignmentPrefix + verificationDispatchSlug(originalAssignmentID)
}

// ManifestVerificationAssignmentID returns the dispatched manifest spelling
// of VerificationAssignmentID (the spelling recorded by
// `runtime repair dispatch --independent-verification`).
func ManifestVerificationAssignmentID(originalAssignmentID string) string {
	return manifestVerificationPrefix + verificationDispatchSlug(originalAssignmentID)
}

func verificationDispatchSlug(originalAssignmentID string) string {
	// Both spellings of a repair assignment name the same work item, so the
	// slug is computed from the bare work-item name either way; an
	// unrecognized spelling passes through unchanged and simply yields a
	// slug that matches no plan entry.
	value := strings.TrimSpace(originalAssignmentID)
	value = strings.TrimPrefix(value, "repair-assignment-")
	value = strings.TrimPrefix(value, "assignment-s9-")
	return dispatchSlugCompat(value)
}

// canonicalPlanAssignmentID resolves the manifest-alias spelling of a plan
// assignment to its plan-side id. An id that matches neither is returned
// unchanged so the caller reports the plan-membership failure.
func canonicalPlanAssignmentID(plan RepairPlan, assignmentID string) string {
	assignmentID = strings.TrimSpace(assignmentID)
	for _, assignment := range plan.Assignments {
		if assignment.AssignmentID == assignmentID {
			return assignment.AssignmentID
		}
		if manifestAssignmentAlias(assignment.AssignmentID) == assignmentID {
			return assignment.AssignmentID
		}
	}
	return assignmentID
}

// ValidateVerificationDispatch checks that an independent verification
// assignment may be synthesized from originalAssignmentID for verifierAgentID
// given the session's recorded assignment owners, and returns its plan-side
// id.
//
// The rules mirror the RC-01/RC-15 gate, so a dispatch accepted here is
// guaranteed to produce a bindable (original, performing) pair, and a
// dispatch rejected here names the missing precondition instead of failing
// later at reverification commit time:
//
//  1. originalAssignmentID must be an assignment of the active RepairPlan
//     (exact id or its manifest alias) — a verification identity is only
//     meaningful as the counterpart of a real repair unit;
//  2. that repair assignment must already carry a recorded owner (its domain
//     PlanReport has bound the repair owner), because the entire point of the
//     verifier identity is that it resolves to a *different* agent;
//  3. verifierAgentID must differ from that repair owner;
//  4. the synthesized verification assignment must not be owned yet —
//     re-dispatching is refused and ownership is never replaced mid-session.
func ValidateVerificationDispatch(plan RepairPlan, originalAssignmentID, verifierAgentID string, owners map[string]string) (string, error) {
	originalAssignmentID = strings.TrimSpace(originalAssignmentID)
	if originalAssignmentID == "" {
		return "", errors.New("independent verification dispatch requires --assignment-id <repair-assignment-...>")
	}
	verifierAgentID = strings.TrimSpace(verifierAgentID)
	if verifierAgentID == "" {
		return "", errors.New("independent verification dispatch requires --agent-id <verifier-agent>")
	}
	inPlan := false
	for _, assignment := range plan.Assignments {
		if assignment.AssignmentID == originalAssignmentID || manifestAssignmentAlias(assignment.AssignmentID) == originalAssignmentID {
			inPlan = true
			break
		}
	}
	if !inPlan {
		return "", fmt.Errorf("independent verification dispatch: %q is not an assignment of RepairPlan %s; the verification identity is synthesized from an approved repair assignment", originalAssignmentID, plan.PlanID)
	}
	// Synthesize from the canonical plan-side spelling so the manifest alias
	// and the plan id cannot produce two different verification identities.
	originalAssignmentID = canonicalPlanAssignmentID(plan, originalAssignmentID)
	repairOwner := dispatchedVerifierAgentID(originalAssignmentID, owners)
	if repairOwner == "" {
		return "", fmt.Errorf("independent verification dispatch: repair assignment %s has no recorded owner; submit its domain PlanReport so the repair owner identity is bound before an independent verifier is dispatched", originalAssignmentID)
	}
	if repairOwner == verifierAgentID {
		return "", fmt.Errorf("independent verification dispatch: Agent %s owns repair assignment %s; the verifier must be a different agent — a repair owner verifying its own repair is not independent", verifierAgentID, originalAssignmentID)
	}
	verificationAssignmentID := VerificationAssignmentID(originalAssignmentID)
	if existing := dispatchedVerifierAgentID(verificationAssignmentID, owners); existing != "" {
		if existing == verifierAgentID {
			return "", fmt.Errorf("independent verification assignment %s is already dispatched to Agent %s; continue that Agent or inspect `runtime repair status`", verificationAssignmentID, existing)
		}
		return "", fmt.Errorf("independent verification assignment %s is already owned by Agent %s; do not replace ownership mid-session", verificationAssignmentID, existing)
	}
	return verificationAssignmentID, nil
}
