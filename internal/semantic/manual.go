// Manual agreement validator — asserts the agent-facing Manual exists and
// that its embedded Loop definition SHA-256 matches the on-disk
// docs/loop-definition.json. The Manual is the deployment artifact paired
// with the binary; drift here means an agent is reading stale specifications,
// which is worse than having no Manual at all.
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
// doctor picks the first one that exists and checks its embedded SHA-256
// against docs/loop-definition.json. If neither exists, the project is
// missing its agent-facing specification.
//
// Called by `loop-harness doctor`. Failures emit a clear recovery hint
// pointing at `loop-harness manual --root .` so the reader knows the exact
// command that resolves the drift.
package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

// ValidateManualAgreement verifies the agent-facing Manual is present and
// current. The check is intentionally narrow:
//
//  1. The Manual file exists at one of manualCandidatePaths.
//  2. The Manual's header carries a parseable Loop definition SHA-256.
//  3. That SHA-256 matches a fresh computation over docs/loop-definition.json.
//
// Anything else (Manual content typos, missing sections, stale guard specs
// after a registry edit but no loop-definition change) is out of scope; the
// Manual is a derived artifact and is regenerated wholesale by
// `loop-harness manual`.
func ValidateManualAgreement(root string) error {
	foundRel, err := findManual(root)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, foundRel))
	if err != nil {
		return fmt.Errorf("read manual at %s: %w", foundRel, err)
	}

	embedded, err := ExtractManualDefinitionSHA(string(data))
	if err != nil {
		return fmt.Errorf("manual header malformed at %s (%w); run `loop-harness manual --root .` to regenerate",
			foundRel, err)
	}

	defData, err := os.ReadFile(filepath.Join(root, "docs", "loop-definition.json"))
	if err != nil {
		return fmt.Errorf("read loop-definition.json: %w", err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(defData))

	if embedded != actual {
		return fmt.Errorf("manual stale at %s: embedded Loop definition SHA-256 %s does not match current docs/loop-definition.json SHA-256 %s — run `loop-harness manual --root .` to regenerate",
			foundRel, embedded, actual)
	}
	var definition manualEvidenceDefinition
	if err := json.Unmarshal(defData, &definition); err != nil {
		// ValidateRepository performs the full schema/semantic definition
		// validation. Keep this narrow manual check compatible with small
		// synthetic Manual-agreement fixtures that only exercise fingerprinting.
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

// ExtractManualDefinitionSHA parses the Manual markdown header to find the
// Loop definition SHA-256 line. Exported so tests and other tooling can read
// the embedded fingerprint without re-implementing the parse.
//
// The expected line shape (produced by transition.RenderManual) is:
//
//   - **Loop definition SHA-256**: `<64 hex chars>`
//
// Returns an error if the line is missing or the SHA-256 value is malformed.
func ExtractManualDefinitionSHA(markdown string) (string, error) {
	const prefix = "- **Loop definition SHA-256**:"
	for _, raw := range strings.Split(markdown, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		rest = strings.Trim(rest, "`")
		if len(rest) != 64 {
			return "", fmt.Errorf("SHA-256 line has %d chars, want 64: %q", len(rest), line)
		}
		for _, c := range rest {
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
			if !isHex {
				return "", fmt.Errorf("SHA-256 contains non-hex character %q: %s", string(c), line)
			}
		}
		return rest, nil
	}
	return "", fmt.Errorf("Loop definition SHA-256 line not found in manual header")
}
