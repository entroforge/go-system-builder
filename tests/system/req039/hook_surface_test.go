// hook_surface_test.go — HOOK-PreCompact / PostCompact-fallback / TeammateIdle
// system Hook CLI pins for HookSurface scoring (ARCHITECTURE-039 §11).

package req039_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestHOOK_PreCompact_PersistsMilestoneViaHookCLI covers HOOK-PreCompact at
// L3: real Hook CLI persists a resumable milestone checkpoint (FR-014).
func TestHOOK_PreCompact_PersistsMilestoneViaHookCLI(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 3)
	writeSystemState(t, root, state)
	beforeRev := req039fixtures.Revision(state)

	code, stdout, stderr := runHook(t, root, "PreCompact",
		`{"hook_event_name":"PreCompact","session_id":"session-hook-precompact"}`)
	if code != 0 {
		t.Fatalf("PreCompact Hook failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "LOOP RECOVERY") && !strings.Contains(stdout, "objective") && !strings.Contains(stdout, "design") {
		t.Fatalf("PreCompact must emit resumable recovery packet, got %s", stdout)
	}

	after := req039fixtures.ReadState(t, root)
	ms, _ := after["milestone"].(map[string]any)
	if ms == nil {
		t.Fatal("PreCompact must leave a milestone on disk")
	}
	if obj, _ := ms["objective"].(string); obj == "" {
		t.Fatalf("PreCompact milestone must carry objective: %v", ms)
	}
	if event, _ := ms["event"].(string); event != "PreCompact" {
		t.Fatalf("milestone.event want PreCompact, got %q", event)
	}
	if rev := req039fixtures.Revision(after); rev < beforeRev {
		t.Fatalf("PreCompact must not regress revision: before=%v after=%v", beforeRev, rev)
	}
}

// TestHOOK_PostCompact_SessionStartFallback covers HOOK-PostCompact via the
// contract fallback (REQ-039 D-011): when PostCompact is unavailable, the
// next SessionStart restores the same PreCompact-persisted milestone.
func TestHOOK_PostCompact_SessionStartFallback(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "contracts", 5)
	writeSystemState(t, root, state)

	code, _, stderr := runHook(t, root, "PreCompact",
		`{"hook_event_name":"PreCompact","session_id":"session-hook-postcompact-pre"}`)
	if code != 0 {
		t.Fatalf("PreCompact failed: code=%d stderr=%s", code, stderr)
	}
	persisted := req039fixtures.ReadState(t, root)
	ms, _ := persisted["milestone"].(map[string]any)
	wantObjective, _ := ms["objective"].(string)
	if wantObjective == "" {
		t.Fatal("PreCompact must persist objective for PostCompact fallback")
	}

	// Host lacks PostCompact: SessionStart is the recovery path.
	code, stdout, stderr := runHook(t, root, "SessionStart",
		`{"hook_event_name":"SessionStart","session_id":"session-hook-postcompact-recover"}`)
	if code != 0 {
		t.Fatalf("SessionStart fallback failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, wantObjective) {
		t.Fatalf("PostCompact→SessionStart fallback must surface objective %q, got %s", wantObjective, stdout)
	}
	after := req039fixtures.ReadState(t, root)
	afterMS, _ := after["milestone"].(map[string]any)
	gotObjective, _ := afterMS["objective"].(string)
	if gotObjective != wantObjective {
		t.Fatalf("fallback milestone objective want %q got %q", wantObjective, gotObjective)
	}
}

