package cli

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS7ObserverSkipsAcceptedReviewerQuestionnaires(t *testing.T) {
	root := t.TempDir()
	const answer = ".claude/submissions/s7-answer.json"
	const planID = "review-plan-1"
	for _, test := range []struct {
		status, event string
		wantCandidate bool
	}{
		{status: "pending", event: "review_plan_registered", wantCandidate: true},
		{status: "dispatched", wantCandidate: true},
		{status: "consumed"},
		{status: "result_submitted"},
		{status: "done", event: "review_result_submitted"},
	} {
		t.Run(test.status, func(t *testing.T) {
			state := map[string]any{
				"lifecycle": map[string]any{"state": "verification", "phase": "running"},
				"review": map[string]any{"round": 1, "round_entry": map[string]any{"plan_id": planID}, "assignments": map[string]any{
					"assignment-qa": map[string]any{"status": test.status, "last_lifecycle_event": test.event},
				}},
				"entities": map[string]any{"work_packets": []any{map[string]any{
					"template_id": "s7-review-result-v1", "answer_path": answer,
					"binding": map[string]any{"assignment_id": "assignment-qa", "owner_agent_id": "reviewer-qa", "review_round": 1, "review_plan_id": planID},
				}}},
			}
			found := false
			for _, q := range observableQuestionnaires(root, runtime.Snapshot{State: state}) {
				if q.path == filepath.Join(root, filepath.FromSlash(answer)) {
					found = true
				}
			}
			if found != test.wantCandidate {
				t.Fatalf("generic candidate=%t, want %t for original activity %s/%s", found, test.wantCandidate, test.status, test.event)
			}
		})
	}
	// The observer selects the current original responsibility by its actual
	// ReviewPlan identity; a packet from another plan identity is not observed
	// (L4-hook-acceptance-checklist.md:52 C03).
	t.Run("other plan identity", func(t *testing.T) {
		state := map[string]any{
			"lifecycle": map[string]any{"state": "verification", "phase": "running"},
			"review": map[string]any{"round": 1, "round_entry": map[string]any{"plan_id": "review-plan-2"}, "assignments": map[string]any{
				"assignment-qa": map[string]any{"status": "pending", "last_lifecycle_event": "review_plan_registered"},
			}},
			"entities": map[string]any{"work_packets": []any{map[string]any{
				"template_id": "s7-review-result-v1", "answer_path": answer,
				"binding": map[string]any{"assignment_id": "assignment-qa", "owner_agent_id": "reviewer-qa", "review_round": 1, "review_plan_id": planID},
			}}},
		}
		for _, q := range observableQuestionnaires(root, runtime.Snapshot{State: state}) {
			if q.path == filepath.Join(root, filepath.FromSlash(answer)) {
				t.Fatal("observer observed a packet that carries another ReviewPlan identity")
			}
		}
	})
}
