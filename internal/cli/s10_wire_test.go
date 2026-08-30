package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// Regression for the 2026-08-28 S10 walkthrough: the live hook wire dropped
// quality_gate.conflicts and error_code, so a fully-coached
// evidence_ref_missing diagnosis reached the agent as a bare
// LOOP_GATE_UNKNOWN with no next action.
func TestQualityGateWireExposesConflictsAndErrorCode(t *testing.T) {
	qg := controller.QualityGateResult{
		Status:       controller.StatusUnknown,
		GateID:       "GATE-ACCEPTANCE-COMPLETE",
		ErrorCode:    qualitygate.ErrorGateUnknown,
		Missing:      nil,
		EvidenceRefs: []string{"acc-record-r2", "clean-round-r2"},
		Conflicts: []string{
			"s10:acceptance_manifest:acc-record-r2:evidence_ref_missing:reverify-r13-1; next: register the referenced current evidence first",
		},
	}

	block, ok := qualityGateEnvelopeFields(qg)["quality_gate"].(map[string]any)
	if !ok {
		t.Fatalf("quality_gate block missing from wire fields")
	}
	conflicts, _ := block["conflicts"].([]string)
	if len(conflicts) != 1 || !strings.Contains(conflicts[0], "evidence_ref_missing") {
		t.Fatalf("wire conflicts = %v, want the manifest conflict verbatim", conflicts)
	}
	if code, _ := block["error_code"].(string); code != qualitygate.ErrorGateUnknown {
		t.Fatalf("wire error_code = %v, want %q", block["error_code"], qualitygate.ErrorGateUnknown)
	}
	if missing, _ := block["missing"].([]string); missing == nil {
		t.Fatalf("nil missing must serialize as an empty list, not be dropped")
	}
}

func TestQualityGateWireWithConflictFieldsSatisfiesHookDecisionSchema(t *testing.T) {
	envelope := policy.DecisionEnvelope{
		SchemaVersion:  "1.1.0",
		DecisionID:     "hook-decision-s10-wire",
		PolicyID:       "hook-policy-main",
		PolicyVersion:  "1.0.0",
		PolicySHA256:   strings.Repeat("a", 64),
		HookEvent:      "PreToolUse",
		SessionID:      "session-s10-wire",
		MatchedRuleIDs: []string{},
		Decision:       "allow",
		Reason:         "quality gate probe",
		Missing:        []string{},
		Recovery:       []string{},
		Retry:          "not_applicable",
		EvaluatedAt:    "2026-08-28T00:00:00Z",
	}
	data, err := json.Marshal(envelopeWithQualityGateMap(envelope, controller.ControlResult{
		QualityGate: controller.QualityGateResult{
			Status:    controller.StatusUnknown,
			ErrorCode: qualitygate.ErrorGateUnknown,
			Conflicts: []string{"manifest evidence ref is missing"},
		},
	}))
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("hook-decision.schema.json", data); err != nil {
		t.Fatalf("quality gate wire must satisfy hook-decision.schema.json: %v\n%s", err, data)
	}
}

// The systemMessage recovery packet must render conflicts too: it is the only
// channel agents reliably read without parsing JSON.
func TestPreToolUseUnknownPacketIncludesConflicts(t *testing.T) {
	qg := controller.QualityGateResult{
		Status:              controller.StatusUnknown,
		GateID:              "GATE-ACCEPTANCE-COMPLETE",
		CandidateTransition: "TR-015",
		ErrorCode:           qualitygate.ErrorGateUnknown,
		Conflicts:           []string{"s10:acceptance_manifest:x:evidence_ref_missing:y"},
	}
	packet := hook.FormatPreToolUseRecoveryPacket(policy.Decision{Decision: "allow"}, qg)
	if !strings.Contains(packet, "Conflicts: s10:acceptance_manifest:x:evidence_ref_missing:y") {
		t.Fatalf("packet missing conflict text: %q", packet)
	}
}
