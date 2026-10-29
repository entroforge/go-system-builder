package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
)

func TestContextSelectionCannotReplaceActivationSkillAuthority(t *testing.T) {
	for _, skills := range [][]string{{"testing-strategy"}, {}, {"unmapped-compatibility-skill"}} {
		root := t.TempDir()
		source := assignment.ActivationSourceEntry{AgentID: "agent-context", AgentDefinitionRef: ".claude/agents/backend-builder.md", SkillRefs: append([]string{}, skills...), WritePaths: []string{"internal/own"}, OutputPaths: []string{"docs/reports/own.json"}}
		beforeRef, err := assignment.PreStageActivationEnvelope(root, "workgroup-context", "TASK-context", "agent-context", "plan_checkpoint", source)
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(root, beforeRef))
		if err != nil {
			t.Fatal(err)
		}
		_, selectionErr := agentcontext.Select("../..", "backend-builder", nil, source.SkillRefs)
		if len(skills) > 0 && skills[0] == "unmapped-compatibility-skill" && selectionErr == nil {
			t.Fatal("unknown knowledge source became valid")
		}
		afterRef, err := assignment.PreStageActivationEnvelope(root, "workgroup-context", "TASK-context", "agent-context", "plan_checkpoint", source)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(root, afterRef))
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) || !reflect.DeepEqual(source.SkillRefs, skills) {
			t.Fatal("shadow selection changed original permission source")
		}
		var envelope assignment.ActivationEnvelope
		json.Unmarshal(after, &envelope)
		if len(skills) > 0 && skills[0] == "testing-strategy" && !reflect.DeepEqual(envelope.AllowedCommandClasses, []string{"lint", "test"}) {
			t.Fatalf("selected backend core widened test/lint authority: %+v", envelope)
		}
	}
}
