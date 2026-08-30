package schema_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestValidatorAcceptsLoopStateExample(t *testing.T) {
	validator := schema.NewEmbeddedValidator()

	err := validator.ValidateEmbedded(
		"loop-state.schema.json",
		"loop-state.example.json",
	)
	if err != nil {
		t.Fatalf("expected loop state example to validate: %v", err)
	}
}

func TestValidatorAcceptsPlanningEvidenceKinds(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{
		map[string]any{
			"id": "ev-design", "kind": "planning_design", "path": "evidence/ev-design.json",
			"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"status": "valid", "baseline_generation": 1, "review_round": nil,
			"produced_by": []any{"architect-1"}, "invalidated_by": nil,
			"invalidation_rule": nil, "invalidation_reason": nil,
			"responsibility_id": "Architect", "scope_refs": []any{},
		},
		map[string]any{
			"id": "ev-manifest-delivery", "kind": "team_manifest", "path": "evidence/ev-manifest-delivery.json",
			"sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"status": "valid", "baseline_generation": 1, "review_round": 1,
			"produced_by": []any{"delivery-1"}, "invalidated_by": nil,
			"invalidation_rule": nil, "invalidation_reason": nil,
			"responsibility_id": "Delivery Verifier", "scope_refs": []any{},
		},
	}
	updated, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", updated); err != nil {
		t.Fatalf("planning_design/team_manifest evidence kinds must validate: %v", err)
	}
}

func TestValidatorAcceptsAgentLifecycleEvidenceReferences(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id":                          "agent-1",
		"role":                        "backend-builder",
		"state":                       "reported",
		"task_ids":                    []any{"TASK-001"},
		"team_id":                     "team-1",
		"definition_ref":              "agents/backend-builder.md",
		"prompt_ref":                  "manifest#assignment-1",
		"readback_ref":                nil,
		"activation_ref":              "docs/evidence/activation-1.json",
		"activation_revision":         3,
		"work_started_ref":            "docs/reports/review/work-start-1.json",
		"completion_reported_ref":     "docs/reports/review/completion-1.json",
		"completion_acknowledged_ref": "docs/reports/review/completion-ack-1.json",
		"work_blocked_ref":            nil,
		"blocker_resolved_ref":        nil,
		"shutdown_approved_ref":       nil,
		"updated_at":                  "2026-07-20T00:00:00Z",
	}}
	updated, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", updated); err != nil {
		t.Fatalf("Agent lifecycle evidence references must be schema-valid: %v", err)
	}
}

func TestValidatorAcceptsRequirementScopedTaskIDs(t *testing.T) {
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["tasks"] = []any{map[string]any{
		"id":              "TASK-039-01",
		"state":           "reviewed",
		"path":            "docs/tasks/TASK-039-01-loop-definition.md",
		"sha256":          "1111111111111111111111111111111111111111111111111111111111111111",
		"owner_agent_ids": []any{"agent-039-01"},
	}}
	updated, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", updated); err != nil {
		t.Fatalf("requirement-scoped TASK IDs must be schema-valid: %v", err)
	}
}

func TestValidatorAcceptsCurrentHookPolicy(t *testing.T) {
	validator := schema.NewValidator(filepath.Join("..", ".."))
	if err := validator.ValidateFile("hook-policy.schema.json", "docs/hook-policy.json"); err != nil {
		t.Fatalf("current Hook policy must match embedded schema: %v", err)
	}
}

func TestValidatorRejectsInvalidHookDecision(t *testing.T) {
	validator := schema.NewEmbeddedValidator()

	// Fixture violates Hook v1 schema (BE-003 §8.1):
	//   - "decision":"block" requires retry="never" (this payload sets it to "")
	//   - "decision":"block" requires rule_id matching ^HOOK_[A-Z0-9_]+$ (this sets
	//     a legacy HS-NNN literal that no longer satisfies the regex).
	err := validator.ValidateBytes(
		"hook-decision.schema.json",
		[]byte(`{
			"schema_version":"1.1.0",
			"decision_id":"hook-decision-invalid",
			"policy_id":"hook-policy-loop-engineering",
			"policy_version":"v1.3.0",
			"policy_sha256":"1111111111111111111111111111111111111111111111111111111111111111",
			"hook_event":"PreToolUse",
			"session_id":"session-1",
			"runtime_id":"loop-REQ-002-example",
			"observed_runtime_revision":1,
			"agent_id":"agent-1",
			"target_id":"tool-1",
			"matched_rule_ids":["HS-002"],
			"decision":"block",
			"rule_id":"HS-002",
			"reason":"blocked",
			"missing":["missing thing"],
			"recovery":["do the thing"],
			"retry":"",
			"human_required":true,
			"evaluated_at":"2026-06-20T00:00:00Z"
		}`),
	)
	if err == nil {
		t.Fatal("expected invalid block decision to be rejected")
	}
}

