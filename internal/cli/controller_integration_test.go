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

func TestSessionStartHookEmitsRecoveryGuidanceAndPersistsMilestone(t *testing.T) {
	sourceRoot := filepath.Join("..", "..")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "release_audits"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"docs/loop-definition.json",
		"docs/hook-policy.json",
		"docs/release_audits/protected_commands.json",
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
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "hook-decisions.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"session-039"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("SessionStart hook failed: code=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("hook output is not JSON: %v; output=%s", err, stdout.String())
	}
	message, _ := payload["systemMessage"].(string)
	for _, expected := range []string{
		"LOOP RECOVERY",
		"docs/agent-protocol.md#s2",
		".claude/bin/loop-harness.md",
		"Next:",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("recovery message missing %q: %s", expected, message)
		}
	}
	updated, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `"milestone"`) {
		t.Fatalf("SessionStart must persist milestone: %s", updated)
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"hook", "--event", "PreCompact", "--root", root}, strings.NewReader(`{"hook_event_name":"PreCompact","session_id":"session-039"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("PreCompact hook failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "LOOP RECOVERY") || !strings.Contains(stdout.String(), "docs/agent-protocol.md#s2") {
		t.Fatalf("PreCompact must emit the resumable recovery packet: %s", stdout.String())
	}

	// A role-bearing spawn is a controller event before the subagent exists. The
	// settings matcher must reach this path and emit the delegation questions.
	current, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var runtimeState map[string]any
	if err := json.Unmarshal(current, &runtimeState); err != nil {
		t.Fatal(err)
	}
	runtimeState["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	current, err = json.Marshal(runtimeState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), current, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"session-039","tool_name":"Agent","tool_input":{"subagent_type":"backend-builder","team_name":"loop-team"}}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("delegation preflight hook failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"Preflight questions", "Agent Team", "worktree", "LOOP RECOVERY"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("delegation preflight output missing %q: %s", expected, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"hook", "--event", "SubagentStop", "--root", root}, strings.NewReader(`{"hook_event_name":"SubagentStop","session_id":"session-039","facts":{"agent_report_complete":true}}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("SubagentStop hook failed: code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"SubagentStop", "develop", "completion_ack"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("allowed SubagentStop must emit integration guidance %q: %s", expected, stdout.String())
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
	if err := os.MkdirAll(filepath.Join(root, "docs", "release_audits"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"docs/loop-definition.json",
		"docs/hook-policy.json",
		"docs/release_audits/protected_commands.json",
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
	// The rendered PreToolUse output (hookSpecificOutput.permissionDecision)
	// must be "allow" because not_ready must NEVER block the tool
	// (BE-039 §3.2 / REQ-039 §10.2).
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("hook output is not JSON: %v; output=%s", err, stdout.String())
	}
	output, _ := payload["hookSpecificOutput"].(map[string]any)
	permissionDecision, _ := output["permissionDecision"].(string)
	if permissionDecision != "allow" {
		t.Fatalf("PreToolUse must allow not_ready, got permissionDecision=%q payload=%v", permissionDecision, payload)
	}
}
