// subagent_integration_test.go — system-level confirmation that a
// SubagentStop event with a clean agent_report_complete surfaces the
// integration guidance (merge target, completion_ack, worktree
// cleanup) and never the legacy HOOK_SUBAGENT_BLOCKED deny (BE-039
// §6.3 / REQ-039 §13.6 / §19 AC-006).

package req039_test

import (
	"strings"
	"testing"
)

func TestSubagent_Integration_Guidance(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 5)
	writeSystemState(t, root, state)

	input := `{
		"session_id":"session-sys-subagent",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-1",
		"agent_report_complete":true
	}`
	code, stdout, stderr := runHook(t, root, "SubagentStop", input)
	if code != 0 {
		t.Fatalf("SubagentStop must not fail: code=%d stderr=%s", code, stderr)
	}
	out := stdout
	for _, expected := range []string{"SubagentStop", "develop", "completion_ack"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("SubagentStop integration guidance must include %q: %s", expected, out)
		}
	}
	// The legacy HOOK_SUBAGENT_BLOCKED reason is retired; the
	// minimal safety policy must not surface it in the systemMessage.
	if strings.Contains(out, "HOOK_SUBAGENT_BLOCKED") {
		t.Fatalf("SubagentStop must not surface the retired HOOK_SUBAGENT_BLOCKED: %s", out)
	}
}
