package integration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
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
	// WorkerLogEvidence contains exact captured output identities supplied by the
	// original domain receiver. Only untracked matching bytes may be projected;
	// this is used only with ValidateDelivery, never as a general cleanliness bypass.
	WorkerLogEvidence map[string]string
	// WorkerPrivateEvidence is the exact original accepted professional input.
	WorkerPrivateEvidence map[string]string
	// WorkerPrivateArea and its frozen source facts are call-only data from the
	// original native responsibility; they never authorize product changes.
	WorkerPrivateArea          string
	WorkerPrivateBaseCommit    string
	WorkerPrivateWritePaths    []string
	WorkerPrivateBaselinePaths []string
	LockedArtifacts            []string
	// RecoveryBase retains the original scope denominator for an already merged branch.
	RecoveryBase string
	// ReceivedSourceHead is an immutable source from the original domain
	// receiver. With ValidateDelivery, a removed native checkout may reuse it
	// only when its entire source range is empty. This never substitutes Main
	// for Worker checks or changes the original branch/path coordinates.
	ReceivedSourceHead string
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
	// ValidateDelivery validates an immutable domain candidate against the
	// inspected source SHA. It replaces only the generic completion envelope;
	// Git cleanliness, scope, locks, checks and merge preconditions still run.
	ValidateDelivery func(context.Context, Inspection) error
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
		LockedPaths:          lockedArtifacts(cfg, req),
		AssignmentID:         req.Assignment.AssignmentID,
		TaskID:               req.Assignment.TaskID,
		WorktreePath:         req.Assignment.WorktreePath,
		SourceBranch:         req.Assignment.Branch,
		TargetBranch:         targetBranch,
		BaselineGeneration:   req.BaselineGeneration,
		NonSquashMode:        true, // invariant: this package never squash-merges
		EffectiveWritePaths:  append([]string{}, req.Assignment.WritePaths...),
		AssignmentProjection: req.Assignment,
	}
	projection, err := cloneAssignmentProjection(req.Assignment)
	if err != nil {
		return Inspection{}, fmt.Errorf("copy canonical Assignment projection: %w", err)
	}
	out.AssignmentProjection = projection
	addBlocker := func(reason string) {
		out.Ready = false
		out.Blockers = append(out.Blockers, reason)
	}

	canonical, err := resolveCanonicalResult(req.Root, req.RuntimeID, req.Assignment)
	if err != nil {
		addBlocker(fmt.Sprintf("canonical Assignment/Result binding failed: %v", err))
		return out, nil
	}
	if canonical != nil {
		out.CanonicalResult = &canonical.Binding
		out.CompletionReportPath = canonical.Binding.ResultPath
		out.CompletionReportSHA256 = canonical.Binding.ResultSHA256
		out.EffectiveWritePaths = append([]string{}, canonical.Record.EffectiveWritePaths...)
		if blockers := canonicalClosingBlockers(canonical); len(blockers) > 0 {
			for _, blocker := range blockers {
				addBlocker(blocker)
			}
			return out, nil
		}
		// v1.2 RequiredChecks are Main post-merge facts. Inspect uses the
		// canonical scope and locks but does not treat worker-side checks as
		// Main receipts or a completion verdict.
		cfg.RequiredChecks = nil
	}

	// Domain delivery retains declared scope as professional context, without
	// forwarding the generic S6 path ACL into Main integration.
	if cfg.ValidateDelivery != nil && canonical == nil {
		out.EffectiveWritePaths = nil
	}

	// 1. Completion report. We don't schema-validate here — that gate is
	//    owned by the Quality Gate completion_report check (BUG-03 area).
	//    Inspect only confirms the file is present and parseable so a
	//    missing report is surfaced as an integration blocker, not as a
	//    generic controller failure.
	if canonical != nil {
		out.CompletionReportPath = canonical.Binding.ResultPath
		out.CompletionReportSHA256 = canonical.Binding.ResultSHA256
	} else if !cfg.SkipCompletionCheck && cfg.ValidateDelivery == nil {
		reportPath := completionReportPath(req.Root, req.Assignment.AssignmentID, req.RuntimeID, req.Assignment.CompletionRef, req.BaselineGeneration)
		if reportPath == "" {
			addBlocker("completion report missing: provide an explicit CompletionRef or valid current Runtime, generation and assignment identity")
			return out, nil
		}
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
		relPath, reportSHA, err := completionReportBinding(req.Root, reportPath, data)
		if err != nil {
			addBlocker(fmt.Sprintf("completion report path is outside the authority root: %v", err))
			return out, nil
		}
		out.CompletionReportPath = relPath
		out.CompletionReportSHA256 = reportSHA
	}

	// 2. Inspect a live checkout normally. Claude Code may already have
	// removed an accepted no-change native checkout and its temporary branch.
	sourceRef, sourceRoot := req.Assignment.Branch, req.Assignment.WorktreePath
	released := false
	if canonical == nil && cfg.ValidateDelivery != nil && cfg.ReceivedSourceHead != "" {
		if _, pathErr := os.Lstat(req.Assignment.WorktreePath); os.IsNotExist(pathErr) {
			released = true
			sourceRef, sourceRoot = cfg.ReceivedSourceHead, req.Root
		}
	}
	if !released {
		if canonical != nil && canonical.Record.Contract == assignmentrecord.NativeDeliveryContract {
			if _, pathErr := os.Lstat(req.Assignment.WorktreePath); os.IsNotExist(pathErr) {
				return out, fmt.Errorf("accepted native S6 checkout %q is missing; Main restores the original Git checkout/branch %q at accepted SourceHead %s, then retries the original task-integrate. The original Plan/Result are already retained in Main; do not recreate private answer caches, recapture HEAD, or add a new commit to repair source identity", req.Assignment.WorktreePath, req.Assignment.Branch, canonical.Accepted.SourceHead)
			}
		}
		var logEvidence, privateEvidence map[string]string
		var privateArea *WorkerPrivateAreaProjection
		if canonical == nil && cfg.ValidateDelivery != nil {
			logEvidence = cfg.WorkerLogEvidence
			privateEvidence = cfg.WorkerPrivateEvidence
			privateArea, err = ObserveWorkerPrivateArea(ctx, req.Assignment.WorktreePath, WorkerPrivateAreaSpec{
				Path: cfg.WorkerPrivateArea, BaseCommit: cfg.WorkerPrivateBaseCommit,
				WritePaths: cfg.WorkerPrivateWritePaths, BaselinePaths: cfg.WorkerPrivateBaselinePaths,
			})
			if err != nil {
				return out, fmt.Errorf("observe original Worker private area: %w", err)
			}
		}
		clean, err := workerWorktreeCleanWithPrivateArea(ctx, req.Root, req.Assignment.WorktreePath, privateArea, logEvidence, privateEvidence)
		if err != nil {
			workerRoot := req.Assignment.WorktreePath
			return out, fmt.Errorf("Worker checkout %q clean check failed; inspect with git -C %q status --short: %w", workerRoot, workerRoot, err)
		}
		if !clean {
			workerRoot := req.Assignment.WorktreePath
			addBlocker(fmt.Sprintf("Worker checkout %q is dirty; inspect with git -C %q status --short: %s", workerRoot, workerRoot, ErrDirtyWorktree))
			return out, nil
		}
	}

	// 3. Resolve the actual Git object, retaining the original metadata.
	sourceHead, err := revParse(ctx, sourceRoot, sourceRef)
	if err != nil || released && sourceHead != cfg.ReceivedSourceHead {
		addBlocker(fmt.Sprintf("original source commit unavailable: %v", err))
		return out, nil
	}
	out.SourceHead = sourceHead
	if canonical != nil && canonical.Record.Contract == assignmentrecord.NativeDeliveryContract && canonical.Accepted.SourceHead != sourceHead {
		addBlocker(fmt.Sprintf("native source commit changed after accepted Result; expected SourceHead %s at checkout %q branch %q. Main restores that exact accepted source for the original task-integrate, or uses the original named Assignment revision for genuinely new work; never recapture HEAD or rewrite the accepted Result", canonical.Accepted.SourceHead, req.Assignment.WorktreePath, req.Assignment.Branch))
		return out, nil
	}

	if cfg.ValidateDelivery != nil {
		if err := cfg.ValidateDelivery(ctx, out); err != nil {
			addBlocker(err.Error())
			return out, nil
		}
	}

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
	base, err := mergeBase(ctx, targetRepo, sourceRef, targetBranch)
	if err != nil {
		addBlocker(fmt.Sprintf("merge base: %v", err))
		return out, nil
	}
	if cfg.RecoveryBase != "" {
		// Both tips must descend from the frozen pre-merge base. Never
		// compute an empty diff from the now-merged branch's current base.
		for _, tip := range []string{sourceHead, targetHead} {
			if _, err := defaultRunner.Run(ctx, targetRepo, "merge-base", "--is-ancestor", cfg.RecoveryBase, tip); err != nil {
				return out, fmt.Errorf("recovery base is not an ancestor of %s", tip)
			}
		}
		base = cfg.RecoveryBase
	}
	out.MergeBase = base
	count, err := countCommitsBetween(ctx, targetRepo, base, sourceHead)
	if err != nil {
		return out, fmt.Errorf("count commits: %w", err)
	}
	if released && count != 0 {
		addBlocker("removed Worker source has a nonempty Git range; Main must recover the original delivery")
		return out, nil
	}
	if count == 0 && (cfg.ValidateDelivery == nil || canonical != nil) {
		addBlocker(ErrMissingCommits.Error())
		return out, nil
	}

	// 6. Enforce the Runtime lock set independently of task checks.
	locked := out.LockedPaths
	// Capture the full actual Git delta, even if this assignment has no lock
	// or scope rule. The same immutable path set feeds both audits and the
	// durable integration checkpoint.
	changedFiles, changedDigest, err := canonicalGitChangedPaths(ctx, targetRepo, base, sourceHead)
	if err != nil {
		return out, fmt.Errorf("list changed files: %w", err)
	}
	out.ChangedPaths = changedFiles
	out.ChangedPathsSHA256 = changedDigest
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
	//     integration and preserve the worktree. Domain delivery records the
	//     deviation for Main judgment while retaining the actual diff.
	if len(req.Assignment.WritePaths) > 0 {
		out.OutOfScopeDiff = outsideWriteScope(changedFiles, req.Assignment.WritePaths)
		if len(out.OutOfScopeDiff) > 0 && (cfg.ValidateDelivery == nil || canonical != nil) {
			addBlocker(fmt.Sprintf("%s: %s", ErrScopeViolation.Error(), strings.Join(out.OutOfScopeDiff, ", ")))
			return out, nil
		}
	}

	// 7. Conflict detection via merge-tree.
	conflicts, err := mergeTreeRuns(ctx, targetRepo, base, sourceRef, targetBranch)
	if err != nil {
		return out, fmt.Errorf("merge-tree: %w", err)
	}
	if len(conflicts) > 0 {
		out.Conflicts = conflicts
		addBlocker(ErrMergeConflict.Error())
		return out, nil
	}

	// 8. Required checks.
	if released && len(cfg.RequiredChecks) != 0 {
		addBlocker("required Worker checks have no live checkout; Main must recover their original source")
		return out, nil
	}
	commands, _ := splitRequiredChecks(cfg.RequiredChecks)
	if len(commands) > 0 {
		// Pre-merge checks must run in the delivered worker checkout. Running
		// them against the authority root here can pass or fail on files that
		// are not part of the candidate; Integrate repeats the same commands
		// against gitRoot after the merge before recording verified.
		checkRoot := req.Assignment.WorktreePath
		for _, command := range commands {
			res := CheckResult{Command: command}
			if cfg.CheckRunner != nil {
				if err := cfg.CheckRunner(ctx, checkRoot, command); err != nil {
					res.Status = "fail"
					res.Output = err.Error()
				} else {
					res.Status = "pass"
				}
			} else {
				res.Status = "fail"
				res.Output = ErrCheckRunnerMissing.Error()
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
	return filepath.ToSlash(filepath.Clean(rel)), fmt.Sprintf("%x", sha256.Sum256(data)), nil
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

// Keep inspection and Hook scope decisions on the same glob semantics.
func pathMatchesPattern(file, pattern string) bool { return workspace.PathMatchesScope(file, pattern) }

// CommandCheckRunner executes one required-check command through the shell
// in the repository root and fails on a non-zero exit. It is the default
// runner the Controller wires into Inspect/Integrate so "Required Checks
// verified" reflects real command executions (L3-S6 §11.2 "Integration
// checks 未接线").
