package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS4RegisteredHookDeliversS5WithinPlatformBudget(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("registered Hook requires bash")
	}
	repo := fixtures.RepoRoot(t)
	data, err := os.ReadFile(filepath.Join(repo, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	var command string
	var timeout int
	for _, registration := range settings.Hooks["PostToolUse"] {
		if registration.Matcher != "*" {
			continue
		}
		for _, hook := range registration.Hooks {
			command, timeout = hook.Command, hook.Timeout
		}
	}
	if command == "" || timeout <= 28 {
		t.Fatal("platform budget cuts off S4 native worker")
	}
	binary := filepath.Join(t.TempDir(), "loop-harness")
	build := exec.CommandContext(t.Context(), "go", "build", "-p", "1", "-o", binary, "./cmd/loop-harness")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native hook: %v %s", err, output)
	}
	for _, tool := range []string{"Write", "mcp__files__execute"} {
		t.Run(tool, func(t *testing.T) {
			root, packet := s4HandoffFixture(t)
			binDir := filepath.Join(root, ".claude/bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(binary, filepath.Join(binDir, "loop-harness")); err != nil {
				t.Fatal(err)
			}
			stageJSON(t, root, packet.AnswerPath, completedTaskPlanningDraft(packet))
			input := map[string]any{}
			if tool == "Write" {
				input["file_path"] = filepath.Join(root, packet.AnswerPath)
			}
			body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "session_id": "registered-s4", "tool_name": tool, "tool_input": input, "tool_response": map[string]any{}})
			var out, diagnostic bytes.Buffer
			run := func() {
				out.Reset()
				diagnostic.Reset()
				ctx, cancel := context.WithTimeout(t.Context(), time.Duration(timeout)*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", command)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+root)
				cmd.Stdin = bytes.NewReader(body)
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				if err := cmd.Run(); err != nil {
					t.Fatalf("native Hook: %v stdout=%s stderr=%s", err, &out, &diagnostic)
				}
			}
			run()
			snapshot := stageSnapshot(t, root)
			packets, err := runtime.S5Packets(root, snapshot.State, "")
			if err != nil || len(packets) != 2 {
				t.Fatalf("S5 was not prepared: %v out=%s stderr=%s", err, &out, &diagnostic)
			}
			for _, p := range packets {
				if _, found, err := runtime.FindWorkPacket(snapshot.State, p.ID); err != nil || !found {
					t.Fatalf("S5 responsibility not issued: %s %v", p.ID, err)
				}
				for _, ref := range []string{p.Binding.Owner, p.Binding.Responsibility, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath)} {
					if !strings.Contains(out.String(), ref) {
						t.Fatalf("native receipt missing %q: stdout=%s stderr=%s", ref, &out, &diagnostic)
					}
				}
			}
			if tool == "Write" {
				run() // Original S4 path is still a valid receipt after entering S5.
				if stageSnapshot(t, root).Revision != snapshot.Revision {
					t.Fatal("native original-answer replay created another transition or responsibility")
				}
				for _, p := range packets {
					if !strings.Contains(out.String(), filepath.Join(root, p.AnswerPath)) {
						t.Fatalf("native replay lost the prepared original S5 handoff: %s %s", &out, &diagnostic)
					}
				}
			}
		})
	}
}
