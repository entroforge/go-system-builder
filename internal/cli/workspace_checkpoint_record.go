package cli

import (
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// These format a slot from the original validated execution or loaded current
// Assignment. Checkpoints cannot select their own revision or grant authority.
func workspaceExecutionCheckpointPath(root string, execution workspace.Execution) string {
	if execution.AssignmentRecord != nil {
		if execution.AssignmentRecord.RuntimeID != execution.RuntimeID || execution.AssignmentRecord.BaselineGeneration != execution.BaselineGeneration || execution.AssignmentRecord.AssignmentID != execution.AssignmentID {
			return ""
		}
		return assignmentrecord.IntegrationCheckpointPath(root, *execution.AssignmentRecord)
	}
	return integration.CheckpointPath(root, execution.RuntimeID, execution.BaselineGeneration, execution.AssignmentID)
}

func workspaceAssignmentCheckpointPath(root, runtimeID string, generation int, assignment hookctx.AssignmentContext) string {
	if assignment.AssignmentRecord != nil {
		if assignment.AssignmentRecord.RuntimeID != runtimeID || assignment.AssignmentRecord.BaselineGeneration != generation || assignment.AssignmentRecord.AssignmentID != assignment.AssignmentID {
			return ""
		}
		return assignmentrecord.IntegrationCheckpointPath(root, *assignment.AssignmentRecord)
	}
	return integration.CheckpointPath(root, runtimeID, generation, assignment.AssignmentID)
}
