package transition

import "testing"

func TestGeneratedEvidenceReferenceMustMatchCatalog(t *testing.T) {
	if err := validateGeneratedEvidence(nil, "pause_record", "generated:unexpected", Request{}); err == nil {
		t.Fatal("generated evidence must reject a reference that is not declared by the catalog")
	}
}

func TestGeneratedEvidenceRejectsPreCatalogReferences(t *testing.T) {
	for _, ref := range []string{
		"runtime:pause-checkpoint",
		"docs/reports/human/pause-record.md",
	} {
		if err := validateGeneratedEvidence(nil, "pause_record", ref, Request{}); err == nil {
			t.Fatalf("pre-catalog generated evidence reference %q must be rejected", ref)
		}
	}
}
