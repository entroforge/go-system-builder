package qualitygate

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const canonicalReceiptReadLimit int64 = 20 << 20

type canonicalAssignmentMode struct {
	Record     assignmentrecord.Record
	HasRecord  bool
	Manifest   string
	Assignment string
}

type canonicalMainCheckpoint struct {
	AssignmentRecord   *assignmentrecord.RecordReference `json:"assignment_record,omitempty"`
	TaskID             string                            `json:"task_id"`
	AssignmentID       string                            `json:"assignment_id"`
	Generation         int                               `json:"baseline_generation"`
	State              string                            `json:"state"`
	VerifiedAt         string                            `json:"verified_at"`
	ReportPath         string                            `json:"completion_report_path"`
	TargetBranch       string                            `json:"target_branch"`
	ChangedPaths       []string                          `json:"changed_paths"`
	PreviousMainRoots  []string                          `json:"previous_main_roots,omitempty"`
	CheckReceipts      []string                          `json:"check_receipts,omitempty"`
	ToolCheckReceipts  []checkexec.ToolReceipt           `json:"tool_check_receipts,omitempty"`
	CanonicalMainFacts *assignmentresult.MainFacts       `json:"canonical_main_facts"`
	Revision           int64                             `json:"revision"`
}

type canonicalBuilderResult struct {
	Record     assignmentrecord.Record
	Result     assignmentresult.AcceptedResult
	ResultRef  string
	Checkpoint string
	Facts      *assignmentresult.MainFacts
	MainReason string
	Integrated bool
	EvidenceID string
	Err        error
}

// canonicalModeForOwner selects the S6 v1.2 path before legacy completion
// qualification. A current Record always opts into the strict path; without
// one, only the exact original Team manifest marker can select it.
func canonicalModeForOwner(input Input, owner string) (canonicalAssignmentMode, bool, error) {
	var mode canonicalAssignmentMode
	if owner == "" {
		return mode, false, nil
	}
	record, found, err := assignmentrecord.ForOwner(input.Snapshot.State, owner)
	if err != nil {
		return mode, true, fmt.Errorf("resolve current canonical Assignment: %w", err)
	}
	if found {
		mode.Record, mode.HasRecord, mode.Manifest, mode.Assignment = record, true, record.Manifest.Ref, record.AssignmentID
		return mode, true, nil
	}
	agent, ok, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
	if err != nil {
		return mode, true, err
	}
	if !ok {
		return mode, false, nil
	}
	teamID := stringValue(agent["team_id"])
	if teamID == "" {
		return mode, false, nil
	}
	teams, _ := input.Snapshot.State["entities"].(map[string]any)
	var team map[string]any
	for _, row := range anyRows(teams["teams"]) {
		if row["id"] != teamID {
			continue
		}
		if team != nil {
			return mode, true, fmt.Errorf("Team %s has duplicate Runtime rows", teamID)
		}
		team = row
	}
	if team == nil {
		return mode, false, nil
	}
	mode.Manifest = stringValue(team["manifest_ref"])
	_, mode.Assignment, _ = strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if mode.Manifest == "" || input.Files == nil {
		return mode, true, fmt.Errorf("cannot resolve original Team manifest to determine the explicit S6 marker")
	}
	manifest, err := input.Files.ReadFile(mode.Manifest)
	if err != nil {
		return mode, true, fmt.Errorf("read original Team manifest %s: %w", mode.Manifest, err)
	}
	enabled, markerErr := assignmentrecord.ManifestAssignmentRecordEnabled(manifest)
	if markerErr != nil {
		return mode, true, fmt.Errorf("read explicit Assignment Record marker: %w", markerErr)
	}
	if !enabled {
		return mode, false, nil
	}
	return mode, true, fmt.Errorf("Team manifest opts into %s but the current Runtime has no canonical Assignment Record", assignmentrecord.AssignmentRecordContract)
}

func uniqueEntityByID(state map[string]any, collection, id string) (map[string]any, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	var found map[string]any
	for _, row := range anyRows(entities[collection]) {
		if row["id"] != id {
			continue
		}
		if found != nil {
			return nil, false, fmt.Errorf("ambiguous %s %s", collection, id)
		}
		found = row
	}
	return found, found != nil, nil
}

