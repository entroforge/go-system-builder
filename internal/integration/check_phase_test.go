package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostMergeChecksUseCandidateOnTargetExactlyOnce(t *testing.T) {
	ctx := context.Background()
	root, wt := t.TempDir(), filepath.Join(t.TempDir(), "candidate")
	old := defaultRunner
	defaultRunner = execRunner{}
	defer func() { defaultRunner = old }()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := defaultRunner.Run(ctx, dir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	git(root, "init", "-b", "develop")
	git(root, "config", "user.name", "Test")
	git(root, "config", "user.email", "test@example.invalid")
	os.WriteFile(filepath.Join(root, "base"), []byte("base"), 0600)
	git(root, "add", "base")
	git(root, "commit", "-m", "base")
	git(root, "worktree", "add", "-b", "feature", wt)
	os.WriteFile(filepath.Join(wt, "new-spec"), []byte("candidate"), 0600)
	git(wt, "add", "new-spec")
	git(wt, "commit", "-m", "candidate")
	a := assignmentContext(wt, "feature", "develop")
	a.IntegrationCheckMode = "post_merge"
	a.RequiredChecks = []string{"test -f new-spec"}
	cfg, err := AssignmentInspectConfig(a)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SkipCompletionCheck = true
	calls := 0
	runner := func(ctx context.Context, dir, command string) error {
		calls++
		if dir != root {
			t.Fatalf("wrong target %s", dir)
		}
		return CommandCheckRunner(ctx, dir, command)
	}
	cfg.CheckRunner = runner
	inspection, err := Inspect(ctx, InspectRequest{Root: root, Assignment: a, TargetBranch: "develop", BaselineGeneration: 1}, cfg)
	if err != nil || !inspection.Ready || calls != 0 {
		t.Fatalf("static inspect: %+v %v calls=%d", inspection, err, calls)
	}
	result, err := Integrate(ctx, IntegrateRequest{Inspection: inspection}, IntegrateConfig{Root: root, RuntimeID: "test", CheckpointDir: filepath.Join(t.TempDir(), "cp.json"), CheckRunner: runner, RequiredChecks: a.RequiredChecks})
	if err != nil || result.Checkpoint.State != StateVerified || calls != 1 {
		t.Fatalf("delivery: %+v %v calls=%d", result, err, calls)
	}
	a.IntegrationCheckMode = ""
	legacy, err := AssignmentInspectConfig(a)
	if err != nil || len(legacy.RequiredChecks) != 1 {
		t.Fatal("legacy manifest lost its checks")
	}
	a.IntegrationCheckMode = "typo"
	if _, err := AssignmentInspectConfig(a); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestVerifiedRejectsTargetDriftEvenWhenCommandReturnsZero(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()
	cfg := IntegrateConfig{Root: f.root, GitRoot: f.root, RuntimeID: "test", CheckpointDir: f.checkpointPath("assignment-test"), RequiredChecks: []string{"check"}, CheckRunner: func(context.Context, string, string) error {
		f.fr.addCommit("foreign", "feature-commit")
		f.fr.setBranch("develop", "foreign")
		return nil
	}}
	result, err := Integrate(context.Background(), IntegrateRequest{Inspection: f.readyInspection()}, cfg)
	if err == nil || result.Checkpoint.State == StateVerified {
		t.Fatalf("drift accepted: %+v %v", result, err)
	}
}
