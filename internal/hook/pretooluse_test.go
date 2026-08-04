package hook_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// decodeHookOutput unmarshals a PreToolUse payload and returns both the
// top-level envelope and the typed quality_gate block. Tests use it to
// assert against the official Claude Code Hook shape without depending on
// map[string]any ordering.
func decodeHookOutput(t *testing.T, data []byte) (map[string]any, map[string]any, map[string]any) {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, string(data))
	}
	specific, ok := envelope["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("hookSpecificOutput missing or wrong type, got %#v", envelope["hookSpecificOutput"])
	}
	qg, ok := specific["quality_gate"].(map[string]any)
	if !ok {
		t.Fatalf("quality_gate missing or wrong type, got %#v", specific["quality_gate"])
	}
	return envelope, specific, qg
}

// TestPreToolUseNotReadyAllowsTool proves the layered PreToolUse path:
// quality_gate.status="not_ready" yields permissionDecision="allow" with a
// Recovery Packet listing the missing items. CT-039-02 from SYNC-039.
func TestPreToolUseNotReadyAllowsTool(t *testing.T) {
	result := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusNotReady,
			GateID:              "GATE-PLANNING-CONTRACTS-COMPLETE",
			CandidateTransition: "PTR-PLAN-02",
			ObservedRevision:    12,
			Fingerprint:         "sha256:notreadyfp",
			Missing:             []string{"contract traceability"},
			EvidenceRefs:        []string{},
			TransitionCommitted: false,
			NextCursor:          "planning.contracts",
		},
		Decision: policy.Decision{Decision: "allow", Reason: "no policy rule blocked this action"},
	}
	output, exitCode, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("not_ready must exit 0, got %d", exitCode)
	}
	_, specific, qg := decodeHookOutput(t, output)
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("not_ready MUST allow the tool, got %v", specific["permissionDecision"])
	}
	if qg["status"] != "not_ready" {
		t.Fatalf("expected quality_gate.status=\"not_ready\", got %v", qg["status"])
	}
	if qg["gate_id"] != "GATE-PLANNING-CONTRACTS-COMPLETE" {
		t.Fatalf("expected gate_id, got %v", qg["gate_id"])
	}
	if qg["next_cursor"] != "planning.contracts" {
		t.Fatalf("expected next_cursor, got %v", qg["next_cursor"])
	}
	missing, _ := qg["missing"].([]any)
	if len(missing) != 1 || missing[0] != "contract traceability" {
		t.Fatalf("expected missing=[contract traceability], got %v", qg["missing"])
	}
	if v, _ := qg["transition_committed"].(bool); v {
		t.Fatalf("not_ready must NOT set transition_committed=true")
	}
	body, _ := envelope2SystemMessage(t, output)
	if !strings.Contains(body, "contract traceability") {
		t.Fatalf("systemMessage must list missing items, got %q", body)
	}
}

// TestPreToolUseAdvancedCommitsTransition — when the controller reports
// TransitionCommitted=true the layered output must surface
// permissionDecision="allow", transition_committed=true, and the post-commit
// observed_revision. CT-039-01 from SYNC-039.
func TestPreToolUseAdvancedCommitsTransition(t *testing.T) {
	result := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusAdvanced,
			GateID:              "GATE-PLANNING-DESIGN-COMPLETE",
			CandidateTransition: "PTR-PLAN-01",
			ObservedRevision:    13, // post-commit N+1
			Fingerprint:         "sha256:advfp",
			Missing:             []string{},
			EvidenceRefs:        []string{"evidence-1"},
			TransitionCommitted: true,
			NextCursor:          "planning.contracts",
		},
		Decision: policy.Decision{Decision: "allow", Reason: "transition committed"},
	}
	output, _, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, specific, qg := decodeHookOutput(t, output)
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("advanced must allow the tool, got %v", specific["permissionDecision"])
	}
	if qg["status"] != "advanced" {
		t.Fatalf("expected status=\"advanced\", got %v", qg["status"])
	}
	if v, _ := qg["transition_committed"].(bool); !v {
		t.Fatalf("advanced must report transition_committed=true")
	}
	if int(qg["observed_revision"].(float64)) != 13 {
		t.Fatalf("expected observed_revision=13 (N+1), got %v", qg["observed_revision"])
	}
	if qg["next_cursor"] != "planning.contracts" {
		t.Fatalf("expected next_cursor=planning.contracts, got %v", qg["next_cursor"])
	}
}

// TestPreToolUseSafetyBlockDeniesTool — locked-artifact / squash-merge
// safety block must surface as permissionDecision="deny" with
// quality_gate.status="blocked". CT-039-04 / CT-039-06 from SYNC-039.
func TestPreToolUseSafetyBlockDeniesTool(t *testing.T) {
	result := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusBlocked,
			GateID:              "GATE-FINAL-SAFETY",
			CandidateTransition: "",
			ObservedRevision:    5,
			Fingerprint:         "sha256:blockedfp",
			Missing:             []string{},
			EvidenceRefs:        []string{},
			TransitionCommitted: false,
			NextCursor:          "verification.delivery",
		},
		Decision: policy.Decision{
			Decision:      "block",
			RuleID:        policy.RuleLockedArtifactWrite,
			Reason:        "final safety block",
			Recovery:      []string{"use a new generation or amendment"},
			Retry:         "never",
			HumanRequired: true,
		},
	}
	output, _, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, specific, qg := decodeHookOutput(t, output)
	if specific["permissionDecision"] != "deny" {
		t.Fatalf("safety block MUST deny, got %v", specific["permissionDecision"])
	}
	if qg["status"] != "blocked" {
		t.Fatalf("expected quality_gate.status=\"blocked\", got %v", qg["status"])
	}
	if v, _ := qg["transition_committed"].(bool); v {
		t.Fatalf("blocked must NOT claim a committed transition")
	}
}

