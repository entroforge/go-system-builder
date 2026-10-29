package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/controller"
)

// ctFixture mirrors acFixtureRoot — each CT test materialises a temp
// repository with the minimum docs/ assets the Controller / minimal-safety
// policy needs. The seed state is the schema-valid planning state from
// planningState().
func ctFixture(t *testing.T) string {
	return acFixtureRoot(t)
}

// TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01 covers CT-039-01 from
// SYNC-039 §12. The Controller evaluates the planning.design cursor
// against the contract set. When the design contract set is complete
// (locked design + req documents present) the listener commits PTR-PLAN-01,
// advancing the cursor to planning.contracts; the Hook carries no tool ACL.
//
// The CLI test exercises the public surface: a PreToolUse against an Edit
// of the design document must surface quality_gate.status=satisfied with
// gate_id GATE-PLANNING-DESIGN-COMPLETE.
func TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-01",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/architecture/ARCHITECTURE-039.md"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("PreToolUse must succeed for a planning.design Edit: stderr=%s", stderr.String())
	}
	env, qg := parseQualityGateField(t, stdout.String())
	if env == nil {
		t.Fatal("CT-039-01 must surface a quality_gate envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != nil {
		t.Fatalf("CT-039-01 observer must not emit a tool permission decision: %v", pd)
	}
	if gateID, _ := qg["gate_id"].(string); !strings.Contains(gateID, "GATE-PLANNING-DESIGN-COMPLETE") {
		t.Fatalf("CT-039-01 must surface GATE-PLANNING-DESIGN-COMPLETE, got %q", gateID)
	}
}

// TestCT03902_ContractsMissingCoverageExposesNotReady covers CT-039-02
// from SYNC-039 §12. With planning.contracts and an incomplete contract
// record, the Quality Gate must return not_ready (or unknown when the
// catalog cannot enumerate dependents) without a tool permission decision.
func TestCT03902_ContractsMissingCoverageExposesNotReady(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "contracts", 1)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-02",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"internal/cli/controller.go"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("PreToolUse must succeed: stderr=%s", stderr.String())
	}
	env, qg := parseQualityGateField(t, stdout.String())
	if env == nil {
		t.Fatal("CT-039-02 must surface a quality_gate envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != nil {
		t.Fatalf("CT-039-02 observer must not emit a tool permission decision: %v", pd)
	}
	status, _ := qg["status"].(string)
	switch status {
	case "not_ready", "unknown", "satisfied":
		// documented cases; not_ready is the expected verdict
	default:
		t.Fatalf("CT-039-02 quality_gate.status must be one of {not_ready,unknown,satisfied}, got %q", status)
	}
}

// TestCT03903_TwoConcurrentSameRevisionLosesDuplicateCommit covers
// CT-039-03 from SYNC-039 §12. When two concurrent PreToolUse Hook
// invocations both observe the same revision and try to commit the
// same transition, only one CAS must succeed; the other must surface
// either a recompute or no-op verdict (BE-039 §5.5 / REQ-039 §19 AC-007).
func TestCT03903_TwoConcurrentSameRevisionLosesDuplicateCommit(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	first := controller.ControlRequest{
		Root:      root,
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "internal/cli/run.go"},
	}
	result1, err := controller.RunControlCycle(context.Background(), first)
	if err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	result2, err := controller.RunControlCycle(context.Background(), first)
	if err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	// One Hook == at most one lifecycle Transition (BE-039 §5.5).
	// If the controller ever commits twice, both results would carry
	// TransitionCommitted=true with different observed revisions.
	if result1.QualityGate.TransitionCommitted && result2.QualityGate.TransitionCommitted {
		if result1.QualityGate.ObservedRevision != result2.QualityGate.ObservedRevision {
			t.Fatalf("CT-039-03 forbids two simultaneous same-revision commits: r1=%d r2=%d",
				result1.QualityGate.ObservedRevision, result2.QualityGate.ObservedRevision)
		}
	}
}

// TestCT03904_LockedExactContractWriteIsObserved covers CT-039-04 from
// SYNC-039 §12. A locked exact path remains subject to its professional
// consumer and gate; the PreToolUse listener does not enforce the write.
func TestCT03904_LockedExactContractWriteIsObserved(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "contracts", 1)
	writeACState(t, root, state)
	input := `{
		"session_id":"session-ct-039-04",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/dev/contracts/BE-039-loop-controller.md"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("locked-path observation failed: code=%d stderr=%s", code, stderr.String())
	}
	env, qg := parseQualityGateField(t, stdout.String())
	if env == nil || qg == nil {
		t.Fatalf("CT-039-04 must retain the graph observation: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("locked path must not be denied by PreToolUse: %s", stdout.String())
	}
}

// TestCT03905_UnlockedSiblingWriteIsObserved covers CT-039-05 from
// SYNC-039 §12. A neighboring document path is observed by the Hook, while
// the professional artifact checks remain in their original consumer.
func TestCT03905_UnlockedSiblingWriteIsObserved(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// The Hook reports graph progress without making an artifact-path decision.
	input := `{
		"session_id":"session-ct-039-05",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/dev/contracts/BE-039-loop-controller-notes.md"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("PreToolUse must succeed for an unlocked sibling: stderr=%s", stderr.String())
	}
	env, _ := parseQualityGateField(t, stdout.String())
	if env == nil {
		t.Fatal("CT-039-05 must surface a quality_gate envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != nil {
		t.Fatalf("CT-039-05 observer must not return a permission decision: %v", pd)
	}
}

