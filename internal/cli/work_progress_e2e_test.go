package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/schema"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func progressHook(t *testing.T, root, cwd, owner, session, tool string, input, response map[string]any) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": session, "agent_id": owner, "cwd": cwd, "tool_name": tool, "tool_input": input, "tool_response": response, "runtime_context": map[string]any{"current_state": "forged", "revision": 999, "agent": map[string]any{"id": "forged"}}})
	var out, errout bytes.Buffer
	started := time.Now()
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse", "--progress-mode", "notify"}, bytes.NewReader(payload), &out, &errout); code != 0 {
		t.Fatalf("observer vetoed successful tool: %d %s", code, errout.String())
	}
	t.Logf("actual Hook %s elapsed=%s stderr=%s", tool, time.Since(started), errout.String())
	if strings.Contains(out.String(), "permissionDecision") || strings.Contains(out.String(), "\"decision\":\"block\"") {
		t.Fatal("diagnostic became policy gate", out.String())
	}
	return out.String()
}

func TestMainOrdinaryReadAndNonCheckpointStatusProgressWire(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "planning", "", 0)
	ref := state["bound_req"].(map[string]any)["path"].(string)
	milestone := state["milestone"].(map[string]any)
	milestone["read"] = []any{ref}
	milestone["missing"] = []any{"current professional requirement decision"}
	fixtures.WriteState(t, root, state)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	beforeState, _ := os.ReadFile(statePath)
	beforeJournal, _ := os.ReadFile(journalPath)
	path := filepath.Join(root, ref)
	body, _ := os.ReadFile(path)
	response := map[string]any{"file": map[string]any{"filePath": path, "content": string(body), "startLine": 1, "numLines": len(strings.Split(strings.TrimSuffix(string(body), "\n"), "\n"))}}
	for i := 0; i < hook.ProgressRepeatThreshold; i++ {
		if out := progressHook(t, root, root, "", "main-wire", "Read", map[string]any{"file_path": path}, response); strings.Contains(out, "WORK PROGRESS") {
			t.Fatal("new necessary read diagnosed early")
		}
	}
	out := progressHook(t, root, root, "", "main-wire", "Bash", map[string]any{"command": ".claude/bin/loop-harness status --root '" + root + "'"}, nil)
	if !strings.Contains(out, "\"kind\":\"stall\"") && !strings.Contains(out, "\\\"kind\\\":\\\"stall\\\"") {
		t.Fatal("ordinary status early-return bypassed sampler", out)
	}
	if !strings.Contains(out, "WORK PROGRESS") {
		t.Fatal(out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	notice := strings.TrimPrefix(envelope["hookSpecificOutput"].(map[string]any)["additionalContext"].(string), "WORK PROGRESS: ")
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-progress-notice.schema.json", []byte(notice)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if out := progressHook(t, root, root, "", "main-wire", "Bash", map[string]any{"command": "git status --short"}, nil); strings.Contains(out, "WORK PROGRESS") {
			t.Fatal("duplicate notice")
		}
	}
	afterState, _ := os.ReadFile(statePath)
	afterJournal, _ := os.ReadFile(journalPath)
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("ordinary observation changed Runtime/journal")
	}
	settings, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(settings, &parsed)
	matches := parsed["hooks"].(map[string]any)["PostToolUse"].([]any)[0].(map[string]any)["matcher"].(string)
	for _, tool := range []string{"Read", "Glob", "Grep"} {
		if !installedHookMatcherMatches(t, matches, tool) {
			t.Fatal("Main matcher lacks ordinary observation", tool)
		}
	}
}

func TestDefaultProgressShadowRecordsWithoutContext(t *testing.T) {
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "planning", "", 0)
	fixtures.WriteState(t, root, state)
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "shadow-session", "cwd": root, "tool_name": "Bash", "tool_input": map[string]any{"command": "git status --short"}, "progress_mode": "notify"})
	for i := 0; i < 7; i++ {
		var out, errout bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(payload), &out, &errout); code != 0 || strings.Contains(out.String(), "WORK PROGRESS") {
			t.Fatal("default shadow promoted by payload", code, out.String(), errout.String())
		}
	}
	files, err := filepath.Glob(filepath.Join(root, ".claude/hook-notices/work-progress/*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	json.Unmarshal(data, &record)
	notice, _ := json.Marshal(record["notice"])
	if err := schema.NewEmbeddedValidator().ValidateBytes("work-progress-notice.schema.json", notice); err != nil {
		t.Fatal("shadow lacks typed diagnostic", err)
	}
}

func TestS5WorkerReadProgressAndReceivedPeerWaitWire(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	f.begin(t, p, &e)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(e.Path, ".claude/inputs", p.Inputs[0].Path)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(body), "\n")
	response := map[string]any{"file": map[string]any{"filePath": path, "content": lines[0], "startLine": 1, "numLines": 1}}
	for i := 0; i < 6; i++ {
		out := progressHook(t, f.root, e.Path, e.AgentID, "worker-wire", "Read", map[string]any{"file_path": path, "offset": 1, "limit": 1}, response)
		if (i == 5) != strings.Contains(out, "WORK PROGRESS") {
			t.Fatalf("worker range count=%d: %s", i, out)
		}
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("ordinary Worker read changed Runtime")
	}
	r := saveStageHook(t, f.root, e.Path, e.AgentID, ".loop-submissions/stage-answer.json", completedDraft(p, "pass"))
	if !r.WorkComplete {
		t.Fatal(r)
	}
	for i := 0; i < 8; i++ {
		if out := progressHook(t, f.root, e.Path, e.AgentID, "worker-wire", "Read", map[string]any{"file_path": path}, response); strings.Contains(out, "WORK PROGRESS") {
			t.Fatal("received delivery waiting for peer diagnosed")
		}
	}
}
