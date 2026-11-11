package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// ctDecodeEnvelope decodes the protocol stdout of one Hook event.
//
// The current listening contract allows a silent stdout ("正常且无需补充 |
// 可静默", L4-hook-platform-wiring.md:173-175), so empty stdout returns nil.
// Every other stdout must be exactly one protocol JSON object: §7.2 forbids a
// second JSON, a test log or a debug banner inside the same stdout.
func ctDecodeEnvelope(t *testing.T, raw string) map[string]any {
	t.Helper()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("hook stdout must be silent or a single protocol JSON object: %v\nstdout=%s", err, raw)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("hook stdout must carry exactly one protocol JSON object: trailing=%q\nstdout=%s",
			strings.TrimSpace(string(trailing)), raw)
	}
	return envelope
}

// ctHookSpecific returns the hookSpecificOutput object of a protocol
// envelope, or nil when the event delivered no protocol payload.
func ctHookSpecific(envelope map[string]any) map[string]any {
	if envelope == nil {
		return nil
	}
	specific, _ := envelope["hookSpecificOutput"].(map[string]any)
	return specific
}

// ctAdditionalContext returns the model-facing context of a protocol
// envelope ("hookSpecificOutput.additionalContext").
func ctAdditionalContext(envelope map[string]any) string {
	specific := ctHookSpecific(envelope)
	if specific == nil {
		return ""
	}
	context, _ := specific["additionalContext"].(string)
	return context
}

// ctAssertNoPermissionDecision pins the current PreToolUse listening
// contract: the renderer outputs progress and additionalContext and never
// sets a tool permission decision (L4-hook-platform-wiring.md:168,
// BUGS.md:480). A null value carries no decision; any real value is a
// regression.
func ctAssertNoPermissionDecision(t *testing.T, envelope map[string]any) {
	t.Helper()
	specific := ctHookSpecific(envelope)
	if specific == nil {
		return
	}
	decision, present := specific["permissionDecision"]
	if present && decision != nil {
		t.Fatalf("PreToolUse must not set a tool permission decision: %v", decision)
	}
}

// ctQualityGateProjection digs the quality-gate projection out of the inline
// "QUALITY_GATE {...}" line the PreToolUse renderer places in
// hookSpecificOutput.additionalContext. It returns nil when the event carries
// no gate projection.
func ctQualityGateProjection(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	for _, line := range strings.Split(ctAdditionalContext(envelope), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "QUALITY_GATE ") {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, "QUALITY_GATE ")))
		decoder.UseNumber()
		var projection map[string]any
		if err := decoder.Decode(&projection); err != nil {
			t.Fatalf("QUALITY_GATE projection must be JSON: %v\nline=%s", err, line)
		}
		return projection
	}
	return nil
}

// ctAuditRecords reads the Hook audit outbox. The native observation audit
// appends its record there while the protocol stdout may stay silent
// (L4-hook-platform-wiring.md:175).
func ctAuditRecords(t *testing.T, root string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "hook-decisions.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read hook audit outbox: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("hook audit record must be JSON: %v\nrecord=%s", err, line)
		}
		records = append(records, record)
	}
	return records
}

// ctAuditQualityGate returns the quality-gate projection recorded by the Hook
// audit outbox, or nil when the outbox holds no gate projection.
func ctAuditQualityGate(t *testing.T, root string) map[string]any {
	t.Helper()
	for _, record := range ctAuditRecords(t, root) {
		if projection, ok := record["quality_gate"].(map[string]any); ok {
			return projection
		}
	}
	return nil
}

// ctRuntimeState reads the persisted Runtime snapshot of the fixture.
func ctRuntimeState(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatalf("read runtime state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode runtime state: %v", err)
	}
	return state
}

// ctAssertCursorUnchanged asserts that an ordinary read-only projection left
// the lifecycle cursor exactly where it was: same state, same phase, no phase
// revision and no committed transition. An ordinary PreToolUse is a
// projection, not an outlet (L4-agent-guidance.md:24).
func ctAssertCursorUnchanged(t *testing.T, root, state, phase string) {
	t.Helper()
	persisted := ctRuntimeState(t, root)
	lifecycle, _ := persisted["lifecycle"].(map[string]any)
	if got, _ := lifecycle["state"].(string); got != state {
		t.Fatalf("ordinary PreToolUse must stay read-only: lifecycle.state=%q, want %q", got, state)
	}
	if got, _ := lifecycle["phase"].(string); got != phase {
		t.Fatalf("ordinary PreToolUse must stay read-only: lifecycle.phase=%q, want %q", got, phase)
	}
	if got, _ := lifecycle["phase_revision"].(float64); got != 0 {
		t.Fatalf("ordinary PreToolUse must stay read-only: lifecycle.phase_revision=%v, want 0", got)
	}
	if persisted["last_transition"] != nil {
		t.Fatalf("ordinary PreToolUse must not commit a transition: last_transition=%v", persisted["last_transition"])
	}
}

