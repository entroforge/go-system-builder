package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterPlanTransactionRejectsMissingOrDriftedBaseline(t *testing.T) {
	for _, mode := range []string{"missing", "drifted"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath := writeState(t, root, baseVerificationState())
			planPath := writePlanFile(t, root)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			subject := filepath.Join(root, "internal/example/service.go")
			if mode == "missing" {
				err = os.Remove(subject)
			} else {
				err = os.WriteFile(subject, []byte("changed product bytes"), 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
			if err == nil || !strings.Contains(err.Error(), "canonical product baseline changed before ReviewPlan commit") {
				t.Fatalf("original transaction must reject %s baseline: %v", mode, err)
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rejected Plan changed Runtime: %v", err)
			}
			journal, err := os.ReadFile(journalPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(bytes.TrimSpace(journal)) != 0 {
				t.Fatal("rejected Plan committed a journal event")
			}
		})
	}
}

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
