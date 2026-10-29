package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func nativeContinuationFixture(t *testing.T) ([]nativeLiveObservation, []byte) {
	t.Helper()
	launch := map[string]any{"isolation": "worktree", "prompt": "exact brief"}
	send := map[string]any{"to": "original", "message": "continue exact brief and unchanged Result"}
	var rows []nativeLiveObservation
	for i, call := range []nativeLiveCall{{"launch", "Agent", launch}, {"send", "SendMessage", send}} {
		at := int64(1 + i*10)
		for _, event := range []string{"PreToolUse", "PostToolUse"} {
			response := map[string]any{"agentId": "original", "worktreePath": "/repo/worktree"}
			if i == 1 {
				response = map[string]any{"success": true, "resumedAgentId": "original"}
			}
			rows = append(rows, nativeLiveObservation{Event: event, Started: at, Finished: at, Payload: map[string]any{"tool_use_id": call.id, "tool_name": call.name, "tool_input": call.input, "tool_response": response}})
		}
		for j, event := range []string{"SubagentStart", "SubagentStop"} {
			rows = append(rows, nativeLiveObservation{Event: event, Started: at + int64(1+j*3), Finished: at + int64(1+j*3), Payload: map[string]any{"agent_id": "original", "cwd": "/repo/worktree"}})
		}
	}
	var stream []byte
	appendFrame := func(row any) {
		b, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		stream = append(stream, b...)
		stream = append(stream, '\n')
	}
	for i, call := range []nativeLiveCall{{"launch", "Agent", launch}, {"send", "SendMessage", send}} {
		appendFrame(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": call.id, "name": call.name, "input": call.input}}}})
		run := "first"
		if i == 1 {
			run = "continued"
		}
		appendFrame(map[string]any{"type": "system", "subtype": "task_started", "task_id": "original", "run_id": run, "tool_use_id": call.id, "is_backgrounded": i == 1})
		appendFrame(map[string]any{"type": "system", "subtype": "task_notification", "task_id": "original", "run_id": run, "tool_use_id": call.id, "status": "completed", "summary": "exact brief and Result returned"})
	}
	return rows, stream
}

func TestNativeContinuationRequiresActualSameActorStopAndNotification(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]nativeLiveObservation, []byte) ([]nativeLiveObservation, []byte)
		want   string
	}{
		{"valid", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) { return r, s }, ""},
		{"model resume discarded by actual payload", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			return r, []byte(strings.Replace(string(s), `"isolation":"worktree"`, `"isolation":"worktree","resume":"original"`, 1))
		}, "Agent.resume"},
		{"replacement actor", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			r[6].Payload["agent_id"] = "replacement"
			return r, s
		}, "new actor"},
		{"worktree replaced", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			r[6].Payload["cwd"] = "/repo/replacement"
			return r, s
		}, "changed actual worktree"},
		{"wrong target", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			r[4].Payload["tool_input"].(map[string]any)["to"] = "other"
			return r, []byte(strings.ReplaceAll(string(s), `"to":"original"`, `"to":"other"`))
		}, "address original actor"},
		{"actual target differs", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			r[4].Payload["tool_input"].(map[string]any)["to"] = "other"
			return r, s
		}, "differs from model request"},
		{"actual response resumed wrong actor", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			r[5].Payload["tool_response"].(map[string]any)["resumedAgentId"] = "replacement"
			return r, s
		}, "did not resume original actor"},
		{"instant success without Stop", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) { return r[:len(r)-1], s }, "incomplete same-actor lifecycle"},
		{"success without completion notification", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			return r, []byte(strings.ReplaceAll(string(s), `"status":"completed"`, `"status":"stopped"`))
		}, "no real completed task_notification"},
		{"notification for wrong run", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			lines := strings.Split(string(s), "\n")
			lines[5] = strings.ReplaceAll(lines[5], `"run_id":"continued"`, `"run_id":"first"`)
			return r, []byte(strings.Join(lines, "\n"))
		}, "no real completed task_notification"},
		{"missing actual SendMessage payload", func(r []nativeLiveObservation, s []byte) ([]nativeLiveObservation, []byte) {
			return append(r[:4], r[6:]...), s
		}, "Hook pair"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, stream := nativeContinuationFixture(t)
			rows, stream = tc.mutate(rows, stream)
			err := checkNativeLiveContinuation(rows, stream, true)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestNativeInitialReturnDoesNotRequireContinuation(t *testing.T) {
	rows, stream := nativeContinuationFixture(t)
	lines := strings.Split(string(stream), "\n")
	if err := checkNativeLiveContinuation(rows[:4], []byte(strings.Join(lines[:3], "\n")), false); err != nil {
		t.Fatal(err)
	}
}

func nativeHandoffFixture(t *testing.T) ([]nativeLiveObservation, []byte, nativeLiveHandoffExpectation) {
	t.Helper()
	rows, stream := nativeContinuationFixture(t)
	want := nativeLiveHandoffExpectation{
		Actor: "original", Run: "continued", ToolID: "send", Owner: "builder-original",
		Assignment: "assignment-original", InputRef: "/repo/.claude/original/brief.md",
		Accepted: "The original PlanReport is accepted", Failure: "original Result source missing: /repo/worktree/evidence.txt",
		Command:      "'/repo/.claude/bin/loop-harness' runtime stage-answer consume --root '/repo' --path '/repo/worktree/result.json'",
		Continuation: true,
	}
	context := strings.Join([]string{want.Owner, want.Assignment, want.InputRef, want.Accepted, want.Failure, want.Command}, "\n")
	encoded, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "additionalContext": context}})
	if err != nil {
		t.Fatal(err)
	}
	for i, call := range []nativeLiveCall{
		{"handoff-read", "Read", map[string]any{"file_path": "/repo/.claude/loop-state.json"}},
		{"main-consume", "Bash", map[string]any{"command": want.Command}},
	} {
		frame, err := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": call.id, "name": call.name, "input": call.input}}}})
		if err != nil {
			t.Fatal(err)
		}
		stream = append(stream, frame...)
		stream = append(stream, '\n')
		for j, event := range []string{"PreToolUse", "PostToolUse"} {
			at := int64(20 + i*10 + j)
			stdout := ""
			if i == 0 && j == 0 {
				stdout = string(encoded)
			}
			rows = append(rows, nativeLiveObservation{Event: event, Started: at, Finished: at, Stdout: stdout, Payload: map[string]any{
				"tool_use_id": call.id, "tool_name": call.name, "tool_input": call.input, "tool_response": map[string]any{"exit_code": 0},
			}})
		}
	}
	return rows, stream, want
}

