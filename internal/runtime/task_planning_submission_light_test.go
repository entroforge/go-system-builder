package runtime

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestTaskPlanningSubmissionUsesCurrentRecordWithoutSourceReproof(t *testing.T) {
	text := func(s string) *string { return &s }
	p := workpacket.TaskPlanningPacket{ID: "packet-s4", TemplateID: "s4-task-planning-v1", Binding: workpacket.PlanningBinding{RuntimeID: "loop-test", Generation: 1, StageEntry: 3, Stage: "S4", Owner: "Main"}}
	data, _ := json.Marshal(p)
	var packet map[string]any
	json.Unmarshal(data, &packet)
	state := map[string]any{"runtime_id": "loop-test", "lifecycle": map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3}, "baseline": map[string]any{"generation": 1}, "entities": map[string]any{"work_packets": []any{packet}, "submissions": []any{}}}
	answer := workpacket.TaskPlanningAnswer{Conclusion: text("pass"), IndexPath: text("docs/dev/tasks/index-REQ-001.md"), TaskPaths: []string{"docs/dev/tasks/TASK-REQ-001-01.md"}}
	draft, _ := json.Marshal(map[string]any{"answer": answer})
	envelope := map[string]any{"evidence_id": "E-S4", "kind": "planning_task", "runtime_id": "loop-test", "baseline_generation": 1, "subject_refs": []any{}, "conclusion": "pass", "created_at": time.Now().UTC().Format(time.RFC3339Nano), "task_planning_answer": answer}
	data, _ = json.Marshal(envelope)
	req := EvidenceRequest{ID: "E-S4", Kind: "planning_task", ProducedBy: nil, ResponsibilityID: "", Submission: &SubmissionRequest{PacketID: p.ID, Owner: "", AnswerJSON: draft, Authorize: func(string, map[string]any) error { t.Fatal("normal recording re-authorized sources"); return nil }}}
	record, err := prepareTaskPlanningSubmission(t.TempDir(), state, req, data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(record)
	var row map[string]any
	json.Unmarshal(encoded, &row)
	state["entities"].(map[string]any)["submissions"] = []any{row}
	state["evidence"] = []any{map[string]any{"id": req.ID, "kind": req.Kind, "path": req.Path, "sha256": workpacket.Digest(data)}}
	if _, err := prepareTaskPlanningSubmission(t.TempDir(), state, req, data); !errors.Is(err, errSubmissionAccepted) {
		t.Fatalf("same answer not idempotent: %v", err)
	}
	answer.Conclusion = text("pass")
	answer.TaskPaths = []string{"docs/dev/tasks/TASK-REQ-001-02.md"}
	req.Submission.AnswerJSON, _ = json.Marshal(map[string]any{"answer": answer})
	if _, err := prepareTaskPlanningSubmission(t.TempDir(), state, req, data); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("changed answer did not conflict: %v", err)
	}
}
