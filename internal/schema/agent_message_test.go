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

// TestAgentMessageWarnTierLayersExtensionsOverBase proves the envelope's
// requirement layering (RC-11 C-6): a message missing only extension fields
// hard-validates against the schema, and WarnMissingExtensionFields names the
// missing extensions — while a missing base field still hard-rejects.
func TestAgentMessageWarnTierLayersExtensionsOverBase(t *testing.T) {
	completionReport := func() map[string]any {
		return map[string]any{
			"schema_version":            "1.0.0",
			"message_type":              "completion_report",
			"message_id":                "msg-warn-001",
			"correlation_id":            "corr-warn-001",
			"runtime_id":                "loop-REQ-001",
			"expected_runtime_revision": 1,
			"agent_id":                  "agent-qa-1",
			"agent_definition_ref":      "agents/qa.md",
			"task_id":                   "TASK-001",
			"bug_id":                    nil,
			"team_id":                   nil,
			"occurred_at":               "2026-08-28T00:00:00Z",
		}
	}
	validator := schema.NewEmbeddedValidator()

	// Extension fields dropped: hard validation passes, warn tier reports.
	baseOnly := completionReport()
	data, err := json.Marshal(baseOnly)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("agent-message.schema.json", data); err != nil {
		t.Fatalf("missing extension fields must not hard-reject (base fields intact): %v", err)
	}
	warnings := schema.WarnMissingExtensionFields(data)
	if len(warnings) == 0 {
		t.Fatal("WarnMissingExtensionFields must report the missing extension fields")
	}
	for _, field := range []string{"activation_id", "status", "summary", "requested_event"} {
		found := false
		for _, warning := range warnings {
			if warning == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("warnings must include missing extension %q, got %v", field, warnings)
		}
	}

	// Supplying every extension silences the warn tier.
	complete := completionReport()
	for field, value := range map[string]any{
		"activation_id":    "act-1",
		"status":           "completed",
		"summary":          "all checks pass",
		"changed_paths":    []any{},
		"reviewed_paths":   []any{},
		"checks":           []any{},
		"evidence_refs":    []any{},
		"finding_refs":     []any{},
		"remaining_risks":  []any{},
		"scope_deviations": []any{},
		"requested_event":  "completion_reported",
	} {
		complete[field] = value
	}
	data, err = json.Marshal(complete)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("agent-message.schema.json", data); err != nil {
		t.Fatalf("complete message must hard-validate: %v", err)
	}
	if warnings := schema.WarnMissingExtensionFields(data); len(warnings) != 0 {
		t.Errorf("complete message must produce no warnings, got %v", warnings)
	}

	// A missing base field still hard-rejects — the warn tier never masks it.
	missingBase := completionReport()
	delete(missingBase, "correlation_id")
	data, err = json.Marshal(missingBase)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("agent-message.schema.json", data); err == nil {
		t.Fatal("missing base field correlation_id must still hard-reject")
	}
}
