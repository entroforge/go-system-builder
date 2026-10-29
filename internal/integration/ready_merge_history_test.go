package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadyMergeRecoveryRejectsDifferentActualParentHistories(t *testing.T) {
	for _, scenario := range []string{"reversed-parents", "wrong-target", "wrong-source", "octopus", "side-parent-only", "source-advanced", "wrong-branch"} {
		t.Run(scenario, func(t *testing.T) {
			root, worker, in, _, git := newRecoveryGitRepo(t)
			cp := Checkpoint{State: StateReady, SourceBranch: in.SourceBranch, SourceHead: in.SourceHead, TargetBranch: in.TargetBranch, TargetHead: in.TargetHead, WorktreePath: worker}
			commitMain := func(name string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, name), []byte("fixture\n"), 0600); err != nil {
					t.Fatal(err)
				}
				git(root, "add", name)
				git(root, "commit", "-m", name)
			}
			switch scenario {
			case "reversed-parents":
				merge := git(root, "commit-tree", git(worker, "rev-parse", "HEAD^{tree}"), "-p", cp.SourceHead, "-p", cp.TargetHead, "-m", "reversed parent order fixture")
				git(root, "reset", "--hard", merge)
			case "wrong-target":
				commitMain("new-target")
				git(root, "merge", "--no-ff", cp.SourceHead, "-m", "different target parent")
			case "wrong-source":
				commitMain("different-source")
				other := git(root, "rev-parse", "HEAD")
				git(root, "reset", "--hard", cp.TargetHead)
				git(root, "merge", "--no-ff", other, "-m", "different source parent")
			case "octopus":
				commitMain("other-source")
				other := git(root, "rev-parse", "HEAD")
				git(root, "reset", "--hard", cp.TargetHead)
				git(root, "merge", "--no-ff", cp.SourceHead, other, "-m", "octopus is not the intended merge")
			case "side-parent-only":
				git(root, "checkout", "-b", "side", cp.TargetHead)
				git(root, "merge", "--no-ff", cp.SourceHead, "-m", "exact pair on a different branch")
				git(root, "checkout", cp.TargetBranch)
				commitMain("main-first-parent")
				git(root, "merge", "--no-ff", "side", "-m", "exact pair is reachable only via a side parent")
			case "source-advanced":
				git(root, "merge", "--no-ff", cp.SourceHead, "-m", "original merge")
				if err := os.WriteFile(filepath.Join(worker, "unreceived"), []byte("new source\n"), 0600); err != nil {
					t.Fatal(err)
				}
				git(worker, "add", "unreceived")
				git(worker, "commit", "-m", "new unreceived source")
			case "wrong-branch":
				git(root, "merge", "--no-ff", cp.SourceHead, "-m", "original merge")
				git(root, "checkout", "-b", "wrong-target")
			}
			head, branch, count := git(root, "rev-parse", "HEAD"), git(root, "branch", "--show-current"), git(root, "rev-list", "--all", "--count")
			if recovered, err := RecoverUnrecordedMerge(context.Background(), root, cp); err == nil || recovered != "" {
				t.Fatalf("different original intent became a merge receipt: %s %v", recovered, err)
			}
			if git(root, "rev-parse", "HEAD") != head || git(root, "branch", "--show-current") != branch || git(root, "rev-list", "--all", "--count") != count {
				t.Fatal("read-only failed merge observation changed Git")
			}
			if _, err := os.Stat(worker); err != nil {
				t.Fatalf("unproven merge removed Worker: %v", err)
			}
		})
	}
}

type boundedReadyHistoryRunner struct {
	gitRunner
	t       *testing.T
	history string
	err     error
	calls   int
}

func (r *boundedReadyHistoryRunner) Run(ctx context.Context, root string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(args) > 1 && args[0] == "rev-list" {
		r.calls++
		want := []string{"rev-list", "--first-parent", "--parents", "--max-count=" + strconv.Itoa(readyMergeHistoryLimit+1), "tip", "^target"}
		if strings.Join(args, " ") != strings.Join(want, " ") {
			r.t.Fatalf("unbounded or different history query: %v", args)
		}
		return r.history, r.err
	}
	return r.gitRunner.Run(ctx, root, args...)
}

// Controlled output verifies the fixed query/limit and rejection behavior;
// actual Git ancestry and public consumers are exercised separately above.
func TestReadyMergeRecoveryHasOneBoundedHistoryRead(t *testing.T) {
	for _, scenario := range []string{"at-limit", "over-limit", "ambiguous", "git-error", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			fr := newFakeRunner()
			fr.addCommit("target")
			fr.addCommit("source", "target")
			fr.addCommit("tip", "source")
			fr.setBranch("feature", "source")
			fr.setBranch("develop", "tip")
			fr.checkout("root", "develop")
			runner := &boundedReadyHistoryRunner{gitRunner: fr, t: t}
			rows := make([]string, readyMergeHistoryLimit)
			for i := range rows {
				rows[i] = fmt.Sprintf("later-%d previous-%d", i, i+1)
			}
			rows[len(rows)-1] = "exact-merge target source"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "over-limit":
				rows = append([]string{"one-more tip"}, rows...)
			case "ambiguous":
				rows[0] = "another-merge target source"
			case "git-error":
				runner.err = errors.New("actual history unavailable")
			case "cancelled":
				cancel()
			}
			runner.history = strings.Join(rows, "\n")
			restore := withRunner(runner)
			defer restore()
			merge, err := RecoverUnrecordedMerge(ctx, "root", Checkpoint{State: StateReady, SourceBranch: "feature", SourceHead: "source", TargetBranch: "develop", TargetHead: "target"})
			if scenario == "at-limit" {
				if err != nil || merge != "exact-merge" {
					t.Fatalf("within original bound: %s %v", merge, err)
				}
			} else if err == nil || merge != "" {
				t.Fatalf("unproven bounded observation succeeded: %s %v", merge, err)
			}
			if scenario == "cancelled" {
				if runner.calls != 0 || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled observation continued: calls=%d err=%v", runner.calls, err)
				}
			} else if runner.calls != 1 {
				t.Fatalf("history read %d times", runner.calls)
			}
		})
	}
}
