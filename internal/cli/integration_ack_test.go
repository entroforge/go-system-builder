package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

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
func TestVerifiedAcknowledgmentUsesAgentLifecycle(t *testing.T) {
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
	cp := integration.Checkpoint{State: integration.StateVerified, BaselineGeneration: 1, MergeCommit: "merge", TestedHead: "merge"}
	runtimeID, _ := snapshot.State["runtime_id"].(string)
	os.MkdirAll(filepath.Dir(integration.CheckpointPath(fix.root, runtimeID, 1, "a")), 0700)
	snapshot.State["workspace"] = workspace.Encode(&workspace.ExecutionRegistry{Version: 1, MainRoot: fix.root, CommonDir: filepath.Join(fix.root, ".git"), Branch: "feature/customer", Executions: map[string]workspace.Execution{"b": {AssignmentID: "b", RuntimeID: runtimeID, BaselineGeneration: 1, AgentID: "agent-ack"}}})
	if _, err := acknowledgeVerifiedIntegration(fix.root, snapshot, a, cp); err == nil {
		t.Fatal("acknowledged owner before its second assignment was verified")
	}
	if a.CompletionAckRef != "" {
		t.Fatal("premature acknowledgment mutated assignment")
	}
	delete(snapshot.State, "workspace")
	next, err := acknowledgeVerifiedIntegration(fix.root, snapshot, a, cp)
	if err != nil {
		t.Fatal(err)
	}
	rows := next.State["entities"].(map[string]any)["agents"].([]any)
	agent := rows[0].(map[string]any)
	if agent["state"] != "done" || agent["completion_acknowledged_ref"] == nil || a.CompletionAckRef == "" {
		t.Fatalf("missing durable ack: %v", agent)
	}
	again, err := acknowledgeVerifiedIntegration(fix.root, next, a, cp)
	if err != nil || again.Revision != next.Revision {
		t.Fatal("ack not idempotent")
	}
}