func canonicalModeForTask(input Input, task map[string]any) (canonicalAssignmentMode, bool, error) {
	taskID := stringValue(task["id"])
	var selected canonicalAssignmentMode
	selectedFound := false
	for _, owner := range canonicalStringValues(task["owner_agent_ids"]) {
		agent, ok, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
		if err != nil || !ok {
			if err != nil {
				return selected, true, err
			}
			continue
		}
		mode, canonical, modeErr := canonicalModeForOwner(input, owner)
		if !canonical {
			continue
		}
		// The registered current Assignment must still name this TASK and
		// the bidirectional Runtime owner links must agree. An explicit S6
		// owner on this TASK cannot use its old legacy report.
		if modeErr == nil && mode.HasRecord && mode.Record.Task.ID != taskID {
			modeErr = fmt.Errorf("current Assignment Record is bound to TASK %s, not %s", mode.Record.Task.ID, taskID)
		}
		if modeErr == nil && (!containsAny(agent["task_ids"], taskID) || !containsAny(task["owner_agent_ids"], owner)) {
			modeErr = fmt.Errorf("current Agent/TASK ownership links differ from the canonical Assignment")
		}
		if modeErr == nil && !mode.HasRecord {
			modeErr = fmt.Errorf("S6 marker has no current canonical Assignment Record")
		}
		if selectedFound {
			return selected, true, fmt.Errorf("TASK %s has multiple canonical Builder Assignments", taskID)
		}
		selected, selectedFound = mode, true
		if modeErr != nil {
			return selected, true, modeErr
		}
	}
	return selected, selectedFound, nil
}

func canonicalStringValues(raw any) []string {
	var values []string
	switch rows := raw.(type) {
	case []string:
		values = append(values, rows...)
	case []any:
		for _, row := range rows {
			if value, ok := row.(string); ok {
				values = append(values, value)
			}
		}
	}
	return values
}

func canonicalOwnerArtifactPrefix(record assignmentrecord.Record) string {
	return fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
}

func safeCanonicalRef(ref string) bool {
	return ref != "" && !filepath.IsAbs(ref) && !strings.ContainsAny(ref, "\\\x00\r\n") && filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref))) == ref && ref != "." && ref != ".." && !strings.HasPrefix(ref, "../")
}

func currentCanonicalReportRef(state map[string]any, owner, taskID string) (string, error) {
	task, found, err := uniqueEntityByID(state, "tasks", taskID)
	if err != nil || !found {
		return "", fmt.Errorf("current TASK is missing or ambiguous")
	}
	agent, found, err := uniqueEntityByID(state, "agents", owner)
	if err != nil || !found {
		return "", fmt.Errorf("current owner Agent is missing or ambiguous")
	}
	if !containsAny(task["owner_agent_ids"], owner) || !containsAny(agent["task_ids"], taskID) {
		return "", fmt.Errorf("current Agent/TASK ownership differs from the Assignment")
	}
	taskRef, agentRef := stringValue(task["completion_report_ref"]), stringValue(agent["completion_reported_ref"])
	if taskRef == "" || agentRef == "" || taskRef != agentRef {
		return "", fmt.Errorf("current Agent/TASK Result references are missing or differ")
	}
	return taskRef, nil
}

func canonicalCurrentPlanRef(input Input, owner string) (string, error) {
	agent, found, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
	if err != nil || !found {
		return "", fmt.Errorf("current owner is unavailable")
	}
	ref := stringValue(agent["plan_reported_ref"])
	if !safeCanonicalRef(ref) {
		return "", fmt.Errorf("current accepted PLAN_REPORT reference is unavailable")
	}
	return ref, nil
}

func readCanonicalResultArtifact(input Input, record assignmentrecord.Record, currentPlan, ref string) ([]byte, error) {
	if !safeCanonicalRef(ref) {
		return nil, fmt.Errorf("unsafe canonical Result artifact reference")
	}
	if ref != currentPlan && !strings.HasPrefix(ref, canonicalOwnerArtifactPrefix(record)) {
		return nil, fmt.Errorf("canonical Result artifact is outside the Assignment evidence namespace")
	}
	return workpacket.ReadFile(input.Root, ref, canonicalReceiptReadLimit)
}

