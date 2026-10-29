package hookctx

import (
	"encoding/json"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// currentMainPlanningAnswerPaths projects only the current, unsubmitted typed
// planning answer for the actual Main Hook identity. A Worker identity, even
// one whose assignment is absent from this Runtime, never falls back to Main.
func currentMainPlanningAnswerPaths(state map[string]any, agentID string, agent *policy.AgentContext) []string {
	if agentID != "" || agent != nil || !runtime.PacketEnabled(state) {
		return nil
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" {
		return nil
	}

	var path string
	switch lifecycle["phase"] {
	case "design":
		path = currentMainDesignAnswerPath(state)
	case "contracts":
		path = currentMainContractAnswerPath(state)
	case "tasks":
		path = currentMainTaskAnswerPath(state)
	default:
		return nil
	}
	if path == "" {
		return nil
	}
	return []string{path}
}

func currentMainDesignAnswerPath(state map[string]any) string {
	expected, err := runtime.CurrentDesignPacket(state)
	if err != nil || expected.Binding.Owner != "Main" {
		return ""
	}
	rows, ok := planningWorkPacketRows(state)
	if !ok {
		return ""
	}
	count := 0
	for _, row := range rows {
		if row["template_id"] != expected.TemplateID {
			continue
		}
		binding, valid := planningRowBinding(row)
		if !valid {
			return ""
		}
		if !sameMainPlanningSlot(binding, expected.Binding) {
			continue
		}
		if binding != expected.Binding {
			return ""
		}
		packet, valid := decodeDesignPacket(row)
		if !valid || !reflect.DeepEqual(packet, expected) {
			return ""
		}
		count++
	}
	if count != 1 {
		return ""
	}
	if !uniquePlanningPacketID(rows, expected.ID) {
		return ""
	}
	stored, found, err := runtime.FindDesignPacket(state, expected.ID)
	if err != nil || !found || !reflect.DeepEqual(stored, expected) || hasPlanningSubmission(state, expected.ID) {
		return ""
	}
	return expected.AnswerPath
}

func currentMainContractAnswerPath(state map[string]any) string {
	binding, designs, err := runtime.CurrentContractBinding(state)
	if err != nil || binding.Owner != "Main" {
		return ""
	}
	rows, ok := planningWorkPacketRows(state)
	if !ok {
		return ""
	}
	var candidate workpacket.ContractPacket
	count := 0
	for _, row := range rows {
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		rowBinding, valid := planningRowBinding(row)
		if !valid {
			return ""
		}
		if !sameMainPlanningSlot(rowBinding, binding) {
			continue
		}
		if rowBinding != binding {
			return ""
		}
		packet, valid := decodeContractPacket(row)
		if !valid || packet.Binding != binding || packet.ID == "" || !reflect.DeepEqual(packet.Inputs.Designs, designs) {
			return ""
		}
		expected, err := workpacket.BuildContracts(binding, packet.Inputs)
		if err != nil || !reflect.DeepEqual(expected, packet) {
			return ""
		}
		candidate = packet
		count++
	}
	if count != 1 {
		return ""
	}
	if !uniquePlanningPacketID(rows, candidate.ID) {
		return ""
	}
	stored, found, err := runtime.FindContractPacket(state, candidate.ID)
	if err != nil || !found || !reflect.DeepEqual(stored, candidate) || hasPlanningSubmission(state, candidate.ID) {
		return ""
	}
	return candidate.AnswerPath
}

func currentMainTaskAnswerPath(state map[string]any) string {
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil || binding.Owner != "Main" {
		return ""
	}
	rows, ok := planningWorkPacketRows(state)
	if !ok {
		return ""
	}
	var candidate workpacket.TaskPlanningPacket
	count := 0
	for _, row := range rows {
		if row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		rowBinding, valid := planningRowBinding(row)
		if !valid {
			return ""
		}
		if !sameMainPlanningSlot(rowBinding, binding) {
			continue
		}
		if rowBinding != binding {
			return ""
		}
		packet, valid := decodeTaskPlanningPacket(row)
		if !valid || packet.Binding != binding || packet.ID == "" {
			return ""
		}
		expected, err := workpacket.BuildTaskPlanningWithTemplate(binding, packet.Inputs, packet.TemplateSHA)
		if err != nil || !reflect.DeepEqual(expected, packet) {
			return ""
		}
		candidate = packet
		count++
	}
	if count != 1 {
		return ""
	}
	if !uniquePlanningPacketID(rows, candidate.ID) {
		return ""
	}
	stored, found, err := runtime.FindTaskPlanningPacket(state, candidate.ID)
	if err != nil || !found || !reflect.DeepEqual(stored, candidate) || hasPlanningSubmission(state, candidate.ID) {
		return ""
	}
	return candidate.AnswerPath
}

func planningWorkPacketRows(state map[string]any) ([]map[string]any, bool) {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return nil, false
	}
	rawRows, ok := entities["work_packets"].([]any)
	if !ok {
		return nil, false
	}
	rows := make([]map[string]any, 0, len(rawRows))
	for _, raw := range rawRows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, false
		}
		rows = append(rows, row)
	}
	return rows, true
}

func planningRowBinding(row map[string]any) (workpacket.PlanningBinding, bool) {
	data, err := json.Marshal(row)
	if err != nil {
		return workpacket.PlanningBinding{}, false
	}
	var envelope struct {
		Binding workpacket.PlanningBinding `json:"binding"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return workpacket.PlanningBinding{}, false
	}
	return envelope.Binding, true
}

func decodeDesignPacket(row map[string]any) (workpacket.DesignPacket, bool) {
	data, err := json.Marshal(row)
	if err != nil {
		return workpacket.DesignPacket{}, false
	}
	var packet workpacket.DesignPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		return workpacket.DesignPacket{}, false
	}
	return packet, true
}

func decodeContractPacket(row map[string]any) (workpacket.ContractPacket, bool) {
	data, err := json.Marshal(row)
	if err != nil {
		return workpacket.ContractPacket{}, false
	}
	var packet workpacket.ContractPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		return workpacket.ContractPacket{}, false
	}
	return packet, true
}

func decodeTaskPlanningPacket(row map[string]any) (workpacket.TaskPlanningPacket, bool) {
	data, err := json.Marshal(row)
	if err != nil {
		return workpacket.TaskPlanningPacket{}, false
	}
	var packet workpacket.TaskPlanningPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		return workpacket.TaskPlanningPacket{}, false
	}
	return packet, true
}

func sameMainPlanningSlot(left, right workpacket.PlanningBinding) bool {
	return left.RuntimeID == right.RuntimeID && left.Generation == right.Generation && left.StageEntry == right.StageEntry
}

func uniquePlanningPacketID(rows []map[string]any, packetID string) bool {
	count := 0
	for _, row := range rows {
		if row["packet_id"] == packetID {
			count++
		}
	}
	return packetID != "" && count == 1
}

func hasPlanningSubmission(state map[string]any, packetID string) bool {
	_, found, err := runtime.FindSubmission(state, packetID)
	return err != nil || found
}
