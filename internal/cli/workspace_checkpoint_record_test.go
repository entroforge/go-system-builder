package cli

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkspaceCheckpointPathUsesRecordRevisionAndRejectsConflictingTuple(t *testing.T) {
	root := t.TempDir()
	ref := assignmentrecord.RecordReference{RuntimeID: "loop-revision", BaselineGeneration: 1, AssignmentID: "assignment-revision", Revision: 2}
	e := workspace.Execution{RuntimeID: ref.RuntimeID, BaselineGeneration: ref.BaselineGeneration, AssignmentID: ref.AssignmentID, AssignmentRecord: &ref}
	old := integration.CheckpointPath(root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID)
	want := filepath.Join(filepath.Dir(old), "a2", "checkpoint.json")
	if got := workspaceExecutionCheckpointPath(root, e); got != want {
		t.Fatalf("execution revision slot: %s", got)
	}
	a := hookctx.AssignmentContext{AssignmentID: ref.AssignmentID, AssignmentRecord: &ref}
	if got := workspaceAssignmentCheckpointPath(root, ref.RuntimeID, ref.BaselineGeneration, a); got != want {
		t.Fatalf("loaded Assignment revision slot: %s", got)
	}
	e.AssignmentRecord = nil
	if got := workspaceExecutionCheckpointPath(root, e); got != old {
		t.Fatalf("legacy slot changed: %s", got)
	}
	bad := ref
	bad.AssignmentID = "assignment-other"
	e.AssignmentRecord = &bad
	if got := workspaceExecutionCheckpointPath(root, e); got != "" {
		t.Fatalf("conflicting execution tuple selects %s", got)
	}
	a.AssignmentRecord = &bad
	if got := workspaceAssignmentCheckpointPath(root, ref.RuntimeID, ref.BaselineGeneration, a); got != "" {
		t.Fatalf("conflicting loaded tuple selects %s", got)
	}
}
