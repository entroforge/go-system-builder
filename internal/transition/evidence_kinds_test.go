package transition

import "testing"

func TestAllowedEvidenceKindsRetainsExecutionBatchRecords(t *testing.T) {
	for _, required := range []string{"contract_set_record", "task_batch_record"} {
		allowed := allowedEvidenceKinds(required)
		if !contains(allowed, "document_review") {
			t.Fatalf("%s must accept document_review evidence, got %v", required, allowed)
		}
	}
	team := allowedEvidenceKinds("team_manifest_record")
	if !contains(team, "builder_report") {
		t.Fatalf("team_manifest_record must accept builder_report evidence, got %v", team)
	}
}

func TestAllowedEvidenceKindsMapsChangeImpactRecord(t *testing.T) {
	allowed := allowedEvidenceKinds("change_impact_record")
	if len(allowed) != 1 || allowed[0] != "change_impact" {
		t.Fatalf("change_impact_record must accept change_impact evidence, got %v", allowed)
	}
}