func TestNativeHandoffRequiresDeliveredContextAndActualMainAction(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]nativeLiveObservation, []byte, *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte)
	}{
		{"only user systemMessage", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			user, _ := json.Marshal(map[string]any{"type": "user", "systemMessage": r[8].Stdout})
			s = append(append(s, user...), '\n')
			r[8].Stdout = ""
			return r, s
		}},
		{"missing exact owner", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Stdout = strings.ReplaceAll(r[8].Stdout, w.Owner, "another-owner")
			return r, s
		}},
		{"missing original input", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Stdout = strings.ReplaceAll(r[8].Stdout, w.InputRef, "/repo/neighbor/brief.md")
			return r, s
		}},
		{"missing exact assignment", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Stdout = strings.ReplaceAll(r[8].Stdout, w.Assignment, "another-assignment")
			return r, s
		}},
		{"missing accepted fact", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Stdout = strings.ReplaceAll(r[8].Stdout, w.Accepted, "unknown receipt")
			return r, s
		}},
		{"missing concrete failure", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Stdout = strings.ReplaceAll(r[8].Stdout, w.Failure, "unknown failure")
			return r, s
		}},
		{"wrong notification actor", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			return r, []byte(strings.ReplaceAll(string(s), `"task_id":"original"`, `"task_id":"other"`))
		}},
		{"stale notification run", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			w.Run = "first"
			return r, s
		}},
		{"stale Main event", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Started = 2
			return r, s
		}},
		{"Worker context cannot stand for Main", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Payload["agent_id"] = "original"
			return r, s
		}},
		{"unmatched Main payload", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[8].Payload["tool_use_id"] = "unrelated"
			return r, s
		}},
		{"action lacks actual Hooks", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			return r[:10], s
		}},
		{"actual action wrong responsibility", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[10].Payload["tool_input"].(map[string]any)["command"] = "echo no-consumer"
			return r, s
		}},
		{"command appears only as echo text", func(r []nativeLiveObservation, s []byte, w *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			return r, []byte(strings.ReplaceAll(string(s), `"command":"'`, `"command":"echo '`))
		}},
		{"action failed", func(r []nativeLiveObservation, s []byte, _ *nativeLiveHandoffExpectation) ([]nativeLiveObservation, []byte) {
			r[11].Payload["tool_response"].(map[string]any)["exit_code"] = 1
			return r, s
		}},
	}
	t.Run("valid recovery", func(t *testing.T) {
		r, s, w := nativeHandoffFixture(t)
		if err := checkNativeLiveHandoff(r, s, w); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, s, w := nativeHandoffFixture(t)
			r, s = tc.mutate(r, s, &w)
			if err := checkNativeLiveHandoff(r, s, w); err == nil {
				t.Fatal("invalid handoff evidence was accepted")
			}
		})
	}
}
