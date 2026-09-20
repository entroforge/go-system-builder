package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestPendingIntegrationsSurviveRestartAndIncludeCleanup(t *testing.T) {
	root := t.TempDir()
	loaded := &hookctx.LoadedContext{BaselineGeneration: 1, PolicyContext: policy.RuntimeContext{RuntimeID: "loop-pending"}, Assignments: []hookctx.AssignmentContext{{AssignmentID: "reported", OwnerAgentID: "a", CompletionRef: "report.json", WorktreePath: "worker"}, {AssignmentID: "cleanup", OwnerAgentID: "b"}, {AssignmentID: "complete", OwnerAgentID: "c"}, {AssignmentID: "review-only", CompletionRef: "review.json"}}}
	for _, id := range []string{"cleanup", "complete"} {
		state := integration.StateCleanupPending
		if id == "complete" {
			state = integration.StateComplete
		}
		path := integration.CheckpointPath(root, "loop-pending", 1, id)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(integration.Checkpoint{AssignmentID: id, BaselineGeneration: 1, State: state})
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
