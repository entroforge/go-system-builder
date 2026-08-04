package req039fixtures

import (
	"os"
	"path/filepath"
	"testing"
)

// WritePlanningContractPass adds locked contract document + planning evidence.
func WritePlanningContractPass(t *testing.T, root string, state map[string]any) {
	t.Helper()
	EnsureStateRoot(state, root)
	contractPath := "docs/contracts/BE-039-loop-controller.md"
	data := []byte("# BE-039\n> Status: locked\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, contractPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, contractPath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	documents, _ := state["documents"].([]any)
	state["documents"] = append(documents, map[string]any{
		"id": "BE-039", "kind": "contract", "path": contractPath, "version": "v1.0.2",
		"sha256": Sha256Hex(data), "status": "locked", "generation": 1,
	})
	envelope := EvidenceEnvelope(state, "ev-contracts", "planning_contract", "contract-planner-1", "Contract Planner", "pass", map[string]any{
		"review_round": 1,
		"subject_refs": []any{map[string]any{"path": contractPath, "version": "v1.0.2", "sha256": Sha256Hex(data)}},
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-contracts", "planning_contract", "contract-planner-1", "Contract Planner", envelope, []any{contractPath}))
}

// WritePlanningTaskPass adds complete task document + planning evidence.
func WritePlanningTaskPass(t *testing.T, root string, state map[string]any) {
	t.Helper()
	EnsureStateRoot(state, root)
	taskPath := "docs/tasks/TASK-039-01-loop-definition.md"
	data := []byte("# TASK-039-01\n> Status: complete\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, taskPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskPath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	documents, _ := state["documents"].([]any)
	state["documents"] = append(documents, map[string]any{
		"id": "TASK-039-01", "kind": "task", "path": taskPath, "version": "v1.0.2",
		"sha256": Sha256Hex(data), "status": "complete", "generation": 1,
	})
	envelope := EvidenceEnvelope(state, "ev-tasks", "planning_task", "task-planner-1", "Task Planner", "pass", map[string]any{
		"review_round": 1,
		"subject_refs": []any{map[string]any{"path": taskPath, "version": "v1.0.2", "sha256": Sha256Hex(data)}},
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-tasks", "planning_task", "task-planner-1", "Task Planner", envelope, []any{taskPath}))
}

// SetLifecyclePhase updates lifecycle + milestone projection for a cursor.
func SetLifecyclePhase(state map[string]any, lifecycleState, phase string) {
	state["lifecycle"] = map[string]any{"state": lifecycleState, "phase": phaseOrNil(phase), "phase_revision": 0}
	if ms, ok := state["milestone"].(map[string]any); ok {
		ms["stage"] = stageFor(lifecycleState, phase)
		ms["lifecycle_state"] = lifecycleState
		ms["lifecycle_phase"] = phaseOrNil(phase)
	}
}
