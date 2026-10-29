package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS6ClosingContractSchemaShapesAndLegacyCompatibility(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	plan := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report", "message_id": "msg-s6-close-plan",
		"correlation_id": "corr-s6-close-plan", "runtime_id": "loop-REQ-001", "agent_id": "agent-s6-builder",
		"agent_definition_ref": "agents/backend-builder.md", "task_id": "TASK-001", "bug_id": nil,
		"team_id": "workgroup-s6", "occurred_at": "2026-10-14T00:00:00Z", "assignment_id": "assignment-s6",
		"assignment_revision": 1, "assignment_record_contract": "s6-v1.2", "assignment_record_sha256": strings.Repeat("a", 64),
		"objective": "Implement the reviewed TASK", "planned_paths": []string{"internal/api/handler.go"},
		"assertion_checks": []any{map[string]any{"assertion": "route behaves correctly", "oracle": "expected status and response", "done_when_indexes": []int{0}}},
		"dependencies":     []string{}, "risks_blockers": []string{},
	}
	validatePlan := func(value map[string]any) error {
		data, _ := json.Marshal(value)
		return validator.ValidateBytes("agent-message.schema.json", data)
	}
	if err := validatePlan(plan); err != nil {
		t.Fatalf("valid closing-contract PLAN shape rejected: %v", err)
	}
	legacy := cloneJSONMap(t, plan)
	delete(legacy["assertion_checks"].([]any)[0].(map[string]any), "done_when_indexes")
	if err := validatePlan(legacy); err != nil {
		t.Fatalf("legacy S6 PLAN without closing mapping changed: %v", err)
	}
	for _, indexes := range [][]int{{-1}, {0, 0}} {
		candidate := cloneJSONMap(t, plan)
		candidate["assertion_checks"].([]any)[0].(map[string]any)["done_when_indexes"] = indexes
		if err := validatePlan(candidate); err == nil {
			t.Fatalf("invalid done_when_indexes %v accepted by schema", indexes)
		}
	}

	result := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "message_id": "msg-s6-close-result",
		"correlation_id": "corr-s6-close-result", "runtime_id": "loop-REQ-001", "baseline_generation": 1,
		"agent_id": "agent-s6-builder", "agent_definition_ref": "agents/backend-builder.md", "task_id": "TASK-001",
		"bug_id": nil, "team_id": "workgroup-s6", "occurred_at": "2026-10-14T00:00:00Z",
		"assignment_id": "assignment-s6", "assignment_revision": 1, "assignment_record_contract": "s6-v1.2",
		"assignment_record_sha256": strings.Repeat("a", 64), "summary": "Implemented the assigned behavior",
		"assertion_evidence": []any{map[string]any{"assertion": "route behaves correctly", "oracle": "expected status and response", "evidence_refs": []string{"src/proof.txt"}}},
		"risks_blockers":     []string{}, "scope_deviations": []string{},
		"closing_assessment": map[string]any{
			"disposition": "complete", "conditions": []any{map[string]any{
				"done_when_index": 0, "judgment": "satisfied", "plan_assertion_indexes": []int{0}, "rationale": "Evidence sources cover the assigned condition.",
			}},
		},
	}
	validateResult := func(value map[string]any) error {
		data, _ := json.Marshal(value)
		return validator.ValidateBytes("agent-message.schema.json", data)
	}
	if err := validateResult(result); err != nil {
		t.Fatalf("valid closing assessment shape rejected: %v", err)
	}
	legacyResult := cloneJSONMap(t, result)
	delete(legacyResult, "closing_assessment")
	if err := validateResult(legacyResult); err != nil {
		t.Fatalf("legacy S6 Result without closing assessment changed: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "unknown disposition", mutate: func(value map[string]any) { value["closing_assessment"].(map[string]any)["disposition"] = "passed" }},
		{name: "duplicate assertion refs", mutate: func(value map[string]any) {
			value["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["plan_assertion_indexes"] = []int{0, 0}
		}},
		{name: "empty rationale", mutate: func(value map[string]any) {
			value["closing_assessment"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["rationale"] = ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneJSONMap(t, result)
			test.mutate(candidate)
			if err := validateResult(candidate); err == nil {
				t.Fatal("invalid closing assessment shape accepted by schema")
			}
		})
	}

	draft := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_report", "runtime_id": "loop-REQ-001",
		"baseline_generation": 1, "assignment_id": "assignment-s6", "assignment_revision": 1,
		"assignment_record_contract": "s6-v1.2", "assignment_record_sha256": strings.Repeat("a", 64),
		"agent_id": "agent-s6-builder", "agent_definition_ref": "agents/backend-builder.md", "task_id": "TASK-001",
		"team_id": "workgroup-s6", "summary": nil, "assertion_evidence": nil, "risks_blockers": nil,
		"scope_deviations": nil, "closing_assessment": nil,
	}
	draftBytes, _ := json.Marshal(draft)
	if err := validator.ValidateBytes("stage-answer-s6-result.schema.json", draftBytes); err != nil {
		t.Fatalf("stage answer schema rejects null closing-assessment scaffold: %v", err)
	}
}

func cloneJSONMap(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
