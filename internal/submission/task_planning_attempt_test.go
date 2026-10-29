package submission

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestTaskPlanningRejectedAttemptRetainsOriginalReceipt(t *testing.T) {
	for _, stage := range []string{"S4", "S5", "replaced"} {
		t.Run(stage, func(t *testing.T) {
			p := workpacket.TaskPlanningPacket{ID: "packet", Binding: workpacket.PlanningBinding{RuntimeID: "runtime", Generation: 1, StageEntry: 3}}
			life := map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3}
			status := "pending"
			if stage == "S5" {
				life["state"], life["phase"] = "document_verification", ""
				status = "advanced"
			} else if stage == "replaced" {
				life["phase_revision"] = 4
			}
			state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "lifecycle": life, "entities": map[string]any{"submissions": []any{map[string]any{"packet_id": p.ID, "submission_key": "original", "evidence_id": "evidence", "followup_event_id": "followup", "control_status": status}}}}
			s := &Service{Root: t.TempDir()}
			RecordPlanningDiagnostic(s.Root, state, "outlet", "GATE-S4: task:missing_closing_check")
			r := s.rejectedTaskPlanning(runtime.Snapshot{State: state}, p, runtime.ErrSubmissionConflict)
			if !r.Accepted || r.AttemptStatus != "rejected" || r.Error == "" || r.SubmissionKey != "original" || len(r.DomainRefs) != 1 || r.DomainRefs[0] != "evidence" {
				t.Fatalf("lost reception facts: %+v", r)
			}
			if stage == "replaced" {
				if r.ResponsibilityStatus != "replaced" || r.WorkComplete || r.ConsumerPending || strings.Contains(r.NextAction, "stage-answer rework") || strings.Contains(r.NextAction, "stage-answer recover") {
					t.Fatalf("old responsibility was revived: %+v", r)
				}
			} else if !r.WorkComplete || r.ResponsibilityStatus != "accepted" {
				t.Fatalf("current reception lost: %+v", r)
			}
			if stage == "S4" && !strings.Contains(r.NextAction, "--target tasks --reason '") {
				t.Fatalf("missing executable rework: %s", r.NextAction)
			}
			if stage == "S4" {
				for _, want := range []string{PlanningRecoveryCommand(s.Root, "recover"), "GATE-S4: task:missing_closing_check", "无需重交或另补答案"} {
					if strings.Count(r.NextAction, want) != 1 {
						t.Fatalf("missing or repeated original recovery %q: %s", want, r.NextAction)
					}
				}
				if strings.Contains(r.NextAction, "恢复原已接收内容") || strings.Contains(r.NextAction, "按原回执处理") {
					t.Fatalf("recovery requires reconstructing an answer or finding old guidance: %s", r.NextAction)
				}
			}
			if stage == "S5" && (!strings.Contains(r.NextAction, "真实发现") || strings.Contains(r.NextAction, "--target tasks")) {
				t.Fatalf("incorrect S5 route: %s", r.NextAction)
			}
		})
	}
}
