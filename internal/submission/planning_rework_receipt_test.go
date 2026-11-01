package submission

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestTaskPlanningInvalidatedReceiptKeepsAcceptanceHistory(t *testing.T) {
	p := workpacket.TaskPlanningPacket{ID: "packet-s4", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 3}}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 5}, "evidence": []any{map[string]any{"id": "evidence", "status": "invalid"}}, "entities": map[string]any{"submissions": []any{map[string]any{"packet_id": p.ID, "submission_key": "key", "evidence_id": "evidence", "control_status": "advanced"}}}}
	s := &Service{Root: t.TempDir()}
	r, err := s.taskPlanningReceipt(runtime.Snapshot{State: state}, p)
	if err != nil || !r.Accepted || r.WorkComplete || r.ResponsibilityStatus != "replaced" || r.ConsumerPending || len(r.WaitingOn) != 0 {
		t.Fatalf("invalidated historical S4 receipt: %+v %v", r, err)
	}
}

func TestDesignInvalidatedReceiptKeepsAcceptanceHistory(t *testing.T) {
	p := workpacket.DesignPacket{ID: "packet-s2", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 1}}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "design", "phase_revision": 4}, "evidence": []any{map[string]any{"id": "evidence", "status": "invalid"}}, "entities": map[string]any{"submissions": []any{map[string]any{"packet_id": p.ID, "submission_key": "key", "evidence_id": "evidence", "control_status": "advanced"}}}}
	s := &Service{Root: t.TempDir()}
	r, err := s.designReceipt(runtime.Snapshot{State: state}, p)
	if err != nil || !r.Accepted || r.WorkComplete || r.ResponsibilityStatus != "replaced" || r.ConsumerPending || len(r.WaitingOn) != 0 || strings.Contains(r.NextAction, "unavailable") {
		t.Fatalf("invalidated historical S2 receipt: %+v %v", r, err)
	}
}