func validateCurrentCanonicalRecord(input Input, mode canonicalAssignmentMode, owner, assignmentID, taskID string, requireDelivery bool) (assignmentrecord.Record, string, error) {
	if !mode.HasRecord {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Assignment Record is unavailable")
	}
	record := mode.Record
	if record.OwnerAgentID != owner || record.AssignmentID != assignmentID || record.Task.ID != taskID || record.RuntimeID != stringValue(input.Snapshot.State["runtime_id"]) || record.BaselineGeneration != nestedInt(input.Snapshot.State, "baseline", "generation") {
		return assignmentrecord.Record{}, "", fmt.Errorf("current Assignment responsibility differs")
	}
	agent, found, err := uniqueEntityByID(input.Snapshot.State, "agents", owner)
	if err != nil || !found {
		return assignmentrecord.Record{}, "", fmt.Errorf("owner Agent unavailable")
	}
	if activity := stringValue(workstate.AgentActivity(agent)); requireDelivery && activity != "reported" && activity != "done" && activity != "stopped" {
		return assignmentrecord.Record{}, "", fmt.Errorf("owner has not recorded delivery")
	}
	task, found, err := uniqueEntityByID(input.Snapshot.State, "tasks", taskID)
	if err != nil || !found || workstate.TaskActivity(task) == "blocked" || workstate.TaskActivity(task) == "cancelled" {
		return assignmentrecord.Record{}, "", fmt.Errorf("current TASK is unavailable")
	}
	if !containsAny(task["owner_agent_ids"], owner) || !containsAny(agent["task_ids"], taskID) {
		return assignmentrecord.Record{}, "", fmt.Errorf("Agent/TASK ownership differs")
	}
	return record, "", nil
}

func canonicalResultAwaitingSubmission(state map[string]any, owner, taskID string) bool {
	agent, agentFound, agentErr := uniqueEntityByID(state, "agents", owner)
	task, taskFound, taskErr := uniqueEntityByID(state, "tasks", taskID)
	if agentErr != nil || taskErr != nil || !agentFound || !taskFound ||
		stringValue(agent["completion_reported_ref"]) != "" || stringValue(task["completion_report_ref"]) != "" {
		return false
	}
	switch workstate.AgentActivity(agent) {
	case "spawned", "queued", "reading", "understanding_submitted", "understanding_approved", "activated", "working":
		return true
	default:
		return false
	}
}

