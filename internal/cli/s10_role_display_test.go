package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestS10LateReviewerDoesNotInheritMainAuditWork(t *testing.T) {
	for _, stage := range []string{"acceptance", "release_audit"} {
		for _, request := range []policy.Input{{Event: "PreToolUse", AgentID: "opaque-reviewer"}, {Event: "SubagentStop", AgentID: "opaque-reviewer"}, {Event: "SubagentStart"}, {Event: "TeammateIdle", TeammateName: "reviewer"}} {
			root := t.TempDir()
			state := map[string]any{"runtime_id": "late-return", "revision": 7, "lifecycle": map[string]any{"state": stage}}
			g := BuildGuidanceForState(root, state, request.Event, request)
			if g.Stage != "S10" || g.LifecycleState != stage || g.RuntimeID != "late-return" || g.Revision != 7 {
				t.Fatalf("lost global facts: %+v", g)
			}
			if g.Action != "" || len(g.Automation) != 0 || len(g.Recovery) != 0 || len(g.DoneWhen) != 0 {
				t.Fatalf("%s/%+v assigned Main audit to Worker: %+v", stage, request, g)
			}
			if g.PrimarySkill != "" || len(g.Read) != 0 || len(g.ReadOrder) != 0 || len(g.Questions) != 0 || len(g.Integration) != 0 || !strings.Contains(g.Objective, "assigns no S10 audit work") {
				t.Fatalf("%s/%+v preloaded Main professional work: %+v", stage, request, g)
			}
			assertNoMainAuditMaterials(t, g.Instruction)
			if strings.Contains(g.Instruction, "Next:") || strings.Contains(g.Instruction, "Next: .") {
				t.Fatal(g.Instruction)
			}
			main := BuildGuidanceForState(root, state, "PreToolUse", policy.Input{Event: "PreToolUse", AgentID: "Main"})
			if main.Action == "" || len(main.Automation) == 0 || len(main.Recovery) == 0 || len(main.DoneWhen) == 0 || main.PrimarySkill == "" || len(main.Read) == 0 || len(main.ReadOrder) == 0 {
				t.Fatalf("Main lost original preparation/recovery: %+v", main)
			}
		}
	}
}

func TestGuidanceInstructionOmitsOnlyEmptyNext(t *testing.T) {
	for _, action := range []string{"", " \t\n", "read exact original receipt"} {
		text := formatGuidanceInstruction(policy.Guidance{Stage: "S10", Action: action, Objective: "retain original facts", ProtocolRef: "protocol", ManualRef: "manual"})
		if strings.Contains(text, "Next:") != (strings.TrimSpace(action) != "") || !strings.Contains(text, "Read protocol") || !strings.Contains(text, "retain original facts") {
			t.Fatal(text)
		}
	}
}

// Exercise the public Hook output after the global cursor has moved to S10.
// The original review index remains an engineering fact, not a new audit task.
func TestS10PublicHookSeparatesLateReviewerFromMainQuestionnaire(t *testing.T) {
	root, state := auditQuestionnaireRuntime(t)
	path, _, err := prepareAuditQuestionnaire(root, state, "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	beforeReview, _ := json.Marshal(state["review"])
	for _, tc := range []struct{ actor, event string }{{"opaque-returned-reviewer", "PreToolUse"}, {"opaque-returned-reviewer", "SubagentStop"}, {"Main", "PreToolUse"}} {
		actor := tc.actor
		body, _ := json.Marshal(map[string]any{"hook_event_name": tc.event, "session_id": "late-" + actor + tc.event, "cwd": root, "agent_id": actor, "tool_name": "Read", "tool_input": map[string]any{"file_path": "docs/control/agent-protocol.md"}})
		var out, diagnostic bytes.Buffer
		if code := Run([]string{"hook", "--root", root, "--event", tc.event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("%s: %d %s", actor, code, &diagnostic)
		}
		var payload map[string]any
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatalf("%s: %v %s", actor, err, &out)
		}
		specific, _ := payload["hookSpecificOutput"].(map[string]any)
		text, _ := specific["additionalContext"].(string)
		if message, _ := payload["systemMessage"].(string); message != "" {
			text += "\n" + message
		}
		if actor == "Main" {
			if !strings.Contains(text, filepath.Join(root, path)) || !strings.Contains(text, "Next:") || !strings.Contains(text, "acceptance-and-handoff") || !strings.Contains(text, "ACC-template") {
				t.Fatalf("Main lost exact audit handoff: %s", text)
			}
		} else {
			assertNoMainAuditMaterials(t, text)
			if !strings.Contains(text, "assigns no S10 audit work") || !strings.Contains(text, "S10") || strings.Contains(text, "Next:") || strings.Contains(text, filepath.Join(root, path)) || strings.Contains(text, "Use the current exact Manifest") {
				t.Fatalf("late reviewer received Main work: %s", text)
			}
		}
	}
	latest := initMap(t, root)
	afterReview, _ := json.Marshal(latest["review"])
	if !bytes.Equal(beforeReview, afterReview) {
		t.Fatal("role display changed original review facts")
	}
}

func assertNoMainAuditMaterials(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{"ACC-template", "release-audits", "release-architecture-audit", "acceptance-and-handoff", "complete acceptance and release audit", "Use the current exact Manifest"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Worker inherited Main audit material %q: %s", forbidden, text)
		}
	}
}

