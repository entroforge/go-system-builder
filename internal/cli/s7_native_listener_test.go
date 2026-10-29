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

func s7ListenerFixture(t *testing.T) (string, workpacket.ReviewResultPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	plan := minimalReviewPlan(t, root, "review-plan-listener")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	stageCLI(t, "runtime", "review-plan", "--root", root, "--file", plan)
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-listener")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "reviewer-listener" {
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v %v", packets, err)
	}
	return root, packets[0]
}

func s7ListenerAnswer(t *testing.T, root string, p workpacket.ReviewResultPacket) {
	t.Helper()
	var claims []any
	for _, id := range p.Binding.ClaimIDs {
		claims = append(claims, map[string]any{"claim_id": id, "conclusion": "pass", "method": "read original frozen source", "observed": "the actual frozen subject satisfies the assigned claim", "evidence_refs": []string{}})
	}
	p.Draft.Answer, _ = json.Marshal(map[string]any{"claim_results": claims, "blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass"})
	stageJSON(t, root, p.AnswerPath, p.Draft)
}

func s7ListenerEvent(t *testing.T, root, event string, fields map[string]any) (string, string) {
	t.Helper()
	body := map[string]any{"hook_event_name": event, "session_id": "parent", "cwd": root, "tool_name": "Bash", "tool_input": map[string]any{"command": "git status"}}
	for k, v := range fields {
		body[k] = v
	}
	data, _ := json.Marshal(body)
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(data), &out, &diagnostic); code != 0 {
		t.Fatalf("ordinary tool blocked: %d %s %s", code, &out, &diagnostic)
	}
	return out.String(), diagnostic.String()
}

