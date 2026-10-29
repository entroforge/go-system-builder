package assignment

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Only parameter handling is exercised here. The original lifecycle Writer
// separately verifies the complete typed blocker capability and source facts.
func TestS9ProfessionalBlockerParamsUseActiveResponsibility(t *testing.T) {
	for _, event := range []string{"readback_started", "readback_submitted", "activation_sent", "work_started", "completion_reported"} {
		t.Run(event, func(t *testing.T) {
			agent := map[string]any{"state": "active", "last_lifecycle_event": event}
			request := AgentEventRequest{Event: "work_blocked", IssuedRepairResultBlocker: &repair.IssuedRepairResultBlockerCapability{}}
			if err := applyAgentEventParams("", agent, workstate.UnifiedVersion, request, agentMessage{}, 0); err != nil {
				t.Fatalf("professional blocker required a legacy working milestone: %v", err)
			}
			request.IssuedRepairResultBlocker = nil
			if err := applyAgentEventParams("", agent, workstate.UnifiedVersion, request, agentMessage{}, 0); (err == nil) != (event == "work_started") {
				t.Fatalf("generic work_blocked compatibility changed: event=%s err=%v", event, err)
			}
		})
	}
	for _, row := range []map[string]any{
		{"state": "pending", "last_lifecycle_event": "agent_created"},
		{"state": "blocked", "last_lifecycle_event": "work_blocked"},
		{"state": "done", "last_lifecycle_event": "completion_acknowledged"},
		{"state": "cancelled", "last_lifecycle_event": "shutdown_approved"},
	} {
		request := AgentEventRequest{Event: "work_blocked", IssuedRepairResultBlocker: &repair.IssuedRepairResultBlockerCapability{}}
		if err := applyAgentEventParams("", row, workstate.UnifiedVersion, request, agentMessage{}, 0); err == nil {
			t.Fatalf("new blocker accepted a non-active responsibility: %v", row)
		}
	}
}
