package integration

import (
	"context"
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
	git(root, "branch", "other")
	git(root, "worktree", "add", "-b", "feature", wt)
	if err := os.WriteFile(filepath.Join(wt, "change"), []byte("change"), 0600); err != nil {
		t.Fatal(err)
	}
	git(wt, "add", ".")
	git(wt, "commit", "-m", "change")
	return root, wt, Inspection{Ready: true, AssignmentID: "review", WorktreePath: wt, SourceBranch: "feature", TargetBranch: "test2", BaselineGeneration: 1, NonSquashMode: true}, IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "review", CheckpointDir: filepath.Join(t.TempDir(), "cp.json")}, git
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
