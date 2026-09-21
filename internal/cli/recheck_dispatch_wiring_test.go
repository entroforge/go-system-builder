package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// Exercise the configured tools through the real Hook entrypoint, including
// the parent-only, lifecycle and async-launch boundaries of the advisory.
func TestRecheckDispatchPostToolUseWiring(t *testing.T) {
	for _, tc := range []struct {
		name, tool, command, status, agent, lifecycle string
		want                                          bool
	}{
		{name: "Agent", tool: "Agent", want: true},
		{name: "legacy-Task", tool: "Task", want: true},
		{name: "integration", tool: "Bash", command: "loop-harness runtime task-integrate --assignment-id A1", want: true},
		{name: "completion", tool: "Bash", command: "loop-harness runtime task-complete --agent-id builder", want: true},
		{name: "registration", tool: "Bash", command: "loop-harness runtime register-workgroup --manifest m.json", want: true},
		{name: "ordinary-Bash", tool: "Bash", command: "go test ./..."},
		{name: "async-launch", tool: "Agent", status: "async_launched"},
		{name: "worker-context", tool: "Agent", agent: "worker"},
		{name: "S5", tool: "Agent", lifecycle: "document_verification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := auditCLIPlanFixture(t)
			statePath := filepath.Join(root, ".claude/loop-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.lifecycle != "" {
				var state map[string]any
				if err := json.Unmarshal(before, &state); err != nil {
					t.Fatal(err)
				}
				state["lifecycle"].(map[string]any)["state"] = tc.lifecycle
				before, err = json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(statePath, before, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var out, errs bytes.Buffer
			if code := cli.Run([]string{"s6", "status", "--root", root, "--capacity", "2"}, strings.NewReader(""), &out, &errs); code != 0 || (tc.want && !strings.Contains(out.String(), "TASK-042-01")) {
				t.Fatalf("positive control: code=%d stdout=%s stderr=%s", code, out.String(), errs.String())
			}
			input, err := json.Marshal(map[string]any{
				"hook_event_name": "PostToolUse", "tool_name": tc.tool, "cwd": root,
				"agent_id":      tc.agent,
				"tool_input":    map[string]any{"command": tc.command},
				"tool_response": map[string]any{"status": tc.status},
			})
			if err != nil {
				t.Fatal(err)
			}
			out.Reset()
			errs.Reset()
			code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(input), &out, &errs)
			if code != 0 {
				t.Fatalf("hook: code=%d stdout=%s stderr=%s", code, out.String(), errs.String())
			}
			if got := strings.Contains(out.String(), "S6 approved dispatch plan:"); got != tc.want {
				t.Fatalf("PostToolUse(%s) advisory=%v want=%v: stdout=%q stderr=%q", tc.tool, got, tc.want, out.String(), errs.String())
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("PostToolUse advisory mutated Runtime")
			}
		})
	}
}

func TestDispatchCheckpointToolsAreInstalled(t *testing.T) {
	data, err := os.ReadFile("../../settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"Agent", "Task", "Bash", "SendMessage", "SubagentHandback"} {
		matched := false
		for _, registration := range settings.Hooks["PostToolUse"] {
			pattern, err := regexp.Compile(registration.Matcher)
			if err != nil {
				t.Fatal(err)
			}
			matched = matched || pattern.MatchString(tool)
		}
		if !matched {
			t.Fatalf("PostToolUse matcher does not deliver %s", tool)
		}
	}
}
