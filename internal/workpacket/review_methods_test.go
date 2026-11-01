package workpacket

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestReviewSelectedMethodsProvideReadGuidance(t *testing.T) {
	inputs := reviewResultTestInputs()
	original, err := BuildReviewResult(reviewResultTestBinding(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	inputs.MethodSource = &ReviewMethodSource{ManifestRef: ".claude/workgroups/qa.json"}
	inputs.Methods = []ReadItem{{Path: ".claude/skills/database-change/SKILL.md", Purpose: ReviewMethodPurpose("database-change")}, {Path: ".claude/skills/code-quality/SKILL.md", Purpose: ReviewMethodPurpose("code-quality")}}
	withMethods, err := BuildReviewResult(original.Binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if withMethods.ID != original.ID || !reflect.DeepEqual(withMethods.Draft.Questions, original.Draft.Questions) {
		t.Fatal("selected methods must not replace the original responsibility or professional questions")
	}
	if len(withMethods.Draft.Read) != len(original.Draft.Read)+2 {
		t.Fatal("selected methods did not reach read guidance")
	}
	encoded, err := json.Marshal(withMethods)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ReviewResultPacket
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded.Inputs.Methods, withMethods.Inputs.Methods) {
		t.Fatalf("read guidance was not retained: %v", err)
	}
}

func TestReviewSelectedMethodsRejectUnsafeGuidance(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ReviewResultInputs)
	}{
		{"missing source", func(i *ReviewResultInputs) { i.MethodSource = nil }},
		{"source traversal", func(i *ReviewResultInputs) { i.MethodSource.ManifestRef = "../manifest.json" }},
		{"method traversal", func(i *ReviewResultInputs) { i.Methods[0].Path = ".claude/skills/../SKILL.md" }},
		{"arbitrary product file", func(i *ReviewResultInputs) { i.Methods[0].Path = "src/app.go" }},
		{"missing purpose", func(i *ReviewResultInputs) { i.Methods[0].Purpose = " " }},
		{"duplicate method", func(i *ReviewResultInputs) { i.Methods = append(i.Methods, i.Methods[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputs := reviewResultTestInputs()
			inputs.MethodSource = &ReviewMethodSource{ManifestRef: ".claude/workgroups/qa.json"}
			inputs.Methods = []ReadItem{{Path: ".claude/skills/database-change/SKILL.md", Purpose: ReviewMethodPurpose("database-change")}}
			test.change(&inputs)
			if _, err := BuildReviewResult(reviewResultTestBinding(), inputs); err == nil {
				t.Fatal("invalid selected method accepted")
			}
		})
	}
}
