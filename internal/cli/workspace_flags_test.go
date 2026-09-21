package cli

import (
	"bytes"
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

	os.MkdirAll(filepath.Join(fix.root, ".claude/agents"), 0700)
	os.MkdirAll(filepath.Join(fix.root, ".claude/skills"), 0700)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Bootstrap(e, binary); err != nil {
		t.Fatal(err)
	}
	e.BootstrapSHA256, err = workspace.BootstrapDigest(e)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	fix.state["workspace"] = workspace.Encode(b)
	fix.persist(t)
	stateBefore, _ := os.ReadFile(fix.statePath)
	for _, view := range []string{"cwd", "status", "diff", "staged", "log"} {
		var out, stderr bytes.Buffer
		if code := Run([]string{"runtime", "workspace", "observe", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--view", view}, bytes.NewReader(nil), &out, &stderr); code != 0 {
			t.Fatalf("observe %s: %d %s", view, code, stderr.String())
		}
		if view == "cwd" && strings.TrimSpace(out.String()) != e.Path {
			t.Fatal("observation routed to Main")
		}
	}
	for _, extra := range [][]string{{"--agent", "wrong"}, {"--assignment", "wrong"}, {"--view", "log --output=bad"}, {"unexpected"}} {
		var out, stderr bytes.Buffer
		args := []string{"runtime", "workspace", "observe", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--view", "cwd"}
		if code := Run(append(args, extra...), bytes.NewReader(nil), &out, &stderr); code == 0 {
			t.Fatalf("invalid observation accepted: %v", extra)
		}
	}
	if got, _ := os.ReadFile(fix.statePath); !bytes.Equal(got, stateBefore) {
		t.Fatal("observation mutated Runtime")
	}

	// Capability failure must precede session reservation and Claude invocation.
	e.Checks = []string{"project-test"}
	b.Executions[e.AssignmentID] = e
	fix.state["workspace"] = workspace.Encode(b)
	fix.persist(t)
	large, err := os.Create(filepath.Join(e.Path, "oversize"))
	if err != nil {
		t.Fatal(err)
	}
	if err = large.Truncate(513 << 20); err != nil {
		t.Fatal(err)
	}
	large.Close()
	stateBefore, _ = os.ReadFile(fix.statePath)
	var launchOut, launchErr bytes.Buffer
	if code := Run([]string{"runtime", "workspace", "launch", "--root", fix.root, "--assignment", e.AssignmentID, "--agent", e.AgentID}, bytes.NewReader(nil), &launchOut, &launchErr); code == 0 || !strings.Contains(launchErr.String(), "512 MiB") {
		t.Fatalf("capability preflight: %d %s", code, launchErr.String())
	}
	if got, _ := os.ReadFile(fix.statePath); !bytes.Equal(got, stateBefore) {
		t.Fatal("capability failure reserved session")
	}
	if err := os.Remove(filepath.Join(e.Path, "oversize")); err != nil {
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
