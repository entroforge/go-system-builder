package workpacket

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewerViewPacketsAreRejectedWithAnswerShapeCorrection(t *testing.T) {
	s7, err := BuildReviewResult(reviewResultTestBinding(), reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	s8, err := BuildHypothesisResult(hypothesisResultTestBinding(t), hypothesisResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		stage       string
		packet      any
		instruction string
		validate    func([]byte) []FieldError
	}{
		{"S7", s7, s7.Draft.Instruction, func(data []byte) []FieldError { _, fields := ValidateReviewResultAnswer(s7, data); return fields }},
		{"S8", s8, s8.Draft.Instruction, func(data []byte) []FieldError { _, fields := ValidateHypothesisResultAnswer(s8, data); return fields }},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			data, err := json.Marshal(tc.packet)
			if err != nil {
				t.Fatal(err)
			}
			fields := tc.validate(data)
			if len(fields) == 0 {
				t.Fatal("accepted full view packet as answer")
			}
			for _, name := range []string{"title", "instruction", "read", "questions", "answer", "packet_id", "binding", "inputs", "draft"} {
				if !strings.Contains(tc.instruction, name) || !strings.Contains(fields[0].Repair, name) {
					t.Fatalf("prompt or concrete correction omits %s: %s", name, fields[0].Repair)
				}
			}
		})
	}
}
