// compact_recovery_test.go — system-level confirmation that a
// post-Compact SessionStart surfaces the same milestone objective
// the prior PreToolUse persisted (REQ-039 §19 AC-008). The
// persisted milestone is the durable lifecycle checkpoint; a
// compacted Agent must resume at the same cursor.

package req039_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompact_Recovery_SameMilestone(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	// Step 1: drive a PreToolUse to persist the milestone.
	input := `{
		"session_id":"session-sys-compact",
		"hook_event_name":"PreToolUse",
		"tool_name":"Edit",
		"tool_input":{"file_path":"internal/cli/run.go"}
	}`
	code, stdout, stderr := runHook(t, root, "PreToolUse", input)
	if code != 0 {
		t.Fatalf("PreToolUse must succeed: stderr=%s", stderr)
	}
	_ = stdout

	// Read the persisted milestone objective.
	rawState, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(rawState, &persisted); err != nil {
		t.Fatal(err)
	}
	ms, _ := persisted["milestone"].(map[string]any)
	persistedObjective, _ := ms["objective"].(string)
	if persistedObjective == "" {
		t.Fatalf("milestone must carry objective after PreToolUse: %s", rawState)
	}

	// Step 2: SessionStart (simulating post-Compact recovery) must
	// surface the same objective in its systemMessage.
	code, stdout, stderr = runHook(t, root, "SessionStart",
		`{"hook_event_name":"SessionStart","session_id":"session-sys-compact-recover"}`)
	if code != 0 {
		t.Fatalf("SessionStart must succeed: stderr=%s", stderr)
	}
	if !strings.Contains(stdout, persistedObjective) {
		t.Fatalf("post-Compact SessionStart must surface persisted objective %q, got %s",
			persistedObjective, stdout)
	}
}
