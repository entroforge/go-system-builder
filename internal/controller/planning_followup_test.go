package controller

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestCurrentPlanningPacketRequiresUniqueExactCurrentBinding(t *testing.T) {
	for _, phase := range []string{"design", "contracts", "tasks"} {
		t.Run(phase, func(t *testing.T) {
			state, binding, row := planningFollowupFixture(t, phase)
			entities := state["entities"].(map[string]any)
			rows := entities["work_packets"].([]any)
			entities["work_packets"] = append(rows, row)
			packet, err := findCurrentPlanningPacket(state, binding.Stage, planningTemplateForPhase(phase), binding, expectedDesignForPhase(t, phase, state))
			if err != nil || packet.id == "" {
				t.Fatalf("select current %s packet: %+v %v", phase, packet, err)
			}

			// A packet from an earlier generation is historical and cannot make
			// the current selector ambiguous or satisfy its current slot.
			old := historicalPlanningPacketRow(t, phase, binding)
			entities["work_packets"] = append(entities["work_packets"].([]any), old)
			if _, err := findCurrentPlanningPacket(state, binding.Stage, planningTemplateForPhase(phase), binding, expectedDesignForPhase(t, phase, state)); err != nil {
				t.Fatalf("old generation packet affected current selection: %v", err)
			}

			// Duplicate exact issuance and same-entry wrong-owner issuance are
			// conflicts, never first-row-wins selection.
			duplicate := clonePlanningPacketRow(t, row)
			entities["work_packets"] = append(entities["work_packets"].([]any), duplicate)
			if _, err := findCurrentPlanningPacket(state, binding.Stage, planningTemplateForPhase(phase), binding, expectedDesignForPhase(t, phase, state)); err == nil {
				t.Fatal("duplicate exact packet identity was accepted")
			}

			wrongOwnerState, wrongOwnerBinding, wrongOwnerRow := planningFollowupFixture(t, phase)
			wrong := wrongOwnerRow["binding"].(map[string]any)
			wrong["owner"] = "worker-a"
			wrongOwnerEntities := wrongOwnerState["entities"].(map[string]any)
			wrongOwnerEntities["work_packets"] = []any{wrongOwnerRow}
			if _, err := findCurrentPlanningPacket(wrongOwnerState, wrongOwnerBinding.Stage, planningTemplateForPhase(phase), wrongOwnerBinding, expectedDesignForPhase(t, phase, wrongOwnerState)); (phase == "design" && err != nil) || (phase != "design" && err == nil) {
				t.Fatalf("owner metadata differs from expected %s behavior: %v", phase, err)
			}

			wrongREQState, wrongREQBinding, wrongREQRow := planningFollowupFixture(t, phase)
			wrongREQ := wrongREQRow["binding"].(map[string]any)["requirement"].(map[string]any)
			wrongREQ["id"] = "REQ-040"
			wrongREQEntities := wrongREQState["entities"].(map[string]any)
			wrongREQEntities["work_packets"] = []any{wrongREQRow}
			if _, err := findCurrentPlanningPacket(wrongREQState, wrongREQBinding.Stage, planningTemplateForPhase(phase), wrongREQBinding, expectedDesignForPhase(t, phase, wrongREQState)); (phase == "design" && err != nil) || (phase != "design" && err == nil) {
				t.Fatalf("REQ display metadata differs from expected %s behavior: %v", phase, err)
			}
			if phase == "design" {
				entities["work_packets"] = []any{row}
				entities["submissions"] = []any{map[string]any{
					"packet_id": row["packet_id"], "owner": "actual-author", "submission_key": "recorded-key",
					"followup_event_id": "recorded-followup", "evidence_id": "accepted-design",
					"control_status": "pending", "origin_state": "planning", "origin_phase": "design", "origin_entry": binding.StageEntry,
				}}
				if _, err := currentPlanningSubmission(state, row["packet_id"].(string), binding, "design", "s2-design-v1"); err != nil {
					t.Fatalf("accepted S2 pending must continue without owner/key proof: %v", err)
				}
			}
		})
	}
}

