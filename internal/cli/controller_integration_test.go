package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// Recover a real issued responsibility; a cached milestone or invented
// SubagentStop facts cannot stand in for native delivery reception.
func TestSessionStartRecoversCurrentResponsibilityWithoutPreCompactWrites(t *testing.T) {
	root, packet := newDesignFixture(t, "none")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"session-039"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("SessionStart hook failed: code=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("hook output is not JSON: %v; output=%s", err, stdout.String())
	}
	message, _ := hookContextValue(payload).(string)
	if strings.Contains(message, "S8 intake pending") || strings.Contains(message, "S8 current ObservationBatch") {
		t.Fatalf("S2 recovery assigned an unrelated S8 responsibility: %s", message)
	}
	for _, expected := range []string{"docs/control/agent-protocol.md#s2", filepath.Join(root, packet.ViewPath), filepath.Join(root, packet.AnswerPath)} {
		if !strings.Contains(message, expected) {
			t.Fatalf("current S2 recovery missing %q: %s", expected, message)
		}
	}
	before := map[string][]byte{}
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		path := filepath.Join(root, ".claude", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"hook", "--event", "PreCompact", "--root", root}, strings.NewReader(`{"hook_event_name":"PreCompact","session_id":"session-039"}`), &stdout, &stderr)
	if code != 0 || strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("PreCompact must stay silent: %d %s %s", code, &stdout, &stderr)
	}
	for path, want := range before {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("PreCompact changed original Runtime/journal %s: %v", path, err)
		}
	}
}

// TestPreToolUseHookDelegatesToControllerCycle is the BUG-039-02 retest
// for the PreToolUse path. The Hook entrypoint must run the new
// internal/controller control cycle (steps 1-11 of §4.1) instead of
// calling policy.Engine.Evaluate + refreshHookControl directly. A
// planning.design cursor without design documents must produce a
// not_ready Quality Gate verdict in the rendered Hook output and the
// tool MUST be allowed to continue (BE-039 §3.2: "Quality not_ready
// must NOT map to a tool block").
func TestPreToolUseHookDelegatesToControllerCycle(t *testing.T) {
	sourceRoot := filepath.Join("..", "..")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "reports", "release-audits"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"docs/control/loop-definition.json",
		"docs/control/hook-policy.json",
		"docs/control/protected-commands.json",
	} {
		data, err := os.ReadFile(filepath.Join(sourceRoot, relative))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	// Pin the cursor to planning.design so the controller resolves
	// GATE-PLANNING-DESIGN-COMPLETE.
	var stateMap map[string]any
	if err := json.Unmarshal(state, &stateMap); err != nil {
		t.Fatal(err)
	}
	stateMap["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": 0}
	// Strip the example documents and evidence so the gate is
	// deterministically not_ready.
	stateMap["documents"] = []any{}
	stateMap["evidence"] = []any{}
	stateRaw, err := json.MarshalIndent(stateMap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), stateRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "hook-decisions.jsonl"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root},
		strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"session-controller","tool_name":"Edit","tool_input":{"file_path":"internal/cli/controller.go"}}`),
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("PreToolUse hook failed: code=%d stderr=%s", code, stderr.String())
	}
	// The Hook output must surface a positive Recovery packet (the
	// controller's Guidance), and the underlying Decision must be allow.
	if !strings.Contains(stdout.String(), "LOOP RECOVERY") {
		t.Fatalf("controller cycle must emit LOOP RECOVERY packet, got: %s", stdout.String())
	}
	// PreToolUse reports the not_ready graph status without setting a
	// permissionDecision; the observer does not authorize or deny tools.
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("hook output is not JSON: %v; output=%s", err, stdout.String())
	}
	output, _ := payload["hookSpecificOutput"].(map[string]any)
	permissionDecision, _ := output["permissionDecision"].(string)
	if permissionDecision == "deny" {
		t.Fatalf("PreToolUse observer must not deny not_ready, got permissionDecision=%q payload=%v", permissionDecision, payload)
	}
}

func TestPreToolUseObservesUnknownMCPWithoutToolClassification(t *testing.T) {
	root := acFixtureRoot(t)
	writeACState(t, root, planningState(t, root, "design", 1))

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(`{
		"hook_event_name":"PreToolUse",
		"session_id":"session-unknown-mcp",
		"tool_name":"mcp__example__mutate",
		"tool_input":{"operation":"write"}
	}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unknown MCP warning must remain non-blocking: code=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Hook output is not JSON: %v; output=%s", err, stdout.String())
	}
	specific, _ := payload["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != nil {
		t.Fatalf("warning must still allow the tool, got %v", specific["permissionDecision"])
	}
	reason, _ := specific["additionalContext"].(string)
	if !strings.Contains(reason, "QUALITY_GATE") || !strings.Contains(reason, "LOOP RECOVERY") || strings.Contains(reason, "unknown_mcp_tool") {
		t.Fatalf("listener must give graph progress without classifying tools: %s", reason)
	}
}

func TestPreToolUseObservesUnknownMCPForWorker(t *testing.T) {
	root := acFixtureRoot(t)
	writeACState(t, root, planningState(t, root, "design", 1))

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(`{
		"hook_event_name":"PreToolUse",
		"session_id":"session-unknown-mcp-worker",
		"agent_id":"agent-worker-1",
		"tool_name":"mcp__example__mutate",
		"tool_input":{"operation":"write"},
		"runtime_context":{"runtime_id":"loop-test","revision":1,"agent":{"id":"agent-worker-1","state":"working"}}
	}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unknown MCP must remain available to the Worker: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Hook output is not JSON: %v; output=%s", err, stdout.String())
	}
	specific, _ := payload["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] == "deny" {
		t.Fatalf("Worker graph listener must not deny tools, got %v", specific["permissionDecision"])
	}
	if !strings.Contains(stdout.String(), "QUALITY_GATE") || strings.Contains(stdout.String(), "unknown_mcp_tool") {
		t.Fatalf("Worker listener must preserve graph feedback without tool policy: %s", stdout.String())
	}
}