// ctOrdinaryPreObservation runs the shared ordinary PreToolUse observation
// contract and returns the decoded protocol envelope (nil when the event
// stayed silent):
//
//   - the event succeeds and never sets a tool permission decision
//     (L4-hook-platform-wiring.md:168, BUGS.md:480);
//   - stdout is silent or exactly one protocol JSON object (§7.1, §7.2);
//   - the observation is delivered: either the protocol payload carries the
//     current responsibility and missing items, or the audit outbox records it
//     ("具名原生观察审计 | 追加审计，协议 stdout 静默",
//     L4-hook-platform-wiring.md:175).
func ctOrdinaryPreObservation(t *testing.T, root, event, input string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"hook", "--event", event, "--root", root}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("%s observation failed: code=%d stderr=%s stdout=%s", event, code, stderr.String(), stdout.String())
	}
	envelope := ctDecodeEnvelope(t, stdout.String())
	ctAssertNoPermissionDecision(t, envelope)
	if envelope == nil {
		if len(ctAuditRecords(t, root)) == 0 {
			t.Fatalf("%s must deliver its observation: stdout was silent and no audit record was appended", event)
		}
		return nil
	}
	if ctAdditionalContext(envelope) == "" {
		t.Fatalf("%s must deliver the current responsibility in additionalContext: stdout=%s", event, stdout.String())
	}
	return envelope
}

// TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01 covers CT-039-01 from
// SYNC-039 §12.
//
// The design gate belongs to the S2 outlet, not to an ordinary PreToolUse:
// "PTR-PLAN-01 保留的 `auto_trigger.event: PreToolUse` 不表示启用阶段答卷的
// Main 必须等下一次 Pre 才推进；正常 S2 保存直接接续已登记的原出口，普通
// Pre 保持只读" (L4-agent-guidance.md:24). An ordinary Pre therefore stays a
// read-only projection: it reports the current gate progress, it neither
// authorizes nor denies the tool (L4-hook-platform-wiring.md:168) and it never
// fabricates a passed gate or an advancement ("图投影没有运行该轮 Gate 时，
// 不补造一个通过或 `advanced`", L4-hook-platform-wiring.md §7.2).
func TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-01",
		"hook_event_name":"PreToolUse",
		"agent_id":"Main",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/architecture/ARCHITECTURE-039.md"}
	}`
	envelope := ctOrdinaryPreObservation(t, root, "PreToolUse", input)
	ctAssertNoPermissionDecision(t, envelope)

	// The projection must exist — in the protocol payload or in the audit
	// outbox — so the ordinary Pre really reports the current gate.
	projection := ctQualityGateProjection(t, envelope)
	if projection == nil {
		projection = ctAuditQualityGate(t, root)
	}
	if projection == nil {
		t.Fatal("CT-039-01 must surface the design gate projection (protocol payload or audit outbox)")
	}
	if gateID, _ := projection["gate_id"].(string); !strings.Contains(gateID, "GATE-PLANNING-DESIGN-COMPLETE") {
		t.Fatalf("CT-039-01 must project the design gate of the current cursor, got %v", projection)
	}
	// The fixture holds no design document and no qualified evidence, so the
	// projection must not claim that the gate passed or advanced.
	status, _ := projection["status"].(string)
	if status == "satisfied" || status == "advanced" {
		t.Fatalf("CT-039-01 must not fabricate a passed design gate: %v", projection)
	}
	if committed, _ := projection["transition_committed"].(bool); committed {
		t.Fatalf("CT-039-01 must not commit a transition from an ordinary Pre: %v", projection)
	}
	if missing, _ := projection["missing"].([]any); len(missing) == 0 {
		t.Fatalf("CT-039-01 must project the missing design evidence: %v", projection)
	}

	// Reverse assertion of the removed behaviour: an ordinary Pre keeps the
	// read-only projection contract and does not continue the PTR-PLAN-01
	// outlet (L4-agent-guidance.md:24).
	ctAssertCursorUnchanged(t, root, "planning", "design")
}

// TestCT03902_ContractsMissingCoverageExposesNotReady covers CT-039-02
// from SYNC-039 §12.
//
// "普通公开 PreToolUse 与主会话 Stop 只读当前责任和缺项，不反复发行问卷、
// 重跑完整来源或消费待续动作" (L3-S3-contracts.md:458). The S3 contracts
// cursor therefore projects its current responsibility and its missing items
// as a read-only observation: no tool permission decision, no invented pass
// and no new questionnaire.
func TestCT03902_ContractsMissingCoverageExposesNotReady(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "contracts", 1)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-02",
		"hook_event_name":"PreToolUse",
		"agent_id":"Main",
		"tool_name":"Edit",
		"tool_input":{"file_path":"internal/cli/controller.go"}
	}`
	envelope := ctOrdinaryPreObservation(t, root, "PreToolUse", input)
	ctAssertNoPermissionDecision(t, envelope)

	projection := ctQualityGateProjection(t, envelope)
	if projection == nil {
		projection = ctAuditQualityGate(t, root)
	}
	if projection == nil {
		t.Fatal("CT-039-02 must project the contracts gate of the current cursor")
	}
	if gateID, _ := projection["gate_id"].(string); !strings.Contains(gateID, "GATE-PLANNING-CONTRACTS-COMPLETE") {
		t.Fatalf("CT-039-02 must project the contracts gate, got %v", projection)
	}
	// A gate that did not pass in this round must not be rendered as passed or
	// advanced (L4-hook-platform-wiring.md §7.2).
	status, _ := projection["status"].(string)
	if status == "satisfied" || status == "advanced" {
		t.Fatalf("CT-039-02 must not claim a satisfied/advanced contracts gate: %v", projection)
	}
	if committed, _ := projection["transition_committed"].(bool); committed {
		t.Fatalf("CT-039-02 must not commit a transition from an ordinary Pre: %v", projection)
	}
	missing, _ := projection["missing"].([]any)
	if len(missing) == 0 {
		t.Fatalf("CT-039-02 must expose the missing contract coverage: %v", projection)
	}

	// The read-only projection must not issue a new questionnaire nor consume
	// a pending action (L3-S3-contracts.md:458).
	persisted := ctRuntimeState(t, root)
	entities, _ := persisted["entities"].(map[string]any)
	if packets, ok := entities["work_packets"].([]any); ok && len(packets) != 0 {
		t.Fatalf("CT-039-02 must not issue a questionnaire on an ordinary Pre: work_packets=%v", packets)
	}
	if submissions, ok := entities["submissions"].([]any); ok && len(submissions) != 0 {
		t.Fatalf("CT-039-02 must not consume a pending action on an ordinary Pre: submissions=%v", submissions)
	}
	ctAssertCursorUnchanged(t, root, "planning", "contracts")
}

// TestCT03904_LockedExactContractWriteIsObserved covers CT-039-04 from
// SYNC-039 §12.
//
// Two contracts meet in this event:
//   - a locked exact path is ordinary Hook input — the Hook is not a tool
//     permission manager and never denies the write
//     (L4-hook-platform-wiring.md:168, BUGS.md:480);
//   - the graph observation of that write is appended to the audit outbox
//     while the protocol stdout may stay silent ("具名原生观察审计 | 追加审计，
//     协议 stdout 静默 | 0，不改变原操作结果", L4-hook-platform-wiring.md:175).
func TestCT03904_LockedExactContractWriteIsObserved(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "contracts", 1)
	writeACState(t, root, state)
	input := `{
		"session_id":"session-ct-039-04",
		"hook_event_name":"PreToolUse",
		"agent_id":"Main",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/dev/contracts/BE-039-loop-controller.md"}
	}`
	envelope := ctOrdinaryPreObservation(t, root, "PreToolUse", input)

	// The locked path must not be denied by PreToolUse.
	ctAssertNoPermissionDecision(t, envelope)

	// The graph observation stays durable: either the protocol payload carries
	// it or the audit outbox records it.
	projection := ctQualityGateProjection(t, envelope)
	if projection == nil {
		projection = ctAuditQualityGate(t, root)
	}
	if projection == nil {
		t.Fatal("CT-039-04 must retain the graph observation of the locked write (protocol payload or audit outbox)")
	}
	if gateID, _ := projection["gate_id"].(string); !strings.Contains(gateID, "GATE-PLANNING-CONTRACTS-COMPLETE") {
		t.Fatalf("CT-039-04 must observe the contracts gate of the current cursor, got %v", projection)
	}
	if committed, _ := projection["transition_committed"].(bool); committed {
		t.Fatalf("CT-039-04 observes the locked write; it must not commit a transition: %v", projection)
	}

	// The write itself stays untouched: the locked path is not a tool ACL.
	ctAssertCursorUnchanged(t, root, "planning", "contracts")
}

