package assignmentrecord

import (
	"path/filepath"
	"testing"
)

func TestIntegrationCheckpointPathRetainsFirstSlotAndSeparatesRevisions(t *testing.T) {
	root := t.TempDir()
	ref := RecordReference{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment-test", Revision: 1}
	first := filepath.Join(root, ".claude/evidence/loop-test/g1/worktree/assignment-test/checkpoint.json")
	if got := IntegrationCheckpointPath(root, ref); got != first {
		t.Fatalf("r1 slot = %s", got)
	}
	ref.Revision = 2
	if got := IntegrationCheckpointPath(root, ref); got != filepath.Join(filepath.Dir(first), "a2/checkpoint.json") {
		t.Fatalf("r2 slot = %s", got)
	}
	for _, invalid := range []RecordReference{
		{RuntimeID: "../foreign", BaselineGeneration: 1, AssignmentID: "assignment-test", Revision: 2},
		{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "../foreign", Revision: 2},
		{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment-test", Revision: 0},
	} {
		if got := IntegrationCheckpointPath(root, invalid); got != "" {
			t.Fatalf("invalid reference obtained slot %s", got)
		}
	}
}
