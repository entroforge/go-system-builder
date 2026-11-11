package controller

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestPlanningFollowupUsesOriginalResponsibilityWithoutDigest(t *testing.T) {
	for _, phase := range []string{"contracts", "tasks"} {
		t.Run(phase, func(t *testing.T) {
			state, binding, packet := planningFollowupFixture(t, phase)
			id := packet["packet_id"].(string)
			row := map[string]any{
				"submission_key": "submission:" + id, "packet_id": id, "owner": "Main",
				"evidence_id": "report-original", "followup_event_id": "followup:" + id,
				"control_status": "pending", "origin_state": "planning", "origin_phase": phase,
				"origin_entry": binding.StageEntry,
			}
			entities := state["entities"].(map[string]any)
			entities["work_packets"], entities["submissions"] = []any{packet}, []any{row}
			template := map[string]string{"contracts": "s3-contract-v1", "tasks": "s4-task-planning-v1"}[phase]
			got, err := currentPlanningSubmission(state, id, binding, phase, template)
			if err != nil {
				t.Fatal(err)
			}
			if got.FollowupID != row["followup_event_id"] {
				t.Fatalf("wrong followup: %+v", got)
			}
			if _, err := runtime.SubmissionFollowup(state, got.FollowupID); err != nil {
				t.Fatalf("original followup rejected: %v", err)
			}
			row["owner"] = "someone-else"
			if _, err := runtime.SubmissionFollowup(state, got.FollowupID); err == nil {
				t.Fatal("different owner's followup accepted")
			}
		})
	}
}
