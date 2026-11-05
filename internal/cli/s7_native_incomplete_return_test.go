package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Start from a genuinely issued questionnaire. An honest environmental return,
// misleading success prose, author omissions and missing preparation all keep
// the original consumer's rejection without forcing a second native Stop.
func TestS7IncompleteNativeReturnHandsOriginalResponsibilityToMain(t *testing.T) {
	for _, tc := range []struct {
		name, message, want string
		prepare             func(*testing.T, string, *workpacket.ReviewResultPacket)
		// received marks the subcase where the machine receives the author's
		// explicit verdict. The machine does not certify per-Claim coverage
		// (internal/review/register.go:134 "Receive the author's expression,
		// without certifying professional coverage"), so an ordinary omitted
		// Claim is the author's own, not a content gate.
		received bool
	}{
		{name: "environment_unfinished", message: "The test service is unreachable; I cannot form a conclusion. Main must restore it.", want: "/answer/"},
		{name: "success_prose_is_not_acceptance", message: "PASS. All Claims verified.", want: "/answer/"},
		{name: "ordinary_missing_claim", message: "See the original answer.", want: "claim-dv-1", received: true, prepare: func(t *testing.T, root string, p *workpacket.ReviewResultPacket) {
			var answer map[string]any
			if err := json.Unmarshal(p.Draft.Answer, &answer); err != nil {
				t.Fatal(err)
			}
			answer["verdict"] = "pass"
			answer["claim_results"] = []any{}
			p.Draft.Answer, _ = json.Marshal(answer)
			stageJSON(t, root, p.AnswerPath, p.Draft)
		}},
		{name: "missing_answer", message: "The exact supplied answer is missing.", want: "no such file", prepare: func(t *testing.T, root string, p *workpacket.ReviewResultPacket) {
			if err := os.Remove(filepath.Join(root, p.AnswerPath)); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtures.FreshRoot(t)
			state := fixtures.BaseState(t, root, "verification", "planned", 1)
			state["schema_version"] = "1.2.0"
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-native-unfinished", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
			planPath := minimalReviewPlan(t, root, "review-plan-native-unfinished")
			fixtures.WriteState(t, root, state)
			stageCLI(t, "runtime", "workspace", "bind", "--root", root)
			// The plan reaches the original consumer through the natural save
			// event. There is no separate plan registration or submission command.
			questionnaire := review.PlanQuestionnairePath(root, stageSnapshot(t, root).State)
			if questionnaire == "" {
				t.Fatal("no original ReviewPlan questionnaire path")
			}
			entryQuestionnaireHook(t, root, "PreToolUse", questionnaire)
			planBytes, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(questionnaire, planBytes, 0600); err != nil {
				t.Fatal(err)
			}
			if out := entryQuestionnaireHook(t, root, "PostToolUse", questionnaire); !strings.Contains(out, "review-plan: registered") {
				t.Fatalf("original Main plan not received: %s", out)
			}
			markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-native-unfinished")
			current := stageSnapshot(t, root)
			for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
				if agent := raw.(map[string]any); agent["id"] == "reviewer-native-unfinished" {
					agent["dispatch_mode"] = "plan_checkpoint"
				}
			}
			stageJSON(t, root, ".claude/loop-state.json", current.State)
			issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
			var packets []workpacket.ReviewResultPacket
			if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 1 {
				t.Fatalf("issue: %s (%v)", issued, err)
			}
			p := packets[0]
			if tc.prepare != nil {
				tc.prepare(t, root, &p)
			}
			before := stageSnapshot(t, root).Revision
			answerPath := filepath.Join(root, p.AnswerPath)
			original, readErr := os.ReadFile(answerPath)
			ref := filepath.Join(root, p.ViewPath)
			for _, active := range []bool{false, true} {
				body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-native-reviewer", "session_id": "native-review-parent", "cwd": root, "stop_hook_active": active, "last_assistant_message": tc.message + "\n" + ref})
				var out, diagnostic bytes.Buffer
				code := cli.Run([]string{"hook", "--root", root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic)
				if code != 0 || out.Len() != 0 {
					t.Fatalf("unreceived return blocked: active=%t code=%d stdout=%s stderr=%s", active, code, &out, &diagnostic)
				}
				after := stageSnapshot(t, root)
				if tc.received {
					// The author's explicit verdict is received; the omitted
					// Claim stays the author's own and is never recorded as
					// observed, and the round stays in S7.
					if !strings.Contains(diagnostic.String(), "原 S7 答卷已接收") {
						t.Fatalf("the author's explicit verdict was not received: %s", &diagnostic)
					}
					claims := after.State["review"].(map[string]any)["claims"].(map[string]any)
					claim, ok := claims["claim-dv-1"].(map[string]any)
					if !ok || claim["disposition"] == "pass" || claim["result_id"] != nil {
						t.Fatalf("the omitted Claim was reported as observed: %#v", claim)
					}
					if after.State["lifecycle"].(map[string]any)["state"] != "verification" {
						t.Fatalf("the omitted Claim advanced the round: %v", after.State["lifecycle"])
					}
				} else {
					// The reception is incomplete and the original responsibility
					// goes back to Main with its actual cause.
					for _, want := range []string{"S7 reception incomplete", "Main owns recovery", p.Binding.OwnerAgentID, answerPath, tc.want} {
						if !strings.Contains(diagnostic.String(), want) {
							t.Fatalf("missing diagnostic %q: %s", want, &diagnostic)
						}
					}
					// Reverse assertion: the unreceived return must never be
					// reported as a completed review.
					for _, forbidden := range []string{"原 S7 答卷已接收", "已完成本人责任", `"accepted":true`, `"work_complete":true`} {
						if strings.Contains(diagnostic.String(), forbidden) {
							t.Fatalf("the unreceived return was reported as received: %s", &diagnostic)
						}
					}
					if after.Revision != before || after.State["lifecycle"].(map[string]any)["state"] != "verification" {
						t.Fatalf("unreceived return changed Runtime: revision %d/%d lifecycle %v", after.Revision, before, after.State["lifecycle"])
					}
				}
				// A prior Stop diagnostic identifies its original responsibility;
				// it does not prove a later status-less or async response has returned.
				for _, response := range []map[string]any{
					{"content": []any{map[string]any{"type": "text", "text": tc.message}}},
					{"status": "async_launched", "isAsync": true},
				} {
					observation := mainNativeReturnResponse(t, root, ref, response)
					executionFact := "Platform execution is unknown here"
					if response["status"] == "async_launched" {
						executionFact = "launch acknowledgement"
					}
					if !strings.Contains(observation, executionFact) {
						t.Fatalf("lost actual execution fact %q: %s", executionFact, observation)
					}
					// While the execution state is unknown the observation uses the
					// recorded reception facts only. It must not re-read the
					// person's original answer to produce a fresh field
					// diagnosis (L4-workflow-convergence.md:217 — "执行状态未知
					// 或仍在后台时不因观察而重读原卷、要求重填或猜测重派").
					wants := []string{p.ID, p.Binding.OwnerAgentID, answerPath}
					if tc.received {
						wants = append(wants, `"accepted":true`, `"work_complete":true`)
					} else {
						wants = append(wants, `"accepted":false`, `"work_complete":false`)
					}
					for _, want := range wants {
						if !strings.Contains(observation, want) {
							t.Fatalf("execution observation lost original fact %q: %s", want, observation)
						}
					}
					if !tc.received && strings.Contains(observation, tc.want) {
						t.Fatalf("the unknown execution observation re-read the original answer: %s", observation)
					}
					for _, bad := range []string{"Native return for ", "续跑原 Reviewer", "不能把已结束但未接收的责任当作仍在后台运行"} {
						if strings.Contains(observation, bad) {
							t.Fatalf("unknown/launch inferred completion or new author duty %q: %s", bad, observation)
						}
					}
				}
				// The real supported completed field supplies execution evidence;
				// prose alone supplies no execution or professional acceptance.
				text := mainNativeReturnResponse(t, root, ref, map[string]any{"status": "completed", "content": []any{map[string]any{"type": "text", "text": tc.message}}})
				wants := []string{p.ID, p.Binding.OwnerAgentID}
				if tc.received {
					wants = append(wants, `"accepted":true`, `"work_complete":true`)
				} else {
					wants = append(wants, tc.want, `"accepted":false`, `"work_complete":false`, "Stop 的具名作者", "具体字段/来源诊断", "续跑原 Reviewer", "不能把已结束但未接收的责任当作仍在后台运行")
				}
				for _, want := range wants {
					if !strings.Contains(text, want) {
						t.Fatalf("Main lost unfinished recovery %q: %s", want, text)
					}
				}
			}
			saved, err := os.ReadFile(answerPath)
			if (readErr == nil) != (err == nil) || !bytes.Equal(original, saved) {
				t.Fatal("unreceived handoff rewrote the author's answer")
			}
		})
	}
}
