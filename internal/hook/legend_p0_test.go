// legend_p0_test.go pins the L3-S6 complexity-pass legend wiring: a
// not_ready builder-batch packet carries the token legend with executable
// next actions; satisfied/unknown packets do not.
package hook_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func legendPacketInput() (policy.Decision, controller.ControlResult) {
	return policy.Decision{Decision: "allow"},
		controller.ControlResult{
			QualityGate: controller.QualityGateResult{
				Status:              controller.StatusNotReady,
				GateID:              "GATE-BUILDER-BATCH-READY",
				CandidateTransition: "TR-006",
				Missing: []string{
					"evidence:completion_report:TASK-039-02",
					"integration_checkpoint:TASK-039-02",
				},
			},
		}
}

func TestNotReadyBuilderPacketCarriesTokenLegend(t *testing.T) {
	decision, result := legendPacketInput()
	data, code, err := hook.PreToolUseWithQualityGate(decision, result)
	if err != nil || code != 0 {
		t.Fatalf("hook render: code=%d err=%v", code, err)
	}
	var payload struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"MISSING TOKENS:",
		"run `runtime task-complete` for the named TASK",
		"run `runtime task-integrate --assignment-id <id>`",
	} {
		if !strings.Contains(payload.SystemMessage, want) {
			t.Fatalf("systemMessage missing %q:\n%s", want, payload.SystemMessage)
		}
	}
}

func TestSatisfiedPacketHasNoLegend(t *testing.T) {
	decision, result := legendPacketInput()
	result.QualityGate.Status = controller.StatusSatisfied
	result.QualityGate.Missing = nil
	data, _, err := hook.PreToolUseWithQualityGate(decision, result)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload.SystemMessage, "MISSING TOKENS:") {
		t.Fatalf("satisfied packet must not carry the legend:\n%s", payload.SystemMessage)
	}
}
