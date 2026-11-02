package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestPendingIntegrationsSurviveRestartAndIncludeCleanup(t *testing.T) {
	root := t.TempDir()
	record := func(id string) assignmentrecord.RecordReference {
		return assignmentrecord.RecordReference{RuntimeID: "loop-pending", BaselineGeneration: 1, AssignmentID: id, Revision: 1}
	}
	reported := record("reported")
	cleanup := record("cleanup")
	complete := record("complete")
	loaded := &hookctx.LoadedContext{BaselineGeneration: 1, PolicyContext: policy.RuntimeContext{RuntimeID: "loop-pending"}, Assignments: []hookctx.AssignmentContext{
		{AssignmentID: "reported", OwnerAgentID: "a", CompletionRef: "report.json", WorktreePath: "worker", AssignmentRecord: &reported, AssignmentRevision: 1},
		{AssignmentID: "cleanup", OwnerAgentID: "b", AssignmentRecord: &cleanup, AssignmentRevision: 1},
		{AssignmentID: "complete", OwnerAgentID: "c", AssignmentRecord: &complete, AssignmentRevision: 1},
		// A legacy coordinate without the current Assignment Record is old data.
		// It must not become a pending current S6 responsibility.
		{AssignmentID: "review-only", CompletionRef: "review.json"},
	}}
	for id, ref := range map[string]assignmentrecord.RecordReference{"cleanup": cleanup, "complete": complete} {
		state := integration.StateCleanupPending
		if id == "complete" {
			state = integration.StateComplete
		}
		path := integration.CheckpointPathForRecord(root, ref)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(integration.Checkpoint{AssignmentID: id, BaselineGeneration: 1, State: state, AssignmentRecord: &ref})
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		rows, err := pendingIntegrations(root, loaded)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].AssignmentID != "cleanup" || rows[1].AssignmentID != "reported" {
			t.Fatalf("lost or fabricated pending work: %+v", rows)
		}
		if rows[0].State != integration.StateCleanupPending || rows[1].State != "reported" {
			t.Fatalf("wrong resume states: %+v", rows)
		}
	}
}
