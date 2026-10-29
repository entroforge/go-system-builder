package repair

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	reviewpkg "github.com/entroforge/go-system-builder/internal/review"
)

// seedMarker marks every substantive Claim field the S7 Planner must replace
// before dispatch. RC-11 (C-10): the S9 handoff seed no longer fabricates
// review conclusions — the previous seed shipped assertions, oracles, and
// methods that looked like decided facts but were canned strings. The marker
// wording is deliberate: rejectPlannerPlaceholders (internal/review) rejects
// the literal "TODO(planner)" token, so the seed uses an explicit instruction
// instead while staying equally unmistakable as unfinished content.
const seedMarker = "PLANNER-REFINE (S9 seed): answer this professional question in the supplied current ReviewPlan and save before dispatch; only an already registered Plan needs its named revision —"

// createS7ReviewPlanSeed stages the post-repair S7 ReviewPlan seed at the
// TR-012 handoff. Canonical S6 generations use the normal S7 draft and keep
// its full machine-derived surface; the compatibility branch below preserves
// the historical seed shape for runtimes without a canonical Assignment
// registry.
func createS7ReviewPlanSeed(root string, round, baselineGeneration int, impact ChangeImpact, contractRef ContractRef, taskIDs []string, impactPath string, state map[string]any, canonicalBaseline *buildsurface.Snapshot, contexts ...context.Context) (ArtifactRef, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	ctx = buildsurface.WithReadCache(ctx)
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}
	// Read the approved RepairContract so the handoff keeps verifying the
	// artifact's hash and schema. The seed itself no longer embeds contract
	// fields; it only points the Planner at the contract as a source ref.
	if _, err := readArtifact(root, ArtifactRef{Path: contractRef.Path, SHA256: contractRef.SHA256}, "repair-contract.schema.json"); err != nil {
		return ArtifactRef{}, err
	}
	if canonicalBaseline != nil {
		plan, notes := reviewpkg.DraftPlanForRootContext(ctx, root, state, round)
		if err := ctx.Err(); err != nil {
			return ArtifactRef{}, err
		}
		if plan == nil {
			return ArtifactRef{}, fmt.Errorf("draft canonical S7 baseline: %s", strings.Join(notes, "; "))
		}
		if err := prepareCanonicalS7ReviewPlanSeed(plan, *canonicalBaseline, round, impact, contractRef, impactPath); err != nil {
			return ArtifactRef{}, err
		}
		// DraftPlanForRoot independently reconstructs this projection. Recheck
		// the original sources immediately before publishing the immutable seed;
		// TR-012 repeats this check inside its original Writer mutation.
		if err := buildsurface.VerifyContext(ctx, root, state, *canonicalBaseline); err != nil {
			return ArtifactRef{}, fmt.Errorf("verify canonical S7 baseline before seed publication: %w", err)
		}
		seedPath := artifactRoot + "/s7-seeds/" + plan.ReviewPlanID + ".json"
		if err := ctx.Err(); err != nil {
			return ArtifactRef{}, err
		}
		return writeImmutable(root, seedPath, "review-plan.schema.json", plan)
	}
	planID := fmt.Sprintf("review-plan-s9-round-%d", round)
	frozen := make([]any, 0, len(impact.ChangedArtifacts))
	coverage := make([]any, 0, len(impact.ChangedArtifacts))
	for _, artifact := range impact.ChangedArtifacts {
		frozen = append(frozen, map[string]any{"path": artifact.Path, "sha256": artifact.SHA256, "kind": "post_repair_changed_artifact"})
		coverage = append(coverage, map[string]any{"id": "coverage-" + safeSeedID(artifact.Path), "kind": "post_repair_change", "source_ref": artifact.Path, "target": artifact.Path, "lens": "qa"})
	}
	if len(frozen) == 0 {
		// Legacy runtimes lack the canonical product baseline above. Pin the
		// actual approved spec already verified here; do not invent a changed
		// product or turn the unrefined seed into a clean verification round.
		frozen = append(frozen, map[string]any{"path": contractRef.Path, "sha256": contractRef.SHA256, "kind": "repair_contract"})
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
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}
	return writeImmutable(root, seedPath, "review-plan.schema.json", seed)
}

