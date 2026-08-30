// auto_chain_test.go — pins the L4 §3.3 plan_checkpoint auto-activation chain
// (PreStageActivationEnvelope + AutoAdvanceToWorking + AgentBegin).
package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// planCheckpointWorkgroupState builds a minimal runtime + a copy of the
// canonical testdata/delivery-manifest.json (with dispatch_mode patched to
// plan_checkpoint on every assignment) so register-workgroup's pre-stage
// step can run for a plan_checkpoint agent. The fixture is self-contained
// (uses t.TempDir() for the runtime state; the manifest lives under the
// repo-rooted .claude/workgroups/<req>/<task>/ path register-workgroup
// expects).
func planCheckpointWorkgroupState(t *testing.T, root, reqID, workgroupID, taskID, agentID string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")

	state := activeState(t, root, "building", nil, 5)
	writeJSON(t, statePath, state)
	if err := os.WriteFile(journalPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestDir := filepath.Join(root, ".claude", "workgroups", reqID, taskID)
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join("testdata", "delivery-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	// Patch: change the workgroup_kind so register-workgroup accepts it
	// for the building state, and stamp plan_checkpoint on every
	// assignment. The fixture reuses the delivery-manifest's rich
	// schema-valid shape (responsibility_ids, separation_edges,
	// baseline_generation, etc.) instead of building a minimal one.
	manifest["workgroup_id"] = workgroupID
	manifest["workgroup_kind"] = "builder"
	manifest["req_id"] = "REQ-002"
	manifest["manifest_id"] = "team-manifest-" + workgroupID
	for _, raw := range manifest["assignments"].([]any) {
		row := raw.(map[string]any)
		row["dispatch_mode"] = "plan_checkpoint"
	}
	out, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(manifestDir, "manifest.json")
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return statePath, journalPath, manifestPath
}

// TestPreStageActivationEnvelopeWritesActivationFile pins the
// register-workgroup side of L4 §3.3: a plan_checkpoint agent's envelope
// is written to .claude/evidence/<workgroup>/<task>/activation-<agent>.json
// with all four hookctx-loader fields populated.
func TestPreStageActivationEnvelopeWritesActivationFile(t *testing.T) {
	root := filepath.Join("..", "..")
	agentID := "agent-pre-stage-1"
	envelopePath, err := assignment.PreStageActivationEnvelope(root, "wg-pre-stage", "TASK-pre", agentID, "plan_checkpoint", assignment.ActivationSourceEntry{
		AgentID:            agentID,
		AgentDefinitionRef: "agents/backend-builder.md",
		SkillRefs:          []string{"backend-engineering", "testing-strategy"},
		WritePaths:         []string{"internal/api/", "docs/"},
		OutputPaths:        []string{"docs/reports/review/REV-001.md"},
	})
	if err != nil {
		t.Fatalf("pre-stage envelope: %v", err)
	}
	if envelopePath == "" {
		t.Fatal("plan_checkpoint agent must produce an envelope path")
	}
	data, err := os.ReadFile(filepath.Join(root, envelopePath))
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	var envelope assignment.ActivationEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.AgentID != agentID {
		t.Fatalf("envelope agent_id = %q, want %q", envelope.AgentID, agentID)
	}
	if len(envelope.AllowedTools) == 0 {
		t.Fatal("envelope allowed_tools must be populated")
	}
	if len(envelope.AllowedWritePaths) == 0 {
		t.Fatal("envelope allowed_write_paths must be populated (sourced from manifest.write_paths + output_paths)")
	}
	if len(envelope.AllowedCommandClasses) == 0 {
		t.Fatal("envelope allowed_command_classes must be populated (sourced from skill_refs map)")
	}
}

// TestPreStageActivationEnvelopeSkipsApprovalMode guards plan_approval_required:
// the pre-stage must NOT write a file (the human Gate signs the readback first,
// and a pre-staged envelope would silently widen the activation surface).
func TestPreStageActivationEnvelopeSkipsApprovalMode(t *testing.T) {
	root := filepath.Join("..", "..")
	envelopePath, err := assignment.PreStageActivationEnvelope(root, "wg-approval", "TASK-app", "agent-approval", "plan_approval_required", assignment.ActivationSourceEntry{
		AgentID: "agent-approval",
	})
	if err != nil {
		t.Fatalf("pre-stage envelope: %v", err)
	}
	if envelopePath != "" {
		t.Fatalf("plan_approval_required must NOT produce an envelope; got %q", envelopePath)
	}
}

func TestPreStageActivationEnvelopeRejectsAuthoringPlaceholderAgentID(t *testing.T) {
	_, err := assignment.PreStageActivationEnvelope(t.TempDir(), "wg-placeholder", "TASK-placeholder", "TODO(planner):agent-id-for-qa", "plan_checkpoint", assignment.ActivationSourceEntry{})
	if err == nil {
		t.Fatal("pre-stage must reject an authoring placeholder identity")
	}
	if !strings.Contains(err.Error(), "authoring placeholder") {
		t.Fatalf("pre-stage error should explain the replacement, got %v", err)
	}
}

func TestAutoAdvanceToWorkingRejectsAuthoringPlaceholderAgentID(t *testing.T) {
	outcome, err := assignment.AutoAdvanceToWorking(assignment.AutoChainInput{
		Root:        t.TempDir(),
		StatePath:   filepath.Join(t.TempDir(), "loop-state.json"),
		JournalPath: filepath.Join(t.TempDir(), "loop-events.jsonl"),
		AgentID:     "TODO(planner):agent-id-for-qa",
		PlanPath:    "plan-report.json",
	})
	if err != nil {
		t.Fatalf("invalid identity should be a non-blocking observer skip, got error: %v", err)
	}
	if outcome.Chained {
		t.Fatalf("placeholder identity must not auto-chain: %#v", outcome)
	}
	if !strings.Contains(outcome.Reason, "authoring placeholder") {
		t.Fatalf("skip reason should explain how to replace the placeholder, got %q", outcome.Reason)
	}
}

func TestAgentBeginRejectsAuthoringPlaceholderBeforeRecoveryLookup(t *testing.T) {
	_, _, err := assignment.AgentBegin(t.TempDir(), filepath.Join(t.TempDir(), "loop-state.json"), filepath.Join(t.TempDir(), "loop-events.jsonl"), assignment.AgentBeginRequest{
		AgentID:  "TODO(planner):agent-id-for-qa",
		PlanPath: "plan-report.json",
	})
	if err == nil {
		t.Fatal("agent-begin must reject an authoring placeholder before reading runtime state")
	}
	if !strings.Contains(err.Error(), "authoring placeholder") {
		t.Fatalf("agent-begin error should explain how to replace the placeholder, got %v", err)
	}
}

// TestAutoAdvanceToWorkingChainsPlanCheckpointAgent is the happy-path test
// for the PostToolUse(SendMessage) auto-chain: from a registered
// plan_checkpoint agent in reading state, one AutoAdvanceToWorking call
// advances the agent to working in three CAS-bound AdvanceAgent calls.
//
// Uses TASK-012 + delivery-manifest so the register-workgroup call goes
// through the existing schema-valid path; then drives the chain against
// the first registered agent.
func TestAutoAdvanceToWorkingChainsPlanCheckpointAgent(t *testing.T) {
	root := filepath.Join("..", "..")
	reqID := "REQ-002"
	workgroupID := "workgroup-delivery-round-1"
	taskID := "TASK-012"
	agentID := "agent-ver-req-gap" // first agent in delivery-manifest

	statePath, journalPath, manifestPath := planCheckpointWorkgroupState(t, root, reqID, workgroupID, taskID, agentID)

	taskPath := filepath.Join(filepath.Dir(manifestPath), taskID+".md")
	if err := os.WriteFile(taskPath, []byte("# "+taskID+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 5,
		ManifestPath:     manifestPath,
		TaskID:           taskID,
		TaskPath:         taskPath,
	}); err != nil {
		t.Fatalf("register workgroup: %v", err)
	}

	planPath, _ := writeAutoChainPlanReport(t, filepath.Dir(statePath), 6)

	outcome, err := assignment.AutoAdvanceToWorking(assignment.AutoChainInput{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
		AgentID:     agentID,
		PlanPath:    planPath,
	})
	if err != nil {
		t.Fatalf("auto-chain: %v", err)
	}
	if !outcome.Chained {
		t.Fatalf("auto-chain did not chain; reason=%q", outcome.Reason)
	}
	if outcome.FinalState != "working" {
		t.Fatalf("final state = %q, want working", outcome.FinalState)
	}
	if outcome.ActivationID == "" {
		t.Fatal("activation_id must be set after chaining")
	}
}

// TestAutoAdvanceToWorkingSkipsPlanApprovalRequired guards the dispatch-mode
// gate: even if the auto-chain is invoked for a plan_approval_required
// agent, it returns a silent skip rather than driving the chain.
func TestAutoAdvanceToWorkingSkipsPlanApprovalRequired(t *testing.T) {
	root := filepath.Join("..", "..")
	agentID := "agent-ver-req-gap"
	statePath, journalPath, manifestPath := planCheckpointWorkgroupState(t, root, "REQ-002", "workgroup-delivery-round-1", "TASK-012", agentID)

	// Force dispatch_mode=plan_approval_required on every assignment row.
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, raw := range manifest["assignments"].([]any) {
		row := raw.(map[string]any)
		row["dispatch_mode"] = "plan_approval_required"
	}
	out, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(filepath.Dir(manifestPath), "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 5,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	}); err != nil {
		t.Fatalf("register workgroup: %v", err)
	}
	planPath, _ := writeAutoChainPlanReport(t, filepath.Dir(statePath), 6)
	outcome, err := assignment.AutoAdvanceToWorking(assignment.AutoChainInput{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
		AgentID:     agentID,
		PlanPath:    planPath,
	})
	if err != nil {
		t.Fatalf("auto-chain: %v", err)
	}
	if outcome.Chained {
		t.Fatal("auto-chain must NOT advance plan_approval_required agents")
	}
	if !strings.Contains(outcome.Reason, "plan_checkpoint only") {
		t.Fatalf("reason must name the gate, got %q", outcome.Reason)
	}
}

// TestAgentBeginFallbackMatchesAutoChain pins that the runtime agent-begin
// recovery verb produces the same end state as the auto-chain when given
// the same inputs (it is a thin wrapper).
func TestAgentBeginFallbackMatchesAutoChain(t *testing.T) {
	root := filepath.Join("..", "..")
	agentID := "agent-ver-req-gap"
	statePath, journalPath, manifestPath := planCheckpointWorkgroupState(t, root, "REQ-002", "workgroup-delivery-round-1", "TASK-012", agentID)
	taskPath := filepath.Join(filepath.Dir(manifestPath), "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 5,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	}); err != nil {
		t.Fatalf("register workgroup: %v", err)
	}
	planPath, _ := writeAutoChainPlanReport(t, filepath.Dir(statePath), 6)
	snap, outcome, err := assignment.AgentBegin(root, statePath, journalPath, assignment.AgentBeginRequest{
		AgentID:  agentID,
		PlanPath: planPath,
	})
	if err != nil {
		t.Fatalf("agent-begin: %v", err)
	}
	if !outcome.Chained {
		t.Fatalf("agent-begin did not chain; reason=%q", outcome.Reason)
	}
	if snap.Revision < 8 {
		t.Fatalf("expected revision >= 8 after three AdvanceAgent CAS calls, got %d", snap.Revision)
	}
}

// stateAfterAutoChain re-reads the state file (the chain mutates it via
// CAS), and is a tiny helper so tests can validate post-state without
// reaching back into assignment internals.
func stateAfterAutoChain(t *testing.T, statePath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// writeAutoChainPlanReport writes a plan_report JSON whose agent_id,
// task_id, runtime_id, and team_id match the auto-chain test fixtures
// (which use the delivery-manifest's first agent: agent-ver-req-gap on
// TASK-012 under REQ-002).
func writeAutoChainPlanReport(t *testing.T, dir string, revision int) (string, string) {
	t.Helper()
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report",
		"message_id": "msg-plan-auto", "correlation_id": "corr-auto",
		"runtime_id": "loop-REQ-002", "expected_runtime_revision": revision,
		"agent_id": "agent-ver-req-gap", "agent_definition_ref": ".claude/agents/delivery-verifier.md",
		"task_id": "TASK-012", "bug_id": nil, "team_id": "workgroup-delivery-round-1",
		"occurred_at":   "2026-08-18T00:00:00Z",
		"assignment_id": "assignment-ver-req-gap", "assignment_revision": 1,
		"objective":     "Verify the locked TASK-012 requirement gap coverage",
		"planned_paths": []string{"docs/reports/review/REV-001.json"},
		"steps":         []any{map[string]any{"description": "verify REQ gap", "target": "docs/reports/review/REV-001.json"}},
		"assertion_checks": []any{map[string]any{
			"assertion": "all REQ clauses covered", "oracle": "clause map complete",
		}},
		"dependencies":   []string{},
		"risks_blockers": []string{},
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan-report.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, transition.SHA256(fileBytes)
}

// Reference schema loader (kept for parity with the production path).
var _ = schema.ReadAsset

// TestAgentBeginSynthesizesEnvelopeForLegacyAgent pins the recovery path for
// agents registered before register-workgroup pre-staging existed: no
// activation_ref on the agent row, so the auto-chain skips, but
// `runtime agent-begin` must synthesize the capability set from the
// workgroup manifest row (never inventing permissions) and chain to working.
func TestAgentBeginSynthesizesEnvelopeForLegacyAgent(t *testing.T) {
	root := filepath.Join("..", "..")
	agentID := "agent-ver-req-gap"
	statePath, journalPath, manifestPath := planCheckpointWorkgroupState(t, root, "REQ-002", "workgroup-delivery-legacy", "TASK-013", agentID)
	taskPath := filepath.Join(filepath.Dir(manifestPath), "TASK-013.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-013\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 5,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-013",
		TaskPath:         taskPath,
	}); err != nil {
		t.Fatalf("register workgroup: %v", err)
	}
	// Simulate the legacy registration: strip the pre-staged envelope from
	// the agent row so the auto-chain has nothing to read.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == agentID {
			// The schema requires the property; the legacy shape is an
			// empty value, not a missing key.
			row["activation_ref"] = ""
			found = true
		}
	}
	if !found {
		t.Fatalf("agent %s not registered", agentID)
	}
	out, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(statePath, out, 0o644); err != nil {
		t.Fatal(err)
	}

	planPath, _ := writeAutoChainPlanReport(t, filepath.Dir(statePath), 6)
	// The shared fixture pins TASK-012/workgroup-delivery-round-1; rebind the
	// plan to this test's task/team.
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var planMsg map[string]any
	if err := json.Unmarshal(planData, &planMsg); err != nil {
		t.Fatal(err)
	}
	planMsg["task_id"] = "TASK-013"
	planMsg["team_id"] = "workgroup-delivery-legacy"
	patched, _ := json.Marshal(planMsg)
	if err := os.WriteFile(planPath, patched, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, outcome, err := assignment.AgentBegin(root, statePath, journalPath, assignment.AgentBeginRequest{
		AgentID:  agentID,
		PlanPath: planPath,
	})
	if err != nil {
		t.Fatalf("agent-begin on legacy agent: %v", err)
	}
	if !outcome.Chained || outcome.FinalState != "working" {
		t.Fatalf("expected chained working state, got %+v", outcome)
	}
	_ = snap
	final := stateAfterAutoChain(t, statePath)
	for _, raw := range final["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == agentID && row["state"] != "working" {
			t.Fatalf("legacy agent state = %v, want working", row["state"])
		}
	}
}

