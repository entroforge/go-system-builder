package runtime

import (
	"fmt"
	"path"
	"reflect"
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

// ValidateContractPacket checks the supplied frozen inputs against the
// original authority root, the original responsibility coordinates, the frozen
// input identity and the existence of the original sources.
// It computes, registers and verifies no revision, digest, fingerprint or
// checksum; historical hash-shaped fields are read only as recorded facts.
// It does not assert that a conditional source resolver is complete and grants
// no issuance/reception authority by itself.
func ValidateContractPacket(root string, state map[string]any, p workpacket.ContractPacket, files fileview.Reader) error {
	if files == nil {
		return fmt.Errorf("S3 requires original authoritative file view")
	}
	binding, designs, err := CurrentContractBinding(state)
	if err != nil {
		return err
	}
	// The current checkout must be the REQ-bound authority root; a copied
	// Runtime is never a second control plane.
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return err
	}
	if !samePlanningResponsibility(binding, p.Binding) {
		return fmt.Errorf("S3 questionnaire does not belong to the current responsibility")
	}
	// The packet must freeze the identity of the currently registered S2
	// designs (ID/Path/Version), never their historic bytes.
	if !sameFrozenDesignIdentity(designs, p.Inputs.Designs) {
		return fmt.Errorf("S3 frozen design identity differs from the currently registered S2 design")
	}
	// Rebuilding the packet in memory compares the original issuance identity
	// only. It writes no file and records no new fact.
	rebuilt, err := workpacket.BuildContracts(binding, p.Inputs)
	if err != nil {
		return err
	}
	if rebuilt.ID != p.ID || rebuilt.TemplateID != p.TemplateID || !samePlanningResponsibility(rebuilt.Binding, p.Binding) || !reflect.DeepEqual(rebuilt.Inputs, p.Inputs) {
		return fmt.Errorf("S3 contract packet identity differs from the original frozen issuance")
	}
	if !sameContractPublication(rebuilt, p, binding) {
		return fmt.Errorf("S3 contract packet publication differs from the original frozen issuance")
	}
	// The original registered sources must still exist and be readable in the
	// declared source view. Only existence and readability are judged; no
	// content is compared.
	for _, ref := range designs {
		if _, err := files.ReadFile(ref.Path); err != nil {
			return fmt.Errorf("S3 original registered design source is unavailable: %s", ref.Path)
		}
	}
	return nil
}

// sameFrozenDesignIdentity aligns the currently registered designs with the
// design references frozen inside the packet. It compares the identity of the
// original design registration only, never its historic content.
func sameFrozenDesignIdentity(current, frozen []workpacket.Reference) bool {
	if len(current) != len(frozen) {
		return false
	}
	registered := map[string]workpacket.Reference{}
	for _, ref := range current {
		registered[ref.Path] = ref
	}
	for _, ref := range frozen {
		other, ok := registered[ref.Path]
		if !ok || other.ID != ref.ID || other.Version != ref.Version {
			return false
		}
	}
	return true
}

// sameContractPublication reports whether the published view and answer paths
// belong to this issuance. The deterministic issuance publishes both under the
// packet's own base. When those paths are already occupied by another owner the
// issuer relocates both into a fresh candidate scope; that relocation is still
// an original publication of the same packet identity. Any other publication
// scope is not an original issuance of this packet.
func sameContractPublication(rebuilt, published workpacket.ContractPacket, binding workpacket.PlanningBinding) bool {
	if rebuilt.ViewPath == published.ViewPath && rebuilt.AnswerPath == published.AnswerPath {
		return true
	}
	view, ok := contractPublicationScope(path.Dir(published.ViewPath), ".claude/work-packets/")
	if !ok {
		return false
	}
	answer, ok := contractPublicationScope(strings.TrimSuffix(published.AnswerPath, "/Main/answer.json"), ".claude/submissions/")
	if !ok || !strings.HasSuffix(published.AnswerPath, "/Main/answer.json") || !strings.Contains(published.AnswerPath, "/"+binding.Stage+"/") {
		return false
	}
	// A candidate scope is a fresh publication directory. It never replaces the
	// deterministic scope of another issued packet.
	if strings.HasPrefix(view, binding.RuntimeID+"/") {
		return false
	}
	return answer == view || strings.HasPrefix(answer, view+"/")
}

// contractPublicationScope returns the publication scope of a published path
// inside its publication root.
func contractPublicationScope(published, root string) (string, bool) {
	if !strings.HasPrefix(published, root) {
		return "", false
	}
	scope := strings.TrimPrefix(published, root)
	if scope == "" || strings.Contains(scope, "..") {
		return "", false
	}
	return scope, true
}
