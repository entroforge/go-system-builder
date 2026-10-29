package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func entryQuestionnaireHook(t *testing.T, root, event, path string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("%s hook failed %d: %s %s", event, code, &out, &errout)
	}
	return out.String()
}

func initEntryQuestionnaireRoot(t *testing.T, reqs map[string]string) string {
	t.Helper()
	root := newUXTestRoot(t, reqs)
	template, err := os.ReadFile(filepath.Join("..", "..", "docs/requirements/REQ-template.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-template.md"), template, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, nil, &out, &errout); code != 0 {
		t.Fatalf("init failed %d: %s", code, &errout)
	}
	return root
}

func TestEntryQuestionnaireS0PreparesActualDraftWithoutHumanDecision(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, nil)
	path := filepath.Join(root, "docs/requirements/REQ-001.md")
	out := entryQuestionnaireHook(t, root, "SessionStart", path)
	var envelope struct {
		Output struct {
			Context    string `json:"additionalContext"`
			Permission string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(envelope.Output.Context, path) || envelope.Output.Permission == "deny" {
		t.Fatalf("no exact draft or unexpected denial: %s", out)
	}
	assertS0CurrentResponsibility(t, envelope.Output.Context)
	if _, err := os.Stat(filepath.Join(root, ".claude/submissions/requirement-binding.md")); !os.IsNotExist(err) {
		t.Fatalf("S0 prepared a binding request before a locked requirement: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("> 状态：draft\n")) || !bytes.Contains(data, []byte("> UI impact：unknown\n")) {
		t.Fatalf("draft was not honest: %s", data[:500])
	}
	data = append(data, []byte("\nHuman questions remain unanswered.\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"PreToolUse", "Stop"} {
		out = entryQuestionnaireHook(t, root, event, path)
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		assertS0CurrentResponsibility(t, envelope.Output.Context)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("repeat listener overwrote draft")
	}
	entryQuestionnaireHook(t, root, "PostToolUse", path)
	assertEntryInactive(t, root)
}

func assertS0CurrentResponsibility(t *testing.T, context string) {
	t.Helper()
	for _, premature := range []string{"complete the actual four binding answers", "TR-001", "req bind", ".claude/loop-state.json"} {
		if strings.Contains(context, premature) {
			t.Fatalf("S0 asks for S1 or internal metadata before the current requirement: %q", context)
		}
	}
	if !strings.Contains(context, "actual lock") || !strings.Contains(context, "requirement") {
		t.Fatalf("S0 must explain the current professional requirement and real lock: %q", context)
	}
}

func TestEntryQuestionnaireS0NextAndLockSaveKeepStageBoundary(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, nil)
	state := readJSONMap(t, filepath.Join(root, ".claude/loop-state.json"))
	milestone, _ := state["milestone"].(map[string]any)
	data, _ := json.Marshal(milestone)
	assertS0CurrentResponsibility(t, string(data))
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"next", "--root", root}, nil, &out, &errout); code != 0 {
		t.Fatalf("next failed: %s", &errout)
	}
	assertS0CurrentResponsibility(t, out.String())

	path := filepath.Join(root, "docs/requirements/REQ-001.md")
	entryQuestionnaireHook(t, root, "SessionStart", path)
	// This is an explicit synthetic lock for this test, not a startup approval.
	locked := strings.Replace(lockedReqBody, "REQ-098", "REQ-001", 1)
	if err := os.WriteFile(path, []byte(locked), 0o644); err != nil {
		t.Fatal(err)
	}
	feedback := entryQuestionnaireHook(t, root, "PostToolUse", path)
	requestPath := filepath.Join(root, ".claude/submissions/requirement-binding.md")
	if !strings.Contains(feedback, requestPath) || !strings.Contains(feedback, "S1 binding questionnaire") {
		t.Fatalf("the actual lock save did not hand off to original S1 preparation: %s", feedback)
	}
	request, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range []string{"Which requirement is selected?", "Who actually authorized this binding?", "Which development branch was selected?", "What is the explicit release destination?"} {
		if !strings.Contains(string(request), question) {
			t.Fatalf("S1 handoff lost original choice %q: %s", question, request)
		}
	}
	if !strings.Contains(string(request), "- Who actually authorized this binding? \n") {
		t.Fatalf("S0 lock was incorrectly treated as startup approval: %s", request)
	}
	entryQuestionnaireHook(t, root, "PostToolUse", requestPath)
	assertEntryInactive(t, root)
}

func TestEntryQuestionnaireStopReadsWithoutPreparingDraft(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, nil)
	path := filepath.Join(root, "docs/requirements/REQ-001.md")
	entryQuestionnaireHook(t, root, "Stop", path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stop prepared draft: %v", err)
	}
	assertEntryInactive(t, root)
}