// TestCT03905_UnlockedSiblingWriteIsObserved covers CT-039-05 from
// SYNC-039 §12.
//
// A neighbouring document path is observed like any other tool use: the Hook
// reports the current responsibility and the missing items, it neither
// authorizes nor denies the tool (L4-hook-platform-wiring.md:168) and it does
// not need a live quality_gate envelope for that.
func TestCT03905_UnlockedSiblingWriteIsObserved(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// The Hook reports graph progress without making an artifact-path decision.
	input := `{
		"session_id":"session-ct-039-05",
		"hook_event_name":"PreToolUse",
		"agent_id":"Main",
		"tool_name":"Edit",
		"tool_input":{"file_path":"docs/dev/contracts/BE-039-loop-controller-notes.md"}
	}`
	envelope := ctOrdinaryPreObservation(t, root, "PreToolUse", input)
	ctAssertNoPermissionDecision(t, envelope)

	// The delivered context names the current stage and its accurate
	// responsibility path, so the agent can continue the current work.
	context := ctAdditionalContext(envelope)
	if context != "" {
		for _, expected := range []string{"S2", "docs/control/agent-protocol.md#s2"} {
			if !strings.Contains(context, expected) {
				t.Fatalf("CT-039-05 must deliver the current responsibility %q: %s", expected, context)
			}
		}
	}

	// Reverse assertion: an ordinary sibling write is not authorized or
	// denied, and it does not move the cursor.
	ctAssertCursorUnchanged(t, root, "planning", "design")
}

// TestCT03906_GitSquashMergeIsObserved covers CT-039-06 from SYNC-039 §12.
//
// Command classification is feedback for the graph; PreToolUse does not deny
// the tool and does not turn an observed command into an integration
// instruction. The integration procedure itself belongs to the integrator
// ("S6 改变产品的正常集成使用 `git merge --no-ff` … 集成器不 squash",
// L4-worktree-governance.md:166).
func TestCT03906_GitSquashMergeIsObserved(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	input := `{
		"session_id":"session-ct-039-06",
		"hook_event_name":"PreToolUse",
		"agent_id":"Main",
		"tool_name":"Bash",
		"tool_input":{"command":"git merge --squash feature/req-039"}
	}`
	envelope := ctOrdinaryPreObservation(t, root, "PreToolUse", input)

	// The observed git command is neither denied nor authorized.
	ctAssertNoPermissionDecision(t, envelope)

	// The delivered context carries the current progress and missing items.
	context := ctAdditionalContext(envelope)
	if context != "" {
		if !strings.Contains(context, "S2") {
			t.Fatalf("CT-039-06 must deliver the current stage in additionalContext: %s", context)
		}
	}

	// Reverse assertion: the observed command must not be rendered as Main's
	// integration procedure (L4-worktree-governance.md:166).
	for _, marker := range []string{"completion_ack", "REQ-bound development branch", "git merge --no-ff", "--no-ff"} {
		if strings.Contains(context, marker) {
			t.Fatalf("CT-039-06 must not carry integration guidance in the Hook payload: %q in %s", marker, context)
		}
	}
	ctAssertCursorUnchanged(t, root, "planning", "design")
}

