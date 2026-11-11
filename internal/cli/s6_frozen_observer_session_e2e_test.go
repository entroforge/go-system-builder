package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestFrozenS6ObserverUsesOriginalWorkspaceSession(t *testing.T) {
	f := coreBuilderFixture(t)
	execution := prepareCoreBuilder(t, f, "backend-core")
	beginCoreBuilder(t, execution)
	fakeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte("#!/bin/sh\nprintf '{}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stageCLI(t, "runtime", "workspace", "launch", "--root", f.root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID)
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	execution = registry.Executions[execution.AssignmentID]
	if execution.PlatformSessionID == "" {
		t.Fatal("launch did not record a session")
	}
	paths, err := registry.WorkerSubmissionPaths(execution)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(execution.Path, filepath.FromSlash(paths.Completion))
	// The canonical Assignment Record freezes the machine tuple of this
	// Builder's Result, and the canonical contract only accepts a draft that
	// carries every frozen template field. The hand that plays the Builder
	// therefore writes that template and then supplies the professional
	// answer; an empty legacy envelope is not a canonical Result at all and
	// the observer would refuse it for the wrong reason.
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, f.root).State, execution.AgentID)
	if err != nil || !found {
		t.Fatalf("canonical Assignment Record was not registered: found=%t err=%v", found, err)
	}
	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	template, _ := agentcontext.ResultTemplate(dispatch, record.RuntimeID, "2026-01-01T00:00:00Z")
	if template == nil {
		t.Fatal("the registered Builder Record has no canonical Result template")
	}
	stageJSON(t, execution.Path, paths.Completion, template)
	invoke := func(session, cwd, tool string, input, response map[string]any) string {
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": session, "cwd": cwd, "tool_name": tool, "tool_input": input, "tool_response": response})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(payload), &out, &diagnostic); code != 0 {
			t.Fatalf("hook: %d %s", code, &diagnostic)
		}
		return out.String()
	}
	bash := map[string]any{"command": "true"}
	if out := invoke(execution.PlatformSessionID, execution.Path, "Bash", bash, map[string]any{}); strings.Contains(out, "questionnaire saved") {
		t.Fatalf("untouched original draft was received: %s", out)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	draft["summary"] = "The actual Builder has begun describing the delivery."
	stageJSON(t, execution.Path, paths.Completion, draft)
	peer := f.packets[0].Binding.Owner
	if peer == execution.AgentID {
		t.Fatal("fixture needs a distinct registered responsibility")
	}
	for _, event := range []struct {
		tool  string
		input map[string]any
	}{
		{"Bash", map[string]any{"command": "true"}},
		{"Write", map[string]any{"file_path": full}},
	} {
		if out := observedQuestionnaireHookAs(t, f.root, peer, event.tool, event.input, map[string]any{}); strings.Contains(out, "S6 Completion questionnaire saved") {
			t.Fatalf("another registered responsibility received the Builder's answer: %s", out)
		}
	}
	for _, tc := range []struct {
		session, cwd string
		response     map[string]any
	}{
		{"not-the-recorded-session", execution.Path, map[string]any{}},
		{execution.PlatformSessionID, f.root, map[string]any{}},
		{execution.PlatformSessionID, execution.Path, map[string]any{"is_error": true}},
	} {
		if out := invoke(tc.session, tc.cwd, "Bash", bash, tc.response); strings.Contains(out, "questionnaire saved") {
			t.Fatalf("unrelated or failed event consumed the answer: %s", out)
		}
	}
	if out := invoke(execution.PlatformSessionID, execution.Path, "Bash", bash, map[string]any{}); !strings.Contains(out, "S6 Completion questionnaire saved") || !strings.Contains(out, full) {
		t.Fatalf("registered session did not reach its original consumer: %s", out)
	}
	if out := invoke(execution.PlatformSessionID, execution.Path, "Write", map[string]any{"file_path": full}, map[string]any{}); !strings.Contains(out, "S6 Completion questionnaire saved") {
		t.Fatalf("registered explicit save did not reach its original consumer: %s", out)
	}
}
