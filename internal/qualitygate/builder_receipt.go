package qualitygate

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type BuilderReceiptRequest struct {
	Owner, AssignmentID, TaskID, CompletionRef string
	// RequiredChecks and DoneWhen carry the assignment's declared self-check
	// commands and closing contract. They are original responsibility facts:
	// a missing or failing self-check stays pending on the Builder's own side
	// and is never satisfied by another artifact.
	RequiredChecks []string
	DoneWhen       []string
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
	return currentLegacyBuilderReceipt(input, req)
}

// currentLegacyBuilderReceipt projects the original Builder responsibility for
// a legacy (non-canonical) Assignment.
//
// The Worker's own self-check source is the Worker's own completion report; a
// Main integration checkpoint only ever names the Main check source, and that
// source stays on the canonical side. Delivery, acceptance, completion,
// pending work and waiting items are all derived from the original
// responsibility coordinate: the registered owner/Assignment/TASK relation,
// the recorded delivery, the current activity state, the original TASK
// document, the Worker's own completion-report envelope, and Main's
// integration checkpoint. Only these structural facts decide the receipt: no
// revision, sha, hash, digest, fingerprint, checksum, round or CAS value is
// compared, registered or used as a gate. A historical digest field is read
// only along its original path and never recomputed.
func currentLegacyBuilderReceipt(input Input, req BuilderReceiptRequest) BuilderReceipt {
	state := input.Snapshot.State
	r := BuilderReceipt{
		SchemaVersion: "1.0.0", RuntimeID: stringValue(state["runtime_id"]),
		Generation: nestedInt(state, "baseline", "generation"), ObservedRevision: input.Snapshot.Revision,
		Owner: req.Owner, AssignmentID: req.AssignmentID, TaskID: req.TaskID,
		ChecksSource: "original_report", ResponsibilityStatus: "not_submitted",
		DomainRefs: []string{}, OwnPending: []string{}, WaitingOn: []ResponsibilityWaiting{},
	}
	missing := func(reason string) BuilderReceipt {
		r.OwnPending = append(r.OwnPending, reason)
		return r
	}
	sourcePending := func(reason, ref string) BuilderReceipt {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: reason, Ref: ref})
		return r
	}
	if req.Owner == "" || req.TaskID == "" || req.AssignmentID == "" || path.Base(req.AssignmentID) != req.AssignmentID || input.Files == nil {
		return missing("registered_assignment")
	}
	task, taskFound, taskErr := uniqueEntityByID(state, "tasks", req.TaskID)
	if taskErr != nil {
		return missing("ambiguous_task")
	}
	agent, agentFound, agentErr := uniqueEntityByID(state, "agents", req.Owner)
	if agentErr != nil {
		return missing("ambiguous_owner")
	}
	_, assignment, _ := strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if !taskFound || !agentFound || assignment != req.AssignmentID ||
		!containsAny(task["owner_agent_ids"], req.Owner) || !containsAny(agent["task_ids"], req.TaskID) {
		return missing("registered_owner_task_assignment")
	}
	if req.CompletionRef == "" || task["completion_report_ref"] != req.CompletionRef || agent["completion_reported_ref"] != req.CompletionRef {
		return missing("current_Result")
	}
	r.DeliveryRecorded = true
	r.ResponsibilityStatus = "invalid"
	taskActivity, agentActivity := workstate.TaskActivity(task), workstate.AgentActivity(agent)
	if taskActivity == "blocked" || taskActivity == "cancelled" ||
		(agentActivity != "reported" && agentActivity != "done" && agentActivity != "stopped") {
		return missing("current_delivery_state")
	}
	var docs []documentFact
	for _, doc := range currentDocuments(state, r.Generation) {
		if doc.Kind == "task" && doc.ID == req.TaskID {
			docs = append(docs, doc)
		}
	}
	if len(docs) != 1 {
		return missing("current_TASK_subject")
	}
	doc := docs[0]
	if doc.Path == "" {
		return missing("current_TASK_subject")
	}
	// The original subject must exist and be readable; no digest of it decides
	// anything.
	if _, err := input.Files.ReadFile(doc.Path); err != nil {
		return sourcePending("task_input_changed", doc.Path)
	}
	requirement := EvidenceRequirement{Kind: "completion_report", Responsibilities: []string{"BUILD-WORK-PACKAGE"}, Conclusions: []string{"completed"}}
	ids, _ := qualifiedEvidence(state, input.Files, r.RuntimeID, r.Generation, requirement, docs)
	var env evidenceEnvelope
	found := false
	matchingPaths := 0
	for _, entry := range anyRows(state["evidence"]) {
		if entry["path"] != req.CompletionRef {
			continue
		}
		matchingPaths++
		if matchingPaths > 1 {
			return sourcePending("result_evidence_invalid", req.CompletionRef)
		}
		if !containsString(ids, stringValue(entry["id"])) {
			continue
		}
		body, err := input.Files.ReadFile(req.CompletionRef)
		if err != nil || json.Unmarshal(body, &env) != nil {
			continue
		}
		found = env.TaskID == req.TaskID && env.ProducerAgentID == req.Owner && legacyExactSubjects(env.SubjectRefs, docs)
	}
	if !found {
		return sourcePending("result_evidence_invalid", req.CompletionRef)
	}
	r.Accepted, r.ResponsibilityStatus = true, "accepted_incomplete"
	r.DomainRefs = append(r.DomainRefs, req.CompletionRef)
	if len(env.Checks) == 0 {
		r.OwnPending = append(r.OwnPending, "owned_checks_missing")
	}
	for _, failed := range failingEnvelopeChecks(env) {
		r.OwnPending = append(r.OwnPending, "owned_check:"+failed)
	}
	for _, command := range req.RequiredChecks {
		present := false
		for _, check := range env.Checks {
			if check.Command == command && check.Result == "pass" {
				present = true
			}
		}
		if !present {
			r.OwnPending = append(r.OwnPending, "required_check:"+command)
		}
	}
	if len(env.ScopeDeviations) > 0 {
		r.OwnPending = append(r.OwnPending, "scope_deviations")
	}
	if len(req.DoneWhen) == 0 {
		r.OwnPending = append(r.OwnPending, "missing_closing_contract")
	}
	r.OwnPending = sortedUnique(r.OwnPending)
	if len(r.OwnPending) > 0 {
		return r
	}
	r.WorkComplete, r.ResponsibilityStatus = true, "complete"
	// Main's durable integration record lives in the slot the integration store
	// owns (integration.CheckpointPath). Only its original responsibility
	// coordinates decide whether Main has integrated this responsibility.
	checkpoint := path.Join(".claude/evidence", r.RuntimeID, fmt.Sprintf("g%d", r.Generation), "worktree", req.AssignmentID, "checkpoint.json")
	cpBytes, err := input.Files.ReadFile(checkpoint)
	var cp integration.Checkpoint
	reason := "integration_pending"
	if err == nil {
		reason = "integration_receipt_invalid"
		if json.Unmarshal(cpBytes, &cp) == nil &&
			cp.AssignmentID == req.AssignmentID &&
			cp.TaskID == req.TaskID &&
			cp.BaselineGeneration == r.Generation &&
			cp.CompletionReportPath == req.CompletionRef {
			if verifiedIntegrationCheckpointState(cp.State) {
				r.Integrated = true
			} else {
				reason = "integration_pending"
			}
		}
	}
	if r.Integrated {
		r.DomainRefs = append(r.DomainRefs, checkpoint)
	} else {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: reason, Ref: checkpoint})
	}
	return r
}

