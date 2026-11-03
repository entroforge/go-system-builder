package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// inspectByResponsibility prepares Main's actual merge and checks from the
// original accepted responsibility and Git branch names. It never pins a Git
// object ID or file digest as a condition of receiving or integrating work.
func inspectByResponsibility(ctx context.Context, req InspectRequest, cfg InspectConfig) (Inspection, error) {
	if req.Root == "" {
		return Inspection{}, errors.New("root is required")
	}
	if strings.TrimSpace(req.Assignment.WorktreePath) == "" || strings.TrimSpace(req.Assignment.Branch) == "" {
		return Inspection{}, errors.New("assignment worktree and branch are required")
	}
	target := req.TargetBranch
	if target == "" {
		target = req.Assignment.TargetBranch
	}
	if target == "" {
		return Inspection{}, errors.New("target branch is required")
	}
	out := Inspection{AssignmentID: req.Assignment.AssignmentID, TaskID: req.Assignment.TaskID, WorktreePath: req.Assignment.WorktreePath, SourceBranch: req.Assignment.Branch, TargetBranch: target, BaselineGeneration: req.BaselineGeneration, NonSquashMode: true, LockedPaths: lockedArtifacts(cfg, req), EffectiveWritePaths: append([]string{}, req.Assignment.WritePaths...)}
	projection, err := cloneAssignmentProjection(req.Assignment)
	if err != nil {
		return out, err
	}
	out.AssignmentProjection = projection
	block := func(reason string) { out.Ready = false; out.Blockers = append(out.Blockers, reason) }
	canonical, err := resolveCanonicalResult(req.Root, req.RuntimeID, req.Assignment)
	if err != nil {
		block(fmt.Sprintf("original Assignment/Result unavailable: %v", err))
		return out, nil
	}
	if canonical != nil {
		out.CanonicalResult = &canonical.Binding
		out.CompletionReportPath = canonical.Binding.ResultPath
		out.EffectiveWritePaths = append([]string{}, canonical.Record.EffectiveWritePaths...)
		if blockers := canonicalClosingBlockers(canonical); len(blockers) != 0 {
			out.Blockers = append(out.Blockers, blockers...)
			return out, nil
		}
	} else if !cfg.SkipCompletionCheck {
		report := completionReportPath(req.Root, req.Assignment.AssignmentID, req.RuntimeID, req.Assignment.CompletionRef, req.BaselineGeneration)
		if report == "" {
			block("completion report location is unavailable")
			return out, nil
		}
		data, readErr := os.ReadFile(report)
		if readErr != nil {
			block(fmt.Sprintf("completion report missing at %s: %v", report, readErr))
			return out, nil
		}
		var value map[string]any
		if json.Unmarshal(data, &value) != nil {
			block("completion report is not valid JSON")
			return out, nil
		}
		if kind, _ := value["message_type"].(string); kind != "" && kind != "completion_report" {
			block(fmt.Sprintf("completion report has wrong message_type %q", kind))
			return out, nil
		}
		rel, _, err := completionReportBinding(req.Root, report, data)
		if err != nil {
			block(fmt.Sprintf("completion report path is unavailable: %v", err))
			return out, nil
		}
		out.CompletionReportPath = rel
	}
	exists, err := branchExists(ctx, req.Root, target)
	if err != nil {
		return out, err
	}
	if !exists {
		block(ErrMissingTarget.Error())
		return out, nil
	}
	// Git resolves branch names internally. The framework consumes actual
	// changed paths, not object IDs or a digest of that path set.
	diff, err := defaultRunner.Run(ctx, req.Root, "diff", "--no-renames", "--name-only", "-z", target+"..."+req.Assignment.Branch)
	if err != nil {
		block(fmt.Sprintf("cannot inspect actual branch changes: %v", err))
		return out, nil
	}
	out.ChangedPaths, err = canonicalizeGitPathOutput(diff)
	if err != nil {
		return out, err
	}
	// The Builder worktree state is a structural fact of the inspected
	// responsibility, so one read serves both consumers: the transient
	// guidance Main reads during an explicit native integration, and the
	// durable list of material that is really present. The dirty list never
	// decides readiness; the checkpoint records it so the site is preserved.
	observation, dirty := observeWorktreeCandidates(ctx, out)
	if cfg.ObserveWorktree {
		out.WorktreeObservation = observation
	}
	out.WorktreeDirty = dirty
	if hits := intersectLocked(out.ChangedPaths, out.LockedPaths); len(hits) != 0 {
		out.LockedDiff = hits
		block(fmt.Sprintf("%s: %s", ErrLockedArtifact, strings.Join(hits, ", ")))
		return out, nil
	}
	if len(out.EffectiveWritePaths) != 0 {
		out.OutOfScopeDiff = outsideWriteScope(out.ChangedPaths, out.EffectiveWritePaths)
		if len(out.OutOfScopeDiff) != 0 {
			block(fmt.Sprintf("%s: %s", ErrScopeViolation, strings.Join(out.OutOfScopeDiff, ", ")))
			return out, nil
		}
	}
	// Main's own checks run after merging. Non-canonical legacy callers may
	// also request actual pre-merge checks in the author's existing worktree.
	if canonical == nil {
		commands, _ := splitRequiredChecks(cfg.RequiredChecks)
		for _, command := range commands {
			row := CheckResult{Command: command, Status: "pass"}
			if cfg.CheckRunner == nil {
				row.Status, row.Output = "fail", ErrCheckRunnerMissing.Error()
			} else if err := cfg.CheckRunner(ctx, req.Assignment.WorktreePath, command); err != nil {
				row.Status, row.Output = "fail", err.Error()
			}
			out.RequiredChecks = append(out.RequiredChecks, row)
			if row.Status == "fail" {
				block(fmt.Sprintf("required check failed: %s", command))
			}
		}
		if len(out.Blockers) != 0 {
			return out, nil
		}
	}
	out.Ready = true
	return out, nil
}

