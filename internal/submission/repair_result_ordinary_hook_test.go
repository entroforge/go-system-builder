package submission

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestRepairResultOrdinarySelectionKeepsCurrentChannelAndExactSource(t *testing.T) {
	root, worker := t.TempDir(), t.TempDir()
	e := workspace.Execution{AssignmentID: "assignment", AgentID: "builder", RuntimeID: "runtime", BaselineGeneration: 1, Generation: 2, Path: worker, Status: "ready", Purpose: workspace.NativeDeliveryPurpose}
	issued := e
	issued.Purpose = "" // An old packet projection cannot select the current channel.
	body, err := json.Marshal(issued)
	if err != nil {
		t.Fatal(err)
	}
	packet := workpacket.RepairResultPacket{ID: "packet", AnswerPath: ".claude/submissions/runtime/g1/S9-result/packet/answer.json", Inputs: workpacket.RepairResultInputs{Execution: body}, Binding: workpacket.RepairResultBinding{Sources: workpacket.RepairPlanReportBinding{RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration, ManifestAssignmentID: e.AssignmentID, OwnerAgentID: e.AgentID, SessionID: "session", PlanRef: "plan"}}}
	state := map[string]any{"runtime_id": e.RuntimeID, "baseline": map[string]any{"generation": 1}, "review": map[string]any{"repair": map[string]any{"session_id": "session", "plan_ref": "plan"}}, "workspace": workspace.ExecutionRegistry{Executions: map[string]workspace.Execution{e.AssignmentID: e}}}
	selected, found, err := selectRepairResultPathInRoots(root, state, filepath.Join(worker, packet.AnswerPath), []workpacket.RepairResultPacket{packet})
	if err != nil || !found || selected.execution.Purpose != workspace.NativeDeliveryPurpose || selected.root != worker || selected.packet.ID != packet.ID {
		t.Fatalf("current source/channel: %+v found=%t err=%v", selected, found, err)
	}
	if _, found, err := selectRepairResultPathInRoots(root, state, filepath.Join(root, packet.AnswerPath), []workpacket.RepairResultPacket{packet}); err != nil || found {
		t.Fatalf("Main same-name path selected: found=%t err=%v", found, err)
	}
	other := packet
	other.ID = "other-packet"
	if _, _, err := selectRepairResultPathInRoots(root, state, filepath.Join(worker, packet.AnswerPath), []workpacket.RepairResultPacket{packet, other}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous source did not fail: %v", err)
	}
}

func TestRepairResultOrdinaryEarlyFailuresNeverMeanQuiet(t *testing.T) {
	root := t.TempDir()
	s := &Service{Root: root, StatePath: filepath.Join(root, "missing-state"), JournalPath: filepath.Join(root, "missing-journal")}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, receive, err := s.SaveRepairResultFromOrdinaryHook(ctx, "answer.json"); !receive || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became quiet: receive=%t err=%v", receive, err)
	}
	if _, receive, err := s.SaveRepairResultFromOrdinaryHook(t.Context(), "answer.json"); !receive || err == nil {
		t.Fatalf("snapshot failure became quiet: receive=%t err=%v", receive, err)
	}
}
