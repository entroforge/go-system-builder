package cli

import (
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// Recovery follows the durable integration receipt, never the generic blocked
// verdict: Inspect can fail before any integration checkpoint has been written.
func s6IntegrationRecoveryGuidance(root string, loaded *hookctx.LoadedContext, assignment hookctx.AssignmentContext, guidance policy.Guidance) (policy.Guidance, string, error) {
	path, err := checkpointPathForLoadedAssignment(root, loaded, assignment)
	if err != nil {
		return guidance, "blocked", err
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(path)
	if err != nil {
		return guidance, "blocked", err
	}
	state := "blocked"
	command := fmt.Sprintf("loop-harness runtime task-integrate --root %s --assignment-id %s --agent-id %s", s6RecoveryShellQuote(root), s6RecoveryShellQuote(assignment.AssignmentID), s6RecoveryShellQuote(assignment.OwnerAgentID))
	if found {
		state = cp.State
		guidance.Integration = appendUniqueStrings(guidance.Integration, "checkpoint_state="+cp.State)
		if cp.State == integration.StatePreserved || cp.State == integration.StateBlocked || cp.State == integration.StateMerged {
			command += " --retry-preserved"
		}
	} else {
		guidance.Integration = appendUniqueStrings(guidance.Integration, "checkpoint_state=absent")
	}
	if !strings.Contains(guidance.Action, command) {
		if !found {
			guidance.Action += ". No integration checkpoint exists."
		}
		guidance.Action += " After resolving the reported cause, run `" + command + "`."
	}
	guidance.Instruction = formatGuidanceInstruction(guidance)
	return guidance, state, nil
}

func s6RecoveryShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
