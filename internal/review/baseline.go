package review

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// s7BaselineProjection is the one planner-facing view of the S6 completion
// facts. The completion envelope remains authoritative; scope_refs is only a
// backwards-compatible index projection for runtimes written before the
// envelope reader was introduced.
type s7BaselineProjection struct {
	ChangedPaths []string
	Diagnostics  []string
}

func buildS7BaselineProjection(root string, state map[string]any) s7BaselineProjection {
	generation := baselineGeneration(state)
	seen := map[string]bool{}
	projection := s7BaselineProjection{}
	add := func(path string) {
		path = normalizeBaselinePath(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		projection.ChangedPaths = append(projection.ChangedPaths, path)
	}
	for _, raw := range evidenceEntries(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || entry["kind"] != "completion_report" || intField(entry["baseline_generation"]) != generation {
			continue
		}
		paths, err := completionChangedPaths(root, entry)
		if err != nil {
			projection.Diagnostics = append(projection.Diagnostics, fmt.Sprintf("completion evidence %s: %v", stringField(entry["id"]), err))
			continue
		}
		for _, path := range paths {
			add(path)
		}
	}
	sort.Strings(projection.ChangedPaths)
	sort.Strings(projection.Diagnostics)
	return projection
}

// completionChangedPaths reads the immutable completion envelope when root
// is available. A state-only caller falls back to scope_refs, which keeps old
// inspection helpers useful while all production S7 drafting goes through the
// root-aware path.
func completionChangedPaths(root string, entry map[string]any) ([]string, error) {
	path := stringField(entry["path"])
	if root == "" || path == "" {
		return normalizeBaselinePaths(stringSliceValue(entry["scope_refs"])), nil
	}
	absolute, err := repositoryContainedPath(root, path)
	if err != nil {
		return nil, fmt.Errorf("artifact path is invalid: %w", err)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		// RC-16: a registered artifact path that does not exist on disk is no
		// longer silently bridged with the agent-controllable scope_refs
		// projection — that fallback let a tampered completion envelope
		// inflate the changed-surface denominator. The registration path must
		// fail closed so the S7 projection reports a diagnostic and the S10
		// gate reports external_baseline_unverifiable instead of waiving the
		// exact-set check.
		return nil, fmt.Errorf("completion artifact %q is registered but missing on disk: %w", path, err)
	}
	if want := stringField(entry["sha256"]); want != "" && sha256Of(data) != want {
		return nil, fmt.Errorf("artifact sha256 mismatch: registered %s, disk contains %s", want, sha256Of(data))
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("artifact is not valid JSON: %w", err)
	}
	if kind := stringField(envelope["kind"]); kind != "" && kind != "completion_report" {
		return nil, fmt.Errorf("artifact kind is %q, want completion_report", kind)
	}
	return normalizeBaselinePaths(stringSliceValue(envelope["changed_paths"])), nil
}

func normalizeBaselinePaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = normalizeBaselinePath(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func normalizeBaselinePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	if path == "" || strings.Contains(path, ":") {
		return ""
	}
	return normalizeSurface(path)
}

func changedSurfaceSubjects(root string, paths []string) ([]FrozenSubject, []string) {
	if root == "" {
		return nil, nil
	}
	subjects := make([]FrozenSubject, 0, len(paths))
	diagnostics := []string{}
	for _, path := range paths {
		absolute, err := repositoryContainedPath(root, path)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("changed surface %s: %v", path, err))
			continue
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("changed surface %s cannot be frozen: %v", path, err))
			continue
		}
		subjects = append(subjects, FrozenSubject{Path: path, SHA256: sha256Of(data), Kind: "changed_surface"})
	}
	sort.Slice(subjects, func(i, j int) bool { return subjects[i].Path < subjects[j].Path })
	sort.Strings(diagnostics)
	return subjects, diagnostics
}

func validateS7BaselineProjection(root string, state map[string]any) error {
	if root == "" {
		return nil
	}
	projection := buildS7BaselineProjection(root, state)
	if len(projection.Diagnostics) == 0 {
		return nil
	}
	return s7GateError(
		"S7_BASELINE_PROJECTION",
		"S7 cannot build a verified projection of the current-generation S6 completion evidence",
		projection.Diagnostics,
		[]string{"restore the canonical completion artifact or run the S6 completion path again so its path and sha256 are registered together"},
		"loop-harness s7 draft --out plan.json",
	)
}
