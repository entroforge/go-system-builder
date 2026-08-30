package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// CheckRunner runs the command list in RequiredChecks. If nil, the
	// check step is recorded as "skip" so the contract's "default to none
	// if not specified" semantic is honoured.
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
//   - source branch has commits beyond the merge base
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
	if req.Root == "" {
		return Inspection{}, errors.New("root is required")
	}
	if strings.TrimSpace(req.Assignment.WorktreePath) == "" {
		return Inspection{}, errors.New("assignment worktree_path is required")
	}
	if strings.TrimSpace(req.Assignment.Branch) == "" {
		return Inspection{}, errors.New("assignment branch is required")
	}
	targetBranch := req.TargetBranch
	if targetBranch == "" {
		targetBranch = req.Assignment.TargetBranch
	}
	if targetBranch == "" {
		return Inspection{}, errors.New("target branch is required")
	}

	out := Inspection{
		AssignmentID:       req.Assignment.AssignmentID,
		TaskID:             req.Assignment.TaskID,
		WorktreePath:       req.Assignment.WorktreePath,
		SourceBranch:       req.Assignment.Branch,
		TargetBranch:       targetBranch,
		BaselineGeneration: req.BaselineGeneration,
		NonSquashMode:      true, // invariant: this package never squash-merges
	}
	addBlocker := func(reason string) {
		out.Ready = false
		out.Blockers = append(out.Blockers, reason)
	}

	// 1. Completion report. We don't schema-validate here — that gate is
	//    owned by the Quality Gate completion_report check (BUG-03 area).
	//    Inspect only confirms the file is present and parseable so a
	//    missing report is surfaced as an integration blocker, not as a
	//    generic controller failure.
	if !cfg.SkipCompletionCheck {
		reportPath := completionReportPath(req.Root, req.Assignment.AssignmentID, req.RuntimeID, req.Assignment.CompletionRef)
		data, err := os.ReadFile(reportPath)
		if err != nil {
			addBlocker(fmt.Sprintf("completion report missing at %s: %v", reportPath, err))
			return out, nil
		}
		var probe map[string]any
		if err := json.Unmarshal(data, &probe); err != nil {
			addBlocker(fmt.Sprintf("completion report is not valid JSON: %v", err))
			return out, nil
		}
		if kind, _ := probe["message_type"].(string); kind != "" && kind != "completion_report" {
			addBlocker(fmt.Sprintf("completion report has wrong message_type %q", kind))
			return out, nil
		}
	}

	// 2. Worktree clean.
	clean, err := worktreeClean(ctx, req.Assignment.WorktreePath)
	if err != nil {
		return out, fmt.Errorf("worktree clean check: %w", err)
	}
	if !clean {
		addBlocker(ErrDirtyWorktree.Error())
		return out, nil
	}

	// 3. Source branch has commits.
	sourceHead, err := revParse(ctx, req.Assignment.WorktreePath, req.Assignment.Branch)
	if err != nil {
		addBlocker(fmt.Sprintf("source branch head: %v", err))
		return out, nil
	}
	out.SourceHead = sourceHead

	// 4. Target branch exists.
	targetRepo := req.Root
	if exists, err := branchExists(ctx, targetRepo, targetBranch); err != nil {
		return out, fmt.Errorf("target branch check: %w", err)
	} else if !exists {
		addBlocker(ErrMissingTarget.Error())
		return out, nil
	}
	targetHead, err := revParse(ctx, targetRepo, targetBranch)
	if err != nil {
		addBlocker(fmt.Sprintf("target branch head: %v", err))
		return out, nil
	}
	out.TargetHead = targetHead

	// 5. Merge base + commit count.
	base, err := mergeBase(ctx, targetRepo, req.Assignment.Branch, targetBranch)
	if err != nil {
		addBlocker(fmt.Sprintf("merge base: %v", err))
		return out, nil
	}
	out.MergeBase = base
	count, err := countCommitsBetween(ctx, targetRepo, base, sourceHead)
	if err != nil {
		return out, fmt.Errorf("count commits: %w", err)
	}
	if count == 0 {
		addBlocker(ErrMissingCommits.Error())
		return out, nil
	}

	// 6. Locked-artifact diff. The locked list is supplied via the
	//    assignment context's WritePaths field plus any extra hints the
	//    caller wires into cfg.RequiredChecks via a separate hook. To
	//    keep this package self-contained we honour an explicit list in
	//    cfg.RequiredChecks (named RequiredLockedArtifacts would be
	//    cleaner, but it would force callers outside REQ-039 to learn a
	//    second name). We instead derive the locked list from the
	//    WritePaths + a parallel field AssignmentContext doesn't carry
	//    today; for the BUG-039-05 contract we accept a list passed via
	//    cfg and otherwise treat WritePaths as best-effort.
	locked := lockedArtifacts(cfg, req)
	// The changed-file list feeds both the locked-artifact screen and the
	// write-scope audit below; compute it once when either consumer needs
	// it.
	var changedFiles []string
	if len(locked) > 0 || len(req.Assignment.WritePaths) > 0 {
		files, err := listChangedFiles(ctx, targetRepo, base, sourceHead)
		if err != nil {
			return out, fmt.Errorf("list changed files: %w", err)
		}
		changedFiles = files
	}
	if len(locked) > 0 {
		out.LockedDiff = intersectLocked(changedFiles, locked)
		if len(out.LockedDiff) > 0 {
			addBlocker(fmt.Sprintf("%s: %s", ErrLockedArtifact.Error(), strings.Join(out.LockedDiff, ", ")))
			return out, nil
		}
	}

	// 6b. Write-scope audit (L3-S6 §7.4 condition 4): the real diff must
	//     be a subset of the assignment's declared WritePaths. An
	//     assignment without WritePaths declares no scope, so the audit
	//     is skipped rather than fabricated — the gap is visible in the
	//     manifest, not hidden here. Out-of-scope files block the
	//     integration and preserve the worktree.
	if len(req.Assignment.WritePaths) > 0 {
		out.OutOfScopeDiff = outsideWriteScope(changedFiles, req.Assignment.WritePaths)
		if len(out.OutOfScopeDiff) > 0 {
			addBlocker(fmt.Sprintf("%s: %s", ErrScopeViolation.Error(), strings.Join(out.OutOfScopeDiff, ", ")))
			return out, nil
		}
	}

	// 7. Conflict detection via merge-tree.
	conflicts, err := mergeTreeRuns(ctx, targetRepo, base, req.Assignment.Branch, targetBranch)
	if err != nil {
		return out, fmt.Errorf("merge-tree: %w", err)
	}
	if len(conflicts) > 0 {
		out.Conflicts = conflicts
		addBlocker(ErrMergeConflict.Error())
		return out, nil
	}

	// 8. Required checks.
	if len(cfg.RequiredChecks) > 0 {
		for _, command := range cfg.RequiredChecks {
			res := CheckResult{Command: command}
			if cfg.CheckRunner != nil {
				if err := cfg.CheckRunner(ctx, targetRepo, command); err != nil {
					res.Status = "fail"
					res.Output = err.Error()
				} else {
					res.Status = "pass"
				}
			} else {
				res.Status = "skip"
			}
			out.RequiredChecks = append(out.RequiredChecks, res)
			if res.Status == "fail" {
				addBlocker(fmt.Sprintf("required check failed: %s", command))
			}
		}
		if hasFailing(out.RequiredChecks) {
			return out, nil
		}
	}

	out.Ready = true
	return out, nil
}

