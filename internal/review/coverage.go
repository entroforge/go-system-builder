package review

import (
	"context"
	"sort"
)

// CoverageItem describes a possible planning input from current work materials.
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