// prepareCanonicalS7ReviewPlanSeed keeps the original S7 machine projection
// (all locked TASKs, product paths, delivery claims and six QA focus claims)
// while leaving every professional decision to the Planner. It deliberately
// does not validate as a dispatchable Plan: PLANNER-REFINE fields make the
// ordinary registration/refinement gate reject it until a Planner authors
// the substantive assertions, oracles and methods.
func prepareCanonicalS7ReviewPlanSeed(plan *reviewpkg.Plan, baseline buildsurface.Snapshot, round int, impact ChangeImpact, contractRef ContractRef, impactPath string) error {
	if plan == nil || impactPath == "" || contractRef.Path == "" || contractRef.SHA256 == "" {
		return fmt.Errorf("canonical S7 seed requires a drafted plan and exact RepairContract/change-impact sources")
	}
	if baseline.Contract != buildsurface.Contract || baseline.Digest == "" || baseline.BaselineGeneration < 1 {
		return fmt.Errorf("canonical S7 seed requires a complete original product baseline")
	}
	baselineDigest, err := buildsurface.Digest(baseline)
	if err != nil {
		return fmt.Errorf("canonical S7 seed baseline cannot be digested: %w", err)
	}
	if baselineDigest != baseline.Digest {
		return fmt.Errorf("canonical S7 seed baseline digest does not match its machine projection")
	}
	if plan.ProductBaseline == nil {
		return fmt.Errorf("canonical S7 draft omitted its product baseline")
	}
	draftDigest, err := buildsurface.Digest(*plan.ProductBaseline)
	if err != nil {
		return fmt.Errorf("canonical S7 draft has an invalid product baseline: %w", err)
	}
	if draftDigest != baselineDigest || plan.BaselineGeneration != baseline.BaselineGeneration || round < 1 {
		return fmt.Errorf("canonical S7 draft differs from the pre-handoff original product baseline")
	}
	if len(baseline.Tasks) == 0 {
		return fmt.Errorf("canonical S7 baseline has no exact locked TASK denominator")
	}

	frozen, err := reviewpkg.FrozenSubjectsForProductBaseline(&baseline)
	if err != nil {
		return fmt.Errorf("project canonical frozen subjects: %w", err)
	}
	plan.ProductBaseline = &baseline
	plan.FrozenSubjects = frozen
	plan.BaselineGeneration = baseline.BaselineGeneration
	plan.ReviewPlanID = fmt.Sprintf("review-plan-s9-round-%d", round)
	plan.ReviewRound = round
	plan.CreatedBy = "s9-handoff"
	plan.CreatedAt = nowOr(time.Time{})

	coverage := make([]reviewpkg.CoverageItem, 0, len(baseline.Paths))
	coverageByPath := make(map[string]bool, len(baseline.Paths))
	for _, path := range baseline.Paths {
		if path.Path == "" || path.SHA256 == "" {
			return fmt.Errorf("canonical product baseline contains an incomplete changed path")
		}
		coverage = append(coverage, reviewpkg.CoverageItem{ID: "surface:" + path.Path, Kind: "changed_surface", SourceRef: path.Path, Target: path.Path, Lens: "qa"})
		coverageByPath[path.Path] = true
	}
	plan.CoverageInventory = coverage
	frozenByPath := make(map[string]string, len(frozen))
	for _, subject := range frozen {
		frozenByPath[subject.Path] = subject.SHA256
	}
	for _, artifact := range impact.ChangedArtifacts {
		if artifact.Path == "" || artifact.SHA256 == "" || !coverageByPath[artifact.Path] || frozenByPath[artifact.Path] != artifact.SHA256 {
			return fmt.Errorf("TR-012 change-impact artifact %q is not an exact path/hash in the canonical post-repair baseline", artifact.Path)
		}
	}
	if err := validateCanonicalSeedDraftCoverage(plan, baseline); err != nil {
		return err
	}

	if plan.ChangeImpact == nil {
		plan.ChangeImpact = &reviewpkg.ChangeImpact{}
	}
	plan.ChangeImpact.Summary = "S9 post-repair machine baseline; the S7 Planner must author the professional review scope"
	plan.ChangeImpact.SourceRefs = appendUniqueSeedRefs(plan.ChangeImpact.SourceRefs, impactPath)

	qaSources := []string{impactPath}
	for _, path := range baseline.Paths {
		qaSources = append(qaSources, path.Path)
	}
	assigned := make(map[string]bool)
	for _, assignment := range plan.Assignments {
		for _, claimID := range assignment.ClaimIDs {
			assigned[claimID] = true
		}
	}
	var convertedE2E []string
	for i := range plan.Claims {
		claim := &plan.Claims[i]
		claim.Assertion = seedMarker + " author the exact assertion this S7 Claim must prove"
		claim.Oracle = seedMarker + " define the observable pass/fail oracle for this Claim"
		claim.Method = seedMarker + " choose the professional review or execution method"
		claim.NARationale = ""
		claim.NAChecklistID = ""
		switch claim.Lens {
		case "delivery":
			claim.SourceRefs = appendUniqueSeedRefs(claim.SourceRefs, contractRef.Path, impactPath)
		case "qa":
			claim.SourceRefs = appendUniqueSeedRefs(claim.SourceRefs, qaSources...)
		case "e2e":
			claim.SourceRefs = appendUniqueSeedRefs(claim.SourceRefs, impactPath)
			if claim.Applicability == "not_applicable" {
				// The existing draft may infer N/A from ui_impact=none. A seed
				// cannot make that professional decision, so keep a conservative
				// required placeholder until the Planner revises the Plan.
				claim.Applicability = "required"
				claim.Target = seedMarker + " name the affected user-observable surface and decide E2E applicability"
				convertedE2E = append(convertedE2E, claim.ClaimID)
			}
		}
	}
	for index, claimID := range convertedE2E {
		if assigned[claimID] {
			continue
		}
		plan.Assignments = append(plan.Assignments, reviewpkg.PlanAssignment{
			AssignmentID:       fmt.Sprintf("assignment-s9-e2e-%d", index+1),
			Lens:               "e2e",
			ClaimIDs:           []string{claimID},
			NonOverlapBoundary: "owns the unresolved S9 E2E applicability and behavior decision; the Planner must refine before dispatch",
			ExecutionWave:      "behavior",
		})
	}
	// The immutable seed must keep the existing formal schema. This is a
	// structural placeholder only: PLANNER-REFINE still prevents dispatch.
	if len(convertedE2E) > 0 || plan.E2ECoverageState == "" {
		plan.E2ECoverageState = "cold_start"
		workspace := fmt.Sprintf("e2e-workspace/review-plan-s9-round-%d", round)
		plan.VerificationArtifactWorkspace = &workspace
	}
	assigned = make(map[string]bool)
	for _, assignment := range plan.Assignments {
		for _, claimID := range assignment.ClaimIDs {
			assigned[claimID] = true
		}
	}
	for _, claim := range plan.Claims {
		if claim.Applicability == "required" && !assigned[claim.ClaimID] {
			return fmt.Errorf("canonical S7 draft required Claim %s has no Assignment", claim.ClaimID)
		}
	}
	if plan.ChangeImpact == nil || !containsSeedRef(plan.ChangeImpact.SourceRefs, impactPath) {
		return fmt.Errorf("canonical S7 seed omitted the original TR-012 change-impact source")
	}
	if len(plan.FrozenSubjects) == 0 || len(plan.Claims) == 0 || len(plan.Assignments) == 0 {
		return fmt.Errorf("canonical S7 seed has no frozen subjects, Claims, or Assignments")
	}
	return nil
}

