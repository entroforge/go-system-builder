package runtime

import (
	"fmt"
	"path/filepath"
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
		ref := workpacket.Reference{ID: stringValue(row["id"]), Path: stringValue(row["path"]), Version: stringValue(row["version"]), SHA256: stringValue(row["sha256"])}
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
	if files == nil {
		return fmt.Errorf("S3 requires original authoritative file view")
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return err
	}
	binding, designs, err := CurrentContractBinding(state)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(binding, p.Binding) || len(designs) != len(p.Inputs.Designs) {
		return fmt.Errorf("S3 frozen Main/design binding differs")
	}
	// Compare the S2 document association, not its historical content hash.
	for i, design := range designs {
		actual := p.Inputs.Designs[i]
		if design.ID != actual.ID || design.Path != actual.Path || design.Version != actual.Version {
			return fmt.Errorf("S3 Main/design document association differs")
		}
	}
	expected, err := workpacket.BuildContracts(binding, p.Inputs)
	if err != nil || !reflect.DeepEqual(expected, p) {
		return fmt.Errorf("S3 frozen template or inputs differ")
	}
	refs := []workpacket.Subject{{Path: binding.Requirement.Path, Version: binding.Requirement.Version, SHA256: binding.Requirement.SHA256}}
	for _, ref := range p.Inputs.Designs {
		refs = append(refs, workpacket.Subject{Path: ref.Path, Version: ref.Version, SHA256: ref.SHA256})
	}
	refs = append(refs, p.Inputs.Sources...)
	for _, ref := range refs {
		view, err := files.ReadFile(ref.Path)
		if err != nil {
			return err
		}
		disk, err := workpacket.ReadFile(root, ref.Path, 4<<20)
		if err != nil {
			return err
		}
		if workpacket.Digest(view) != ref.SHA256 || workpacket.Digest(disk) != ref.SHA256 {
			return fmt.Errorf("S3 frozen input drifted: %s", ref.Path)
		}
	}
	for dir, names := range p.Inputs.Directories {
		entries, err := files.ReadDir(dir)
		if err != nil {
			return err
		}
		actual := []string{}
		for _, entry := range entries {
			actual = append(actual, entry.Name())
		}
		sort.Strings(actual)
		if !reflect.DeepEqual(actual, names) {
			return fmt.Errorf("S3 source directory changed: %s", dir)
		}
		live, err := liveContractDirectoryEntries(root, dir)
		if err != nil {
			return fmt.Errorf("S3 authority checkout source directory is invalid: %s: %w", dir, err)
		}
		if !sameDirectoryEntries(entries, live) {
			return fmt.Errorf("S3 authority checkout source directory changed: %s", dir)
		}
		// A symlink or directory substitution in the live checkout cannot
		// replace an authoritative source directory.
		if err := workpacket.CheckPath(root, filepath.ToSlash(filepath.Join(dir, ".source-probe"))); err != nil {
			return err
		}
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		return verified.Verify()
	}
	return nil
}
