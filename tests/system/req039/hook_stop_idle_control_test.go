// hook_stop_idle_control_test.go — L4 §15.2 P0 / §16.1 acceptance tests for
// the real platform control on TeammateIdle / SubagentStop and the
// PreToolUse(TaskUpdate) self-claim guard.
//
// All payloads use the Claude Code 2.1.218 official shapes as recorded in
// the platform documentation: TeammateIdle carries
// teammate_name/team_name/transcript_path (no agent_id), SubagentStop
// carries agent_id/agent_transcript_path/last_assistant_message/
// stop_hook_active. No self-made facts or agent_id on TeammateIdle.
//
// HONESTY NOTE: these tests are written against the documented payload
// shapes; no real Claude Code 2.1.218 runtime was available to doctor the
// wiring end-to-end (see the task report for the unverified list).
package req039_test

import (
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// stopIdleFixture seeds one builder agent on TASK-039-01 with the given
// agent row overrides.
func stopIdleFixture(t *testing.T, root string, agent map[string]any) {
	t.Helper()
	state := systemPlanningState(t, root, "tasks", 21)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	base := map[string]any{
		"id": "builder-l4", "role": "builder", "state": "working",
		"task_ids": []any{"TASK-039-01"}, "team_id": "team-l4",
	}
	for k, v := range agent {
		base[k] = v
	}
	state["entities"] = map[string]any{
		"agents": []any{base},
		"tasks":  []any{map[string]any{"id": "TASK-039-01", "state": "in_progress"}},
		"bugs":   []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)
}

// L4 §16.1: teammate idles after the plan but before the Result → exit 2,
// feedback routed to the same teammate on stderr.
func TestHOOK_TeammateIdle_PostPlanPreResult_Exit2(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"plan_reported_ref": "plan_report:msg-1"})

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-l4-idle-postplan",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"builder-l4",
		"team_name":"team-l4",
		"transcript_path":"/tmp/claude/transcripts/builder-l4.jsonl"
	}`)
	if code != 2 {
		t.Fatalf("post-plan pre-result idle must exit 2: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "teammate_idle_resume_assignment") || !strings.Contains(stderr, "builder-l4") {
		t.Fatalf("stderr must route teammate_idle_resume_assignment feedback to builder-l4, got %s", stderr)
	}
	if !strings.Contains(stderr, "plan is not the deliverable") {
		t.Fatalf("post-plan idle must state the plan is not the deliverable, got %s", stderr)
	}
}

// A terminal Agent label without its PLAN_REPORT or original Result is not a
// registered delivery; the listener reminds the same owner to finish it.
func TestHOOK_TeammateIdle_ReportedLabelWithoutDeliveryReminds(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"state": "reported"})
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-l4-idle-reported",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"builder-l4",
		"team_name":"team-l4",
		"transcript_path":"/tmp/claude/transcripts/builder-l4.jsonl"
	}`)
	if code != 2 || !strings.Contains(stderr, "PLAN_REPORT") {
		t.Fatalf("reported label without registered delivery must preserve the original responsibility: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	assertRuntimeCursorUnchanged(t, "TeammateIdle reminder", before, req039fixtures.ReadState(t, root))
}

// A blocked Agent label without a registered blocker is not blocker evidence;
// the same owner still receives the missing PLAN_REPORT reminder.
func TestHOOK_TeammateIdle_BlockedLabelWithoutEvidenceReminds(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"state": "blocked"})
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-l4-idle-blocked",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"builder-l4",
		"team_name":"team-l4",
		"transcript_path":"/tmp/claude/transcripts/builder-l4.jsonl"
	}`)
	if code != 2 || !strings.Contains(stderr, "PLAN_REPORT") {
		t.Fatalf("blocked label without registered blocker must preserve the original responsibility: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	assertRuntimeCursorUnchanged(t, "TeammateIdle reminder", before, req039fixtures.ReadState(t, root))
}

// Fail-open contract: a TeammateIdle payload naming a teammate the runtime
// does not know must not fabricate a binding — allow and stay silent on the
// control channel.
func TestHOOK_TeammateIdle_UnidentifiedTeammate_FailOpen(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{})

	code, _, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-l4-idle-unknown",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"ghost-teammate",
		"team_name":"team-l4",
		"transcript_path":"/tmp/claude/transcripts/ghost.jsonl"
	}`)
	if code != 0 {
		t.Fatalf("unidentified teammate must fail open: code=%d stderr=%s", code, stderr)
	}
}

