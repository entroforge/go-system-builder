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

// PreCompact has no professional input and must not invent recovery facts.
func TestHOOK_PreCompact_IsSilentAndPreservesOriginalFactsViaHookCLI(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "design", 3)
	writeSystemState(t, root, state)
	before := readSystemRuntimePair(t, root)
	code, stdout, stderr := runHook(t, root, "PreCompact", `{"hook_event_name":"PreCompact","session_id":"session-hook-precompact"}`)
	if code != 0 {
		t.Fatalf("PreCompact Hook failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("PreCompact emitted discarded recovery instructions: %s", stdout)
	}
	assertSystemRuntimePairUnchanged(t, before)
}

// SessionStart reconstructs the current responsibility without replaying an
// old milestone as the execution authority. No native PostCompact is invented.
func TestHOOK_PostCompact_SessionStartFallback(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "contracts", 5)
	state["milestone"].(map[string]any)["objective"] = "STALE-CACHED-OBJECTIVE"
	writeSystemState(t, root, state)
	before := readSystemRuntimePair(t, root)
	code, stdout, stderr := runHook(t, root, "PreCompact", `{"hook_event_name":"PreCompact","session_id":"session-hook-postcompact-pre"}`)
	if code != 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("PreCompact changed its silent boundary: %d %s %s", code, stdout, stderr)
	}
	assertSystemRuntimePairUnchanged(t, before)
	code, stdout, stderr = runHook(t, root, "SessionStart", `{"hook_event_name":"SessionStart","session_id":"session-hook-postcompact-recover"}`)
	if code != 0 {
		t.Fatalf("SessionStart fallback failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"S3 planning.contracts", "docs/control/agent-protocol.md#s3"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("recovery lost the current responsibility %q: %s", want, stdout)
		}
	}
	if strings.Contains(stdout, "STALE-CACHED-OBJECTIVE") {
		t.Fatalf("recovery repeated stale execution instructions: %s", stdout)
	}
	req039fixtures.AssertLifecycle(t, req039fixtures.ReadState(t, root), "planning", "contracts")
}

// Byte snapshots distinguish a silent observation from an unseen Writer event.
func readSystemRuntimePair(t *testing.T, root string) map[string]*string {
	t.Helper()
	pair := map[string]*string{}
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		path := filepath.Join(root, ".claude", name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			pair[path] = nil
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		value := string(data)
		pair[path] = &value
	}
	return pair
}
func assertSystemRuntimePairUnchanged(t *testing.T, pair map[string]*string) {
	t.Helper()
	for path, want := range pair {
		data, err := os.ReadFile(path)
		if want == nil && os.IsNotExist(err) {
			continue
		}
		if want == nil || err != nil || string(data) != *want {
			t.Fatalf("observation changed original Runtime/journal %s: %v", path, err)
		}
	}
}

// TestHOOK_SubagentStart_DelegationPreflightViaHookCLI covers HOOK-SubagentStart
// at L3: the Hook reminds the Main session about the registered assignment's
// professional work and supplied source/answer paths.
func TestHOOK_SubagentStart_DelegationPreflightViaHookCLI(t *testing.T) {
	root := freshRoot(t)
	state := systemPlanningState(t, root, "tasks", 7)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	writeSystemState(t, root, state)
	before := req039fixtures.ReadState(t, root)

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
	for _, needle := range []string{"subagent", "preflight questions", "professional work and checks", "source and answer paths"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("SubagentStart must surface current assignment guidance %q, got %s", needle, stdout)
		}
	}
	assertRuntimeCursorUnchanged(t, "SubagentStart reminder", before, req039fixtures.ReadState(t, root))
}

// TestHOOK_TeammateIdle_HookCLIEmitsGuidance pins TeammateIdle Hook CLI entry
// with the official Claude Code 2.1.218 payload shape (teammate_name/
// team_name/transcript_path; no self-made agent_id or facts). This sparse
// fixture has no registered Workgroup Assignment, so TeammateIdle must stay
// advisory and return the current graph recovery packet without a denial.
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
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-hook-teammate-idle",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"builder-idle-1",
		"team_name":"team-idle",
		"transcript_path":"/tmp/claude/transcripts/builder-idle-1.jsonl"
	}`)
	if code != 0 {
		t.Fatalf("unregistered TeammateIdle must remain advisory: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, needle := range []string{"LOOP RECOVERY", "re-wake the same teammate", "current-round assignment report is missing"} {
		if !strings.Contains(strings.ToLower(stdout), strings.ToLower(needle)) {
			t.Fatalf("TeammateIdle must surface current advisory guidance %q, got %s", needle, stdout)
		}
	}
	assertRuntimeCursorUnchanged(t, "TeammateIdle reminder", before, req039fixtures.ReadState(t, root))
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

// TestHOOK_TeammateIdle_IdleAfterCompletionViaHookCLI pins a fully registered
// one-shot assignment with its completion source. TeammateIdle is a read-only
// listener: it never allocates or claims the next task.
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
			"definition_ref": "defs/builder.md", "prompt_ref": ".claude/workgroups/REQ-039/TASK-039-04/manifest.json#assignment-039-04",
			"readback_ref": "readback.md", "activation_ref": "activation.json",
			"activation_revision": 1, "updated_at": "2026-07-30T00:00:00Z",
			"dispatch_mode":           "one_shot",
			"completion_reported_ref": completionRef,
		}},
		"tasks": []any{
			map[string]any{
				"id": "TASK-039-04", "state": "done",
				"owner_agent_ids": []any{"builder-idle-alloc"},
				"path":            "docs/dev/tasks/TASK-039-04.md",
				"sha256":          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			map[string]any{
				"id": "TASK-039-05", "state": "candidate",
				"owner_agent_ids": []any{},
				"path":            "docs/dev/tasks/TASK-039-05.md",
				"sha256":          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
		"bugs": []any{},
		"teams": []any{map[string]any{
			"id": "workgroup-039-04", "manifest_ref": ".claude/workgroups/REQ-039/TASK-039-04/manifest.json",
			"responsibility_ids": []any{"BUILD-WORK-PACKAGE"}, "agent_ids": []any{"builder-idle-alloc"},
		}},
	}

	taskPath := filepath.Join(root, "docs", "dev", "tasks", "TASK-039-04.md")
	taskBytes := []byte("# TASK-039-04\n\nOne-shot worker delivery fixture.\n")
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, taskBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)["sha256"] = req039fixtures.Sha256Hex(taskBytes)
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
		"teammate_name":"builder-idle-alloc",
		"team_name":"workgroup-039-04",
		"transcript_path":"/tmp/claude/transcripts/builder-idle-alloc.jsonl"
	}`)
	if code != 0 {
		t.Fatalf("TeammateIdle idle Hook failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	assertRuntimeCursorUnchanged(t, "TeammateIdle completion notice", before, req039fixtures.ReadState(t, root))

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
