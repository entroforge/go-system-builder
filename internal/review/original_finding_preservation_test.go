package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIssuedFindingPreservesOriginalProfessionalFieldsAndReplay(t *testing.T) {
	root, statePath, journalPath, packet, request := issuedClaimStopFixture(t)
	answer := claimStopAnswer(t, "P1")
	answer["subject_digest"] = map[string]any{"retired": true}
	answer["verification_artifact_digest"] = 7
	finding := answer["findings"].([]any)[0].(map[string]any)
	finding["reproducibility"] = "not_reproducible"
	finding["author_context"] = map[string]any{"source": "actual observation: sha256 is product text", "uncertainty": true, "subject_digest": "business detail", "hash": map[string]any{"sha256": "user example"}}
	encounter := finding["encounter"].(map[string]any)
	encounter["last_good"] = "last actual state"
	encounter["wall"] = "observed action"
	encounter["first_bad"] = "not reproducible"
	encounter["timeline"] = []any{map[string]any{"sequence": 1, "action": "inspect", "observed_checkpoint": "unchanged", "author_note": "original extra detail"}}
	// Normalize JSON numbers as the actual authoring file does.
	encodedFinding, err := json.Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encodedFinding, &finding); err != nil {
		t.Fatal(err)
	}
	encounter = finding["encounter"].(map[string]any)
	original := saveClaimStopAnswer(t, root, packet, answer)

	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("receive original Finding: %v", err)
	}
	rows := accepted.State["entities"].(map[string]any)["findings"].([]any)
	if len(rows) != 1 {
		t.Fatalf("Finding rows: %v", rows)
	}
	row := rows[0].(map[string]any)
	if row["assignment_id"] != packet.Binding.AssignmentID || row["original_finder"] != packet.Binding.OwnerAgentID || row["claim_id"] != finding["claim_id"] {
		t.Fatalf("wrong original responsibility: %v", row)
	}
	data, err := os.ReadFile(filepath.Join(root, row["path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reproducibility", "author_context", "encounter"} {
		if !reflect.DeepEqual(saved[key], finding[key]) {
			t.Fatalf("Finding %s changed: got %#v; want %#v", key, saved[key], finding[key])
		}
	}
	assignment := resultPacketAssignmentRow(accepted.State, packet.Binding.AssignmentID)
	receipt := assignment["result_receipt"].(map[string]any)
	data, err = os.ReadFile(filepath.Join(root, receipt["result_ref"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Effective struct {
			Findings []map[string]any `json:"findings"`
		} `json:"effective_answer"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Effective.Findings) != 1 || !reflect.DeepEqual(result.Effective.Findings[0]["encounter"], encounter) {
		t.Fatalf("effective answer lost original Finding: %s", data)
	}
	if !reflect.DeepEqual(result.Effective.Findings[0]["author_context"], finding["author_context"]) {
		t.Fatalf("effective answer changed professional hash content: %s", data)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var professional map[string]json.RawMessage
	if err := json.Unmarshal(envelope["effective_answer"], &professional); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"subject_digest", "verification_artifact_digest"} {
		if _, exists := professional[key]; exists {
			t.Fatalf("new result retained top-level machine field %s", key)
		}
	}
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	replay, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil || replay.Revision != accepted.Revision {
		t.Fatalf("replay: revision=%d, err=%v", replay.Revision, err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(original, mustReadTestFile(t, filepath.Join(root, packet.AnswerPath))) {
		t.Fatal("replay changed accepted facts or original answer")
	}
}
