package runtime

import (
	"fmt"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// CurrentDesignPacket projects an actual Main responsibility, not a workgroup.
// The original Writer retains its current-entry and atomic recording boundary.
func CurrentDesignBinding(state map[string]any) (workpacket.PlanningBinding, error) {
	if !PacketEnabled(state) {
		return workpacket.PlanningBinding{}, fmt.Errorf("explicit packet mode is required")
	}
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "design" {
		return workpacket.PlanningBinding{}, fmt.Errorf("S2 requires the original planning.design cursor")
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" {
		return workpacket.PlanningBinding{}, fmt.Errorf("S2 requires the authorized locked REQ")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return workpacket.PlanningBinding{}, err
	}
	entry, err := integerField(life, "phase_revision")
	if err != nil {
		return workpacket.PlanningBinding{}, err
	}
	return workpacket.PlanningBinding{RuntimeID: stringValue(state["runtime_id"]), Generation: generation, StageEntry: entry, Stage: "S2", Owner: "Main", Requirement: workpacket.Reference{ID: stringValue(bound["id"]), Path: stringValue(bound["path"]), Version: stringValue(bound["version"])}}, nil
}

// CurrentDesignPacket constructs a new S2 questionnaire only for first issuance.
func CurrentDesignPacket(state map[string]any) (workpacket.DesignPacket, error) {
	binding, err := CurrentDesignBinding(state)
	if err != nil {
		return workpacket.DesignPacket{}, err
	}
	return workpacket.BuildDesign(binding)
}

// ValidateDesignPacket associates the existing questionnaire with this entry.
// Its template and historical source bytes are not authoring qualifications.
func ValidateDesignPacket(root string, state map[string]any, p workpacket.DesignPacket, files fileview.Reader) error {
	life, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation, generationErr := integerField(baseline, "generation")
	entry, entryErr := integerField(life, "phase_revision")
	if generationErr != nil || entryErr != nil || state["runtime_id"] != p.Binding.RuntimeID || generation != p.Binding.Generation ||
		life["state"] != "planning" || life["phase"] != "design" || entry != p.Binding.StageEntry {
		return fmt.Errorf("design questionnaire does not belong to the current design responsibility")
	}
	return nil
}

// samePlanningResponsibility accepts original historical bindings without using
// the historic digest as an authorization or current-content check.
func samePlanningResponsibility(a, b workpacket.PlanningBinding) bool {
	return a.RuntimeID == b.RuntimeID && a.Generation == b.Generation && a.StageEntry == b.StageEntry && a.Stage == b.Stage && a.Owner == b.Owner && a.Requirement.ID == b.Requirement.ID && a.Requirement.Path == b.Requirement.Path && a.Requirement.Version == b.Requirement.Version
}
