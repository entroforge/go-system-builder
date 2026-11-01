package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS7ResultReceptionUsesActualAnswerWithoutDigests(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-nohash", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(original, &document); err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"subject_digest", "verification_artifact_digest", "answer_sha256", "effective_answer_sha256", "review_plan_sha256", "assignment_sha256", "result_sha256", "packet_sha256"} {
		document[key] = []any{nil, 7, "old", map[string]any{"old": true}}[i%4]
	}
	original, _ = json.Marshal(document)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	req := SubmitRequest{ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: path}
	accepted, err := SubmitResult(root, statePath, journalPath, req)
	if err != nil {
		t.Fatal(err)
	}
	row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	receipt := row["result_receipt"].(map[string]any)
	for _, key := range []string{"answer_sha256", "effective_answer_sha256", "review_plan_sha256", "assignment_sha256", "result_sha256", "packet_sha256", "subject_digest", "verification_artifact_digest"} {
		if _, exists := receipt[key]; exists {
			t.Fatalf("new receipt registered %s", key)
		}
	}
	artifact, err := os.ReadFile(filepath.Join(root, receipt["result_ref"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(artifact, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"answer_sha256", "effective_answer_sha256", "result_sha256", "packet_sha256", "subject_digest", "verification_artifact_digest", "submitted_answer"} {
		if _, exists := envelope[key]; exists {
			t.Fatalf("new result registered %s", key)
		}
	}
	if len(envelope["effective_answer"]) == 0 {
		t.Fatal("original professional content not saved")
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(envelope["effective_answer"], &saved); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"subject_digest", "verification_artifact_digest", "answer_sha256", "effective_answer_sha256", "review_plan_sha256", "assignment_sha256", "result_sha256", "packet_sha256"} {
		if _, exists := saved[key]; exists {
			t.Fatalf("saved professional result registered %s", key)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatalf("authoring file changed: %v", err)
	}
	again, err := SubmitResult(root, statePath, journalPath, req)
	if err != nil || again.Revision != accepted.Revision {
		t.Fatalf("replay: rev=%d err=%v", again.Revision, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	answer["claim_results"].([]any)[0].(map[string]any)["observed"] = "a new professional observation"
	data, _ = json.Marshal(answer)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitResult(root, statePath, journalPath, req); err == nil {
		t.Fatal("different content incorrectly replayed as same answer")
	}
}

func TestS7RevisionPathSourceDoesNotRequireDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("actual review context"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateRevisionSource(root, map[string]any{}, "path:source.txt", 1, 1); err != nil {
		t.Fatalf("real source requires no machine digest: %v", err)
	}
	// A legacy reference is accepted on its actual path; its old value is not checked.
	if err := validateRevisionSource(root, map[string]any{}, "path:source.txt#sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 1, 1); err != nil {
		t.Fatalf("legacy source path was re-authenticated: %v", err)
	}
}

func TestPlanObservationPreservesOriginalProfessionalContent(t *testing.T) {
	input := []byte(`{"review_plan_id":"review-plan-a","claims":[{"assertion":"The user may discuss SHA as ordinary text"}]}`)
	data, err := PlanObservationSnapshot(input)
	if err != nil || string(data) != string(input) {
		t.Fatalf("original observation changed: %s %v", data, err)
	}
}

func TestS7LongPlanQuestionnaireKeepsUsableComponents(t *testing.T) {
	state := map[string]any{"runtime_id": "runtime-" + strings.Repeat("r", 300), "baseline": map[string]any{"generation": 1}, "review": map[string]any{"round_entry": map[string]any{"plan_id": "review-plan-" + strings.Repeat("p", 300)}}}
	path := PlanQuestionnairePath(t.TempDir(), state)
	if path == "" {
		t.Fatal("long existing responsibility was rejected")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if len(part) > 255 {
			t.Fatalf("oversize path component %d", len(part))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
}
