package review

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CoverageItem is a frozen planning fact, not a new Runtime state. It names an
// object S7 must account for before a ReviewPlan can be registered.
type CoverageItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SourceRef string `json:"source_ref"`
	Target    string `json:"target"`
	Lens      string `json:"lens"`
}

// BuildCoverageInventory projects state-only S6 facts into a deterministic
// planner input. Production callers should use BuildCoverageInventoryForRoot
// so the immutable completion envelope, rather than a copied index field, is
// the authority for changed paths.
func BuildCoverageInventory(state map[string]any) []CoverageItem {
	return legacyBuildCoverageInventory(state)
}

// BuildCoverageInventoryForRoot reads the canonical S6 product snapshot when
// one exists; historical runtimes use the indexed completion envelope. The
// resulting changed-surface set is the denominator that S7 must cover.
func BuildCoverageInventoryForRoot(root string, state map[string]any) []CoverageItem {
	return BuildCoverageInventoryForRootContext(context.Background(), root, state)
}

func BuildCoverageInventoryForRootContext(ctx context.Context, root string, state map[string]any) []CoverageItem {
	return coverageInventoryForProjection(buildS7BaselineProjection(root, state, ctx))
}

func coverageInventoryForProjection(projection s7BaselineProjection) []CoverageItem {
	seen := map[string]bool{}
	var items []CoverageItem
	add := func(item CoverageItem) {
		if item.SourceRef == "" || seen[item.SourceRef] {
			return
		}
		seen[item.SourceRef] = true
		items = append(items, item)
	}
	for _, ref := range projection.ChangedPaths {
		add(CoverageItem{
			ID:        "surface:" + ref,
			Kind:      "changed_surface",
			SourceRef: ref,
			Target:    ref,
			Lens:      "qa",
		})
	}
	return sortedCoverageItems(items)
}

// ChangedPathsForRoot returns the changed-surface path set of the current
// baseline generation without allocating CoverageItem rows. It is the same
// authoritative projection BuildCoverageInventoryForRoot freezes into the S7
// plan, exported so the S10 Quality Gate can reconcile a manifest's
// changed_path denominator against an external anchor (RC-05 S10-5).
//
// RC-16: callers that must distinguish "a verifiably empty surface" from "an
// unverifiable projection" (diagnostics present) must use
// ChangedPathsForRootDetailed — a nil return here is intentionally ambiguous
// for backwards compatibility, and the S10 gate now fails closed on it.
func ChangedPathsForRoot(root string, state map[string]any, contexts ...context.Context) []string {
	paths, _ := ChangedPathsForRootDetailed(root, state, contexts...)
	return paths
}

// ChangedPathsForRootDetailed is ChangedPathsForRoot with the projection
// diagnostics exposed (RC-16). A non-empty diagnostics slice means the
// completion-envelope projection could not be fully verified: the returned
// path set is not a denominator, and the caller must fail closed (the S10
// gate reports external_baseline_unverifiable) rather than reconcile against
// a partial or self-declared surface.
func ChangedPathsForRootDetailed(root string, state map[string]any, contexts ...context.Context) ([]string, []string) {
	projection := buildS7BaselineProjection(root, state, contexts...)
	if len(projection.Diagnostics) > 0 {
		// An unverifiable projection is not a denominator: a caller that
		// receives diagnostics alongside empty paths must fail closed rather
		// than reconcile against a partial surface.
		return nil, projection.Diagnostics
	}
	return append([]string(nil), projection.ChangedPaths...), nil
}

func sortedCoverageItems(items []CoverageItem) []CoverageItem {
	sort.Slice(items, func(i, j int) bool { return items[i].SourceRef < items[j].SourceRef })
	return items
}

// legacyBuildCoverageInventory is retained only for tests and callers that
// intentionally pass a state snapshot without a repository root. It reads
// scope_refs as the compatibility projection produced by S6.
func legacyBuildCoverageInventory(state map[string]any) []CoverageItem {
	generation := baselineGeneration(state)
	seen := map[string]bool{}
	var items []CoverageItem
	add := func(item CoverageItem) {
		if item.SourceRef == "" || seen[item.SourceRef] {
			return
		}
		seen[item.SourceRef] = true
		items = append(items, item)
	}
	for _, raw := range evidenceEntries(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || entry["kind"] != "completion_report" || intField(entry["baseline_generation"]) != generation {
			continue
		}
		for _, ref := range normalizeBaselinePaths(stringSliceValue(entry["scope_refs"])) {
			add(CoverageItem{ID: "surface:" + ref, Kind: "changed_surface", SourceRef: ref, Target: ref, Lens: "qa"})
		}
	}
	return sortedCoverageItems(items)
}

