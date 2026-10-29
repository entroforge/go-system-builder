package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestNativeExecutionS6LaunchIsNotAWorkerReturn(t *testing.T) {
	root, _, record := nativeDeliveryFixture(t)
	brief, _, _ := assignmentrecord.NativePaths(record)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": filepath.Join(root, brief)}, "tool_response": map[string]any{"status": "async_launched", "isAsync": true}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("S6 Main launch: %d %s %s", code, &out, &diagnostic)
	}
	var envelope struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	text := envelope.Output.Context
	if !strings.Contains(text, "launch acknowledgement") || strings.Contains(text, "Native return for ") || strings.Contains(text, "Main must resume") {
		t.Fatal(text)
	}
}

func TestNativeExecutionS5LaunchDoesNotRequestMissingStopDiagnostic(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	for _, packet := range f.packets {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": f.root, "agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": filepath.Join(f.root, packet.ViewPath)}, "tool_response": map[string]any{"status": "async_launched", "isAsync": true}})
		var out, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
			t.Fatalf("S5 launch: %d %s %s", code, &out, &diagnostic)
		}
		var payload struct {
			Output struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.Output.Context, "launch acknowledgement") || strings.Contains(payload.Output.Context, "No readable original Stop diagnostic") || strings.Contains(payload.Output.Context, "Native return for ") {
			t.Fatal(payload.Output.Context)
		}
	}
	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatal("launch received original S5 answer")
	}
}
