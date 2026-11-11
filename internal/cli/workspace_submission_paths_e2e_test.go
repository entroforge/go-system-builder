package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkerS5OccupiedSubmissionDirectoryPreservesFileAndReceivesAnswer(t *testing.T) {
	f := newStageAnswerFixtureMode(t, true, func(root string, _ map[string]any) {
		dir := filepath.Join(root, ".loop-submissions")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "unknown.txt"), []byte("user file"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	p := f.packets[0]
	e := f.prepare(t, p)
	binding, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := binding.WorkerSubmissionPaths(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(paths.Answer, ".loop-submissions-candidate-") {
		t.Fatalf("unknown directory was claimed: %s", paths.Answer)
	}
	data, err := os.ReadFile(filepath.Join(e.Path, ".loop-submissions/unknown.txt"))
	if err != nil || string(data) != "user file" {
		t.Fatalf("unknown file changed: %v", err)
	}
	r := saveStageHook(t, f.root, e.Path, p.Binding.Owner, paths.Answer, completedDraft(p, "pass"))
	if !r.WorkComplete {
		t.Fatalf("recorded candidate was not received: %+v", r)
	}
}

func TestWorkerS6OccupiedSubmissionDirectoryReachesResultConsumer(t *testing.T) {
	f := coreBuilderFixtureChecks(t, []string{}, func(root string, _ map[string]any) {
		dir := filepath.Join(root, ".loop-submissions")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "unknown.txt"), []byte("user file"), 0600); err != nil {
			t.Fatal(err)
		}
	})
	e := prepareCoreBuilder(t, f, "backend-core")
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := registry.WorkerSubmissionPaths(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(paths.Completion, ".loop-submissions-candidate-") {
		t.Fatalf("wrong Result path: %s", paths.Completion)
	}
	var plan map[string]any
	if err := json.Unmarshal(mustReadCoreFile(t, e.Path, paths.Plan), &plan); err != nil {
		t.Fatal(err)
	}
	plan["objective"] = "Implement the registered backend value and its owned regression assertions"
	plan["steps"] = []any{map[string]any{"description": "Update the scoped behavior", "target": "src/value.go"}}
	plan["assertion_checks"] = []any{map[string]any{"assertion": "Updated Value returns the agreed result", "oracle": "Owned behavior matches the registered task"}}
	plan["risks_blockers"] = []any{}
	stageJSON(t, e.Path, paths.Plan, plan)
	stageCLI(t, "runtime", "workspace", "begin", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	product := "src/owner_candidate.go"
	if err := os.WriteFile(filepath.Join(e.Path, product), []byte("package src\nconst Candidate = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", product}, {"commit", "-m", "Candidate result"}} {
		if _, err := workspace.Git(t.Context(), e.Path, args...); err != nil {
			t.Fatal(err)
		}
	}
	examples, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(examples, &messages); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	for _, m := range messages {
		if m["message_type"] == "completion_report" {
			result = m
		}
	}
	result["runtime_id"], result["agent_id"], result["agent_definition_ref"] = e.RuntimeID, e.AgentID, ".claude/agents/backend-builder.md"
	result["task_id"], result["team_id"], result["message_id"] = f.taskID, "workgroup-core-builder", "msg-completion-"+e.AgentID
	result["summary"] = "Delivered the assigned candidate"
	result["changed_paths"], result["checks"], result["evidence_refs"] = []string{product}, []any{}, []string{product}
	stageJSON(t, e.Path, paths.Completion, result)
	full := filepath.Join(e.Path, paths.Completion)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "agent_id": e.AgentID, "cwd": e.Path, "tool_name": "Write", "tool_input": map[string]any{"file_path": full}})
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("hook %d: %s", code, diag.String())
	}
	if !strings.Contains(out.String(), "S6 Completion questionnaire saved") {
		t.Fatalf("candidate was not received: %s %s", out.String(), diag.String())
	}
	data, err := os.ReadFile(filepath.Join(e.Path, ".loop-submissions/unknown.txt"))
	if err != nil || string(data) != "user file" {
		t.Fatalf("unknown file changed: %v", err)
	}
}
