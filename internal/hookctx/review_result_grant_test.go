package hookctx

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS7ReviewResultFreshWriteGrantRequiresActiveVerificationAndOpenAssignment(t *testing.T) {
	packet := workpacket.ReviewResultPacket{Binding: workpacket.ReviewResultBinding{
		AssignmentID: "assignment-reviewer-1",
		OwnerAgentID: "reviewer-1",
	}}
	stateFor := func(lifecycle, planStatus, assignmentStatus string, resultRef any) map[string]any {
		return map[string]any{
			"lifecycle": map[string]any{"state": lifecycle},
			"review": map[string]any{
				"plan": map[string]any{"status": planStatus},
				"assignments": map[string]any{packet.Binding.AssignmentID: map[string]any{
					"agent_id":   packet.Binding.OwnerAgentID,
					"status":     assignmentStatus,
					"result_ref": resultRef,
				}},
			},
		}
	}
	for _, status := range []string{"running", "cannot_clean", "discovery_draining"} {
		state := stateFor("verification", status, "dispatched", nil)
		if !s7ReviewResultFreshWriteStage(state) || !s7ReviewResultAssignmentWritable(state, packet) {
			t.Fatalf("current open Reviewer write was lost at active status %q", status)
		}
	}
	if !s7ReviewResultAssignmentWritable(stateFor("verification", "running", "blocked", nil), packet) {
		t.Fatal("a nonterminal blocked Assignment with the original working permission was not retained")
	}
	for _, tc := range []struct {
		name            string
		lifecycle, plan string
		assignment      string
		resultRef       any
	}{
		{name: "outside verification", lifecycle: "building", plan: "running", assignment: "dispatched"},
		{name: "sealed Plan", lifecycle: "verification", plan: "clean", assignment: "dispatched"},
		{name: "consumed Assignment", lifecycle: "verification", plan: "running", assignment: "consumed", resultRef: ".claude/evidence/result.json"},
		{name: "unknown terminal status", lifecycle: "verification", plan: "running", assignment: "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := stateFor(tc.lifecycle, tc.plan, tc.assignment, tc.resultRef)
			if s7ReviewResultFreshWriteStage(state) && s7ReviewResultAssignmentWritable(state, packet) {
				t.Fatal("terminal or stale ReviewResult received a fresh authoring grant")
			}
		})
	}
}
