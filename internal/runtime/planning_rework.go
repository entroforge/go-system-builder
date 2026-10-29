package runtime

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// PlanningReworkReplay reads the original journal anchor, including rotated
// segments, rather than inferring a request from published packet files.
func PlanningReworkReplay(state map[string]any, journalPath, requestID, target string) (bool, error) {
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
	return j.Events[index]["request_id"] == requestID, nil
}

// ApplyPlanningRework invalidates dependent projections in the original
// transition Writer transaction. Packet and accepted submission history remain.
func ApplyPlanningRework(root string, state map[string]any, target, reason, id string, files fileview.Reader) error {
	bound, _ := state["bound_req"].(map[string]any)
	if bound["status"] != "locked" || files == nil {
		return fmt.Errorf("planning rework requires the original locked REQ and authoritative source view")
	}
	data, err := files.ReadFile(stringValue(bound["path"]))
	if err != nil || workpacket.Digest(data) != bound["sha256"] {
		return fmt.Errorf("locked REQ changed; use the original requirement-change boundary, not planning rework")
	}
	disk, err := workpacket.ReadFile(root, stringValue(bound["path"]), 4<<20)
	if err != nil || workpacket.Digest(disk) != bound["sha256"] {
		return fmt.Errorf("locked REQ working bytes changed; use the original requirement-change boundary")
	}
	if verify, ok := files.(interface{ Verify() error }); ok {
		if err := verify.Verify(); err != nil {
			return err
		}
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

func validateReworkFrozenDesign(root string, state map[string]any, files fileview.Reader) error {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	life, _ := state["lifecycle"].(map[string]any)
	entry, err := integerField(life, "phase_revision")
	if err != nil {
		return err
	}
	var latest *workpacket.ContractPacket
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		p, found, err := FindContractPacket(state, stringValue(row["packet_id"]))
		if err != nil {
			return err
		}
		if !found || p.Binding.Generation != baselineGeneration(state) || p.Binding.RuntimeID != state["runtime_id"] || p.Binding.StageEntry >= entry {
			continue
		}
		if latest == nil || latest.Binding.StageEntry < p.Binding.StageEntry {
			copy := p
			latest = &copy
		}
	}
	if latest == nil {
		return fmt.Errorf("original S3 issuance is required before replacing contracts")
	}
	projection := make(map[string]any, len(state))
	for k, v := range state {
		projection[k] = v
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "contracts", "phase_revision": latest.Binding.StageEntry}
	return ValidateContractPacket(root, projection, *latest, files)
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
