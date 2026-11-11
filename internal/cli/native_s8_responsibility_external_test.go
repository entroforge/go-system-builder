package cli_test

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/submission"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS8NativeExplicitIdentityReceivesOriginalAnswer(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, packet := minimalAnswerInvestigator(t)
			packet.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The original observation remains inconclusive."}`)
			stageJSON(t, root, packet.AnswerPath, packet.Draft)
			task := "Investigate `" + packet.Binding.HypothesisID + "` (owner `" + packet.Binding.OwnerAgentID + "`). Original path: .claude/wrong/answer.json"
			payload := s7ListenerPayload(t, "opaque-investigator-"+event, task)
			payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
			payload["last_assistant_message"] = "My own investigation is complete."
			s7ListenerEvent(t, root, event, payload)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			got, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
			if err != nil || !got.Accepted {
				t.Fatalf("%s did not receive its original answer: %+v %v", event, got, err)
			}
		})
	}
}

func TestS8CompletedMainReturnReceivesOnlyItsOriginalResponsibility(t *testing.T) {
	root, packet := minimalAnswerInvestigator(t)
	packet.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The original observation remains inconclusive."}`)
	stageJSON(t, root, packet.AnswerPath, packet.Draft)
	task := "Investigate `" + packet.Binding.HypothesisID + "` (owner `" + packet.Binding.OwnerAgentID + "`). Original path: .claude/wrong/answer.json"
	out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
	if !strings.Contains(out, "Native return") {
		t.Fatalf("completed original return: %s %s", out, diagnostic)
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
	if err != nil || !got.Accepted {
		t.Fatalf("original return did not receive its answer: %+v %v %s", got, err, out)
	}
}

func TestS7CoordinatorMessageCannotReceiveAnotherAuthorsAnswer(t *testing.T) {
	for _, mention := range []string{"other responsibility", "same responsibility"} {
		t.Run(mention, func(t *testing.T) {
			root, packets := minimalAnswerReviewResponsibilities(t)
			for _, packet := range packets {
				s7ListenerAnswer(t, root, packet)
			}
			payload := s7ListenerPayload(t, "platform-reviewer", "Review original "+filepath.Join(root, packets[0].ViewPath))
			child := filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+payload["agent_id"].(string)+".jsonl")
			mentioned := packets[1].AnswerPath
			if mention == "same responsibility" {
				mentioned = packets[0].AnswerPath
			}
			row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "isMeta": true, "agentId": "platform-reviewer", "origin": map[string]any{"kind": "coordinator"}, "message": map[string]any{"role": "user", "content": "The coordinator sent a message: inspect " + filepath.Join(root, mentioned)}})
			f, err := os.OpenFile(child, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write(append(row, '\n')); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			payload["agent_transcript_path"] = child
			payload["last_assistant_message"] = "I saw " + filepath.Join(root, packets[1].AnswerPath)
			s7ListenerEvent(t, root, "PostToolUse", payload)
			if got := minimalAnswerReviewReceipt(t, root, packets[0]); !got.Accepted {
				t.Fatalf("original responsibility was not received: %+v", got)
			}
			if got := minimalAnswerReviewReceipt(t, root, packets[1]); got.Accepted {
				t.Fatalf("coordinator/final message received another author's answer: %+v", got)
			}
		})
	}
}

func TestS8OriginalPathAllowsOtherContextButNotExplicitConflict(t *testing.T) {
	for _, tc := range []struct {
		name, suffix string
		want         bool
	}{
		{"ordinary context", " Compare hypothesis-other and the notes from owner `investigator-other`.", true},
		{"explicit different dispatch", " Investigate `hypothesis-other` (owner `investigator-other`).", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, packet := minimalAnswerInvestigator(t)
			packet.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The original observation remains inconclusive."}`)
			stageJSON(t, root, packet.AnswerPath, packet.Draft)
			payload := s7ListenerPayload(t, "s8-context-"+tc.name, "Read the original "+filepath.Join(root, packet.ViewPath)+"."+tc.suffix)
			s7ListenerEvent(t, root, "PreToolUse", payload)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			got, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
			if err != nil || got.Accepted != tc.want {
				t.Fatalf("accepted=%v want=%v: %+v %v", got.Accepted, tc.want, got, err)
			}
		})
	}
}

func TestS7ExplicitAssignedOwnerSeparatesBackgroundOriginalQuestion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label bool
		want  bool
	}{
		{"assigned owner and background", true, true},
		{"two unlabelled questions", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, packets := minimalAnswerReviewResponsibilities(t)
			for _, packet := range packets {
				s7ListenerAnswer(t, root, packet)
			}
			task := "Review " + filepath.Join(root, packets[0].ViewPath) + "; compare " + filepath.Join(root, packets[1].ViewPath)
			if tc.label {
				task = "Assigned owner `" + packets[0].Binding.OwnerAgentID + "`. " + task
			}
			payload := s7ListenerPayload(t, "platform-owner-test", task)
			s7ListenerEvent(t, root, "PreToolUse", payload)
			if got := minimalAnswerReviewReceipt(t, root, packets[0]); got.Accepted != tc.want {
				t.Fatalf("first accepted=%v want=%v", got.Accepted, tc.want)
			}
			if got := minimalAnswerReviewReceipt(t, root, packets[1]); got.Accepted {
				t.Fatal("background responsibility was received")
			}
		})
	}
}

// The packet is issued through the original S7→S8 flow. The later lifecycle
// cursor is a test fixture, not a claim of real approval or release.
func TestS8IssuedAnswerNaturalHookAfterStageChange(t *testing.T) {
	for _, tc := range []struct{ name, state, phase string }{
		{"s9_planning", "bug_resolution", "planning"},
		{"fixing", "bug_resolution", "fixing"},
		{"acceptance", "acceptance", ""},
		{"s11", "awaiting_human_release", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, packet := minimalAnswerInvestigator(t)
			before := stageSnapshot(t, root)
			life := before.State["lifecycle"].(map[string]any)
			life["state"] = tc.state
			if tc.phase == "" {
				life["phase"] = nil
			} else {
				life["phase"] = tc.phase
			}
			stageJSON(t, root, ".claude/loop-state.json", before.State)
			packet.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The previously issued investigation remains inconclusive."}`)
			stageJSON(t, root, packet.AnswerPath, packet.Draft)
			task := "Investigate `" + packet.Binding.HypothesisID + "` (owner `" + packet.Binding.OwnerAgentID + "`)"
			payload := s7ListenerPayload(t, "late-actor-"+tc.name, task)
			out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", payload)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			got, err := service.HypothesisResultReceipt(packet.Binding.OwnerAgentID, packet.ID)
			if err != nil || !got.Accepted {
				t.Fatalf("late original answer not received: %+v %v stdout=%s stderr=%s", got, err, out, diagnostic)
			}
			after := stageSnapshot(t, root)
			if after.State["lifecycle"].(map[string]any)["state"] != tc.state {
				t.Fatalf("late original answer moved stage: %#v", after.State["lifecycle"])
			}
		})
	}
}
