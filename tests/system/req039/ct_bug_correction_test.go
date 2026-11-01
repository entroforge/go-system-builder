// ct_bug_correction_test.go — targeted recheck failure.

package req039_test

import (
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03923_TargetedRecheckFailedSystem drives PTR-BUG-07 via Hook.
func TestCT03923_TargetedRecheckFailedSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 36)
	req039fixtures.SeedTargetedReverificationFail(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.MainPreToolUseBody(root, "session-ct-039-23-sys", "Edit", map[string]any{
		"file_path": "docs/reports/bugs/BUG-039-15.md",
	})
	code, _, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	after := req039fixtures.ReadState(t, root)
	_, ph := req039fixtures.Lifecycle(after)
	if ph != "investigation" {
		t.Fatalf("PTR-BUG-07 must return to investigation, got %q", ph)
	}
}
