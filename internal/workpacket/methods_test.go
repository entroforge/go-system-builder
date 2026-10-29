package workpacket

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS5StopGuidancePreservesIssuedTemplateAndMethods(t *testing.T) {
	original, err := schema.ReadAsset("stage-form-s5-template-save-guidance-legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	if Digest(original) != "8ac20d1ca0a4b45134afd452c03caec4db095ca8cf554fdefaa843e2ccaedfec" {
		t.Fatal("previously issued template bytes changed")
	}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		t.Run(responsibility, func(t *testing.T) {
			binding := testBinding()
			binding.Responsibility = responsibility
			inputs := []Reference{{ID: "REQ-039", Path: "req.md", Version: "v1", SHA256: strings.Repeat("b", 64)}}
			old, err := buildS5(binding, inputs, original)
			if err != nil {
				t.Fatal(err)
			}
			old, err = BindMethods(old, []ReadItem{{Path: ".claude/skills/document-verification/SKILL.md", Purpose: "Review this original responsibility."}})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(old)
			rebuilt, err := RebuildS5(old)
			after, _ := json.Marshal(rebuilt)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("new Stop guidance rewrote an issued packet", err)
			}
			pass := "pass"
			draft := old.Draft
			draft.Answer.Conclusion = &pass
			answer, _ := json.Marshal(draft)
			if _, issues := ValidateAnswer(old, answer); len(issues) != 0 {
				t.Fatal("old professional answer rejected", issues)
			}
			binding.Attempt = 2
			binding.PreviousSubmissionKey = strings.Repeat("d", 64)
			binding.RevisionReason = "Recheck the originally assigned subject."
			revision, err := RebuildS5ForBinding(binding, old)
			if err != nil || revision.TemplateSHA != old.TemplateSHA || !reflect.DeepEqual(revision.Draft, old.Draft) || !reflect.DeepEqual(revision.Methods, old.Methods) {
				t.Fatal("revision replaced frozen instructions or methods", err)
			}
			current, err := BuildS5(old.Binding, inputs)
			if err != nil || current.TemplateSHA == old.TemplateSHA {
				t.Fatal("new issuance did not bind current guidance", err)
			}
			for _, required := range []string{"普通保存不接收审查结果", "SubagentStop", "真实阻塞", "conclusion 为 null", "未接收不算 PASS"} {
				if !strings.Contains(current.Draft.Instruction, required) {
					t.Fatalf("new reviewer misses handoff guidance: %s", required)
				}
			}
			if len(current.Draft.Questions) != 2 || !reflect.DeepEqual(current.Draft.Answer, old.Draft.Answer) || !reflect.DeepEqual(current.Draft.Questions["conclusion"].Choices, old.Draft.Questions["conclusion"].Choices) {
				t.Fatal("Stop guidance added author fields or changed professional choices")
			}
		})
	}
}

func TestS5MethodsRetainSubjectsAnswersAndFrozenRevisions(t *testing.T) {
	base, err := BuildS5(testBinding(), []Reference{{ID: "REQ-039", Path: "req.md", Version: "v1", SHA256: strings.Repeat("b", 64)}})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(base)
	legacy, err := RebuildS5(base)
	legacyBytes, _ := json.Marshal(legacy)
	if err != nil || string(original) != string(legacyBytes) {
		t.Fatal("old packet changed", err)
	}
	methods := []ReadItem{{Path: ".claude/skills/specification-planning/SKILL.md", Purpose: "Apply the assigned planning method."}, {Path: ".claude/skills/api-contracts/SKILL.md", Purpose: "Review the actual API contract."}}
	bound, err := BindMethods(base, methods)
	if err != nil {
		t.Fatal(err)
	}
	if bound.ID == base.ID || !reflect.DeepEqual(bound.Inputs, base.Inputs) || !reflect.DeepEqual(bound.Draft, base.Draft) {
		t.Fatal("method projection changed subjects or author fields")
	}
	sha := strings.Repeat("c", 64)
	bound, err = BindTaskAnalysis(bound, &TaskAnalysisRef{Path: ".claude/work-packets/analysis/" + sha + ".json", SHA256: sha, ParserVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := RebuildS5(bound)
	if err != nil || !reflect.DeepEqual(bound, rebuilt) {
		t.Fatal("methods and analysis do not reconstruct", err)
	}
	data, _ := json.Marshal(bound)
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet.schema.json", data); err != nil {
		t.Fatal(err)
	}
	binding := bound.Binding
	binding.Attempt = 2
	binding.PreviousSubmissionKey = strings.Repeat("d", 64)
	binding.RevisionReason = "Recheck the original protocol observation."
	revised, err := RebuildS5ForBinding(binding, bound)
	if err != nil || revised.ID == bound.ID || !reflect.DeepEqual(revised.Methods, bound.Methods) {
		t.Fatal("revision lost original methods", err)
	}
	for _, path := range []string{"../skills/api-contracts/SKILL.md", ".claude/skills/../api-contracts/SKILL.md", "docs/requirements/REQ-039.md"} {
		if _, err := BindMethods(base, []ReadItem{{Path: path, Purpose: "method"}}); err == nil {
			t.Fatalf("invalid method source accepted: %s", path)
		}
	}
}