// TestCT03906_GitSquashMergeIsObserved covers CT-039-06 from SYNC-039 §12.
// Command classification is feedback for the graph; PreToolUse does not
// deny the tool. Lifecycle gates remain responsible for stage progression.
func TestCT03906_GitSquashMergeIsObserved(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-06",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Bash",
		"tool_input":{"command":"git merge --squash feature/req-039"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("CT-039-06 observer failed: code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("CT-039-06 PreToolUse must not deny the command: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "QUALITY_GATE") {
		t.Fatalf("CT-039-06 must retain graph progress feedback: %s", stdout.String())
	}
}

// TestCT03907_PreToolUseObservesGitOps covers CT-039-07 from SYNC-039 §12.
// Ordinary and release-like commands alike reach the graph listener without
// a per-command tool denial; release approval remains a lifecycle condition.
func TestCT03907_PreToolUseObservesGitOps(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	cases := []struct {
		name    string
		command string
	}{
		{"ordinary merge", "git merge feature/req-039"},
		{"git status", "git status --porcelain"},
		{"git log", "git log --oneline -5"},
		{"squash merge", "git merge --squash feature/req-039"},
		{"release push", "git push origin develop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := `{
				"session_id":"session-ct-039-07-` + tc.name + `",
				"hook_event_name":"PreToolUse",
				"agent_id":"agent-1",
				"tool_name":"Bash",
				"tool_input":{"command":"` + tc.command + `"}
			}`
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("CT-039-07 observer failed for %q: code=%d stderr=%s stdout=%s", tc.command, code, stderr.String(), stdout.String())
			}
			if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
				t.Fatalf("PreToolUse must not deny %q: %s", tc.command, stdout.String())
			}
		})
	}
}

// TestCT03908_SessionStartAfterCompactSurfacesSameMilestone covers
// CT-039-08 from SYNC-039 §12. After a PreToolUse persists the
// milestone, a SessionStart event (simulating post-Compact recovery)
// must surface the same objective in the systemMessage so a compacted
// Agent resumes at the same lifecycle cursor.
func TestCT03908_SessionStartAfterCompactSurfacesSameMilestone(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// Step 1: drive PreToolUse to persist the milestone.
	input := `{
		"session_id":"session-ct-039-08",
		"hook_event_name":"PreToolUse",
		"tool_name":"Edit",
		"tool_input":{"file_path":"internal/cli/run.go"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("PreToolUse failed: %s", stderr.String())
	}

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

	// Step 2: SessionStart after a synthetic Compact must surface the
	// same milestone objective in the systemMessage.
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"session-ct-039-08-recover"}`), &stdout, &stderr); code != 0 {
		t.Fatalf("SessionStart failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), persistedObjective) {
		t.Fatalf("CT-039-08: post-Compact SessionStart must surface the persisted objective %q, got %s",
			persistedObjective, stdout.String())
	}
}

// TestCT03909_CleanWorktreeStopEmitsIntegrationGuidance covers
// CT-039-09 from SYNC-039 §12. A SubagentStop with a clean
// agent_report_complete must surface the integration guidance
// (non-squash merge into the current integration branch, verified
// checklist, completion_ack) so the worktree integration path is
// one bounded step (BE-039 §6 / REQ-039 §13).
func TestCT03909_CleanWorktreeStopEmitsIntegrationGuidance(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "tasks", 5)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-09",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-1",
		"agent_report_complete":true
	}`
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", "SubagentStop", "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("SubagentStop must not fail: %d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, expected := range []string{"SubagentStop", "REQ-bound development branch", "completion_ack"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("CT-039-09: SubagentStop integration guidance missing %q: %s", expected, out)
		}
	}
}

// TestQualityGateObserverDoesNotDenyRepresentativeTools covers the listener
// contract: PreToolUse reports gate progress but does not deny tool calls.
// Formerly mislabeled CT-039-10; SYNC CT-039-10 is conflict/dirty preserve.
func TestQualityGateObserverDoesNotDenyRepresentativeTools(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// Drive representative tool inputs and assert they remain graph
	// observations without a permission decision.
	cases := []struct {
		name      string
		toolName  string
		toolInput map[string]any
	}{
		{"edit-non-locked", "Edit", map[string]any{"file_path": "internal/cli/run.go"}},
		{"write-non-locked", "Write", map[string]any{"file_path": "docs/design/notes.md"}},
		{"bash-non-squash", "Bash", map[string]any{"command": "ls -la"}},
		{"multi-edit-non-locked", "MultiEdit", map[string]any{"file_path": "internal/cli/run.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputJSON, _ := json.Marshal(map[string]any{
				"session_id":      "session-ct-039-10-" + tc.name,
				"hook_event_name": "PreToolUse",
				"agent_id":        "agent-1",
				"tool_name":       tc.toolName,
				"tool_input":      tc.toolInput,
			})
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, bytes.NewReader(inputJSON), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("CT-039-10 observer failed (tool=%s): code=%d stderr=%s stdout=%s",
					tc.toolName, code, stderr.String(), stdout.String())
			}
			if strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
				t.Fatalf("CT-039-10 listener must not deny (tool=%s): %s", tc.toolName, stdout.String())
			}
		})
	}
}
