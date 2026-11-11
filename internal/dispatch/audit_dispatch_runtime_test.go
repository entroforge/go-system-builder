package dispatch

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// These tests exercise the S6 projection through Board.Load, rather than
// calling the platform adapter.  A dispatch projection is read-only: the
// fixtures below mutate only the in-memory Runtime snapshot and the evidence
// files that the projection is meant to consume.

func TestAuditDispatchCapacitySelectsIndependentTasks(t *testing.T) {
	root, state := boardFixture(t)

	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := board.Next, []string{"TASK-042-01", "TASK-042-02"}; !sameStrings(got, want) {
		t.Fatalf("capacity two selected %v, want %v", got, want)
	}

}

func TestAuditDispatchConflictRecoveryAndLegacyProjection(t *testing.T) {
	plan := &semantic.DispatchPlan{Tasks: []semantic.DispatchTask{
		{ID: "TASK-A", Wave: 1, Writes: []string{"shared/output"}},
		{ID: "TASK-B", Wave: 1, Writes: []string{"shared/output"}},
	}}
	rows, next := semantic.NextDispatch(plan, map[string]semantic.DispatchFact{
		"TASK-A": {State: workstate.Active, Reason: "active"},
	}, 1)
	if len(next) != 0 || auditDispatchSemanticRow(rows, "TASK-B").State != workstate.Pending || auditDispatchSemanticRow(rows, "TASK-B").Eligible || auditDispatchSemanticRow(rows, "TASK-B").Dispatchable {
		t.Fatalf("active write conflict was released: rows=%+v next=%v", rows, next)
	}
	rows, next = semantic.NextDispatch(plan, map[string]semantic.DispatchFact{
		"TASK-A": {State: workstate.Done, Reason: "verified", Integrated: true, DeliveryRecorded: true},
	}, 1)
	if !sameStrings(next, []string{"TASK-B"}) {
		t.Fatalf("conflict recovery did not release TASK-B: rows=%+v next=%v", rows, next)
	}
	assertAuditDispatchFacts(t, auditDispatchSemanticRow(rows, "TASK-A"), workstate.Done, true, true, false, false)
	assertAuditDispatchFacts(t, auditDispatchSemanticRow(rows, "TASK-B"), workstate.Pending, false, false, true, true)

	root, state := boardFixture(t)
	docs := state["documents"].([]any)
	kept := make([]any, 0, len(docs))
	for _, raw := range docs {
		doc := raw.(map[string]any)
		if doc["kind"] != "dispatch_plan" {
			kept = append(kept, raw)
		}
	}
	state["documents"] = kept
	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !board.Legacy || len(board.Next) != 0 {
		t.Fatalf("legacy state was not recoverable read-only projection: %+v", board)
	}
}

func assertAuditDispatchFacts(t *testing.T, row semantic.DispatchRow, state workstate.Status, integrated, deliveryRecorded, eligible, dispatchable bool) {
	t.Helper()
	if row.State != state || row.Integrated != integrated || row.DeliveryRecorded != deliveryRecorded || row.Eligible != eligible || row.Dispatchable != dispatchable {
		t.Fatalf("dispatch row facts = %+v, want state=%s integrated=%t delivery_recorded=%t eligible=%t dispatchable=%t", row, state, integrated, deliveryRecorded, eligible, dispatchable)
	}
}

func auditDispatchSemanticRow(rows []semantic.DispatchRow, taskID string) semantic.DispatchRow {
	for _, row := range rows {
		if row.Task.ID == taskID {
			return row
		}
	}
	return semantic.DispatchRow{Task: semantic.DispatchTask{ID: taskID}, State: "missing"}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
