package review

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestRegisterPlanTransactionRegistersOnce(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	planPath := writePlanFile(t, root)
	snapshot, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
	if err != nil {
		t.Fatal(err)
	}
	if ptr := PlanPointerFromState(snapshot.State); ptr == nil || ptr.Status != "running" {
		t.Fatal("original registration did not start the Plan")
	}
	if _, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath}); err == nil {
		t.Fatal("second Plan registration was accepted")
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(journal), []byte("\n")) {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event["transition_id"] == "REVIEW-PLAN" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("original registration committed %d times", count)
	}
}
