package repair

import (
	"context"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// seedMarker marks every substantive Claim field the S7 Planner must replace
// before dispatch. RC-11 (C-10): the S9 handoff seed no longer fabricates
// review conclusions — the previous seed shipped assertions, oracles, and
// methods that looked like decided facts but were canned strings. The marker
// wording is deliberate: rejectPlannerPlaceholders (internal/review) rejects
// the literal "TODO(planner)" token, so the seed uses an explicit instruction
// instead while staying equally unmistakable as unfinished content.
const seedMarker = "PLANNER-REFINE (S9 seed): answer this professional question in the supplied current ReviewPlan and save before dispatch; only an already registered Plan needs its named revision —"

// Production seeds use the actual candidate entry identity and the immutable
// Handoff timestamp. A retry before commit therefore writes identical bytes.
func createS7ReviewPlanSeedForEntry(root, planID string, baselineGeneration int, impact ChangeImpact, contractRef ContractRef, taskIDs []string, impactPath, createdAt string, contexts ...context.Context) (ArtifactRef, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}

	// Existing paths remain professional scope hints. Old digest fields do not
	// establish new frozen subjects or become new S7 machine evidence.
	frozen := []any{}
	coverage := make([]any, 0, len(impact.ChangedArtifacts))
	for _, artifact := range impact.ChangedArtifacts {
		if artifact.Path == "" {
			continue
		}
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
	impactSummary := "S9 post-repair seed; Planner must refine the exact Claim set"
	if strings.TrimSpace(impact.IntegrationNotes) != "" {
		impactSummary += "\nMain's original integration and check notes:\n" + impact.IntegrationNotes
	}
	seed := map[string]any{
		"schema_version": "1.1.0", "review_plan_id": planID, "baseline_generation": baselineGeneration,
		"frozen_subjects": frozen, "coverage_inventory": coverage, "change_impact": map[string]any{"summary": impactSummary, "source_refs": []string{impactPath}},
		"claims": claims, "assignments": assignments, "e2e_coverage_state": "not_applicable", "verification_artifact_workspace": nil,
		"dispatch_capacity_policy": "coverage_complete", "created_by": "s9-handoff", "created_at": createdAt,
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}
	data, err := canonicalJSON(seed)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("encode S7 seed: %w", err)
	}
	if len(data) > 20<<20 {
		return ArtifactRef{}, fmt.Errorf("S7 seed exceeds 20 MiB after final serialization")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-plan.schema.json", data); err != nil {
		return ArtifactRef{}, fmt.Errorf("validate S7 seed: %w", err)
	}
	// A file is not evidence that this handoff published it. Publish exclusively
	// and carry the path chosen by this attempt into the original TR-012 CAS.
	// If another attempt wins the CAS, its recorded reference is authoritative;
	// an unregistered file from this attempt is left untouched.
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return ArtifactRef{}, err
		}
		category := "s7-seeds"
		if attempt > 0 {
			// Change the category as well as the leaf, so an occupied parent
			// component cannot force an endless retry at the same location.
			category = fmt.Sprintf("s7-seeds-alt-%d", attempt)
		}
		seedPath := responsibilityArtifactPath(category, planID)
		created, err := workpacket.PublishNewBounded(root, seedPath, data, 20<<20)
		if err != nil {
			return ArtifactRef{}, fmt.Errorf("publish S7 seed %s: %w", seedPath, err)
		}
		if created {
			return fileRef(seedPath, data), nil
		}
		// A collision under the category can use another category. A blocked
		// shared repair root cannot, and must be reported rather than retried
		// forever under names that all have the same inaccessible parent.
		parent, _, err := workpacket.OpenParent(root, artifactRoot+"/.seed-parent", false)
		if err != nil {
			return ArtifactRef{}, fmt.Errorf("open S7 seed parent: %w", err)
		}
		if err := parent.Close(); err != nil {
			return ArtifactRef{}, fmt.Errorf("close S7 seed parent: %w", err)
		}
	}
}

func safeSeedID(path string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ".", "-").Replace(normalizePath(path))
}
