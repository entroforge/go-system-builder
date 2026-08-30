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
	contractPath := "docs/contracts/BE-039.md"
	data := []byte("# BE-039\n\n> Status: locked\n> Version: v1.0.2\n\n" +
		"### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n")
	indexPath := "docs/contracts/CONTRACTS-039.md"
	indexData := []byte("# CONTRACTS-039\n\n> 状态：locked\n> 版本：v1.0.2\n\n## 需求覆盖矩阵\n\n" +
		"| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n" +
		"| REQ-039/FR-001 | — | BE-039 §1 | — |\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, contractPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, indexPath), indexData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, contractPath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Do not hand-seed documents[] — the disk declaration and
	// PTR-PLAN-02's commit-time registration must carry the chain.
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
	data := []byte("# TASK-039-01\n\n> Status: complete\n> Version: v1.0.2\n> Primary contract: BE-039\n\n" +
		"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039 | §1 |\n\n" +
		"## 7. Closing Contract\n\n```text\nassert BE-039 §1 == satisfied\n```\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, taskPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskPath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Do not hand-seed documents[] — disk + TR-002 registration.
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
