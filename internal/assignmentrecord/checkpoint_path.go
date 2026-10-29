package assignmentrecord

import (
	"fmt"
	"path/filepath"
	"strings"
)

// IntegrationCheckpointPath formats a slot for an already resolved Record.
// It does not resolve current authority. Revision one retains its original
// path; later charter revisions keep separate Main checkpoint histories.
func IntegrationCheckpointPath(root string, ref RecordReference) string {
	validCoordinate := func(value string) bool {
		return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
	}
	if !validCoordinate(ref.RuntimeID) || !validCoordinate(ref.AssignmentID) || ref.BaselineGeneration < 1 || ref.Revision < 1 {
		return ""
	}
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	directory := filepath.Join(root, ".claude", "evidence", ref.RuntimeID, fmt.Sprintf("g%d", ref.BaselineGeneration), "worktree", ref.AssignmentID)
	if ref.Revision > 1 {
		directory = filepath.Join(directory, fmt.Sprintf("a%d", ref.Revision))
	}
	return filepath.Join(directory, "checkpoint.json")
}
