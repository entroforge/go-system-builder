package controller

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestGraphCompletionPreservesStageConditionsWithoutToolDenial(t *testing.T) {
	for _, status := range []ControlStatus{StatusNotReady, StatusUnknown, StatusSatisfied, StatusAdvanced} {
		t.Run(string(status), func(t *testing.T) {
			result := finishControlCycle(ControlResult{
				Decision:    policy.Decision{Decision: "deny"},
				QualityGate: QualityGateResult{Status: status, TransitionCommitted: status == StatusAdvanced},
			})
			if result.Decision.Decision != "allow" || result.QualityGate.Status != status || result.QualityGate.TransitionCommitted != (status == StatusAdvanced) {
				t.Fatalf("listener changed stage progress or denied the tool: %+v", result)
			}
		})
	}
}
