package req039_test

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/cli"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Exercise registration -> real worktree -> canonical completion -> integration
// -> successor eligibility. Real CLI adapters run from Worker CWD; platform spawning is not
// simulated as a successful operation.
func TestNativeWorkerAdapterChain(t *testing.T) {
	name := "loop-harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/loop-harness")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	for _, location := range []string{"main", "worker"} {
		t.Run(location, func(t *testing.T) {
			if location == "worker" {
				if runtime.GOOS != "linux" {
					t.Skip("Worker isolation requires native Linux acceptance")
				}
				if _, err := exec.LookPath("bwrap"); err != nil {
					t.Skip("Bubblewrap not installed")
				}
			}
			nativeWorkerAdapterChain(t, binary, location)
		})
	}
}
func nativeWorkerAdapterChain(t *testing.T, harnessBinary, location string) {
	root := s4AuditRegistrationRoot(t)
	runGitIn(t, root, "config", "user.name", "Fixture")
	runGitIn(t, root, "config", "user.email", "fixture@example.com")
	run := func(args ...string) string {
		t.Helper()
		var out, errout bytes.Buffer
		commandRoot, binary := root, harnessBinary
		if len(args) > 2 && args[0] == "runtime" && args[1] == "workspace" {
			switch args[2] {
			case "observe", "begin", "commit", "report", "check":
				id := ""
				for i := 0; i < len(args)-1; i++ {
					if args[i] == "--assignment" {
						id = args[i+1]
					}
				}
				b, _, err := workspace.Load(root)
				if err != nil {
					t.Fatal(err)
				}
				commandRoot = b.Executions[id].Path
				binary = filepath.Join(commandRoot, ".claude/bin/loop-harness")
			}
		}
		args = append(args, "--root", commandRoot)
		cmd := exec.Command(binary, args...)
		cmd.Dir = commandRoot
		cmd.Stdout = &out
		cmd.Stderr = &errout
		if err := cmd.Run(); err != nil {
			t.Fatalf("native %v: %v %s %s", args, err, out.String(), errout.String())
		}
		return out.String()
	}

	for _, dir := range []string{"agents", ".claude/agents", ".claude/skills"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	role, err := os.ReadFile(filepath.Join(repoRoot(t), "agents/backend-builder.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"agents/backend-builder.md", ".claude/agents/backend-builder.md"} {
		if err := os.WriteFile(filepath.Join(root, rel), role, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGitIn(t, root, "branch", "-M", "test2")
	writeSystemState(t, root, readSystemState(t, root))
	run("runtime", "workspace", "bind")
	assertSuccessor := func(want bool) {
		t.Helper()
		err := dispatch.CheckRegistration(root, readSystemState(t, root), "TASK-042-04")
		if (err == nil) != want {
			t.Fatalf("successor ready=%v: %v", want, err)
		}
	}
	assertSuccessor(false)
	for _, item := range []struct{ suffix, task, scope string }{
		{"shared", "TASK-042-01", "packages/validation"},
		{"page", "TASK-042-02", "web/pages"},
	} {
		task, aid, agent := item.task, "assignment-audit-"+item.suffix, "builder-audit-"+item.suffix
		output := item.scope + "/generated/result.json"
		manifest := s4AuditManifest(t, root, item.suffix, task, item.scope+"/source", output)
		canonicalManifest := filepath.Join(root, ".claude/workgroups/REQ-039", task, "manifest.json")
		if err := os.MkdirAll(filepath.Dir(canonicalManifest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(manifest, canonicalManifest); err != nil {
			t.Fatal(err)
		}
		manifest = canonicalManifest
		manifestBytes, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var manifestBody map[string]any
		if err := json.Unmarshal(manifestBytes, &manifestBody); err != nil {
			t.Fatal(err)
		}
		manifestBody["assignments"].([]any)[0].(map[string]any)["required_checks"] = []string{"git rev-parse --verify HEAD"}
		manifestBytes, err = json.Marshal(manifestBody)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, manifestBytes, 0644); err != nil {
			t.Fatal(err)
		}
		if code, out, errout := s4AuditRegister(t, root, manifest, task); code != 0 {
			t.Fatalf("register: %d %s %s", code, out, errout)
		}
		run("runtime", "workspace", "prepare", "--assignment", aid, "--agent", agent, "--check-location", location)
		loaded, err := hookctx.LoadFull(root, "")
		if err != nil {
			t.Fatal(err)
		}
		worker := ""
		for _, a := range loaded.Assignments {
			if a.AssignmentID == aid {
				worker = a.WorktreePath
			}
		}
		if worker == "" {
			t.Fatal("missing worktree")
		}
		for _, view := range []string{"cwd", "status", "diff", "staged", "log"} {
			run("runtime", "workspace", "observe", "--assignment", aid, "--agent", agent, "--view", view)
		}
		bindingForHooks, _, err := workspace.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		execution := bindingForHooks.Executions[aid]
		hookProbe := func(tool string, input map[string]any, want int) {
			t.Helper()
			payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "agent_id": agent, "cwd": worker, "tool_name": tool, "tool_input": input})
			cmd := exec.Command(harnessBinary, "hook", "--event", "PreToolUse", "--root", worker)
			cmd.Dir = worker
			cmd.Stdin = bytes.NewReader(payload)
			got, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				if ex, ok := err.(*exec.ExitError); ok {
					code = ex.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != want {
				t.Fatalf("Hook %s %v exit=%d want=%d: %s", tool, input, code, want, got)
			}
		}
		hookProbe("Write", map[string]any{"file_path": filepath.Join(worker, ".claude/submissions/plan.json")}, 0)
		hookProbe("Bash", map[string]any{"command": "pwd"}, 0)
		hookProbe("Bash", map[string]any{"command": workspace.ObservationCommands(execution)["status"]}, 0)
		hookProbe("Bash", map[string]any{"command": workspace.WorkerInvocation(execution, "begin")}, 0)
		hookProbe("Bash", map[string]any{"command": "git status --short"}, 2)
		hookProbe("Write", map[string]any{"file_path": filepath.Join(root, output)}, 2)
		writeRecheckPlan(t, filepath.Join(worker, ".claude/submissions/plan.json"), recheckWorkerSpec{suffix: item.suffix, agentID: agent, taskID: task, assignment: aid, output: output})
		run("runtime", "workspace", "begin", "--assignment", aid, "--agent", agent)
		target := filepath.Join(worker, output)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}

		request := map[string]any{"expected_head": strings.TrimSpace(runGitIn(t, worker, "rev-parse", "HEAD")), "message": "native adapter candidate", "paths": []string{output}}
		requestData, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(worker, ".claude/submissions"), 0700)
		if err := os.WriteFile(filepath.Join(worker, ".claude/submissions/commit.json"), requestData, 0600); err != nil {
			t.Fatal(err)
		}
		run("runtime", "workspace", "commit", "--assignment", aid, "--agent", agent)
		if location == "worker" {
			run("runtime", "workspace", "check", "--assignment", aid, "--agent", agent, "--check", "0")
		}
		var data []byte
		message := filepath.Join(root, ".claude/messages/"+agent+".json")
		writeAuditCompletionMessage(t, message, agent, task)
		data, err = os.ReadFile(message)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		body["changed_paths"] = []string{output}
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(message, data, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(worker, ".claude/submissions/completion.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		run("runtime", "workspace", "report", "--assignment", aid, "--agent", agent)
		assertSuccessor(false) // A report alone never releases its consumers.
		code, out, errout := runTaskIntegrate(t, root, aid)
		if code != 0 || !strings.Contains(errout, "integrated") {
			t.Fatalf("integrate: %d %s %s", code, out, errout)
		}
		if _, err := os.Stat(filepath.Join(root, output)); err != nil {
			t.Fatal(err)
		}
		binding, _, err := workspace.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if status := binding.Executions[aid].Status; status != "complete" {
			t.Errorf("cleaned execution remains %q after integration", status)
		}
		if _, err := os.Stat(worker); !os.IsNotExist(err) {
			t.Fatalf("worker not cleaned: %v", err)
		}
	}
	assertSuccessor(true) // TASK-042-03 is unrelated and has not been dispatched.
	// A new canonical Result after cleanup must revoke readiness until rechecked,
	// then recover without remerging or recreating the cleaned worktree.
	head := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	run("runtime", "task-complete", "--agent-id", "builder-audit-page", "--message", filepath.Join(root, ".claude/messages/builder-audit-page.json"))
	assertSuccessor(false)
	if pending := run("runtime", "workspace", "pending"); !strings.Contains(pending, "assignment-audit-page") {
		t.Fatal("resubmitted report disappeared from pending")
	}
	if code, out, errout := runTaskIntegrate(t, root, "assignment-audit-page"); code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("recheck resubmitted Result: %d %s %s", code, out, errout)
	}
	if current := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); current != head {
		t.Fatal("resubmission repeated the merge")
	}
	assertSuccessor(true)

	binding, state, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the legacy cleanup/CAS crash window: the checkpoint is complete
	// and the Worker is gone, but the registry still says ready.
	stale := binding.Executions["assignment-audit-shared"]
	stale.Status = "ready"
	binding.Executions[stale.AssignmentID] = stale
	state["workspace"] = workspace.Encode(binding)
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), stateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if pending := run("runtime", "workspace", "pending"); !strings.Contains(pending, "assignment-audit-shared") {
		t.Fatal("completion projection disappeared from pending")
	}
	moved := filepath.Join(t.TempDir(), "moved")
	paths := map[string]string{}
	for _, e := range binding.Executions {
		paths[e.Path] = filepath.Join(moved, ".worktrees", filepath.Base(e.Path))
	}
	req := workspace.RebindRequest{RuntimeID: workspace.RuntimeID(state), OldMainRoot: root, OldCommonDir: binding.CommonDir, NewMainRoot: moved, NewCommonDir: filepath.Join(moved, ".git"), Branch: binding.Branch, ExpectedHead: strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")), Paths: paths}
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}

	requestPath := filepath.Join(t.TempDir(), "rebind.json")
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		var out, errout bytes.Buffer
		if code := cli.Run([]string{"runtime", "workspace", "rebind", "--root", moved, "--request", requestPath, "--reason", "completed chain relocation"}, nil, &out, &errout); code != 0 {
			t.Fatalf("rebind %d: %s %s", code, &out, &errout)
		}
	}
	root = moved
	relocated, _, err := workspace.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range relocated.Executions {
		if e.Status != "complete" {
			t.Fatalf("legacy completion not recovered: %s=%s", e.AssignmentID, e.Status)
		}
	}
	if code, out, errout := runTaskIntegrate(t, root, "assignment-audit-page"); code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("repeat relocated integration: %d %s %s", code, out, errout)
	}
	run("runtime", "task-complete", "--agent-id", "builder-audit-page", "--message", filepath.Join(root, ".claude/messages/builder-audit-page.json"))
	assertSuccessor(false)
	if pending := run("runtime", "workspace", "pending"); !strings.Contains(pending, "assignment-audit-page") {
		t.Fatal("resubmitted report disappeared from pending")
	}
	if code, out, errout := runTaskIntegrate(t, root, "assignment-audit-page"); code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("refresh relocated integration: %d %s %s", code, out, errout)
	}
	assertSuccessor(true)
	if current := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); current != head {
		t.Fatal("relocation repeated merge")
	}

	manifest := s4AuditManifest(t, root, "successor", "TASK-042-04", "web/features", "web/features/result.json")
	if code, out, errout := s4AuditRegister(t, root, manifest, "TASK-042-04"); code != 0 {
		t.Fatalf("successor register: %d %s %s", code, out, errout)
	}
}
