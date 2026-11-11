package runtime

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// PlanningReworkReplay reads the original journal anchor, including rotated
// segments, rather than inferring a request from published packet files.
func PlanningReworkReplay(state map[string]any, journalPath, reason, target string) (bool, error) {
	life, _ := state["lifecycle"].(map[string]any)
	last, _ := state["last_transition"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != target || last["event"] != "planning_professional_rework" {
		return false, nil
	}
	// A new accepted professional delivery ends the retry window. Main may
	// then request another correction for the same professional reason.
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	entry, _ := integerField(life, "phase_revision")
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		origin, _ := integerField(row, "origin_entry")
		if row["origin_state"] == "planning" && row["origin_phase"] == target && origin == entry {
			return false, nil
		}
	}
	j, err := inspectJournal(journalPath)
	if err != nil {
		return false, err
	}
	index, ok := j.EventIndex[stringValue(last["event_id"])]
	if !ok {
		return false, fmt.Errorf("planning rework journal anchor missing")
	}
	// The transition action already persisted the actual professional reason.
	// Compare that fact; the original request ID remains opaque, including
	// historically hash-shaped IDs.
	actions, _ := j.Events[index]["action_results"].([]any)
	for _, raw := range actions {
		action, _ := raw.(map[string]any)
		if action["id"] == "planning_professional_rework" && action["result"] == "committed" {
			return strings.TrimSpace(stringValue(action["detail"])) == strings.TrimSpace(reason), nil
		}
	}
	return false, nil
}

// ApplyPlanningRework invalidates dependent projections in the original
// transition Writer transaction. Packet and accepted submission history remain.
func ApplyPlanningRework(root string, state map[string]any, target, reason, id string, files fileview.Reader) error {
	bound, _ := state["bound_req"].(map[string]any)
	if bound["status"] != "locked" || files == nil {
		return fmt.Errorf("planning rework requires the original locked REQ and authoritative source view")
	}
	if _, err := files.ReadFile(stringValue(bound["path"])); err != nil {
		return fmt.Errorf("original REQ unavailable: %w", err)
	}
	if target == "contracts" {
		if err := validateReworkFrozenDesign(root, state, files); err != nil {
			return fmt.Errorf("frozen S2 inputs changed or unavailable: %w; use --target design for substantive S2 correction", err)
		}
	}
	if target == "tasks" {
		if err := validateReworkFrozenTaskInputs(root, state, files); err != nil {
			return fmt.Errorf("frozen S4 inputs changed or unavailable: %w; use the original design/contracts rework boundary for upstream corrections", err)
		}
	}
	generation := baselineGeneration(state)
	docs, _ := state["documents"].([]any)
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		g, _ := integerField(d, "generation")
		kind := stringValue(d["kind"])
		if g == generation && (kind == "task" || kind == "dispatch_plan" || (target != "tasks" && (kind == "contract" || (kind == "design" && strings.HasPrefix(stringValue(d["id"]), "shared-model:")))) || (target == "design" && (kind == "design" || kind == "ui_baseline" || kind == "ui_prototype"))) {
			d["status"] = "draft"
		}
	}
	evidence, _ := state["evidence"].([]any)
	for _, raw := range evidence {
		e, _ := raw.(map[string]any)
		g, _ := integerField(e, "baseline_generation")
		kind := stringValue(e["kind"])
		if g != generation || e["status"] != "valid" || ((kind == "planning_design" || kind == "planning_design_record") && target != "design") {
			continue
		}
		if kind == "planning_task" || kind == "planning_task_record" || kind == "document_review" || kind == "document_review_record" || (target != "tasks" && (kind == "planning_design" || kind == "planning_design_record" || kind == "planning_contract" || kind == "planning_contract_record")) {
			e["status"], e["invalidated_by"], e["invalidation_rule"], e["invalidation_reason"] = "invalid", id, "planning_rework", reason
		}
	}
	return nil
}

