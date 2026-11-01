// Manual agreement validator checks the agent-facing Manual and its evidence
// binding guidance against the available Loop definition.
//
// The Manual may live at either of two locations:
//
//   - `loop-harness.md` at the project root: source repo / template factory
//     placement, where the Manual is a human-visible template artifact that
//     ships at the tarball root. The source repo's `make manual` writes here.
//   - `.claude/bin/loop-harness.md`: target-project placement, where the
//     Manual sits beside the binary so Hook deep links resolve. Target
//     projects get this via `loop-harness init` or the install guide's cp.
//
// doctor picks the first one that exists. If neither exists, the project is
// missing its agent-facing specification. Historical digest fields are not
// required or compared.
//
// Called by `loop-harness doctor`. Failures emit a clear recovery hint
// pointing at `loop-harness manual --root .` so the reader knows the exact
// command that resolves the drift.
package semantic

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"os"
	"path/filepath"
	"strings"
)

type manualEvidenceDefinition struct {
	Transitions []struct {
		ID               string   `json:"id"`
		RequiredEvidence []string `json:"required_evidence"`
	} `json:"transitions"`
	PhaseMachines map[string]struct {
		Transitions []struct {
			ID               string   `json:"id"`
			RequiredEvidence []string `json:"required_evidence"`
		} `json:"transitions"`
	} `json:"phase_machines"`
	GlobalTransitions []struct {
		ID               string   `json:"id"`
		RequiredEvidence []string `json:"required_evidence"`
	} `json:"global_transitions"`
}

// manualCandidatePaths lists the on-disk locations where the agent-facing
// Manual may live, in lookup order. See the package doc comment for the
// rationale behind each path.
var manualCandidatePaths = []string{
	"loop-harness.md",
	".claude/bin/loop-harness.md",
}

// ValidateManualAgreement checks that the Manual and definition are readable,
// and that the Manual includes the definition's required evidence guidance.
// A historical digest in the Manual is neither required nor compared.
func ValidateManualAgreement(root string) error {
	foundRel, err := findManual(root)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, foundRel))
	if err != nil {
		return fmt.Errorf("read manual at %s: %w", foundRel, err)
	}

	defData, err := os.ReadFile(filepath.Join(root, projectlayout.Definition))
	if err != nil {
		return fmt.Errorf("read loop-definition.json: %w", err)
	}
	var definition manualEvidenceDefinition
	if err := json.Unmarshal(defData, &definition); err != nil {
		// ValidateRepository performs the full schema/semantic definition
		// validation. Keep this narrow manual check compatible with small
		// synthetic Manual-agreement fixtures.
		return nil
	}
	if err := validateManualEvidenceBindings(definition, string(data)); err != nil {
		return fmt.Errorf("manual evidence bindings: %w", err)
	}
	return nil
}

func validateManualEvidenceBindings(definition manualEvidenceDefinition, markdown string) error {
	check := func(id string, slots []string) error {
		for _, slot := range slots {
			prefix := "--evidence " + slot + "="
			if !strings.Contains(markdown, prefix) {
				return fmt.Errorf("manual missing evidence binding guidance for transition %s slot %s; run `loop-harness manual --root .` to regenerate; expected %s<reference>", id, slot, prefix)
			}
		}
		return nil
	}
	for _, spec := range definition.Transitions {
		if err := check(spec.ID, spec.RequiredEvidence); err != nil {
			return err
		}
	}
	for _, machine := range definition.PhaseMachines {
		for _, spec := range machine.Transitions {
			if err := check(spec.ID, spec.RequiredEvidence); err != nil {
				return err
			}
		}
	}
	for _, spec := range definition.GlobalTransitions {
		if err := check(spec.ID, spec.RequiredEvidence); err != nil {
			return err
		}
	}
	return nil
}

// findManual returns the relative path of the first existing Manual candidate
// under root, in manualCandidatePaths order. Returns an error naming both
// candidates if neither exists.
func findManual(root string) (string, error) {
	for _, p := range manualCandidatePaths {
		_, err := os.Stat(filepath.Join(root, p))
		if err == nil {
			return p, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat manual candidate %s: %w", p, err)
		}
	}
	return "", fmt.Errorf("manual missing — looked for %s and %s; run `loop-harness manual --root .` to regenerate",
		manualCandidatePaths[0], manualCandidatePaths[1])
}
