package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestTaskPlanningSubmissionUsesCurrentRecordWithoutSourceReproof(t *testing.T) {
	root := t.TempDir()
	text := func(s string) *string { return &s }
	p := workpacket.TaskPlanningPacket{ID: "packet-s4", TemplateID: "s4-task-planning-v1", Binding: workpacket.PlanningBinding{RuntimeID: "loop-test", Generation: 1, StageEntry: 3, Stage: "S4", Owner: "Main"}}
	data, _ := json.Marshal(p)
	var packet map[string]any
	json.Unmarshal(data, &packet)
	state := map[string]any{"runtime_id": "loop-test", "lifecycle": map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3}, "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"work_packets": []any{packet}, "submissions": []any{}}}
	answer := workpacket.TaskPlanningAnswer{Conclusion: text("pass"), IndexPath: text("docs/dev/tasks/index-REQ-001.md"), TaskPaths: []string{"docs/dev/tasks/TASK-REQ-001-01.md", "docs/dev/tasks/TASK-REQ-001-02.md"}}
	draft, _ := json.Marshal(map[string]any{"answer": answer})
	envelope := map[string]any{"evidence_id": "E-S4", "kind": "planning_task", "runtime_id": "loop-test", "baseline_generation": 1, "subject_refs": []any{}, "conclusion": "pass", "created_at": time.Now().UTC().Format(time.RFC3339Nano), "task_planning_answer": answer}
	data, _ = json.Marshal(envelope)
	req := EvidenceRequest{ID: "E-S4", Kind: "planning_task", ProducedBy: nil, ResponsibilityID: "", Submission: &SubmissionRequest{PacketID: p.ID, Owner: "", AnswerJSON: draft, Authorize: func(string, map[string]any) error { t.Fatal("normal recording re-authorized sources"); return nil }}}
	record, err := prepareTaskPlanningSubmission(root, state, req, data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(record)
	var row map[string]any
	json.Unmarshal(encoded, &row)
	state["entities"].(map[string]any)["submissions"] = []any{row}
	winnerPath := "winner.json"
	if err := os.WriteFile(filepath.Join(root, winnerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	req.Path = "different-candidate.json"
	state["evidence"] = []any{map[string]any{"id": req.ID, "kind": req.Kind, "path": winnerPath}}
	if _, err := prepareTaskPlanningSubmission(root, state, req, data); !errors.Is(err, errSubmissionAccepted) {
		t.Fatalf("same answer not idempotent: %v", err)
	}
	answer.TaskPaths = []string{"docs/dev/tasks/TASK-REQ-001-02.md", "docs/dev/tasks/TASK-REQ-001-01.md"}
	req.Submission.AnswerJSON, _ = json.Marshal(map[string]any{"answer": answer})
	if _, err := prepareTaskPlanningSubmission(root, state, req, []byte("unused candidate")); !errors.Is(err, errSubmissionAccepted) {
		t.Fatalf("TASK order or unused candidate changed winner replay: %v", err)
	}
	for _, field := range []string{"runtime_id", "created_at", "conclusion"} {
		t.Run("bad winner "+field, func(t *testing.T) {
			broken := map[string]any{}
			for key, value := range envelope {
				broken[key] = value
			}
			broken[field] = "invalid"
			bad, err := json.Marshal(broken)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, winnerPath), bad, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := prepareTaskPlanningSubmission(root, state, req, data); err == nil || errors.Is(err, ErrSubmissionConflict) {
				t.Fatalf("invalid winner association should retain report error: %v", err)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(root, winnerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	answer.Conclusion = text("pass")
	answer.TaskPaths = []string{"docs/dev/tasks/TASK-REQ-001-03.md"}
	req.Submission.AnswerJSON, _ = json.Marshal(map[string]any{"answer": answer})
	if _, err := prepareTaskPlanningSubmission(root, state, req, data); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("changed answer did not conflict with the valid winner: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, winnerPath), []byte("broken winner"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareTaskPlanningSubmission(root, state, req, data); err == nil || errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("bad winner should report its actual error: %v", err)
	}
	if err := os.Remove(filepath.Join(root, winnerPath)); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareTaskPlanningSubmission(root, state, req, data); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing winner should retain the read error: %v", err)
	}
}