// L4 §16.1: a Sub-agent stopping with the plan as its final response is
// blocked — exit 2, stderr feedback names the missing Result.
func TestHOOK_SubagentStop_PlanAsFinalResponse_Exit2(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"plan_reported_ref": "plan_report:msg-9"})

	code, stdout, stderr := runHook(t, root, "SubagentStop", `{
		"session_id":"session-l4-stop-plan",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-l4",
		"agent_transcript_path":"/tmp/claude/transcripts/builder-l4.jsonl",
		"last_assistant_message":"PLAN_REPORT: I will implement TASK-039-01 in three steps ...",
		"stop_hook_active":false
	}`)
	if code != 2 {
		t.Fatalf("plan-as-final-response stop must exit 2: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "subagent_stop_missing_result") || !strings.Contains(stderr, "builder-l4") {
		t.Fatalf("stderr must carry subagent_stop_missing_result for builder-l4, got %s", stderr)
	}
	if !strings.Contains(stderr, "not the Result") {
		t.Fatalf("plan-as-final-response stop must state the plan is not the Result, got %s", stderr)
	}
}

// Official loop guard: stop_hook_active=true means the agent is already
// continuing because of a previous stop reminder; do not repeat it.
func TestHOOK_SubagentStop_StopHookActiveDoesNotRepeatReminder(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"plan_reported_ref": "plan_report:msg-9"})

	code, _, stderr := runHook(t, root, "SubagentStop", `{
		"session_id":"session-l4-stop-active",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-l4",
		"agent_transcript_path":"/tmp/claude/transcripts/builder-l4.jsonl",
		"last_assistant_message":"continuing after stop hook feedback",
		"stop_hook_active":true
	}`)
	if code != 0 {
		t.Fatalf("stop_hook_active must not repeat the stop reminder: code=%d stderr=%s", code, stderr)
	}
}

// A terminal Agent label without the original Result cannot satisfy Stop.
func TestHOOK_SubagentStop_ReportedLabelWithoutDeliveryReminds(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"state": "reported"})
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "SubagentStop", `{
		"session_id":"session-l4-stop-done",
		"hook_event_name":"SubagentStop",
		"agent_id":"builder-l4",
		"agent_transcript_path":"/tmp/claude/transcripts/builder-l4.jsonl",
		"last_assistant_message":"TASK-039-01 complete; result registered.",
		"stop_hook_active":false
	}`)
	if code != 2 || !strings.Contains(stderr, "PLAN_REPORT") {
		t.Fatalf("reported label without registered delivery must preserve the original responsibility: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	assertRuntimeCursorUnchanged(t, "SubagentStop reminder", before, req039fixtures.ReadState(t, root))
}

// PreToolUse observes a TaskUpdate event but does not authorize individual
// tools or claims.
func TestHOOK_PreToolUseTaskUpdate_UnauthorizedSelfClaimIsObserved(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"plan_reported_ref": "plan_report:msg-1"})
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "PreToolUse", `{
		"session_id":"session-l4-claim",
		"hook_event_name":"PreToolUse",
		"agent_id":"builder-l4",
		"tool_name":"TaskUpdate",
		"tool_input":{"taskId":"TASK-039-99","status":"in_progress"}
	}`)
	after := req039fixtures.ReadState(t, root)
	assertPreToolUseObservedWithoutDenyOrAdvance(t, code, stdout, stderr, before, after)
}

// The same guard fires when the teammate assigns the task to itself via the
// owner field instead of flipping status.
func TestHOOK_PreToolUseTaskUpdate_OwnerSelfAssignmentIsObserved(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"plan_reported_ref": "plan_report:msg-1"})
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "PreToolUse", `{
		"session_id":"session-l4-claim-owner",
		"hook_event_name":"PreToolUse",
		"agent_id":"builder-l4",
		"tool_name":"TaskUpdate",
		"tool_input":{"taskId":"TASK-039-99","owner":"builder-l4"}
	}`)
	after := req039fixtures.ReadState(t, root)
	assertPreToolUseObservedWithoutDenyOrAdvance(t, code, stdout, stderr, before, after)
}

