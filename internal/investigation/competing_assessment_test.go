package investigation_test

import (
	"bytes"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

// TestCompetingAssessmentUsesCurrentConclusiveDiscriminator proves the S8-4
// shape of `competing_hypothesis_assessment`. L3-S8:160 states that the
// assessment and `no_competing_hypothesis` express Main's judgment and
// reasoning rather than two machine proof routes, and that Main may cite
// already obtained results without the machine demanding a conclusive current
// discriminator before an exit may be selected. L3-S8:307 repeats that
// hypothesis state and "at least one refutation" are not machine gates.
//
// The machine therefore still keeps the assessment well-formed (a non-empty
// conclusion and a non-empty, duplicate-free id set) and still keeps the two
// declarations distinct from each other, but it no longer refuses an
// assessment whose cited hypothesis has no conclusive current result.
func TestCompetingAssessmentUsesCurrentConclusiveDiscriminator(t *testing.T) {
	for _, scenario := range []string{"supported", "refuted", "unknown", "inconclusive", "duplicate", "empty", "double", "historical"} {
		t.Run(scenario, func(t *testing.T) {
			f := readyCaseFixture(t, []string{"finding-1"})
			registerCurrentQuestion(t, f, "hypothesis-leading")
			submitCurrentResult(t, f, "hypothesis-leading", "assignment-hypothesis-leading", "supported")
			registerCurrentQuestion(t, f, "hypothesis-contrast")
			result := "supported"
			if scenario == "refuted" || scenario == "inconclusive" {
				result = scenario
			}
			submitCurrentResult(t, f, "hypothesis-contrast", "assignment-hypothesis-contrast", result)
			seedCausalClosure(t, f, investigationPointer(t, f), func(doc map[string]any) error {
				if scenario == "historical" {
					for _, raw := range doc["hypotheses"].([]any) {
						h := raw.(map[string]any)
						if h["hypothesis_id"] == "hypothesis-contrast" {
							h["assignment_id"] = "assignment-new-current"
						}
					}
				}
				return nil
			})
			ids := []any{"hypothesis-contrast"}
			if scenario == "unknown" {
				ids = []any{"hypothesis-unknown"}
			}
			if scenario == "duplicate" {
				ids = append(ids, ids[0])
			}
			if scenario == "empty" {
				ids = []any{}
			}
			request := investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "The actual discriminator located the causal gap while retaining its credible contrast.", CompetingHypothesisAssessment: map[string]any{"hypothesis_ids": ids, "conclusion": "Comparison isolated the loss at the decoder boundary; the contrast remains credible as a propagation surface."}}
			if scenario == "double" {
				request.NoCompetingHypothesis = "No credible alternative exists."
			}
			state, journal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			_, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, request)
			switch scenario {
			case "duplicate", "empty":
				// The assessment object itself must stay well-formed.
				if err == nil {
					t.Fatal("malformed competing assessment authorized repair")
				}
				if !bytes.Equal(state, mustRead(t, f.statePath)) || !bytes.Equal(journal, mustRead(t, f.journalPath)) {
					t.Fatal("rejected assessment mutated Runtime")
				}
			case "double":
				// Both declarations are Main's professional judgment expressed
				// as authored (L3-S8:160). The machine stores them without
				// rewriting, conflating or dropping either one.
				if err != nil {
					t.Fatalf("Main's two competing declarations were refused: %v", err)
				}
				doc := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
				if doc["competing_hypothesis_assessment"] == nil || doc["no_competing_hypothesis"] == nil {
					t.Fatalf("machine dropped or conflated Main's competing declarations: %v", doc)
				}
			default:
				// A cited discriminator without a conclusive current result is
				// Main's professional judgment, not a machine proof route.
				if err != nil {
					t.Fatalf("Main's competing assessment was refused: %v", err)
				}
				doc := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
				if doc["competing_hypothesis_assessment"] == nil || doc["no_competing_hypothesis"] != nil {
					t.Fatalf("assessment conflated with absence: %v", doc)
				}
			}
		})
	}
}
