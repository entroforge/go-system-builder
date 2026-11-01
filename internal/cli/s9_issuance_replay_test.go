package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// Concurrent replays reuse the already issued original responsibility.
func TestS9AssignmentIssuanceConcurrentReplayReusesOriginal(t *testing.T) {
	root, plan, _, _, path := repairExecutionPlanningFixture(t)
	assignment := plan.Assignments[0].AssignmentID
	rows := []repairDispatchChoice{{AssignmentID: assignment, RoleFamily: "backend-builder"}}
	if err := persistRepairLeadQuestionnaire(path, rows); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if handled, code := runSavedRepairDispatch(root, repairLeadSaveInput(root, path, "Main"), &stdout, &stderr); !handled || code != 0 {
		t.Fatalf("original dispatch setup: handled=%t code=%d stdout=%s stderr=%s", handled, code, stdout.String(), stderr.String())
	}
	snap := repairDispatchSnapshot(t, root)
	owner := stringMapCLI(repairPointerCLI(snap.State)["assignment_owners"])[assignment]
	if owner == "" {
		t.Fatal("original dispatch did not register owner")
	}
	svc := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
	type observation struct {
		packet string
		fresh  bool
		err    error
	}
	var outcomes [2]observation
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results, err := svc.IssueRepairPlanReportForAssignment(context.Background(), owner, assignment)
			if err != nil {
				outcomes[i].err = err
				return
			}
			if len(results) != 1 {
				outcomes[i].err = os.ErrInvalid
				return
			}
			outcomes[i] = observation{packet: results[0].Packet.ID, fresh: results[0].IssuedByThisCall}
		}(i)
	}
	wg.Wait()
	for _, result := range outcomes {
		if result.err != nil || result.packet == "" {
			t.Fatalf("concurrent issuance failed: %+v", outcomes)
		}
	}
	if outcomes[0].packet != outcomes[1].packet || outcomes[0].fresh || outcomes[1].fresh {
		t.Fatalf("original Writer did not reuse the already issued responsibility: %+v", outcomes)
	}
	results, err := svc.IssueRepairPlanReportForAssignment(context.Background(), owner, assignment)
	if err != nil || len(results) != 1 || results[0].Packet.ID != outcomes[0].packet || results[0].IssuedByThisCall {
		t.Fatalf("same responsibility replay was not reused: %#v %v", results, err)
	}
}
