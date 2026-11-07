package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

// RequiredCheckRunner is the hook the Integrator uses to execute a single
// integration check command. The default (nil) means "no checks configured"
// per the contract: Inspect verifies required checks only when the caller
// supplies a non-empty list. The integration verification skill in the
// closed-by-extension wiring (BUG-06) provides a real implementation.
type RequiredCheckRunner func(ctx context.Context, root, command string) error

// InspectConfig tunes the Inspect behaviour. Zero-value is fine for
// production use; tests use it to inject a check runner and to skip the
// completion-report check (which depends on a specific on-disk layout).
type InspectConfig struct {
	// ObserveWorktree is advisory and used only by Main during actual native S6 integration.
	ObserveWorktree bool
	LockedArtifacts []string
	// CheckRunner runs RequiredChecks. A missing executor fails closed when
	// checks are declared; an empty RequiredChecks list still means none.
	CheckRunner RequiredCheckRunner
	// RequiredChecks is the command list to execute. Empty means no
	// checks required.
	RequiredChecks []string
	// SkipCompletionCheck, when true, skips the on-disk completion-report
	// existence check. Tests use this to drive the merge-back path with
	// only a clean tree.
	SkipCompletionCheck bool
}

// Inspect validates every precondition for SubagentStop merge-back per
// BE-039 §8 / REQ-039 §13.6:
//
//   - completion report exists and parses
//   - worktree tree is clean
//   - source branch has commits beyond the merge base, or a professional
//     delivery records work that required no source changes
//   - target branch exists
//   - required checks pass (when configured)
//   - the source/target diff does not touch any locked artifact
//   - merge-tree reports no conflicts
//   - the merge mode is non-squash (always true in this package)
//
// When any precondition fails, Inspection.Ready is false and
// Inspection.Blockers carries human-readable reasons; the caller is then
// expected to surface those reasons as an integration blocker (the
// milestone update produced by Integrate does not advance the state
// machine).
func Inspect(ctx context.Context, req InspectRequest, cfg InspectConfig) (Inspection, error) {
	return inspectByResponsibility(ctx, req, cfg)
}

// completionReportBinding returns the durable, repository-relative identity
// of the report Inspect actually read. Both the lexical and resolved paths are
// checked so a report reference cannot escape the authority root through a
// symlink. Integrate and the quality gate consume this exact pair.
func completionReportBinding(root, reportPath string, data []byte) (string, string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	reportAbs, err := filepath.Abs(reportPath)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(rootAbs, reportAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("path %q is outside %q", reportPath, root)
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve root: %w", err)
	}
	resolvedReport, err := filepath.EvalSymlinks(reportAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolve report: %w", err)
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedReport)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
		return "", "", fmt.Errorf("resolved path %q is outside %q", reportPath, root)
	}
	return filepath.ToSlash(filepath.Clean(rel)), "", nil
}

// completionReportPath never substitutes another report for an explicit binding.
// Without a binding, only the current runtime/generation's canonical assignment
// location is eligible. Unknown identity requires explicit recovery, not a scan.
func completionReportPath(root, assignmentID, runtimeID, completionRef string, generation int) string {
	if completionRef != "" {
		if filepath.IsAbs(completionRef) {
			return completionRef
		}
		return filepath.Join(root, completionRef)
	}
	if runtimeID == "" || generation <= 0 || assignmentID == "" {
		return ""
	}
	for _, id := range []string{runtimeID, assignmentID} {
		if id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
			return ""
		}
	}
	return filepath.Join(root, ".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation), "assignments", assignmentID, "completion.json")
}

// lockedArtifacts combines authoritative paths with legacy explicit hints.
// Assignment WritePaths grants scope; it never grants permission to edit locks.
func lockedArtifacts(cfg InspectConfig, req InspectRequest) []string {
	locked := append([]string(nil), req.LockedPaths...)
	locked = append(locked, cfg.LockedArtifacts...)
	for _, hint := range cfg.RequiredChecks {
		if strings.HasPrefix(hint, "locked:") {
			locked = append(locked, strings.TrimPrefix(hint, "locked:"))
		}
	}
	return locked
}

func intersectLocked(files, locked []string) []string {
	var hits []string
	for _, file := range files {
		for _, l := range locked {
			if file == l || strings.HasSuffix(file, string(filepath.Separator)+l) {
				hits = append(hits, file)
				break
			}
		}
	}
	return hits
}

// outsideWriteScope returns the changed files not covered by any declared
// write path pattern. Patterns follow the manifest's glob conventions:
// `**` matches any number of path segments, `*` matches within one
// segment, and a bare directory pattern covers everything beneath it.
func outsideWriteScope(changed, writePaths []string) []string {
	var outside []string
	for _, file := range changed {
		if !pathMatchesAnyPattern(file, writePaths) {
			outside = append(outside, file)
		}
	}
	return outside
}

func pathMatchesAnyPattern(file string, patterns []string) bool {
	for _, pattern := range patterns {
		if pathMatchesPattern(file, pattern) {
			return true
		}
	}
	return false
}

// Keep inspection and Hook scope decisions on the same glob semantics.
func pathMatchesPattern(file, pattern string) bool { return workspace.PathMatchesScope(file, pattern) }

// CommandCheckRunner executes one required-check command through the shell
// in the repository root and fails on a non-zero exit. It is the default
// runner the Controller wires into Inspect/Integrate so "Required Checks
// verified" reflects real command executions (L3-S6 §11.2 "Integration
// checks 未接线").
