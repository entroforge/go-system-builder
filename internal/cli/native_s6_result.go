package cli

import (
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
)

// projectNativeS6Result expands only references to the author's actual Plan.
// The shared projection lives in internal/assignmentresult so that the
// original receiver and the native delivery comparison canonicalize the same
// professional answer in exactly the same way.
func projectNativeS6Result(plan, result map[string]any, doneWhenCount int) error {
	return assignmentresult.ProjectCompactResult(plan, result, doneWhenCount)
}
