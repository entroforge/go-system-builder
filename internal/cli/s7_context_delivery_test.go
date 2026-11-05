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

// s7NativeChildTranscript writes the platform's real child session transcript
// for one actor. The actor's original task lives in that transcript, so the
// hook identifies the actor's own original responsibility from the platform's
// own file instead of guessing it from a platform actor string.
// L4-hook-platform-wiring.md — a native return identifies its original
// responsibility from the actor's own original task.
func s7NativeChildTranscript(t *testing.T, root, actor, task string) string {
	t.Helper()
	path := filepath.Join(root, ".claude", "transcripts", "agent-"+actor+".jsonl")
	row, err := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": actor, "message": map[string]any{"role": "user", "content": task}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestS7ExactNativeStartAndChangedMainContext(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-guidance", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	plan := minimalReviewPlan(t, root, "review-plan-guidance")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	// The plan reaches the original consumer through the natural save event.
	// There is no separate plan registration or submission command.
	planPath := review.PlanQuestionnairePath(root, stageSnapshot(t, root).State)
	if planPath == "" {
		t.Fatal("no original ReviewPlan questionnaire path")
	}
	entryQuestionnaireHook(t, root, "PreToolUse", planPath)
	planBytes, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if out := entryQuestionnaireHook(t, root, "PostToolUse", planPath); !strings.Contains(out, "review-plan: registered") {
		t.Fatalf("original Main plan not received: %s", out)
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-guidance-dv")
	markAssignmentDispatched(t, root, "assignment-qa-1", "reviewer-guidance-qa")
	registered := stageSnapshot(t, root)
	for _, raw := range registered.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", registered.State)

	var packets []workpacket.ReviewResultPacket
	issued := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")
	if err := json.Unmarshal([]byte(issued), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("issue: %s %v", issued, err)
	}
	before := stageSnapshot(t, root).Revision
	call := func(event, agent, text string) string {
		t.Helper()
		payload := map[string]any{"hook_event_name": event, "agent_id": agent, "session_id": "s7-native", "cwd": root, "last_assistant_message": text}
		if event == "SubagentStart" {
			// A real platform Start carries the child session's own transcript.
			payload["agent_transcript_path"] = s7NativeChildTranscript(t, root, agent, text)
		}
		body, _ := json.Marshal(payload)
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("%s: %d %s %s", event, code, &out, &diagnostic)
		}
		return out.String()
	}
	ref := filepath.Join(root, packets[0].ViewPath)
	for _, agent := range []string{"Explore", "qa", "unknown-native"} {
		if got := call("SubagentStart", agent, "Explore the project"); got != "" {
			t.Fatalf("unknown Start got formal questionnaire: %s", got)
		}
	}
	for _, tc := range []struct{ agent, text string }{{"opaque-native", ref}, {packets[0].Binding.OwnerAgentID, ref}, {packets[1].Binding.OwnerAgentID, ref}} {
		if got := call("SubagentStart", tc.agent, tc.text); !strings.Contains(got, packets[0].ViewPath) || !strings.Contains(got, packets[0].AnswerPath) {
			t.Fatalf("exact task reference lost to a platform actor string: %s", got)
		}
	}
	if got := call("SubagentStart", packets[0].Binding.OwnerAgentID, "continue assigned work"); strings.Contains(got, packets[0].AnswerPath) {
		t.Fatalf("platform actor string alone claimed an engineering responsibility: %s", got)
	}
	// The handoff mismatch must not hand this Worker another responsibility.
	// The ordinary tool event still carries exactly its own original
	// responsibility and nothing of the mismatched one.
	mismatched := call("PreToolUse", packets[1].Binding.OwnerAgentID, ref)
	if !strings.Contains(mismatched, packets[1].ViewPath) || !strings.Contains(mismatched, packets[1].AnswerPath) {
		t.Fatalf("the ordinary worker tool lost its own original responsibility: %s", mismatched)
	}
	if strings.Contains(mismatched, packets[0].ViewPath) || strings.Contains(mismatched, packets[0].AnswerPath) {
		t.Fatalf("ordinary worker tool was affected by a handoff mismatch: %s", mismatched)
	}
	if got := call("SubagentStart", "opaque-native", ref+"\n"+filepath.Join(root, packets[1].ViewPath)); got != "" {
		t.Fatalf("ambiguous Start selected responsibility: %s", got)
	}
	first := designGuidanceEvent(t, root, "s7-context", "PreToolUse", "Read")
	for _, tool := range []string{"Read", "Bash", "Write", "mcp__sandbox__execute"} {
		next := designGuidanceEvent(t, root, "s7-context", "PreToolUse", tool)
		if !strings.Contains(next, "S7 guidance is unchanged") || len(next) >= len(first)/2 {
			t.Fatalf("%s repeated S7 guidance: first=%d next=%d %s", tool, len(first), len(next), next)
		}
	}
	// Stop stores its actual rejection. Main observation must not re-read the
	// private draft to replace that cause with a fresh validation.
	call("SubagentStop", "opaque-native", ref)
	changed := designGuidanceEvent(t, root, "s7-context", "PreToolUse", "Read")
	if strings.Contains(changed, "guidance is unchanged") || !strings.Contains(changed, "Last S7 native Stop") || !strings.Contains(changed, packets[0].ID) {
		t.Fatalf("new Stop error hidden: %s", changed)
	}
	if err := os.WriteFile(filepath.Join(root, packets[0].AnswerPath), []byte("private unreadable draft"), 0600); err != nil {
		t.Fatal(err)
	}
	// Main must answer from the recorded original reception fact. Reading the
	// person's own answer along the registered path is a current observation
	// only (L4-workflow-convergence.md:217); it never becomes a reception.
	observed := mainNativeReturn(t, root, ref)
	if !strings.Contains(observed, "Last S7 native Stop") {
		t.Fatalf("Main lost the recorded original Stop fact: %s", observed)
	}
	for _, forbidden := range []string{`"accepted":true`, `"work_complete":true`, "已完成本人责任"} {
		if strings.Contains(observed, forbidden) {
			t.Fatalf("Main read the private unaccepted draft as a reception: %s", observed)
		}
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		designGuidanceEvent(t, root, "s7-context", event, "Read")
		restored := designGuidanceEvent(t, root, "s7-context", "PreToolUse", "Read")
		if strings.Contains(restored, "guidance is unchanged") || !strings.Contains(restored, packets[0].ViewPath) || !strings.Contains(restored, "Last S7 native Stop") {
			t.Fatalf("%s lost recovery context: %s", event, restored)
		}
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("presentation or rejected Stop changed Runtime")
	}
}
