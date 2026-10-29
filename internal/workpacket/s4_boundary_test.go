package workpacket

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS4KnownTemplateIdentityAndFieldDiagnostics(t *testing.T) {
	current := taskPlanningFixture(t)
	oldBytes, err := schema.ReadAsset("stage-form-s4-template-before-guidance.json")
	if err != nil {
		t.Fatal(err)
	}
	old, err := BuildTaskPlanningWithTemplate(current.Binding, current.Inputs, Digest(oldBytes))
	if err != nil {
		t.Fatal(err)
	}
	if old.TemplateSHA != Digest(oldBytes) || !strings.Contains(old.Draft.Instruction, "human gates") {
		t.Fatal("lost original template")
	}
	if current.ID == old.ID || strings.Contains(current.Draft.Instruction, "human gates") {
		t.Fatal("new issuance did not select the revised template")
	}
	if _, err := BuildTaskPlanningWithTemplate(current.Binding, current.Inputs, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unregistered template accepted")
	}
	var draft map[string]any
	data, _ := json.Marshal(old.Draft)
	json.Unmarshal(data, &draft)
	draft["answer"].(map[string]any)["unknown_scope"] = "wrong field"
	data, _ = json.Marshal(draft)
	_, issues := ValidateTaskPlanningAnswer(old, data)
	if len(issues) == 0 || issues[0].Pointer != "/answer/unknown_scope" || !strings.Contains(issues[0].Observed, "unknown_scope") {
		t.Fatalf("lost exact field: %+v", issues)
	}
}

func TestS4LargeAnswerKeepsByteBudgetAndUniqueness(t *testing.T) {
	p := taskPlanningFixture(t)
	pass, index := "pass", "docs/dev/tasks/index-REQ-001.md"
	draft := p.Draft
	draft.Answer = TaskPlanningAnswer{Conclusion: &pass, IndexPath: &index}
	for i := 0; i < 160; i++ {
		draft.Answer.TaskPaths = append(draft.Answer.TaskPaths, fmt.Sprintf("docs/dev/tasks/TASK-%03d.md", i+1))
	}
	data, _ := json.Marshal(draft)
	if _, issues := ValidateTaskPlanningAnswer(p, data); len(issues) != 0 {
		t.Fatalf("valid complete large answer rejected: %+v", issues)
	}
	draft.Answer.TaskPaths = append(draft.Answer.TaskPaths, draft.Answer.TaskPaths[0])
	data, _ = json.Marshal(draft)
	if _, issues := ValidateTaskPlanningAnswer(p, data); len(issues) == 0 {
		t.Fatal("duplicate accepted")
	}
	data = append(data, []byte(strings.Repeat(" ", int(TaskPlanningAnswerReadLimit(p))))...)
	if _, issues := ValidateTaskPlanningAnswer(p, data); len(issues) == 0 || issues[0].Observed != "oversized" {
		t.Fatalf("byte budget lost: %+v", issues)
	}
}
