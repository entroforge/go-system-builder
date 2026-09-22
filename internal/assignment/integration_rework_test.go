package assignment_test

import (
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/transition"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
	"testing"
)

func TestIntegrationReworkReopensReportedOwnerAndSupersedesOnlyItsTask(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	agent := map[string]any{"id": "builder", "state": "reported", "completion_reported_ref": "old-report"}
	task := map[string]any{"id": "TASK-1", "state": "review", "completion_report_ref": "old-envelope"}
	old := map[string]any{"kind": "completion_report", "path": "old-envelope", "status": "valid"}
	other := map[string]any{"kind": "completion_report", "path": "peer-envelope", "status": "valid"}
	state := map[string]any{"entities": map[string]any{"agents": []any{agent}, "tasks": []any{task}}, "evidence": []any{old, other}}
	if err = assignment.ApplyIntegrationRework(state, catalog, "builder", "TASK-1", "archive.json", "failed project check"); err != nil {
		t.Fatal(err)
	}
	if agent["state"] != "working" || task["state"] != "in_progress" || old["status"] != "superseded" || other["status"] != "valid" || agent["completion_reported_ref"] != nil {
		t.Fatalf("invalid rework mutation: %+v", state)
	}
	if err = assignment.ApplyIntegrationRework(state, catalog, "builder", "TASK-1", "archive.json", "retry"); err == nil {
		t.Fatal("rework accepted without a reported delivery")
	}
}
