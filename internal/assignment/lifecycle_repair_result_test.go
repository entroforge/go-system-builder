package assignment

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestRepairResultLifecyclePacketTargetsRegisteredSlot(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any, *workpacket.RepairResultPacket)
		want    bool
		wantErr bool
	}{
		{name: "exact blocked Agent slot", want: true},
		{
			name: "different runtime is legacy slot",
			mutate: func(_ map[string]any, packet *workpacket.RepairResultPacket) {
				packet.Binding.Sources.RuntimeID = "loop-other"
			},
		},
		{
			name: "different generation is legacy slot",
			mutate: func(_ map[string]any, packet *workpacket.RepairResultPacket) {
				packet.Binding.Sources.BaselineGeneration++
			},
		},
		{
			name: "different round is legacy slot",
			mutate: func(_ map[string]any, packet *workpacket.RepairResultPacket) {
				packet.Binding.Sources.ReviewRound++
			},
		},
		{
			name: "complete different manifest assignment remains legacy",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				agent := lifecycleTestAgent(state)
				agent["prompt_ref"] = "manifests/team.json#assignment-s9-other"
				agent["task_ids"] = []any{"TASK-005"}
				pointer := lifecycleTestRepairPointer(state)
				pointer["assignment_owners"] = map[string]any{"repair-assignment-other": "agent-s9-owner"}
			},
		},
		{
			name: "new registered manifest digest is another slot",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				lifecycleTestTeam(state)["manifest_sha256"] = strings.Repeat("b", 64)
			},
		},
		{
			name: "new repair pointer is another slot",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				lifecycleTestRepairPointer(state)["session_id"] = "repair-session-new"
			},
		},
		{
			name: "same prompt with missing TASK registration is malformed",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				lifecycleTestAgent(state)["task_ids"] = []any{"TASK-OTHER"}
			},
			wantErr: true,
		},
		{
			name: "missing pointer tuple is malformed",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				delete(lifecycleTestRepairPointer(state), "plan_sha256")
			},
			wantErr: true,
		},
		{
			name: "missing original assignment owner is malformed",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				lifecycleTestRepairPointer(state)["assignment_owners"] = map[string]any{}
			},
			wantErr: true,
		},
		{
			name: "conflicting registered assignment owner is malformed",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				lifecycleTestRepairPointer(state)["assignment_owners"].(map[string]any)["repair-assignment-s9-unit"] = "agent-s9-other"
			},
			wantErr: true,
		},
		{
			name: "duplicate Agent registration is ambiguous",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				entities := state["entities"].(map[string]any)
				rows := entities["agents"].([]any)
				entities["agents"] = append(rows, cloneLifecycleTestMap(rows[0].(map[string]any)))
			},
			wantErr: true,
		},
		{
			name: "duplicate Team registration is ambiguous",
			mutate: func(state map[string]any, _ *workpacket.RepairResultPacket) {
				entities := state["entities"].(map[string]any)
				rows := entities["teams"].([]any)
				entities["teams"] = append(rows, cloneLifecycleTestMap(rows[0].(map[string]any)))
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, packet := lifecycleRepairResultFixture()
			if test.mutate != nil {
				test.mutate(state, &packet)
			}
			before, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			got, err := repairResultLifecyclePacketTargetsOwner(state, "agent-s9-owner", packet)
			after, marshalErr := json.Marshal(state)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("lifecycle target helper mutated Runtime state")
			}
			if test.wantErr {
				if err == nil {
					t.Fatal("malformed or ambiguous registered tuple unexpectedly passed")
				}
				return
			}
			if err != nil {
				t.Fatalf("tuple match: %v", err)
			}
			if got != test.want {
				t.Fatalf("targeted = %t, want %t", got, test.want)
			}
		})
	}
}

