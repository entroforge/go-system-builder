package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestSessionStartCommittedS7HandoffDefersOnlyMissingMainQuestionnaire(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": "loop-S7-handoff",
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"baseline":      map[string]any{"generation": 1},
		"lifecycle":     map[string]any{"state": "verification", "phase": "planned"},
		"review":        map[string]any{"round": 1},
	}
	result := controller.ControlResult{Snapshot: runtime.Snapshot{State: state}, QualityGate: controller.QualityGateResult{TransitionCommitted: true}}
	request := policy.Input{Event: "SessionStart", AgentID: "Main"}
	path := review.PlanQuestionnairePath(root, state)
	// No baseline, Plan, Service or journal exists: the handoff only names the
	// exact questionnaire, leaving original draft preparation to the first Pre.
	message, handled := sessionStartS7QuestionnaireHandoff(root, request, result)
	if !handled || !strings.Contains(message, path) || !strings.Contains(message, "first tool PreToolUse") || !strings.Contains(message, "does not need to manually issue or retry") {
		t.Fatalf("missing questionnaire handoff: handled=%t message=%s", handled, message)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("handoff attempted draft preparation: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	authored := []byte("preserved professional plan answers")
	if err := os.WriteFile(path, authored, 0600); err != nil {
		t.Fatal(err)
	}
	message, handled = sessionStartS7QuestionnaireHandoff(root, request, result)
	if !handled || !strings.Contains(message, path) || strings.Contains(message, "Preparation pending") {
		t.Fatalf("existing questionnaire was not reused: %s", message)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(authored) {
		t.Fatalf("existing questionnaire changed: %q %v", data, err)
	}
	for _, event := range []string{"PreToolUse", "SubagentStart", "PreCompact", "Stop"} {
		request.Event = event
		if _, handled := sessionStartS7QuestionnaireHandoff(root, request, result); handled {
			t.Fatalf("handoff intercepted original %s path", event)
		}
	}
	request.Event = "SessionStart"
	request.AgentID = "reviewer-current"
	if _, handled := sessionStartS7QuestionnaireHandoff(root, request, result); handled {
		t.Fatal("handoff intercepted Reviewer responsibility")
	}
	request.AgentID = "Main"
	result.QualityGate.TransitionCommitted = false
	if _, handled := sessionStartS7QuestionnaireHandoff(root, request, result); handled {
		t.Fatal("handoff intercepted ordinary S7 recovery")
	}
	result.QualityGate.TransitionCommitted = true
	state["lifecycle"] = map[string]any{"state": "building"}
	if _, handled := sessionStartS7QuestionnaireHandoff(root, request, result); handled {
		t.Fatal("handoff intercepted S6")
	}
	state["lifecycle"] = map[string]any{"state": "verification"}
	state["review"].(map[string]any)["plan"] = map[string]any{"plan_id": "registered-plan", "path": "registered-plan.json"}
	if _, handled := sessionStartS7QuestionnaireHandoff(root, request, result); handled {
		t.Fatal("handoff intercepted a registered ReviewPlan")
	}
}
