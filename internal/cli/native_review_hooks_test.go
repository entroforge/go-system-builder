package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
)

func nativeReviewEvent(t *testing.T, root, event, actor, ref string, binary ...string) (int, string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "agent_id": actor, "session_id": "native-review-parent", "agent_type": "professional-reviewer", "cwd": root, "last_assistant_message": ref, "tool_name": "Write", "tool_input": map[string]any{"file_path": ref}, "tool_response": map[string]any{"filePath": ref}})
	if len(binary) > 0 {
		return factory.RunHook(t, binary[0], root, event, body)
	}
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}

func TestNativeS5IndependentAnswersReachOriginalGateWithoutPrepareOrActivation(t *testing.T) {
	f := newStageAnswerFixture(t)
	for index, p := range f.packets {
		actor := []string{"opaque-document-reviewer-one", "opaque-document-reviewer-two"}[index]
		if code, out, diagnostic := nativeReviewEvent(t, f.root, "SubagentStart", actor, ""); code != 0 || !strings.Contains(out, "original questionnaire") {
			t.Fatalf("Start: %d %s %s", code, out, diagnostic)
		}
		stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
		before := stageSnapshot(t, f.root)
		for _, event := range []string{"PreToolUse", "PostToolUse", "Stop", "TeammateIdle"} {
			if code, out, diagnostic := nativeReviewEvent(t, f.root, event, actor, filepath.Join(f.root, p.AnswerPath)); code != 0 || out != "" {
				t.Fatalf("ordinary %s: %d %s %s", event, code, out, diagnostic)
			}
			if stageSnapshot(t, f.root).Revision != before.Revision {
				t.Fatalf("ordinary %s managed execution", event)
			}
		}
		if code, out, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", actor, filepath.Join(f.root, p.ViewPath)); code != 0 || out != "" || !strings.Contains(diagnostic, "original consumer") {
			t.Fatalf("Stop: %d %s %s", code, out, diagnostic)
		}
		snapshot := stageSnapshot(t, f.root)
		record, accepted, err := runtime.FindSubmission(snapshot.State, p.ID)
		if err != nil || !accepted || record.ContentHash == "" {
			t.Fatalf("original S5 Writer did not receive answer: %+v %v", record, err)
		}
		if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", actor, filepath.Join(f.root, p.ViewPath)); code != 0 {
			t.Fatalf("accepted return replay: %d %s", code, diagnostic)
		}
		if replay := stageSnapshot(t, f.root); replay.Revision != snapshot.Revision {
			t.Fatal("accepted return replay created another evidence or transition")
		}
		if executions, _ := snapshot.State["workspace"].(map[string]any)["executions"].(map[string]any); len(executions) != 0 {
			t.Fatal("native independent context created a framework execution")
		}
		for _, path := range []string{".claude/workspace-launch", ".claude/loop-bootstrap.json"} {
			if _, err := os.Stat(filepath.Join(f.root, path)); !os.IsNotExist(err) {
				t.Fatalf("unexpected %s", path)
			}
		}
	}
	if got := stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"]; got != "building" {
		t.Fatalf("original S5 pass gate did not reach S6: %v", got)
	}
}

func TestNativeS5NamedRevisionDoesNotConfuseOriginalAndResumedReferences(t *testing.T) {
	f := newStageAnswerFixture(t)
	old := f.packets[0]
	stageJSON(t, f.root, old.AnswerPath, completedDraft(old, "pass"))
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "native-document-actor", filepath.Join(f.root, old.ViewPath)); code != 0 {
		t.Fatal(diagnostic)
	}
	snapshot := stageSnapshot(t, f.root)
	previous, accepted, err := runtime.FindSubmission(snapshot.State, old.ID)
	if err != nil || !accepted {
		t.Fatalf("original review missing: %v", err)
	}
	data := stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", previous.Key, "--reason", "Recheck the actual dependency boundary")
	var next workpacket.Packet
	if err := json.Unmarshal([]byte(data), &next); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, f.root, next.AnswerPath, completedDraft(next, "pass"))
	text := filepath.Join(f.root, old.ViewPath) + "\n" + filepath.Join(f.root, next.AnswerPath)
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "different-platform-actor-on-resume", text); code != 0 || !strings.Contains(diagnostic, "original consumer") {
		t.Fatalf("current named revision was confused with retired initial prompt: %d %s", code, diagnostic)
	}
	snapshot = stageSnapshot(t, f.root)
	if _, accepted, err := runtime.FindSubmission(snapshot.State, next.ID); err != nil || !accepted {
		t.Fatalf("revised original Writer receipt missing: %v", err)
	}
}

func TestNativeS5AmbiguousReferencesDoNotReceiveEitherAnswer(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root).Revision
	text := filepath.Join(f.root, f.packets[0].ViewPath) + "\n" + filepath.Join(f.root, f.packets[1].ViewPath)
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "native-actor", text); code != 0 || !strings.Contains(diagnostic, "multiple exact") {
		t.Fatalf("ambiguous task was claimed: %d %s", code, diagnostic)
	}
	if stageSnapshot(t, f.root).Revision != before {
		t.Fatal("ambiguous return changed Runtime")
	}
}

