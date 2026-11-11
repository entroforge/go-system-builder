package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS6ThinPlanRequiresProfessionalAnswersAndRecordBinding(t *testing.T) {
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-s6-plan", "correlation_id": "corr-s6-plan",
		"runtime_id": "loop-REQ-001", "agent_id": "agent-s6-builder", "agent_definition_ref": "agents/backend-builder.md",
		"task_id": "TASK-001", "bug_id": nil, "team_id": "workgroup-s6", "occurred_at": "2026-10-14T00:00:00Z",
		"assignment_id": "assignment-s6", "assignment_revision": 2, "assignment_record_contract": "s6-v1.2",
		"objective": "Implement the reviewed TASK", "planned_paths": []string{"internal/api/handler.go"},
		"assertion_checks": []any{map[string]any{"assertion": "route behaves correctly", "oracle": "expected status and response"}},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	validator := schema.NewEmbeddedValidator()
	validate := func(value map[string]any) error {
		data, _ := json.Marshal(value)
		return validator.ValidateBytes("agent-message.schema.json", data)
	}
	if err := validate(message); err != nil {
		t.Fatal(err)
	}
	// Historical machine metadata is optional and never authenticates the Plan.
	// Keep its compatibility independent from the actual professional fields.
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "null", value: nil},
		{name: "nonstandard text", value: "legacy"},
		{name: "number", value: 17},
		{name: "array", value: []any{"legacy"}},
		{name: "object", value: map[string]any{"legacy": true}},
	} {
		t.Run("historical digest/"+tc.name, func(t *testing.T) {
			message["assignment_record_sha256"] = tc.value
			defer delete(message, "assignment_record_sha256")
			if err := validate(message); err != nil {
				t.Fatalf("historical machine metadata blocked the original Plan: %v", err)
			}
		})
	}
	data, _ := json.Marshal(message)
	if _, exists := message["assignment_record_sha256"]; exists {
		t.Fatal("new Plan unexpectedly contains historical digest")
	}
	if warnings := schema.WarnMissingExtensionFields(data); len(warnings) != 0 {
		t.Fatalf("thin plan must not request legacy steps: %v", warnings)
	}
	for _, field := range []string{"runtime_id", "agent_id", "assignment_id", "assignment_revision", "objective", "planned_paths", "assertion_checks", "dependencies", "risks_blockers"} {
		saved := message[field]
		delete(message, field)
		if err := validate(message); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
		message[field] = saved
	}
	message["steps"] = []any{map[string]any{"description": "legacy step"}}
	if err := validate(message); err != nil {
		t.Fatalf("unrelated professional field blocked original Plan: %v", err)
	}
}
