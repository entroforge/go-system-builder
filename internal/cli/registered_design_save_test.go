package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Exercise the shipped registration and its actual shell command. Only the
// platform's unambiguous match-all form is accepted here; this is not an
// approximation of Claude's arbitrary JavaScript regex matching engine.
func TestRegisteredCustomToolSaveReachesOriginalDesignReceiver(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("the shipped shell Hook registration requires bash")
	}
	repo := fixtures.RepoRoot(t)
	settingsBytes, err := os.ReadFile(filepath.Join(repo, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatal(err)
	}
	var commands []string
	var preCommand string
	for _, registration := range settings.Hooks["PreToolUse"] {
		if registration.Matcher == "*" || registration.Matcher == "" {
			for _, h := range registration.Hooks {
				if h.Type == "command" {
					preCommand = h.Command
				}
			}
		}
	}
	for _, registration := range settings.Hooks["PostToolUse"] {
		if registration.Matcher != "*" && registration.Matcher != "" {
			continue
		}
		for _, hook := range registration.Hooks {
			if hook.Type == "command" {
				commands = append(commands, hook.Command)
			}
		}
	}
	if len(commands) != 1 || preCommand == "" {
		t.Fatalf("custom tools must reach one original PostToolUse receiver; got %v", commands)
	}
	binary := filepath.Join(t.TempDir(), "loop-harness")
	build := exec.Command("go", "build", "-o", binary, "./cmd/loop-harness")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real registered Harness: %v %s", err, output)
	}
	for _, test := range []struct {
		name, tool, owner                        string
		authored, path, failed, denied, received bool
		preauthored, noPre                       bool
	}{
		{name: "script_custom_tool", tool: "mcp__sandbox__execute", authored: true, received: true},
		{name: "exact_path_custom_tool", tool: "mcp__sandbox__write_file", authored: true, path: true, received: true},
		{name: "failed_tool", tool: "mcp__sandbox__execute", authored: true, failed: true},
		{name: "denied_tool", tool: "mcp__sandbox__write_file", authored: true, path: true, denied: true},
		{name: "worker_ordinary_save", tool: "mcp__sandbox__write_file", owner: "independent-worker", authored: true, path: true},
		{name: "unrelated_lookup", tool: "ToolSearch"},
		{name: "custom_read_existing_answer", tool: "mcp__filesystem__read_file", path: true, authored: true, preauthored: true},
		{name: "unknown_without_before_observation", tool: "mcp__sandbox__write_file", path: true, authored: true, noPre: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, packet := newDesignFixture(t, "none")
			binDir := filepath.Join(root, ".claude/bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(binary, filepath.Join(binDir, "loop-harness")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude/settings.json"), settingsBytes, 0644); err != nil {
				t.Fatal(err)
			}
			if test.authored && test.preauthored {
				stageJSON(t, root, packet.AnswerPath, designDraft(packet))
			}
			before := stageSnapshot(t, root).Revision
			input := map[string]any{"command": "tool wrote a known answer; shell text is not parsed"}
			if test.path {
				input = map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}
			}
			payload := map[string]any{
				"hook_event_name": "PostToolUse", "session_id": "registered-save", "tool_use_id": "registered-tool", "cwd": root,
				"agent_id": test.owner, "tool_name": test.tool, "tool_input": input,
				"tool_response": map[string]any{"is_error": test.failed},
			}
			if !test.noPre {
				payload["hook_event_name"] = "PreToolUse"
				body, _ := json.Marshal(payload)
				command := exec.Command(bash, "-c", preCommand)
				command.Dir, command.Env, command.Stdin = root, append(os.Environ(), "CLAUDE_PROJECT_DIR="+root), bytes.NewReader(body)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("registered Pre: %v %s", err, output)
				}
				payload["hook_event_name"] = "PostToolUse"
			}
			if test.authored && !test.preauthored {
				stageJSON(t, root, packet.AnswerPath, designDraft(packet))
			}
			if test.denied {
				payload["error"] = "permission denied by the tool host"
			}
			body, _ := json.Marshal(payload)
			command := exec.Command(bash, "-c", commands[0])
			command.Dir, command.Env, command.Stdin = root, append(os.Environ(), "CLAUDE_PROJECT_DIR="+root), bytes.NewReader(body)
			var out, diagnostic bytes.Buffer
			command.Stdout, command.Stderr = &out, &diagnostic
			if err := command.Run(); err != nil {
				t.Fatalf("registered Hook command: %v %s %s", err, &out, &diagnostic)
			}
			snapshot := stageSnapshot(t, root)
			record, received, err := runtime.FindSubmission(snapshot.State, packet.ID)
			if err != nil || received != test.received {
				t.Fatalf("registration/owner/success reception mismatch: received=%t expected=%t err=%v out=%s", received, test.received, err, &out)
			}
			if test.received {
				if record.ControlStatus != "advanced" || !strings.Contains(out.String(), "S2 SUBMISSION RECEIPT") || !strings.Contains(out.String(), packet.AnswerPath) {
					t.Fatalf("registered original receiver lost transition or receipt: %+v %s", record, &out)
				}
			} else if snapshot.Revision != before || strings.Contains(out.String(), "SUBMISSION RECEIPT") {
				t.Fatalf("failed/unrelated/worker tool mutated Main responsibility: %d -> %d %s", before, snapshot.Revision, &out)
			}
		})
	}
}
