package cli_test

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/cli"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"os"
	"path/filepath"
	"testing"
)

func TestBatchScopeRepairCASAndJournal(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "building", "", 7)
	state["review"] = map[string]any{"round": 0, "clean_round": nil}
	docs := []any{}
	for _, pair := range [][2]string{{"TASK-1", "REQ-038"}, {"TASK-2", "REQ-039"}} {
		rel := "docs/dev/tasks/" + pair[0] + ".md"
		body := []byte("> Status: complete\n> Source REQ refs: " + pair[1] + "\n")
		os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0755)
		os.WriteFile(filepath.Join(root, rel), body, 0644)
		docs = append(docs, map[string]any{"id": pair[0], "kind": "task", "path": rel, "sha256": fixtures.Sha256Hex(body), "status": "complete", "version": "v1", "generation": 1})
	}
	state["documents"] = docs
	fixtures.WriteState(t, root, state)
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	var out, errout bytes.Buffer
	run := func(args ...string) int {
		out.Reset()
		errout.Reset()
		return cli.Run(append([]string{"runtime", "repair-batch-scope", "--root", root}, args...), nil, &out, &errout)
	}
	if code := run(); code != 0 {
		t.Fatal(errout.String())
	}
	plan := append([]byte(nil), out.Bytes()...)
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("preview mutated Runtime")
	}
	path := filepath.Join(root, "plan.json")
	os.WriteFile(path, plan, 0600)
	var tampered map[string]any
	json.Unmarshal(plan, &tampered)
	tampered["revision"] = 8
	b, _ := json.Marshal(tampered)
	bad := filepath.Join(root, "bad.json")
	os.WriteFile(bad, b, 0600)
	if run("--apply-plan", bad) == 0 {
		t.Fatal("changed plan accepted")
	}
	if run("--apply-plan", path) != 0 {
		t.Fatal(errout.String())
	}
	s := fixtures.ReadState(t, root)
	if s["lifecycle"].(map[string]any)["state"] != "document_verification" || len(s["documents"].([]any)) != 1 {
		t.Fatalf("state=%v", s)
	}
	journal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !bytes.Contains(journal, []byte("BATCH-SCOPE-REPAIR")) {
		t.Fatal("repair missing journal audit")
	}
	if _, err := os.Stat(filepath.Join(root, "docs/dev/tasks/TASK-1.md")); err != nil {
		t.Fatal("historical file removed")
	}
	if run("--apply-plan", path) == 0 {
		t.Fatal("plan replay accepted")
	}
}
