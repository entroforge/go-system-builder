package hook

import (
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// currentBuilderWorkComplete reads the original registered result and owner
// facts. Main integration and its checks remain Main's separate responsibility.
func currentBuilderWorkComplete(root string, snapshot loopruntime.Snapshot, assignment *hookctx.AssignmentContext) bool {
	if assignment == nil || assignment.OwnerAgentID == "" || assignment.AssignmentID == "" || assignment.TaskID == "" || assignment.CompletionRef == "" || !requiresBuilderReceipt(assignment) {
		return false
	}
	record, found, err := assignmentrecord.ForOwner(snapshot.State, assignment.OwnerAgentID)
	if err != nil || !found || record.AssignmentID != assignment.AssignmentID || record.Task.ID != assignment.TaskID || record.ClosingContract == nil {
		return false
	}
	if assignment.AssignmentRecord != nil && *assignment.AssignmentRecord != record.Reference() {
		return false
	}
	result, err := assignmentresult.LoadRecordedAccepted(snapshot.State, record, assignment.CompletionRef, func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, 20<<20)
	})
	if err != nil || !assignmentresult.ClosingAssessmentComplete(result.Answer) {
		return false
	}
	for _, fact := range result.WorkerCheckFacts {
		if fact.Status == "fail" {
			return false
		}
	}
	return true
}
