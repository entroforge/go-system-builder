package cli

import (
	"strings"
	"testing"
)

func TestStatusProjectionAtHumanReleaseUsesEmptyOpenItemsArray(t *testing.T) {
	projection := buildStatusProjection(map[string]any{
		"runtime_id": "loop-REQ-001",
		"revision":   52,
		"lifecycle":  map[string]any{"state": "awaiting_human_release"},
	}, "S11", "awaiting_human_release", nil, ".")
	if len(projection.OpenItems) != 1 || projection.OpenItems[0] == "" {
		t.Fatalf("S11 status projection must expose one actionable human decision, got %v", projection.OpenItems)
	}
	gateway, ok := projection.HumanGateway.(map[string]any)
	if !ok {
		t.Fatalf("S11 status projection must expose a human gateway object, got %T", projection.HumanGateway)
	}
	if gateway["type"] != "human_release_gateway" {
		t.Fatalf("unexpected S11 gateway type: %#v", gateway["type"])
	}
	dispositions, ok := gateway["dispositions"].([]string)
	if !ok || len(dispositions) != 6 {
		t.Fatalf("S11 gateway must expose six finite dispositions, got %#v", gateway["dispositions"])
	}
	if gateway["human_required"] != true {
		t.Fatal("S11 status omitted the actual human decision boundary")
	}
	for _, retired := range []string{"decision_command", "finding_evidence"} {
		if _, exists := gateway[retired]; exists {
			t.Fatalf("S11 status still teaches the legacy %s submission requirement", retired)
		}
	}
	guidance, _ := gateway["guidance"].(string)
	for _, want := range []string{"exact application path", "human explicitly chooses", "disposition and optional reason", "saves that application", "Empty disposition", "without requiring a Finding", "does not publish or deploy", "Empty disposition keeps waiting"} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("S11 status lacks normal human handoff guidance %q: %s", want, guidance)
		}
	}
}

func TestTerminalProjectionsDoNotFallBackToCrossStage(t *testing.T) {
	for _, tc := range []struct {
		state string
		stage string
		want  string
	}{
		{state: "release_authorized", stage: "S11", want: "human-authorized terminal"},
		{state: "aborted", stage: "aborted", want: "aborted terminal"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			state := map[string]any{
				"runtime_id": "loop-REQ-001",
				"revision":   53,
				"lifecycle":  map[string]any{"state": tc.state},
			}
			stage, _, action := projectNext(tc.state, "", ".")
			if stage != tc.stage {
				t.Fatalf("state %s projected as stage %q, want %q", tc.state, stage, tc.stage)
			}
			if !strings.Contains(action, tc.want) {
				t.Fatalf("state %s action %q does not contain %q", tc.state, action, tc.want)
			}
			status := buildStatusProjection(state, stage, tc.state, nil, ".")
			gateway, ok := status.HumanGateway.(map[string]any)
			if !ok || gateway["type"] == "" {
				t.Fatalf("state %s status must expose terminal gateway projection, got %#v", tc.state, status.HumanGateway)
			}
		})
	}
}

func TestS10ProjectionKeepsTheAuditSequenceVisible(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  []string
	}{
		{state: "acceptance", want: []string{"exact questionnaire path supplied by the listener", "professional dispositions", "counterevidence", "references and conclusion", "original acceptance consumer"}},
		{state: "release_audit", want: []string{"actual release risks", "accepted acceptance results", "save the exact supplied release-audit questionnaire", "original exit", "reading prompts, not required tasks"}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			stage, _, action := projectNext(tc.state, "", ".")
			if stage != "S10" {
				t.Fatalf("state %s projected as stage %q, want S10", tc.state, stage)
			}
			for _, want := range tc.want {
				if !strings.Contains(action, want) {
					t.Fatalf("S10 action %q does not contain %q", action, want)
				}
			}
		})
	}
}

func TestS6DoneWhenUsesSavedResultAndOriginalConsumer(t *testing.T) {
	contract := contractFor("S6", map[string]any{}, t.TempDir())
	joined := strings.Join(contract.DoneWhen, " ")
	for _, want := range []string{"Builder Result", "exact Result path supplied by the listener", "original Result consumer", "Main verifies the required integration checkpoints"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("S6 DoneWhen lacks %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "runtime task-complete") {
		t.Fatalf("S6 DoneWhen still asks for a manual completion command: %s", joined)
	}
}

func TestS6NextAssignsAuthoringToRegisteredBuildersAndIntegrationToMain(t *testing.T) {
	stage, skill, action := projectNext("building", "", t.TempDir())
	if stage != "S6" || skill != "agent-dispatch" {
		t.Fatalf("S6 projection = stage %q skill %q", stage, skill)
	}
	for _, want := range []string{"Main follows actual TASK progress", "s6 prepare", "its exact brief", "native Agent/worktree", "integrate a received delivery", "registration alone does not prove a Builder is running"} {
		if !strings.Contains(action, want) {
			t.Fatalf("S6 projection omits responsibility guidance %q: %s", want, action)
		}
	}
	for _, stale := range []string{"complete Builder assignments", "Main fills", "Main completes each TASK"} {
		if strings.Contains(action, stale) {
			t.Fatalf("S6 projection retains ambiguous Main authoring language %q: %s", stale, action)
		}
	}
}

func TestS7FallbackUsesListenerQuestionnairesAndOriginalMainFlow(t *testing.T) {
	stage, skill, action := projectNext("verification", "unclassified_phase", t.TempDir())
	if stage != "S7" || skill != PrimarySkillS7 {
		t.Fatalf("S7 fallback projected stage=%q skill=%q", stage, skill)
	}
	for _, want := range []string{"exact ReviewPlan", "questionnaire paths", "original consumers", "Main retains plan registration, dispatch and queue release"} {
		if !strings.Contains(action, want) {
			t.Fatalf("S7 fallback action lacks %q: %s", want, action)
		}
	}
	for _, stale := range []string{"s7 status", "s7 draft", "manifest-draft", "register it"} {
		if strings.Contains(action, stale) {
			t.Fatalf("S7 fallback action still asks the user to guess a CLI step %q: %s", stale, action)
		}
	}
}
