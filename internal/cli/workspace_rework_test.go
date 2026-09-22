package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkspaceReworkArchivesFailureAndResumesInterruptedFinalization(t *testing.T) {
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if _, err := runGit(t, fix.root, args...); err != nil {
			t.Fatal(err)
		}
	}
	definition := filepath.Join(fix.root, "docs/control/loop-definition.json")
	data, _ := os.ReadFile(definition)
	var def map[string]any
	json.Unmarshal(data, &def)

	lifecycles, _ := def["entity_lifecycles"].(map[string]any)
	if lifecycles == nil {
		lifecycles = map[string]any{}
		def["entity_lifecycles"] = lifecycles
	}
	for name, states := range map[string][2]string{"agent": {"reported", "working"}, "task": {"review", "in_progress"}} {
		lifecycles[name] = map[string]any{"states": []string{states[0], states[1]}, "initial_state": states[0], "transitions": []any{map[string]any{"from": states[0], "event": "integration_rework_requested", "to": states[1], "guards": []string{}}}}
	}
	data, _ = json.Marshal(def)
	os.WriteFile(definition, data, 0600)
	fix.addAgent("builder-rework", "builder", "reported", "workgroup-rework", []string{"TASK-001"})
	// Replace sample tasks to keep this recovery fixture exact.
	fix.state["entities"].(map[string]any)["tasks"] = []any{}
	fix.addTask("TASK-001", "review", []string{"builder-rework"})
	agents := fix.state["entities"].(map[string]any)["agents"].([]any)
	var agent map[string]any
	for _, raw := range agents {
		row := raw.(map[string]any)
		if row["id"] == "builder-rework" {
			agent = row
		}
	}
	agent["completion_reported_ref"] = ".claude/report.json"
	agent["prompt_ref"] = ".claude/workgroups/REQ-039/workgroup-rework/manifest.json#assignment-rework"
	manifest := filepath.Join(fix.root, ".claude/workgroups/REQ-039/workgroup-rework/manifest.json")
	os.MkdirAll(filepath.Dir(manifest), 0700)
	os.WriteFile(filepath.Join(filepath.Dir(manifest), "activation.json"), []byte(`{"agent_id":"builder-rework","allowed_tools":["Write"],"allowed_write_paths":["src"]}`), 0600)
	data, _ = json.Marshal(map[string]any{"runtime_id": workspace.RuntimeID(fix.state), "assignments": []any{map[string]any{"assignment_id": "assignment-rework", "agent_id": "builder-rework", "write_paths": []string{"src"}}}})
	os.WriteFile(manifest, data, 0600)
	bindFixtureWorkspace(t, fix)
	b, err := workspace.New(context.Background(), fix.root)
	if err != nil {
		t.Fatal(err)
	}
	e := workspace.Execution{AssignmentID: "assignment-rework", RuntimeID: workspace.RuntimeID(fix.state), BaselineGeneration: workspace.Generation(fix.state), Generation: 1, AgentID: "builder-rework", Path: filepath.Join(fix.root, ".worktrees/rework"), Branch: "codex/rework", TargetBranch: b.Branch, BaseCommit: b.BoundHead, Status: "preparing", Inputs: map[string]string{}, WritePaths: []string{"src"}, Checks: []string{}}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	fix.state["workspace"] = workspace.Encode(b)
	fix.persist(t)
	cpPath := integration.CheckpointPath(fix.root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)
	os.MkdirAll(filepath.Dir(cpPath), 0700)
	cp, err := integration.DefaultCheckpointStore().CompareAndSwap(cpPath, integration.Checkpoint{}, integration.Checkpoint{AssignmentID: e.AssignmentID, TaskID: "TASK-001", State: integration.StatePreserved, WorktreePath: e.Path, SourceBranch: e.Branch, SourceHead: e.BaseCommit, TargetBranch: e.TargetBranch, BaselineGeneration: e.BaselineGeneration, FailureReason: "project check failed"})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--root", fix.root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--reason", "correct failing assertion"}
	var out, stderr bytes.Buffer
	if code := runWorkspaceRework(args, &out, &stderr); code != 0 {
		t.Fatalf("rework=%d %s", code, &stderr)
	}
	bound, state, err := workspace.Load(fix.root)
	if err != nil {
		t.Fatal(err)
	}
	next := bound.Executions[e.AssignmentID]
	if next.Status != "ready" || next.ReworkRef == "" {
		t.Fatalf("not ready after rework: %+v", next)
	}
	if _, err = os.Stat(cpPath); !os.IsNotExist(err) {
		t.Fatal("old checkpoint still active")
	}
	if _, err = os.Stat(filepath.Join(fix.root, next.ReworkRef) + ".checkpoint.json"); err != nil {
		t.Fatal("old failure lost")
	}
	found := false
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == e.AgentID {
			found = row["state"] == "working" && row["completion_reported_ref"] == nil
		}
	}
	if !found {
		t.Fatal("owner cannot resume actual work")
	}
	// Reproduce interruption after the archive rename but before final ready CAS.
	next.Status = "preparing"
	bound.Executions[e.AssignmentID] = next
	state["workspace"] = workspace.Encode(bound)
	data, _ = json.Marshal(state)
	os.WriteFile(fix.statePath, data, 0600)
	out.Reset()
	stderr.Reset()
	if code := runWorkspaceRework(args, &out, &stderr); code != 0 {
		t.Fatalf("rework resume=%d %s", code, &stderr)
	}
	preservedData, _ := os.ReadFile(filepath.Join(fix.root, next.ReworkRef) + ".checkpoint.json")
	var preserved integration.Checkpoint
	json.Unmarshal(preservedData, &preserved)
	if preserved.Revision != cp.Revision || preserved.State != integration.StatePreserved {
		t.Fatal("history changed during resume")
	}
}
