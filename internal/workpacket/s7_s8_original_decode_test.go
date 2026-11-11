package workpacket

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOriginalS7S8PacketReadsIgnoreUnrelatedFieldsAndPreserveProfessionalText(t *testing.T) {
	t.Run("S7 original inputs", func(t *testing.T) {
		raw := []byte(`{"claims":[{"claim_id":"claim-a","observed":"SHA is ordinary product text"}],"frozen_subjects":[{"path":"source.go","sha256":{"unrelated":true}}],"unrelated_metadata":{"old":true}}`)
		var inputs ReviewResultInputs
		if err := json.Unmarshal(raw, &inputs); err != nil {
			t.Fatalf("unrelated fields blocked S7 inputs: %v", err)
		}
		if !strings.Contains(string(inputs.Claims), "SHA is ordinary product text") || !strings.Contains(string(inputs.FrozenSubjects), `"unrelated":true`) {
			t.Fatalf("original professional inputs were rewritten: %s %s", inputs.Claims, inputs.FrozenSubjects)
		}
		if err := json.Unmarshal([]byte(`{"claims":[],"claims":[],"frozen_subjects":[]}`), &inputs); err == nil {
			t.Fatal("duplicate original keys were accepted")
		}
	})
	t.Run("S8 original Finding", func(t *testing.T) {
		raw := []byte(`{"finding_id":"finding-a","ref":"finding.json","document":{"observed":"The product displays SHA as text","sha256":{"ordinary":"content"}},"unrelated_metadata":{"old":true}}`)
		var finding HypothesisResultFinding
		if err := json.Unmarshal(raw, &finding); err != nil {
			t.Fatalf("unrelated fields blocked S8 Finding: %v", err)
		}
		if !strings.Contains(string(finding.Document), `"ordinary":"content"`) || !strings.Contains(string(finding.Document), "The product displays SHA as text") {
			t.Fatalf("professional Finding was rewritten: %s", finding.Document)
		}
		if err := json.Unmarshal([]byte(`{"finding_id":"a","finding_id":"b","ref":"x","document":{}}`), &finding); err == nil {
			t.Fatal("duplicate original keys were accepted")
		}
	})
	t.Run("S8 packet responsibility", func(t *testing.T) {
		raw := []byte(`{"packet_id":"p","template_id":"s8-hypothesis-result-v1","binding":{"assignment_id":"assignment-a","unrelated_metadata":{}},"inputs":{"hypothesis":{},"case_route":{},"findings":[],"evidence_refs":[],"unrelated_metadata":true},"draft":{"title":"","instruction":"","read":[],"questions":{},"answer":{}},"unrelated_metadata":true}`)
		var packet HypothesisResultPacket
		if err := json.Unmarshal(raw, &packet); err != nil {
			t.Fatalf("unrelated fields blocked S8 responsibility: %v", err)
		}
		if packet.ID != "p" || packet.Binding.AssignmentID != "assignment-a" {
			t.Fatalf("original responsibility lost: %+v", packet.Binding)
		}
	})
}