func TestIssuedRepairResultLifecycleTargetReadsCanonicalPacketHistory(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-s9-lifecycle",
		"baseline":   map[string]any{"generation": 2},
		"review":     map[string]any{"round": 3},
		"entities":   map[string]any{"work_packets": []any{}},
	}
	if targeted, err := issuedRepairResultLifecycleTarget(state, "agent-s9-owner"); err != nil || targeted {
		t.Fatalf("empty typed history target = %t, %v; want false, nil", targeted, err)
	}
	state["entities"].(map[string]any)["work_packets"] = []any{
		map[string]any{"packet_id": "packet-duplicate", "template_id": "s9-repair-result-v1"},
		map[string]any{"packet_id": "packet-duplicate", "template_id": "s9-repair-result-v1"},
	}
	if targeted, err := issuedRepairResultLifecycleTarget(state, "agent-s9-owner"); err == nil || targeted {
		t.Fatalf("duplicate packet identity must fail closed, got target=%t err=%v", targeted, err)
	}
}

func lifecycleRepairResultFixture() (map[string]any, workpacket.RepairResultPacket) {
	sha := strings.Repeat("a", 64)
	binding := workpacket.RepairPlanReportBinding{
		RuntimeID: "loop-s9-lifecycle", BaselineGeneration: 2, ReviewRound: 3,
		CaseID: "investigation-case-s9", CaseRef: ".claude/review/case.json", CaseSHA256: sha,
		ContractID: "repair-contract-s9", ContractRef: ".claude/review/contract.json", ContractSHA256: sha,
		SessionID: "repair-session-s9", SessionRef: ".claude/review/session.json", SessionSHA256: sha,
		PlanID: "repair-plan-s9", PlanRef: ".claude/review/plan.json", PlanSHA256: sha,
		RepairAssignmentID: "repair-assignment-s9-unit", RepairAssignmentSHA256: sha,
		ManifestAssignmentID: "assignment-s9-unit", ManifestAssignmentSHA256: sha,
		TeamID: "team-s9", TaskID: "TASK-004", TaskRef: ".claude/workgroups/TASK-004.json", TaskSHA256: sha,
		ManifestRef: "manifests/team.json", ManifestSHA256: sha, OwnerAgentID: "agent-s9-owner",
	}
	state := map[string]any{
		"runtime_id": binding.RuntimeID,
		"baseline":   map[string]any{"generation": binding.BaselineGeneration},
		"review": map[string]any{
			"round": binding.ReviewRound,
			"repair": map[string]any{
				"session_id": binding.SessionID, "path": binding.SessionRef, "sha256": binding.SessionSHA256,
				"plan_ref": binding.PlanRef, "plan_sha256": binding.PlanSHA256,
				"contract_id": binding.ContractID, "contract_ref": binding.ContractRef, "contract_sha256": binding.ContractSHA256,
				"assignment_owners": map[string]any{binding.RepairAssignmentID: binding.OwnerAgentID},
			},
		},
		"entities": map[string]any{
			"agents": []any{map[string]any{
				"id": binding.OwnerAgentID, "state": "blocked", "team_id": binding.TeamID,
				"task_ids": []any{binding.TaskID}, "prompt_ref": binding.ManifestRef + "#" + binding.ManifestAssignmentID,
			}},
			"teams": []any{map[string]any{
				"id": binding.TeamID, "manifest_ref": binding.ManifestRef, "manifest_sha256": binding.ManifestSHA256,
			}},
		},
	}
	return state, workpacket.RepairResultPacket{Binding: workpacket.RepairResultBinding{Sources: binding}}
}

func lifecycleTestAgent(state map[string]any) map[string]any {
	return state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
}

func lifecycleTestTeam(state map[string]any) map[string]any {
	return state["entities"].(map[string]any)["teams"].([]any)[0].(map[string]any)
}

func lifecycleTestRepairPointer(state map[string]any) map[string]any {
	return state["review"].(map[string]any)["repair"].(map[string]any)
}

func cloneLifecycleTestMap(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	var clone map[string]any
	_ = json.Unmarshal(encoded, &clone)
	return clone
}
