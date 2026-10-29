package controller

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type planningSubmissionPacket struct {
	id       string
	template string
	binding  workpacket.PlanningBinding
	value    any
}

// currentPlanningFollowup resolves the one accepted Main submission that must
// accompany a packet-mode S2/S3/S4 automatic transition. The caller passes the
// same snapshot used for gate evaluation; transition.Apply rechecks the
// returned followup under its original Writer lock before stamping it.
func currentPlanningFollowup(root string, state map[string]any, candidate *transition.TransitionSpec) (bool, string, error) {
	if !runtime.PacketEnabled(state) {
		return false, "", nil
	}
	phaseState, phase := snapshotCursor(state)
	if phaseState != "planning" {
		return false, "", nil
	}

	var (
		stage, templateID, transitionID string
		binding                         workpacket.PlanningBinding
		expectedDesign                  *workpacket.DesignPacket
	)
	switch phase {
	case "design":
		stage, templateID, transitionID = "S2", "s2-design-v1", "PTR-PLAN-01"
		var current struct {
			RuntimeID string `json:"runtime_id"`
			Baseline  struct {
				Generation int `json:"generation"`
			} `json:"baseline"`
			Lifecycle struct {
				Entry int `json:"phase_revision"`
			} `json:"lifecycle"`
		}
		data, err := json.Marshal(state)
		if err != nil || json.Unmarshal(data, &current) != nil {
			return true, "", fmt.Errorf("current S2 responsibility is unreadable")
		}
		binding = workpacket.PlanningBinding{RuntimeID: current.RuntimeID, Generation: current.Baseline.Generation, StageEntry: current.Lifecycle.Entry, Stage: "S2"}
	case "contracts":
		stage, templateID, transitionID = "S3", "s3-contract-v1", "PTR-PLAN-02"
		var err error
		binding, _, err = runtime.CurrentContractBinding(state)
		if err != nil {
			return true, "", fmt.Errorf("resolve current S3 Main binding: %w", err)
		}
	case "tasks":
		stage, templateID, transitionID = "S4", "s4-task-planning-v1", "TR-002"
		var err error
		binding, err = runtime.CurrentTaskPlanningBinding(state)
		if err != nil {
			return true, "", fmt.Errorf("resolve current S4 Main binding: %w", err)
		}
	default:
		return false, "", nil
	}
	if candidate == nil || candidate.ID != transitionID {
		got := "<none>"
		if candidate != nil {
			got = candidate.ID
		}
		return true, "", fmt.Errorf("%s automatic transition %s does not match the current planning route %s", stage, got, transitionID)
	}

	packet, err := findCurrentPlanningPacket(state, stage, templateID, binding, expectedDesign)
	if err != nil {
		return true, "", err
	}
	record, err := currentPlanningSubmission(state, packet.id, binding, phase, templateID)
	if err != nil {
		return true, "", err
	}
	if err := validatePlanningSubmissionEvidence(root, state, packet, record, binding); err != nil {
		return true, "", err
	}
	if _, err := runtime.SubmissionFollowup(state, record.FollowupID); err != nil {
		return true, "", fmt.Errorf("current %s followup is not retryable at this exact stage entry: %w", stage, err)
	}
	return true, record.FollowupID, nil
}

