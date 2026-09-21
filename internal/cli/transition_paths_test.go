package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// Exercise the public CLI and the real invalidation action against disposable
// state. Other TR-007 actions/guards are omitted to isolate argument transport.
func TestTransitionAffectedPathsCLI(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"missing", nil, false},
		{"scoped", []string{"--affected-paths", "docs/dev/tasks/TASK-313.md"}, true},
		{"repeat", []string{"--affected-paths", "docs/dev/tasks/TASK-313.md", "--affected-paths", "docs/dev/tasks/TASK-313.md"}, true},
		{"all", []string{"--affected-paths", "all"}, true},
		{"escape", []string{"--affected-paths", "../outside"}, false},
		{"absolute", []string{"--affected-paths", "/tmp/outside"}, false},
		{"empty", []string{"--affected-paths", ""}, false},
		{"glob", []string{"--affected-paths", "docs/*"}, false},
		{"implicit-all", []string{"--affected-paths", "./all"}, false},
		{"mixed-all", []string{"--affected-paths", "all", "--affected-paths", "docs/dev/tasks/TASK-313.md"}, false},
		{"positional", []string{"--affected-paths", "docs/dev/tasks/TASK-313.md", "docs/dev/tasks/TASK-314.md"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(p string, b []byte) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, b, 0644); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile("../../docs/control/loop-definition.json")
			if err != nil {
				t.Fatal(err)
			}
			var def map[string]any
			json.Unmarshal(raw, &def)
			for _, v := range def["transitions"].([]any) {
				tr := v.(map[string]any)
				if tr["id"] == "TR-007" {
					tr["guards"] = []any{}
					tr["required_evidence"] = []any{}
					tr["actions"] = []any{"invalidate_affected_evidence"}
				}
			}
			raw, _ = json.Marshal(def)
			write(filepath.Join(root, "docs/control/loop-definition.json"), raw)
			raw, err = schema.ReadAsset("loop-state.example.json")
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			json.Unmarshal(raw, &state)
			state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
			state["revision"] = 1
			state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
			// Minimal evidence rows conform to the Runtime schema; scope is exact.
			ev := func(id, p string) map[string]any {
				return map[string]any{"id": id, "kind": "delivery_review", "path": "evidence/" + id + ".json", "sha256": strings.Repeat("a", 64), "status": "valid", "baseline_generation": 1, "review_round": 1, "produced_by": []any{"agent-1"}, "invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": "Builder", "scope_refs": []any{p}}
			}
			state["evidence"] = []any{ev("ev-affected", "docs/dev/tasks/TASK-313.md"), ev("ev-unrelated", "docs/dev/tasks/TASK-303.md")}
			for _, args := range [][]string{{"init", "-b", "dev"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "base"}} {
				if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("%s %v", out, err)
				}
			}
			head, _ := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
			state["bound_req"].(map[string]any)["workspace"] = map[string]any{"project_root": root, "dev_branch": "dev", "release_upstream": "main", "bound_commit": strings.TrimSpace(string(head))}
			before, _ := json.Marshal(state)
			sp := filepath.Join(root, ".claude/loop-state.json")
			jp := filepath.Join(root, ".claude/loop-events.jsonl")
			write(sp, before)
			write(jp, nil)
			args := []string{"runtime", "transition", "--root", root, "--state", sp, "--journal", jp, "--id", "TR-007", "--actor", "orchestrator", "--expected-revision", "1"}
			args = append(args, tc.args...)
			var out, errout bytes.Buffer
			code := cli.Run(args, strings.NewReader(""), &out, &errout)
			after, _ := os.ReadFile(sp)
			journal, _ := os.ReadFile(jp)
			if !tc.ok {
				if tc.name == "missing" && !strings.Contains(errout.String(), "affected_paths required") && !strings.Contains(errout.String(), "requires the affected paths") {
					t.Fatalf("unexpected missing-path failure: %s", errout.String())
				}
				if code == 0 {
					t.Fatal("unexpected success")
				}
				if !bytes.Equal(before, after) || len(journal) != 0 {
					t.Fatal("failed transition mutated state or journal")
				}
				return
			}
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, errout.String())
			}
			var got map[string]any
			json.Unmarshal(after, &got)
			rows := got["evidence"].([]any)
			wantUnrelated := "valid"
			if tc.name == "all" {
				wantUnrelated = "invalid"
			}
			if rows[0].(map[string]any)["status"] != "invalid" || rows[1].(map[string]any)["status"] != wantUnrelated {
				t.Fatalf("wrong evidence status: %v", rows)
			}
			if len(journal) == 0 {
				t.Fatal("missing journal commit")
			}
		})
	}
}
