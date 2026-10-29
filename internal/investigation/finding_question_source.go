package investigation

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// remainingFindingQuestionRefs accepts only exact typed sources from this
// question's selected Findings, after checking the original Case/batch closure.
// It never registers an alias or re-signs the original source.
func remainingFindingQuestionRefs(root string, state, document map[string]any, selected, refs []string) ([]string, error) {
	caseIDs := stringSliceAnyToStrings(document["source_finding_ids"])
	if err := subsetOf(selected, caseIDs, "Hypothesis.source_finding_ids"); err != nil {
		return nil, err
	}
	data, err := workpacket.ReadFile(root, stringField(document["observation_batch_ref"]), hypothesisResultPacketReadLimit)
	if err != nil || workpacket.Digest(data) != stringField(document["observation_batch_sha256"]) {
		return nil, fmt.Errorf("S8 original ObservationBatch bytes/SHA differ: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("observation-batch.schema.json", data); err != nil {
		return nil, err
	}
	var batch struct {
		ID         string   `json:"observation_batch_id"`
		Runtime    string   `json:"runtime_id"`
		Round      int      `json:"review_round"`
		Generation int      `json:"baseline_generation"`
		Digest     string   `json:"subject_digest"`
		Findings   []string `json:"finding_ids"`
	}
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, err
	}
	if batch.ID != stringField(document["observation_batch_id"]) || batch.Runtime != stringField(state["runtime_id"]) || batch.Round != currentReviewRound(state) || batch.Generation != intFieldS8(s8Map(state["baseline"])["generation"]) || batch.Generation != intFieldS8(document["baseline_generation"]) || batch.Digest != stringField(document["baseline_digest"]) || !sameStringSet(batch.Findings, caseIDs) {
		return nil, fmt.Errorf("S8 ObservationBatch differs from current Case/source generation")
	}
	findings, err := readS8FindingDocuments(root, state, selected)
	if err != nil {
		return nil, err
	}
	frozen := map[string]bool{}
	for _, finding := range findings {
		var doc struct {
			Refs []string `json:"evidence_refs"`
		}
		if err := json.Unmarshal(finding.Document, &doc); err != nil {
			return nil, err
		}
		for _, ref := range doc.Refs {
			_, typed, err := evidence.ParseFindingReference(ref)
			if err != nil {
				return nil, err
			}
			if typed {
				frozen[ref] = true
			}
		}
	}
	remaining := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !frozen[ref] {
			remaining = append(remaining, ref)
			continue
		}
		if err := evidence.ValidateFindingReference(root, ref); err != nil {
			return nil, fmt.Errorf("S8 original Finding source %q: %w; restore the frozen source", ref, err)
		}
	}
	return remaining, nil
}
