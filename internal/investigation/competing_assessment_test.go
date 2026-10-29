package investigation_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

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
			if scenario == "supported" || scenario == "refuted" {
				if err != nil {
					t.Fatalf("truthful discriminated assessment rejected: %v", err)
				}
				doc := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
				if doc["competing_hypothesis_assessment"] == nil || doc["no_competing_hypothesis"] != nil {
					t.Fatalf("assessment conflated with absence: %v", doc)
				}
			} else {
				if err == nil {
					t.Fatal("invalid assessment authorized repair")
				}
				if !bytes.Equal(state, mustRead(t, f.statePath)) || !bytes.Equal(journal, mustRead(t, f.journalPath)) {
					t.Fatal("rejected assessment mutated Runtime")
				}
				if scenario == "double" && !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatal(err)
				}
			}
		})
	}
}