func findCurrentPlanningPacket(state map[string]any, stage, templateID string, binding workpacket.PlanningBinding, expectedDesign *workpacket.DesignPacket) (planningSubmissionPacket, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var matches []planningSubmissionPacket
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok || row["template_id"] != templateID {
			continue
		}
		id := stringValue(row["packet_id"])
		if id == "" {
			return planningSubmissionPacket{}, fmt.Errorf("current %s typed packet has no packet_id", stage)
		}
		packet := planningSubmissionPacket{id: id, template: templateID}
		switch stage {
		case "S2":
			p, found, err := runtime.FindDesignPacket(state, id)
			if err != nil || !found {
				return planningSubmissionPacket{}, fmt.Errorf("decode current S2 typed packet %s: %v", id, err)
			}
			packet.binding, packet.value = p.Binding, p
		case "S3":
			p, found, err := runtime.FindContractPacket(state, id)
			if err != nil || !found {
				return planningSubmissionPacket{}, fmt.Errorf("decode current S3 typed packet %s: %v", id, err)
			}
			packet.binding, packet.value = p.Binding, p
		case "S4":
			p, found, err := runtime.FindTaskPlanningPacket(state, id)
			if err != nil || !found {
				return planningSubmissionPacket{}, fmt.Errorf("decode current S4 typed packet %s: %v", id, err)
			}
			packet.binding, packet.value = p.Binding, p
		default:
			return planningSubmissionPacket{}, fmt.Errorf("unknown planning stage %q", stage)
		}
		if !samePlanningStageSlot(packet.binding, binding) {
			continue
		}
		if stage != "S2" && !reflect.DeepEqual(packet.binding, binding) {
			return planningSubmissionPacket{}, fmt.Errorf("current %s typed packet has a different owner or locked REQ binding", stage)
		}
		if stage != "S2" && expectedDesign != nil {
			actual, ok := packet.value.(workpacket.DesignPacket)
			if !ok || !reflect.DeepEqual(actual, *expectedDesign) {
				return planningSubmissionPacket{}, fmt.Errorf("current S2 packet differs from the original Main template projection")
			}
		}
		matches = append(matches, packet)
	}
	if len(matches) != 1 {
		return planningSubmissionPacket{}, fmt.Errorf("current %s Main typed packet is missing or ambiguous (%d exact stage-entry packets)", stage, len(matches))
	}
	allIDCount := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["packet_id"] == matches[0].id {
			allIDCount++
		}
	}
	if allIDCount != 1 {
		return planningSubmissionPacket{}, fmt.Errorf("current %s packet identity is duplicated across Runtime subtypes", stage)
	}
	return matches[0], nil
}

func samePlanningStageSlot(actual, current workpacket.PlanningBinding) bool {
	return actual.RuntimeID == current.RuntimeID && actual.Generation == current.Generation && actual.StageEntry == current.StageEntry && actual.Stage == current.Stage
}

func currentPlanningSubmission(state map[string]any, packetID string, binding workpacket.PlanningBinding, phase, templateID string) (runtime.SubmissionRecord, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	count := 0
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if ok && row["packet_id"] == packetID {
			count++
		}
	}
	if count != 1 {
		return runtime.SubmissionRecord{}, fmt.Errorf("current %s packet has %d submission rows; require exactly one", binding.Stage, count)
	}
	record, found, err := runtime.FindSubmission(state, packetID)
	if err != nil || !found {
		return runtime.SubmissionRecord{}, fmt.Errorf("current %s submission is unreadable: %v", binding.Stage, err)
	}
	if binding.Stage == "S2" {
		if record.PacketID != packetID || record.FollowupID == "" || record.EvidenceID == "" || record.OriginState != "planning" || record.OriginPhase != "design" || record.OriginEntry != binding.StageEntry || (record.ControlStatus != "pending" && record.ControlStatus != "no_transition") {
			return runtime.SubmissionRecord{}, fmt.Errorf("current S2 report is not pending at this design entry")
		}
		if _, err := runtime.SubmissionFollowup(state, record.FollowupID); err != nil {
			return record, err
		}
		return record, nil
	}
	key := workpacket.Digest([]byte("submission:" + packetID))
	retryable := record.ControlStatus == "pending" || ((binding.Stage == "S3" || binding.Stage == "S4") && record.ControlStatus == "no_transition")
	if record.Key != key || record.PacketID != packetID || record.Owner != "Main" || record.FollowupID != workpacket.Digest([]byte(key+":after-domain")) || record.ContentHash == "" || record.EvidenceID == "" || !retryable || record.OriginState != "planning" || record.OriginPhase != phase || record.OriginEntry != binding.StageEntry || record.RevisionPacketID != "" || record.SupersededBy != "" {
		return runtime.SubmissionRecord{}, fmt.Errorf("current %s submission does not bind its accepted Main packet and pending original entry", binding.Stage)
	}
	// Confirm the deterministic followup has exactly one row across the shared
	// submission index. Do not let a duplicate event ID select another packet.
	followupRows := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["followup_event_id"] == record.FollowupID {
			followupRows++
		}
	}
	if followupRows != 1 {
		return runtime.SubmissionRecord{}, fmt.Errorf("current %s followup identity is duplicated", binding.Stage)
	}
	// Guard against a packet ID being routed through a different packet subtype.
	if expected := map[string]string{"S2": "s2-design-v1", "S3": "s3-contract-v1", "S4": "s4-task-planning-v1"}[binding.Stage]; expected != templateID {
		return runtime.SubmissionRecord{}, fmt.Errorf("current planning submission subtype differs")
	}
	return record, nil
}

