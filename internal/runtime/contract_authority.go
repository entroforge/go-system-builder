package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// CurrentContractBinding derives only the original Main cursor. Conditional
// source closure is resolved by the composition layer before issuance.
func CurrentContractBinding(state map[string]any) (workpacket.PlanningBinding, []workpacket.Reference, error) {
	if !PacketEnabled(state) {
		return workpacket.PlanningBinding{}, nil, fmt.Errorf("explicit packet mode required")
	}
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "contracts" {
		return workpacket.PlanningBinding{}, nil, fmt.Errorf("S3 requires original planning.contracts cursor")
	}
	// Reuse the actual REQ binding projection through a temporary cursor;
	// no Runtime bytes or lifecycle facts are changed.
	projection := map[string]any{}
	for k, v := range state {
		projection[k] = v
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": life["phase_revision"]}
	design, err := CurrentDesignPacket(projection)
	if err != nil {
		return workpacket.PlanningBinding{}, nil, err
	}
	binding := design.Binding
	binding.Stage = "S3"
	designs := []workpacket.Reference{}
	rows, _ := state["documents"].([]any)
	seen := map[string]bool{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "design" || strings.HasPrefix(stringValue(row["id"]), "shared-model:") {
			continue
		}
		generation, err := integerField(row, "generation")
		if err != nil {
			return binding, nil, fmt.Errorf("registered design generation missing")
		}
		if generation != binding.Generation {
			continue
		}
		if row["status"] != "locked" {
			return binding, nil, fmt.Errorf("current registered design is not locked")
		}
		ref := workpacket.Reference{ID: stringValue(row["id"]), Path: stringValue(row["path"]), Version: stringValue(row["version"])}
		if seen[ref.Path] {
			return binding, nil, fmt.Errorf("duplicate original design path")
		}
		seen[ref.Path] = true
		designs = append(designs, ref)
	}
	if len(designs) == 0 {
		return binding, nil, fmt.Errorf("S3 has no original registered current design")
	}
	sort.Slice(designs, func(i, j int) bool { return designs[i].Path < designs[j].Path })
	return binding, designs, nil
}

// ValidateContractPacket checks supplied frozen inputs against original facts.
// It does not assert that a conditional source resolver is complete and grants
// no issuance/reception authority by itself.
func ValidateContractPacket(root string, state map[string]any, p workpacket.ContractPacket, files fileview.Reader) error {
	binding, _, err := CurrentContractBinding(state)
	if err != nil {
		return err
	}
	if !samePlanningResponsibility(binding, p.Binding) {
		return fmt.Errorf("S3 questionnaire does not belong to the current responsibility")
	}
	return nil
}
