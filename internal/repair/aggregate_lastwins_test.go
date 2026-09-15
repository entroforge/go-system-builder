package repair

import "testing"

// G12: sequential edits of shared files must aggregate last-wins instead of
// failing the batch; the later result describes the on-disk state.
func TestAggregateRepairResultArtifactsLastWins(t *testing.T) {
	results := []RepairResult{
		{ResultID: "r1", ChangedArtifacts: []ChangedArtifact{
			{Path: "shared.go", SHA256: "aaa", Status: "modified"},
			{Path: "only-r1.go", SHA256: "bbb", Status: "modified"},
		}},
		{ResultID: "r2", ChangedArtifacts: []ChangedArtifact{
			{Path: "shared.go", SHA256: "ccc", Status: "modified"},
			{Path: "only-r2.go", SHA256: "ddd", Status: "modified"},
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
	if byPath["shared.go"].SHA256 != "ccc" {
		t.Fatalf("shared.go: want later sha ccc, got %q", byPath["shared.go"].SHA256)
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
