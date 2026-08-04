package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeRunner is a deterministic git implementation used by tests. It tracks
// the in-memory state of one repository so we can exercise the state
// machine without depending on the system git binary.
//
// The runner deliberately implements a tiny subset — enough to drive
// Inspect + Integrate through the canonical success and failure paths.
// Anything it does not understand returns an explicit unsupported
// error so missing-feature bugs are loud, not silent.
type fakeRunner struct {
	// branchHeads maps "branch" -> commit SHA
	branchHeads map[string]string
	// workdirHeads maps a worktree root path -> branch it currently has
	// checked out (and therefore its HEAD SHA via branchHeads).
	workdirBranches map[string]string
	// workdirClean tracks whether each worktree has uncommitted changes
	workdirClean map[string]bool
	// commits records commit SHAs and their parents. Used to compute
	// merge bases and rev-list counts.
	commits map[string]fakeCommit
	// fileContents maps "worktree:path" -> content. Used to materialise
	// diff outputs.
	fileContents map[string]string
	// mergeTreeOutput, when set, is returned verbatim from merge-tree.
	mergeTreeOutput string
	// worktrees maps "<repoRoot>:<path>" -> registered. removeWorktree
	// flips the value to false.
	worktrees map[string]bool
}

type fakeCommit struct {
	sha     string
	parents []string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		branchHeads:     map[string]string{},
		workdirBranches: map[string]string{},
		workdirClean:    map[string]bool{},
		commits:         map[string]fakeCommit{},
		fileContents:    map[string]string{},
		worktrees:       map[string]bool{},
	}
}

func (f *fakeRunner) Run(ctx context.Context, root string, args ...string) (string, error) {
	return f.run(ctx, "", root, args...)
}

func (f *fakeRunner) RunStdin(ctx context.Context, stdin string, root string, args ...string) (string, error) {
	return f.run(ctx, stdin, root, args...)
}

func (f *fakeRunner) run(ctx context.Context, stdin string, root string, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("fakeRunner: empty args")
	}
	switch args[0] {
	case "status":
		if len(args) >= 2 && args[1] == "--porcelain" {
			if f.workdirClean[root] {
				return "", nil
			}
			return "?? dirty-file\n", nil
		}
	case "show-ref":
		if len(args) >= 4 && args[2] == "--quiet" {
			ref := args[3]
			if _, ok := f.branchHeads[strings.TrimPrefix(ref, "refs/heads/")]; ok {
				return "", nil
			}
			return "", fmt.Errorf("fakeRunner: show-ref %s failed", ref)
		}
	case "rev-parse":
		if len(args) >= 3 && args[1] == "--verify" {
			rev := args[2]
			if strings.HasSuffix(rev, "^{commit}") {
				rev = strings.TrimSuffix(rev, "^{commit}")
			}
			if c, ok := f.commits[rev]; ok {
				return c.sha, nil
			}
			if head, ok := f.branchHeads[rev]; ok {
				return head, nil
			}
			// "HEAD" — resolve via the workdir's current branch.
			if rev == "HEAD" {
				if b, ok := f.workdirBranches[root]; ok {
					if h, ok := f.branchHeads[b]; ok {
						return h, nil
					}
				}
			}
			return "", fmt.Errorf("fakeRunner: rev-parse %s unknown", rev)
		}
		if len(args) >= 2 && args[1] == "--abbrev-ref" && args[2] == "HEAD" {
			if b, ok := f.workdirBranches[root]; ok {
				return b, nil
			}
			return "HEAD", nil
		}
	case "merge-base":
		if len(args) >= 3 {
			return f.computeMergeBase(args[1], args[2]), nil
		}
	case "rev-list":
		if len(args) >= 3 && args[1] == "--count" {
			rng := args[2]
			parts := strings.SplitN(rng, "..", 2)
			if len(parts) == 2 {
				return f.countCommitsBetween(parts[0], parts[1]), nil
			}
		}
	case "diff":
		if len(args) >= 3 && args[1] == "--name-only" {
			rng := args[2]
			parts := strings.SplitN(rng, "..", 2)
			if len(parts) == 2 {
				return f.diffNameOnly(parts[0], parts[1]), nil
			}
		}
	case "merge-tree":
		if f.mergeTreeOutput != "" {
			return f.mergeTreeOutput, nil
		}
		return "", nil
	case "cat-file":
		if len(args) >= 3 && args[1] == "-t" {
			if _, ok := f.commits[args[2]]; ok {
				return "commit", nil
			}
		}
	case "checkout":
		if len(args) >= 3 && args[2] == "--quiet" {
			branch := args[len(args)-1]
			f.workdirBranches[root] = branch
			return "", nil
		}
	case "merge":
		// args: merge --no-ff -m <msg> <source>
		if len(args) >= 3 && args[1] == "--no-ff" {
			source := args[len(args)-1]
			head, ok := f.branchHeads[source]
			if !ok || head == "" {
				return "", fmt.Errorf("fakeRunner: merge source %s unknown", source)
			}
			currentBranch, _ := f.workdirBranches[root]
			currentHead := f.branchHeads[currentBranch]
			mergeSHA := "merge-" + currentHead + "-" + head
			f.commits[mergeSHA] = fakeCommit{sha: mergeSHA, parents: []string{currentHead, head}}
			f.branchHeads[currentBranch] = mergeSHA
			return "", nil
		}
	case "worktree":
		if len(args) >= 3 && args[1] == "remove" {
			path := args[2]
			key := root + ":" + path
			if _, ok := f.worktrees[key]; !ok {
				// Already gone: idempotent success per BE-039 §8.
				return "", nil
			}
			delete(f.worktrees, key)
			return "", nil
		}
	}
	return "", fmt.Errorf("fakeRunner: unsupported args %v in %s", args, root)
}