// Validate the original S3 responsibility before opening a replacement
// contracts entry. The frozen source is the responsibility whose recorded
// reception actually advanced the original S3->S4 continuation; a packet
// that was merely issued carries no reception and is therefore not a source.
func validateReworkFrozenDesign(root string, state map[string]any, files fileview.Reader) error {
	life, _ := state["lifecycle"].(map[string]any)
	entry, err := integerField(life, "phase_revision")
	if err != nil {
		return err
	}
	latest, err := originalS3Responsibility(state, entry)
	if err != nil {
		return err
	}
	projection := make(map[string]any, len(state))
	for k, v := range state {
		projection[k] = v
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": latest.Binding.StageEntry}
	return ValidateContractPacket(root, projection, latest, files)
}

// originalS3Responsibility resolves the S3 responsibility this rework
// replaces from the recorded reception facts of the original planning chain.
// The comparison is the original monotonic stage entry together with the
// established continuation relation; an issued but never received packet is
// not a responsibility and an unresolvable newer reception is an explicit gap
// that must not be answered with an older one.
func originalS3Responsibility(state map[string]any, entry int) (workpacket.ContractPacket, error) {
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	submissions, _ := entities["submissions"].([]any)
	generation := baselineGeneration(state)
	runtimeID := stringValue(state["runtime_id"])
	bound, _ := state["bound_req"].(map[string]any)
	requirement := workpacket.Reference{ID: stringValue(bound["id"]), Path: stringValue(bound["path"]), Version: stringValue(bound["version"])}

	// The S3 responsibilities this chain actually owns. The index only tells
	// which entries carry a responsibility; it never selects the source.
	packetCounts := map[int]int{}
	seenPacketIDs := map[string]bool{}
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		id := stringValue(row["packet_id"])
		if id == "" || seenPacketIDs[id] {
			return workpacket.ContractPacket{}, fmt.Errorf("original S3 packet identity is empty or duplicated")
		}
		seenPacketIDs[id] = true
		p, found, err := FindContractPacket(state, id)
		if err != nil {
			return workpacket.ContractPacket{}, err
		}
		if !found {
			return workpacket.ContractPacket{}, fmt.Errorf("original S3 packet %s cannot be decoded", id)
		}
		if p.Binding.RuntimeID != runtimeID || p.Binding.Generation != generation ||
			p.Binding.Requirement != requirement || p.Binding.Stage != "S3" || p.Binding.StageEntry >= entry {
			continue
		}
		packetCounts[p.Binding.StageEntry]++
	}

	// The recorded S3 receptions of this chain, located on the original
	// submission index rather than on the packet index.
	type s3Reception struct {
		entry    int
		packetID string
	}
	receptions := make([]s3Reception, 0, len(submissions))
	receptionCounts := map[int]int{}
	for _, raw := range submissions {
		row, _ := raw.(map[string]any)
		origin, err := integerField(row, "origin_entry")
		if err != nil || row["origin_state"] != "planning" || row["origin_phase"] != "contracts" || origin >= entry {
			continue
		}
		receptions = append(receptions, s3Reception{entry: origin, packetID: stringValue(row["packet_id"])})
		receptionCounts[origin]++
	}

	// The source is the most recent S3 stage entry whose reception advanced
	// the original continuation.
	source := -1
	var selected SubmissionRecord
	for _, reception := range receptions {
		record, found, err := FindSubmission(state, reception.packetID)
		if err != nil {
			return workpacket.ContractPacket{}, err
		}
		if !found || record.ControlStatus != "advanced" || record.TransitionEventID == "" ||
			record.RevisionPacketID != "" || record.SupersededBy != "" ||
			record.Owner != "Main" || record.PacketID != reception.packetID ||
			record.OriginState != "planning" || record.OriginPhase != "contracts" ||
			record.OriginEntry != reception.entry {
			continue
		}
		if reception.entry > source {
			source, selected = reception.entry, record
		}
	}

	// A reception newer than the selected source must still resolve its own
	// packet. An unresolvable newer reception is a gap in the original index:
	// it cannot be answered with an earlier responsibility.
	for _, reception := range receptions {
		if reception.entry <= source {
			continue
		}
		if _, found, err := FindContractPacket(state, reception.packetID); err != nil {
			return workpacket.ContractPacket{}, err
		} else if !found {
			return workpacket.ContractPacket{}, fmt.Errorf("original S3 reception at entry %d lacks its matching source packet %q", reception.entry, reception.packetID)
		}
	}
	if source < 0 {
		return workpacket.ContractPacket{}, fmt.Errorf("no original S3 reception advanced the S3 continuation before entry %d", entry)
	}
	if packetCounts[source] == 0 {
		return workpacket.ContractPacket{}, fmt.Errorf("original S3 reception at entry %d lacks its matching source packet %q", source, selected.PacketID)
	}
	if packetCounts[source] != 1 {
		return workpacket.ContractPacket{}, fmt.Errorf("original S3 responsibility is duplicated at entry %d", source)
	}
	if receptionCounts[source] != 1 {
		return workpacket.ContractPacket{}, fmt.Errorf("original S3 reception is duplicated at entry %d", source)
	}
	p, found, err := FindContractPacket(state, selected.PacketID)
	if err != nil {
		return workpacket.ContractPacket{}, err
	}
	if !found {
		return workpacket.ContractPacket{}, fmt.Errorf("original S3 reception at entry %d lacks its matching source packet %q", source, selected.PacketID)
	}
	// The reception and the packet must describe the same original
	// responsibility: this runtime, this generation, this locked REQ and the
	// stage entry the recorded transition advanced.
	if p.Binding.RuntimeID != runtimeID || p.Binding.Generation != generation ||
		p.Binding.Stage != "S3" || p.Binding.Owner != "Main" ||
		p.Binding.StageEntry != source || p.Binding.Requirement != requirement ||
		selected.OriginState != "planning" || selected.OriginPhase != "contracts" ||
		selected.OriginEntry != source || selected.Owner != "Main" ||
		selected.PacketID != p.ID || selected.ControlStatus != "advanced" ||
		selected.TransitionEventID == "" || selected.RevisionPacketID != "" || selected.SupersededBy != "" {
		return workpacket.ContractPacket{}, fmt.Errorf("original S3 reception/transition association is incomplete at entry %d", source)
	}
	if countContractSubmissions(state, p.ID) != 1 {
		return workpacket.ContractPacket{}, fmt.Errorf("original S3 reception identity is duplicated at entry %d", source)
	}
	return p, nil
}

// Validate the original current S4 issuance before opening a replacement. TASK
// and dispatch outputs are mutable; the original packet only freezes upstream
// formal sources and contracts. Historical method descriptions do not gate
// reception; the replacement issuer freezes the current method bytes afresh.
func validateReworkFrozenTaskInputs(root string, state map[string]any, files fileview.Reader) error {
	binding, err := CurrentTaskPlanningBinding(state)
	if err != nil {
		return err
	}
	// The transition Writer has already incremented the phase cursor before
	// invoking actions. Verify the entry being replaced, not the new entry.
	binding.StageEntry--
	projection := make(map[string]any, len(state))
	for key, value := range state {
		projection[key] = value
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "tasks", "phase_revision": binding.StageEntry}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		p, found, err := FindTaskPlanningPacket(state, stringValue(row["packet_id"]))
		if err != nil {
			return err
		}
		if found && p.Binding == binding {
			return ValidateTaskPlanningReceptionSources(root, projection, p, files)
		}
	}
	return fmt.Errorf("original current S4 issuance is required before replacing tasks")
}