func TestEntryQuestionnaireS1NamesHumanChoicesAndSaveDoesNotBind(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	path := filepath.Join(root, ".claude/submissions/requirement-binding.md")
	out := entryQuestionnaireHook(t, root, "SessionStart", path)
	if !strings.Contains(out, path) || !strings.Contains(out, "startup decision") || !strings.Contains(out, "release destination") {
		t.Fatalf("incomplete current binding questions: %s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(filepath.Join(root, "docs/requirements/REQ-098.md"))) {
		t.Fatalf("actual requirement path absent: %s", data)
	}
	if !bytes.Contains(data, []byte("Write each selected ref verbatim")) || !bytes.Contains(data, []byte("develop, main or origin/main")) {
		t.Fatalf("branch answer format guidance absent: %s", data)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	before, _ := os.ReadFile(statePath)
	journalBefore, _ := os.ReadFile(journalPath)
	entryQuestionnaireHook(t, root, "PreToolUse", path)
	entryQuestionnaireHook(t, root, "Stop", path)
	after, _ := os.ReadFile(statePath)
	journalAfter, _ := os.ReadFile(journalPath)
	if !bytes.Equal(before, after) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("ordinary S0/S1 Pre/Stop wrote Runtime or journal")
	}
	entryQuestionnaireHook(t, root, "PostToolUse", path)
	assertEntryInactive(t, root)
	// Synthetic explicit human lock/start intent is faithfully recorded in
	// the existing four answers. The normal save performs the original bind.
	text := string(data)
	text = strings.Replace(text, "- Who actually authorized this binding? ", "- Who actually authorized this binding? actual-test-human", 1)
	text = strings.Replace(text, "- Which development branch was selected? ", "- Which development branch was selected? feature/req", 1)
	text = strings.Replace(text, "- What is the explicit release destination? ", "- What is the explicit release destination? origin/main", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	out = entryQuestionnaireHook(t, root, "PostToolUse", path)
	bound := readJSONMap(t, statePath)
	if bound["lifecycle"].(map[string]any)["state"] != "planning" || bound["lifecycle"].(map[string]any)["phase"] != "design" || bound["bound_req"].(map[string]any)["approved_by"] != "actual-test-human" {
		t.Fatalf("normal binding save did not reach original TR001/S2: %s", out)
	}
	workspace := bound["bound_req"].(map[string]any)["workspace"].(map[string]any)
	if workspace["dev_branch"] != "feature/req" || workspace["release_upstream"] != "origin/main" {
		t.Fatalf("selected branch refs were not recorded verbatim: %#v", workspace)
	}
	boundBytes, _ := os.ReadFile(statePath)
	entryQuestionnaireHook(t, root, "PostToolUse", path)
	replayBytes, _ := os.ReadFile(statePath)
	if !bytes.Equal(boundBytes, replayBytes) {
		t.Fatal("consumed binding request replay changed the active Runtime")
	}
	// The original explicit unbind creates a new inactive responsibility even
	// though its Runtime tuple resets. Even with a custom unbind archive, the
	// original bind already created a new default archive coordinate. No old
	// startup choice may authorize this new responsibility.
	var unbindOut, unbindErr bytes.Buffer
	if code := cli.Run([]string{"req", "unbind", "--root", root, "--archive-dir", ".claude/custom-runtime-archive", "--approved-by", "actual-test-human", "--reason", "Synthetic end of the current binding."}, nil, &unbindOut, &unbindErr); code != 0 {
		t.Fatalf("original unbind failed: %s %s", unbindOut.String(), unbindErr.String())
	}
	currentFiles, err := filepath.Glob(filepath.Join(root, ".claude/submissions/*/requirement-binding.md"))
	if err != nil || len(currentFiles) != 1 {
		t.Fatalf("new archive-bound entry was not prepared: %v %v", currentFiles, err)
	}
	currentData, err := os.ReadFile(currentFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(currentData, []byte("actual-test-human")) {
		t.Fatal("new responsibility reused the old authorization")
	}
	entryQuestionnaireHook(t, root, "PostToolUse", path)
	assertEntryInactive(t, root)
	entryQuestionnaireHook(t, root, "PostToolUse", currentFiles[0])
	assertEntryInactive(t, root)
}

func assertEntryInactive(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state["lifecycle"].(map[string]any)["state"] != "inactive" {
		t.Fatalf("draft/save created a transition: %s", data)
	}
	if bound, _ := state["bound_req"].(map[string]any); bound != nil && bound["id"] != "" {
		t.Fatalf("draft/save bound a requirement: %+v", bound)
	}
}

func TestEntryQuestionnaireS1AuthorizationQuestionAcceptsShortAndLegacy(t *testing.T) {
	for _, tc := range []struct{ name, question, authorization string }{
		{"short actual decision", "Who actually authorized this binding?", "actual-test-human"},
		{"legacy actual decision", "Who actually authorized this binding? Record that human decision; do not infer it from Git identity.", "actual-test-human"},
		{"short empty", "Who actually authorized this binding?", ""},
		{"legacy empty", "Who actually authorized this binding? Record that human decision; do not infer it from Git identity.", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := initEntryQuestionnaireRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
			path := filepath.Join(root, ".claude/submissions/requirement-binding.md")
			entryQuestionnaireHook(t, root, "SessionStart", path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(data), "- Who actually authorized this binding? ", "- "+tc.question+" "+tc.authorization, 1)
			text = strings.Replace(text, "- Which development branch was selected? ", "- Which development branch was selected? feature/req", 1)
			text = strings.Replace(text, "- What is the explicit release destination? ", "- What is the explicit release destination? origin/main", 1)
			if err := os.WriteFile(path, []byte(text), 0644); err != nil {
				t.Fatal(err)
			}
			out := entryQuestionnaireHook(t, root, "PostToolUse", path)
			if tc.authorization == "" {
				assertEntryInactive(t, root)
				if !strings.Contains(out, "Still unanswered") {
					t.Fatal(out)
				}
				return
			}
			state := readJSONMap(t, filepath.Join(root, ".claude/loop-state.json"))
			if state["lifecycle"].(map[string]any)["state"] != "planning" || state["bound_req"].(map[string]any)["approved_by"] != tc.authorization {
				t.Fatalf("actual decision not accepted: %s", out)
			}
		})
	}
}
