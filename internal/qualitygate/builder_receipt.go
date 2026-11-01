package qualitygate

import (
	"path"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type BuilderReceiptRequest struct {
	Owner, AssignmentID, TaskID, CompletionRef string
}

type ResponsibilityWaiting struct {
	Owner  string `json:"owner"`
	Reason string `json:"reason"`
	Ref    string `json:"ref"`
}

// BuilderReceipt projects original domain facts; it neither executes checks
// nor authorizes a transition. Check results retain their original report source.
type BuilderReceipt struct {
	ownerStarted                      bool
	explicitBlocker                   bool
	originalCancellationOrReplacement bool
	SchemaVersion                     string                              `json:"schema_version"`
	RuntimeID                         string                              `json:"runtime_id"`
	Generation                        int                                 `json:"baseline_generation"`
	ObservedRevision                  int                                 `json:"observed_revision"`
	Owner                             string                              `json:"owner"`
	AssignmentID                      string                              `json:"assignment_id"`
	TaskID                            string                              `json:"task_id"`
	DeliveryRecorded                  bool                                `json:"delivery_recorded"`
	Accepted                          bool                                `json:"accepted"`
	WorkComplete                      bool                                `json:"work_complete"`
	Integrated                        bool                                `json:"integrated"`
	ResponsibilityStatus              string                              `json:"responsibility_status"`
	ChecksSource                      string                              `json:"checks_source"`
	ProfessionalStatus                string                              `json:"professional_status,omitempty"`
	AssignmentRecord                  *assignmentrecord.RecordReference   `json:"assignment_record,omitempty"`
	ResultRef                         string                              `json:"result_ref,omitempty"`
	ResultRevision                    int                                 `json:"result_revision,omitempty"`
	PlanRef                           string                              `json:"plan_ref,omitempty"`
	ResolvedEvidenceRefs              []assignmentresult.ResolvedEvidence `json:"resolved_evidence_refs,omitempty"`
	WorkerCheckFacts                  []assignmentresult.WorkerCheckFact  `json:"worker_check_facts,omitempty"`
	MainCheckpointRef                 string                              `json:"main_checkpoint_ref,omitempty"`
	ActualChangedPaths                []string                            `json:"actual_changed_paths,omitempty"`
	MainCheckReceipts                 []assignmentresult.MainCheckFact    `json:"main_check_receipts,omitempty"`
	DomainRefs                        []string                            `json:"domain_refs"`
	OwnPending                        []string                            `json:"own_pending"`
	WaitingOn                         []ResponsibilityWaiting             `json:"waiting_on"`
}

func CurrentBuilderReceipt(input Input, req BuilderReceiptRequest) (result BuilderReceipt) {
	state := input.Snapshot.State
	started, blocked, cancelled := currentBuilderResponsibilityFacts(state, req)
	mode, canonical, modeErr := canonicalModeForOwner(input, req.Owner)
	defer func() {
		result.ownerStarted = started
		result.explicitBlocker = blocked
		result.originalCancellationOrReplacement = cancelled
	}()
	if blocked || cancelled {
		checksSource := "original_report"
		if canonical {
			checksSource = "main_checkpoint"
		}
		return BuilderReceipt{
			SchemaVersion: "1.0.0", RuntimeID: stringValue(state["runtime_id"]),
			Generation: nestedInt(state, "baseline", "generation"), ObservedRevision: input.Snapshot.Revision,
			Owner: req.Owner, AssignmentID: req.AssignmentID, TaskID: req.TaskID,
			ChecksSource: checksSource, ResponsibilityStatus: "invalid",
			DomainRefs: []string{}, OwnPending: []string{"current_delivery_state"},
			WaitingOn: []ResponsibilityWaiting{},
		}
	}
	if canonical {
		return currentCanonicalBuilderReceipt(input, req, mode, modeErr)
	}
	ref := req.AssignmentID
	if ref == "" {
		ref = req.TaskID
	}
	if ref == "" {
		ref = "current-assignment"
	}
	return BuilderReceipt{
		SchemaVersion: "1.0.0", RuntimeID: stringValue(state["runtime_id"]),
		Generation: nestedInt(state, "baseline", "generation"), ObservedRevision: input.Snapshot.Revision,
		Owner: req.Owner, AssignmentID: req.AssignmentID, TaskID: req.TaskID,
		ChecksSource: "main_checkpoint", ResponsibilityStatus: "invalid",
		DomainRefs: []string{}, OwnPending: []string{},
		WaitingOn: []ResponsibilityWaiting{{Owner: "main", Reason: "assignment_unavailable", Ref: ref}},
	}
}

// currentBuilderResponsibilityFacts projects only original Runtime lifecycle
// facts for the exact owner/Assignment/TASK tuple. Report strings and
// diagnostic status labels are deliberately not completion or blocker proof.
func currentBuilderResponsibilityFacts(state map[string]any, req BuilderReceiptRequest) (started, blocked, cancelled bool) {
	if state == nil || req.Owner == "" || req.AssignmentID == "" || req.TaskID == "" || path.Base(req.AssignmentID) != req.AssignmentID {
		return false, false, false
	}
	version := stringValue(state["schema_version"])
	if version == "" {
		version = "1.2.0"
	}
	task, taskFound, err := uniqueEntityByID(state, "tasks", req.TaskID)
	if err != nil || !taskFound {
		return false, false, false
	}
	agent, agentFound, err := uniqueEntityByID(state, "agents", req.Owner)
	if err != nil || !agentFound {
		return false, false, false
	}
	_, assignment, _ := strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if assignment != req.AssignmentID || !containsAny(agent["task_ids"], req.TaskID) {
		return false, false, false
	}
	if record, found, recordErr := assignmentrecord.ForOwner(state, req.Owner); recordErr != nil || found &&
		(record.AssignmentID != req.AssignmentID || record.Task.ID != req.TaskID) {
		return false, false, false
	}
	agentProjection, err := workstate.AgentProjection(agent, version)
	if err != nil {
		return false, false, false
	}
	taskProjection, err := workstate.TaskProjection(task, version)
	if err != nil {
		return false, false, false
	}
	agentEvent := stringValue(agent["last_lifecycle_event"])
	taskOwned := containsAny(task["owner_agent_ids"], req.Owner)
	assignmentReplaced := version == workstate.UnifiedVersion &&
		agentProjection.Status == workstate.Cancelled && agentProjection.Activity == "stopped" && agentEvent == "assignment_revision_issued"
	if !taskOwned && !assignmentReplaced {
		return false, false, false
	}

	started = workstate.AgentStarted(agentProjection.Activity)
	agentBlocker := false
	if agentProjection.Status == workstate.Blocked && agentProjection.Activity == "blocked" {
		workBlocker := stringValue(agent["work_blocked_ref"]) != "" && stringValue(agent["blocker_resolved_ref"]) == ""
		// document_conflict_reported binds its submitted readback through the
		// original readback_ref field (see agentEventEvidenceKey).
		documentBlocker := stringValue(agent["readback_ref"]) != ""
		if version == workstate.UnifiedVersion {
			agentBlocker = agentEvent == "work_blocked" && workBlocker || agentEvent == "document_conflict_reported" && documentBlocker
		} else {
			agentBlocker = workBlocker || documentBlocker
		}
	}
	taskBlocker := taskOwned && taskProjection.Status == workstate.Blocked && taskProjection.Activity == "blocked" && stringValue(task["blocker_evidence"]) != ""
	blocked = agentBlocker || taskBlocker

	agentCancelled := agentProjection.Status == workstate.Cancelled && agentProjection.Activity == "stopped" &&
		(stringValue(agent["shutdown_approved_ref"]) != "" || assignmentReplaced)
	taskCancelled := taskOwned && taskProjection.Status == workstate.Cancelled && taskProjection.Activity == "cancelled" &&
		stringValue(task["cancellation_reason"]) != ""
	cancelled = agentCancelled || taskCancelled
	return started, blocked, cancelled
}

func anyRows(value any) []map[string]any {
	var result []map[string]any
	rows, _ := value.([]any)
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok {
			result = append(result, row)
		}
	}
	return result
}
