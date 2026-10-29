package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRepairResultPacketMatchesOnlyExactRegisteredExecution(t *testing.T) {
	for _, tc := range []struct{ name, registered, canonical, manifest string }{
		{"registered manifest identity", "assignment-registered", "repair-assignment-canonical", "assignment-registered"},
		{"canonical identity only when it is the execution ID", "repair-assignment-canonical", "repair-assignment-canonical", "assignment-alias"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packet, execution, state := repairResultMatcherFixture(t, tc.registered, tc.canonical, tc.manifest)
			selected, err := runtime.RepairResultExecution(state, packet)
			if err != nil || selected["assignment_id"] != execution.AssignmentID {
				t.Fatalf("exact original registered execution rejected: %v %v", selected, err)
			}
		})
	}
	t.Run("arbitrary alias is unrelated", func(t *testing.T) {
		packet, _, state := repairResultMatcherFixture(t, "assignment-other", "repair-assignment-canonical", "assignment-registered")
		if selected, err := runtime.RepairResultExecution(state, packet); err == nil {
			t.Fatalf("unregistered alias matched original authority: %v", selected)
		}
	})
	t.Run("frozen assignment cannot substitute another ID", func(t *testing.T) {
		packet, _, state := repairResultMatcherFixture(t, "assignment-registered", "repair-assignment-canonical", "assignment-registered")
		var frozen map[string]any
		if err := json.Unmarshal(packet.Inputs.Execution, &frozen); err != nil {
			t.Fatal(err)
		}
		frozen["assignment_id"] = "assignment-forged"
		packet.Inputs.Execution, _ = json.Marshal(frozen)
		if selected, err := runtime.RepairResultExecution(state, packet); err == nil {
			t.Fatalf("forged frozen execution accepted: %v", selected)
		}
	})
	t.Run("frozen definition must equal current registered definition", func(t *testing.T) {
		packet, execution, state := repairResultMatcherFixture(t, "assignment-registered", "repair-assignment-canonical", "assignment-registered")
		current := state["workspace"].(map[string]any)["executions"].(map[string]any)[execution.AssignmentID].(map[string]any)
		current["write_paths"] = []any{"src/other/**"}
		if selected, err := runtime.RepairResultExecution(state, packet); err == nil {
			t.Fatalf("drifted actual registered definition accepted: %v", selected)
		}
	})
}

