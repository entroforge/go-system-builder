package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// planCheckpointState builds a runtime whose reviewer agent is dispatched
// with dispatch_mode=plan_checkpoint in reading state.
func planCheckpointState(t *testing.T, root, mode string) (string, string) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-REQ-002-plan"
	state["revision"] = 7
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	agent := map[string]any{
		"id": "agent-plan-1", "role": "backend-builder", "state": "reading",
		"task_ids": []any{"TASK-002"}, "team_id": "workgroup-build-1",
		"definition_ref": "agents/backend-builder.md",
		"prompt_ref":     "manifest#assignment-build-1",
		"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-18T00:00:00Z",
	}
	if mode != "" {
		agent["dispatch_mode"] = mode
	}
	state["entities"] = map[string]any{
		"agents": []any{agent}, "tasks": []any{}, "bugs": []any{}, "teams": []any{},
	}
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	out, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	return statePath, journalPath
}

// writePlanReport writes a minimal plan_report message file and returns its
// path plus the file's sha256 for the activation chain.
func writePlanReport(t *testing.T, dir string, revision int) (string, string) {
	t.Helper()
	message := map[string]any{
		"schema_version": "1.0.0", "message_type": "plan_report",
		"message_id": "msg-plan-0001", "correlation_id": "corr-0001",
		"runtime_id": "loop-REQ-002-plan", "expected_runtime_revision": revision,
		"agent_id": "agent-plan-1", "agent_definition_ref": "agents/backend-builder.md",
		"task_id": "TASK-002", "bug_id": nil, "team_id": "workgroup-build-1",
		"occurred_at":   "2026-08-18T00:00:00Z",
		"assignment_id": "assignment-build-1", "assignment_revision": 1,
		"objective":     "Implement the locked TASK-002 API surface with passing checks",
		"planned_paths": []string{"internal/api/handler.go"},
		"steps":         []any{map[string]any{"description": "implement handler", "target": "internal/api/handler.go"}},
		"assertion_checks": []any{map[string]any{
			"assertion": "contract clauses all routed", "oracle": "clause map complete",
		}},
		"dependencies": []string{}, "risks_blockers": []string{},
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

// planCheckpointActivation builds the activation message from the schema's
// canonical example, rebinding the hash chain to the plan report file.
func planCheckpointActivation(t *testing.T, revision int, planSHA string) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message["message_type"] != "activation" {
			continue
		}
		message["agent_id"] = "agent-plan-1"
		message["task_id"] = "TASK-002"
		message["runtime_id"] = "loop-REQ-002-plan"
		message["team_id"] = "workgroup-build-1"
		message["expected_runtime_revision"] = float64(revision)
		message["approved_readback_sha256"] = planSHA
		message["approved_readback_message_id"] = "msg-plan-0001"
		return message
	}
	t.Fatal("missing activation example")
	return nil
}

func writeMessage(t *testing.T, dir, name string, body map[string]any) string {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPlanCheckpointActivatesStraightOffPlanReport pins L4 §3.3 continuous
// execution: plan_report (readback_submitted) → activation_sent directly,
// no approval round. The hash chain still binds the activation to the plan
// file bytes.
func TestPlanCheckpointActivatesStraightOffPlanReport(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := planCheckpointState(t, root, "plan_checkpoint")
	dir := filepath.Dir(statePath)

	planPath, planSHA := writePlanReport(t, dir, 7)
	submitted, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-plan-1", Event: "readback_submitted",
		MessagePath: planPath,
	})
	if err != nil {
		t.Fatalf("plan report submission: %v", err)
	}
	// Direct activation without understanding_approved.
	activationPath := writeMessage(t, dir, "activation.json", planCheckpointActivation(t, submitted.Revision, planSHA))
	activated, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: submitted.Revision, AgentID: "agent-plan-1", Event: "activation_sent",
		MessagePath: activationPath,
	})
	if err != nil {
		t.Fatalf("direct activation off the plan report: %v", err)
	}
	state := activated.State
	agents := state["entities"].(map[string]any)["agents"].([]any)
	if agents[0].(map[string]any)["state"] != "activated" {
		t.Fatalf("agent state = %v, want activated", agents[0].(map[string]any)["state"])
	}
	if data, _ := json.Marshal(state); semantic.ValidateRuntimeBytes(root, data) != nil {
		t.Fatalf("invalid runtime: %v", semantic.ValidateRuntimeBytes(root, data))
	}
}

// TestApprovalModeStillRequiresApproval pins the exception path:
// plan_approval_required agents cannot activate off the plan submission.
func TestApprovalModeStillRequiresApproval(t *testing.T) {
	root := filepath.Join("..", "..")
	statePath, journalPath := planCheckpointState(t, root, "plan_approval_required")
	dir := filepath.Dir(statePath)

	// Approval mode rejects the plan_report message type.
	planPath, _ := writePlanReport(t, dir, 7)
	_, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-plan-1", Event: "readback_submitted",
		MessagePath: planPath,
	})
	if err == nil || !strings.Contains(err.Error(), "readback_response") {
		t.Fatalf("approval mode must reject plan_report, got %v", err)
	}
}
