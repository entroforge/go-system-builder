package cli

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// mainPlanningNext carries the Main-only normal continuation guidance for an
// explicit packet-mode S2/S3/S4 cursor. It is a projection of Runtime facts;
// it never issues a packet or reads authoring files.
type mainPlanningNext struct {
	Action      string
	Automation  string
	IssueAction bool
}

func isMainPlanningViewer(input policy.Input) bool {
	return hookReaderIsMain(input)
}

func mainPlanningNextForState(state map[string]any, stage string) *mainPlanningNext {
	if !runtime.PacketEnabled(state) {
		return nil
	}

	if stage == "S3" || stage == "S4" {
		row, err := registeredPlanningRow(state)
		if err != nil {
			return &mainPlanningNext{Action: stage + " current entry: " + err.Error(), Automation: "Read registered facts; no packet rebuild or source qualification during ordinary observation"}
		}
		if row == nil {
			return &mainPlanningNext{Action: stage + " current questionnaire is missing from its successful entry outlet; resolve the named preparation error and continue the original entry, without guessing paths or resubmitting the preceding report", Automation: "Ordinary Pre/Stop reads existing work and does not prepare or restore questionnaires"}
		}
		view, answer := stringValue(row["view_path"]), stringValue(row["answer_path"])
		if record, found, err := runtime.FindSubmission(state, stringValue(row["packet_id"])); err == nil && found {
			if record.ControlStatus == "pending" || record.ControlStatus == "no_transition" {
				return &mainPlanningNext{Action: stage + " professional answer already received; preserve the answer and resolve the named gap, then use runtime stage-answer recover for the original outlet; original answer: " + answer, Automation: "No duplicate report or ordinary-read progression"}
			}
			return &mainPlanningNext{Action: stage + " answer already received; continue the current responsibility without resubmitting: " + answer, Automation: "Read registered completion facts only"}
		}
		return &mainPlanningNext{Action: fmt.Sprintf("%s packet %s is current: read %s; fill only the answer professional fields in %s and save normally", stage, stringValue(row["packet_id"]), view, answer), Automation: "Ordinary Pre/Stop reads current facts; actual saves handle reception and the original stage outlet"}
	}

	var packetID, viewPath, answerPath, phase string
	var binding workpacket.PlanningBinding
	var found, conflicting bool
	switch stage {
	case "S2":
		phase = "design"
		var current struct {
			RuntimeID string `json:"runtime_id"`
			Baseline  struct {
				Generation int `json:"generation"`
			} `json:"baseline"`
			Lifecycle struct {
				Entry int `json:"phase_revision"`
			} `json:"lifecycle"`
		}
		data, _ := json.Marshal(state)
		if json.Unmarshal(data, &current) == nil {
			binding = workpacket.PlanningBinding{RuntimeID: current.RuntimeID, Generation: current.Baseline.Generation, StageEntry: current.Lifecycle.Entry}
			stored, issued, conflict := currentDesignPlanningPacket(state, workpacket.DesignPacket{Binding: binding})
			conflicting = conflict
			if issued {
				packetID, viewPath, answerPath, found = stored.ID, stored.ViewPath, stored.AnswerPath, true
			}
		}
	default:
		return nil
	}

	stageName := stage + " Main"
	if conflicting {
		return unverifiedPlanningRecord(stageName)
	}
	if !found {
		if stage == "S2" {
			return &mainPlanningNext{Action: "S2 questionnaire missing: the system design-entry preparation has not supplied the current answer path; report this preparation gap", Automation: "Ordinary Pre/Stop only reads existing facts; it does not prepare or restore the design questionnaire"}
		}
		return &mainPlanningNext{
			Action:      fmt.Sprintf("%s questionnaire is being prepared by the Hook listener; use the exact answer path in its delivery. If preparation fails, Main resolves the named missing input", stageName),
			Automation:  fmt.Sprintf("The Hook prepares the current %s questionnaire automatically and preserves saved answers; the author does not run issue or consume", stage),
			IssueAction: false,
		}
	}

	if submission, received, err := runtime.FindSubmission(state, packetID); err != nil {
		return unverifiedPlanningRecord(stageName)
	} else if received {
		if (stage != "S2" && submission.Owner != "Main") || submission.PacketID != packetID || submission.OriginState != "planning" || submission.OriginPhase != phase || submission.OriginEntry != binding.StageEntry {
			return unverifiedPlanningRecord(stageName)
		}
		if stage == "S2" && (submission.ControlStatus == "pending" || submission.ControlStatus == "no_transition") {
			return &mainPlanningNext{Action: fmt.Sprintf("S2 answer already received at %s; resolve the reported material gaps, preserving the original answer; an interrupted outlet can use runtime stage-answer recover", answerPath), Automation: "Reception is recorded; no new issue or registration is needed"}
		}
		return &mainPlanningNext{
			Action:     fmt.Sprintf("%s answer for the current Main packet was already received; do not edit or resubmit it, and continue from the existing gate result", stageName),
			Automation: fmt.Sprintf("The current %s Main submission is already registered in Runtime; leave its answer unchanged and let the original gate/CAS determine the next cursor", stage),
		}
	}

	if stage == "S2" {
		return &mainPlanningNext{Action: fmt.Sprintf("S2 packet %s is current: read %s, fill only the answer professional fields and actual architecture path in %s; leave conclusion empty while work is incomplete", packetID, viewPath, answerPath), Automation: "Save the existing questionnaire normally; Pre/Stop reads facts without preparing or restoring files. Missing materials remain actual design work"}
	}
	return &mainPlanningNext{
		Action:      fmt.Sprintf("%s packet %s is current: read the exact frozen view %s, fill only the answer and actual artifact paths in %s, and save it through the normal file hook for the original receiver", stageName, packetID, viewPath, answerPath),
		Automation:  fmt.Sprintf("Fill the current %s questionnaire at the exact answer path above and save; the Hook restores missing views automatically while preserving saved answers", stage),
		IssueAction: false,
	}
}

