package repair

import "testing"

// G12: sequential edits of shared files must aggregate last-wins instead of
// failing the batch; the later result describes the on-disk state.
func TestAggregateRepairResultArtifactsLastWins(t *testing.T) {
	results := []RepairResult{
		{ResultID: "r1", ChangedArtifacts: []ChangedArtifact{
			{Path: "shared.go", Status: "modified"},
			{Path: "only-r1.go", Status: "modified"},
		}},
		{ResultID: "r2", ChangedArtifacts: []ChangedArtifact{
			{Path: "shared.go", Status: "deleted"},
			{Path: "only-r2.go", Status: "modified"},
		}},
	}
	got, err := aggregateRepairResultArtifacts(results)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	byPath := map[string]ChangedArtifact{}
	for _, artifact := range got {
		byPath[artifact.Path] = artifact
	}
	if byPath["shared.go"].Status != "deleted" {
		t.Fatalf("shared.go lost later status: %+v", byPath["shared.go"])
	}
	if _, ok := byPath["only-r1.go"]; !ok {
		t.Fatal("only-r1.go must survive aggregation")
	}
	if _, ok := byPath["only-r2.go"]; !ok {
		t.Fatal("only-r2.go must survive aggregation")
	}
	if len(got) != 3 {
		t.Fatalf("want 3 unique paths, got %d", len(got))
	}
}
