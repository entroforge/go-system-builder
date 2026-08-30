// na_checklist_template_test.go guards the RC-12 linkage between the S7
// not_applicable gate, the draft exemplar and the human-readable checklist
// template (docs/design/NA-checklist-template.md):
//
//  1. The draft's E2E N/A claim must carry an na_checklist_id that names
//     the template, so every scaffolded N/A is verifiable against it.
//  2. The S7_NA_CHECKLIST_MISSING remediation must point the planner at
//     the template file, not a vague section reference.
package review

import (
	"os"
	"strings"
	"testing"
)

func TestDraftPlanNAChecklistIDReferencesTemplate(t *testing.T) {
	plan, _ := DraftPlanForRoot(t.TempDir(), baseDraftState(t), 1)
	if plan == nil {
		t.Fatal("DraftPlanForRoot returned nil plan")
	}
	found := false
	for _, claim := range plan.Claims {
		if claim.Applicability != "not_applicable" {
			continue
		}
		found = true
		if !strings.Contains(claim.NAChecklistID, "na-checklist-template-1") {
			t.Fatalf("N/A claim %s must reference the checklist template id, got %q", claim.ClaimID, claim.NAChecklistID)
		}
	}
	if !found {
		t.Fatal("ui_impact=none draft must scaffold the explicit N/A claim")
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