// TestPreToolUseUnknownAllowsTool — gate/selector conflict surfaces as
// status="unknown" but the tool still proceeds (per REQ-039 §10.2 — quality
// conflicts MUST NOT block the tool). CT-039-16 from SYNC-039.
func TestPreToolUseUnknownAllowsTool(t *testing.T) {
	result := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusUnknown,
			GateID:              "GATE-DOCUMENT-PASS",
			CandidateTransition: "TR-003",
			ObservedRevision:    8,
			Fingerprint:         "sha256:unknownfp",
			Missing:             []string{},
			EvidenceRefs:        []string{},
			Conflicts:           []string{"PTR-PLAN-01", "PTR-PLAN-02"},
			ErrorCode:           "LOOP_TRIGGER_CONFLICT",
			TransitionCommitted: false,
			NextCursor:          "planning.contracts",
		},
		Decision: policy.Decision{Decision: "allow", Reason: "no policy rule blocked this action"},
	}
	output, _, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, specific, qg := decodeHookOutput(t, output)
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("unknown MUST allow, got %v", specific["permissionDecision"])
	}
	if qg["status"] != "unknown" {
		t.Fatalf("expected status=\"unknown\", got %v", qg["status"])
	}
}

// TestPreToolUseSatisfiedAllowsTool — gate is satisfied but no transition
// has been committed yet (e.g. cursor has no auto-trigger candidate). The
// tool proceeds and the next cursor is preserved.
func TestPreToolUseSatisfiedAllowsTool(t *testing.T) {
	result := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusSatisfied,
			GateID:              "GATE-PLANNING-TASKS-COMPLETE",
			CandidateTransition: "TR-002",
			ObservedRevision:    20,
			Fingerprint:         "sha256:satisfied",
			Missing:             []string{},
			EvidenceRefs:        []string{"ev-task-1"},
			TransitionCommitted: false,
			NextCursor:          "document_verification",
		},
		Decision: policy.Decision{Decision: "allow", Reason: "no policy rule blocked this action"},
	}
	output, _, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, specific, qg := decodeHookOutput(t, output)
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("satisfied must allow, got %v", specific["permissionDecision"])
	}
	if qg["status"] != "satisfied" {
		t.Fatalf("expected status=\"satisfied\", got %v", qg["status"])
	}
}

// TestPreToolUseDoesNotFabricateAdvancedStatus — when the controller
// reports TransitionCommitted=false the layered render must NOT surface
// status="advanced" (closing contract: never_fabricate_advanced_status).
func TestPreToolUseDoesNotFabricateAdvancedStatus(t *testing.T) {
	result := controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:              controller.StatusSatisfied, // controller said satisfied, not advanced
			GateID:              "GATE-X",
			CandidateTransition: "TR-X",
			ObservedRevision:    4,
			Missing:             []string{},
			EvidenceRefs:        []string{},
			TransitionCommitted: false,
			NextCursor:          "planning.tasks",
		},
		Decision: policy.Decision{Decision: "allow"},
	}
	output, _, err := hook.PreToolUseWithQualityGate(result.Decision, result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, _, qg := decodeHookOutput(t, output)
	if qg["status"] == "advanced" {
		t.Fatalf("render MUST NOT fabricate advanced when TransitionCommitted=false, got %v", qg)
	}
	if v, _ := qg["transition_committed"].(bool); v {
		t.Fatalf("render MUST NOT fabricate transition_committed=true, got %v", qg)
	}
}

// TestPreToolUseSystemMessageAlwaysPresent — systemMessage must always
// accompany the PreToolUse payload so the agent has the Recovery Packet
// for every cursor (allow, advanced, blocked, unknown).
func TestPreToolUseSystemMessageAlwaysPresent(t *testing.T) {
	cases := []struct {
		name   string
		status controller.ControlStatus
	}{
		{"not_ready", controller.StatusNotReady},
		{"satisfied", controller.StatusSatisfied},
		{"unknown", controller.StatusUnknown},
		{"advanced", controller.StatusAdvanced},
		{"blocked", controller.StatusBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := controller.ControlResult{
				QualityGate: controller.QualityGateResult{
					Status:           tc.status,
					GateID:           "GATE-X",
					ObservedRevision: 7,
					Missing:          []string{},
					EvidenceRefs:     []string{},
					NextCursor:       "planning.design",
				},
				Decision: policy.Decision{Decision: "allow", Reason: "test"},
			}
			if tc.status == controller.StatusBlocked {
				result.Decision = policy.Decision{Decision: "block", RuleID: policy.RuleLockedArtifactWrite}
			}
			output, _, err := hook.PreToolUseWithQualityGate(result.Decision, result)
			if err != nil {
				t.Fatalf("render %s: %v", tc.name, err)
			}
			body, ok := envelope2SystemMessage(t, output)
			if !ok || body == "" {
				t.Fatalf("systemMessage must be present for status=%s, got %q", tc.name, body)
			}
		})
	}
}

// envelope2SystemMessage extracts the systemMessage string from a Hook
// payload. Returns the string and true, or "" and false when the field is
// missing / wrong type.
func envelope2SystemMessage(t *testing.T, data []byte) (string, bool) {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	s, ok := env["systemMessage"].(string)
	return s, ok
}
