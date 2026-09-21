package cli

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkspaceFlagRoutingUsesParsedArgumentsAndPreservesArtifactRoot(t *testing.T) {
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-b", "feature/customer"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if _, err := runGit(t, fix.root, args...); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	bindFixtureWorkspace(t, fix)
	b, err := workspace.New(ctx, fix.root)
	if err != nil {
		t.Fatal(err)
	}
	e := workspace.Execution{AssignmentID: "a", RuntimeID: workspace.RuntimeID(fix.state), BaselineGeneration: workspace.Generation(fix.state), Generation: 1, AgentID: "builder", Path: filepath.Join(fix.root, ".worktrees/a"), Branch: "codex/a", TargetBranch: b.Branch, BaseCommit: b.BoundHead, Status: "preparing", Inputs: map[string]string{}}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	fix.state["workspace"] = workspace.Encode(b)
	fix.persist(t)
	if err := os.WriteFile(filepath.Join(e.Path, "--root"), []byte(`{"body":"Worker report"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var durableSubmission string
	before, _ := os.Getwd()
	for _, name := range []string{"projection", "runtime agent-event", "runtime task-integrate"} {
		fs := flag.NewFlagSet(name, flag.ContinueOnError)
		root := fs.String("root", ".", "")
		message := fs.String("message", "", "")
		fs.String("agent-id", "builder", "")
		fs.String("event", "completion_reported", "")
		state := fs.String("state", ".claude/loop-state.json", "")
		err := parseWorkspaceFlags(fs, []string{"--message", "--root", "--root", e.Path})
		if name == "runtime task-integrate" {
			if err == nil {
				t.Fatal("Worker received integration authority")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if *root != fix.root || *state != fix.statePath {
			t.Fatalf("wrong control coordinates: %s %s", *root, *state)
		}
		if name == "runtime agent-event" {
			if !strings.HasPrefix(*message, filepath.Join(fix.root, ".claude/evidence")+string(filepath.Separator)) {
				t.Fatalf("artifact not preserved in Main: %s", *message)
			}
			durableSubmission = *message
		}
	}
	after, _ := os.Getwd()
	if before != after {
		t.Fatal("routing changed Main cwd")
	}
	if _, err := os.Stat(filepath.Join(e.Path, ".claude/loop-state.json")); !os.IsNotExist(err) {
		t.Fatal("duplicated Runtime")
	}
	if err := os.Remove(filepath.Join(e.Path, "--root")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(durableSubmission)
	if err != nil || string(data) != `{"body":"Worker report"}` {
		t.Fatalf("lost submission after Worker cleanup: %s %v", data, err)
	}
}
