package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Select a frozen review before projecting its status. Owner alone does not
// identify an execution: named revisions retain the same professional owner.
func resolveS5HookResponsibility(root string, in policy.Input, reader *hookReader, state map[string]any) bool {
	registry, err := workspace.Decode(state)
	if err != nil || registry == nil {
		return false
	}
	var candidates []workspace.Execution
	relevant := false
	for _, e := range registry.AllExecutions() {
		if e.Purpose != workspace.DocumentReviewPurpose {
			continue
		}
		atRoot := in.CWD != "" && samePath(e.Path, in.CWD)
		if e.AgentID == reader.Owner || atRoot {
			relevant = true
		}
		if !atRoot {
			continue
		}
		candidates = append(candidates, e)
	}
	if !relevant {
		return false
	}
	issue := func(reason string) bool {
		reader.Kind = "Worker"
		reader.SelectionIssue = "S5 original execution cannot be selected: " + reason + ". Main restores the original responsibility association; ordinary tools remain available."
		return true
	}
	if len(candidates) != 1 {
		return issue("event must identify one exact registered execution root")
	}
	e := candidates[0]
	if reader.Owner != "" && reader.Owner != e.AgentID {
		return issue("actor and execution root refer to different responsibilities")
	}
	if in.SessionID != "" && e.PlatformSessionID != "" && in.SessionID != e.PlatformSessionID {
		return issue("session and execution root refer to different responsibilities")
	}
	reader.Kind, reader.Owner = "Worker", e.AgentID
	reader.Retired = e.Status == "retired"
	p, err := workspace.DocumentReviewPacket(state, e)
	if err != nil {
		return issue(err.Error())
	}
	paths, err := registry.WorkerSubmissionPaths(e)
	if err != nil {
		return issue(err.Error())
	}
	reader.References = []string{filepath.Join(e.Path, workspace.DispatchContextPath), filepath.Join(e.Path, workpacket.WorkerViewPath), filepath.Join(e.Path, paths.Answer)}
	record, received, err := runtime.FindSubmission(state, p.ID)
	if err != nil {
		return issue(err.Error())
	}
	reader.Retired = reader.Retired || received && record.SupersededBy != ""
	if received {
		reader.Receipts = []string{fmt.Sprintf("original answer %s is accepted; original control status=%s", p.ID, record.ControlStatus)}
	}
	reader.S5Context = fmt.Sprintf("S5 Reviewer %s: read local context %s, then execution view %s. Fill answer.conclusion and answer.findings in %s; save the answer for the original consumer. Main owns peer dispatch and stage followup.", e.AgentID, reader.References[0], reader.References[1], reader.References[2])
	if reader.Retired {
		reader.S5Context = fmt.Sprintf("Original S5 execution for %s is retired/superseded. Preserve its original files and receipt; it cannot deliver a new attempt. Original references: %s.", e.AgentID, strings.Join(reader.References, ", "))
	}
	if len(reader.Receipts) > 0 {
		reader.S5Context += " Recorded facts: " + strings.Join(reader.Receipts, "; ") + ". Accepted answers are not requested again."
	}
	return true
}
