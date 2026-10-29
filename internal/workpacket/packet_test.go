package workpacket

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/schema"
	"reflect"
	"strings"
	"testing"
)

func testBinding() Binding {
	return Binding{RuntimeID: "loop-REQ-039", Generation: 1, StageEntry: 7,
		Owner: "reviewer-spec", AssignmentID: "assignment-spec", AssignmentSHA: strings.Repeat("a", 64),
		Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1}
}

func TestS5PacketAndDraftSchemas(t *testing.T) {
	p, err := BuildS5(testBinding(), []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1", SHA256: strings.Repeat("b", 64)}})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"work-packet.schema.json": p, "stage-answer.schema.json": p.Draft} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes(name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestS5QuestionnaireStatesReviewCoverageWithoutExtraDeliveries(t *testing.T) {
	for responsibility, topics := range map[string][]string{
		"DV-SPEC-CONSISTENCY":   {"AC", "NFR", "负向路径", "CASE"},
		"DV-TASK-EXECUTABILITY": {"单一职责", "真实依赖", "自包含", "收尾", "并行", "数据迁移", "外部依赖", "高风险"},
	} {
		t.Run(responsibility, func(t *testing.T) {
			binding := testBinding()
			binding.Responsibility = responsibility
			p, err := BuildS5(binding, []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1", SHA256: strings.Repeat("b", 64)}})
			if err != nil {
				t.Fatal(err)
			}
			for _, topic := range topics {
				if !strings.Contains(p.Draft.Questions["conclusion"].Question, topic) {
					t.Errorf("questionnaire omits review topic %q", topic)
				}
			}
			if len(p.Draft.Questions) != 2 || !strings.Contains(p.Draft.Instruction, "不另交 PLAN、信封或重复报告") {
				t.Fatal("S5 must retain one professional conclusion/findings delivery")
			}
			for _, guidance := range []string{
				"pass（通过）＝本责任未发现问题",
				"fix_required（修正文档）＝REQ 含义明确",
				"req_change_required（需求待人决策）＝REQ 本身存在歧义、冲突或缺失",
				"不表示已获变更授权", "保持预填 []", "不用抄身份、指纹或机检汇总",
			} {
				if !strings.Contains(p.Draft.Instruction, guidance) {
					t.Errorf("questionnaire leaves verdict or authoring boundary implicit: %q", guidance)
				}
			}
			for _, guidance := range []string{"path（read 中的具体文件路径", "expected（应有内容）", "observed（实际缺口）"} {
				if !strings.Contains(p.Draft.Questions["findings"].Question, guidance) {
					t.Errorf("questionnaire leaves finding field implicit: %q", guidance)
				}
			}
			if p.Draft.Answer.Conclusion != nil || p.Draft.Answer.Findings == nil || len(p.Draft.Answer.Findings) != 0 || !reflect.DeepEqual(p.Draft.Questions["conclusion"].Choices, []string{"pass", "fix_required", "req_change_required"}) {
				t.Fatal("guidance must retain the original choices and prefilled empty findings")
			}
		})
	}
}

func TestS5WordingUpdatePreservesIssuedQuestionnaireAndRevision(t *testing.T) {
	data, err := schema.ReadAsset("stage-form-s5-template-legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	if Digest(data) != "cbe36ef5367a0684c7538aa18e7f78b753813606f1d2f0967a4326e7a4aa72c2" {
		t.Fatal("archived template no longer matches the previously issued questionnaires")
	}
	inputs := []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1", SHA256: strings.Repeat("b", 64)}}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		t.Run(responsibility, func(t *testing.T) {
			binding := testBinding()
			binding.Responsibility = responsibility
			old, err := buildS5(binding, inputs, data)
			if err != nil {
				t.Fatal(err)
			}
			oldBytes, _ := json.Marshal(old)
			rebuilt, err := RebuildS5(old)
			if err != nil || !reflect.DeepEqual(rebuilt, old) {
				t.Fatalf("issued questionnaire changed: %v", err)
			}
			pass := "pass"
			draft := old.Draft
			draft.Answer.Conclusion = &pass
			answerJSON, _ := json.Marshal(draft)
			if _, issues := ValidateAnswer(old, answerJSON); len(issues) != 0 {
				t.Fatalf("old answer no longer validates: %+v", issues)
			}
			current, err := BuildS5(binding, inputs)
			if err != nil || current.ID == old.ID || current.TemplateSHA == old.TemplateSHA {
				t.Fatalf("new issuance did not bind the new wording: %v", err)
			}
			binding.Attempt++
			binding.PreviousSubmissionKey = strings.Repeat("d", 64)
			binding.RevisionReason = "Recheck one original observation"
			revision, err := RebuildS5ForBinding(binding, old)
			if err != nil || revision.TemplateSHA != old.TemplateSHA || revision.ID == old.ID || !reflect.DeepEqual(revision.Draft, old.Draft) {
				t.Fatalf("named revision lost its original questionnaire: %v", err)
			}
			afterBytes, _ := json.Marshal(old)
			if !bytes.Equal(oldBytes, afterBytes) {
				t.Fatal("reconstruction changed the issued packet")
			}
			old.TemplateSHA = strings.Repeat("0", 64)
			if _, err := RebuildS5(old); err == nil {
				t.Fatal("unknown template fingerprint silently used current wording")
			}
		})
	}
}

