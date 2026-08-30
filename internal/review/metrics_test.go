package review

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

// ---------------------------------------------------------------------------
// S7 operational metrics (L3-S7 §14.2 machine-collectible subset): the submit
// path accumulates round shape, first-pass success, per-Claim lead time,
// finding counts, first-finding -> seal duration and clean rounds.
// ---------------------------------------------------------------------------

func TestSubmitResultRecordsS7MetricsOnSealPath(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	if _, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	}); err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}

	snapMetrics, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := snapMetrics.S7ResultSubmits["accepted"]; got != 2 {
		t.Fatalf("accepted submits = %d, want 2", got)
	}
	if got := snapMetrics.S7ResultSubmits["rejected"]; got != 0 {
		t.Fatalf("rejected submits = %d, want 0", got)
	}
	if got := snapMetrics.S7Assignments["1"]; got != 2 {
		t.Fatalf("round 1 assignments = %d, want 2", got)
	}
	if got := snapMetrics.S7Claims["1"]; got != 4 {
		t.Fatalf("round 1 claims = %d, want 4", got)
	}
	if got := snapMetrics.S7PlanRevision["1"]; got != 1 {
		t.Fatalf("round 1 plan revision = %d, want 1", got)
	}
	if got := snapMetrics.S7Findings["1"]; got != 1 {
		t.Fatalf("round 1 findings = %d, want 1", got)
	}
	seal, recorded := snapMetrics.S7FirstFindingToSeal["1"]
	if !recorded || seal.Count != 1 {
		t.Fatalf("first-finding -> seal not recorded once: %+v", snapMetrics.S7FirstFindingToSeal)
	}
	for _, claimID := range []string{"claim-qa-1", "claim-qa-2", "claim-dv-1"} {
		stats, ok := snapMetrics.S7ClaimLeadTime["r1:"+claimID]
		if !ok || stats.Count != 1 {
			t.Fatalf("claim lead time for %s missing: %+v", claimID, snapMetrics.S7ClaimLeadTime)
		}
	}
	if got := snapMetrics.S7CleanRounds["1"]; got != 0 {
		t.Fatalf("clean rounds = %d, want 0 on the seal path", got)
	}
}

func TestSubmitResultRecordsS7MetricsOnCleanPath(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	if _, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	}); err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}

	snapMetrics, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := snapMetrics.S7CleanRounds["1"]; got != 1 {
		t.Fatalf("clean rounds = %d, want 1", got)
	}
	if got := snapMetrics.S7Findings["1"]; got != 0 {
		t.Fatalf("findings = %d, want 0 on the clean path", got)
	}
	if _, recorded := snapMetrics.S7FirstFindingToSeal["1"]; recorded {
		t.Fatal("first-finding -> seal must not be recorded on the clean path")
	}
}

func TestSubmitResultRecordsRejectedSubmits(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	// Exact-set violation: claim-qa-2 missing -> rejected before the CAS.
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass"}, nil)
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	}); err == nil {
		t.Fatal("expected exact-set rejection")
	}

	snapMetrics, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := snapMetrics.S7ResultSubmits["rejected"]; got != 1 {
		t.Fatalf("rejected submits = %d, want 1", got)
	}
	if got := snapMetrics.S7ResultSubmits["accepted"]; got != 0 {
		t.Fatalf("accepted submits = %d, want 0", got)
	}
	if got := snapMetrics.S7Claims["1"]; got != 0 {
		t.Fatalf("round shape must not be recorded for a rejected submit, got %d", got)
	}
}
