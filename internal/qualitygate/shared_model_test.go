package qualitygate

import "testing"

func TestSharedModelSubjectDoesNotSatisfyArchitecture(t *testing.T) {
	data := []byte(`{"type":"object"}`)
	doc := documentFact{ID: "shared-model:api/model.json", Kind: "design", Path: "api/model.json", Status: "locked", SHA256: sha256Hex(data)}
	files := memoryFileView{"api/model.json": data}
	if _, ok := findCurrentDocument([]documentFact{doc}, "design", files); ok {
		t.Fatal("schema substituted for architecture")
	}
	state := map[string]any{"documents": []any{map[string]any{"id": doc.ID, "kind": "design", "path": doc.Path, "sha256": doc.SHA256, "status": "locked", "generation": 1}}}
	docs := currentDocuments(state, 1)
	if len(docs) != 1 || docs[0].ID != doc.ID {
		t.Fatal("shared input lost from S5 subjects")
	}
}

func TestDispatchPlanIsReviewedButNotATaskOrArchitecture(t *testing.T) {
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "documents": []any{
		map[string]any{"id": "TASK-042-01", "kind": "task", "path": "task.md", "sha256": "a", "version": "1", "generation": 1},
		map[string]any{"id": "dispatch-plan:REQ-042", "kind": "dispatch_plan", "path": "plan.md", "sha256": "b", "version": "1", "generation": 1},
	}}
	docs := currentDocuments(state, 1)
	if len(docs) != 2 {
		t.Fatal(docs)
	}
	if exactSubjects([]subjectRef{{Path: "task.md", SHA256: "a", Version: "1"}}, docs) {
		t.Fatal("S5 accepted missing plan subject")
	}
	if got := executionBatchTasks(state); len(got) != 1 || got[0] != "TASK-042-01" {
		t.Fatal(got)
	}
	if _, ok := findCurrentDocument(docs, "design", nil); ok {
		t.Fatal("plan substituted for architecture")
	}
}
