package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS6ResultMessageRejectsMachineClaimsAndUnansweredProfessionalFields(t *testing.T) {
	message := map[string]any{"schema_version": "1.0.0", "message_type": "completion_report", "message_id": "msg-s6-result", "correlation_id": "corr-s6-result", "runtime_id": "loop-REQ-001", "baseline_generation": 1, "agent_id": "agent-s6-builder", "agent_definition_ref": "agents/backend-builder.md", "task_id": "TASK-001", "bug_id": nil, "team_id": "workgroup-s6", "occurred_at": "2026-10-14T00:00:00Z", "assignment_id": "assignment-s6", "assignment_revision": 1, "assignment_record_contract": "s6-v1.2", "assignment_record_sha256": strings.Repeat("a", 64), "summary": "Implemented the assigned behavior", "assertion_evidence": []any{map[string]any{"assertion": "value persists", "oracle": "readback matches", "evidence_refs": []string{"src/proof.txt"}}}, "risks_blockers": []string{}, "scope_deviations": []string{}}
	validator := schema.NewEmbeddedValidator()
	validate := func() error {
		body, _ := json.Marshal(message)
		return validator.ValidateBytes("agent-message.schema.json", body)
	}
	if err := validate(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(message)
	if warnings := schema.WarnMissingExtensionFields(body); len(warnings) != 0 {
		t.Fatalf("canonical Result requested legacy fields: %v", warnings)
	}
	for _, field := range []string{"summary", "assertion_evidence", "risks_blockers", "scope_deviations"} {
		old := message[field]
		message[field] = nil
		if err := validate(); err == nil {
			t.Fatalf("unanswered %s accepted", field)
		}
		message[field] = old
	}
	for _, field := range []string{"checks", "changed_paths", "reviewed_paths", "status"} {
		message[field] = []any{}
		if err := validate(); err == nil {
			t.Fatalf("Worker machine claim %s accepted", field)
		}
		delete(message, field)
	}
}
