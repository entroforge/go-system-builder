package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestS7CompletionProjectionRejectsGenericReportedState(t *testing.T) {
	fix := newRuntimeFixture(t)
	const owner = "agent-qa-1"
	fix.addAgent(owner, "qa", "reported", "workgroup-qa", []string{"TASK-QA"})
	fix.addTeam("workgroup-qa", "active", []string{owner})
	fix.addTask("TASK-QA", "review", []string{owner})
	fix.state["review"] = map[string]any{"round": 1, "assignments": map[string]any{owner: map[string]any{
		"agent_id": owner, "status": "dispatched", "lens": "qa", "claim_ids": []any{"claim-qa"},
	}}}
	agent := fix.state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	agent["completion_reported_ref"] = "generic-completion.json"
	fix.persist(t)
	wgDir := filepath.Join(fix.root, ".claude", "workgroups", "REQ-039", "workgroup-qa")
	if err := os.MkdirAll(wgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]any{
		"activation.json": map[string]any{"agent_id": owner},
		"manifest.json": map[string]any{"runtime_id": "loop-REQ-039", "baseline_generation": 1, "assignments": []any{map[string]any{
			"assignment_id": owner, "agent_id": owner, "task_id": "TASK-QA", "responsibility_id": "QA-REVIEW", "role_family": "qa", "write_paths": []any{"docs/reports/"}, "status": "complete",
		}}},
	} {
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wgDir, name), encoded, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := hookctx.LoadFull(fix.root, owner)
	if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil || !loaded.PolicyContext.Agent.ReviewAssignment {
		t.Fatalf("fixture did not resolve the original S7 dispatch: loaded=%+v err=%v", loaded, err)
	}
	if resolveAgentReportComplete(fix.root, policy.Input{AgentID: owner}) {
		t.Fatal("generic completion or reported state replaced the canonical S7 consumed Result")
	}
	manifestPath := filepath.Join(wgDir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["assignments"].([]any)[0].(map[string]any)["status"] = "blocked"
	blockedBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, blockedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err = hookctx.LoadFull(fix.root, owner)
	if err != nil || loaded == nil {
		t.Fatalf("fixture did not expose manifest blocked text: loaded=%+v err=%v", loaded, err)
	}
	assignment := hook.AssignmentForAgent(loaded.Assignments, owner)
	if assignment == nil || assignment.ReportStatus != "blocked" {
		t.Fatalf("fixture did not retain manifest blocked text: %+v", assignment)
	}
	if hook.HasBlockerReport(assignment) {
		t.Fatal("manifest blocked text was mistaken for an original blocker receipt")
	}
	if resolveAgentReportComplete(fix.root, policy.Input{AgentID: owner}) {
		t.Fatal("manifest blocked text replaced the original Runtime blocker receipt")
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if resolveAgentReportComplete(fix.root, policy.Input{AgentID: owner}) {
		t.Fatal("known S7 owner with missing manifest fell back to generic completion")
	}
}