func planningFollowupFixture(t *testing.T, phase string) (map[string]any, workpacket.PlanningBinding, map[string]any) {
	t.Helper()
	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     "runtime-planning-test",
		"baseline":       map[string]any{"generation": float64(2)},
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"bound_req": map[string]any{
			"id": "REQ-039", "path": "docs/requirements/REQ-039.md", "version": "v1.0.0",
			"status": "locked",
		},
		"lifecycle": map[string]any{"state": "planning", "phase": phase, "phase_revision": float64(4)},
		"documents": []any{map[string]any{
			"id": "ARCHITECTURE-039", "kind": "design", "path": "docs/architecture/ARCHITECTURE-039.md",
			"version": "v1.0.0", "status": "locked", "generation": float64(2),
		}},
		"entities": map[string]any{"work_packets": []any{}, "submissions": []any{}},
	}
	var binding workpacket.PlanningBinding
	var row map[string]any
	switch phase {
	case "design":
		packet, err := runtime.CurrentDesignPacket(state)
		if err != nil {
			t.Fatal(err)
		}
		binding = packet.Binding
		row = planningPacketMap(t, packet)
	case "contracts":
		binding, _, _ = runtime.CurrentContractBinding(state)
		packet := workpacket.ContractPacket{ID: "packet-contract-current", TemplateID: "s3-contract-v1", Binding: binding, AnswerPath: ".claude/forms/S3.json"}
		row = planningPacketMap(t, packet)
	case "tasks":
		binding, _ = runtime.CurrentTaskPlanningBinding(state)
		packet := workpacket.TaskPlanningPacket{ID: "packet-task-current", TemplateID: "s4-task-planning-v1", Binding: binding, AnswerPath: ".claude/forms/S4.json"}
		row = planningPacketMap(t, packet)
	default:
		t.Fatalf("unknown phase %s", phase)
	}
	return state, binding, row
}

func historicalPlanningPacketRow(t *testing.T, phase string, current workpacket.PlanningBinding) map[string]any {
	t.Helper()
	old := current
	old.Generation--
	var packet any
	switch phase {
	case "design":
		p, err := workpacket.BuildDesign(old)
		if err != nil {
			t.Fatal(err)
		}
		packet = p
	case "contracts":
		inputs := workpacket.ContractInputs{
			Designs: []workpacket.Reference{{
				ID: "ARCHITECTURE-039", Path: "docs/architecture/ARCHITECTURE-039.md",
				Version: "v1.0.0",
			}},
			Sources: []workpacket.Subject{}, Directories: map[string][]string{},
		}
		p, err := workpacket.BuildContracts(old, inputs)
		if err != nil {
			t.Fatal(err)
		}
		packet = p
	case "tasks":
		inputs := workpacket.TaskPlanningInputs{
			Contracts: []workpacket.Reference{
				{ID: "CONTRACTS-039", Path: "docs/dev/contracts/CONTRACTS-039.md", Version: "v1.0.0"},
				{ID: "BE-039", Path: "docs/dev/contracts/BE-039.md", Version: "v1.0.0"},
			},
			Sources: []workpacket.Subject{}, Directories: map[string][]string{},
			ClauseUniverse: []workpacket.TaskClause{{ContractID: "BE-039", Section: 1}},
		}
		p, err := workpacket.BuildTaskPlanning(old, inputs)
		if err != nil {
			t.Fatal(err)
		}
		packet = p
	default:
		t.Fatalf("unknown planning phase %s", phase)
	}
	return planningPacketMap(t, packet)
}

func expectedDesignForPhase(t *testing.T, phase string, state map[string]any) *workpacket.DesignPacket {
	t.Helper()
	if phase != "design" {
		return nil
	}
	packet, err := runtime.CurrentDesignPacket(state)
	if err != nil {
		t.Fatal(err)
	}
	return &packet
}

func planningTemplateForPhase(phase string) string {
	return map[string]string{"design": "s2-design-v1", "contracts": "s3-contract-v1", "tasks": "s4-task-planning-v1"}[phase]
}

func planningPacketMap(t *testing.T, packet any) map[string]any {
	t.Helper()
	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func clonePlanningPacketRow(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
