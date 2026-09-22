package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS6DispatchCLIUsesReviewedGitInputs(t *testing.T) {
	root := s6Fixture(t)
	source := "../../docs/examples/dispatch-plan/project"
	if err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, p)
		out := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(out, b, 0644)
	}); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	git("init", "-b", "dev")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	git("add", "docs")
	git("commit", "-m", "plan")
	statePath := filepath.Join(root, ".claude/loop-state.json")
	b, _ := os.ReadFile(statePath)
	state := map[string]any{}
	json.Unmarshal(b, &state)
	state["bound_req"] = map[string]any{"id": "REQ-042", "workspace": map[string]any{"dev_branch": "dev"}}
	p, e := semantic.LoadDispatchPlan(root, fileview.Disk{Root: root}, "REQ-042")
	if e != nil {
		t.Fatal(e)
	}
	docs := []any{}
	add := func(id, kind, path string) {
		b, _ := os.ReadFile(filepath.Join(root, path))
		docs = append(docs, map[string]any{"id": id, "kind": kind, "path": path, "version": "1", "status": "complete", "sha256": fmt.Sprintf("%x", sha256.Sum256(b)), "generation": 1})
	}
	add("dispatch-plan:REQ-042", "dispatch_plan", p.Path)
	for _, task := range p.Tasks {
		add(task.ID, "task", task.Path)
	}
	state["documents"] = docs
	state["evidence"] = []any{}
	b, _ = json.Marshal(state)
	os.WriteFile(statePath, b, 0644)
	// The parent receives actual candidates in additionalContext, not terminal-only systemMessage.
	guidance := cli.BuildGuidanceForState(root, state, "SessionStart", policy.Input{})
	wire, exitCode, wireErr := hook.RenderWithAdditionalContext(root, "SessionStart", policy.Decision{Decision: "allow", Guidance: &guidance}, policy.RuntimeContext{}, "")
	if wireErr != nil || exitCode != 0 {
		t.Fatalf("wire: %d %v", exitCode, wireErr)
	}
	var packet struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if e := json.Unmarshal(wire, &packet); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"eligible tasks", "TASK-042-01", "TASK-042-02", "TASK-042-03"} {
		if !strings.Contains(packet.HookSpecificOutput.AdditionalContext, want) {
			t.Fatalf("missing Agent context %s: %s", want, wire)
		}
	}
	var stdout, stderr bytes.Buffer
	args := []string{"s6", "status", "--root", root, "--capacity", "3", "--json"}
	if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	var board struct {
		Next []string `json:"next"`
	}
	if e = json.Unmarshal(stdout.Bytes(), &board); e != nil || len(board.Next) != 3 {
		t.Fatalf("%s %v", stdout.String(), e)
	}
	before, _ := os.ReadFile(statePath)
	// A dirty copy must not alter the approved Git input or write runtime state.
	path := filepath.Join(root, p.Path)
	os.WriteFile(path, []byte("dirty uncommitted plan"), 0644)
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("dirty disk replaced committed plan: %s", stderr.String())
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("status mutated runtime")
	}
}
