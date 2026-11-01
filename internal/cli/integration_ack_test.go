package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestControllerCheckpointPathUsesLoadedCurrentRecordRevision(t *testing.T) {
	loaded := &hookctx.LoadedContext{
		BaselineGeneration: 3,
		PolicyContext:      policy.RuntimeContext{RuntimeID: "loop-ack-path"},
	}
	ref := assignmentrecord.RecordReference{
		RuntimeID: "loop-ack-path", BaselineGeneration: 3, AssignmentID: "assignment-ack-path", Revision: 2,
	}
	assignment := hookctx.AssignmentContext{
		AssignmentID: "assignment-ack-path", AssignmentRecord: &ref, AssignmentRevision: 2,
	}
	got, err := checkpointPathForLoadedAssignment("/repo", loaded, assignment)
	if err != nil || got != integration.CheckpointPathForRecord("/repo", ref) {
		t.Fatalf("current r2 controller slot = %q, err %v", got, err)
	}
	assignment.AssignmentRecord = nil
	if _, err := checkpointPathForLoadedAssignment("/repo", loaded, assignment); err == nil {
		t.Fatal("missing current responsibility selected a legacy checkpoint slot")
	}
}

func TestIntegrationSelectionRequiresExactAssignmentAndOwner(t *testing.T) {
	loaded := &hookctx.LoadedContext{Assignments: []hookctx.AssignmentContext{{AssignmentID: "a", OwnerAgentID: "builder"}, {AssignmentID: "b", OwnerAgentID: "builder"}}}
	if findAssignmentForInput(loaded, policy.Input{AgentID: "builder"}) != nil {
		t.Fatal("ambiguous owner selected")
	}
	if findAssignmentForInput(loaded, policy.Input{AgentID: "builder", TargetID: "missing"}) != nil {
		t.Fatal("unknown assignment fell back to owner")
	}
	a := findAssignmentForInput(loaded, policy.Input{AgentID: "builder", TargetID: "b"})
	if a == nil || a.AssignmentID != "b" {
		t.Fatal("wrong assignment")
	}
	a.CompletionAckRef = "ack"
	if loaded.Assignments[1].CompletionAckRef != "ack" {
		t.Fatal("lost ack through row copy")
	}
}
func TestVerifiedAcknowledgmentRequiresOriginalResponsibility(t *testing.T) {
	fix := newRuntimeFixture(t)
	definitionPath := filepath.Join(fix.root, "docs/control/loop-definition.json")
	data, _ := os.ReadFile(definitionPath)
	var definition map[string]any
	json.Unmarshal(data, &definition)
	definition["entity_lifecycles"].(map[string]any)["agent"].(map[string]any)["transitions"] = []any{map[string]any{"from": "reported", "event": "completion_acknowledged", "to": "done"}}
	data, _ = json.Marshal(definition)
	os.WriteFile(definitionPath, data, 0600)
	entities := fix.state["entities"].(map[string]any)
	entities["agents"] = []any{map[string]any{"id": "agent-ack", "role": "builder", "state": "reported", "task_ids": []any{"TASK-001"}, "team_id": nil, "definition_ref": "agents/builder.md", "prompt_ref": "manifest#a", "readback_ref": nil, "activation_ref": nil, "activation_revision": nil, "updated_at": "2026-09-20T00:00:00Z"}}
	fix.persist(t)
	snapshot, err := runtime.NewStore(filepath.Join(fix.root, ".claude/loop-state.json"), filepath.Join(fix.root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	a := &hookctx.AssignmentContext{AssignmentID: "a", OwnerAgentID: "agent-ack", TaskID: "TASK-001"}
	cp := integration.Checkpoint{State: integration.StateVerified, BaselineGeneration: 1}
	if _, err := acknowledgeVerifiedIntegration(fix.root, snapshot, a, cp); err == nil {
		t.Fatal("acknowledged without the original responsibility and accepted result")
	}
	if a.CompletionAckRef != "" {
		t.Fatal("premature acknowledgment mutated assignment")
	}
}
