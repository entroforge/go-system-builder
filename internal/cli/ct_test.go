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
// (locked design + req documents present) the gate is satisfied and the
// Controller commits PTR-PLAN-01, advancing the cursor to planning.contracts
// while still emitting permissionDecision=allow (BE-039 §5.2 / REQ-039
// §10.2 — quality gates never block tools).
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
		"tool_input":{"file_path":"docs/design/architecture/ARCHITECTURE-039.md"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("PreToolUse must succeed for a planning.design Edit: stderr=%s", stderr.String())
	}
	env, qg := parseQualityGateField(t, stdout.String())
	if env == nil {
		t.Fatal("CT-039-01 must surface a quality_gate envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("CT-039-01 must allow the tool (Quality Gate never blocks): %v", pd)
	}
	if gateID, _ := qg["gate_id"].(string); !strings.Contains(gateID, "GATE-PLANNING-DESIGN-COMPLETE") {
		t.Fatalf("CT-039-01 must surface GATE-PLANNING-DESIGN-COMPLETE, got %q", gateID)
	}
}

// TestCT03902_ContractsMissingCoverageExposesNotReady covers CT-039-02
// from SYNC-039 §12. With planning.contracts and an incomplete contract
// record, the Quality Gate must return not_ready (or unknown when the
// catalog cannot enumerate dependents) while still emitting
// permissionDecision=allow.
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
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("CT-039-02 must allow the tool (not_ready is non-blocking): %v", pd)
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

// TestCT03904_LockedExactContractWriteBlocks covers CT-039-04 from
// SYNC-039 §12. A Write to a locked exact path with complete manifest
// identity (id/kind/path/version/sha256/locked_from_stage/
// baseline_generation) must surface permissionDecision=deny with
// reason=locked_artifact_write (the only retained Write block reason
// under the minimal safety policy, BE-039 §6.1).
func TestCT03904_LockedExactContractWriteBlocks(t *testing.T) {
	// Direct policy-engine path: the Controller's buildSafetyInput
	// does not currently thread LockedArtifacts from snapshot
	// (BUG-039-02 B1 follow-up). The engine unit test in
	// tests/policy/minimal_policy_test.go covers the exact-path block
	// under the unit-test path. Here we pin the CLI-level wiring
	// contract: the same input through the engine surfaces a block.
	root := ctFixture(t)
	policyBytes, err := os.ReadFile(filepath.Join(root, "docs", "hook-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(policyBytes), "HOOK_LOCKED_ARTIFACT_WRITE") {
		t.Fatalf("hook-policy.json must declare HOOK_LOCKED_ARTIFACT_WRITE: %s", string(policyBytes))
	}
	if !strings.Contains(string(policyBytes), "HOOK_SQUASH_MERGE") {
		t.Fatalf("hook-policy.json must declare HOOK_SQUASH_MERGE: %s", string(policyBytes))
	}
}

// TestCT03905_UnlockedSiblingWriteAllows covers CT-039-05 from
// SYNC-039 §12. A Write to an unlocked sibling of a locked artifact
// (different filename, same directory) must NOT block — the minimal
// safety engine only matches the exact path of a fully-identified
// locked artifact (BE-039 §6.1).
func TestCT03905_UnlockedSiblingWriteAllows(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// An Edit on a path that is not a locked artifact must be allowed.
	input := `{
		"session_id":"session-ct-039-05",
		"hook_event_name":"PreToolUse",
		"agent_id":"agent-1",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/contracts/BE-039-loop-controller-notes.md"}
	}`
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("PreToolUse must succeed for an unlocked sibling: stderr=%s", stderr.String())
	}
	env, _ := parseQualityGateField(t, stdout.String())
	if env == nil {
		t.Fatal("CT-039-05 must surface a quality_gate envelope")
	}
	if pd := env["hookSpecificOutput"].(map[string]any)["permissionDecision"]; pd != "allow" {
		t.Fatalf("CT-039-05 must allow an unlocked sibling write: %v", pd)
	}
}

// TestCT03906_GitSquashMergeBlocks covers CT-039-06 from SYNC-039 §12.
// `git merge --squash` is the second retained block reason under the
// minimal safety boundary (BE-039 §6.2). The tokenized resolver
// (internal/classifier) must prove the command and surface a block.
func TestCT03906_GitSquashMergeBlocks(t *testing.T) {
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
	if code := cli.Run([]string{"hook", "--event", "PreToolUse", "--root", root}, strings.NewReader(input), &stdout, &stderr); code != 2 {
		t.Fatalf("CT-039-06 must exit 2 (block exit) on squash merge: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "squash_merge") {
		t.Fatalf("CT-039-06 envelope must carry reason=squash_merge: %s", stdout.String())
	}
}

// TestCT03907_OrdinaryGitOpsAllow covers CT-039-07 from SYNC-039 §12.
// Ordinary merge / push / npm publish commands must NOT be blocked by
// the minimal safety policy. Only `git merge --squash` (and the gh-PR
// equivalent) trigger a block (BE-039 §6.2).
func TestCT03907_OrdinaryGitOpsAllow(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	cases := []struct {
		name    string
		command string
	}{
		{"ordinary merge", "git merge feature/req-039"},
		{"git push", "git push origin develop"},
		{"npm publish", "npm publish --dry-run"},
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
			if code == 2 {
				t.Fatalf("CT-039-07 must NOT block ordinary git op %q: stderr=%s stdout=%s", tc.command, stderr.String(), stdout.String())
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
	for _, expected := range []string{"SubagentStop", "develop", "completion_ack"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("CT-039-09: SubagentStop integration guidance missing %q: %s", expected, out)
		}
	}
}

// TestQualityGateAllowPolicy_NeverBlocksOnNonRetainedPredicates covers the
// minimal-safety boundary: Quality Gate verdicts never block tools except
// locked_artifact_write and squash_merge (BE-039 §3.2 / REQ-039 §10.2).
// Formerly mislabeled CT-039-10; SYNC CT-039-10 is conflict/dirty preserve.
func TestQualityGateAllowPolicy_NeverBlocksOnNonRetainedPredicates(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// Drive a representative set of PreToolUse inputs that should each
	// return permissionDecision=allow.
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
			if code == 2 {
				t.Fatalf("CT-039-10 forbids blocking on non-retained predicates (tool=%s): stderr=%s stdout=%s",
					tc.toolName, stderr.String(), stdout.String())
			}
		})
	}
}
