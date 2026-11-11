package workpacket

import (
	"encoding/json"
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
			if fields[0].Repair == "" {
				t.Fatal("invalid answer lacks actionable correction")
			}
		})
	}
}
