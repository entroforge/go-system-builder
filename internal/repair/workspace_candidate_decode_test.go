package repair

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkspaceCandidatePreservesProfessionalAnswer(t *testing.T) {
	raw := []byte(`{"version":1,"record_type":"repair_workspace_candidate","runtime_id":"runtime-1","baseline_generation":1,"execution_generation":1,"assignment_id":"a","session_id":"s","plan_id":"p","proposal":{"receipt":{"provenance":{"submitted_answer":{"business":{"sha256":"receipt-professional"}}},"main_proof":{"verified_facts":{"business":{"sha256":"facts-professional"}}}}},"provenance":{"submitted_answer":{"business":{"sha256":"professional-value"}}}}`)
	got, err := DecodeWorkspaceCandidate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provenance == nil || !strings.Contains(string(got.Provenance.SubmittedAnswer), `"sha256":"professional-value"`) {
		t.Fatalf("professional submitted answer was altered: %+v", got.Provenance)
	}
	if got.Proposal.Receipt == nil || !strings.Contains(string(got.Proposal.Receipt.Provenance.SubmittedAnswer), `"sha256":"receipt-professional"`) || !strings.Contains(string(got.Proposal.Receipt.MainProof.VerifiedFacts), `"sha256":"facts-professional"`) {
		t.Fatalf("professional nested receipt content was altered: %+v", got.Proposal.Receipt)
	}

}

func TestNewWorkspaceCandidateOmitsUnobservedOID(t *testing.T) {
	raw, err := json.Marshal(WorkspaceCandidate{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source_head", "base_commit", "contract_sha256"} {
		if _, exists := doc[key]; exists {
			t.Fatalf("new candidate registered a machine field %s: %s", key, raw)
		}
	}
}
