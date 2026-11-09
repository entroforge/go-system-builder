package cli_test

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// The upstream Plan/dispatch are fixtures. Both professional answers go through
// the public native Stop, original Result consumer and real TR-009; no test
// writes a clean label or snapshot to stand in for the final received result.
func TestAcceptanceExternalS7LastResultAndGuidance(t *testing.T) {
	binary := factory.BuildHarness(t)
	for _, version := range []string{"1.2.0", "1.3.0"} {
		t.Run(version, func(t *testing.T) {
			root := fixtures.FreshRoot(t)
			state := fixtures.BaseState(t, root, "verification", "planned", 1)
			state["schema_version"] = version
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-native-clean-outlet", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
			plan := minimalReviewPlan(t, root, "review-plan-native-clean-outlet")
			fixtures.WriteState(t, root, state)
			stageCLI(t, "runtime", "workspace", "bind", "--root", root)
			registerReviewPlanNaturally(t, root, state, "review-plan-native-clean-outlet", plan)
			for _, id := range []string{"assignment-dv-1", "assignment-qa-1"} {
				markAssignmentDispatched(t, root, id, "reviewer-"+id)
			}
			current := stageSnapshot(t, root)
			for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
				a := raw.(map[string]any)
				a["dispatch_mode"] = "plan_checkpoint"
				if version == "1.3.0" {
					if err := workstate.RecordAgentActivity(current.State, a, "reading", "workgroup_registered"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if version == "1.3.0" {
				for _, raw := range current.State["review"].(map[string]any)["assignments"].(map[string]any) {
					if err := workstate.RecordReviewAssignmentActivity(current.State, raw.(map[string]any), "dispatched", "workgroup_registered"); err != nil {
						t.Fatal(err)
					}
				}
			}
			stageJSON(t, root, ".claude/loop-state.json", current.State)
			var packets []workpacket.ReviewResultPacket
			if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil || len(packets) != 2 {
				t.Fatalf("packets: %v %d", err, len(packets))
			}
			for i, packet := range packets {
				var claims []any
				for _, id := range packet.Binding.ClaimIDs {
					claims = append(claims, map[string]any{"claim_id": id, "conclusion": "pass", "method": "read actual frozen fixture", "observed": "the frozen fixture subject satisfies this assigned check", "evidence_refs": []string{}})
				}
				packet.Draft.Answer, _ = json.Marshal(map[string]any{"claim_results": claims, "blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass"})
				stageJSON(t, root, packet.AnswerPath, packet.Draft)
				ref := filepath.Join(root, packet.ViewPath)
				code, _, diagnostic := acceptanceNativeSidechainEvent(t, binary, root, "SubagentStop", "opaque-"+packet.Binding.OwnerAgentID, ref)
				if code != 0 || !strings.Contains(diagnostic, "original consumer") {
					t.Fatalf("native reception: %d %s", code, diagnostic)
				}
				after := stageSnapshot(t, root)
				if i < len(packets)-1 {
					if after.State["lifecycle"].(map[string]any)["state"] != "verification" {
						t.Fatal("partial coverage advanced the round")
					}
					// The sibling answer is private and unreceived. Its contents
					// must not affect Main's read-only responsibility projection.
					sibling := packets[i+1]
					answer := filepath.Join(root, sibling.AnswerPath)
					original, err := os.ReadFile(answer)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(answer, []byte("PRIVATE-UNRECEIVED-ANSWER invalid JSON"), 0600); err != nil {
						t.Fatal(err)
					}
					text := acceptanceMainNativeReturn(t, binary, root, ref)
					for _, want := range []string{`"accepted":true`, `"work_complete":true`, `"waiting_on":[]`, sibling.Binding.AssignmentID, sibling.Binding.OwnerAgentID, filepath.Join(root, sibling.ViewPath), answer, "does not prove a native Reviewer is running"} {
						if !strings.Contains(text, want) {
							t.Fatalf("partial Main return missing %q: %s", want, text)
						}
					}
					if strings.Contains(text, "PRIVATE-UNRECEIVED-ANSWER") || stageSnapshot(t, root).Revision != after.Revision {
						t.Fatalf("Main projection read private contents or changed Runtime: %s", text)
					}
					if err := os.WriteFile(answer, original, 0600); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if after.State["lifecycle"].(map[string]any)["state"] != "acceptance" {
					t.Fatalf("last Result did not enter S10: %s", diagnostic)
				}
				// The current S10 outlet addresses the active review by its
				// original Plan identity instead of a numeric round
				// (internal/acceptance/review_identity.go:7-22
				// "New responsibilities carry no numeric review_round";
				// internal/cli/audit_questionnaire.go:328-338).
				path := filepath.Join(root, "s10", after.State["runtime_id"].(string), "g1", "review-plan-native-clean-outlet", "acceptance-manifest.json")
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("same outlet did not prepare S10: %v; %s", err, diagnostic)
				}
				beforeMain := after.Revision
				text := acceptanceMainNativeReturn(t, binary, root, ref)
				for _, want := range []string{`"accepted":true`, "TR-009", path} {
					if !strings.Contains(text, want) {
						t.Fatalf("Main missing %q: %s", want, text)
					}
				}
				if stageSnapshot(t, root).Revision != beforeMain {
					t.Fatal("Main return projection changed Runtime")
				}
				if err := acceptanceReturnContract(text, packet.ID, path); err != nil {
					t.Fatal(err)
				}
				for name, changed := range map[string]string{
					"missing-next-path":                   strings.ReplaceAll(text, path, ""),
					"wrong-reception":                     strings.ReplaceAll(text, `"accepted":true`, `"accepted":false`),
					"wrong-packet":                        strings.ReplaceAll(text, packet.ID, "old-packet"),
					"extra-author-save":                   text + " save the unchanged file again",
					"false-receipt-with-positive-comment": strings.ReplaceAll(text, `"accepted":true`, `"accepted":false`) + ` The documentation mentions "accepted":true.`,
				} {
					if err := acceptanceReturnContract(changed, packet.ID, path); err == nil {
						t.Fatalf("guidance oracle missed mutation %s", name)
					}
				}

				if code, _, diagnostic := acceptanceNativeReviewEvent(t, binary, root, "SubagentStop", "opaque-replay", ref); code != 0 {
					t.Fatalf("replay: %d %s", code, diagnostic)
				}
				if stageSnapshot(t, root).Revision != beforeMain {
					t.Fatal("late return repeated transition")
				}
			}
		})
	}
}

// Calls the actual production entrypoint including its own process watchdog.
func acceptanceNativeReviewEvent(t *testing.T, binary, root, event, actor, ref string) (int, string, string) {
	t.Helper()
	started := time.Now()
	code, out, diagnostic := nativeReviewEvent(t, root, event, actor, ref, binary)
	t.Logf("external event=%s elapsed_ms=%d code=%d stdout=%s stderr=%s", event, time.Since(started).Milliseconds(), code, out, diagnostic)
	return code, out, diagnostic
}

// acceptanceNativeSidechainEvent delivers a native event the way the platform
// does: transcript_path names Main's session while the actor's own task lives
// in its sibling subagents transcript.
//
// The current contract resolves an issued S7 responsibility from that actor's
// own original task (blueprint/l4-mechanisms/L4-hook-platform-wiring.md:56
// "父会话转录、子会话转录与最后收尾文字不同"; :122 "成功 PostToolUse 和 Stop／
// SubagentStop 复用同一消费者兜底"), so the exact original reference must be
// present in the actor's own task prompt.
func acceptanceNativeSidechainEvent(t *testing.T, binary, root, event, actor, ref string) (int, string, string) {
	t.Helper()
	rel := ref
	if strings.HasPrefix(ref, root+string(filepath.Separator)) {
		rel = strings.TrimPrefix(ref, root+string(filepath.Separator))
	}
	parent := filepath.Join(t.TempDir(), "parent-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": "Source root: `" + root + "`. Original questionnaire: `" + rel + "`."}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte(`{"type":"user","message":{"role":"user","content":"Main's own unrelated conversation"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "agent_id": actor, "session_id": "native-review-parent", "agent_type": "professional-reviewer", "cwd": root, "transcript_path": parent, "agent_transcript_path": child, "last_assistant_message": ref, "tool_name": "Write", "tool_input": map[string]any{"file_path": ref}, "tool_response": map[string]any{"filePath": ref}})
	started := time.Now()
	code, out, diagnostic := factory.RunHook(t, binary, root, event, body)
	t.Logf("external event=%s elapsed_ms=%d code=%d stdout=%s stderr=%s", event, time.Since(started).Milliseconds(), code, out, diagnostic)
	return code, out, diagnostic
}

func acceptanceMainNativeReturn(t *testing.T, binary, root, ref string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Agent", "tool_input": map[string]any{"prompt": ref}, "tool_response": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Finished."}}}})
	if err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root).Revision
	code, out, diagnostic := factory.RunHook(t, binary, root, "PostToolUse", body)
	if code != 0 {
		t.Fatalf("external Main return: %d %s %s", code, out, diagnostic)
	}
	var envelope struct {
		Output struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.Output.Event != "PostToolUse" || envelope.Output.Context == "" {
		t.Fatalf("model channel absent: %v %s %s", err, out, diagnostic)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("read-only Main return received or advanced work")
	}
	t.Logf("actual Main model context: %s", envelope.Output.Context)
	return envelope.Output.Context
}

// An independent delivery assertion; it does not call a prompt generator.
func acceptanceReturnContract(text, packet, next string) error {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return fmt.Errorf("model context has no original receipt")
	}
	var receipt struct {
		PacketID     string          `json:"packet_id"`
		Accepted     bool            `json:"accepted"`
		WorkComplete bool            `json:"work_complete"`
		CurrentStage string          `json:"current_stage"`
		WaitingOn    json.RawMessage `json:"waiting_on"`
		NextAction   string          `json:"next_action"`
	}
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&receipt); err != nil {
		return fmt.Errorf("model context receipt: %w", err)
	}
	if receipt.PacketID != packet || !receipt.Accepted || !receipt.WorkComplete || receipt.CurrentStage != "S10" || strings.TrimSpace(string(receipt.WaitingOn)) != "[]" {
		return fmt.Errorf("model receipt does not describe this completed original responsibility: %+v", receipt)
	}
	for _, required := range []string{next, "TR-009"} {
		if !strings.Contains(receipt.NextAction, required) {
			return fmt.Errorf("original receipt next action lacks %q", required)
		}
	}
	for _, forbidden := range []string{"save the unchanged file again", "change the artifact id", "wait for the next tool"} {
		if strings.Contains(text, forbidden) {
			return fmt.Errorf("author chore in delivery: %q", forbidden)
		}
	}
	return nil
}
