// ct_rework_test.go — BUG-039-39 specification rework / amendment paths:
// TR-004 document_fix_required, TR-023 finding_spec_change, TR-024 pause,
// and CT-039-18 g2 rework via system Hook CLI.

package req039_test

import (
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestTR004_DocumentFixRequiredReturnsPlanningSystem covers TR-004:
// document_verification + fix_required DV → PreToolUse → planning.design.
func TestTR004_DocumentFixRequiredReturnsPlanningSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "design", 21)
	req039fixtures.SeedDocumentFixRequiredS5(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-tr-004", "Edit", map[string]any{
		"file_path": "docs/contracts/BE-039-loop-controller.md",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("TR-004 must not use manual transition CLI")
	}
	if !strings.Contains(stdout, "TR-004") && !strings.Contains(stdout, "GATE-DOCUMENT-FIX-REQUIRED") {
		t.Fatalf("TR-004 must surface TR-004 / GATE-DOCUMENT-FIX-REQUIRED, got %s", stdout)
	}
	after := req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, after, "planning", "design")
	req039fixtures.AssertLastTransition(t, after, "TR-004")
}

// TestTR023_FindingSpecChangeReturnsPlanningSystem covers TR-023:
// bug_resolution + finding_spec_change_required → planning.design.
func TestTR023_FindingSpecChangeReturnsPlanningSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 33)
	req039fixtures.SeedFindingSpecChangeRequired(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-tr-023", "Edit", map[string]any{
		"file_path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("TR-023 must not use manual transition CLI")
	}
	after := req039fixtures.ReadState(t, root)
	lc, ph := req039fixtures.Lifecycle(after)
	if lc != "planning" || ph != "design" {
		t.Fatalf("TR-023 must land planning.design, got %q/%q stdout=%s", lc, ph, stdout)
	}
	req039fixtures.AssertLastTransition(t, after, "TR-023")
}

// TestTR024_FindingReqChangePausesSystem covers TR-024:
// bug_resolution + finding_req_change_required → paused.
func TestTR024_FindingReqChangePausesSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 34)
	req039fixtures.SeedFindingReqChangeRequired(t, root, state)
	writeSystemState(t, root, state)

	body := req039fixtures.PreToolUseBody("session-tr-024", "Edit", map[string]any{
		"file_path": "docs/requirements/REQ-039-loop-control-plane.md",
	})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("PreToolUse failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("TR-024 must not use manual transition CLI")
	}
	after := req039fixtures.ReadState(t, root)
	lc, _ := req039fixtures.Lifecycle(after)
	if lc != "paused" {
		t.Fatalf("TR-024 must land paused, got %q stdout=%s", lc, stdout)
	}
	req039fixtures.AssertLastTransition(t, after, "TR-024")
}

// TestCT03918_G2ReworkOldArtifactImmutableSystem covers CT-039-18 at L4:
// g2 active path allowed via Hook; g1 remains superseded in manifest.
func TestCT03918_G2ReworkOldArtifactImmutableSystem(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := req039fixtures.BaseState(t, root, "document_verification", "", 20)
	req039fixtures.SeedG2Rework(t, root, state)
	writeSystemState(t, root, state)

	after := req039fixtures.ReadState(t, root)
	baseline, _ := after["baseline"].(map[string]any)
	if gen, _ := baseline["generation"].(float64); gen != 2 {
		t.Fatalf("CT-039-18 baseline generation must be 2, got %v", baseline["generation"])
	}
	docs, _ := after["documents"].([]any)
	var g1Status, g2Status string
	for _, raw := range docs {
		doc, _ := raw.(map[string]any)
		if gen, _ := doc["generation"].(float64); gen == 1 {
			g1Status, _ = doc["status"].(string)
		}
		if gen, _ := doc["generation"].(float64); gen == 2 {
			g2Status, _ = doc["status"].(string)
		}
	}
	if g1Status != "superseded" {
		t.Fatalf("CT-039-18 g1 must be superseded, got %q", g1Status)
	}
	if g2Status != "locked" {
		t.Fatalf("CT-039-18 g2 must be locked, got %q", g2Status)
	}

	body := req039fixtures.PreToolUseBody("session-ct-039-18-sys", "Edit", map[string]any{
		"file_path": "docs/design/versions/REQ-039/g2/ARCHITECTURE-039-loop-control-plane.md",
	})
	code, _, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code == 2 {
		t.Fatalf("CT-039-18 g2 active path must not be blocked: stderr=%s", stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-18 must not use manual transition CLI")
	}
}
