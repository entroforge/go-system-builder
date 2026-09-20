package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceBindPersistsCurrentBranchAndRejectsDrift(t *testing.T) {
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if out, err := runGit(t, fix.root, args...); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
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
