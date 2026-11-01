package integration

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

type PendingIntegration struct {
	CurrentReport bool     `json:"-"`
	Acknowledged  bool     `json:"-"`
	TaskID        string   `json:"task_id,omitempty"`
	CheckpointRef string   `json:"checkpoint_ref,omitempty"`
	AssignmentID  string   `json:"assignment_id"`
	AgentID       string   `json:"agent_id"`
	State         string   `json:"state"`
	Failure       string   `json:"failure,omitempty"`
	Command       []string `json:"command_argv"`
	ReworkCommand []string `json:"rework_command_argv,omitempty"`
}

// Pending projects original reports and checkpoints; it does not maintain a queue.
func Pending(root string, loaded *hookctx.LoadedContext) ([]PendingIntegration, error) {
	rows := []PendingIntegration{}
	for _, a := range loaded.Assignments {
		// S6 uses the current assignment Record. Legacy workspace executions
		// and S9 delivery candidates do not create pending S6 responsibilities.
		if !canonicalAssignmentProjection(a) {
			continue
		}
		checkpointPath, err := pendingCheckpointPath(root, loaded, a)
		if err != nil {
			return nil, err
		}
		cp, found, err := DefaultCheckpointStore().Load(checkpointPath)
		if err != nil {
			return nil, err
		}
		if found && (cp.AssignmentID != a.AssignmentID || cp.BaselineGeneration != loaded.BaselineGeneration) {
			return nil, fmt.Errorf("checkpoint identity differs from pending assignment %s", a.AssignmentID)
		}
		if found {
			if err := validatePendingCheckpointRecordBinding(cp, a); err != nil {
				return nil, err
			}
		}
		if found && cp.State == StateComplete {
			currentReport := a.CompletionRef == "" || cp.CompletionReportPath == "" || a.CompletionRef == cp.CompletionReportPath
			if currentReport && (!loaded.DispatchBackpressure || acknowledgedReport(a, cp)) {
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
		row := PendingIntegration{AssignmentID: a.AssignmentID, AgentID: a.OwnerAgentID, State: state, Failure: cp.FailureReason, Command: []string{"loop-harness", "runtime", "task-integrate", "--root", root, "--assignment-id", a.AssignmentID, "--agent-id", a.OwnerAgentID}}
		if found && (cp.State == StatePreserved || cp.State == StateBlocked) {
			row.Command = append(row.Command, "--retry-preserved")
			if cp.VerifiedAt == "" {
				command, commandErr := canonicalRevisionCommand(root, loaded, a)
				if commandErr != nil {
					row.Failure = appendPendingFailure(row.Failure, "S6 correction requires the current Assignment record: "+commandErr.Error())
				} else {
					row.ReworkCommand = command
				}
			}
		}
		row.CurrentReport = found && cp.CompletionReportPath != "" && cp.CompletionReportPath == a.CompletionRef
		row.Acknowledged = row.CurrentReport && acknowledgedReport(a, cp)
		row.TaskID = a.TaskID
		if found {
			row.CheckpointRef = checkpointPath
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AssignmentID < rows[j].AssignmentID })
	return rows, nil
}

func canonicalAssignmentProjection(assignment hookctx.AssignmentContext) bool {
	return assignment.AssignmentRecord != nil || assignment.AssignmentRevision != 0
}

// canonicalRevisionCommand obtains the task path from the same current
// Runtime Record that was projected into LoadedContext. The Writer derives
// its retry identity from this original responsibility.
func canonicalRevisionCommand(root string, loaded *hookctx.LoadedContext, assignment hookctx.AssignmentContext) ([]string, error) {
	if loaded == nil || assignment.AssignmentRecord == nil {
		return nil, fmt.Errorf("incomplete canonical Assignment projection")
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return nil, err
	}
	if snapshot.Revision != loaded.PolicyContext.Revision || snapshot.State["runtime_id"] != loaded.PolicyContext.RuntimeID {
		return nil, fmt.Errorf("Runtime changed after the pending projection; refresh it before issuing a revision")
	}
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	generation := pendingNumber(baseline["generation"])
	if generation != loaded.BaselineGeneration {
		return nil, fmt.Errorf("Runtime baseline changed after the pending projection; refresh it before issuing a revision")
	}
	record, found, err := assignmentrecord.ForOwner(snapshot.State, assignment.OwnerAgentID)
	if err != nil {
		return nil, err
	}
	if !found || record.AssignmentID != assignment.AssignmentID || record.Reference() != *assignment.AssignmentRecord {
		return nil, fmt.Errorf("current owner Record differs from the pending Assignment")
	}
	if record.Task.ID != assignment.TaskID || record.Task.Ref == "" {
		return nil, fmt.Errorf("current owner Record TASK differs from the pending Assignment")
	}
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		return []string{"loop-harness", "s6", "prepare", "--root", root, "--task", record.Task.ID, "--role", record.RoleFamily, "--revise-assignment", record.AssignmentID}, nil
	}
	return []string{
		"loop-harness", "runtime", "register-workgroup", "--root", root,
		"--manifest", "<fresh-manifest>", "--task-id", record.Task.ID, "--task", record.Task.Ref,
		"--revise-assignment", record.AssignmentID,
	}, nil
}

func pendingNumber(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func appendPendingFailure(current, hint string) string {
	if current == "" {
		return hint
	}
	return current + "; " + hint
}

func validatePendingCheckpointRecordBinding(checkpoint Checkpoint, assignment hookctx.AssignmentContext) error {
	if assignment.AssignmentRecord == nil || assignment.AssignmentRevision < 1 {
		return fmt.Errorf("pending Assignment %s has an incomplete canonical Record projection", assignment.AssignmentID)
	}
	wantRef := *assignment.AssignmentRecord
	if checkpoint.CanonicalMainFacts != nil {
		binding := checkpoint.CanonicalMainFacts.Binding
		if binding.AssignmentRecord != wantRef {
			return fmt.Errorf("pending checkpoint Main facts belong to another Assignment revision")
		}
	}
	if checkpoint.AssignmentRecord != nil {
		if *checkpoint.AssignmentRecord != wantRef {
			return fmt.Errorf("pending checkpoint early Record binding differs from the current Assignment")
		}
	}
	if checkpoint.CanonicalMainFacts == nil && checkpoint.AssignmentRecord == nil {
		return fmt.Errorf("pending canonical checkpoint lacks its early Assignment Record binding")
	}
	return nil
}

// pendingCheckpointPath consumes only a Record reference already resolved by
// hookctx from the current Runtime. A partial canonical projection fails
// closed; an unprojected legacy assignment is not an S6 responsibility.
func pendingCheckpointPath(root string, loaded *hookctx.LoadedContext, assignment hookctx.AssignmentContext) (string, error) {
	if loaded == nil {
		return "", fmt.Errorf("pending integration requires the loaded current Assignment context")
	}
	if assignment.AssignmentRecord == nil {
		return "", fmt.Errorf("pending Assignment %s has an incomplete canonical Record projection", assignment.AssignmentID)
	}
	ref := *assignment.AssignmentRecord
	if ref.RuntimeID != loaded.PolicyContext.RuntimeID || ref.BaselineGeneration != loaded.BaselineGeneration || ref.AssignmentID != assignment.AssignmentID || ref.Revision != assignment.AssignmentRevision {
		return "", fmt.Errorf("pending Assignment %s Record reference differs from current Runtime coordinates", assignment.AssignmentID)
	}
	path := CheckpointPathForRecord(root, ref)
	if path == "" {
		return "", fmt.Errorf("pending Assignment %s Record reference has no checkpoint slot", assignment.AssignmentID)
	}
	return path, nil
}

func acknowledgedReport(a hookctx.AssignmentContext, cp Checkpoint) bool {
	qualifiedState := cp.State == StateVerified || cp.State == StateAcknowledged || cp.State == StateCleanupPending || cp.State == StateComplete
	return qualifiedState && a.CompletionAckRef != "" && cp.CompletionReportPath != "" && cp.CompletionReportPath == a.CompletionRef
}
