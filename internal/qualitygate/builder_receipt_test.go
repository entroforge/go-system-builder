package qualitygate_test

import (
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"testing"
)

func TestBuilderReceiptRequiresOriginalRegisteredResponsibility(t *testing.T) {
	b := newBatchP0Input(t)
	const taskID = "TASK-TEST-01"
	b.addCompletion(t, taskID, nil)
	b.addCheckpoint(t, taskID, "verified")
	ref := "evidence/ev-completion-" + taskID + ".json"
	task := b.input.Snapshot.State["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
	task["owner_agent_ids"], task["completion_report_ref"] = []any{"builder-1"}, ref
	b.input.Snapshot.State["entities"].(map[string]any)["agents"] = []any{map[string]any{"id": "builder-1", "task_ids": []any{taskID}, "state": "reported", "prompt_ref": "manifest#assignment-1", "completion_reported_ref": ref}}
	got := qualitygate.CurrentBuilderReceipt(b.input, qualitygate.BuilderReceiptRequest{Owner: "builder-1", AssignmentID: "assignment-1", TaskID: taskID, CompletionRef: ref})
	if got.Accepted || got.WorkComplete || got.Integrated || len(got.WaitingOn) != 1 || got.WaitingOn[0].Owner != "main" || got.WaitingOn[0].Reason != "assignment_unavailable" {
		t.Fatalf("old envelope or checkpoint invented an original Assignment: %+v", got)
	}
	assertCanonicalReceiptSchema(t, got)
}

func TestCanonicalBuilderReceiptDoesNotTransferOriginalResponsibility(t *testing.T) {
	for _, change := range []string{"owner", "assignment", "task"} {
		t.Run(change, func(t *testing.T) {
			fixture := newCanonicalReceiptFixtureWithClosing(t)
			req := fixture.request
			switch change {
			case "owner":
				req.Owner = "other-owner"
			case "assignment":
				req.AssignmentID = "other-assignment"
			case "task":
				req.TaskID = "TASK-other"
			}
			got := qualitygate.CurrentBuilderReceipt(fixture.input, req)
			if got.Accepted || got.WorkComplete || got.Integrated {
				t.Fatalf("another responsibility claimed the original result: %+v", got)
			}
		})
	}
}