// Owned TaskUpdate calls reach the graph listener without a per-tool
// authorization result; the event itself does not advance Runtime.
func TestHOOK_PreToolUseTaskUpdate_OwnedTaskUpdateIsObserved(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{"plan_reported_ref": "plan_report:msg-1"})

	for _, toolInput := range []string{
		`{"taskId":"TASK-039-01","status":"in_progress"}`,
		`{"taskId":"TASK-039-01","status":"completed"}`,
	} {
		before := req039fixtures.ReadState(t, root)
		code, stdout, stderr := runHook(t, root, "PreToolUse", `{
			"session_id":"session-l4-owned",
			"hook_event_name":"PreToolUse",
			"agent_id":"builder-l4",
			"tool_name":"TaskUpdate",
			"tool_input":`+toolInput+`
		}`)
		after := req039fixtures.ReadState(t, root)
		assertPreToolUseObservedWithoutDenyOrAdvance(t, code, stdout, stderr, before, after)
	}
}

// The main session TaskUpdate event is also observed without a per-tool
// authorization result.
func TestHOOK_PreToolUseTaskUpdate_MainSessionIsObserved(t *testing.T) {
	root := freshRoot(t)
	stopIdleFixture(t, root, map[string]any{})
	before := req039fixtures.ReadState(t, root)

	code, stdout, stderr := runHook(t, root, "PreToolUse", `{
		"session_id":"session-l4-main",
		"hook_event_name":"PreToolUse",
		"tool_name":"TaskUpdate",
		"tool_input":{"taskId":"TASK-039-99","status":"in_progress"}
	}`)
	after := req039fixtures.ReadState(t, root)
	assertPreToolUseObservedWithoutDenyOrAdvance(t, code, stdout, stderr, before, after)
}

// stopIdleNonVerificationFixture seeds one builder agent on TASK-039-01
// under the bug_resolution.investigation lifecycle (S8) with the supplied
// agent row overrides. The Controller cycle on this cursor returns
// StatusSatisfied + allow — i.e. it does NOT carry the stop/idle gate, so
// the Hook transport's evaluate() must invoke StopIdleDecision on its own
// to keep the platform from letting the agent go idle before its
// PLAN_REPORT.
func stopIdleNonVerificationFixture(t *testing.T, root, lifecycleState, lifecyclePhase string, agent map[string]any) {
	t.Helper()
	state := systemPlanningState(t, root, "tasks", 21)
	state["lifecycle"] = map[string]any{
		"state":          lifecycleState,
		"phase":          lifecyclePhase,
		"phase_revision": 0,
	}
	base := map[string]any{
		"id": "reviewer-s8", "role": "reviewer", "state": "reading",
		"task_ids": []any{"TASK-039-01"}, "team_id": "team-s8",
		"dispatch_mode": "plan_checkpoint",
	}
	for k, v := range agent {
		base[k] = v
	}
	state["entities"] = map[string]any{
		"agents": []any{base},
		"tasks":  []any{map[string]any{"id": "TASK-039-01", "state": "in_progress"}},
		"bugs":   []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)
}