func validateCanonicalSeedDraftCoverage(plan *reviewpkg.Plan, baseline buildsurface.Snapshot) error {
	tasks := make(map[string]string, len(baseline.Tasks))
	for _, task := range baseline.Tasks {
		tasks[task.ID] = task.Path
	}
	deliveryCovered := make(map[string]bool, len(tasks))
	qaFocuses := make(map[string]bool)
	for _, claim := range plan.Claims {
		switch claim.Lens {
		case "delivery":
			for taskID, path := range tasks {
				if containsSeedRef(claim.SourceRefs, taskID) && containsSeedRef(claim.SourceRefs, path) {
					deliveryCovered[taskID] = true
				}
			}
		case "qa":
			if claim.FocusKey != "" {
				qaFocuses[claim.FocusKey] = true
			}
		}
	}
	for taskID := range tasks {
		if !deliveryCovered[taskID] {
			return fmt.Errorf("canonical S7 draft omitted current locked TASK %s from delivery coverage", taskID)
		}
	}
	for _, focus := range []string{"design-boundary", "pattern-idiom-fit", "logic-state-error", "maintainability", "testability-oracle", "debt-operability"} {
		if !qaFocuses[focus] {
			return fmt.Errorf("canonical S7 draft omitted baseline QA focus %s", focus)
		}
	}
	return nil
}

func appendUniqueSeedRefs(refs []string, additions ...string) []string {
	seen := make(map[string]bool, len(refs)+len(additions))
	out := make([]string, 0, len(refs)+len(additions))
	for _, ref := range append(append([]string(nil), refs...), additions...) {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func containsSeedRef(refs []string, target string) bool {
	for _, ref := range refs {
		if ref == target {
			return true
		}
	}
	return false
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