func TestRepairResultCandidateMustMatchPacketAndRegisteredExecution(t *testing.T) {
	packet, execution, _ := repairResultMatcherFixture(t, "assignment-registered", "repair-assignment-canonical", "assignment-registered")
	packet.ID = "packet-test"
	packet.IssuanceEventID = "evt-repair-result-packet-packet-test"
	packet.ResultID = "repair-result-test"
	packet.Binding.PlanReportRef = "runtime/g2/S9/assignment/owner/report.json"
	packet.Binding.PlanReportSHA256 = strings.Repeat("a", 64)
	packet.Binding.ReworkRef = ""
	packet.Binding.ReworkSHA256 = ""
	packetSHA, err := workpacket.RepairResultPacketContentDigest(packet)
	if err != nil {
		t.Fatal(err)
	}

	base := repair.WorkspaceCandidate{
		RuntimeID: execution.RuntimeID, BaselineGeneration: execution.BaselineGeneration,
		ExecutionGeneration: execution.Generation, AssignmentID: execution.AssignmentID,
		SourceHead: strings.Repeat("c", 40), BaseCommit: execution.BaseCommit,
		SessionID: packet.Binding.Sources.SessionID, PlanID: packet.Binding.Sources.PlanID,
		ContractSHA256: packet.Binding.Sources.ContractSHA256,
		Proposal:       repair.RepairResultRequest{ResultID: packet.ResultID, AssignmentID: packet.Binding.Sources.RepairAssignmentID, ProducerAgentID: execution.AgentID},
		Provenance: &runtime.RepairResultCandidateProvenance{
			Contract: "s9-repair-result-candidate-v1", PacketID: packet.ID, PacketSHA256: packetSHA,
			IssuanceEventID: packet.IssuanceEventID, OwnerAgentID: execution.AgentID,
			RuntimeID: execution.RuntimeID, BaselineGeneration: execution.BaselineGeneration,
			ReviewRound: packet.Binding.Sources.ReviewRound, ExecutionAssignmentID: execution.AssignmentID,
			ExecutionGeneration: execution.Generation, ExecutionSHA256: packet.Binding.ExecutionSHA256,
			ExecutionRoot: execution.Path, SourceHead: strings.Repeat("c", 40), BaseCommit: execution.BaseCommit,
			PlanReportRef: packet.Binding.PlanReportRef, PlanReportSHA256: packet.Binding.PlanReportSHA256,
		},
	}
	if !repairResultCandidateMatchesPacketExecution(base, packet, execution, packetSHA) {
		t.Fatal("exact candidate/packet/execution tuple was rejected")
	}

	cases := []struct {
		name   string
		mutate func(*repair.WorkspaceCandidate)
	}{
		{name: "candidate assignment", mutate: func(c *repair.WorkspaceCandidate) { c.AssignmentID = "assignment-other" }},
		{name: "proposal result identity", mutate: func(c *repair.WorkspaceCandidate) { c.Proposal.ResultID = "repair-result-other" }},
		{name: "candidate runtime", mutate: func(c *repair.WorkspaceCandidate) { c.RuntimeID = "loop-other" }},
		{name: "candidate generation", mutate: func(c *repair.WorkspaceCandidate) { c.ExecutionGeneration++ }},
		{name: "candidate session", mutate: func(c *repair.WorkspaceCandidate) { c.SessionID = "session-other" }},
		{name: "candidate plan", mutate: func(c *repair.WorkspaceCandidate) { c.PlanID = "plan-other" }},
		{name: "candidate contract", mutate: func(c *repair.WorkspaceCandidate) { c.ContractSHA256 = strings.Repeat("d", 64) }},
		{name: "packet digest", mutate: func(c *repair.WorkspaceCandidate) { c.Provenance.PacketSHA256 = strings.Repeat("e", 64) }},
		{name: "execution assignment", mutate: func(c *repair.WorkspaceCandidate) { c.Provenance.ExecutionAssignmentID = "assignment-other" }},
		{name: "execution source", mutate: func(c *repair.WorkspaceCandidate) { c.Provenance.ExecutionRoot = "/other/worker" }},
		{name: "plan report digest", mutate: func(c *repair.WorkspaceCandidate) { c.Provenance.PlanReportSHA256 = strings.Repeat("f", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			provenance := *base.Provenance
			candidate.Provenance = &provenance
			tc.mutate(&candidate)
			if repairResultCandidateMatchesPacketExecution(candidate, packet, execution, packetSHA) {
				t.Fatal("candidate with mismatched tuple was accepted")
			}
		})
	}
}

func repairResultMatcherFixture(t *testing.T, registeredID, canonicalID, manifestID string) (workpacket.RepairResultPacket, workspace.Execution, map[string]any) {
	t.Helper()
	execution := workspace.Execution{
		AssignmentID: registeredID, RuntimeID: "loop-s9-matcher", BaselineGeneration: 2,
		Generation: 3, AgentID: "agent-builder", Path: "/tmp/worker-s9", Branch: "s9-worker",
		TargetBranch: "main", BaseCommit: strings.Repeat("1", 40), Status: "ready",
		WritePaths: []string{"src/**"}, Checks: []string{"go test ./..."}, Inputs: map[string]string{"task": "TASK-1"},
	}
	encoded, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	if err := json.Unmarshal(encoded, &current); err != nil {
		t.Fatal(err)
	}
	projection, err := runtime.ProjectRepairResultExecution(current)
	if err != nil {
		t.Fatal(err)
	}
	packet := workpacket.RepairResultPacket{
		Binding: workpacket.RepairResultBinding{
			Sources: workpacket.RepairPlanReportBinding{
				RuntimeID: execution.RuntimeID, BaselineGeneration: execution.BaselineGeneration,
				ReviewRound: 1, RepairAssignmentID: canonicalID, ManifestAssignmentID: manifestID,
				OwnerAgentID: execution.AgentID, SessionID: "repair-session-1", PlanID: "repair-plan-1",
				ContractSHA256: strings.Repeat("2", 64),
			},
			ExecutionSHA256: workpacket.Digest(projection),
		},
		Inputs: workpacket.RepairResultInputs{Execution: projection},
	}
	return packet, execution, map[string]any{"workspace": map[string]any{"executions": map[string]any{execution.AssignmentID: current}}}
}
