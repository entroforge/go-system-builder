// ct_s11_stop_test.go — system-level CT-039-15 S11 terminal stop.

package req039_test

import (
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestCT03915_S11TerminalStopSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 50)
	req039fixtures.SeedAwaitingHumanRelease(t, root, state)
	writeSystemState(t, root, state)

	before := req039fixtures.ReadState(t, root)
	beforeLC, beforePh := req039fixtures.Lifecycle(before)

	body := req039fixtures.PreToolUseBody("session-ct-039-15-sys", "Bash", map[string]any{"command": "ls"})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse at S11 failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-15 must not use manual transition CLI")
	}

	after := req039fixtures.ReadState(t, root)
	afterLC, afterPh := req039fixtures.Lifecycle(after)
	if afterLC != beforeLC || afterPh != beforePh {
		t.Fatalf("CT-039-15 must not cross terminal lifecycle: %s/%s -> %s/%s", beforeLC, beforePh, afterLC, afterPh)
	}
	_, qg := parseEnv(t, stdout)
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("CT-039-15 must not commit transition at S11, qg=%v", qg)
	}
	if !strings.Contains(stdout, "release") && !strings.Contains(stdout, "human") && !strings.Contains(stdout, "Gateway") {
		t.Fatalf("CT-039-15 must surface human Gateway guidance, got %s", stdout)
	}
}