func unverifiedPlanningRecord(stage string) *mainPlanningNext {
	return &mainPlanningNext{
		Action:     fmt.Sprintf("Runtime has a %s packet or submission whose current binding cannot be confirmed; do not edit an answer file or issue another packet, and use the original Runtime recovery path", stage),
		Automation: "Runtime contains a planning record whose current REQ, owner, or stage-entry binding cannot be confirmed; no authoring path or issue action is projected",
	}
}

func currentDesignPlanningPacket(state map[string]any, expected workpacket.DesignPacket) (workpacket.DesignPacket, bool, bool) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var stored workpacket.DesignPacket
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s2-design-v1" {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			continue
		}
		var packet workpacket.DesignPacket
		if json.Unmarshal(data, &packet) != nil {
			continue
		}
		if !samePlanningSlot(packet.Binding, expected.Binding) {
			continue
		}
		count++
		stored = packet
	}
	return stored, count == 1, count > 1
}

func samePlanningSlot(left, right workpacket.PlanningBinding) bool {
	return left.RuntimeID == right.RuntimeID && left.Generation == right.Generation && left.StageEntry == right.StageEntry
}

func currentContractPlanningPacket(state map[string]any, binding workpacket.PlanningBinding, designs []workpacket.Reference) (workpacket.ContractPacket, bool, bool) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var candidateIDs []string
	slotSeen := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			continue
		}
		var envelope struct {
			Binding workpacket.PlanningBinding `json:"binding"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			return workpacket.ContractPacket{}, false, true
		}
		if !samePlanningSlot(envelope.Binding, binding) {
			continue
		}
		slotSeen = true
		var packet workpacket.ContractPacket
		if json.Unmarshal(data, &packet) != nil || !samePlanningSlot(packet.Binding, binding) {
			return workpacket.ContractPacket{}, false, true
		}
		if packet.Binding != binding || len(packet.Inputs.Designs) != len(designs) || packet.ID == "" {
			return workpacket.ContractPacket{}, false, true
		}
		for i, design := range designs {
			actual := packet.Inputs.Designs[i]
			if design.ID != actual.ID || design.Path != actual.Path || design.Version != actual.Version {
				return workpacket.ContractPacket{}, false, true
			}
		}
		candidateIDs = append(candidateIDs, packet.ID)
	}
	if len(candidateIDs) != 1 {
		return workpacket.ContractPacket{}, false, slotSeen
	}
	packet, found, err := runtime.FindContractPacket(state, candidateIDs[0])
	if err != nil || !found {
		return workpacket.ContractPacket{}, false, true
	}
	expected, err := workpacket.BuildContracts(binding, packet.Inputs)
	if err != nil || !reflect.DeepEqual(expected, packet) {
		return workpacket.ContractPacket{}, false, true
	}
	return packet, true, false
}

func currentTaskPlanningPacket(state map[string]any, binding workpacket.PlanningBinding) (workpacket.TaskPlanningPacket, bool, bool) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var candidateIDs []string
	slotSeen := false
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			continue
		}
		var envelope struct {
			Binding workpacket.PlanningBinding `json:"binding"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			return workpacket.TaskPlanningPacket{}, false, true
		}
		if !samePlanningSlot(envelope.Binding, binding) {
			continue
		}
		slotSeen = true
		var packet workpacket.TaskPlanningPacket
		if json.Unmarshal(data, &packet) != nil || !samePlanningSlot(packet.Binding, binding) {
			return workpacket.TaskPlanningPacket{}, false, true
		}
		if packet.Binding != binding || packet.ID == "" {
			return workpacket.TaskPlanningPacket{}, false, true
		}
		candidateIDs = append(candidateIDs, packet.ID)
	}
	if len(candidateIDs) != 1 {
		return workpacket.TaskPlanningPacket{}, false, slotSeen
	}
	packet, found, err := runtime.FindTaskPlanningPacket(state, candidateIDs[0])
	if err != nil || !found {
		return workpacket.TaskPlanningPacket{}, false, true
	}
	expected, err := workpacket.BuildTaskPlanningWithTemplate(binding, packet.Inputs, packet.TemplateSHA)
	if err != nil || !reflect.DeepEqual(expected, packet) {
		return workpacket.TaskPlanningPacket{}, false, true
	}
	return packet, true, false
}
