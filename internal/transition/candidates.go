package transition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/missingtokens"
)

// EvidenceCandidate is a current Runtime evidence artifact that can satisfy
// one required evidence slot. Eligibility is deliberately delegated to the
// same validation path used by Apply, including catalog kind compatibility,
// status, baseline/review-round, path safety, and fingerprint checks.
type EvidenceCandidate struct {
	ID                 string
	Path               string
	Kind               string
	Status             string
	BaselineGeneration int
	ReviewRound        int
}

// RenderTransitionWithCandidates renders the static transition explanation
// and appends read-only candidates from the supplied Runtime state. It never
// opens or writes the Runtime pair; callers decide how the state was loaded.
func RenderTransitionWithCandidates(def *LoopDefinition, id, root string, state map[string]any) string {
	body := RenderTransition(def, id)
	spec, ok := findTransitionSpec(def, id)
	if !ok || state == nil {
		return body
	}

	var out strings.Builder
	out.WriteString(body)
	// Gates with tokenized missing matrices get their legend up front, so
	// the agent can read the vocabulary before the first not_ready packet
	// (L3-S6 §9.3).
	if spec.AutoTrigger != nil && spec.AutoTrigger.QualityGateID != "" {
		if legend := missingtokens.RenderGateTokenLegend(spec.AutoTrigger.QualityGateID); legend != "" {
			out.WriteString(legend)
			out.WriteString("\n\n")
		}
	}
	if len(spec.RequiredEvidence) == 0 {
		return out.String()
	}
	out.WriteString("Current Runtime evidence candidates:\n\n")
	catalog := evidence.DefaultCatalog()
	for _, slot := range spec.RequiredEvidence {
		fmt.Fprintf(&out, "- `%s`:\n", slot)
		if generator, generated := catalog.Generator(slot); generated {
			fmt.Fprintf(&out, "  generated reference: `%s` (%s)\n", generator.Reference, generator.Description)
			continue
		}
		candidates := currentEvidenceCandidates(root, state, slot)
		if len(candidates) == 0 {
			out.WriteString("  _(no eligible current evidence candidates)_\n")
			continue
		}
		for _, candidate := range candidates {
			fmt.Fprintf(&out, "  - id: `%s`, path: `%s`, kind: `%s`, status: %s, baseline_generation: %d, review_round: %d\n",
				candidate.ID, candidate.Path, candidate.Kind, candidate.Status,
				candidate.BaselineGeneration, candidate.ReviewRound)
		}
	}
	out.WriteString("\n")
	return out.String()
}

func currentEvidenceCandidates(root string, state map[string]any, slot string) []EvidenceCandidate {
	raw, _ := state["evidence"].([]any)
	candidates := make([]EvidenceCandidate, 0, len(raw))
	for _, item := range raw {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := candidateString(record["id"])
		if id == "" || validateCurrentEvidence(root, state, slot, id) != nil {
			continue
		}
		candidates = append(candidates, EvidenceCandidate{
			ID:                 id,
			Path:               candidateString(record["path"]),
			Kind:               candidateString(record["kind"]),
			Status:             candidateString(record["status"]),
			BaselineGeneration: integer(record["baseline_generation"]),
			ReviewRound:        integer(record["review_round"]),
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return candidates
}

func candidateString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}
