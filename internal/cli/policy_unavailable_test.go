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

func TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible(t *testing.T) {
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
				if code != 0 {
					t.Fatalf("unavailable Hook policy must not block PreToolUse: exit=%d stdout=%s stderr=%s", code, &out, &errout)
				}
				if strings.Contains(out.String(), `"permissionDecision":"deny"`) {
					t.Fatalf("unavailable Hook policy must not produce a tool denial: %s", &out)
				}
				qualityGate := parseHookQualityGate(t, out.String())
				if qualityGate["status"] != "unknown" || !strings.Contains(out.String(), "LOOP RECOVERY") || !strings.Contains(out.String(), "recover the runtime checkpoint") {
					t.Fatalf("unavailable Hook policy must preserve graph diagnostics and recovery guidance: gate=%#v stdout=%s", qualityGate, &out)
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