func validatePlanningSubmissionEvidence(root string, state map[string]any, packet planningSubmissionPacket, record runtime.SubmissionRecord, binding workpacket.PlanningBinding) error {
	if binding.Stage == "S2" {
		// The accepted record identifies the report. Its professional conclusion
		// and actual materials are read by the original design gate.
		return nil
	}
	evidenceRows, _ := state["evidence"].([]any)
	var evidence map[string]any
	count := 0
	for _, raw := range evidenceRows {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == record.EvidenceID {
			count++
			evidence = row
		}
	}
	if count != 1 || evidence == nil {
		return fmt.Errorf("current %s submission does not have one exact accepted evidence row", binding.Stage)
	}
	wantKind := map[string]string{"S2": "planning_design", "S3": "planning_contract", "S4": "planning_task"}[binding.Stage]
	producers, _ := evidence["produced_by"].([]any)
	genOK := integerValueEquals(evidence["baseline_generation"], binding.Generation)
	if evidence["status"] != "valid" || evidence["kind"] != wantKind || evidence["responsibility_id"] != "Orchestrator" || !genOK || len(producers) != 1 || producers[0] != "Main" || evidence["invalidated_by"] != nil || evidence["invalidation_rule"] != nil || evidence["invalidation_reason"] != nil || evidence["review_round"] != nil {
		return fmt.Errorf("current %s evidence differs from the accepted Main planning responsibility", binding.Stage)
	}
	path, sha := stringValue(evidence["path"]), stringValue(evidence["sha256"])
	if path == "" || sha == "" {
		return fmt.Errorf("current %s evidence path/hash is incomplete", binding.Stage)
	}
	data, err := workpacket.ReadFile(root, path, 1<<20)
	if err != nil || workpacket.Digest(data) != sha {
		return fmt.Errorf("current %s accepted evidence bytes differ from its registered digest", binding.Stage)
	}
	switch p := packet.value.(type) {
	case workpacket.ContractPacket:
		var envelope struct {
			Answer workpacket.ContractAnswer `json:"contract_answer"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("current S3 accepted evidence envelope is malformed")
		}
		if err := runtime.ValidateContractEnvelope(p, record.EvidenceID, envelope.Answer, data); err != nil {
			return fmt.Errorf("current S3 accepted evidence does not match its packet: %w", err)
		}
		digest, err := workpacket.ContractAnswerDigest(envelope.Answer)
		if err != nil || digest != record.ContentHash {
			return fmt.Errorf("current S3 accepted answer digest differs from its submission")
		}
	case workpacket.TaskPlanningPacket:
		var envelope struct {
			Answer workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("current S4 accepted evidence envelope is malformed")
		}
		if err := runtime.ValidateTaskPlanningEnvelope(p, record.EvidenceID, envelope.Answer, data); err != nil {
			return fmt.Errorf("current S4 accepted evidence does not match its packet: %w", err)
		}
		digest, err := workpacket.TaskPlanningAnswerDigest(envelope.Answer)
		if err != nil || digest != record.ContentHash {
			return fmt.Errorf("current S4 accepted answer digest differs from its submission")
		}
	default:
		return fmt.Errorf("current planning packet has an unsupported typed value")
	}
	return nil
}

func integerValueEquals(value any, want int) bool {
	switch actual := value.(type) {
	case float64:
		return actual == float64(want)
	case int:
		return actual == want
	case int64:
		return actual == int64(want)
	default:
		return false
	}
}
