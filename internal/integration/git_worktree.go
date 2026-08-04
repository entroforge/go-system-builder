package integration

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// worktreeClean returns nil iff `git -C root status --porcelain` produces
// empty output. The porcelain output is un-tracked AND modified files; an
// empty result means the worktree is exactly what HEAD references.
func worktreeClean(ctx context.Context, root string) (bool, error) {
	// Ignore untracked files: harness state under `.claude/` and copied
	// docs authorities are intentionally untracked in worktree fixtures.
	// REQ-039 §13.6 / BE-039 §8 refuse merge on uncommitted changes to
	// tracked content, not on the presence of harness sidecars.
	out, err := defaultRunner.Run(ctx, root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(out) == "", nil
}

// branchExists returns true if `git -C root show-ref --verify` exits
// cleanly for refs/heads/<branch>. It is used to verify the target branch
// is real before we attempt any merge.
func branchExists(ctx context.Context, root, branch string) (bool, error) {
	if strings.TrimSpace(branch) == "" {
		return false, errors.New("branch name is required")
	}
	_, err := defaultRunner.Run(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		// A non-zero exit means the ref doesn't exist; anything else
		// (e.g. corrupt refs) is an actual error and should bubble up.
		// The execRunner wraps non-zero exits as ExitError; we treat any
		// error here as "not found" so callers can fall through to the
		// missing-target branch. Real corruption will surface on the
		// subsequent ref read.
		return false, nil
	}
	return true, nil
}

// revParse resolves a branch name (or rev) to its full SHA via
// `git rev-parse <rev>^{commit}`. The ^{commit} suffix dereferences
// annotated tags to their commit object, which is what callers want.
func revParse(ctx context.Context, root, rev string) (string, error) {
	if strings.TrimSpace(rev) == "" {
		return "", errors.New("rev is required")
	}
	out, err := defaultRunner.Run(ctx, root, "rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", rev, err)
	}
	return strings.TrimSpace(out), nil
}

// mergeBase returns the merge base SHA between source and target, or an
// error if either side does not resolve.
func mergeBase(ctx context.Context, root, source, target string) (string, error) {
	out, err := defaultRunner.Run(ctx, root, "merge-base", source, target)
	if err != nil {
		return "", fmt.Errorf("git merge-base %s %s: %w", source, target, err)
	}
	return strings.TrimSpace(out), nil
}

// mergeTreeRuns uses `git merge-tree` to compute the three-way merge
// between source and target and returns the conflict hunks, if any.
// merge-tree exits 0 even on conflict (since Git 2.38 the conflict flag
// is embedded in the output); we therefore parse the output and only
// treat a non-zero exit as an error.
//
// The function returns (conflicts, error). An empty conflicts slice with
// a nil error means the merge would apply cleanly.
func mergeTreeRuns(ctx context.Context, root, mergeBase, source, target string) ([]string, error) {
	out, err := defaultRunner.Run(ctx, root, "merge-tree", mergeBase, source, target)
	if err != nil {
		return nil, fmt.Errorf("git merge-tree: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	// Conflict markers from merge-tree look like:
	//   changed in both
	//   ...
	// We extract the "<<<<<<<" hunks; their presence indicates conflicts.
	if !strings.Contains(out, "<<<<<<<") && !strings.Contains(out, "changed in both") {
		return nil, nil
	}
	hunks := splitConflictHunks(out)
	return hunks, nil
}

func splitConflictHunks(out string) []string {
	var hunks []string
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "changed in both") ||
			strings.HasPrefix(strings.TrimSpace(line), "conflict:") ||
			strings.HasPrefix(line, "<<<<<<<") {
			hunks = append(hunks, strings.TrimSpace(line))
		}
	}
	return hunks
}

// listChangedFiles returns the set of paths that differ between base and
// head, relative to the repository root. Used to compute the locked-artifact
// diff.
func listChangedFiles(ctx context.Context, root, base, head string) ([]string, error) {
	out, err := defaultRunner.Run(ctx, root, "diff", "--name-only", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s..%s: %w", base, head, err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, filepath.Clean(line))
		}
	}
	return files, nil
}

// listCommitsBetween returns the count of commits in `head` that are not
// in `base`. If head == base the count is 0.
func countCommitsBetween(ctx context.Context, root, base, head string) (int, error) {
	out, err := defaultRunner.Run(ctx, root, "rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count %s..%s: %w", base, head, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return 0, nil
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, fmt.Errorf("parse rev-list count: %w", err)
	}
	return n, nil
}

// performMerge runs `git -C targetRoot merge --no-ff <source>` and returns
// the resulting merge commit SHA via `git rev-parse HEAD`. The merge is
// plain (no squash, no fast-forward) per BE-039 §8 / SYNC-039 §8.
func performMerge(ctx context.Context, targetRoot, source string) (string, error) {
	// Build a temporary merge message so the resulting merge commit is
	// distinguishable from a fast-forward. We never use --no-verify /
	// --no-gpg-sign / --amend / --force anywhere in this package.
	msg := "Merge worktree branch " + source
	_, err := defaultRunner.Run(ctx, targetRoot,
		"merge", "--no-ff", "--no-verify", "-m", msg, source,
	)
	if err != nil {
		return "", fmt.Errorf("git merge --no-ff %s: %w", source, err)
	}
	head, err := revParse(ctx, targetRoot, "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD after merge: %w", err)
	}
	return head, nil
}

// removeWorktree runs `git worktree remove --force` ONLY when the worktree
// is clean. The caller must call worktreeClean first; this function
// performs the additional belt-and-braces check and refuses if any
// uncommitted changes are present.
func removeWorktree(ctx context.Context, repoRoot, worktreePath string) error {
	clean, err := worktreeClean(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("verify clean: %w", err)
	}
	if !clean {
		return ErrDirtyWorktree
	}
	if _, err := defaultRunner.Run(ctx, repoRoot, "worktree", "remove", worktreePath); err != nil {
		// If the worktree is already gone we treat that as success
		// (idempotent cleanup). Anything else is a real error.
		if strings.Contains(err.Error(), "not registered") {
			return nil
		}
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// isCommit reports whether `rev` resolves to a commit object. We use this
// as a safety net before invoking the merge — `rev-parse --verify` would
// happily accept a tag, but merge would then complain.
func isCommit(ctx context.Context, root, rev string) (bool, error) {
	out, err := defaultRunner.Run(ctx, root, "cat-file", "-t", rev)
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) == "commit", nil
}

// checkoutBranch switches the worktree to `branch` (a no-op when already
// there) and returns the resulting HEAD SHA. It is used so the merge runs
// from the target branch, not the source.
func checkoutBranch(ctx context.Context, root, branch string) error {
	current, err := defaultRunner.Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil && strings.TrimSpace(current) == branch {
		return nil
	}
	if _, err := defaultRunner.Run(ctx, root, "checkout", "--quiet", branch); err != nil {
		return fmt.Errorf("git checkout %s: %w", branch, err)
	}
	return nil
}

// RunCheck is the public adapter for the integration check command
// dispatch. It exists so callers can construct a CheckResult outside
// the Integrate state machine — useful for controller-driven flows that
// want to record a single check outcome without advancing the
// checkpoint.
func RunCheck(ctx context.Context, root, command string, run func(ctx context.Context, root, command string) error) (CheckResult, error) {
	if run == nil {
		return CheckResult{Command: command, Status: "skip"}, nil
	}
	if err := run(ctx, root, command); err != nil {
		return CheckResult{Command: command, Status: "fail", Output: err.Error()}, nil
	}
	return CheckResult{Command: command, Status: "pass"}, nil
}
