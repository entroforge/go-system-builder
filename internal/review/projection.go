package review

import (
	"fmt"
	"time"
)

// ApplyRegisteredPlanProjection applies the state projection that normally
// accompanies RegisterPlan. It is intentionally separate from the CAS
// transaction so another stage (TR-012 S9 handoff) can register a previously
// schema-validated seed in the same runtime mutation that opens S7.
func ApplyRegisteredPlanProjection(state map[string]any, plan Plan, planPath, planSHA string, round int, workspace, artifactDigest string, occurredAt time.Time) error {
	if plan.ReviewPlanID == "" || planPath == "" || planSHA == "" || round < 1 {
		return fmt.Errorf("registered ReviewPlan projection requires plan identity, path, sha256, and positive round")
	}
	reviewMap, ok := state["review"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime review section must be an object")
	}
	reviewMap["plan"] = map[string]any{
		"plan_id":                         plan.ReviewPlanID,
		"path":                            planPath,
		"sha256":                          planSHA,
		"revision":                        1,
		"review_round":                    round,
		"status":                          "running",
		"e2e_coverage_state":              plan.E2ECoverageState,
		"verification_artifact_workspace": workspace,
		"verification_artifact_digest":    artifactDigestOrNil(artifactDigest),
		"submitted_at":                    occurredAt.UTC().Format(time.RFC3339Nano),
	}
	claimsProjection := map[string]any{}
	assignmentsProjection := map[string]any{}
	ownerByClaim := map[string]string{}
	for _, assignment := range plan.Assignments {
		claimIDs := make([]any, 0, len(assignment.ClaimIDs))
		for _, claimID := range assignment.ClaimIDs {
			claimIDs = append(claimIDs, claimID)
			ownerByClaim[claimID] = assignment.AssignmentID
		}
		locks := mergedResourceLocks(assignment.ResourceLocks, &plan, assignment.ClaimIDs)
		assignmentsProjection[assignment.AssignmentID] = map[string]any{
			"lens":            assignment.Lens,
			"claim_ids":       claimIDs,
			"status":          "planned",
			"agent_id":        nil,
			"result_ref":      nil,
			"queued_agent_id": nil,
			"blocker_ref":     nil,
			"blocked_at":      nil,
			"resource_locks":  locks,
			"queue_reason":    nil,
		}
	}
	for _, claim := range plan.Claims {
		disposition := "planned"
		if claim.Applicability == "not_applicable" {
			disposition = "not_applicable"
		}
		claimsProjection[claim.ClaimID] = map[string]any{
			"lens":          claim.Lens,
			"applicability": claim.Applicability,
			"disposition":   disposition,
			"assignment_id": ownerByClaim[claim.ClaimID],
			"result_id":     nil,
			"finding_ids":   []any{},
		}
	}
	reviewMap["claims"] = claimsProjection
	reviewMap["assignments"] = assignmentsProjection
	reviewMap["observation_batch"] = nil
	if entities, ok := state["entities"].(map[string]any); ok {
		if _, present := entities["findings"]; !present {
			entities["findings"] = []any{}
		}
	}
	if lifecycle, ok := state["lifecycle"].(map[string]any); ok {
		lifecycle["state"] = "verification"
		lifecycle["phase"] = "running"
		lifecycle["phase_revision"] = intField(lifecycle["phase_revision"]) + 1
	}
	state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return nil
}
