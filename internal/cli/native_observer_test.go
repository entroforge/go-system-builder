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
		"file_path":"docs/control/hook-policy.json"
	}`), &stdout, &stderr)
	if code != 0 || stdout.Len() != 0 {
		t.Fatalf("ConfigChange must be audit-only: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	record := readLastHookDecision(t, root)
	if record["hook_event"] != "ConfigChange" || record["decision"] != "audit" {
		t.Fatalf("unexpected config-change audit: %#v", record)
	}
}

func TestNativeObserverAuditsMainWhenRuntimeUnreadable(t *testing.T) {
	for _, event := range []string{"PostToolUseFailure", "ConfigChange"} {
		t.Run(event, func(t *testing.T) {
			root := acFixtureRoot(t)
			writeACState(t, root, planningState(t, root, "design", 1))
			statePath := filepath.Join(root, ".claude", "loop-state.json")
			journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
			invalid := []byte("{invalid runtime source\n")
			if err := os.WriteFile(statePath, invalid, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{"hook_event_name": event, "session_id": "main-unreadable-" + event, "agent_id": "Main", "cwd": root, "tool_name": "Write", "error": "permission denied", "source": "project_settings", "file_path": "settings.json"}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := cli.Run([]string{"hook", "--event", event, "--root", root}, bytes.NewReader(body), &stdout, &stderr); code != 0 || stdout.Len() != 0 {
				t.Fatalf("%s changed the original tool outcome: code=%d stdout=%s stderr=%s", event, code, &stdout, &stderr)
			}
			if record := readLastHookDecision(t, root); record["hook_event"] != event || record["decision"] != "audit" {
				t.Fatalf("%s lost its original audit: %#v", event, record)
			}
			if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(after, invalid) {
				t.Fatalf("%s changed invalid Runtime: %v", event, err)
			}
			if after, err := os.ReadFile(journalPath); err != nil || len(after) != 0 {
				t.Fatalf("%s wrote a Runtime event: %v", event, err)
			}
		})
	}
}

func TestDistinctConfigChangeEventsAreNotDeduplicated(t *testing.T) {
	root := acFixtureRoot(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, filePath := range []string{"docs/control/hook-policy.json", "settings.json"} {
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

func TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity(t *testing.T) {
	root := acFixtureRoot(t)
	writeACState(t, root, planningState(t, root, "design", 1))
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	const key = "SYNTHETIC_R29_NO_REAL_CREDENTIAL"
	var ids []string
	for _, tc := range []struct{ name, event, field, value string }{
		{"failed command credential", "PostToolUseFailure", "error", "authentication failed; api_key=" + key},
		{"second credential same visible category", "PostToolUseFailure", "error", "authentication failed; api_key=" + key + "_OTHER"},
		{"credential crosses audit bound", "PostToolUseFailure", "error", strings.Repeat("x", 210) + " token=" + key},
		{"config source", "ConfigChange", "source", "project_settings secret=" + key},
		{"config path", "ConfigChange", "file_path", "settings/password=" + key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"hook_event_name": tc.event, "session_id": "r29-observer", "tool_name": "Bash", tc.field: tc.value, "tool_input": map[string]any{"command": "synthetic api_key=" + key}, "tool_response": map[string]any{"stderr": "synthetic token=" + key}}
			body, _ := json.Marshal(payload)
			for repeat := 0; repeat < 2; repeat++ {
				var out, diagnostic bytes.Buffer
				if code := cli.Run([]string{"hook", "--event", tc.event, "--root", root}, bytes.NewReader(body), &out, &diagnostic); code != 0 || out.Len() != 0 || bytes.Contains(diagnostic.Bytes(), []byte(key)) {
					t.Fatalf("native observer changed tool outcome or echoed sensitive output: %d", code)
				}
			}
			data, err := os.ReadFile(filepath.Join(root, ".claude/hook-decisions.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
			if last := lines[len(lines)-1]; bytes.Contains(last, []byte(key)) || bytes.Contains(last, []byte("SYNTHETIC_R29")) {
				t.Fatal("audit persisted external credential text or its truncated prefix")
			}
			last := readLastHookDecision(t, root)
			if !strings.Contains(last["reason"].(string), "withheld") || last["decision"] != "audit" || last["hook_event"] != tc.event {
				t.Fatalf("native audit lost its limited withholding/observation meaning: %v", last["reason"])
			}
			id := last["decision_id"].(string)
			for _, old := range ids {
				if id == old {
					t.Fatal("distinct external events collapsed after filtering their visible reason")
				}
			}
			ids = append(ids, id)
			if rows := bytes.Count(bytes.TrimSpace(data), []byte("\n")) + 1; rows != len(ids) {
				t.Fatalf("same-event retry duplicated audit: rows=%d unique=%d", rows, len(ids))
			}
		})
	}
	if data, err := os.ReadFile(filepath.Join(root, ".claude/hook-decisions.jsonl")); err != nil || bytes.Contains(data, []byte("SYNTHETIC_R29")) {
		t.Fatal("audit history contains external sensitive text")
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(after, beforeState) {
		t.Fatal("audit filtering changed Runtime")
	}
	if after, err := os.ReadFile(journalPath); err != nil || len(after) != 0 {
		t.Fatalf("audit filtering wrote a domain event: %v", err)
	}
	body := `{"hook_event_name":"PostToolUseFailure","session_id":"r29-normal","tool_name":"Write","error":"permission denied"}`
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUseFailure", "--root", root}, strings.NewReader(body), &out, &diagnostic); code != 0 || out.Len() != 0 {
		t.Fatalf("ordinary native failure outcome changed: %d", code)
	}
	if reason := readLastHookDecision(t, root)["reason"].(string); !strings.Contains(reason, "permission denied") {
		t.Fatalf("nonsensitive failure lost useful diagnostics: %s", reason)
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
