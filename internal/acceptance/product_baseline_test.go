package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalEmptyProductBaselineDoesNotDemandInventedPath(t *testing.T) {
	var manifest Manifest
	if err := json.Unmarshal(validAcceptanceManifest(t), &manifest); err != nil {
		t.Fatal(err)
	}
	var inventory []CoverageItem
	removed := map[string]bool{}
	for _, item := range manifest.CoverageInventory {
		if item.Category == "changed_path" {
			removed[item.ID] = true
		} else {
			inventory = append(inventory, item)
		}
	}
	manifest.CoverageInventory = inventory
	var counterevidence []CounterevidenceItem
	for _, item := range manifest.Counterevidence {
		if !removed[item.InventoryID] {
			counterevidence = append(counterevidence, item)
		}
	}
	manifest.Counterevidence = counterevidence
	manifest.Metrics = DeriveMetrics(manifest)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateWithBaseline(data, ManifestAcceptance, Baseline{CanonicalProduct: true, ChangedPaths: []string{}}); err != nil {
		t.Fatalf("verified empty product diff cannot invent a path to pass S10: %v", err)
	}
	if _, err := Validate(data, ManifestAcceptance); err != nil {
		t.Fatalf("report reader must not demand invented denominator rows: %v", err)
	}
	if _, err := ValidateWithBaseline(data, ManifestAcceptance, Baseline{CanonicalProduct: true, ChangedPaths: []string{"internal/product.go"}}); err != nil {
		t.Fatalf("report projection must not recertify the upstream product denominator: %v", err)
	}
}

func TestCanonicalAllKeepsLiteralProductDenominator(t *testing.T) {
	path := "src/space and newline\n.go"
	baseline := Baseline{ChangedPaths: []string{path}, AffectedPathsAll: true, CanonicalProduct: true}
	if got := baselineChangedPathIssues(nil, baseline); len(got) == 0 {
		t.Fatal("all erased canonical required changed paths")
	}
	rows := []CoverageItem{{ID: "path:" + path, Category: "changed_path", SourceRefs: []string{path}}}
	if got := baselineChangedPathIssues(rows, baseline); len(got) != 0 {
		t.Fatalf("literal canonical path lost: %v", got)
	}
	if got := baselineChangedPathIssues(rows, Baseline{CanonicalProduct: true, ChangedPaths: []string{}}); len(got) == 0 {
		t.Fatal("invented path accepted against verified empty canonical denominator")
	}
}

func TestCanonicalS10MissingPlanCannotFallBackToLegacy(t *testing.T) {
	state := map[string]any{"runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"assignments": []any{map[string]any{"runtime_id": "loop-test", "baseline_generation": 1}}}}
	if !S10AuthorityAvailable(state) {
		t.Fatal("canonical missing sources were hidden by legacy availability check")
	}
	if _, err := BuildS10InventoryAuthority(t.TempDir(), state, Baseline{}); err == nil {
		t.Fatal("canonical authority inferred without pinned Plan")
	}
	if _, err := BuildS10ExternalBaseline(t.TempDir(), state, []string{"all"}); err == nil || !strings.Contains(err.Error(), "pinned ReviewPlan") {
		t.Fatalf("canonical all bypassed source proof: %v", err)
	}
}
