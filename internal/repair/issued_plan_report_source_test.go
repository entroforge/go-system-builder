package repair

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRepairPlanReportWorkerDeclaredMatchesCanonicalAndManifestIdentity(t *testing.T) {
	packet := workpacket.RepairPlanReportPacket{Binding: workpacket.RepairPlanReportBinding{
		RuntimeID: "loop-s9-source", BaselineGeneration: 2,
		RepairAssignmentID:   "repair-assignment-unit-api",
		ManifestAssignmentID: "assignment-s9-unit-api",
		OwnerAgentID:         "agent-builder-s9",
	}}
	cases := []struct {
		name       string
		key        string
		rowID      string
		runtimeID  string
		generation int
		owner      string
		want       bool
	}{
		{name: "canonical execution key", key: packet.Binding.RepairAssignmentID, rowID: packet.Binding.RepairAssignmentID, runtimeID: packet.Binding.RuntimeID, generation: 2, owner: packet.Binding.OwnerAgentID, want: true},
		{name: "registered manifest alias key", key: packet.Binding.ManifestAssignmentID, rowID: packet.Binding.ManifestAssignmentID, runtimeID: packet.Binding.RuntimeID, generation: 2, owner: packet.Binding.OwnerAgentID, want: true},
		{name: "canonical row under alias key", key: packet.Binding.ManifestAssignmentID, rowID: packet.Binding.RepairAssignmentID, runtimeID: packet.Binding.RuntimeID, generation: 2, owner: packet.Binding.OwnerAgentID, want: true},
		{name: "different owner is not this packet execution", key: packet.Binding.ManifestAssignmentID, rowID: packet.Binding.ManifestAssignmentID, runtimeID: packet.Binding.RuntimeID, generation: 2, owner: "agent-other", want: false},
		{name: "different Runtime is not this packet execution", key: packet.Binding.ManifestAssignmentID, rowID: packet.Binding.ManifestAssignmentID, runtimeID: "loop-other", generation: 2, owner: packet.Binding.OwnerAgentID, want: false},
		{name: "different baseline generation is not this packet execution", key: packet.Binding.ManifestAssignmentID, rowID: packet.Binding.ManifestAssignmentID, runtimeID: packet.Binding.RuntimeID, generation: 1, owner: packet.Binding.OwnerAgentID, want: false},
		{name: "unrelated assignment is not this packet execution", key: "assignment-s9-other", rowID: "assignment-s9-other", runtimeID: packet.Binding.RuntimeID, generation: 2, owner: packet.Binding.OwnerAgentID, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := &workspace.ExecutionRegistry{Executions: map[string]workspace.Execution{
				tc.key: {AssignmentID: tc.rowID, RuntimeID: tc.runtimeID, BaselineGeneration: tc.generation, AgentID: tc.owner},
			}}
			if got := repairPlanReportWorkerDeclared(registry, packet); got != tc.want {
				t.Fatalf("repairPlanReportWorkerDeclared() = %v, want %v", got, tc.want)
			}
		})
	}
}
