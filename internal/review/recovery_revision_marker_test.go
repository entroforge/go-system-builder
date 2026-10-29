package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryRevisionRecordsActualInvalidationRevision(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-recovery-source", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	revisionPath := receiptRevisionPlan(t, root, accepted.State, "review-result-recovery-source", false)
	r2, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: accepted.Revision, PlanPath: revisionPath,
		SourceRef: "review-result-recovery-source", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatal(err)
	}
	r2 = markDispatchedForRevision(t, root, statePath, journalPath, r2, "assignment-qa-1", "agent-qa-1")
	qaPath := writeResultFile(t, root, mustLoadPlan(t, root, r2.State), "assignment-qa-1", "review-result-recovery-qa", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	setResultAssignmentRevision(t, qaPath, 2)
	// Cause and observe a real original drift rejection, then restore the
	// same frozen bytes. Do not hand-edit the plan status to manufacture r3.
	subjectPath := filepath.Join(root, "internal/example/service.go")
	subjectBytes, err := os.ReadFile(subjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subjectPath, append(append([]byte(nil), subjectBytes...), []byte("\ntransient source drift\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: r2.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	}); err == nil {
		t.Fatal("source drift did not reject the original pending QA answer")
	}
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateBytes, &current); err != nil {
		t.Fatal(err)
	}
	if ptr := PlanPointerFromState(current); ptr.Status != "stale" || ptr.Revision != 2 {
		t.Fatalf("actual drift did not enter original stale r2: %+v", ptr)
	}
	if err := os.WriteFile(subjectPath, subjectBytes, 0600); err != nil {
		t.Fatal(err)
	}
	revisionPath = receiptRevisionPlan(t, root, current, "review-result-recovery-source", true)
	data, err := os.ReadFile(revisionPath)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	for _, raw := range body["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["assertion"] = "recover the source-bound QA assertion after original drift restoration"
		}
	}
	writeJSONFile(t, revisionPath, body)
	r3, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: intField(current["revision"]), PlanPath: revisionPath,
		SourceRef: "review-result-recovery-source", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatal(err)
	}
	ptr := PlanPointerFromState(r3.State)
	if ptr.Revision != 3 {
		t.Fatalf("original recovery revision = %+v", ptr)
	}
	for _, raw := range r3.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == "review-result-recovery-source" {
			if entry["status"] != "invalid" || entry["invalidated_by"] != ptr.PlanID+"@r3" {
				t.Fatalf("r3 invalidation claimed a different accepted revision: %+v", entry)
			}
			return
		}
	}
	t.Fatal("original Result was removed from history")
}
