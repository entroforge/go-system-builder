package assignment

import (
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9LifecycleRetainsOriginalLateResponsibility(t *testing.T) {
	state, packet := lifecycleRepairResultFixture()
	b := packet.Binding.Sources
	execution := map[string]any{
		"assignment_id": b.ManifestAssignmentID, "runtime_id": b.RuntimeID,
		"baseline_generation": b.BaselineGeneration, "agent_id": b.OwnerAgentID,
		"execution_generation": 1, "worktree_path": "/tmp/original-s9-worker", "base_commit": "original-base",
	}
	projection, err := loopruntime.ProjectRepairResultExecution(execution)
	if err != nil {
		t.Fatal(err)
	}
	packet.Inputs.Execution = projection
	state["workspace"] = map[string]any{"executions": map[string]any{b.ManifestAssignmentID: execution}}
	state["baseline"].(map[string]any)["generation"] = b.BaselineGeneration + 1
	selected, err := repairResultLifecyclePacketTargetsOwner(state, b.OwnerAgentID, packet)
	if err != nil || !selected {
		t.Fatalf("retained responsibility selected=%t err=%v", selected, err)
	}
	execution["agent_id"] = "different-owner"
	selected, err = repairResultLifecyclePacketTargetsOwner(state, b.OwnerAgentID, packet)
	if err != nil || selected {
		t.Fatalf("different original execution selected=%t err=%v", selected, err)
	}
}