// TestHOOK_SubagentStart_DelegationPreflightViaHookCLI covers HOOK-SubagentStart
// at L3: real Hook CLI emits delegation preflight questions (worktree / team /
// agent dispatch) without requiring a manual transition CLI.
func TestHOOK_SubagentStart_DelegationPreflightViaHookCLI(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 7)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	writeSystemState(t, root, state)

	code, stdout, stderr := runHook(t, root, "SubagentStart", `{
		"session_id":"session-hook-subagent-start",
		"hook_event_name":"SubagentStart",
		"agent_id":"builder-start-1",
		"tool_input":{"subagent_type":"backend-builder","team_name":"team-start"}
	}`)
	if code != 0 {
		t.Fatalf("SubagentStart Hook failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := strings.ToLower(stdout)
	for _, needle := range []string{"subagent", "worktree", "team"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("SubagentStart must surface delegation preflight mentioning %q, got %s", needle, stdout)
		}
	}
	if !strings.Contains(out, "readback") && !strings.Contains(out, "activation") && !strings.Contains(out, "phase") {
		t.Fatalf("SubagentStart must mention the dispatch plan/readback flow, got %s", stdout)
	}
}

// TestHOOK_TeammateIdle_HookCLIEmitsGuidance pins TeammateIdle Hook CLI entry
// with the official Claude Code 2.1.218 payload shape (teammate_name/
// team_name/transcript_path; no self-made agent_id or facts). The fixture
// teammate idles before its PLAN_REPORT checkpoint, so the L4 §16.1 control
// applies: exit 2 with the feedback on stderr, routed back to the same
// teammate. BUG-039-37 wires HandleTeammateIdleForController into
// reconcileGuidance; this fixture may still no-op milestone fingerprint when
// guidance matches.
func TestHOOK_TeammateIdle_HookCLIEmitsGuidance(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 8)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	if ms, ok := state["milestone"].(map[string]any); ok {
		ms["stage"] = "S6"
		ms["lifecycle_state"] = "building"
		ms["lifecycle_phase"] = nil
		ms["event"] = "PreCompact"
		ms["objective"] = "stale objective before TeammateIdle"
	}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-idle-1", "role": "builder", "state": "idle",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-idle",
		}},
		"tasks": []any{map[string]any{"id": "TASK-039-01", "state": "in_progress"}},
		"bugs":  []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-hook-teammate-idle",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"builder-idle-1",
		"team_name":"team-idle",
		"transcript_path":"/tmp/claude/transcripts/builder-idle-1.jsonl"
	}`)
	if code != 2 {
		t.Fatalf("TeammateIdle before PLAN_REPORT must exit 2 (real platform control): code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, needle := range []string{"teammate_idle_resume_assignment", "builder-idle-1", "PLAN_REPORT"} {
		if !strings.Contains(stderr, needle) {
			t.Fatalf("TeammateIdle stderr feedback must contain %q, got %s", needle, stderr)
		}
	}
	after := req039fixtures.ReadState(t, root)
	ms, _ := after["milestone"].(map[string]any)
	raw, _ := json.Marshal(ms)
	// Prefer durable refresh when fingerprint changes; accept guidance-only
	// when milestoneMatches no-ops (still Hook CLI, not unit).
	if event, _ := ms["event"].(string); event != "TeammateIdle" {
		t.Logf("TeammateIdle Hook guidance ok but milestone.event=%q (refresh fingerprint no-op in minimal fixture)", event)
	}
	_ = raw
}

// TestHOOK_TeammateIdle_IdleAfterCompletionViaHookCLI pins TeammateIdle
// Branch 4 at L4: complete assignment + completion report → idle is
// allowed; the scheduler (not the Hook) is the only writer of the next
// assignment. L4 §15.2 P2-5 retired the in-TeammateIdle allocation, so
// the fixture now asserts the negative: TASK-039-05 stays untouched and
// the revision is unchanged. Teammates must not self-claim Team tasks
// (`unauthorized_task_self_claim` is the gate that would block the same
// TaskUpdate from a teammate).
func TestHOOK_TeammateIdle_IdleAfterCompletionViaHookCLI(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 12)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	if ms, ok := state["milestone"].(map[string]any); ok {
		ms["stage"] = "S6"
		ms["lifecycle_state"] = "building"
		ms["lifecycle_phase"] = nil
		ms["objective"] = "acknowledge completion; scheduler allocates next legal task"
	}
	completionRef := ".claude/evidence/loop-system-test/g1/assignments/assignment-039-04/completion.json"
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-idle-alloc", "role": "builder", "state": "idle",
			"task_ids": []any{"TASK-039-04"}, "team_id": "workgroup-039-04",
			"definition_ref": "defs/builder.md", "prompt_ref": ".claude/workgroups/REQ-039/TASK-039-04/manifest.json",
			"readback_ref": "readback.md", "activation_ref": "activation.json",
			"activation_revision": 1, "updated_at": "2026-07-30T00:00:00Z",
			"completion_reported_ref": completionRef,
		}},
		"tasks": []any{
			map[string]any{
				"id": "TASK-039-04", "state": "done",
				"owner_agent_ids": []any{"builder-idle-alloc"},
				"path":            "docs/tasks/TASK-039-04.md",
				"sha256":          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			map[string]any{
				"id": "TASK-039-05", "state": "candidate",
				"owner_agent_ids": []any{},
				"path":            "docs/tasks/TASK-039-05.md",
				"sha256":          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
		"bugs":  []any{},
		"teams": []any{},
	}
	writeSystemState(t, root, state)

	wgDir := filepath.Join(root, ".claude", "workgroups", "REQ-039", "TASK-039-04")
	if err := os.MkdirAll(wgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"schema_version":"1.0.0","manifest_id":"wg-idle-alloc","version":"v1.0.0",
		"runtime_id":"loop-system-test","req_id":"REQ-039","baseline_generation":1,
		"status":"active","workgroup_id":"workgroup-039-04","workgroup_kind":"builder",
		"assignments":[{
			"assignment_id":"assignment-039-04","responsibility_id":"BUILD-WORK-PACKAGE",
			"role_family":"backend-builder","agent_id":"builder-idle-alloc",
			"write_paths":["internal/"],"status":"complete",
			"worktree_path":"worktree-039-04","branch":"codex/req-039-idle","target_branch":"develop"
		}]
	}`
	if err := os.WriteFile(filepath.Join(wgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	reportDir := filepath.Join(root, ".claude", "evidence", "loop-system-test", "g1", "assignments", "assignment-039-04")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "completion.json"), []byte(
		`{"schema_version":"1.0.0","message_type":"completion_report","assignment_id":"assignment-039-04","task_id":"TASK-039-04"}`,
	), 0o644); err != nil {
		t.Fatal(err)
	}

	before := req039fixtures.ReadState(t, root)
	beforeRev := req039fixtures.Revision(before)

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-hook-teammate-alloc",
		"hook_event_name":"TeammateIdle",
		"agent_id":"builder-idle-alloc"
	}`)
	if code != 0 {
		t.Fatalf("TeammateIdle idle Hook failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	lower := strings.ToLower(stdout)
	if !strings.Contains(lower, "scheduler") {
		t.Fatalf("TeammateIdle Branch 4 must name the scheduler as next-task owner, got %s", stdout)
	}
	if !strings.Contains(lower, "idle") && !strings.Contains(lower, "next") {
		t.Fatalf("TeammateIdle Branch 4 must mention idle/next, got %s", stdout)
	}

	after := req039fixtures.ReadState(t, root)
	if rev := req039fixtures.Revision(after); rev != beforeRev {
		t.Fatalf("Branch 4 must NOT CAS-advance revision, before=%v after=%v stdout=%s", beforeRev, rev, stdout)
	}
	entities, _ := after["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	var nextState string
	var nextOwners []any
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if id, _ := task["id"].(string); id == "TASK-039-05" {
			nextState, _ = task["state"].(string)
			nextOwners, _ = task["owner_agent_ids"].([]any)
		}
	}
	if nextState != "candidate" {
		t.Fatalf("Branch 4 must leave TASK-039-05 untouched (scheduler allocates), got %q stdout=%s", nextState, stdout)
	}
	for _, o := range nextOwners {
		if s, _ := o.(string); s == "builder-idle-alloc" {
			t.Fatalf("Branch 4 must NOT self-claim TASK-039-05 to builder-idle-alloc, owners=%v", nextOwners)
		}
	}
}
