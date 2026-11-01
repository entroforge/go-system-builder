package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOriginalRuntimeReceiptsPreserveProfessionalJSONAndComposition(t *testing.T) {
	input := []byte(`{"provenance":{"submitted_answer":{"sha256":{"professional":true}},"private_submissions":[{"path":"answer.json"}],"local_evidence":[{"path":"evidence.txt"}]},"main_proof":{"verified_facts":{"sha256":"professional"}}}`)
	original := append([]byte(nil), input...)
	var receipt RepairResultReceipt
	if err := decodeOriginalRepairResultReceipt(input, &receipt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receipt.Provenance.SubmittedAnswer, []byte(`"sha256":{"professional":true}`)) || !bytes.Contains(receipt.MainProof.VerifiedFacts, []byte(`"sha256":"professional"`)) {
		t.Fatalf("professional JSON changed: answer=%s facts=%s", receipt.Provenance.SubmittedAnswer, receipt.MainProof.VerifiedFacts)
	}
	if len(receipt.Provenance.PrivateSubmissions) != 1 || receipt.Provenance.PrivateSubmissions[0].Path != "answer.json" || !bytes.Equal(input, original) {
		t.Fatal("real submission path or source bytes changed")
	}
	for _, tc := range []struct {
		name string
		new  func() any
	}{
		{"hypothesis", func() any {
			return &struct {
				HypothesisResultReceipt
				Extra string `json:"extra"`
			}{}
		}},
		{"candidate", func() any {
			return &struct {
				RepairResultCandidateReceipt
				Extra string `json:"extra"`
			}{}
		}},
		{"result", func() any {
			return &struct {
				RepairResultReceipt
				Extra string `json:"extra"`
			}{}
		}},
		{"private", func() any {
			return &struct {
				RepairResultPrivateSubmission
				Extra string `json:"extra"`
			}{}
		}},
		{"evidence", func() any {
			return &struct {
				RepairResultLocalEvidence
				Extra string `json:"extra"`
			}{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.new()
			if err := json.Unmarshal([]byte(`{"extra":"kept"}`), value); err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(value)
			if err != nil || !strings.Contains(string(got), `"extra":"kept"`) {
				t.Fatalf("outer field lost: %s, %v", got, err)
			}
		})
	}
	var provenance RepairResultCandidateProvenance
	if err := json.Unmarshal([]byte(`{"submitted_answer":{},"packet_id":"one","packet_id":"two"}`), &provenance); err == nil {
		t.Fatal("existing duplicate-key validation was bypassed")
	}
}
