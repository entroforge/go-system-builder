package assignment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestPlanCheckpointActivationUsesOriginalMessageWithoutDigest(t *testing.T) {
	root := t.TempDir()
	plan := map[string]any{"message_id": "msg-actual-plan", "correlation_id": "corr-actual", "objective": "actual source"}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plan.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := BuildPlanCheckpointActivationMessage(root, "loop-test", "agent-test", ".claude/agents/backend.md", "TASK-001", "team-test", -1, path, ActivationSourceEntry{AgentID: "agent-test", WritePaths: []string{"src/"}}, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if actual["approved_readback_message_id"] != "msg-actual-plan" {
		t.Fatalf("original identity lost: %#v", actual)
	}
	if _, exists := actual["approved_readback_sha256"]; exists {
		t.Fatal("new activation registered digest")
	}
	for _, row := range actual["documents"].([]any) {
		if _, exists := row.(map[string]any)["sha256"]; exists {
			t.Fatal("example digest was copied")
		}
	}
	encoded, _ := json.Marshal(actual)
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", encoded); err != nil {
		t.Fatalf("schema rejected actual activation: %v", err)
	}
}
