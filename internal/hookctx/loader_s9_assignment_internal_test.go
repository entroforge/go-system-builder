package hookctx

import (
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9RepairAssignmentUsesPointerOwnerAndExactRegisteredManifestTuple(t *testing.T) {
	plan := repairPlanHook{Assignments: []repairPlanHookAssignment{{
		AssignmentID: "repair-assignment-api",
		Scope:        []string{"internal/api/"},
		// The immutable RepairPlan does not carry owner_agent_id. Ownership
		// comes from the Runtime pointer and original packet binding.
	}}}
	owners := map[string]any{"repair-assignment-api": "builder-api"}
	agent := &policy.AgentContext{
		ID:      "builder-api",
		TeamID:  "team-api",
		TaskIDs: []string{"TASK-API"},
	}
	packet := workpacket.RepairPlanReportPacket{Binding: workpacket.RepairPlanReportBinding{
		RepairAssignmentID:   "repair-assignment-api",
		ManifestAssignmentID: "assignment-manifest-api",
		OwnerAgentID:         "builder-api",
		TeamID:               "team-api",
		TaskID:               "TASK-API",
	}}

	got, ok := repairAssignmentForPacket(plan, owners, packet, "builder-api", "assignment-manifest-api", agent)
	if !ok || !reflect.DeepEqual(got.Scope, []string{"internal/api/"}) {
		t.Fatalf("exact packet should resolve the owner-mapped RepairAssignment: got=%+v ok=%v", got, ok)
	}

	badCases := []struct {
		name   string
		mutate func(*workpacket.RepairPlanReportPacket, map[string]any, *policy.AgentContext)
	}{
		{
			name: "wrong manifest assignment",
			mutate: func(packet *workpacket.RepairPlanReportPacket, _ map[string]any, _ *policy.AgentContext) {
				packet.Binding.ManifestAssignmentID = "assignment-other"
			},
		},
		{
			name: "wrong team",
			mutate: func(packet *workpacket.RepairPlanReportPacket, _ map[string]any, _ *policy.AgentContext) {
				packet.Binding.TeamID = "team-other"
			},
		},
		{
			name: "undispatched task",
			mutate: func(packet *workpacket.RepairPlanReportPacket, _ map[string]any, _ *policy.AgentContext) {
				packet.Binding.TaskID = "TASK-OTHER"
			},
		},
		{
			name: "pointer names a different owner",
			mutate: func(_ *workpacket.RepairPlanReportPacket, owners map[string]any, _ *policy.AgentContext) {
				owners["repair-assignment-api"] = "builder-other"
			},
		},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := packet
			candidateOwners := map[string]any{"repair-assignment-api": "builder-api"}
			candidateAgent := *agent
			tc.mutate(&candidate, candidateOwners, &candidateAgent)
			if _, ok := repairAssignmentForPacket(plan, candidateOwners, candidate, "builder-api", "assignment-manifest-api", &candidateAgent); ok {
				t.Fatal("mismatched original registration tuple was accepted")
			}
		})
	}
}
