package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceBindPersistsCurrentBranchAndRejectsDrift(t *testing.T) {
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if out, err := runGit(t, fix.root, args...); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	bindFixtureWorkspace(t, fix)
	var out, stderr bytes.Buffer
	if code := runRuntimeWorkspace([]string{"bind", "--root", fix.root}, &out, &stderr); code != 0 {
		t.Fatalf("bind=%d %s", code, stderr.String())
	}
	var binding map[string]any
	if err := json.Unmarshal(out.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding["integration_branch"] != "test2" {
		t.Fatalf("%v", binding)
	}
	data, err := os.ReadFile(filepath.Join(fix.root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	json.Unmarshal(data, &state)
	if state["workspace"] == nil {
		t.Fatal("binding wasn't persisted")
	}
	if _, err := runGit(t, fix.root, "switch", "-c", "test3"); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runRuntimeWorkspace([]string{"bind", "--root", fix.root}, &out, &stderr); code == 0 {
		t.Fatal("silently rebound main")
	}
	branch, _ := runGit(t, fix.root, "branch", "--show-current")
	if string(bytes.TrimSpace(branch)) != "test3" {
		t.Fatal("switched main")
	}
}

// G18 §1.3: a workspace operation's identity carries the responsibility (the
// Runtime that owns the slot, the assignment slot, its owner and the
// execution generation) and the intent (the operation). The same operation
// retried keeps its identity, two different operations do not collide, and
// two different responsibilities do not collide either. No Runtime write
// count is encoded.
func TestWorkspaceEventIdentitySeparatesIntentAndResponsibility(t *testing.T) {
	eventID, key := workspaceEventIdentity("loop-REQ-1", "launch", "assign-1", "agent-1", 1, "session-1", "")
	againEvent, againKey := workspaceEventIdentity("loop-REQ-1", "launch", "assign-1", "agent-1", 1, "session-1", "")
	if eventID != againEvent || key != againKey {
		t.Fatal("the same workspace operation must keep its identity")
	}
	otherEvent, otherKey := workspaceEventIdentity("loop-REQ-1", "rework", "assign-1", "agent-1", 1, "session-1", "")
	if otherEvent == eventID || otherKey == key {
		t.Fatal("two different workspace operations must not collide")
	}
	slotEvent, slotKey := workspaceEventIdentity("loop-REQ-1", "launch", "assign-2", "agent-1", 1, "session-1", "")
	if slotEvent == eventID || slotKey == key {
		t.Fatal("two different assignment slots must not collide")
	}
	_, runtimeKey := workspaceEventIdentity("loop-REQ-2", "launch", "assign-1", "agent-1", 1, "session-1", "")
	if runtimeKey == key {
		t.Fatal("two different Runtime responsibilities must not share one idempotency key")
	}
	if !strings.HasPrefix(eventID, "evt-workspace-") {
		t.Fatalf("workspace event id must name its event domain: %s", eventID)
	}
	if !strings.HasPrefix(key, "runtime:workspace:loop-REQ-1:") {
		t.Fatalf("workspace idempotency key must name its responsibility: %s", key)
	}
}

func TestInitAndBindRejectWorkerWithoutCreatingRuntime(t *testing.T) {
	for _, command := range []string{"init", "bind"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			os.Mkdir(filepath.Join(root, ".claude"), 0700)
			os.WriteFile(filepath.Join(root, ".claude/loop-workspace.json"), []byte("{"), 0600)
			sub := filepath.Join(root, "sub")
			os.Mkdir(sub, 0700)
			var out, stderr bytes.Buffer
			code := 0
			if command == "init" {
				code = runInit([]string{"--root", sub}, &out, &stderr)
			} else {
				code = runRuntimeWorkspace([]string{"bind", "--root", sub}, &out, &stderr)
			}
			if code == 0 {
				t.Fatal("worker accepted as main")
			}
			if !bytes.Contains(stderr.Bytes(), []byte("second Runtime")) {
				t.Fatalf("wrong rejection: %s", stderr.String())
			}
			if _, err := os.Stat(filepath.Join(sub, ".claude")); !os.IsNotExist(err) {
				t.Fatal("command created secondary control directory")
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); !os.IsNotExist(err) {
				t.Fatal("command created secondary Runtime")
			}
		})
	}
}

func bindFixtureWorkspace(t *testing.T, fix *runtimeFixture) {
	t.Helper()
	branch, err := runGit(t, fix.root, "branch", "--show-current")
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := fix.state["bound_req"].(map[string]any)
	if bound == nil {
		t.Fatal("fixture has no bound REQ")
	}
	bound["workspace"] = map[string]any{"project_root": fix.root, "dev_branch": strings.TrimSpace(string(branch)), "release_upstream": "main"}
	fix.persist(t)
}
