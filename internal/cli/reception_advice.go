package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func receptionAdvice(root string, board dispatch.Board, capacity, maxUnreceived *int) (dispatch.ReceptionAdvice, error) {
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		return dispatch.ReceptionAdvice{}, err
	}
	if board.RuntimeID != loaded.PolicyContext.RuntimeID || board.Generation != loaded.BaselineGeneration || dispatch.Number(board.Revision) != loaded.PolicyContext.Revision {
		return dispatch.ReceptionAdvice{}, fmt.Errorf("runtime changed while reading reception advice; refresh s6 status")
	}
	rows, err := pendingIntegrations(root, loaded)
	if err != nil {
		return dispatch.ReceptionAdvice{}, err
	}
	latest, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return dispatch.ReceptionAdvice{}, err
	}
	baseline, _ := latest.State["baseline"].(map[string]any)
	if latest.State["runtime_id"] != board.RuntimeID || dispatch.Number(latest.State["revision"]) != dispatch.Number(board.Revision) || dispatch.Number(baseline["generation"]) != board.Generation {
		return dispatch.ReceptionAdvice{}, fmt.Errorf("runtime changed while reading reception advice; refresh s6 status")
	}
	pending := []dispatch.Reception{}
	for _, row := range rows {
		if board.Pressure != nil {
			if row.Acknowledged {
				row.State = "acknowledged"
			} else if (row.State == "cleanup_pending" || row.State == "acknowledged") && !row.CurrentReport {
				row.State = "reported"
			}
		}
		var pendingCommand []string
		if len(row.ReworkCommand) > 0 {
			pendingCommand = []string{"loop-harness", "runtime", "workspace", "pending", "--root", root}
		}
		pending = append(pending, dispatch.Reception{
			AssignmentID:      row.AssignmentID,
			TaskID:            row.TaskID,
			AgentID:           row.AgentID,
			State:             row.State,
			Failure:           row.Failure,
			Command:           row.Command,
			ReworkCommand:     append([]string(nil), row.ReworkCommand...),
			PendingCommand:    pendingCommand,
			CheckpointRef:     row.CheckpointRef,
			NeedsReworkReason: len(row.ReworkCommand) > 0,
		})
	}
	return dispatch.WithIntegrationObservation(dispatch.AdviseReception(board, pending, capacity, maxUnreceived), integration.ObserveWorkspace(root)), nil
}

func receptionAdviceText(advice dispatch.ReceptionAdvice) []string {
	a := advice.Action
	result := []string{fmt.Sprintf("S6 current Main action: %s %s — %s", a.Kind, a.Subject, a.Reason)}
	if p := advice.ConfiguredPressure; p != nil {
		result = append(result, fmt.Sprintf("Configured S6 backpressure: paused=%t; original unreceived=%d; trigger=%d; resume<=%d. New Builder registration is checked by the original Writer.", p.Paused, p.Unreceived, p.Max, p.Resume))
	}
	if len(a.Argv) > 0 {
		quoted := []string{}
		for _, arg := range a.Argv {
			quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", `'"'"'`)+"'")
		}
		result = append(result, "Original action argv: "+strings.Join(quoted, " "))
	}
	if len(a.ReworkCommand) > 0 {
		if prepareCommand := s6RevisionPrepareArgv(a.ReworkCommand); len(prepareCommand) > 0 {
			result = append(result, "Prepare this revision draft first: "+formatReceptionArgv(prepareCommand))
			result = append(result, "Replace the role, new Agent ID, new Team ID, and intent key placeholders with Main's actual choices. This writes the fixed manifest-s6-r<N>.json draft and returns the full register command to review and use; root, TASK ID/path and Assignment are carried from the original rework argv and current Record.")
		}
		result = append(result, "Original rework argv: "+formatReceptionArgv(a.ReworkCommand))
	}
	if len(a.PendingCommand) > 0 {
		result = append(result, "Pending diagnostic argv: "+formatReceptionArgv(a.PendingCommand))
	}
	if a.Question != "" {
		result = append(result, a.Question)
	}
	if observation := advice.IntegrationObservation; observation != nil && observation.LockState == "unknown" {
		result = append(result, "S6 integration observation unavailable: "+observation.Diagnostic+"; original execution locks remain authoritative.")
	}
	if advice.DispatchBudget == nil {
		result = append(result, "Dispatch budget unknown; optional --capacity advises a batch.")
	} else {
		result = append(result, fmt.Sprintf("Advisory dispatch budget: %d; original registration/dependency/conflict gates remain authoritative.", *advice.DispatchBudget))
	}
	return result
}

func formatReceptionArgv(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "'\"'\"'")+"'")
	}
	return strings.Join(quoted, " ")
}

func s6RevisionPrepareArgv(rework []string) []string {
	if len(rework) < 3 || rework[0] != "loop-harness" || rework[1] != "runtime" || rework[2] != "register-workgroup" {
		return nil
	}
	root, rootOK := argValue(rework, "--root")
	taskID, taskOK := argValue(rework, "--task-id")
	assignmentID, assignmentOK := argValue(rework, "--revise-assignment")
	intentKey, intentOK := argValue(rework, "--revision-intent-key")
	if !rootOK || !taskOK || !assignmentOK || !intentOK || root == "" || taskID == "" || assignmentID == "" || intentKey == "" {
		return nil
	}
	return []string{
		"loop-harness", "s6", "prepare",
		"--root", root,
		"--task", taskID,
		"--role", "<Builder-role>",
		"--agent-id", "<new-platform-Agent-ID>",
		"--platform-team-id", "<new-platform-Team-ID>",
		"--revise-assignment", assignmentID,
		"--revision-intent-key", intentKey,
	}
}

func argValue(argv []string, name string) (string, bool) {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == name {
			return argv[i+1], true
		}
	}
	return "", false
}
