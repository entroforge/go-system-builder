package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	err          error
}

func buildS7BaselineProjection(root string, state map[string]any, contexts ...context.Context) s7BaselineProjection {
	ctx := baselineContext(contexts)
	if err := ctx.Err(); err != nil {
		return s7BaselineProjection{Diagnostics: []string{err.Error()}, err: err}
	}

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
		if err := ctx.Err(); err != nil {
			projection.err = err
			projection.Diagnostics = append(projection.Diagnostics, err.Error())
			return projection
		}
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
	if err := ctx.Err(); err != nil {
		projection.err = err
		projection.Diagnostics = append(projection.Diagnostics, err.Error())
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

	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("artifact is not valid JSON: %w", err)
	}
	if kind := stringField(envelope["kind"]); kind != "" && kind != "completion_report" {
		return nil, fmt.Errorf("artifact kind is %q, want completion_report", kind)
	}
	// A thin canonical Result deliberately contains no product diff. Only
	// the original Main facts can supply it; treating the absent legacy
	// changed_paths field as an empty surface would silently waive coverage.
	if _, canonical := envelope["assignment_record"]; canonical {
		return normalizeBaselinePaths(stringSliceValue(entry["scope_refs"])), nil
	}
	paths := normalizeBaselinePaths(stringSliceValue(envelope["changed_paths"]))
	for _, changed := range paths {
		absolute, err := repositoryContainedPath(root, changed)
		if err != nil {
			return nil, fmt.Errorf("changed path %q: %w", changed, err)
		}
		if info, err := os.Stat(absolute); err == nil && info.IsDir() && !evidenceDirectory(root, changed) {
			return nil, fmt.Errorf("changed path %q is a product directory; submit a canonical file-level completion report including deleted files", changed)
		}
	}
	return filterFreezableChangedPaths(root, paths), nil
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
	// Changed paths are professional review inputs, not a byte proof.
	subjects := make([]FrozenSubject, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			subjects = append(subjects, FrozenSubject{Path: path, Kind: "changed_surface"})
		}
	}
	return subjects, nil
}

// Only framework-owned evidence bookkeeping directories may be dropped.
// Product directories are retained by this helper and diagnosed by the reader.
func evidenceDirectory(root, path string) bool {
	if path != ".claude/evidence" && !strings.HasPrefix(path, ".claude/evidence/") {
		return false
	}
	// A lexical evidence prefix cannot exempt a symlink into product code.
	current := root
	for _, part := range strings.Split(path, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func filterFreezableChangedPaths(root string, paths []string) []string {
	if root == "" {
		return paths
	}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		absolute, err := repositoryContainedPath(root, path)
		if err == nil {
			if info, statErr := os.Stat(absolute); statErr == nil && info.IsDir() && evidenceDirectory(root, path) {
				continue
			}
		}
		result = append(result, path)
	}
	return result
}