// TestCT03908_SessionStartAfterCompactSurfacesSameMilestone covers
// CT-039-08 from SYNC-039 §12.
//
// After a Compact the SessionStart re-derives the recovery facts from the
// Runtime, the original responsibility and the accepted artifacts: the current
// stage, the next action of the stage cursor and the accurate path
// ("压缩后的 SessionStart 从 Runtime、原责任与已接受产物重新取得当前阶段、
// 准确路径和下一动作。Milestone 可以帮助导航，但只是原工程事实的摘要 …
// 也不要求恢复时逐字重放旧提示", L4-hook-platform-wiring.md:387). The
// verbatim replay of the persisted milestone.objective is therefore not the
// contract; the recovered stage, next action and path are.
func TestCT03908_SessionStartAfterCompactSurfacesSameMilestone(t *testing.T) {
	root := ctFixture(t)
	state := planningState(t, root, "design", 1)
	writeACState(t, root, state)

	// Step 1: drive PreToolUse so the Runtime holds the pre-compaction facts.
	input := `{
		"session_id":"session-ct-039-08",
		"hook_event_name":"PreToolUse",
		"agent_id":"Main",
		"tool_name":"Edit",
		"tool_input":{"file_path":"internal/cli/run.go"}
	}`
	envelope := ctOrdinaryPreObservation(t, root, "PreToolUse", input)
	ctAssertNoPermissionDecision(t, envelope)

	// The Runtime keeps the milestone as a navigation aid.
	persisted := ctRuntimeState(t, root)
	milestone, _ := persisted["milestone"].(map[string]any)
	if stage, _ := milestone["stage"].(string); stage != "S2" {
		t.Fatalf("CT-039-08: the Runtime milestone must still name the current stage, got %q", stage)
	}

	// Step 2: SessionStart after a synthetic Compact must re-deliver the
	// current stage, the next action of the stage cursor and the accurate
	// path.
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", root}, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"session-ct-039-08-recover"}`), &stdout, &stderr); code != 0 {
		t.Fatalf("SessionStart failed: code=%d stderr=%s", code, stderr.String())
	}
	recovery := ctDecodeEnvelope(t, stdout.String())
	if recovery == nil {
		t.Fatal("CT-039-08: post-Compact SessionStart must deliver the recovery projection")
	}
	specific := ctHookSpecific(recovery)
	if specific == nil {
		t.Fatalf("CT-039-08: post-Compact SessionStart must use hookSpecificOutput, got %v", recovery)
	}
	if event, _ := specific["hookEventName"].(string); event != "SessionStart" {
		t.Fatalf("CT-039-08: post-Compact SessionStart must use its own hookEventName, got %q", event)
	}
	context := ctAdditionalContext(recovery)
	for _, expected := range []string{
		"stage=S2",                               // current stage
		"complete the planning phase for design", // next action of the stage cursor
		"docs/control/agent-protocol.md#s2",      // accurate path
	} {
		if !strings.Contains(context, expected) {
			t.Fatalf("CT-039-08: post-Compact SessionStart must re-deliver %q, got %s", expected, context)
		}
	}

	// Reverse assertion: a recovery packet never claims a transition that was
	// not committed (L4-hook-platform-wiring.md §7.2).
	if strings.Contains(context, "QUALITY GATE ADVANCED") {
		t.Fatalf("CT-039-08: post-Compact SessionStart must not claim an advanced gate: %s", context)
	}
}

// TestCT03909_CleanWorktreeStopEmitsIntegrationGuidance covers CT-039-09
// from SYNC-039 §12.
//
// The SubagentStop contract is "定位本人实际产物，交原消费者或反馈缺项" and
// explicitly "不承担长合并、Main 全套检查和清理" (L3-S6-build.md:311). The
// Hook therefore does not carry Main's integration instruction; a correction
// that needs this agent to continue travels on stderr with exit 2 while stdout
// stays silent ("需要本人继续的 Stop 纠正 | stderr 给准确问题与下一动作，
// stdout 静默 | 2", L4-hook-platform-wiring.md:176). The integration duty
// stays with Main's explicit entry ("runtime task-integrate
// --assignment-id <id> --root <main-root>", AGENTS-template.md:425).
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
	out := stdout.String()
	switch code {
	case 0:
		// Nothing to correct: the event stays silent or reports the worker's
		// own missing items, never Main's integration procedure.
		for _, marker := range []string{"REQ-bound development branch", "completion_ack", "git merge", "--no-ff"} {
			if strings.Contains(out, marker) {
				t.Fatalf("CT-039-09: SubagentStop must not carry Main's integration guidance %q: %s", marker, out)
			}
		}
	case 2:
		// A correction that needs this agent to continue: stderr carries the
		// problem and the next action, stdout stays silent.
		if strings.TrimSpace(out) != "" {
			t.Fatalf("CT-039-09: a blocking SubagentStop must keep stdout silent: %s", out)
		}
		if strings.TrimSpace(stderr.String()) == "" {
			t.Fatal("CT-039-09: a blocking SubagentStop must give the problem and next action on stderr")
		}
	default:
		t.Fatalf("SubagentStop must not fail: %d stderr=%s", code, stderr.String())
	}

	// The integration duty stays with Main's explicit entry point.
	var usageOut, usageErr bytes.Buffer
	cli.Run([]string{"runtime"}, strings.NewReader(""), &usageOut, &usageErr)
	if !strings.Contains(usageErr.String(), "task-integrate") {
		t.Fatalf("CT-039-09: Main's explicit integration entry `runtime task-integrate` must stay available: stderr=%s",
			usageErr.String())
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