func TestNativeS5MainToolsCannotSweepOrExplicitlyReceiveReadyWorkerAnswer(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	answer := filepath.Join(f.root, p.AnswerPath)
	before := stageSnapshot(t, f.root)
	for _, tc := range []struct {
		tool  string
		input map[string]any
	}{
		{"SendMessage", map[string]any{"to": "opaque-native-reviewer", "message": "Return your mock answer at " + answer}},
		{"Bash", map[string]any{"command": "cat " + answer}},
		{"Write", map[string]any{"file_path": answer}},
		{"mcp__fixture__save", map[string]any{"file_path": answer}},
	} {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "native-parent", "cwd": f.root, "tool_name": tc.tool, "tool_input": tc.input, "tool_response": map[string]any{"file_path": answer}})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("Main %s: %d %s %s", tc.tool, code, &out, &diagnostic)
		}
		snapshot := stageSnapshot(t, f.root)
		if _, accepted, err := runtime.FindSubmission(snapshot.State, p.ID); err != nil || accepted || snapshot.Revision != before.Revision {
			t.Fatalf("Main %s received a Worker answer before native Stop: accepted=%t revision=%d/%d err=%v stdout=%s stderr=%s", tc.tool, accepted, snapshot.Revision, before.Revision, err, &out, &diagnostic)
		}
	}
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "actual-reviewer", filepath.Join(f.root, p.ViewPath)); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, p.ID); err != nil || !accepted {
		t.Fatalf("original consumer did not accept actual native Stop: %t %v", accepted, err)
	}
}

func TestNativeS5ReceiverSourceFailureBelongsToMain(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	// Control-plane preparation is owned by Main, not by the answer author.
	if err := os.Remove(filepath.Join(f.root, "docs/control/loop-definition.json")); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, f.root).Revision
	code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "native-reviewer", filepath.Join(f.root, p.ViewPath))
	if code != 0 || !strings.Contains(diagnostic, "Main owns recovery") {
		t.Fatalf("receiver failure sent back to author: %d %s", code, diagnostic)
	}
	if stageSnapshot(t, f.root).Revision != before {
		t.Fatal("failed reception fabricated acceptance")
	}
}

func TestNativeS5IncompleteReturnHandsBackWithoutInventingReviewCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, message, want string
		prepare             func(t *testing.T, root string, p workpacket.Packet)
	}{
		{
			name:    "lost_independence",
			message: "我参与了被审材料的编写，无法独立签字。请 Main 重新安排独立审查者。",
			want:    "/answer/conclusion",
		},
		{
			name:    "prose_does_not_supply_conclusion",
			message: "PASS. Review complete.",
			want:    "/answer/conclusion",
		},
		{
			name:    "professional_field_error",
			message: "The reviewed contract needs correction; see my actual answer.",
			want:    "/answer/findings",
			prepare: func(t *testing.T, root string, p workpacket.Packet) {
				draft := completedDraft(p, "fix_required")
				draft.Answer.Findings = []workpacket.Finding{{Path: p.Inputs[0].Path}}
				stageJSON(t, root, p.AnswerPath, draft)
			},
		},
		{
			name:    "missing_answer",
			message: "The supplied answer is missing; Main must restore its original preparation.",
			want:    "no such file",
			prepare: func(t *testing.T, root string, p workpacket.Packet) {
				if err := os.Remove(filepath.Join(root, p.AnswerPath)); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			p := f.packets[0]
			if tc.prepare != nil {
				tc.prepare(t, f.root, p)
			}
			before := stageSnapshot(t, f.root).Revision
			ref := filepath.Join(f.root, p.ViewPath)
			for _, active := range []bool{false, true} {
				body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "native-reviewer", "session_id": "native-review-parent", "cwd": f.root, "stop_hook_active": active, "last_assistant_message": tc.message + "\n" + ref})
				var out, diagnostic bytes.Buffer
				code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic)
				if code != 0 || out.Len() != 0 {
					t.Fatalf("unreceived responsibility blocked native handoff: active=%t code=%d stdout=%s stderr=%s", active, code, &out, &diagnostic)
				}
				for _, want := range []string{"Main owns recovery", p.Binding.Owner, filepath.Join(f.root, p.AnswerPath), tc.want, "does not establish review completion"} {
					if !strings.Contains(diagnostic.String(), want) {
						t.Fatalf("unreceived handoff lost %q: %s", want, &diagnostic)
					}
				}
				snapshot := stageSnapshot(t, f.root)
				if _, accepted, err := runtime.FindSubmission(snapshot.State, p.ID); err != nil || accepted || snapshot.Revision != before || snapshot.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
					t.Fatalf("unreceived handoff changed acceptance or stage: accepted=%t revision=%d/%d err=%v", accepted, snapshot.Revision, before, err)
				}
			}
			text := mainNativeReturn(t, f.root, ref, tc.message)
			for _, want := range []string{`"accepted":false`, `"work_complete":false`, "独立性或来源阻塞", "专业答案缺项"} {
				if !strings.Contains(text, want) {
					t.Fatalf("Main missed original unreceived responsibility guidance %q: %s", want, text)
				}
			}
		})
	}
}