// Validate actual role projections through the embedded decision envelope's
// original guidance $ref. Only empty recipient action/skill strings are legal;
// the required fields, their types and array shapes remain enforced.
func TestS10RoleGuidanceSatisfiesEmbeddedDecisionSchema(t *testing.T) {
	root, state := auditQuestionnaireRuntime(t)
	path, _, err := prepareAuditQuestionnaire(root, state, "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	validator := schema.NewEmbeddedValidator()
	validate := func(guidance map[string]any) error {
		envelope := policy.DecisionEnvelope{SchemaVersion: "1.1.0", DecisionID: "hook-decision-s10-role", PolicyID: "hook-policy-main", PolicyVersion: "1.0.0", PolicySHA256: strings.Repeat("a", 64), HookEvent: "PreToolUse", SessionID: "s10-role-schema", MatchedRuleIDs: []string{}, Decision: "allow", Reason: "recipient-specific stage projection", Missing: []string{}, Recovery: []string{}, Retry: "not_applicable", EvaluatedAt: "2026-10-25T00:00:00Z"}
		data, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		var instance map[string]any
		if err := json.Unmarshal(data, &instance); err != nil {
			return err
		}
		instance["guidance"] = guidance
		data, err = json.Marshal(instance)
		if err != nil {
			return err
		}
		return validator.ValidateBytes("hook-decision.schema.json", data)
	}
	for _, actor := range []string{"opaque-late-reviewer", "Main"} {
		t.Run(actor, func(t *testing.T) {
			request := policy.Input{Event: "PreToolUse", AgentID: actor}
			g := BuildGuidanceForState(root, state, request.Event, request)
			if actor == "Main" {
				if g.Action == "" || g.PrimarySkill == "" || !strings.Contains(g.Action, filepath.Join(root, path)) {
					t.Fatalf("Main lost assigned audit work: %+v", g)
				}
			} else if g.Action != "" || g.PrimarySkill != "" {
				t.Fatalf("Worker gained Main audit work: %+v", g)
			}
			data, err := json.Marshal(g)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"read", "missing", "done_when", "recovery"} {
				if _, ok := fields[name].([]any); !ok {
					t.Fatalf("%s must remain an array: %s", name, data)
				}
			}
			if err := validate(fields); err != nil {
				t.Fatalf("real %s Guidance fails embedded schema: %v\n%s", actor, err, data)
			}
			for _, name := range []string{"action", "primary_skill"} {
				original := fields[name]
				delete(fields, name)
				if err := validate(fields); err == nil {
					t.Fatalf("missing required %s accepted", name)
				}
				for _, invalid := range []any{nil, false, 7, []any{}, map[string]any{}} {
					fields[name] = invalid
					if err := validate(fields); err == nil {
						t.Fatalf("non-string %s=%v accepted", name, invalid)
					}
				}
				fields[name] = original
			}
			for _, name := range []string{"read", "missing", "done_when", "recovery"} {
				original := fields[name]
				fields[name] = nil
				if err := validate(fields); err == nil {
					t.Fatalf("null %s array accepted", name)
				}
				fields[name] = original
			}
		})
	}
}
