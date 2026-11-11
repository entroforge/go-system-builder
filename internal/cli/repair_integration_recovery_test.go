package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Execute the original native/Integrator/Final APIs up to the actual crash
// boundary. Nothing rewinds Runtime or manufactures a verified checkpoint.
// Professional answers are synthetic; Git and Main checks really execute.
func s9NativeIntegrationCandidate(t *testing.T) (string, string, workspace.Execution, workpacket.RepairResultPacket, integration.Inspection) {
	t.Helper()
	root, worker, planPacket, input, _ := nativeS9WorkerFixture(t)
	input.Event = "SubagentStop"
	var diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &bytes.Buffer{}, &diagnostic); !handled || code != 0 {
		t.Fatalf("original native Stop: %t %d %s", handled, code, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("actual delivery registry: %v", err)
	}
	e := registry.Executions[planPacket.Binding.ManifestAssignmentID]
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef})
	if err != nil {
		t.Fatal(err)
	}
	packet, typed, err := currentRepairResultPacketForDelivery(root, snapshot.State, e, candidate)
	if err != nil || !typed {
		t.Fatalf("original typed candidate: %v", err)
	}
	ctx := context.Background()
	a := hookctx.AssignmentContext{AssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID, TaskID: packet.Binding.Sources.TaskID, WorktreePath: e.Path, Branch: e.Branch, TargetBranch: e.TargetBranch}
	inspection, err := integration.Inspect(ctx, integration.InspectRequest{Root: root, Assignment: a, TargetBranch: e.TargetBranch, BaselineGeneration: e.BaselineGeneration, RuntimeID: e.RuntimeID}, integration.InspectConfig{SkipCompletionCheck: true})
	if err != nil || !inspection.Ready {
		t.Fatalf("original Main Inspect: %+v %v", inspection, err)
	}
	return root, worker, e, packet, inspection
}

func s9BeforeIntegrationAck(t *testing.T, acceptFinal bool) (string, string, workspace.Execution, workpacket.RepairResultPacket, integration.Checkpoint, repair.ArtifactRef) {
	t.Helper()
	root, worker, e, packet, inspection := s9NativeIntegrationCandidate(t)
	snapshot := repairDispatchSnapshot(t, root)
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: []string{"git grep -q 'const Restored = true' -- internal/api/one.go"}, CheckRunner: integration.CommandCheckRunner})
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("real Main check did not verify: %+v %v", verified, err)
	}
	var resultRef repair.ArtifactRef
	if acceptFinal {
		proof, err := repairResultMainProof(root, e, verified.Checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		_, result, ref, err := repair.SubmitIssuedRepairResult(context.Background(), root, resolveRootPath(root, ".claude/loop-state.json"), resolveRootPath(root, ".claude/loop-events.jsonl"), repair.SubmitIssuedRepairResultRequest{ExpectedRevision: snapshot.Revision, PacketID: packet.ID, OwnerAgentID: e.AgentID, MainProof: proof})
		if err != nil || result.Result != "pass" || result.Receipt == nil {
			t.Fatalf("original Final Writer did not accept actual Main proof: %+v %v", result, err)
		}
		resultRef = ref
	}
	return root, worker, e, packet, verified.Checkpoint, resultRef
}
