package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRecoveryGitRepo(t *testing.T) (string, string, Inspection, IntegrateConfig, func(string, ...string) string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	wt := filepath.Join(t.TempDir(), "wt")
	old := defaultRunner
	defaultRunner = execRunner{}
	t.Cleanup(func() { defaultRunner = old })
	git := func(dir string, args ...string) string {
		t.Helper()
		o, e := defaultRunner.Run(ctx, dir, args...)
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(o)
	}
	git(root, "init", "-b", "test2")
	git(root, "config", "user.name", "Review")
	git(root, "config", "user.email", "review@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "base"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	git(root, "add", ".")
	git(root, "commit", "-m", "base")
	base := git(root, "rev-parse", "HEAD")
	git(root, "branch", "other")
	git(root, "worktree", "add", "-b", "feature", wt)
	if err := os.WriteFile(filepath.Join(wt, "change"), []byte("change"), 0600); err != nil {
		t.Fatal(err)
	}
	git(wt, "add", ".")
	git(wt, "commit", "-m", "change")
	source := git(wt, "rev-parse", "HEAD")
	return root, wt, Inspection{Ready: true, AssignmentID: "review", WorktreePath: wt, SourceBranch: "feature", SourceHead: source, TargetBranch: "test2", TargetHead: base, MergeBase: base, BaselineGeneration: 1, NonSquashMode: true}, IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "review", CheckpointDir: filepath.Join(t.TempDir(), "cp.json")}, git
}

func TestRetryChecksRequireRecordedTargetBranch(t *testing.T) {
	for _, scenario := range []string{"other_commit", "same_commit", "detached", "valid_target"} {
		t.Run(scenario, func(t *testing.T) {
			root, _, in, cfg, git := newRecoveryGitRepo(t)
			cfg.RequiredChecks = []string{"test -f change"}
			cfg.CheckRunner = func(context.Context, string, string) error { return errors.New("first check failed") }
			initial, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
			if !errors.Is(err, ErrCheckFailed) {
				t.Fatal(err)
			}
			in.TargetHead = initial.Checkpoint.MergeCommit
			switch scenario {
			case "other_commit":
				git(root, "checkout", "other")
			case "same_commit":
				git(root, "checkout", "-b", "same-tree")
			case "detached":
				git(root, "checkout", "--detach")
			}
			before := git(root, "rev-parse", "--abbrev-ref", "HEAD")
			calls := 0
			cfg.CheckRunner = func(ctx context.Context, root, cmd string) error { calls++; return CommandCheckRunner(ctx, root, cmd) }
			result, err := Integrate(context.Background(), IntegrateRequest{Inspection: in, RetryPreserved: true}, cfg)
			if scenario == "valid_target" {
				if err != nil || result.Checkpoint.State != StateVerified || calls != 1 {
					t.Fatalf("valid retry: %+v %v calls=%d", result, err, calls)
				}
			} else if !errors.Is(err, ErrCheckFailed) || result.Checkpoint.State != StatePreserved || calls != 0 {
				t.Fatalf("wrong target tested: %+v %v calls=%d", result, err, calls)
			}
			if got := git(root, "rev-parse", "--abbrev-ref", "HEAD"); got != before {
				t.Fatalf("branch changed: %s -> %s", before, got)
			}
		})
	}
}

func TestChecksRejectBranchSwitchEvenWithSameHead(t *testing.T) {
	root, _, in, cfg, git := newRecoveryGitRepo(t)
	cfg.RequiredChecks = []string{"check"}
	cfg.CheckRunner = func(context.Context, string, string) error { git(root, "checkout", "-b", "same-tree"); return nil }
	result, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
	if !errors.Is(err, ErrCheckFailed) || result.Checkpoint.State != StatePreserved {
		t.Fatalf("branch switch accepted: %+v %v", result, err)
	}
}

func TestLockedHintsStayMetadataAcrossCheckModes(t *testing.T) {
	for _, mode := range []string{"", "legacy_pre_and_post_merge", "post_merge"} {
		t.Run("mode="+mode, func(t *testing.T) {
			root, wt, _, cfg, _ := newRecoveryGitRepo(t)
			a := assignmentContext(wt, "feature", "test2")
			a.RequiredChecks = []string{"locked:base", "test -f base"}
			a.IntegrationCheckMode = mode
			inspectCfg, err := AssignmentInspectConfig(a)
			if err != nil {
				t.Fatal(err)
			}
			inspectCfg.SkipCompletionCheck = true
			inspectCfg.CheckRunner = CommandCheckRunner
			in, err := Inspect(context.Background(), InspectRequest{Root: root, Assignment: a, TargetBranch: "test2", BaselineGeneration: 1}, inspectCfg)
			if err != nil || !in.Ready {
				t.Fatalf("inspection: %+v %v", in, err)
			}
			cfg.RequiredChecks = a.RequiredChecks
			cfg.CheckRunner = CommandCheckRunner
			result, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
			if err != nil || result.Checkpoint.State != StateVerified {
				t.Fatalf("integration: %+v %v", result, err)
			}
		})
	}
}

func TestLockedHintStillRejectsChangedArtifact(t *testing.T) {
	for _, mode := range []string{"", "post_merge"} {
		t.Run("mode="+mode, func(t *testing.T) {
			root, wt, _, _, _ := newRecoveryGitRepo(t)
			a := assignmentContext(wt, "feature", "test2")
			a.RequiredChecks = []string{"locked:change"}
			a.IntegrationCheckMode = mode
			cfg, err := AssignmentInspectConfig(a)
			if err != nil {
				t.Fatal(err)
			}
			cfg.SkipCompletionCheck = true
			cfg.CheckRunner = func(context.Context, string, string) error { t.Fatal("locked metadata executed"); return nil }
			in, err := Inspect(context.Background(), InspectRequest{Root: root, Assignment: a, TargetBranch: "test2", BaselineGeneration: 1}, cfg)
			if err != nil || in.Ready || len(in.LockedDiff) != 1 {
				t.Fatalf("locked change accepted: %+v %v", in, err)
			}
		})
	}
}

func TestTargetAdvanceBeforeAcknowledgmentRechecksCurrentHead(t *testing.T) {
	root, _, in, cfg, git := newRecoveryGitRepo(t)
	checks := 0
	cfg.RequiredChecks = []string{"declared-check"}
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	first, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, cfg)
	if err != nil || first.Checkpoint.State != StateVerified {
		t.Fatalf("initial verify: %+v %v", first, err)
	}
	git(root, "commit", "--allow-empty", "-m", "target advances before ack")
	head := git(root, "rev-parse", "HEAD")
	second, err := Integrate(context.Background(), IntegrateRequest{Inspection: InspectionFromCheckpoint(first.Checkpoint), Acknowledge: true}, cfg)
	if err != nil || second.Checkpoint.State != StateAcknowledged || second.Checkpoint.TestedHead != head || checks != 2 {
		t.Fatalf("stale verified head reused: checks=%d result=%+v err=%v", checks, second, err)
	}
	if second.Checkpoint.MergeCommit != first.Checkpoint.MergeCommit {
		t.Fatal("reverification merged twice")
	}
}
