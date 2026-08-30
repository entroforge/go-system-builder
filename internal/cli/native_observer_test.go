package cli_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestPostToolUseFailureIsAuditedWithoutChangingToolOutcome(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PostToolUseFailure", "--root", root}, strings.NewReader(`{
		"hook_event_name":"PostToolUseFailure",
		"session_id":"session-native-failure",
		"tool_name":"Write",
		"tool_use_id":"toolu_failure_1",
		"error":"permission denied"
	}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("native failure observer must remain observation-only: code=%d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("native failure observer must not emit a permission payload: %s", stdout.String())
	}
	record := readLastHookDecision(t, root)
	if record["hook_event"] != "PostToolUseFailure" || record["decision"] != "audit" {
		t.Fatalf("unexpected native failure audit: %#v", record)
	}
	if !strings.Contains(record["reason"].(string), "Write") {
		t.Fatalf("failure audit should identify the tool: %#v", record)
	}
}

func TestConfigChangeIsAuditedAndNeverPretendsToBlock(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "ConfigChange", "--root", root}, strings.NewReader(`{
		"hook_event_name":"ConfigChange",
		"session_id":"session-config-change",
		"source":"project_settings",
		"file_path":"docs/hook-policy.json"
	}`), &stdout, &stderr)
	if code != 0 || stdout.Len() != 0 {
		t.Fatalf("ConfigChange must be audit-only: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	record := readLastHookDecision(t, root)
	if record["hook_event"] != "ConfigChange" || record["decision"] != "audit" {
		t.Fatalf("unexpected config-change audit: %#v", record)
	}
}

func TestDistinctConfigChangeEventsAreNotDeduplicated(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, filePath := range []string{"docs/hook-policy.json", "settings.json"} {
		var stdout, stderr bytes.Buffer
		code := cli.Run([]string{"hook", "--event", "ConfigChange", "--root", root}, strings.NewReader(`{
			"hook_event_name":"ConfigChange",
			"session_id":"session-config-repeat",
			"source":"project_settings",
			"file_path":"`+filePath+`"
		}`), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("ConfigChange observer failed for %s: code=%d stderr=%s", filePath, code, stderr.String())
		}
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude", "hook-decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; got != 2 {
		t.Fatalf("distinct ConfigChange events must both be retained, got %d records: %s", got, data)
	}
}

func TestNativeObserverAuditFailureRemainsFailOpen(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".claude", "hook-decisions.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "ConfigChange", "--root", root}, strings.NewReader(`{
		"hook_event_name":"ConfigChange",
		"session_id":"session-config-fail-open",
		"source":"project_settings",
		"file_path":"settings.json"
	}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("audit-only observer failure must remain fail-open, code=%d stderr=%s", code, stderr.String())
	}
}

func readLastHookDecision(t *testing.T, root string) map[string]any {
	t.Helper()
	file, err := os.Open(filepath.Join(root, ".claude", "hook-decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var last map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		last = record
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if last == nil {
		t.Fatal("expected one hook audit record")
	}
	return last
}
