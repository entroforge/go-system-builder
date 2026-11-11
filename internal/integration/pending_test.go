package integration

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestPendingAcknowledgementUsesCurrentReportAndActualAck(t *testing.T) {
	cp := Checkpoint{State: StateVerified, CompletionReportPath: "current/report.json"}
	a := hookctx.AssignmentContext{CompletionAckRef: "original/ack.json", CompletionRef: "old/report.json"}
	if acknowledgedReport(a, cp) {
		t.Fatal("another report must not be acknowledged")
	}
	a.CompletionRef = cp.CompletionReportPath
	if !acknowledgedReport(a, cp) {
		t.Fatal("current report and actual ACK were not accepted")
	}
	cp.State = StatePreserved
	if acknowledgedReport(a, cp) {
		t.Fatal("ACK hid a failed integration")
	}
	cp.State = StateVerified
	a.CompletionAckRef = ""
	if acknowledgedReport(a, cp) {
		t.Fatal("missing ACK was accepted")
	}
}

func TestPendingCheckpointPathUsesAlreadyResolvedCurrentRecord(t *testing.T) {
	ref := assignmentrecord.RecordReference{
		RuntimeID: "loop-pending", BaselineGeneration: 4,
		AssignmentID: "assignment-pending", Revision: 2,
	}
	loaded := &hookctx.LoadedContext{
		BaselineGeneration: 4,
		PolicyContext:      policy.RuntimeContext{RuntimeID: "loop-pending"},
	}
	assignment := hookctx.AssignmentContext{
		AssignmentID: "assignment-pending", AssignmentRecord: &ref,
		AssignmentRevision: 2,
	}
	want := CheckpointPathForRecord("/repo", ref)
	got, err := pendingCheckpointPath("/repo", loaded, assignment)
	if err != nil || got != want || got == CheckpointPath("/repo", "loop-pending", 4, "assignment-pending") {
		t.Fatalf("canonical pending path = %q, err %v; want revision slot %q", got, err, want)
	}
	assignment.AssignmentRecord = nil
	if _, err := pendingCheckpointPath("/repo", loaded, assignment); err == nil {
		t.Fatal("revision without a Record must not fall back to the legacy r1 checkpoint path")
	}
	wrongRef := ref
	wrongRef.RuntimeID = "another-runtime"
	assignment.AssignmentRecord = &wrongRef
	if _, err := pendingCheckpointPath("/repo", loaded, assignment); err == nil {
		t.Fatal("Record from another Runtime must not use this pending slot")
	}
}

func TestPendingRejectsCheckpointFromAnotherRecordRevision(t *testing.T) {
	current := hookctx.AssignmentContext{
		AssignmentID: "assignment-pending", AssignmentRevision: 2,
		AssignmentRecord: &assignmentrecord.RecordReference{
			RuntimeID: "loop-pending", BaselineGeneration: 4, AssignmentID: "assignment-pending", Revision: 2,
		},
	}
	oldRef := assignmentrecord.RecordReference{
		RuntimeID: "loop-pending", BaselineGeneration: 4, AssignmentID: "assignment-pending", Revision: 1,
	}
	old := Checkpoint{AssignmentRecord: &oldRef}
	if err := validatePendingCheckpointRecordBinding(old, current); err == nil {
		t.Fatal("pending accepted an early checkpoint copied from the predecessor revision")
	}
	old.CanonicalMainFacts = &assignmentresult.MainFacts{Binding: assignmentresult.ResultBinding{
		AssignmentRecord: oldRef,
	}}
	if err := validatePendingCheckpointRecordBinding(old, current); err == nil {
		t.Fatal("pending accepted predecessor Main facts under the current revision path")
	}
}
