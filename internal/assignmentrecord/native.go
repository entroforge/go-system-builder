package assignmentrecord

import "fmt"

// NativePaths names professional artifacts, not platform execution state.
func NativePaths(r Record) (brief, plan, result string) {
	suffix := fmt.Sprintf("%s/r%d", r.AssignmentID, r.Revision)
	return ".claude/assignments/native/" + suffix + "/task.md", ".claude/assignments/native/" + suffix + "/plan.json", ".claude/assignments/native/" + suffix + "/result.json"
}