// Regression (S8/S10/S11 stop/idle gate): on the bug_resolution.investigation
// lifecycle the Controller cycle folds the verdict to allow (no auto-candidate
// at that cursor); StopIdleDecision is the only authority that can keep the
// reviewer teammate on the platform. A teammate idling before its
// PLAN_REPORT checkpoint MUST exit 2 with the recovery feedback naming
// PLAN_REPORT.
func TestHOOK_TeammateIdle_NonVerificationLifecycle_ReadingBeforePlan_Exit2(t *testing.T) {
	root := freshRoot(t)
	stopIdleNonVerificationFixture(t, root, "bug_resolution", "investigation", map[string]any{})

	code, stdout, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-s8-idle-preplan",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"reviewer-s8",
		"team_name":"team-s8",
		"transcript_path":"/tmp/claude/transcripts/reviewer-s8.jsonl"
	}`)
	if code != 2 {
		t.Fatalf("non-verification (bug_resolution.investigation) idle must exit 2 even though the controller cycle allowed it: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "teammate_idle_resume_assignment") || !strings.Contains(stderr, "reviewer-s8") {
		t.Fatalf("stderr must route teammate_idle_resume_assignment feedback to reviewer-s8, got %s", stderr)
	}
	if !strings.Contains(stderr, "PLAN_REPORT") {
		t.Fatalf("stderr must mention PLAN_REPORT (the missing checkpoint), got %s", stderr)
	}
}

// Regression (S8/S10/S11 stop/idle gate): on the bug_resolution.investigation
// lifecycle a Sub-agent stopping before any PLAN_REPORT MUST exit 2 with
// the recovery feedback naming the missing checkpoint. Same root cause as
// the TeammateIdle regression above — the controller cycle folds the verdict
// to allow on this cursor, so StopIdleDecision has to take over.
func TestHOOK_SubagentStop_NonVerificationLifecycle_ReadingBeforePlan_Exit2(t *testing.T) {
	root := freshRoot(t)
	stopIdleNonVerificationFixture(t, root, "bug_resolution", "investigation", map[string]any{})

	code, stdout, stderr := runHook(t, root, "SubagentStop", `{
		"session_id":"session-s8-stop-preplan",
		"hook_event_name":"SubagentStop",
		"agent_id":"reviewer-s8",
		"agent_transcript_path":"/tmp/claude/transcripts/reviewer-s8.jsonl",
		"last_assistant_message":"",
		"stop_hook_active":false
	}`)
	if code != 2 {
		t.Fatalf("non-verification (bug_resolution.investigation) stop must exit 2 even though the controller cycle allowed it: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "subagent_stop_missing_result") || !strings.Contains(stderr, "reviewer-s8") {
		t.Fatalf("stderr must carry subagent_stop_missing_result for reviewer-s8, got %s", stderr)
	}
	if !strings.Contains(stderr, "PLAN_REPORT") {
		t.Fatalf("stderr must mention PLAN_REPORT (the missing checkpoint), got %s", stderr)
	}
}

// Fail-open contract (regression preserved): a SubagentStop firing under a
// non-verification lifecycle for a one_shot dispatch_mode agent must NOT
// be blocked by StopIdleDecision. one_shot is the official fail-open branch
// (L4 §16.1: the final message IS the result).
func TestHOOK_SubagentStop_NonVerificationLifecycle_OneShot_FailsOpen(t *testing.T) {
	root := freshRoot(t)
	stopIdleNonVerificationFixture(t, root, "bug_resolution", "investigation", map[string]any{
		"dispatch_mode": "one_shot",
	})

	code, _, stderr := runHook(t, root, "SubagentStop", `{
		"session_id":"session-s8-stop-oneshot",
		"hook_event_name":"SubagentStop",
		"agent_id":"reviewer-s8",
		"agent_transcript_path":"/tmp/claude/transcripts/reviewer-s8.jsonl",
		"last_assistant_message":"final result",
		"stop_hook_active":false
	}`)
	if code != 0 {
		t.Fatalf("one_shot dispatch_mode must remain fail-open on non-verification lifecycles: code=%d stderr=%s", code, stderr)
	}
}

// Fail-open contract (regression preserved): a TeammateIdle firing under a
// non-verification lifecycle for a plan_approval_required dispatch_mode
// agent in `reading` state must NOT be blocked. plan_approval_required is
// out of scope for the PLAN_REPORT checkpoint gate (L4 §3.3: only
// plan_checkpoint agents must send PLAN_REPORT).
func TestHOOK_TeammateIdle_NonVerificationLifecycle_PlanApprovalRequired_FailsOpen(t *testing.T) {
	root := freshRoot(t)
	stopIdleNonVerificationFixture(t, root, "bug_resolution", "investigation", map[string]any{
		"dispatch_mode": "plan_approval_required",
	})

	code, _, stderr := runHook(t, root, "TeammateIdle", `{
		"session_id":"session-s8-idle-par",
		"hook_event_name":"TeammateIdle",
		"teammate_name":"reviewer-s8",
		"team_name":"team-s8",
		"transcript_path":"/tmp/claude/transcripts/reviewer-s8.jsonl"
	}`)
	if code != 0 {
		t.Fatalf("plan_approval_required dispatch_mode must remain fail-open on non-verification lifecycles: code=%d stderr=%s", code, stderr)
	}
}
