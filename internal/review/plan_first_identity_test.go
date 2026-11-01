package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentMainPlanFirstReceptionOwnsMachineIdentity(t *testing.T) {
	for _, supplied := range []string{"missing", "old"} {
		t.Run(supplied, func(t *testing.T) {
			root := t.TempDir()
			state := baseVerificationState()
			entryID := "review-plan-current-entry"
			state["review"].(map[string]any)["round_entry"] = map[string]any{"transition_id": "TR-006", "plan_id": entryID, "baseline_generation": 1, "change_impact_ref": nil}
			statePath, journalPath := writeState(t, root, state)
			fixture := writePlanFile(t, root)
			var answer map[string]any
			if err := json.Unmarshal(mustReadTestFile(t, fixture), &answer); err != nil {
				t.Fatal(err)
			}
			answer["schema_version"] = "1.1.0"
			delete(answer, "review_round")
			if supplied == "missing" {
				delete(answer, "review_plan_id")
			} else {
				answer["review_plan_id"] = "review-plan-old-entry"
			}
			path := PlanQuestionnairePath(root, state)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			write := func() []byte {
				t.Helper()
				data, err := json.Marshal(answer)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				return data
			}
			original := write()
			snapshot, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
			if err != nil || outcome != PlanReceptionAccepted {
				t.Fatalf("first receive: %s %v", outcome, err)
			}
			plan, ptr, err := LoadPlan(root, snapshot.State)
			if err != nil || ptr.PlanID != entryID || plan.Claims[0].Assertion != "REQ covered" {
				t.Fatalf("accepted facts: %+v %+v %v", plan, ptr, err)
			}
			if actual := mustReadTestFile(t, path); !bytes.Equal(actual, original) {
				t.Fatal("reception changed the original answer")
			}
			same, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
			if err != nil || outcome != PlanReceptionSame || same.Revision != snapshot.Revision {
				t.Fatalf("same answer: %s %v", outcome, err)
			}
			answer["claims"].([]any)[0].(map[string]any)["assertion"] = "later actual professional judgment"
			revisedOriginal := write()
			revised, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
			if err != nil || outcome != PlanReceptionRevised {
				t.Fatalf("revision: %s %v", outcome, err)
			}
			plan, ptr, err = LoadPlan(root, revised.State)
			if err != nil || ptr.PlanID != entryID || plan.Claims[0].Assertion != "later actual professional judgment" {
				t.Fatalf("revised facts: %+v %+v %v", plan, ptr, err)
			}
			if actual := mustReadTestFile(t, path); !bytes.Equal(actual, revisedOriginal) {
				t.Fatal("revision changed the original answer")
			}
		})
	}
}

func TestOtherPlanPathDoesNotAcquireCurrentEntryIdentity(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["review"].(map[string]any)["round_entry"] = map[string]any{"transition_id": "TR-006", "plan_id": "review-plan-current-entry", "baseline_generation": 1}
	statePath, journalPath := writeState(t, root, state)
	path := writePlanFile(t, root)
	if _, _, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path}); err == nil {
		t.Fatal("another plan path was rebound to the current entry")
	}
}

func TestCurrentMainRepairSeedRequiresItsActualEntryAssociation(t *testing.T) {
	for _, associated := range []bool{true, false} {
		t.Run(map[bool]string{true: "associated", false: "different-entry"}[associated], func(t *testing.T) {
			root, statePath, journalPath, professionalPath, _ := s9SeedRegistrationFixture(t)
			var state map[string]any
			if err := json.Unmarshal(mustReadTestFile(t, statePath), &state); err != nil {
				t.Fatal(err)
			}
			review := state["review"].(map[string]any)
			ptr := review["plan"].(map[string]any)
			entry := review["round_entry"].(map[string]any)
			entry["plan_id"] = ptr["plan_id"]
			if !associated {
				entry["plan_id"] = "review-plan-other-entry"
			}
			stateBytes, _ := json.Marshal(state)
			if err := os.WriteFile(statePath, stateBytes, 0600); err != nil {
				t.Fatal(err)
			}
			var answer map[string]any
			if err := json.Unmarshal(mustReadTestFile(t, professionalPath), &answer); err != nil {
				t.Fatal(err)
			}
			answer["review_plan_id"] = "review-plan-stale-copy"
			data, _ := json.Marshal(answer)
			path := PlanQuestionnairePath(root, state)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			snapshot, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
			if !associated {
				if err == nil {
					t.Fatal("unassociated seed was rebound")
				}
				return
			}
			if err != nil || outcome != PlanReceptionAccepted {
				t.Fatalf("seed reception: %s %v", outcome, err)
			}
			accepted := PlanPointerFromState(snapshot.State)
			if accepted == nil || accepted.PlanID != ptr["plan_id"] {
				t.Fatalf("seed identity lost: %+v", accepted)
			}
			if actual := mustReadTestFile(t, path); !bytes.Equal(actual, data) {
				t.Fatal("seed reception changed the original answer")
			}
		})
	}
}
