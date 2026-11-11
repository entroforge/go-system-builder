package qualitygate_test

import (
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"testing"
)

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