// legacyExactSubjects matches the completion-report subject refs against the
// current TASK document. The original subject identity is its registered path
// and version; no digest, fingerprint or revision participates.
func legacyExactSubjects(subjects []subjectRef, documents []documentFact) bool {
	if len(subjects) != len(documents) {
		return false
	}
	wanted := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		wanted[document.Path+"|"+document.Version] = struct{}{}
	}
	for _, subject := range subjects {
		key := subject.Path + "|" + subject.Version
		if _, ok := wanted[key]; !ok {
			return false
		}
		delete(wanted, key)
	}
	return len(wanted) == 0
}

// failingEnvelopeChecks lists the Worker's own self-checks that did not pass.
// The label is the original check name, or its command when the report records
// no name; the result is reported exactly as it was recorded.
func failingEnvelopeChecks(envelope evidenceEnvelope) []string {
	var failing []string
	for _, check := range envelope.Checks {
		if check.Result == "pass" {
			continue
		}
		label := check.Name
		if label == "" {
			label = check.Command
		}
		failing = append(failing, label+"="+check.Result)
	}
	return failing
}

// verifiedIntegrationCheckpointState reports whether Main's durable
// integration checkpoint reached a verified state. It reuses the same state
// set the S9 repair result already accepts.
func verifiedIntegrationCheckpointState(state string) bool {
	switch state {
	case integration.StateVerified, integration.StateAcknowledged, integration.StateCleanupPending, integration.StateComplete:
		return true
	default:
		return false
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
