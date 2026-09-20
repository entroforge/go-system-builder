package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestBoundHookRejectsWrongDirectoryBeforeRuntimeMutation(t *testing.T) {
	for _, kind := range []string{"main_in_worker", "main_writes_worker", "forged_runtime", "unknown_worker", "invalid_binding"} {
		t.Run(kind, func(t *testing.T) {
			fix := newRuntimeFixture(t)
			for _, args := range [][]string{{"init", "-b", "customer-development"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
				if _, err := runGit(t, fix.root, args...); err != nil {
					t.Fatal(err)
				}
			}
			policyData, err := os.ReadFile("../../docs/hook-policy.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(fix.root, "docs/hook-policy.json"), policyData, 0600); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			binding, err := workspace.New(ctx, fix.root)
			if err != nil {
				t.Fatal(err)
			}
			e := workspace.Execution{AssignmentID: "assignment", RuntimeID: workspace.RuntimeID(fix.state), BaselineGeneration: workspace.Generation(fix.state), Generation: 1, AgentID: "builder", Path: filepath.Join(fix.root, ".worktrees/worker"), Branch: "codex/worker", TargetBranch: binding.Branch, BaseCommit: binding.BoundHead, Status: "preparing", WritePaths: []string{"src/"}, Checks: []string{}, Inputs: map[string]string{}}
			binding.Executions[e.AssignmentID] = e
			if err = binding.Materialize(ctx, e); err != nil {
				t.Fatal(err)
			}
			e.Status = "ready"
			binding.Executions[e.AssignmentID] = e
			fix.state["workspace"] = workspace.Encode(binding)
			fix.persist(t)
			payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Write", "cwd": e.Path, "tool_input": map[string]any{"file_path": "src/change.go"}}
			root := e.Path
			switch kind {
			case "main_writes_worker":
				root = fix.root
				payload["cwd"] = fix.root
				payload["tool_input"] = map[string]any{"file_path": filepath.Join(e.Path, "src/change.go")}
			case "forged_runtime":
				payload["runtime_context"] = map[string]any{"runtime_id": "loop-forged", "project_root": e.Path}
			case "invalid_binding":
				root = fix.root
				payload["cwd"] = fix.root
				fix.state["workspace"] = "invalid"
				fix.persist(t)
			case "unknown_worker":
				payload["agent_id"] = "builder"
			}
			before, _ := os.ReadFile(fix.statePath)
			journalBefore, _ := os.ReadFile(fix.journalPath)
			data, _ := json.Marshal(payload)
			var out, stderr bytes.Buffer
			code := evaluate(root, "PreToolUse", bytes.NewReader(data), &out, &stderr, true)
			if code != 2 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, &out, &stderr)
			}
			if bytes.Contains(stderr.Bytes(), []byte("load policy")) {
				t.Fatalf("did not route to Main policy: %s", &stderr)
			}
			after, _ := os.ReadFile(fix.statePath)
			journalAfter, _ := os.ReadFile(fix.journalPath)
			if !bytes.Equal(before, after) || !bytes.Equal(journalBefore, journalAfter) {
				t.Fatal("denied boundary advanced Runtime before rejection")
			}
			if _, err := os.Stat(filepath.Join(e.Path, ".claude/loop-state.json")); !os.IsNotExist(err) {
				t.Fatal("Hook created a second Runtime")
			}
			branch, _ := runGit(t, fix.root, "branch", "--show-current")
			if string(bytes.TrimSpace(branch)) != "customer-development" {
				t.Fatal("Hook switched user's Main branch")
			}
		})
	}
}