// observeWorktreeCandidates supplies Main with context, not an assertion that
// any particular file belongs to the original Result. It reads the Builder
// worktree once and never changes readiness. It returns the transient
// guidance (used only by an explicit native S6 integration) and the material
// that is really present in the worktree; an unobservable worktree is
// unknown, never dirty.
func observeWorktreeCandidates(ctx context.Context, in Inspection) ([]string, []string) {
	budget, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	status, err := defaultRunner.Run(budget, in.WorktreePath, "status", "--porcelain=v1", "--untracked-files=normal", "--ignored=matching")
	branch := fmt.Sprintf("Source branch %q to target %q: %d changed path(s) observed for the branch integration", in.SourceBranch, in.TargetBranch, len(in.ChangedPaths))
	if len(in.ChangedPaths) > 0 {
		paths := in.ChangedPaths
		if len(paths) > 8 {
			paths = paths[:8]
		}
		quoted := make([]string, 0, len(paths))
		for _, path := range paths {
			quoted = append(quoted, fmt.Sprintf("%q", path))
		}
		branch += ": " + strings.Join(quoted, ", ")
		if len(in.ChangedPaths) > len(paths) {
			branch += fmt.Sprintf("; and %d more paths", len(in.ChangedPaths)-len(paths))
		}
	}
	if err != nil {
		// The worktree could not be observed: the material is unknown, so no
		// dirty entry is recorded and nothing is gated.
		return []string{branch, fmt.Sprintf("Worktree candidates at %q could not be observed (%v); their relationship to the delivered Result is unknown", in.WorktreePath, err)}, nil
	}
	if strings.TrimSpace(status) == "" {
		return nil, nil
	}
	lines := strings.Split(strings.TrimRight(status, "\r\n"), "\n")
	// Status can summarize directories, so the result is explicitly not a
	// complete file inventory. Output remains bounded even for large trees.
	const maxEntries = 12
	shown := make([]string, 0, maxEntries)
	for _, line := range lines {
		if line == "" {
			continue
		}
		if len(shown) == maxEntries {
			break
		}
		if len(line) > 240 {
			line = line[:240] + "..."
		}
		shown = append(shown, line)
	}
	if len(shown) == 0 {
		return nil, nil
	}
	detail := strings.Join(shown, "; ")
	if len(lines) > len(shown) {
		detail += fmt.Sprintf("; and %d more status entries", len(lines)-len(shown))
	}
	observation := []string{branch, fmt.Sprintf("Worktree status at %q (staged, unstaged, untracked or ignored; directories may be summarized): %s. These entries are not part of the branch merge; whether they belong to the delivery is unknown. Main uses the original Result and worktree to decide; private material is retained", in.WorktreePath, detail)}
	return observation, shown
}
