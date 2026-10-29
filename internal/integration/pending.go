package integration

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
		if b := loaded.PolicyContext.Workspace; b != nil {
			if e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration); ok && e.Status == "preparing" && e.ReworkRef != "" {
				row := PendingIntegration{TaskID: a.TaskID, AssignmentID: a.AssignmentID, AgentID: a.OwnerAgentID, State: "rework_pending"}
				if canonicalAssignmentProjection(a) {
					command, err := canonicalRevisionCommand(root, loaded, a)
					if err != nil {
						row.Failure = "canonical S6 rework must use a new named Assignment revision; current TASK path could not be verified: " + err.Error()
					} else {
						row.Command = command
						row.ReworkCommand = append([]string(nil), command...)
						row.Failure = "canonical S6 rework requires a new named Assignment revision; resume through the original register-workgroup writer"
					}
				} else {
					row.Command = []string{"loop-harness", "runtime", "workspace", "rework", "--root", root, "--assignment", a.AssignmentID, "--agent", a.OwnerAgentID, "--reason", "resume recorded rework"}
				}
				rows = append(rows, row)
				continue
			}
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
			projected := true
			if b := loaded.PolicyContext.Workspace; b != nil {
				e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration)
				projected = ok && e.Status == "complete"
				if state, err := readCanonicalRuntimeState(root); err == nil {
					if current, found, err := assignmentrecord.ForOwner(state, a.OwnerAgentID); err == nil && found && current.Contract == assignmentrecord.NativeDeliveryContract {
						projected = true
					}
				}
			}
			currentReport := true
			if cp.CompletionReportSHA256 != "" {
				refreshed, err := RefreshCompletionBinding(root, loaded.PolicyContext.RuntimeID, a.CompletionRef, InspectionFromCheckpoint(cp))
				currentReport = err == nil && refreshed.CompletionReportSHA256 == cp.CompletionReportSHA256 && refreshed.CompletionReportPath == cp.CompletionReportPath
			}
			if projected && currentReport && (!loaded.DispatchBackpressure || acknowledgedReport(a, cp)) {
				continue
			}
		}
		typedCandidate := false
		if loaded.PolicyContext.CurrentState == "bug_resolution" {
			if b := loaded.PolicyContext.Workspace; b != nil {
				if e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration); ok {
					typedCandidate = e.DeliveryRef != ""
				}
			}
		}
		if !found && !typedCandidate && (a.CompletionRef == "" || a.WorktreePath == "") {
			continue
		}
		state := cp.State
		if !found {
			state = "reported"
		}
		row := PendingIntegration{AssignmentID: a.AssignmentID, AgentID: a.OwnerAgentID, State: state, Failure: cp.FailureReason, Command: []string{"loop-harness", "runtime", "task-integrate", "--root", root, "--assignment-id", a.AssignmentID, "--agent-id", a.OwnerAgentID}}
		if b := loaded.PolicyContext.Workspace; b != nil {
			if e, ok := b.Execution(a.AssignmentID, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration); ok && e.DeliveryRef != "" {
				row.Command = []string{"loop-harness", "runtime", "workspace", "integrate", "--root", root, "--assignment", a.AssignmentID, "--agent", a.OwnerAgentID}
			}
		}

		if typedCandidate && !found {
			row.Failure = "Main integration and checks pending; choose each actual necessary Main check with repeatable --check-command."
		}

		if found && (cp.State == StatePreserved || cp.State == StateBlocked) {
			row.Command = append(row.Command, "--retry-preserved")
			if cp.TestedHead == "" {
				if canonicalAssignmentProjection(a) {
					command, commandErr := canonicalRevisionCommand(root, loaded, a)
					if commandErr != nil {
						row.Failure = appendPendingFailure(row.Failure, "canonical S6 correction needs a new named Assignment revision; current TASK path could not be verified: "+commandErr.Error())
					} else {
						row.ReworkCommand = command
					}
				} else if loaded.PolicyContext.Workspace != nil {
					row.ReworkCommand = []string{"loop-harness", "runtime", "workspace", "rework", "--root", root, "--assignment", a.AssignmentID, "--agent", a.OwnerAgentID, "--reason", "<describe required code correction>"}
				}
			}
		}
		row.CurrentReport = false
		if found && cp.CompletionReportSHA256 != "" {
			refreshed, refreshErr := RefreshCompletionBinding(root, loaded.PolicyContext.RuntimeID, a.CompletionRef, InspectionFromCheckpoint(cp))
			row.CurrentReport = refreshErr == nil && refreshed.CompletionReportSHA256 == cp.CompletionReportSHA256 && refreshed.CompletionReportPath == cp.CompletionReportPath
		}
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
	return assignment.AssignmentRecord != nil || assignment.AssignmentRecordSHA256 != "" || assignment.AssignmentRevision != 0
}

