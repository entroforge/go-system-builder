package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Exercise the actual S4 writer, independent S5 receptions and S6 native
// preparation with the registered materials, without supplying digests.
func TestS4RegisteredMaterialsReachS6NativePreparationWithoutDigests(t *testing.T) {
	root, planning := s4HandoffFixture(t)
	repo := fixtures.RepoRoot(t)
	copyS6RecordContextTree(t, filepath.Join(repo, "agents/backend-builder.md"), filepath.Join(root, ".claude/agents/backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyS6RecordContextTree(t, filepath.Join(repo, "skills", skill.Name, "SKILL.md"), filepath.Join(root, ".claude/skills", skill.Name, "SKILL.md"))
	}
	stageJSON(t, root, planning.AnswerPath, completedTaskPlanningDraft(planning))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s4-to-s6", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, planning.AnswerPath)}, "tool_response": map[string]any{}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("S4 original save: %d %s %s", code, &out, &diagnostic)
	}
	snapshot := stageSnapshot(t, root)
	if snapshot.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatalf("S4 did not hand off to S5: %s %s", &out, &diagnostic)
	}
	seen := map[string]bool{}
	for _, raw := range snapshot.State["documents"].([]any) {
		doc := raw.(map[string]any)
		if doc["id"] == "TASK-039-01" || doc["id"] == "dispatch-plan:REQ-039" {
			if _, present := doc["sha256"]; present {
				t.Fatalf("S4 unexpectedly registered a digest: %v", doc)
			}
			seen[doc["id"].(string)] = true
		}
	}
	if !seen["TASK-039-01"] || !seen["dispatch-plan:REQ-039"] {
		t.Fatalf("S4 original registration omitted TASK or plan: %v", seen)
	}
	packets, err := runtime.S5Packets(root, snapshot.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("original independent S5 responsibilities: %d %v", len(packets), err)
	}
	for i, packet := range packets {
		actor := []string{"s5-spec-reviewer", "s5-task-reviewer"}[i]
		if code, _, diag := nativeReviewEvent(t, root, "SubagentStart", actor, ""); code != 0 {
			t.Fatalf("S5 reviewer start: %d %s", code, diag)
		}
		stageJSON(t, root, packet.AnswerPath, completedDraft(packet, "pass"))
		if code, _, diag := nativeReviewEvent(t, root, "SubagentStop", actor, filepath.Join(root, packet.ViewPath)); code != 0 {
			t.Fatalf("S5 original reception: %d %s", code, diag)
		}
	}
	if lifecycle := stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"]; lifecycle != "building" {
		t.Fatalf("original S5 pass did not reach S6: %v", lifecycle)
	}
	status := stageCLI(t, "s6", "status", "--root", root, "--capacity", "1", "--json")
	if !strings.Contains(status, "TASK-039-01") {
		t.Fatalf("S6 status omitted registered TASK: %s", status)
	}
	prepared := stageCLI(t, "s6", "prepare", "--root", root, "--task", "TASK-039-01", "--role", "backend-builder")
	if !strings.Contains(prepared, "native Agent") {
		t.Fatalf("S6 did not give Main the native dispatch: %s", prepared)
	}
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-TASK-039-01")
	if err != nil || !found || record.Task.ID != "TASK-039-01" || record.Task.Ref != "docs/dev/tasks/TASK-039-01.md" {
		t.Fatalf("original TASK responsibility not registered: %+v %v", record, err)
	}
	brief, _, _ := assignmentrecord.NativePaths(record)
	if _, err := os.Stat(filepath.Join(root, brief)); err != nil || !strings.Contains(prepared, brief) {
		t.Fatalf("S6 dispatch did not provide the real original brief %s: %v / %s", brief, err, prepared)
	}
}
