package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real changed path remains available to the next S7 plan even when its
// author never supplied a file digest. A contract without a digest cannot be
// projected as a frozen substitute for the product.
func TestS9SeedKeepsChangedPathWithoutInventingDigest(t *testing.T) {
	root := t.TempDir()
	ref, err := createS7ReviewPlanSeedForEntry(root, "review-plan-real-entry", 1,
		ChangeImpact{ChangedArtifacts: []ArtifactRef{{Path: "src/app.go"}}, IntegrationNotes: "merged contribution; checks pending"},
		ContractRef{Path: "contract.json"}, []string{"TASK-1"}, "impact.json", "2026-10-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ref.Path))
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Frozen []json.RawMessage `json:"frozen_subjects"`
		Impact struct {
			Summary string `json:"summary"`
		} `json:"change_impact"`
		Coverage []struct {
			Source string `json:"source_ref"`
		} `json:"coverage_inventory"`
		Claims []struct {
			Lens    string   `json:"lens"`
			Sources []string `json:"source_refs"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Frozen) != 0 || len(plan.Coverage) != 1 || plan.Coverage[0].Source != "src/app.go" {
		t.Fatalf("unexpected frozen or coverage: %s", data)
	}
	if !strings.Contains(plan.Impact.Summary, "merged contribution; checks pending") {
		t.Fatalf("original integration notes lost: %s", data)
	}
	found := false
	for _, claim := range plan.Claims {
		for _, source := range claim.Sources {
			found = found || claim.Lens == "qa" && source == "src/app.go"
		}
	}
	if !found {
		t.Fatalf("changed path absent from QA Claim: %s", data)
	}
}
