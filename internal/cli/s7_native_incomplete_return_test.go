package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
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
	}{
		{name: "environment_unfinished", message: "The test service is unreachable; I cannot form a conclusion. Main must restore it.", want: "/answer/"},
		{name: "success_prose_is_not_acceptance", message: "PASS. All Claims verified.", want: "/answer/"},
		{name: "ordinary_missing_claim", message: "See the original answer.", want: "claim-dv-1", prepare: func(t *testing.T, root string, p *workpacket.ReviewResultPacket) {
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
			planPath := minimalReviewPlan(t, root, "review-plan-native-unfinished")
			fixtures.WriteState(t, root, state)
			stageCLI(t, "runtime", "workspace", "bind", "--root", root)
			stageCLI(t, "runtime", "review-plan", "--root", root, "--file", planPath)
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
				for _, want := range []string{"Main owns recovery", p.Binding.OwnerAgentID, answerPath, "does not establish review completion", tc.want} {
					if !strings.Contains(diagnostic.String(), want) {
						t.Fatalf("missing diagnostic %q: %s", want, &diagnostic)
					}
				}
				after := stageSnapshot(t, root)
				if after.Revision != before || after.State["lifecycle"].(map[string]any)["state"] != "verification" {
					t.Fatalf("unreceived return changed Runtime: revision %d/%d lifecycle %v", after.Revision, before, after.State["lifecycle"])
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
					for _, want := range []string{tc.want, p.ID, p.Binding.OwnerAgentID, answerPath, `"accepted":false`, `"work_complete":false`} {
						if !strings.Contains(observation, want) {
							t.Fatalf("execution observation lost original fact %q: %s", want, observation)
						}
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
				for _, want := range []string{tc.want, p.ID, p.Binding.OwnerAgentID, `"accepted":false`, `"work_complete":false`, "Stop 的具名作者", "具体字段/来源诊断", "续跑原 Reviewer", "不能把已结束但未接收的责任当作仍在后台运行"} {
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
