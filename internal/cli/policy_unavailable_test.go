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

func TestUnavailablePolicyUsesPlatformControl(t *testing.T) {
	for _, broken := range []string{"missing", "invalid"} {
		for _, tool := range []string{"Write", "Edit", "Bash", "PowerShell", "Agent", "TaskUpdate", "mcp__custom__read", "unknown-tool", "Read", "Grep", "Glob"} {
			t.Run(broken+"/"+tool, func(t *testing.T) {
				root := t.TempDir()
				if broken == "invalid" {
					if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "docs/control/hook-policy.json"), []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": map[string]any{"command": "loop-harness init && touch product", "file_path": "product.go"}})
				var out, errout bytes.Buffer
				code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, bytes.NewReader(payload), &out, &errout)
				want := 2
				if tool == "Read" || tool == "Grep" || tool == "Glob" {
					want = 0
				}
				if code != want {
					t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", code, want, &out, &errout)
				}
				if !strings.Contains(errout.String(), "external terminal") {
					t.Fatalf("missing actionable recovery: %s", &errout)
				}
				if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); !os.IsNotExist(err) {
					t.Fatalf("error handler must not create a Runtime: %v", err)
				}
			})
		}
	}
}

func TestUnavailablePolicyDoesNotBlockLifecycleNotifications(t *testing.T) {
	for _, event := range []string{"SessionStart", "SubagentStart", "Stop", "SubagentStop", "TeammateIdle", "PreCompact"} {
		t.Run(event, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"hook_event_name": event})
			var out, errout bytes.Buffer
			code := cli.Run([]string{"hook", "--event", event, "--root", t.TempDir()}, bytes.NewReader(payload), &out, &errout)
			if code != 0 {
				t.Fatalf("lifecycle error caused continuation/block: %d %s", code, &errout)
			}
		})
	}
}