func resolveCanonicalBuilderResult(input Input, mode canonicalAssignmentMode, owner, assignmentID, taskID string) canonicalBuilderResult {
	out := canonicalBuilderResult{MainReason: "integration_pending"}
	record, _, err := validateCurrentCanonicalRecord(input, mode, owner, assignmentID, taskID, true)
	if err != nil {
		out.Err = err
		return out
	}
	out.Record = record
	ref, err := currentCanonicalReportRef(input.Snapshot.State, owner, taskID)
	if err != nil {
		out.Err = err
		return out
	}
	out.ResultRef = ref
	planRef, err := canonicalCurrentPlanRef(input, owner)
	if err != nil {
		out.Err = err
		out.MainReason = "result_evidence_invalid"
		return out
	}
	read := func(p string) ([]byte, error) { return readCanonicalResultArtifact(input, record, planRef, p) }
	accepted, err := assignmentresult.LoadAccepted(input.Snapshot.State, record, ref, read)
	if err != nil {
		out.Err = err
		out.MainReason = "result_evidence_invalid"
		return out
	}
	out.Result, out.EvidenceID = accepted, accepted.EvidenceID
	checkpointRel := filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", record.Reference()))
	out.Checkpoint = checkpointRel
	data, err := workpacket.ReadFile(input.Root, checkpointRel, canonicalReceiptReadLimit)
	if err != nil {
		return out
	}
	var cp canonicalMainCheckpoint
	if json.Unmarshal(data, &cp) != nil {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	if cp.AssignmentID != record.AssignmentID || cp.TaskID != record.Task.ID || cp.Generation != record.BaselineGeneration || cp.ReportPath != ref || cp.CanonicalMainFacts == nil {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	switch cp.State {
	case "verified", "acknowledged", "cleanup_pending", "complete":
	case "pending", "ready", "merged":
		return out
	default:
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	facts := *cp.CanonicalMainFacts
	if facts.TargetBranch != boundDevelopmentBranch(input.Snapshot.State) {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	if err := assignmentresult.ValidateMainFacts(record, accepted, ref, facts); err != nil {
		out.MainReason = "integration_receipt_invalid"
		return out
	}
	out.Facts = &facts
	out.Integrated = true
	out.MainReason = ""
	return out
}

func readCanonicalMainCheckArtifact(root string, record assignmentrecord.Record, factsRoot string, previous []string, ref string) ([]byte, error) {
	rel, err := canonicalMainCheckArtifactRel(root, record, factsRoot, previous, ref)
	if err != nil {
		return nil, err
	}
	return workpacket.ReadFile(root, rel, canonicalReceiptReadLimit)
}

func canonicalMainCheckArtifactRel(root string, record assignmentrecord.Record, factsRoot string, previous []string, ref string) (string, error) {
	if !filepath.IsAbs(ref) {
		return "", fmt.Errorf("Main check artifact reference must be the original absolute runner ref")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	refAbs, err := filepath.Abs(ref)
	if err != nil {
		return "", err
	}
	allowedRoots := []string{rootAbs}
	if factsRoot != "" {
		factsAbs, absErr := filepath.Abs(factsRoot)
		if absErr != nil {
			return "", absErr
		}
		allowedRoots = append(allowedRoots, factsAbs)
	}
	for _, prior := range previous {
		if abs, absErr := filepath.Abs(prior); absErr == nil {
			allowedRoots = append(allowedRoots, abs)
		}
	}
	var rel string
	for _, allowedRoot := range allowedRoots {
		candidate, relErr := filepath.Rel(allowedRoot, refAbs)
		if relErr == nil && !filepath.IsAbs(candidate) && candidate != ".." && !strings.HasPrefix(candidate, ".."+string(filepath.Separator)) {
			rel = candidate
			break
		}
	}
	if rel == "" {
		return "", fmt.Errorf("Main check artifact escapes the original authorized Main roots")
	}
	rel = filepath.ToSlash(rel)
	prefix := path.Join(path.Dir(filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", record.Reference()))), "checks") + "/"
	if !strings.HasPrefix(rel, prefix) {
		return "", fmt.Errorf("Main check artifact is outside the exact Assignment check-receipt directory")
	}
	remainder := strings.TrimPrefix(rel, prefix)
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "loop-integration-check-") || len(parts[0]) == len("loop-integration-check-") || (parts[1] != "receipt.json" && parts[1] != "output.log") {
		return "", fmt.Errorf("Main check artifact is outside the original runner receipt layout")
	}
	return rel, nil
}

func currentCanonicalBuilderReceipt(input Input, req BuilderReceiptRequest, mode canonicalAssignmentMode, modeErr error) BuilderReceipt {
	r := BuilderReceipt{SchemaVersion: "1.0.0", RuntimeID: stringValue(input.Snapshot.State["runtime_id"]), Generation: nestedInt(input.Snapshot.State, "baseline", "generation"), ObservedRevision: input.Snapshot.Revision, Owner: req.Owner, AssignmentID: req.AssignmentID, TaskID: req.TaskID, ChecksSource: "main_checkpoint", ResponsibilityStatus: "invalid", DomainRefs: []string{}, OwnPending: []string{}, WaitingOn: []ResponsibilityWaiting{}, ProfessionalStatus: "not_submitted"}
	if modeErr != nil {
		r.OwnPending = append(r.OwnPending, "canonical_assignment_invalid")
		return r
	}
	awaiting := canonicalResultAwaitingSubmission(input.Snapshot.State, req.Owner, req.TaskID)
	record, _, err := validateCurrentCanonicalRecord(input, mode, req.Owner, req.AssignmentID, req.TaskID, !awaiting)
	if err != nil {
		r.OwnPending = append(r.OwnPending, "canonical_assignment_invalid")
		return r
	}
	if awaiting {
		r.ResponsibilityStatus = "not_submitted"
		r.OwnPending = append(r.OwnPending, "current_Result")
		return r
	}
	ref, err := currentCanonicalReportRef(input.Snapshot.State, req.Owner, req.TaskID)
	if err != nil || req.CompletionRef != "" && req.CompletionRef != ref {
		r.OwnPending = append(r.OwnPending, "current_Result")
		return r
	}
	projection := resolveCanonicalBuilderResult(input, mode, req.Owner, req.AssignmentID, req.TaskID)
	if projection.Err != nil {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: projection.MainReason, Ref: ref})
		return r
	}
	accepted := projection.Result
	reference := record.Reference()
	r.DeliveryRecorded, r.Accepted, r.ResponsibilityStatus, r.ProfessionalStatus = true, true, "accepted_incomplete", "accepted_unassessed"
	r.AssignmentRecord, r.ResultRef, r.ResultRevision, r.PlanRef = &reference, ref, accepted.ResultRevision, accepted.PlanRef
	for _, evidence := range accepted.ResolvedEvidenceRefs {
		r.ResolvedEvidenceRefs = append(r.ResolvedEvidenceRefs, assignmentresult.ResolvedEvidence{Ref: evidence.Ref, Path: evidence.Path})
	}
	r.DomainRefs = append(r.DomainRefs, ref)
	applyCanonicalProfessionalClosure(&r, record, accepted)
	r.MainCheckpointRef = projection.Checkpoint
	if projection.Integrated && projection.Facts != nil {
		r.Integrated = true
		r.ActualChangedPaths = append([]string{}, projection.Facts.ChangedPaths...)
		r.MainCheckReceipts = append([]assignmentresult.MainCheckFact{}, projection.Facts.Checks...)
		r.DomainRefs = append(r.DomainRefs, projection.Checkpoint)
	} else {
		r.WaitingOn = append(r.WaitingOn, ResponsibilityWaiting{Owner: "main", Reason: projection.MainReason, Ref: projection.Checkpoint})
	}
	return r
}

func applyCanonicalProfessionalClosure(receipt *BuilderReceipt, record assignmentrecord.Record, result assignmentresult.AcceptedResult) {
	if receipt == nil {
		return
	}
	answer := result.Answer
	if record.ClosingContract == nil {
		receipt.OwnPending = append(receipt.OwnPending, "professional_closing_contract_unproven")
		return
	}
	assessment := answer.ClosingAssessment
	if assessment == nil {
		receipt.OwnPending = append(receipt.OwnPending, "professional_closing_contract_unproven")
		return
	}
	if assessment.Disposition == "blocked" {
		receipt.ProfessionalStatus = "incomplete"
	} else if assessment.Disposition == "incomplete" {
		receipt.ProfessionalStatus = "incomplete"
	} else if assessment.Disposition == "complete" && !assignmentresult.ClosingAssessmentComplete(answer) {
		receipt.ProfessionalStatus = "incomplete"
	}
	receipt.OwnPending = append(receipt.OwnPending, canonicalProfessionalClosurePending(record, answer, result)...)
	if len(receipt.OwnPending) == 0 {
		receipt.WorkComplete = true
		receipt.ResponsibilityStatus = "complete"
		receipt.ProfessionalStatus = "complete"
	}
}

func canonicalProfessionalClosurePending(record assignmentrecord.Record, answer assignmentresult.Answer, result assignmentresult.AcceptedResult) []string {
	if record.ClosingContract == nil || answer.ClosingAssessment == nil {
		return []string{"professional_closing_contract_unproven"}
	}
	pending := []string{}
	switch answer.ClosingAssessment.Disposition {
	case "blocked":
		pending = append(pending, "builder_blocked")
	case "incomplete":
		pending = append(pending, "closing_conditions_incomplete")
	case "complete":
		if !assignmentresult.ClosingAssessmentComplete(answer) {
			pending = append(pending, "closing_conditions_incomplete")
		}
	default:
		pending = append(pending, "professional_closing_contract_unproven")
	}
	for _, fact := range result.WorkerCheckFacts {
		if fact.Status == "fail" {
			pending = append(pending, fmt.Sprintf("owned_check_failed:%d", fact.Index))
		}
	}

	return pending
}

func canonicalProgressForTask(input Input, task map[string]any, mode canonicalAssignmentMode, modeErr error) BuilderProgress {
	id := stringValue(task["id"])
	progress := BuilderProgress{State: workstate.Active, Reason: "canonical result/integration not verified", Canonical: true}
	if modeErr != nil || !mode.HasRecord {
		progress.Reason = "canonical_assignment_invalid"
		return progress
	}
	_, err := currentCanonicalReportRef(input.Snapshot.State, mode.Record.OwnerAgentID, id)
	if err != nil {
		return progress
	}
	progress = BuilderProgress{State: workstate.Active, DeliveryRecorded: true, Reason: "canonical result/integration not verified", Canonical: true}
	projection := resolveCanonicalBuilderResult(input, mode, mode.Record.OwnerAgentID, mode.Record.AssignmentID, id)
	progress.EvidenceID = projection.EvidenceID
	if projection.Err != nil {
		if projection.MainReason == "result_evidence_invalid" {
			progress.Reason = "canonical_result_invalid"
		}
		return progress
	}
	if projection.Integrated {
		progress.Integrated = true
		if mode.Record.ClosingContract != nil {
			pending := canonicalProfessionalClosurePending(projection.Record, projection.Result.Answer, projection.Result)
			if len(pending) > 0 {
				progress.Reason = "professional_closure_incomplete:" + strings.Join(pending, ",")
				return progress
			}
		}
		progress.State = workstate.Done
		progress.Integrated = true
		progress.Reason = "current canonical Result and Main checkpoint verified"
	}
	return progress
}
