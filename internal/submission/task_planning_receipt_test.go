package submission

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestTaskPlanningReceiptProjectsRecordedFactsWithoutArtifactReads(t *testing.T) {
	for _, status := range []string{"pending", "no_transition", "advanced"} {
		t.Run(status, func(t *testing.T) {
			p := workpacket.TaskPlanningPacket{ID: "packet-s4", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 3}}
			state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3}, "entities": map[string]any{"submissions": []any{map[string]any{"packet_id": p.ID, "submission_key": "recorded-key", "evidence_id": "recorded-evidence", "followup_event_id": "recorded-followup", "control_status": status}}}}
			s := &Service{Root: t.TempDir()}
			got, err := s.taskPlanningReceipt(runtime.Snapshot{State: state}, p)
			if err != nil || !got.Accepted || !got.WorkComplete || got.SubmissionKey != "recorded-key" || len(got.DomainRefs) != 1 || got.DomainRefs[0] != "recorded-evidence" {
				t.Fatalf("record projection: %+v %v", got, err)
			}
			if status == "advanced" {
				if len(got.WaitingOn) != 0 || got.ConsumerPending || !strings.Contains(got.NextAction, "S5") {
					t.Fatalf("advanced receipt: %+v", got)
				}
			} else if len(got.WaitingOn) != 1 || got.WaitingOn[0].Ref != "recorded-followup" || !got.ConsumerPending || got.ConsumerOwner != "main" || !strings.Contains(got.NextAction, "recorded-followup") {
				t.Fatalf("pending receipt: %+v", got)
			}
			state["baseline"] = map[string]any{"generation": 2}
			replaced, err := s.taskPlanningReceipt(runtime.Snapshot{State: state}, p)
			if err != nil || !replaced.Accepted || replaced.WorkComplete || replaced.ResponsibilityStatus != "replaced" || replaced.ConsumerPending || len(replaced.WaitingOn) != 0 || strings.Contains(replaced.NextAction, "S5") {
				t.Fatalf("replaced receipt: %+v %v", replaced, err)
			}
		})
	}
}

func TestTaskPlanningReceiptWithoutRecordDoesNotClaimAcceptance(t *testing.T) {
	s := &Service{Root: t.TempDir()}
	got, err := s.taskPlanningReceipt(runtime.Snapshot{State: map[string]any{}}, workpacket.TaskPlanningPacket{ID: "missing"})
	if err != nil || got.Accepted || got.WorkComplete {
		t.Fatalf("missing receipt: %+v %v", got, err)
	}
}

func TestTaskPlanningControlGateCauseSurvivesNewSessionAndClearsOnCommit(t *testing.T) {
	root := t.TempDir()
	p := workpacket.TaskPlanningPacket{ID: "packet-s4", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 3}}
	row := map[string]any{"packet_id": p.ID, "submission_key": "recorded-key", "evidence_id": "recorded-evidence", "followup_event_id": "recorded-followup", "control_status": "pending"}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3}, "entities": map[string]any{"submissions": []any{row}}}
	snap := runtime.Snapshot{State: state}
	service := &Service{Root: root}
	result := controller.ControlResult{Snapshot: snap, QualityGate: controller.QualityGateResult{GateID: "GATE-S4", Missing: []string{"TASK:missing_closing_check"}, Conflicts: []string{"TASK:resource_conflict"}}}
	r, err := service.taskPlanningControlReceipt(snap, p, result, nil)
	if err != nil || !r.Accepted {
		t.Fatalf("gate receipt %+v %v", r, err)
	}
	for _, want := range []string{"GATE-S4", "TASK:missing_closing_check", "TASK:resource_conflict", "stage-answer recover"} {
		if !strings.Contains(r.NextAction, want) {
			t.Fatalf("first cause missing %s: %s", want, r.NextAction)
		}
		resumed, err := (&Service{Root: root}).taskPlanningReceipt(snap, p)
		if err != nil || !strings.Contains(resumed.NextAction, want) {
			t.Fatalf("new session lost %s: %+v %v", want, resumed, err)
		}
	}
	row["control_status"] = "advanced"
	result.QualityGate = controller.QualityGateResult{TransitionCommitted: true}
	if _, err := service.taskPlanningControlReceipt(snap, p, result, nil); err != nil {
		t.Fatal(err)
	}
	if cause := PlanningDiagnostic(root, state, "outlet"); cause != "" {
		t.Fatal("committed outlet retained error", cause)
	}
}
