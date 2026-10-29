package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Display-only cache, scoped to the exact frozen responsibility and revision.
// It neither reads private answers nor serves as an acceptance receipt.
func nativeS6DiagnosticKind(r assignmentrecord.Record) string {
	return fmt.Sprintf("S6 native Stop %s/r%d", r.AssignmentID, r.Revision)
}

func nativeS6DiagnosticMessage(root string, in policy.Input, r assignmentrecord.Record, text string, failure error) string {
	brief, _, result := assignmentrecord.NativePaths(r)
	references := []string{}
	if listing, err := workspace.Git(contextForHook(in), root, "worktree", "list", "--porcelain"); err == nil {
		for _, line := range strings.Split(listing, "\n") {
			if strings.HasPrefix(line, "worktree ") {
				candidate := strings.TrimPrefix(line, "worktree ")
				if nativeS6ExactReference(text, filepath.Join(candidate, result)) {
					references = append(references, filepath.Join(candidate, result))
				}
			}
		}
	}
	if len(references) == 0 {
		references = append(references, filepath.Join(in.CWD, result))
	}
	status := "reception failure"
	var handoff *nativeS6Handoff
	if errors.As(failure, &handoff) {
		status = "blocked handoff"
	}
	// Errors may embed full professional artifacts. Keep display storage bounded;
	// truncation never changes the Stop receiver's original error or decisions.
	detail := failure.Error()
	if len(detail) > 24000 {
		detail = string([]rune(detail)[:min(len([]rune(detail)), 6000)]) + " [diagnostic truncated]"
	}
	return fmt.Sprintf("%s; TASK %s; Assignment %s revision %d; owner %s; brief %s; Result %s; reason: %s", status, r.Task.ID, r.AssignmentID, r.Revision, r.OwnerAgentID, filepath.Join(root, brief), strings.Join(references, "; "), detail)
}

func nativeS6ReturnDiagnostic(root string, state map[string]any, r assignmentrecord.Record) string {
	if detail := submission.PlanningDiagnostic(root, state, nativeS6DiagnosticKind(r)); detail != "" {
		return detail + ". This describes the last attempted reception, not a fresh validation of edited files; no completion receipt was created. Main reads the actual native return and preserves the original worktree: resolve the named responsibility/source mismatch or source/environment blocker, or resume the same responsibility to correct the specific professional fields. Check the actual platform execution and this Stop outcome before deciding whether the Worker is continuing or Main must recover; do not infer execution state from missing reception or re-dispatch by guess."
	}
	return "No Result reception or readable original Stop diagnostic is recorded for " + r.Task.ID + ". A background launch can return before completion; Main checks the actual native execution. If it has ended, Main inspects its original return/Stop failure and preserves the worktree before recovering the same responsibility. This observation does not scan or receive private answers."
}