func validateCoverageInventory(root string, state map[string]any, plan *Plan, contexts ...context.Context) error {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyProductBaselineDeclarationContext(ctx, root, state, plan); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return s7GateError(
			"S7_PRODUCT_BASELINE",
			"ReviewPlan does not carry the current canonical S6 product baseline",
			[]string{err.Error()},
			[]string{"re-draft from the pinned original TR-003 source and current official Main HEAD; do not hand-edit ProductBaseline or frozen_subjects"},
			"loop-harness s7 draft --out plan.json",
		)
	}
	return validateVerifiedCoverageInventory(root, state, plan, ctx)
}

// validateVerifiedCoverageInventory consumes only the baseline declaration
// freshly verified by this caller. Canonical registration invokes the complete
// coverage gate inside its Writer, so coverage and live source proof share one CAS.
func validateVerifiedCoverageInventory(root string, state map[string]any, plan *Plan, ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Validation and the coverage denominator consume the same synchronous
	// projection. No verified projection is retained across a Writer boundary.
	var projection s7BaselineProjection
	if plan.ProductBaseline != nil {
		// The caller freshly reconstructed every original source and
		// proved DeepEqual with this declaration. Its Paths are therefore the
		// same verified denominator; do not reconstruct the whole source proof
		// again within the same validation. Writer checks remain live.
		for _, path := range plan.ProductBaseline.Paths {
			projection.ChangedPaths = append(projection.ChangedPaths, path.Path)
		}
		sort.Strings(projection.ChangedPaths)
	} else {
		projection = buildS7BaselineProjection(root, state, ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBuiltS7BaselineProjection(root, projection); err != nil {
		return err
	}
	required := coverageInventoryForProjection(projection)
	if len(required) == 0 {
		return nil
	}
	if len(plan.CoverageInventory) == 0 {
		missing := make([]string, 0, len(required))
		for _, item := range required {
			missing = append(missing, item.SourceRef+" has no Coverage Inventory item")
		}
		return s7GateError(
			"S7_PLAN_SURFACE_COVERAGE",
			"ReviewPlan does not declare the complete S6 changed-surface inventory",
			missing,
			[]string{"restore the machine-generated coverage_inventory through original S7 preparation, then add each source_ref to at least one focused Claim"},
			"runtime review-plan --file plan.json",
		)
	}
	bySource := make(map[string]CoverageItem, len(plan.CoverageInventory))
	for _, item := range plan.CoverageInventory {
		if err := validateCoverageItem(item); err != nil {
			return s7GateError(
				"S7_PLAN_SURFACE_COVERAGE",
				"Coverage Inventory contains an invalid item",
				[]string{err.Error()},
				[]string{"complete id, kind, source_ref, target and lens for every coverage_inventory item"},
				"runtime review-plan --file plan.json",
			)
		}
		if item.SourceRef == "" {
			return s7GateError(
				"S7_PLAN_SURFACE_COVERAGE",
				"Coverage Inventory contains an item without source_ref",
				[]string{"coverage_inventory item " + item.ID},
				[]string{"set source_ref to the exact changed path or authoritative surface id"},
				"runtime review-plan --file plan.json",
			)
		}
		bySource[item.SourceRef] = item
	}
	claimed := map[string]bool{}
	for _, claim := range plan.Claims {
		for _, sourceRef := range claim.SourceRefs {
			claimed[sourceRef] = true
		}
	}
	frozen := make(map[string]bool, len(plan.FrozenSubjects))
	for _, subject := range plan.FrozenSubjects {
		frozen[normalizeSurface(subject.Path)] = true
	}
	var missing []string
	for _, item := range required {
		if _, ok := bySource[item.SourceRef]; !ok {
			missing = append(missing, item.SourceRef+" is absent from coverage_inventory")
			continue
		}
		if !claimed[item.SourceRef] {
			missing = append(missing, item.SourceRef+" has no Claim source_ref")
		}
		if item.Kind == "changed_surface" && !frozen[normalizeSurface(item.SourceRef)] {
			missing = append(missing, item.SourceRef+" is a changed surface but is absent from frozen_subjects")
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return s7GateError(
			"S7_PLAN_SURFACE_COVERAGE",
			"ReviewPlan changed-surface coverage is incomplete",
			missing,
			[]string{"add each missing Claim source_ref to a focused QA or E2E Claim with a specific target and oracle; restore inconsistent machine projections through original S7 preparation, never hand-edit frozen_subjects or deletion preimage SHA-256"},
			"runtime review-plan --file plan.json",
		)
	}
	return nil
}

func validateCoverageItem(item CoverageItem) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.SourceRef) == "" || strings.TrimSpace(item.Target) == "" || strings.TrimSpace(item.Lens) == "" {
		return fmt.Errorf("coverage_inventory item must include id, kind, source_ref, target and lens")
	}
	return nil
}
