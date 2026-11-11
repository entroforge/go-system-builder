package assignment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalS6CompletionIgnoresHistoricalRecordMetadata(t *testing.T) {
	values := []struct {
		name  string
		value any
		omit  bool
	}{
		{name: "missing", omit: true},
		{name: "null", value: nil},
		{name: "text", value: "old"},
		{name: "number", value: 17},
		{name: "object", value: map[string]any{"legacy": true}},
	}
	for _, tc := range values {
		t.Run(tc.name, func(t *testing.T) {
			fixture, statePath, journalPath, record, sourceRef, _ := readyCanonicalResultFixture(t)
			planPath := filepath.Join(fixture.root, ".claude", "evidence", "workgroup-s6-registration-round-1", "TASK-001-01", "thin-plan-msg-s6-result-plan.json")
			planBytes, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			var plan map[string]any
			if err := json.Unmarshal(planBytes, &plan); err != nil {
				t.Fatal(err)
			}
			if tc.omit {
				delete(plan, "assignment_record_sha256")
			} else {
				plan["assignment_record_sha256"] = tc.value
			}
			planBytes, err = json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			message := canonicalResultMessage(t, fixture, record, "msg-s6-nohash-result", "The original responsibility is complete.", []string{sourceRef})
			if tc.omit {
				delete(message, "assignment_record_sha256")
			} else {
				message["assignment_record_sha256"] = tc.value
			}
			message["professional_note"] = map[string]any{"details": "The author can retain additional context."}
			messagePath := writeRegistrationMessage(t, fixture.root, "s6-nohash-result.json", message)
			original, err := os.ReadFile(messagePath)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, receipt, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: messagePath})
			if err != nil || receipt.ResultRef == "" || snapshot.Revision == 0 {
				t.Fatalf("original completion did not receive result: receipt=%+v err=%v", receipt, err)
			}
			for _, raw := range snapshot.State["evidence"].([]any) {
				row, _ := raw.(map[string]any)
				if row["kind"] == "completion_report" {
					if _, exists := row["review_round"]; exists {
						t.Fatal("S6 completion registered a review-round counter")
					}
				}
			}
			after, err := os.ReadFile(messagePath)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("author Result was modified: %v", err)
			}
			afterPlan, err := os.ReadFile(planPath)
			if err != nil || !bytes.Equal(planBytes, afterPlan) {
				t.Fatalf("accepted author Plan was modified: %v", err)
			}
			message["message_id"] = "msg-s6-other-owner"
			message["agent_id"] = "another-owner"
			badOwner := writeRegistrationMessage(t, fixture.root, "s6-other-owner.json", message)
			if _, _, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: badOwner}); err == nil {
				t.Fatal("different owner acquired the original responsibility")
			}
			message["message_id"] = "msg-s6-other-revision"
			message["agent_id"] = record.OwnerAgentID
			message["assignment_revision"] = record.Revision + 1
			badRevision := writeRegistrationMessage(t, fixture.root, "s6-other-revision.json", message)
			if _, _, err := CompleteTaskWithReceipt(fixture.root, statePath, journalPath, CompletionRequest{ExpectedRevision: -1, AgentID: record.OwnerAgentID, MessagePath: badRevision}); err == nil {
				t.Fatal("different assignment revision acquired the original responsibility")
			}
		})
	}
}