// Match actual Claude Code ordinary child payloads: transcript_path names
// Main, while the actor's own task lives in the sibling subagents directory.
func s7ListenerPayload(t *testing.T, actor, task string) map[string]any {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "parent-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+actor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": task}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte(`{"type":"user","message":{"role":"user","content":"Main's own unrelated conversation"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"agent_id": actor, "agent_type": "reviewer", "transcript_path": parent}
}

func TestS7NativeListenerReceivesAssignedOriginalOnNaturalToolEvent(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		t.Run(event, func(t *testing.T) {
			root, p := s7ListenerFixture(t)
			before := stageSnapshot(t, root).Revision
			payload := s7ListenerPayload(t, p.Binding.OwnerAgentID, "Review "+filepath.Join(root, p.ViewPath))
			// A bound original owner can observe an unrelated ordinary tool, but the
			// untouched issued template cannot manufacture professional acceptance.
			s7ListenerEvent(t, root, event, payload)
			if stageSnapshot(t, root).Revision != before {
				t.Fatal("pristine draft received")
			}
			s7ListenerAnswer(t, root, p)
			out, diagnostic := s7ListenerEvent(t, root, event, payload)
			for _, want := range []string{"additionalContext", "accepted=true", "work_complete=true", p.ID, p.Binding.OwnerAgentID, "Main owns"} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing visible receipt %q: %s", want, out)
				}
			}
			if !strings.Contains(diagnostic, "received by the original consumer") {
				t.Fatal(diagnostic)
			}
			accepted := stageSnapshot(t, root)
			row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)[p.Binding.AssignmentID].(map[string]any)
			if row["result_receipt"] == nil {
				t.Fatal("original receipt missing")
			}
			replay, _ := s7ListenerEvent(t, root, event, payload)
			if replay != "" {
				t.Fatalf("accepted ordinary replay repeated feedback: %s", replay)
			}
			if stageSnapshot(t, root).Revision != accepted.Revision {
				t.Fatal("natural event replay duplicated consequences")
			}
		})
	}
}

func TestS7NativeListenerUsesNestedTaskAndNeverMainToolPath(t *testing.T) {
	root, p := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, p)
	before := stageSnapshot(t, root).Revision
	for _, fields := range []map[string]any{
		{"tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}},
		{"agent_id": "unrelated-worker", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}, "last_assistant_message": filepath.Join(root, p.ViewPath)},
		{"agent_id": p.Binding.OwnerAgentID, "error": "failed"},
		{"agent_id": p.Binding.OwnerAgentID, "tool_response": map[string]any{"is_error": true}},
	} {
		s7ListenerEvent(t, root, "PostToolUse", fields)
		if stageSnapshot(t, root).Revision != before {
			t.Fatal("unproven/failed event received answer")
		}
	}
	for _, failure := range []map[string]any{{"error": "failed"}, {"tool_response": map[string]any{"is_error": true}}} {
		failedPayload := s7ListenerPayload(t, "opaque-native-reviewer", "Review "+filepath.Join(root, p.ViewPath))
		for key, value := range failure {
			failedPayload[key] = value
		}
		s7ListenerEvent(t, root, "PostToolUse", failedPayload)
		if stageSnapshot(t, root).Revision != before {
			t.Fatal("failed real child event received original")
		}
	}
	payload := s7ListenerPayload(t, "opaque-native-reviewer", "Review this exact original: "+filepath.Join(root, p.ViewPath))
	_, diagnostic := s7ListenerEvent(t, root, "PreToolUse", payload)
	if !strings.Contains(diagnostic, "received by the original consumer") {
		t.Fatal(diagnostic)
	}
}

func TestS7NativeListenerReportsOriginalFieldErrorWithoutDenyingTool(t *testing.T) {
	root, p := s7ListenerFixture(t)
	var answer map[string]any
	if err := json.Unmarshal(p.Draft.Answer, &answer); err != nil {
		t.Fatal(err)
	}
	answer["verdict"] = "pass"
	p.Draft.Answer, _ = json.Marshal(answer)
	stageJSON(t, root, p.AnswerPath, p.Draft)
	before := stageSnapshot(t, root).Revision
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, p.Binding.OwnerAgentID, "Review "+filepath.Join(root, p.ViewPath)))
	if !strings.Contains(out, "additionalContext") || !strings.Contains(diagnostic, "/answer/") || !strings.Contains(out, p.Binding.OwnerAgentID) {
		t.Fatalf("missing actionable fields: %s %s", out, diagnostic)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("invalid original accepted")
	}
	// Stop remains the original final fallback and diagnostics outlet.
	if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "opaque-reviewer", filepath.Join(root, p.ViewPath)); code != 0 || !strings.Contains(diagnostic, "Main owns recovery") {
		t.Fatalf("stop fallback: %d %s", code, diagnostic)
	}
}

func TestS7NativeListenerCannotBorrowParentOrWrongSidechain(t *testing.T) {
	root, p := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, p)
	before := stageSnapshot(t, root).Revision
	payload := s7ListenerPayload(t, "opaque-native-reviewer", "Unrelated native work")
	parent := payload["transcript_path"].(string)
	row, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "Main mentioned " + filepath.Join(root, p.ViewPath)}})
	if err := os.WriteFile(parent, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		s7ListenerEvent(t, root, event, payload)
		if stageSnapshot(t, root).Revision != before {
			t.Fatal("borrowed Main task for unrelated child")
		}
	}
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-opaque-native-reviewer.jsonl")
	for _, identity := range []map[string]any{
		{"isSidechain": false, "agentId": "opaque-native-reviewer"},
		{"isSidechain": true, "agentId": "different-child"},
	} {
		identity["type"] = "user"
		identity["message"] = map[string]any{"role": "user", "content": filepath.Join(root, p.ViewPath)}
		data, _ := json.Marshal(identity)
		if err := os.WriteFile(child, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		s7ListenerEvent(t, root, "PreToolUse", payload)
		if stageSnapshot(t, root).Revision != before {
			t.Fatal("mismatched sidechain selected original")
		}
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	s7ListenerEvent(t, root, "PreToolUse", payload)
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("missing child borrowed parent task")
	}
}