// TestValidatorAcceptsCurrentHookPolicyVersion confirms the published policy
// ships at the locked v1.3.0 contract (TASK-025 §4.6).
func TestValidatorAcceptsCurrentHookPolicyVersion(t *testing.T) {
	validator := schema.NewValidator(filepath.Join("..", ".."))
	if err := validator.ValidateFile("hook-policy.schema.json", "docs/hook-policy.json"); err != nil {
		t.Fatalf("current Hook policy must match embedded schema: %v", err)
	}
}

// TestValidatorAcceptsQualityGateEnvelopeExamples confirms the BUG-039-03
// schema additions — `quality_gate` is an optional property on the
// DecisionEnvelope and the three layered examples (not_ready, advanced,
// blocked) all validate against the embedded hook-decision.schema.json.
func TestValidatorAcceptsQualityGateEnvelopeExamples(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	examples, err := schema.ReadAsset("hook-decision.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []any
	if err := json.Unmarshal(examples, &list); err != nil {
		t.Fatalf("hook-decision.examples.json must be a JSON array: %v", err)
	}
	cases := []string{"not_ready", "advanced", "blocked"}
	seen := map[string]bool{}
	for _, item := range list {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		qg, ok := record["quality_gate"].(map[string]any)
		if !ok {
			continue
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("re-encode example: %v", err)
		}
		if err := validator.ValidateBytes("hook-decision.schema.json", encoded); err != nil {
			t.Fatalf("quality_gate example %v must validate: %v", qg["status"], err)
		}
		if status, _ := qg["status"].(string); status != "" {
			seen[status] = true
		}
	}
	for _, want := range cases {
		if !seen[want] {
			t.Fatalf("hook-decision.examples.json must include a %q quality_gate example, got %v", want, seen)
		}
	}
}

// TestValidatorRejectsAdvancedWithoutTransitionCommitted locks the
// never-fabricate-advanced-status invariant at the schema level
// (BUG-039-03 §4.2 / SYNC-039 §4): an envelope claiming
// status="advanced" must also assert transition_committed=true.
func TestValidatorRejectsAdvancedWithoutTransitionCommitted(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	payload := []byte(`{
		"schema_version":"1.1.0",
		"decision_id":"hook-decision-fab-advanced",
		"policy_id":"hook-policy-loop-engineering",
		"policy_version":"v2.0.0",
		"policy_sha256":"1111111111111111111111111111111111111111111111111111111111111111",
		"hook_event":"PreToolUse",
		"session_id":"session-fab",
		"runtime_id":"loop-REQ-039-example",
		"observed_runtime_revision":7,
		"agent_id":"agent-1",
		"target_id":"tool-1",
		"matched_rule_ids":[],
		"decision":"allow",
		"rule_id":null,
		"reason":"No policy rule blocked or warned on this action.",
		"missing":[],
		"recovery":[],
		"retry":"not_applicable",
		"human_required":false,
		"evaluated_at":"2026-07-30T09:00:00Z",
		"quality_gate":{
			"status":"advanced",
			"gate_id":"GATE-X",
			"candidate_transition":"TR-X",
			"observed_revision":7,
			"fingerprint":"sha256:fake",
			"missing":[],
			"evidence_refs":[],
			"transition_committed":false,
			"next_cursor":"planning.contracts"
		}
	}`)
	if err := validator.ValidateBytes("hook-decision.schema.json", payload); err == nil {
		t.Fatal("expected validator to reject status=advanced when transition_committed=false (never_fabricate_advanced_status)")
	}
}
