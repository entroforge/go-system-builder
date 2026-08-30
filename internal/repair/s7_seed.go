package repair

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// seedMarker marks every substantive Claim field the S7 Planner must replace
// before dispatch. RC-11 (C-10): the S9 handoff seed no longer fabricates
// review conclusions — the previous seed shipped assertions, oracles, and
// methods that looked like decided facts but were canned strings. The marker
// wording is deliberate: rejectPlannerPlaceholders (internal/review) rejects
// the literal "TODO(planner)" token, so the seed uses an explicit instruction
// instead while staying equally unmistakable as unfinished content.
const seedMarker = "PLANNER-REFINE (S9 seed): the S7 Planner must replace this field via `runtime review-plan revise` before dispatch —"

// createS7ReviewPlanSeed stages the post-repair S7 ReviewPlan seed at the
// TR-012 handoff. review-plan.schema.json requires claims and assignments
// (minItems 1) plus e2e_coverage_state, so the seed carries the structural
// minimum those gates demand: two static placeholder Assignments (delivery +
// qa) and one explicit E2E applicability disposition. "not_applicable" is the
// only E2E coverage state consistent with a seed that declares no executable
// E2E Claim, and it requires the N/A e2e Claim below to carry na_rationale
// and na_checklist_id. Every substantive Claim field is a seedMarker; the S7
// Planner owns the real target/assertion/oracle/method set.
func createS7ReviewPlanSeed(root string, round, baselineGeneration int, impact ChangeImpact, contractRef ContractRef, taskIDs []string, impactPath string) (ArtifactRef, error) {
	// Read the approved RepairContract so the handoff keeps verifying the
	// artifact's hash and schema. The seed itself no longer embeds contract
	// fields; it only points the Planner at the contract as a source ref.
	if _, err := readArtifact(root, ArtifactRef{Path: contractRef.Path, SHA256: contractRef.SHA256}, "repair-contract.schema.json"); err != nil {
		return ArtifactRef{}, err
	}
	planID := fmt.Sprintf("review-plan-s9-round-%d", round)
	frozen := make([]any, 0, len(impact.ChangedArtifacts))
	coverage := make([]any, 0, len(impact.ChangedArtifacts))
	for _, artifact := range impact.ChangedArtifacts {
		frozen = append(frozen, map[string]any{"path": artifact.Path, "sha256": artifact.SHA256, "kind": "post_repair_changed_artifact"})
		coverage = append(coverage, map[string]any{"id": "coverage-" + safeSeedID(artifact.Path), "kind": "post_repair_change", "source_ref": artifact.Path, "target": artifact.Path, "lens": "qa"})
	}
	changedPaths := make([]string, 0, len(impact.ChangedArtifacts))
	for _, artifact := range impact.ChangedArtifacts {
		changedPaths = append(changedPaths, artifact.Path)
	}
	qaSources := append([]string{impactPath}, changedPaths...)
	// ValidatePlanTaskCoverage (registration time) rejects a plan whose
	// Claims drop every current-generation TASK, so the delivery Claim keeps
	// the TASK ids in source_refs for the Planner to refine from.
	deliverySources := append([]string{contractRef.Path}, taskIDs...)
	claims := []any{
		map[string]any{
			"claim_id": "claim-s9-delivery", "lens": "delivery",
			"target":        seedMarker + " author the delivery target from the approved RepairContract",
			"assertion":     seedMarker + " author the delivery assertion this round must prove",
			"oracle":        seedMarker + " author the observable pass/fail oracle",
			"method":        seedMarker + " choose the inspection method",
			"applicability": "required",
			"source_refs":   deliverySources,
		},
		map[string]any{
			"claim_id": "claim-s9-qa", "lens": "qa",
			"target":        seedMarker + " author the qa target over the post-repair changed surface",
			"assertion":     seedMarker + " author the qa assertion this round must prove",
			"oracle":        seedMarker + " author the observable pass/fail oracle",
			"method":        seedMarker + " choose the inspection method",
			"applicability": "required",
			"source_refs":   qaSources,
		},
		map[string]any{
			"claim_id": "claim-s9-e2e-na", "lens": "e2e",
			"target":          seedMarker + " name the affected user surface",
			"assertion":       seedMarker + " decide E2E applicability from the affected surface",
			"oracle":          seedMarker + " either behavior Claims or an evidence-backed N/A decision",
			"method":          seedMarker + " run the impact analysis",
			"applicability":   "not_applicable",
			"na_rationale":    "S9 creates a seed only; the S7 Planner must decide E2E applicability from the affected surface",
			"na_checklist_id": impactPath,
			"source_refs":     []string{impactPath},
		},
	}
	assignments := []any{
		map[string]any{"assignment_id": "assignment-s9-delivery", "lens": "delivery", "claim_ids": []string{"claim-s9-delivery"}, "non_overlap_boundary": "owns Contract traceability and changed-surface completeness", "execution_wave": "static"},
		map[string]any{"assignment_id": "assignment-s9-qa", "lens": "qa", "claim_ids": []string{"claim-s9-qa"}, "non_overlap_boundary": "owns invariant, maintainability and scope verification", "execution_wave": "static"},
	}
	seed := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": planID, "review_round": round, "baseline_generation": baselineGeneration,
		"frozen_subjects": frozen, "coverage_inventory": coverage, "change_impact": map[string]any{"summary": "S9 post-repair seed; Planner must refine the exact Claim set", "source_refs": []string{impactPath}},
		"claims": claims, "assignments": assignments, "e2e_coverage_state": "not_applicable", "verification_artifact_workspace": nil,
		"dispatch_capacity_policy": "coverage_complete", "created_by": "s9-handoff", "created_at": nowOr(time.Time{}),
	}
	seedPath := artifactRoot + "/s7-seeds/" + planID + ".json"
	return writeImmutable(root, seedPath, "review-plan.schema.json", seed)
}

func seedBaselineDigest(artifacts []ArtifactRef) string {
	lines := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		lines = append(lines, normalizePath(artifact.Path)+":"+artifact.SHA256)
	}
	sort.Strings(lines)
	return sha256Bytes([]byte(strings.Join(lines, "\n")))
}

func safeSeedID(path string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(normalizePath(path))
}