func TestS5PacketIsStableAndOwnerIsolated(t *testing.T) {
	inputs := []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1", SHA256: strings.Repeat("b", 64)}}
	p, err := BuildS5(testBinding(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildS5(testBinding(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != again.ID || p.AnswerPath != again.AnswerPath {
		t.Fatal("unchanged responsibility must not reissue")
	}
	binding := testBinding()
	binding.Owner = "reviewer-task"
	binding.Responsibility = "DV-TASK-EXECUTABILITY"
	other, err := BuildS5(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if p.AnswerPath == other.AnswerPath {
		t.Fatal("shared checkout answers collide")
	}
	binding = testBinding()
	binding.StageEntry++
	reentry, err := BuildS5(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == reentry.ID {
		t.Fatal("stage reentry must not reuse an old packet")
	}
	if p.Draft.Answer.Conclusion != nil || !strings.Contains(p.Draft.Instruction, "answer") {
		t.Fatal("draft must be answerable without protocol lookup")
	}
}

func TestS5RejectsUnsafeIdentityAndUnknownResponsibility(t *testing.T) {
	inputs := []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1", SHA256: strings.Repeat("b", 64)}}
	for _, owner := range []string{"../other", "TODO(planner):owner", "", "x/y"} {
		binding := testBinding()
		binding.Owner = owner
		if _, err := BuildS5(binding, inputs); err == nil {
			t.Fatalf("unsafe owner accepted: %q", owner)
		}
	}
	binding := testBinding()
	binding.Responsibility = "QA-UNIT"
	if _, err := BuildS5(binding, inputs); err == nil {
		t.Fatal("unregistered S5 responsibility accepted")
	}
}

func TestS5AnswerCompletenessAndFrozenQuestionnaire(t *testing.T) {
	p, err := BuildS5(testBinding(), []Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1", SHA256: strings.Repeat("b", 64)}})
	if err != nil {
		t.Fatal(err)
	}
	pass := "pass"
	fix := "fix_required"
	for _, tc := range []struct {
		name     string
		answer   Answer
		complete bool
		pointer  string
	}{
		{"draft", Answer{Findings: []Finding{}}, false, "/answer/conclusion"},
		{"pass", Answer{Conclusion: &pass, Findings: []Finding{}}, true, ""},
		{"fix needs observation", Answer{Conclusion: &fix, Findings: []Finding{{Path: "docs/requirements/REQ-039.md", Expected: "deny"}}}, false, "/answer/findings/0/observed"},
		{"fix", Answer{Conclusion: &fix, Findings: []Finding{{Path: "docs/requirements/REQ-039.md#errors", Expected: "deny", Observed: "unspecified"}}}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := p.Draft
			draft.Answer = tc.answer
			data, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			_, issues := ValidateAnswer(p, data)
			if (len(issues) == 0) != tc.complete {
				t.Fatalf("issues: %+v", issues)
			}
			if tc.pointer != "" {
				found := false
				for _, issue := range issues {
					if issue.Pointer == tc.pointer {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing pointer %s: %+v", tc.pointer, issues)
				}
			}
		})
	}
	for _, change := range []func(*Draft){
		func(d *Draft) { d.Title = "a different responsibility" },
		func(d *Draft) { d.Read[0].Path = "docs/foreign.md" },
	} {
		data, err := json.Marshal(p.Draft)
		if err != nil {
			t.Fatal(err)
		}
		var draft Draft
		if err := json.Unmarshal(data, &draft); err != nil {
			t.Fatal(err)
		}
		change(&draft)
		draft.Answer = Answer{Conclusion: &pass, Findings: []Finding{}}
		data, err = json.Marshal(draft)
		if err != nil {
			t.Fatal(err)
		}
		if _, issues := ValidateAnswer(p, data); len(issues) == 0 {
			t.Fatal("machine questionnaire changed without rejection")
		}
	}
}
