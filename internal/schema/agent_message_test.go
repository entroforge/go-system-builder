package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestAgentMessageSchemaAcceptsWorkStartLifecycleEvent(t *testing.T) {
	message := map[string]any{
		"schema_version":            "1.0.0",
		"message_type":              "work_start",
		"message_id":                "msg-work-start-001",
		"correlation_id":            "corr-workgroup-001-assignment-001",
		"runtime_id":                "loop-REQ-001",
		"expected_runtime_revision": 20,
		"agent_id":                  "agent-builder-001",
		"agent_definition_ref":      "agents/backend-builder.md",
		"task_id":                   "TASK-001",
		"bug_id":                    nil,
		"team_id":                   "workgroup-001",
		"occurred_at":               "2026-07-20T02:40:00Z",
		"activation_id":             "act-agent-builder-001",
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", data); err != nil {
		t.Fatalf("work_start lifecycle message must be schema-valid: %v", err)
	}
}

func TestAgentMessageSchemaAcceptsRequirementScopedTaskID(t *testing.T) {
	message := map[string]any{
		"schema_version":            "1.0.0",
		"message_type":              "work_start",
		"message_id":                "msg-work-start-039-01",
		"correlation_id":            "corr-workgroup-039-assignment-01",
		"runtime_id":                "loop-REQ-039",
		"expected_runtime_revision": 7,
		"agent_id":                  "agent-039-01",
		"agent_definition_ref":      "agents/backend-builder.md",
		"task_id":                   "TASK-039-01",
		"bug_id":                    nil,
		"team_id":                   "workgroup-REQ-039-builder-task-01",
		"occurred_at":               "2026-07-29T05:30:00Z",
		"activation_id":             "act-agent-039-01",
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", data); err != nil {
		t.Fatalf("requirement-scoped TASK IDs must be valid in Agent messages: %v", err)
	}
}

func TestAgentMessageSchemaRejectsWorkStartWithoutActivationID(t *testing.T) {
	message := map[string]any{
		"schema_version":            "1.0.0",
		"message_type":              "work_start",
		"message_id":                "msg-work-start-002",
		"correlation_id":            "corr-workgroup-001-assignment-001",
		"runtime_id":                "loop-REQ-001",
		"expected_runtime_revision": 20,
		"agent_id":                  "agent-builder-001",
		"agent_definition_ref":      "agents/backend-builder.md",
		"task_id":                   "TASK-001",
		"bug_id":                    nil,
		"team_id":                   "workgroup-001",
		"occurred_at":               "2026-07-20T02:40:00Z",
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", data); err == nil {
		t.Fatal("work_start lifecycle message without activation_id must be rejected")
	}
}
