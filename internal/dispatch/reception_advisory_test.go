package dispatch

import (
	"github.com/entroforge/go-system-builder/internal/integration"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func receptionBoard() Board {
	p := &semantic.DispatchPlan{Tasks: []semantic.DispatchTask{{ID: "TASK-A"}, {ID: "TASK-B"}, {ID: "TASK-C", Dependencies: []string{"TASK-A"}}, {ID: "TASK-D", Dependencies: []string{"TASK-A"}}}}
	return Board{Plan: p, Rows: []semantic.DispatchRow{
		{Task: p.Tasks[0], State: workstate.Active, DeliveryRecorded: true},
		{Task: p.Tasks[1], State: workstate.Pending, Eligible: true, Dispatchable: true},
		{Task: p.Tasks[2], State: workstate.Pending, Reason: "await verified integration: TASK-A"},
		{Task: p.Tasks[3], State: workstate.Pending, Reason: "await verified integration: TASK-A"},
	}, Next: []string{"TASK-B"}}
}

func TestActualBusyObservationSelectsReadOnlyActionWithoutDroppingPending(t *testing.T) {
	capacity := 2
	pending := []Reception{{AssignmentID: "original", TaskID: "TASK-A", State: "reported", Command: []string{"original", "receive"}}}
	a := AdviseReception(receptionBoard(), pending, &capacity, nil)
	for _, state := range []string{"available", "not_created", "unknown"} {
		observed := WithIntegrationObservation(a, integration.WorkspaceObservation{LockState: state})
		if observed.Action.Kind != "receive_integration" || observed.Action.Subject != "original" {
			t.Fatal(observed)
		}
	}
	observed := WithIntegrationObservation(a, integration.WorkspaceObservation{Checkout: "/actual-main", LockRef: "/actual-main/.claude/integration.lock", LockState: "busy"})
	if observed.Action.Kind != "observe_integration" || !reflect.DeepEqual(observed.Action.Argv, []string{"loop-harness", "runtime", "workspace", "integration-status", "--root", "/actual-main"}) || len(observed.Pending) != 1 || observed.Pending[0].Subject != "original" || !reflect.DeepEqual(observed.RecommendedTasks, a.RecommendedTasks) {
		t.Fatal(observed)
	}
}

func TestReceptionPriorityPreservesOriginalBoardAndRequiredSet(t *testing.T) {
	b := receptionBoard()
	before := append([]semantic.DispatchRow{}, b.Rows...)
	capacity := 3
	rows := []Reception{{AssignmentID: "independent", TaskID: "TASK-B", Command: []string{"original", "B"}, State: "reported"}, {AssignmentID: "critical", TaskID: "TASK-A", Command: []string{"original", "A"}, State: "reported"}}
	a := AdviseReception(b, rows, &capacity, nil)
	if a.Action.Subject != "critical" || !reflect.DeepEqual(a.Action.Argv, rows[1].Command) || !a.AdvisoryOnly || *a.DispatchBudget != 3 {
		t.Fatal(a)
	}
	if !reflect.DeepEqual(before, b.Rows) || b.Next[0] != "TASK-B" {
		t.Fatal("advice changed original board")
	}
	if len(a.RecommendedTasks) != 1 || a.RecommendedTasks[0] != "TASK-B" {
		t.Fatal("reported result released consumers", a)
	}
	reversed := []Reception{rows[1], rows[0]}
	if !reflect.DeepEqual(a, AdviseReception(b, reversed, &capacity, nil)) {
		t.Fatal("restart order depended on input ordering")
	}
}

func TestReceptionUnknownCapacityThresholdAndCleanupAreSeparate(t *testing.T) {
	b := receptionBoard()
	if a := AdviseReception(b, nil, nil, nil); a.DispatchBudget != nil || len(a.RecommendedTasks) != 0 || a.Action.Kind != "dispatch_ready" || a.Action.Subject != "" || len(a.Action.Argv) != 0 {
		t.Fatal(a)
	}
	capacity, ceiling := 3, 1
	rows := []Reception{{AssignmentID: "result", TaskID: "TASK-A", State: "reported", Command: []string{"receive"}}}
	a := AdviseReception(b, rows, &capacity, &ceiling)
	if *a.DispatchBudget != 0 || !a.Backpressure || len(a.RecommendedTasks) != 0 {
		t.Fatal(a)
	}
	a = AdviseReception(b, nil, &capacity, &ceiling)
	if *a.DispatchBudget != 1 || len(a.RecommendedTasks) != 1 {
		t.Fatal("threshold release did not refill", a)
	}
	cleanup := []Reception{{AssignmentID: "cleanup", State: "cleanup_pending", Command: []string{"cleanup"}}}
	a = AdviseReception(b, cleanup, &capacity, &ceiling)
	if a.Cleanup != 1 || a.Unreceived != 0 || *a.DispatchBudget != 1 || len(a.RecommendedTasks) != 1 {
		t.Fatal("cleanup blocked all dispatch", a)
	}
	b.Rows = append(b.Rows, semantic.DispatchRow{Task: semantic.DispatchTask{ID: "SLOW"}, State: workstate.Active})
	a = AdviseReception(b, rows, &capacity, nil)
	if a.Action.Subject != "result" || a.Running != 1 || *a.DispatchBudget != 2 {
		t.Fatal("slow independent job blocked reception", a)
	}
}

func TestReceptionPreservedQuestionRetainsRetryAndOriginalReworkArgv(t *testing.T) {
	rework := []string{"loop-harness", "runtime", "register-workgroup", "--root", "/project", "--manifest", "<fresh-manifest>", "--task-id", "TASK-002", "--task", "docs/dev/tasks/TASK-002.md", "--revise-assignment", "assignment-build-TASK-002-g1", "--revision-intent-key", "<new-key>"}
	pendingCommand := []string{"loop-harness", "runtime", "workspace", "pending", "--root", "/project"}
	rows := []Reception{{AssignmentID: "repair", State: "preserved", Command: []string{"loop-harness", "runtime", "task-integrate", "--assignment-id", "assignment-build-TASK-002-g1", "--retry-preserved"}, ReworkCommand: rework, PendingCommand: pendingCommand, NeedsReworkReason: true, Failure: "actual failing check"}, {AssignmentID: "verified", State: "verified", Command: []string{"ack"}}, {AssignmentID: "cleanup", State: "acknowledged", Command: []string{"cleanup"}}}
	a := AdviseReception(receptionBoard(), rows, nil, nil)
	if a.Unreceived != 1 || a.Failed != 1 || a.VerifiedAwaitingAck != 1 || a.Cleanup != 1 {
		t.Fatal(a)
	}
	if a.Action.Subject != "repair" || a.Action.Question == "" || !reflect.DeepEqual(a.Action.Argv, rows[0].Command) || !reflect.DeepEqual(a.Action.ReworkCommand, rework) || !reflect.DeepEqual(a.Action.PendingCommand, pendingCommand) {
		t.Fatal("invented professional correction", a)
	}
	for _, action := range a.Pending {
		if action.Subject != "repair" && (len(action.ReworkCommand) != 0 || len(action.PendingCommand) != 0) {
			t.Fatalf("non-rework action received a synthetic route: %#v", action)
		}
	}
}
