package cli

import (
	"bytes"
	"context"
	"fmt"
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
	prepared := false
	issued, err := repair.IssueRepairPlanReportForAssignmentPreparedContext(context.Background(), root, svc.StatePath, svc.JournalPath, revision-1, owner, assignment, func(_ context.Context, packet *workpacket.RepairPlanReportPacket) error {
		prepared = true
		if packet.Binding.OwnerAgentID != owner {
			return fmt.Errorf("preparation left the original owner %s", packet.Binding.OwnerAgentID)
		}
		if packet.Binding.RepairAssignmentID != assignment {
			return fmt.Errorf("preparation left the original assignment %s", packet.Binding.RepairAssignmentID)
		}
		return nil
	})
	// The Runtime revision is an audit coordinate, not a write gate
	// (L4-revision-usage.md:141), and L4-workflow-convergence.md:138 coordinates
	// the original transaction on the existing responsibility and internal
	// version instead of a hash. A stale revision coordinate therefore no
	// longer refuses the original responsibility.
	if err != nil {
		t.Fatalf("stale revision coordinate refused the original responsibility: %v", err)
	}
	// The issuance stays inside the original responsibility: exactly one
	// PlanReport packet, bound to the original owner and the original
	// assignment.
	if len(issued) != 1 {
		t.Fatalf("original responsibility issued %d PlanReport packets", len(issued))
	}
	if issued[0].Packet.Binding.OwnerAgentID != owner || issued[0].Packet.Binding.RepairAssignmentID != assignment {
		t.Fatalf("issuance left the original responsibility: %+v", issued[0].Packet.Binding)
	}
	// L3-S9-repair.md:142 keeps already issued material valid. The original
	// questionnaire is replayed exactly and is neither issued nor prepared a
	// second time.
	if issued[0].IssuedByThisCall {
		t.Fatal("already issued PlanReport material was issued again")
	}
	if prepared {
		t.Fatal("already issued PlanReport material was prepared again")
	}
}
