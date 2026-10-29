// na_checklist_template_test.go guards the RC-12 linkage between the S7
// not_applicable gate, the draft exemplar and the human-readable checklist
// template (docs/design/NA-checklist-template.md):
//
//  1. UI facts cannot stand in for Main completing the N/A checklist.
//  2. The S7_NA_CHECKLIST_MISSING remediation must point the planner at
//     the template file, not a vague section reference.
package review

import (
	"os"
	"strings"
	"testing"
)

func TestDraftPlanDoesNotInventNAChecklistDecision(t *testing.T) {
	plan, _ := DraftPlanForRoot(t.TempDir(), baseDraftState(t), 1)
	if plan == nil {
		t.Fatal("DraftPlanForRoot returned nil plan")
	}
	for _, claim := range plan.Claims {
		if claim.Lens == "e2e" && (claim.Applicability == "not_applicable" || claim.NAChecklistID != "") {
			t.Fatalf("UI impact alone cannot establish a completed N/A checklist: %+v", claim)
		}
	}
}

func TestNAChecklistTemplateDocExists(t *testing.T) {
	data, err := os.ReadFile("../../docs/design/NA-checklist-template.md")
	if err != nil {
		t.Fatalf("read N/A checklist template: %v", err)
	}
	text := string(data)
	for _, want := range []string{"scope", "impact", "evidence", "alternative", "sign-off"} {
		if !strings.Contains(text, want) {
			t.Fatalf("template lacks the %q section", want)
		}
	}
}