// completionReportPath returns the location of the completion report for
// an assignment. Preferred order:
//
//  1. Explicit CompletionRef on the assignment (when the file exists)
//  2. `.claude/evidence/<runtimeID>/g*/assignments/<id>/completion.json`
//  3. Legacy loop-REQ-039 paths
//  4. Broad scan under `.claude/evidence/*/g*/assignments/<id>/`
//
// Because the generator (BUG-04) writes the report at assignment creation
// time using the same layout, Inspect just looks up the path. The runtime
// id is best-effort — Inspect does not fail when it is missing because
// the contract §4.1 only requires the file's existence.
func completionReportPath(root, assignmentID, runtimeID, completionRef string) string {
	if completionRef != "" {
		path := completionRef
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	var candidates []string
	if runtimeID != "" {
		candidates = append(candidates,
			filepath.Join(root, ".claude", "evidence", runtimeID, "g1", "assignments", assignmentID, "completion.json"),
			filepath.Join(root, ".claude", "evidence", runtimeID, "assignments", assignmentID, "completion.json"),
		)
	}
	candidates = append(candidates,
		filepath.Join(root, ".claude", "evidence", "loop-REQ-039", "g1", "assignments", assignmentID, "completion.json"),
		filepath.Join(root, ".claude", "evidence", "loop-REQ-039", "assignments", assignmentID, "completion.json"),
		filepath.Join(root, ".claude", "evidence", "assignments", assignmentID, "completion.json"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if found := scanCompletionReport(root, assignmentID); found != "" {
		return found
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(root, ".claude", "evidence", "loop-REQ-039", "g1", "assignments", assignmentID, "completion.json")
}

func scanCompletionReport(root, assignmentID string) string {
	evidenceRoot := filepath.Join(root, ".claude", "evidence")
	runtimeEntries, err := os.ReadDir(evidenceRoot)
	if err != nil {
		return ""
	}
	for _, runtimeEntry := range runtimeEntries {
		if !runtimeEntry.IsDir() {
			continue
		}
		runtimeDir := filepath.Join(evidenceRoot, runtimeEntry.Name())
		genEntries, err := os.ReadDir(runtimeDir)
		if err != nil {
			continue
		}
		for _, genEntry := range genEntries {
			if !genEntry.IsDir() {
				continue
			}
			path := filepath.Join(runtimeDir, genEntry.Name(), "assignments", assignmentID, "completion.json")
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
		path := filepath.Join(runtimeDir, "assignments", assignmentID, "completion.json")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// lockedArtifacts returns the merged list of locked artifact paths the
// Inspect step enforces. The list is sourced from:
//   - cfg.RequiredChecks (reused as "explicit locked hints" — see Inspect
//     for the rationale),
//   - assignment.WritePaths (best-effort — WritePaths is the agent's
//     declared allow-list, not the locked manifest; we intersect with
//     anything tagged "locked:" to keep the heuristic safe).
func lockedArtifacts(cfg InspectConfig, req InspectRequest) []string {
	var locked []string
	for _, hint := range cfg.RequiredChecks {
		// Convention: a RequiredChecks entry prefixed with "locked:" is
		// actually a locked-artifact path, not a check command. This
		// lets callers pass both signals in one slice without expanding
		// InspectConfig.
		if strings.HasPrefix(hint, "locked:") {
			locked = append(locked, strings.TrimPrefix(hint, "locked:"))
		}
	}
	// Nothing else: WritePaths is permissive, not locked. Locked
	// artifacts are owned by the hookctx loader (LockedArtifacts on
	// PolicyContext), but InspectRequest does not carry that field by
	// design — see hookctx/types.go. The Controller (BUG-02) supplies
	// them when wiring this call from SubagentStop.
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

func hasFailing(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == "fail" {
			return true
		}
	}
	return false
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

// pathMatchesPattern reports whether a slash-separated repo-relative file
// path matches one write-path pattern. A pattern ending in "/**" (or a
// bare directory without wildcards) covers the whole subtree.
func pathMatchesPattern(file, pattern string) bool {
	file = strings.Trim(file, "/")
	pattern = strings.Trim(pattern, "/")
	if file == "" || pattern == "" {
		return false
	}
	if pattern == "**" {
		return true
	}
	fileParts := strings.Split(file, "/")
	patternParts := strings.Split(pattern, "/")
	// A directory-only pattern (no extension and no wildcard in the last
	// segment) is a prefix match: "internal/order" covers
	// internal/order/anything.go.
	if !strings.Contains(patternParts[len(patternParts)-1], "*") &&
		!strings.Contains(patternParts[len(patternParts)-1], ".") {
		return pathUnderDirectory(fileParts, patternParts)
	}
	return segmentsMatch(fileParts, 0, patternParts, 0)
}

func pathUnderDirectory(fileParts []string, dirParts []string) bool {
	if len(fileParts) <= len(dirParts) {
		return false
	}
	for i, part := range dirParts {
		if !segmentGlob(part, fileParts[i]) {
			return false
		}
	}
	return true
}

// segmentsMatch implements `**` (any number of segments) and `*`
// (within one segment) glob matching over slash-split paths.
func segmentsMatch(file []string, fi int, pattern []string, pi int) bool {
	if pi == len(pattern) {
		return fi == len(file)
	}
	if pattern[pi] == "**" {
		for skip := fi; skip <= len(file); skip++ {
			if segmentsMatch(file, skip, pattern, pi+1) {
				return true
			}
		}
		return false
	}
	if fi == len(file) {
		return false
	}
	if !segmentGlob(pattern[pi], file[fi]) {
		return false
	}
	return segmentsMatch(file, fi+1, pattern, pi+1)
}

// segmentGlob matches one path segment with `*` wildcards.
func segmentGlob(pattern, segment string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == segment
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == segment
	}
	if !strings.HasPrefix(segment, parts[0]) || !strings.HasSuffix(segment, parts[len(parts)-1]) {
		return false
	}
	rest := segment
	if len(parts[0]) > 0 {
		rest = strings.TrimPrefix(rest, parts[0])
	}
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(rest, part)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(part):]
	}
	if tail := parts[len(parts)-1]; len(tail) > 0 {
		return strings.HasSuffix(rest, tail) || strings.Contains(rest, tail)
	}
	return true
}

// CommandCheckRunner executes one required-check command through the shell
// in the repository root and fails on a non-zero exit. It is the default
// runner the Controller wires into Inspect/Integrate so "Required Checks
// verified" reflects real command executions (L3-S6 §11.2 "Integration
// checks 未接线").
func CommandCheckRunner(ctx context.Context, root, command string) error {
	execCmd := exec.CommandContext(ctx, "sh", "-c", command)
	execCmd.Dir = root
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("check %q failed (%v): %s", command, err, strings.TrimSpace(string(output)))
	}
	return nil
}
