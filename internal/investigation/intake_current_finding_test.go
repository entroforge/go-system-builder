package investigation_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

// The original S7 Finding is sufficient for the S8 Case: no batch or hash
// is created or requested, and its identity is available to the Case writer.
func TestCurrentFindingEntersInvestigationWithoutBatchOrDigest(t *testing.T) {
	f := newIntakeFixture(t, []string{"finding-1"})
	raw, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	review := state["review"].(map[string]any)
	delete(review, "observation_batch")
	delete(review, "round")
	review["round_entry"] = map[string]any{"plan_id": "review-plan-current", "transition_id": "TR-006", "baseline_generation": 3, "change_impact_ref": nil}
	for _, rawFinding := range state["entities"].(map[string]any)["findings"].([]any) {
		finding := rawFinding.(map[string]any)
		delete(finding, "sha256")
		delete(finding, "review_round")
		finding["review_plan_id"] = "review-plan-current"
	}
	raw, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.statePath, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "one observed defect"})
	if err != nil {
		t.Fatalf("original Finding intake: %v", err)
	}
	pointer := result.State["review"].(map[string]any)["investigation"].(map[string]any)
	doc := readCaseDocument(t, f.root, pointer["path"].(string))
	ids := doc["source_finding_ids"].([]any)
	if len(ids) != 1 || ids[0] != "finding-1" {
		t.Fatalf("lost original Finding: %v", ids)
	}
	for _, field := range []string{"observation_batch_id", "observation_batch_ref", "observation_batch_sha256", "baseline_digest"} {
		if _, ok := doc[field]; ok {
			t.Fatalf("legacy source %s in current Case", field)
		}
	}
}
