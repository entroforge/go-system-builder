package controller

import (
	"encoding/json"
	"strings"
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
					"control_status": "no_transition", "origin_state": "planning", "origin_phase": "design", "origin_entry": binding.StageEntry,
				}}
				if _, err := currentPlanningSubmission(state, row["packet_id"].(string), binding, "design", "s2-design-v1"); err != nil {
					t.Fatalf("accepted S2 no_transition must continue without owner/key proof: %v", err)
				}
			}
		})
	}
}

func TestCurrentPlanningSubmissionRequiresOnePendingMainCurrentEntry(t *testing.T) {
	state, binding, packetRow := planningFollowupFixture(t, "tasks")
	packetID := packetRow["packet_id"].(string)
	key := workpacket.Digest([]byte("submission:" + packetID))
	followup := workpacket.Digest([]byte(key + ":after-domain"))
	row := map[string]any{
		"submission_key": key, "packet_id": packetID, "owner": "Main",
		"content_sha256": strings.Repeat("a", 64), "evidence_id": "ev-s4",
		"followup_event_id": followup, "control_status": "pending",
		"origin_state": "planning", "origin_phase": "tasks", "origin_entry": float64(binding.StageEntry),
	}
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = []any{packetRow}
	entities["submissions"] = []any{row}
	got, err := currentPlanningSubmission(state, packetID, binding, "tasks", "s4-task-planning-v1")
	if err != nil || got.FollowupID != followup {
		t.Fatalf("select accepted pending Main submission: %+v %v", got, err)
	}
	if _, err := runtime.SubmissionFollowup(state, followup); err != nil {
		t.Fatalf("original followup validator rejected exact current submission: %v", err)
	}

	for name, rows := range map[string][]any{
		"missing":   nil,
		"duplicate": {row, cloneMap(row)},
	} {
		t.Run(name, func(t *testing.T) {
			entities["submissions"] = rows
			if _, err := currentPlanningSubmission(state, packetID, binding, "tasks", "s4-task-planning-v1"); err == nil {
				t.Fatalf("%s submission rows were accepted", name)
			}
		})
	}

	for name, mutate := range map[string]func(map[string]any){
		"wrong owner":       func(r map[string]any) { r["owner"] = "worker-a" },
		"wrong stage":       func(r map[string]any) { r["origin_phase"] = "contracts" },
		"wrong stage entry": func(r map[string]any) { r["origin_entry"] = float64(binding.StageEntry - 1) },
		"already consumed":  func(r map[string]any) { r["control_status"] = "replaced" },
		"revision packet":   func(r map[string]any) { r["revision_packet_id"] = "replacement" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneMap(row)
			mutate(changed)
			entities["submissions"] = []any{changed}
			if _, err := currentPlanningSubmission(state, packetID, binding, "tasks", "s4-task-planning-v1"); err == nil {
				t.Fatalf("%s submission was accepted", name)
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
			"sha256": strings.Repeat("a", 64), "status": "locked",
		},
		"lifecycle": map[string]any{"state": "planning", "phase": phase, "phase_revision": float64(4)},
		"documents": []any{map[string]any{
			"id": "ARCHITECTURE-039", "kind": "design", "path": "docs/architecture/ARCHITECTURE-039.md",
			"version": "v1.0.0", "sha256": strings.Repeat("b", 64), "status": "locked", "generation": float64(2),
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
				Version: "v1.0.0", SHA256: strings.Repeat("b", 64),
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
				{ID: "CONTRACTS-039", Path: "docs/dev/contracts/CONTRACTS-039.md", Version: "v1.0.0", SHA256: strings.Repeat("c", 64)},
				{ID: "BE-039", Path: "docs/dev/contracts/BE-039.md", Version: "v1.0.0", SHA256: strings.Repeat("d", 64)},
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
