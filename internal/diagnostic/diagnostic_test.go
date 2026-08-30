package diagnostic

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestErrorRendersRecoverableGateDiagnostic(t *testing.T) {
	err := New(ErrorInput{
		Code:    "S7_PLAN_COVERAGE",
		Summary: "ReviewPlan coverage is incomplete",
		Missing: []string{"CC-007 has no Claim", "CASE-014/PATH-003 has no E2E Claim"},
		Repair:  []string{"edit plan.json and add Claims for the missing items"},
		Next:    "runtime review-plan --file plan.json --expected-revision 12",
		Verify:  "loop-harness s7 status",
	})

	message := err.Error()
	for _, want := range []string{
		"S7_PLAN_COVERAGE",
		"ReviewPlan coverage is incomplete",
		"missing:",
		"CC-007 has no Claim",
		"repair:",
		"edit plan.json",
		"next: runtime review-plan --file plan.json --expected-revision 12",
		"verify: loop-harness s7 status",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("diagnostic message missing %q: %s", want, message)
		}
	}
}

func TestErrorMarshalsMachineReadableDiagnostic(t *testing.T) {
	err := New(ErrorInput{
		Code:    "S7_EVIDENCE_TYPE",
		Summary: "evidence kind does not match the Claim requirement",
		Missing: []string{"claim-qa-2 requires console evidence"},
		Repair:  []string{"capture a console:<id> reference"},
		Next:    "runtime review-result submit --assignment-id assignment-qa-2 --result result.json",
		Verify:  "loop-harness s7 status",
	})

	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal diagnostic: %v", marshalErr)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode diagnostic: %v", err)
	}
	if got["code"] != "S7_EVIDENCE_TYPE" {
		t.Fatalf("code = %v", got["code"])
	}
	if got["next"] != "runtime review-result submit --assignment-id assignment-qa-2 --result result.json" {
		t.Fatalf("next = %v", got["next"])
	}
}
