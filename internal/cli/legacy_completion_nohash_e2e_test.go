package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestLegacyTaskCompleteCLIUsesOriginalResponsibilityWithoutDigest(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "building", "", 7)
	state["documents"] = []any{map[string]any{"id": "TASK-001", "kind": "task", "path": "docs/dev/tasks/TASK-001.md", "version": "v1", "sha256": "old-invalid-value", "status": "complete", "generation": 1}}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{"id": "builder-1", "role": "backend-builder", "state": "working", "task_ids": []any{"TASK-001"}, "team_id": "workgroup-build-1", "definition_ref": ".claude/agents/backend-builder.md", "prompt_ref": "manifest#assignment-builder-1", "readback_ref": nil, "activation_ref": nil, "activation_revision": nil, "updated_at": "2026-08-20T00:00:00Z"}},
		"tasks":  []any{map[string]any{"id": "TASK-001", "state": "in_progress", "path": "docs/dev/tasks/TASK-001.md", "owner_agent_ids": []any{"builder-1"}}},
		"bugs":   []any{}, "teams": []any{},
	}
	fixtures.WriteState(t, root, state)
	examples, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(examples, &messages); err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	for _, row := range messages {
		if row["message_type"] == "completion_report" {
			message = row
			break
		}
	}
	if message == nil {
		t.Fatal("missing original completion example")
	}
	message["agent_id"], message["task_id"], message["runtime_id"], message["status"] = "builder-1", "TASK-001", state["runtime_id"], "completed"
	message["checks"] = []any{map[string]any{"name": "unit", "command": "go test ./...", "result": "pass", "evidence_ref": nil}}
	message["scope_deviations"] = []any{}
	original, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	messagePath := filepath.Join(root, "completion.json")
	if err := os.WriteFile(messagePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-complete", "--root", root, "--agent-id", "builder-1", "--message", messagePath}, nil, &out, &stderr); code != 0 {
		t.Fatalf("task-complete exit %d: %s", code, stderr.String())
	}
	current := fixtures.ReadState(t, root)
	rows := current["evidence"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want original result evidence, got %v", rows)
	}
	entry := rows[0].(map[string]any)
	if _, found := entry["sha256"]; found {
		t.Fatalf("new evidence registered a digest: %v", entry)
	}
	envelope, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry["path"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(envelope, &value); err != nil {
		t.Fatal(err)
	}
	refs := value["subject_refs"].([]any)
	if len(refs) != 1 || refs[0].(map[string]any)["path"] != "docs/dev/tasks/TASK-001.md" {
		t.Fatalf("lost original TASK: %v", refs)
	}
	if _, found := refs[0].(map[string]any)["sha256"]; found {
		t.Fatalf("copied old TASK digest: %v", refs)
	}
	after, err := os.ReadFile(messagePath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("original answer changed: %v", err)
	}
}
