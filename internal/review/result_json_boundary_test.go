package review

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOriginalResultProjectionPreservesJSONBoundaries(t *testing.T) {
	input := []byte(`{"result_id":"result-1","subject_digest":{"old":1},"verification_artifact_digest":12,"findings":[{"observed":"The product says sha256 is required"}]}`)
	original := append([]byte(nil), input...)
	var got Result
	if err := json.Unmarshal(input, &got); err != nil || len(got.Findings) != 1 || got.Findings[0].Observed != "The product says sha256 is required" {
		t.Fatalf("professional finding lost: %+v, %v", got.Findings, err)
	}
	if !bytes.Equal(input, original) {
		t.Fatal("original authoring bytes changed")
	}
	for _, source := range []string{`{"subject_digest":9}`, `{"subject_digest":{"legacy":true}}`, `{"subject_digest":null}`} {
		var result Result
		if err := json.Unmarshal([]byte(source), &result); err != nil {
			t.Fatalf("legacy result metadata blocked: %v", err)
		}
	}
	var receipt ResultReceipt
	if err := decodeOriginalResultReceipt([]byte(`{"packet_id":"p","answer_sha256":[1,2]}`), &receipt); err != nil || receipt.PacketID != "p" {
		t.Fatalf("legacy receipt metadata blocked identity: %+v, %v", receipt, err)
	}
	for _, tc := range []struct {
		name string
		new  func() any
	}{
		{"result", func() any {
			return &struct {
				Result
				Extra string `json:"extra"`
			}{}
		}},
		{"receipt", func() any {
			return &struct {
				ResultReceipt
				Extra string `json:"extra"`
			}{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outer := tc.new()
			if err := json.Unmarshal([]byte(`{"extra":"kept"}`), outer); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(outer)
			if err != nil || !strings.Contains(string(encoded), `"extra":"kept"`) {
				t.Fatalf("embedded DTO swallowed outer field: %s, %v", encoded, err)
			}
		})
	}
}
