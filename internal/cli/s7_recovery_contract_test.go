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
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Recovery must describe facts produced by the original consumer. In particular,
// an ordinary finding can be accepted without a Batch, while site_lost records
// a blocked Assignment without accepting the professional result.
func TestS7RecoveryDescribesOriginalNativeResultState(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "ordinary-finding-drains"
		if lost {
			name = "site-lost-blocked"
		}
		t.Run(name, func(t *testing.T) {
			root := fixtures.FreshRoot(t)
			state := fixtures.BaseState(t, root, "verification", "planned", 1)
			state["schema_version"] = "1.2.0"
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-native-recovery", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
			planPath := minimalReviewPlan(t, root, "review-plan-native-recovery")
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
			markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-recovery")
			current := stageSnapshot(t, root)
			for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
				if agent := raw.(map[string]any); agent["id"] == "reviewer-recovery" {
					agent["dispatch_mode"] = "plan_checkpoint"
				}
			}
			stageJSON(t, root, ".claude/loop-state.json", current.State)
			issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
			var packets []workpacket.ReviewResultPacket
			if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 1 {
				t.Fatalf("issue original responsibility: %s (%v)", issued, err)
			}
			packet := packets[0]
			evidence := "docs/reports/review/recovery-observation.txt"
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, evidence)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, evidence), []byte("fixture observation: rejected input reaches a success response\n"), 0600); err != nil {
				t.Fatal(err)
			}
			encounter := map[string]any{
				"journey_summary":  "trace the rejected input through the response selection branch",
				"inspection_entry": "internal/example/service.go:1", "symbol_trail": "reject -> success",
				"wall_action": "inspect the rejection branch", "first_bad_checkpoint": "the success branch is selected",
				"last_good_checkpoint": "input is rejected", "terminal_state": "success response is selected",
			}
			siteLost := []any{}
			if lost {
				delete(encounter, "last_good_checkpoint")
				siteLost = append(siteLost, map[string]any{"finding_id": "finding-recovery", "reason": "The original rejection boundary trace is gone; the remaining output cannot reconstruct it."})
			}
			answer := map[string]any{
				"claim_results": []any{map[string]any{
					"claim_id": "claim-dv-1", "conclusion": "fail", "method": "traceability",
					"observed": "rejected input selects a success response", "evidence_refs": []string{"path:" + evidence},
				}},
				"blocked_claims": []any{}, "checks": []any{}, "deviations": []any{}, "site_lost": siteLost,
				"verification_artifact_digest": nil, "verdict": "finding",
				"findings": []any{map[string]any{
					"finding_id": "finding-recovery", "claim_id": "claim-dv-1", "severity": "P2",
					"expected": "rejected input returns an error", "authority_refs": []string{"REQ-WORKTREE"},
					"observed": "rejected input selects success", "observation_mode": "code_inspection",
					"reproducibility": "once_with_deterministic_trace", "evidence_refs": []string{"path:" + evidence},
					"encounter": encounter,
				}},
			}
			packet.Draft.Answer, _ = json.Marshal(answer)
			stageJSON(t, root, packet.AnswerPath, packet.Draft)
			// The original consumer observes the saved answer on the natural
			// successful tool event. SubagentStop stays the final fallback.
			payload := s7ListenerPayload(t, "native-recovery-actor", "Review "+filepath.Join(root, packet.ViewPath))
			_, diagnostic := s7ListenerEvent(t, root, "PostToolUse", payload)
			if !strings.Contains(diagnostic, "original consumer") {
				t.Fatalf("native result handoff: %s", diagnostic)
			}
			after := stageSnapshot(t, root)
			reviewState := after.State["review"].(map[string]any)
			row := reviewState["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
			if reviewState["observation_batch"] != nil {
				t.Fatal("a pending peer Claim must prevent ordinary sealing")
			}
			// The original result reaches the drain state through the original
			// consumer. A site_lost explanation keeps the actual finding and the
			// unknowns; it must not, on its own, forbid the handover.
			if workstate.ReviewAssignmentActivity(row) != "consumed" {
				t.Fatalf("original result did not enter actual drain state: %v", row)
			}
			if row["result_ref"] == nil || row["result_receipt"] == nil {
				t.Fatalf("original result receipt missing: %v", row)
			}
			claim := reviewState["claims"].(map[string]any)["claim-dv-1"].(map[string]any)
			if claim["disposition"] != "finding" {
				t.Fatalf("actual finding was not preserved: %v", claim)
			}
			observe := func(event string) string {
				t.Helper()
				body, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": root, "session_id": "s7-recovery-contract", "tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "docs/control/agent-protocol.md")}})
				var out, diagnostic bytes.Buffer
				if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
					t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
				}
				return out.String() + diagnostic.String()
			}
			for _, event := range []string{"SessionStart", "PreCompact"} {
				message := observe(event)
				if event == "PreCompact" {
					// The shared compaction boundary only resets presentation.
					// The next observation must restore guidance from original facts.
					if message != "" {
						t.Fatalf("PreCompact should not emit substitute context: %s", message)
					}
					message = observe("PreToolUse")
				}
				for _, wrong := range []string{"pointer missing from state.review", "ObservationBatch is open"} {
					if strings.Contains(message, wrong) {
						t.Fatalf("%s invents an unsealed Batch inconsistency: %s", event, message)
					}
				}
				if lost {
					// The site_lost explanation does not by itself forbid the
					// handover. The actual finding must still be visible.
					for _, want := range []string{"finding-recovery"} {
						if !strings.Contains(message, want) {
							t.Fatalf("%s loses the actual finding %q: %s", event, want, message)
						}
					}
					if strings.Contains(message, "S7 assignments blocked: none") {
						t.Fatalf("%s hides the actual state: %s", event, message)
					}
				}
			}
		})
	}
}