// Helper functions used by the fake runner.

// addCommit registers a commit and returns its SHA.
func (f *fakeRunner) addCommit(sha string, parents ...string) string {
	f.commits[sha] = fakeCommit{sha: sha, parents: parents}
	return sha
}

// setBranch binds a branch name to a commit SHA.
func (f *fakeRunner) setBranch(branch, sha string) {
	f.branchHeads[branch] = sha
}

// checkout marks `root` as currently on `branch`.
func (f *fakeRunner) checkout(root, branch string) {
	f.workdirBranches[root] = branch
	f.workdirClean[root] = true
}

// markDirty flags `root` as having uncommitted changes.
func (f *fakeRunner) markDirty(root string) {
	f.workdirClean[root] = false
}

// markClean flags `root` as clean.
func (f *fakeRunner) markClean(root string) {
	f.workdirClean[root] = true
}

// addWorktree registers a worktree entry so removeWorktree is allowed.
func (f *fakeRunner) addWorktree(repoRoot, path string) {
	f.worktrees[repoRoot+":"+path] = true
}

func (f *fakeRunner) computeMergeBase(a, b string) string {
	// Translate branch names to commit SHAs so the caller can pass
	// either a branch or a SHA. This matches the real `git merge-base`
	// behaviour.
	if sha, ok := f.branchHeads[a]; ok {
		a = sha
	}
	if sha, ok := f.branchHeads[b]; ok {
		b = sha
	}
	// Walk ancestors of a until we find one that also appears in b's
	// ancestor chain. This is good enough for tests; production
	// behaviour is provided by `git merge-base`.
	aAncestors := f.ancestors(a)
	for _, sha := range aAncestors {
		for _, other := range f.ancestors(b) {
			if sha == other {
				return sha
			}
		}
	}
	return ""
}

func (f *fakeRunner) ancestors(sha string) []string {
	var out []string
	visited := map[string]bool{}
	var walk func(string)
	walk = func(s string) {
		if visited[s] {
			return
		}
		visited[s] = true
		c, ok := f.commits[s]
		if !ok {
			return
		}
		out = append(out, s)
		for _, p := range c.parents {
			walk(p)
		}
	}
	walk(sha)
	return out
}

func (f *fakeRunner) countCommitsBetween(base, head string) string {
	// Resolve branch names to SHAs.
	if sha, ok := f.branchHeads[base]; ok {
		base = sha
	}
	if sha, ok := f.branchHeads[head]; ok {
		head = sha
	}
	if base == head {
		return "0"
	}
	ancestors := map[string]bool{}
	for _, sha := range f.ancestors(base) {
		ancestors[sha] = true
	}
	count := 0
	for _, sha := range f.ancestors(head) {
		if !ancestors[sha] {
			count++
		}
	}
	// Subtract 1 for the head itself when it equals base (handled by
	// the ancestor loop above — head is always included in its own
	// ancestor list).
	if ancestors[head] {
		count--
	}
	if count < 0 {
		count = 0
	}
	return fmt.Sprintf("%d", count)
}

func (f *fakeRunner) diffNameOnly(base, head string) string {
	// Track which files changed between base and head via the
	// fileContents "worktree:path" map. We compare the keys present in
	// head but missing in base, and vice versa.
	headFiles := map[string]bool{}
	baseFiles := map[string]bool{}
	for k := range f.fileContents {
		if strings.HasPrefix(k, head+":") {
			headFiles[strings.TrimPrefix(k, head+":")] = true
		}
		if strings.HasPrefix(k, base+":") {
			baseFiles[strings.TrimPrefix(k, base+":")] = true
		}
	}
	var diff []string
	for k := range headFiles {
		if !baseFiles[k] {
			diff = append(diff, k)
		}
	}
	return strings.Join(diff, "\n")
}

// fakeRunnerT is a tiny shim that lets tests grab a fresh fakeRunner
// without manually invoking withRunner. The helper exists because every
// test file wants one and we don't want to import the package-global
// internals from a sibling file.
func fakeRunnerT(t *testing.T) (*fakeRunner, func()) {
	t.Helper()
	fr := newFakeRunner()
	restore := withRunner(fr)
	return fr, restore
}

// runFakeRunner is a tiny convenience that returns a fresh runner and
// hooks it into the package default. Tests should defer the returned
// restore function.
func runFakeRunner(t *testing.T) (*fakeRunner, func()) {
	t.Helper()
	return fakeRunnerT(t)
}

// compile-time assertion: fakeRunner implements gitRunner.
var _ gitRunner = (*fakeRunner)(nil)
