package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// These tests cover JSON shape only. The lifecycle writer must still require
// the issued-target capability and verify each identity/source field against
// the original Runtime and journal; a valid message is not authority.
func TestS9BlockerLifecycleMessageSchemaAcceptsLegacyAndTypedShapes(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	for _, test := range []struct {
		name    string
		message map[string]any
	}{
		{name: "legacy blocker report", message: s9LifecycleMessage("blocker_report")},
		{name: "typed blocker report", message: s9ValidBlockerMessage()},
		{name: "legacy blocker resolution", message: s9LifecycleMessage("blocker_resolution")},
		{name: "typed resolution with repair assignment", message: s9ValidResolutionMessage("repair-assignment-s9-1")},
		{name: "typed resolution with workspace assignment", message: s9ValidResolutionMessage("assignment-s9-1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if body, ok := test.message["body"].(string); !ok || body == "" {
				t.Fatalf("lifecycle body must remain a string, got %#v", test.message["body"])
			}
			if err := s9ValidateLifecycleMessage(validator, test.message); err != nil {
				t.Fatalf("valid lifecycle message rejected: %v", err)
			}
		})
	}
}

func TestS9BlockerLifecycleMessageSchemaRejectsSubtypeMixing(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	tests := []struct {
		name    string
		message map[string]any
	}{
		{
			name:    "blocker payload on resolution type",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) { m["message_type"] = "blocker_resolution" }),
		},
		{
			name:    "resolution payload on blocker type",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) { m["message_type"] = "blocker_report" }),
		},
		{
			name: "both payloads",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) {
				m["s9_resolution"] = s9ValidResolutionMessage("repair-assignment-s9-1")["s9_resolution"]
			}),
		},
		{
			name:    "non-string body remains invalid",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) { m["body"] = map[string]any{"summary": "blocked"} }),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := s9ValidateLifecycleMessage(validator, test.message); err == nil {
				t.Fatal("invalid lifecycle subtype shape accepted")
			}
		})
	}
}

func TestS9BlockerLifecycleMessageSchemaRejectsIncompleteUnknownAndForgedFields(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	for _, field := range []string{"packet_id", "packet_sha256", "answer_path", "answer_sha256"} {
		t.Run("blocker missing "+field, func(t *testing.T) {
			message := s9ValidBlockerMessage()
			delete(message["s9_blocker"].(map[string]any), field)
			if err := s9ValidateLifecycleMessage(validator, message); err == nil {
				t.Fatalf("missing s9_blocker.%s accepted", field)
			}
		})
	}
	for _, field := range []string{"decision", "rationale", "evidence_refs", "packet_id", "packet_sha256", "blocker_ref", "blocker_sha256", "execution_assignment_id", "execution_generation", "execution_sha256"} {
		t.Run("resolution missing "+field, func(t *testing.T) {
			message := s9ValidResolutionMessage("repair-assignment-s9-1")
			delete(message["s9_resolution"].(map[string]any), field)
			if err := s9ValidateLifecycleMessage(validator, message); err == nil {
				t.Fatalf("missing s9_resolution.%s accepted", field)
			}
		})
	}
	for _, test := range []struct {
		name    string
		message map[string]any
	}{
		{
			name: "unknown blocker field",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) {
				m["s9_blocker"].(map[string]any)["runtime_id"] = "loop-fabricated"
			}),
		},
		{
			name: "resolution actor claim",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["main_agent_id"] = "agent-main-claimed"
			}),
		},
		{
			name: "resolution accepted coordinate claim",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["accepted_revision"] = 99
			}),
		},
		{
			name: "resolution extra runtime coordinate",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["runtime_id"] = "loop-fabricated"
			}),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := s9ValidateLifecycleMessage(validator, test.message); err == nil {
				t.Fatal("unknown or self-asserted field accepted")
			}
		})
	}
}

