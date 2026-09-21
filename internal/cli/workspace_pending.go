package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

type pendingIntegration struct {
	AssignmentID  string   `json:"assignment_id"`
	AgentID       string   `json:"agent_id"`
	State         string   `json:"state"`
	Failure       string   `json:"failure,omitempty"`
	Command       []string `json:"command_argv"`
	ReworkCommand []string `json:"rework_command_argv,omitempty"`
}

// Pending work is derived from durable completion reports and checkpoints;
// it never introduces a second queue whose authority could drift from Runtime.
func runWorkspacePending(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime workspace pending", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "Main control root")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	loaded, err := hookctx.LoadFull(*root, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rows, err := pendingIntegrations(*root, loaded)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(rows); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func pendingIntegrations(root string, loaded *hookctx.LoadedContext) ([]pendingIntegration, error) {
	rows := []pendingIntegration{}
	for _, a := range loaded.Assignments {
		if b := loaded.PolicyContext.Workspace; b != nil {
			if e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration); ok && e.Status == "preparing" && e.ReworkRef != "" {
				rows = append(rows, pendingIntegration{AssignmentID: a.AssignmentID, AgentID: a.OwnerAgentID, State: "rework_pending", Command: []string{"loop-harness", "runtime", "workspace", "rework", "--root", root, "--assignment", a.AssignmentID, "--agent", a.OwnerAgentID, "--reason", "resume recorded rework"}})
				continue
			}
		}
		cp, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(root, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration, a.AssignmentID))
		if err != nil {
			return nil, err
		}
		if found && (cp.AssignmentID != a.AssignmentID || cp.BaselineGeneration != loaded.BaselineGeneration) {
			return nil, fmt.Errorf("checkpoint identity differs from pending assignment %s", a.AssignmentID)
		}
		if found && cp.State == integration.StateComplete {
			projected := true
			if b := loaded.PolicyContext.Workspace; b != nil {
				e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration)
				projected = ok && e.Status == "complete"
			}
			currentReport := true
			if cp.CompletionReportSHA256 != "" {
				refreshed, err := integration.RefreshCompletionBinding(root, loaded.PolicyContext.RuntimeID, a.CompletionRef, integration.InspectionFromCheckpoint(cp))
				currentReport = err == nil && refreshed.CompletionReportSHA256 == cp.CompletionReportSHA256 && refreshed.CompletionReportPath == cp.CompletionReportPath
			}
			if projected && currentReport {
				continue
			}
		}
		if !found && (a.CompletionRef == "" || a.WorktreePath == "") {
			continue
		}
		state := cp.State
		if !found {
			state = "reported"
		}
		row := pendingIntegration{AssignmentID: a.AssignmentID, AgentID: a.OwnerAgentID, State: state, Failure: cp.FailureReason, Command: []string{"loop-harness", "runtime", "task-integrate", "--root", root, "--assignment-id", a.AssignmentID, "--agent-id", a.OwnerAgentID}}
		if b := loaded.PolicyContext.Workspace; b != nil {
			if e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration); ok && e.DeliveryRef != "" {
				row.Command = []string{"loop-harness", "runtime", "workspace", "integrate", "--root", root, "--assignment", a.AssignmentID, "--agent", a.OwnerAgentID}
			}
		}

		if found && (cp.State == integration.StatePreserved || cp.State == integration.StateBlocked) {
			row.Command = append(row.Command, "--retry-preserved")
			if loaded.PolicyContext.Workspace != nil && cp.TestedHead == "" {
				row.ReworkCommand = []string{"loop-harness", "runtime", "workspace", "rework", "--root", root, "--assignment", a.AssignmentID, "--agent", a.OwnerAgentID, "--reason", "<describe required code correction>"}
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AssignmentID < rows[j].AssignmentID })
	return rows, nil
}
