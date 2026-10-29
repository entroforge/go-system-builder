package workpacket

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReviewSelectedMethodsBindOnlyNewPacketAndReadGuidance(t *testing.T) {
	inputs := reviewResultTestInputs()
	legacy, err := BuildReviewResult(reviewResultTestBinding(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(legacy)
	inputs.MethodSource = &ReviewMethodSource{ManifestRef: ".claude/workgroups/qa.json", SHA256: strings.Repeat("e", 64)}
	inputs.Methods = []ReadItem{{Path: ".claude/skills/database-change/SKILL.md", Purpose: ReviewMethodPurpose("database-change")}, {Path: ".claude/skills/code-quality/SKILL.md", Purpose: ReviewMethodPurpose("code-quality")}}
	current, err := BuildReviewResult(legacy.Binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID == legacy.ID || !bytes.Equal(current.Draft.Answer, legacy.Draft.Answer) || !reflect.DeepEqual(current.Draft.Questions, legacy.Draft.Questions) {
		t.Fatal("selected methods changed professional questions or failed to bind issuance")
	}
	if len(current.Draft.Read) != len(legacy.Draft.Read)+2 {
		t.Fatal("selected methods did not reach read guidance")
	}
	for _, p := range []ReviewResultPacket{legacy, current} {
		restored, err := RebuildReviewResult(p)
		if err != nil || !reflect.DeepEqual(restored, p) {
			t.Fatal("frozen selection was refreshed", err)
		}
	}
	restored, _ := RebuildReviewResult(legacy)
	after, _ := json.Marshal(restored)
	if !bytes.Equal(before, after) || bytes.Contains(after, []byte("method_source")) || bytes.Contains(after, []byte(`"methods"`)) {
		t.Fatal("old absent fields changed identity")
	}
	current.Draft.Read[len(current.Draft.Read)-1].Path = ".claude/skills/security-review/SKILL.md"
	if _, err := ReviewResultPacketDigest(current); err == nil {
		t.Fatal("changed delivered method accepted as original")
	}
}

func TestReviewSelectedMethodsRejectUnboundOrUnsafeGuidance(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ReviewResultInputs)
	}{
		{"missing source", func(i *ReviewResultInputs) { i.MethodSource = nil }},
		{"bad source digest", func(i *ReviewResultInputs) { i.MethodSource.SHA256 = "not-a-digest" }},
		{"source traversal", func(i *ReviewResultInputs) { i.MethodSource.ManifestRef = "../manifest.json" }},
		{"method traversal", func(i *ReviewResultInputs) { i.Methods[0].Path = ".claude/skills/../SKILL.md" }},
		{"arbitrary product file", func(i *ReviewResultInputs) { i.Methods[0].Path = "src/app.go" }},
		{"missing purpose", func(i *ReviewResultInputs) { i.Methods[0].Purpose = " " }},
		{"duplicate method", func(i *ReviewResultInputs) { i.Methods = append(i.Methods, i.Methods[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputs := reviewResultTestInputs()
			inputs.MethodSource = &ReviewMethodSource{ManifestRef: ".claude/workgroups/qa.json", SHA256: strings.Repeat("e", 64)}
			inputs.Methods = []ReadItem{{Path: ".claude/skills/database-change/SKILL.md", Purpose: ReviewMethodPurpose("database-change")}}
			test.change(&inputs)
			if _, err := BuildReviewResult(reviewResultTestBinding(), inputs); err == nil {
				t.Fatal("invalid selected method accepted")
			}
		})
	}
}
