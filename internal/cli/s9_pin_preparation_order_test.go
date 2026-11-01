package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s9RegisteredPreparationFixture(t *testing.T) (string, string, string, *submission.Service) {
	t.Helper()
	root, plan, _, _, path := repairExecutionPlanningFixture(t)
	assignment := plan.Assignments[0].AssignmentID
	if err := persistRepairLeadQuestionnaire(path, []repairDispatchChoice{{AssignmentID: assignment, RoleFamily: "backend-builder"}}); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if handled, code := runSavedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("registration: %s %s", &out, &diagnostic)
	}
	owner := stringMapCLI(repairPointerCLI(repairDispatchSnapshot(t, root).State)["assignment_owners"])[assignment]
	return root, owner, assignment, &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
}

func TestS9PreparationCallbackFollowsOriginalCAS(t *testing.T) {
	root, owner, assignment, svc := s9RegisteredPreparationFixture(t)
	revision := repairDispatchSnapshot(t, root).Revision
	_, err := repair.IssueRepairPlanReportForAssignmentPreparedContext(context.Background(), root, svc.StatePath, svc.JournalPath, revision-1, owner, assignment, func(context.Context, *workpacket.RepairPlanReportPacket) error {
		t.Fatal("stale original CAS prepared native source")
		return nil
	})
	if err == nil {
		t.Fatal("stale original CAS succeeded")
	}
}