// canonicalRevisionCommand obtains the task path from the same current
// Runtime Record that was projected into LoadedContext. The manifest and
// revision intent key are Main-authored inputs, so the argv contains explicit
// placeholders for those values rather than guessing them.
func canonicalRevisionCommand(root string, loaded *hookctx.LoadedContext, assignment hookctx.AssignmentContext) ([]string, error) {
	if loaded == nil || assignment.AssignmentRecord == nil || assignment.AssignmentRecordSHA256 == "" {
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
	digest, err := record.Digest()
	if err != nil {
		return nil, err
	}
	if digest != assignment.AssignmentRecordSHA256 || record.Task.ID != assignment.TaskID || record.Task.Ref == "" {
		return nil, fmt.Errorf("current owner Record TASK or digest differs from the pending Assignment")
	}
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		return []string{"loop-harness", "s6", "prepare", "--root", root, "--task", record.Task.ID, "--role", record.RoleFamily, "--revise-assignment", record.AssignmentID, "--revision-intent-key", "<new-key>"}, nil
	}
	return []string{
		"loop-harness", "runtime", "register-workgroup", "--root", root,
		"--manifest", "<fresh-manifest>", "--task-id", record.Task.ID, "--task", record.Task.Ref,
		"--revise-assignment", record.AssignmentID, "--revision-intent-key", "<new-key>",
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
	canonicalProjection := assignment.AssignmentRecord != nil || assignment.AssignmentRecordSHA256 != "" || assignment.AssignmentRevision != 0
	if !canonicalProjection {
		if checkpoint.CanonicalMainFacts != nil || checkpoint.AssignmentRecord != nil || checkpoint.AssignmentRecordSHA256 != "" {
			return fmt.Errorf("legacy pending Assignment cannot reuse a canonical revision checkpoint")
		}
		return nil
	}
	if assignment.AssignmentRecord == nil || assignment.AssignmentRecordSHA256 == "" || assignment.AssignmentRevision < 1 {
		return fmt.Errorf("pending Assignment %s has an incomplete canonical Record projection", assignment.AssignmentID)
	}
	wantRef, wantSHA := *assignment.AssignmentRecord, assignment.AssignmentRecordSHA256
	if checkpoint.CanonicalMainFacts != nil {
		binding := checkpoint.CanonicalMainFacts.Binding
		if binding.AssignmentRecord != wantRef || binding.AssignmentRecordSHA256 != wantSHA {
			return fmt.Errorf("pending checkpoint Main facts belong to another Assignment revision")
		}
	}
	if checkpoint.AssignmentRecord != nil || checkpoint.AssignmentRecordSHA256 != "" {
		if checkpoint.AssignmentRecord == nil || *checkpoint.AssignmentRecord != wantRef || checkpoint.AssignmentRecordSHA256 != wantSHA {
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
// closed; an unprojected legacy assignment retains its historical r1 path.
func pendingCheckpointPath(root string, loaded *hookctx.LoadedContext, assignment hookctx.AssignmentContext) (string, error) {
	if loaded == nil {
		return "", fmt.Errorf("pending integration requires the loaded current Assignment context")
	}
	canonicalFieldsPresent := assignment.AssignmentRecord != nil || assignment.AssignmentRecordSHA256 != "" || assignment.AssignmentRevision != 0
	if !canonicalFieldsPresent {
		return CheckpointPath(root, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration, assignment.AssignmentID), nil
	}
	if assignment.AssignmentRecord == nil || len(assignment.AssignmentRecordSHA256) != 64 || strings.ToLower(assignment.AssignmentRecordSHA256) != assignment.AssignmentRecordSHA256 {
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
	return qualifiedState && cp.TestedHead != "" && a.CompletionAckRef != "" && cp.CompletionReportPath != "" && cp.CompletionReportSHA256 != "" && a.AcknowledgedReports[cp.CompletionReportPath] == cp.CompletionReportSHA256
}
