package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
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
	if buildsurface.HasCanonical(state) {
		projection := s7BaselineProjection{}
		snapshot, err := buildsurface.ReadContext(ctx, root, state)
		if err != nil {
			projection.err = err
			projection.Diagnostics = []string{fmt.Sprintf("canonical S6 product baseline: %v", err)}
			return projection
		}
		for _, path := range snapshot.Paths {
			projection.ChangedPaths = append(projection.ChangedPaths, path.Path)
		}
		sort.Strings(projection.ChangedPaths)
		return projection
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
	// A thin canonical Result deliberately contains no product diff. Only
	// the original Main facts can supply it; treating the absent legacy
	// changed_paths field as an empty surface would silently waive coverage.
	if _, canonical := envelope["assignment_record"]; canonical {
		return nil, fmt.Errorf("canonical S6 Result requires the original Main facts changed-surface projection; this reader cannot establish the fresh S7 baseline")
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

func validateS7BaselineProjection(root string, state map[string]any, contexts ...context.Context) error {
	if err := baselineContext(contexts).Err(); err != nil {
		return err
	}
	if root == "" {
		return nil
	}
	projection := buildS7BaselineProjection(root, state, contexts...)
	return validateBuiltS7BaselineProjection(root, projection)
}

func validateBuiltS7BaselineProjection(root string, projection s7BaselineProjection) error {
	if root == "" {
		return nil
	}
	if errors.Is(projection.err, context.Canceled) || errors.Is(projection.err, context.DeadlineExceeded) {
		return projection.err
	}
	if len(projection.Diagnostics) == 0 {
		return nil
	}
	return s7GateError(
		"S7_BASELINE_PROJECTION",
		"S7 cannot build a verified projection of the current-generation S6 completion evidence",
		projection.Diagnostics,
		[]string{"restore any missing or changed original completion artifact; a thin canonical S6 Result requires the Main facts projection adapter before fresh S7 can register"},
		"loop-harness s7 draft --out plan.json",
	)
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
