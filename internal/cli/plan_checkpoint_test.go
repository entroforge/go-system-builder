package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestValidatePlanReportCheckpointAcceptsManifestAssignmentOutsideS7(t *testing.T) {
	root := t.TempDir()
	manifest := decodePlanCheckpointAsset(t, "team-manifest.example.json")
	manifest["manifest_id"] = "team-manifest-s9"
	manifest["runtime_id"] = "loop-REQ-123"
	manifest["req_id"] = "REQ-123"
	manifest["workgroup_id"] = "workgroup-s9"
	manifest["workgroup_kind"] = "builder"
	manifest["planned_agent_count"] = 1
	manifest["max_parallel_agents"] = 1
	manifest["separation_edges"] = []any{}
	assignments := manifest["assignments"].([]any)
	assignment := assignments[0].(map[string]any)
	assignment["assignment_id"] = "assignment-s9-unit"
	assignment["role_family"] = "backend-builder"
	assignment["agent_id"] = "agent-s9"
	manifest["assignments"] = []any{assignment}
	writePlanCheckpointJSON(t, root, "manifest.json", manifest)

	plan := decodePlanCheckpointMessage(t)
	plan["runtime_id"] = "loop-REQ-123"
	plan["agent_id"] = "agent-s9"
	plan["task_id"] = "TASK-123"
	plan["team_id"] = "workgroup-s9"
	plan["assignment_id"] = "assignment-s9-unit"
	plan["assignment_revision"] = 1
	plan["expected_runtime_revision"] = 7
	planPath := writePlanCheckpointJSON(t, root, "plan.json", plan)

	state := map[string]any{
		"runtime_id": "loop-REQ-123",
		"review":     map[string]any{},
		"entities": map[string]any{
			"teams": []any{map[string]any{
				"id": "workgroup-s9", "manifest_ref": "manifest.json", "agent_ids": []any{"agent-s9"},
			}},
			"agents": []any{map[string]any{
				"id": "agent-s9", "team_id": "workgroup-s9", "task_ids": []any{"TASK-123"},
			}},
		},
	}

	if err := validatePlanReportCheckpoint(root, runtime.Snapshot{State: state}, "agent-s9", planPath); err != nil {
		t.Fatalf("manifest-bound S9 PLAN_REPORT must be accepted: %v", err)
	}
}

func TestValidatePlanReportCheckpointRejectsManifestOutsideRepository(t *testing.T) {
	root := t.TempDir()
	outsideRoot := t.TempDir()
	outsideManifest := filepath.Join(outsideRoot, "manifest.json")
	manifestBytes, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatalf("read manifest asset: %v", err)
	}
	if err := os.WriteFile(outsideManifest, manifestBytes, 0o644); err != nil {
		t.Fatalf("write outside manifest: %v", err)
	}

	plan := decodePlanCheckpointMessage(t)
	plan["runtime_id"] = "loop-REQ-123"
	plan["agent_id"] = "agent-s9"
	plan["task_id"] = "TASK-123"
	plan["team_id"] = "workgroup-s9"
	plan["assignment_id"] = "assignment-s9-unit"
	plan["assignment_revision"] = 1
	planPath := writePlanCheckpointJSON(t, root, "plan.json", plan)

	state := map[string]any{
		"runtime_id": "loop-REQ-123",
		"review":     map[string]any{},
		"entities": map[string]any{
			"teams": []any{map[string]any{
				"id": "workgroup-s9", "manifest_ref": outsideManifest, "agent_ids": []any{"agent-s9"},
			}},
			"agents": []any{map[string]any{
				"id": "agent-s9", "team_id": "workgroup-s9", "task_ids": []any{"TASK-123"},
			}},
		},
	}

	err = validatePlanReportCheckpoint(root, runtime.Snapshot{State: state}, "agent-s9", planPath)
	if err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("manifest path escape must be rejected explicitly, got %v", err)
	}
}

func decodePlanCheckpointAsset(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset(name)
	if err != nil {
		t.Fatalf("read embedded asset %s: %v", name, err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode embedded asset %s: %v", name, err)
	}
	return value
}

func decodePlanCheckpointMessage(t *testing.T) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatalf("read embedded agent messages: %v", err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("decode embedded agent messages: %v", err)
	}
	for _, message := range messages {
		if message["message_type"] == "plan_report" {
			return message
		}
	}
	t.Fatal("embedded agent messages have no plan_report")
	return nil
}

func writePlanCheckpointJSON(t *testing.T, root, name string, value map[string]any) string {
	t.Helper()
	path := filepath.Join(root, name)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
