package hookctx_test

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitAssignmentReferenceNeverFallsBackToAnotherOwner(t *testing.T) {
	for _, tc := range []struct {
		name, ref, owner, fragment         string
		duplicate, wrongTask, wrongRuntime bool
		want                               string
	}{
		{name: "addendum", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#addition", owner: "worker", want: "addition"},
		{name: "backslash reference", ref: `.claude\workgroups\REQ-X\TASK-X-addendum\manifest.json#addition`, owner: "worker", want: "addition"},
		{name: "unique owner without fragment", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json", owner: "worker", want: "addition"},
		{name: "missing file", ref: "missing.json#addition", owner: "worker"},
		{name: "wrong fragment", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#other", owner: "worker"},
		{name: "empty fragment", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#", owner: "worker"},
		{name: "different owner", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#addition", owner: "someone-else"},
		{name: "duplicate assignment", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#addition", owner: "worker", duplicate: true},
		{name: "wrong task subject", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#addition", owner: "worker", wrongTask: true},
		{name: "wrong runtime", ref: ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json#addition", owner: "worker", wrongRuntime: true},
		{name: "path traversal", ref: "../outside.json#addition", owner: "worker"},
		{name: "Windows absolute", ref: `C:\repo\manifest.json#addition`, owner: "worker"},
		{name: "no reference legacy owner", owner: "worker", want: "legacy"},
	} {
		for _, state := range []string{"working", "done"} {
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				root := t.TempDir()
				snapshot := map[string]any{"runtime_id": "loop-X", "revision": 1, "baseline": map[string]any{"generation": 1}, "entities": map[string]any{
					"agents": []any{map[string]any{"id": "worker", "state": state, "task_ids": []string{"TASK-X"}, "prompt_ref": tc.ref}},
					"tasks":  []any{map[string]any{"id": "TASK-X", "state": "in_progress", "owner_agent_ids": []string{"worker"}}}, "bugs": []any{}, "teams": []any{}}}
				b, _ := json.Marshal(snapshot)
				writeJSONL(t, filepath.Join(root, ".claude/loop-state.json"), string(b))
				writeJSONL(t, filepath.Join(root, ".claude/workgroups/REQ-X/TASK-X/manifest.json"), `{"assignments":[{"assignment_id":"legacy","agent_id":"worker","write_paths":["legacy/"]}]}`)
				row := map[string]any{"assignment_id": "addition", "agent_id": tc.owner, "write_paths": []string{"new/"}, "done_when": []string{"verify actual changes"}}
				rows := []any{row}
				if tc.duplicate {
					rows = append(rows, row)
				}
				task := "TASK-X"
				if tc.wrongTask {
					task = "TASK-Y"
				}
				runtimeID := "loop-X"
				if tc.wrongRuntime {
					runtimeID = "loop-other"
				}
				b, _ = json.Marshal(map[string]any{"runtime_id": runtimeID, "documents": []any{map[string]any{"id": task, "kind": "task"}}, "assignments": rows})
				writeJSONL(t, filepath.Join(root, ".claude/workgroups/REQ-X/TASK-X-addendum/manifest.json"), string(b))
				loaded, err := hookctx.LoadFull(root, "worker")
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" {
					if len(loaded.Assignments) != 0 || loaded.PolicyContext.AssignmentID != "" {
						t.Fatalf("invalid reference inherited another assignment: %+v / %s", loaded.Assignments, loaded.PolicyContext.AssignmentID)
					}
					return
				}
				if len(loaded.Assignments) != 1 || loaded.Assignments[0].AssignmentID != tc.want || loaded.PolicyContext.AssignmentID != tc.want {
					t.Fatalf("wrong assignment: %+v / %s", loaded.Assignments, loaded.PolicyContext.AssignmentID)
				}
				if tc.want == "addition" && loaded.Assignments[0].WritePaths[0] != "new/" {
					t.Fatal("lost explicit scope")
				}
			})
		}
	}
}

func TestExplicitManifestRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "manifest.json")
	writeJSONL(t, outside, `{"assignments":[{"assignment_id":"outside","agent_id":"worker"}]}`)
	if err := os.Symlink(outside, filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(root, ".claude/loop-state.json"), `{"runtime_id":"loop-X","revision":1,"entities":{"agents":[{"id":"worker","task_ids":["TASK-X"],"prompt_ref":"manifest.json#outside"}],"tasks":[{"id":"TASK-X","state":"in_progress","owner_agent_ids":["worker"]}]}}`)
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Assignments) != 0 {
		t.Fatal("accepted external manifest")
	}
}