func TestS9BlockerLifecycleMessageSchemaRejectsBadDigestPathAndDecision(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	tests := []struct {
		name    string
		message map[string]any
	}{
		{
			name: "bad blocker packet digest",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) {
				m["s9_blocker"].(map[string]any)["packet_sha256"] = "ABC"
			}),
		},
		{
			name: "bad blocker answer digest",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) {
				m["s9_blocker"].(map[string]any)["answer_sha256"] = "short"
			}),
		},
		{
			name: "bad answer path",
			message: s9MutateLifecycleMessage(s9ValidBlockerMessage(), func(m map[string]any) {
				m["s9_blocker"].(map[string]any)["answer_path"] = "../../answer.json"
			}),
		},
		{
			name: "bad resolution packet digest",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["packet_sha256"] = "not-a-digest"
			}),
		},
		{
			name: "bad blocker ref path",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["blocker_ref"] = "evidence/arbitrary.json"
			}),
		},
		{
			name: "bad blocker digest",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["blocker_sha256"] = "short"
			}),
		},
		{
			name: "bad execution assignment id",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["execution_assignment_id"] = "assignment/forged"
			}),
		},
		{
			name: "bad execution generation",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["execution_generation"] = 0
			}),
		},
		{
			name: "bad execution digest",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["execution_sha256"] = "short"
			}),
		},
		{
			name: "decision cannot approve",
			message: s9MutateLifecycleMessage(s9ValidResolutionMessage("repair-assignment-s9-1"), func(m map[string]any) {
				m["s9_resolution"].(map[string]any)["decision"] = "approve"
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := s9ValidateLifecycleMessage(validator, test.message); err == nil {
				t.Fatal("invalid digest, path, machine tuple or decision accepted")
			}
		})
	}
}

func s9LifecycleMessage(messageType string) map[string]any {
	return map[string]any{
		"schema_version":       "1.0.0",
		"message_type":         messageType,
		"message_id":           "msg-s9-lifecycle-001",
		"correlation_id":       "corr-s9-lifecycle-001",
		"runtime_id":           "loop-REQ-001",
		"agent_id":             "agent-builder-1",
		"agent_definition_ref": "agents/backend-builder.md",
		"task_id":              "TASK-001",
		"bug_id":               nil,
		"team_id":              "team-s9",
		"occurred_at":          "2026-10-14T00:00:00Z",
		"body":                 "This message preserves the original lifecycle body string.",
	}
}

func s9ValidBlockerMessage() map[string]any {
	message := s9LifecycleMessage("blocker_report")
	message["s9_blocker"] = map[string]any{
		"packet_id":     "packet-" + strings.Repeat("a", 64),
		"packet_sha256": strings.Repeat("b", 64),
		"answer_path":   ".claude/submissions/loop-REQ-001/g1/S9-result/r1/repair-assignment-s9-1/packet-" + strings.Repeat("a", 64) + "/agent-builder-1/answer.json",
		"answer_sha256": strings.Repeat("c", 64),
	}
	return message
}

func s9ValidResolutionMessage(executionAssignmentID string) map[string]any {
	message := s9LifecycleMessage("blocker_resolution")
	message["s9_resolution"] = map[string]any{
		"decision":                "retry",
		"rationale":               "The blocker is understood and the same authorized attempt may resume.",
		"evidence_refs":           []string{"evidence://reviewed-source"},
		"packet_id":               "packet-" + strings.Repeat("a", 64),
		"packet_sha256":           strings.Repeat("b", 64),
		"blocker_ref":             ".claude/evidence/loop-REQ-001/g1/S9/blockers/packet-" + strings.Repeat("a", 64) + "/" + strings.Repeat("d", 64) + ".json",
		"blocker_sha256":          strings.Repeat("d", 64),
		"execution_assignment_id": executionAssignmentID,
		"execution_generation":    1,
		"execution_sha256":        strings.Repeat("e", 64),
	}
	return message
}

func s9MutateLifecycleMessage(message map[string]any, mutate func(map[string]any)) map[string]any {
	data, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		panic(err)
	}
	mutate(clone)
	return clone
}

func s9ValidateLifecycleMessage(validator *schema.Validator, message map[string]any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return validator.ValidateBytes("agent-message.schema.json", data)
}