// TestAgentBeginResumesMidChainAfterFailure pins the recovery contract for a
// chain that advanced to understanding_submitted and then failed (2026-08-23
// E2E finding: the asset lookup bug stranded agents there, and the
// idempotency short-circuit made the state unrecoverable). The chain must
// resume at activation_sent, not declare the agent idempotent.
func TestAgentBeginResumesMidChainAfterFailure(t *testing.T) {
	root := filepath.Join("..", "..")
	agentID := "agent-ver-req-gap"
	statePath, journalPath, manifestPath := planCheckpointWorkgroupState(t, root, "REQ-002", "workgroup-delivery-resume", "TASK-014", agentID)
	taskPath := filepath.Join(filepath.Dir(manifestPath), "TASK-014.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-014\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 5,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-014",
		TaskPath:         taskPath,
	}); err != nil {
		t.Fatalf("register workgroup: %v", err)
	}
	planPath, _ := writeAutoChainPlanReport(t, filepath.Dir(statePath), 6)
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var planMsg map[string]any
	if err := json.Unmarshal(planData, &planMsg); err != nil {
		t.Fatal(err)
	}
	planMsg["task_id"] = "TASK-014"
	planMsg["team_id"] = "workgroup-delivery-resume"
	patched, _ := json.Marshal(planMsg)
	if err := os.WriteFile(planPath, patched, 0o644); err != nil {
		t.Fatal(err)
	}

	// Drive step 1 only, simulating a chain that died before activation.
	if _, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 6,
		AgentID:          agentID,
		Event:            "readback_submitted",
		MessagePath:      planPath,
	}); err != nil {
		t.Fatalf("readback_submitted: %v", err)
	}

	snap, outcome, err := assignment.AgentBegin(root, statePath, journalPath, assignment.AgentBeginRequest{
		AgentID:  agentID,
		PlanPath: planPath,
	})
	if err != nil {
		t.Fatalf("agent-begin resume: %v", err)
	}
	if !outcome.Chained || outcome.FinalState != "working" {
		t.Fatalf("expected resumed chain to reach working, got %+v", outcome)
	}
	final := stateAfterAutoChain(t, statePath)
	for _, raw := range final["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == agentID && row["state"] != "working" {
			t.Fatalf("resumed agent state = %v, want working", row["state"])
		}
	}
	_ = snap
}
