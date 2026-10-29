package integration

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestPendingAcknowledgementRequiresExactCurrentReportBinding(t *testing.T) {
	cp := Checkpoint{State: StateVerified, TestedHead: "original-tested-head", CompletionReportPath: "current/report.json", CompletionReportSHA256: "current-sha"}
	a := hookctx.AssignmentContext{CompletionAckRef: "original/ack.json", AcknowledgedReports: map[string]string{"old/report.json": "old-sha"}}
	if acknowledgedReport(a, cp) {
		t.Fatal("old ACK released another report")
	}
	a.AcknowledgedReports[cp.CompletionReportPath] = "old-sha"
	if acknowledgedReport(a, cp) {
		t.Fatal("old ACK released changed bytes")
	}
	a.AcknowledgedReports[cp.CompletionReportPath] = cp.CompletionReportSHA256
	if !acknowledgedReport(a, cp) {
		t.Fatal("original matching ACK binding was ignored")
	}
	cp.State = StatePreserved
	if acknowledgedReport(a, cp) {
		t.Fatal("old ACK hid a current failed integration")
	}
	cp.State = StateVerified
	a.CompletionAckRef = ""
	if acknowledgedReport(a, cp) {
		t.Fatal("digest alone manufactured original ACK")
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
		AssignmentRecordSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		AssignmentRevision:     2,
	}
	want := CheckpointPathForRecord("/repo", ref)
	got, err := pendingCheckpointPath("/repo", loaded, assignment)
	if err != nil || got != want || got == CheckpointPath("/repo", "loop-pending", 4, "assignment-pending") {
		t.Fatalf("canonical pending path = %q, err %v; want revision slot %q", got, err, want)
	}
	assignment.AssignmentRecordSHA256 = ""
	if _, err := pendingCheckpointPath("/repo", loaded, assignment); err == nil {
		t.Fatal("incomplete canonical projection must not fall back to the legacy r1 checkpoint path")
	}
	assignment = hookctx.AssignmentContext{AssignmentID: "legacy-assignment"}
	loaded.BaselineGeneration = 1
	if got, err := pendingCheckpointPath("/repo", loaded, assignment); err != nil || got != CheckpointPath("/repo", "loop-pending", 1, "legacy-assignment") {
		t.Fatalf("legacy pending path = %q, err %v", got, err)
	}
}

func TestPendingRejectsCheckpointFromAnotherRecordRevision(t *testing.T) {
	current := hookctx.AssignmentContext{
		AssignmentID: "assignment-pending", AssignmentRevision: 2,
		AssignmentRecord: &assignmentrecord.RecordReference{
			RuntimeID: "loop-pending", BaselineGeneration: 4, AssignmentID: "assignment-pending", Revision: 2,
		},
		AssignmentRecordSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
	}
	oldRef := assignmentrecord.RecordReference{
		RuntimeID: "loop-pending", BaselineGeneration: 4, AssignmentID: "assignment-pending", Revision: 1,
	}
	old := Checkpoint{AssignmentRecord: &oldRef, AssignmentRecordSHA256: "1111111111111111111111111111111111111111111111111111111111111111"}
	if err := validatePendingCheckpointRecordBinding(old, current); err == nil {
		t.Fatal("pending accepted an early checkpoint copied from the predecessor revision")
	}
	old.CanonicalMainFacts = &assignmentresult.MainFacts{Binding: assignmentresult.ResultBinding{
		AssignmentRecord: oldRef, AssignmentRecordSHA256: old.AssignmentRecordSHA256,
	}}
	if err := validatePendingCheckpointRecordBinding(old, current); err == nil {
		t.Fatal("pending accepted predecessor Main facts under the current revision path")
	}
}
