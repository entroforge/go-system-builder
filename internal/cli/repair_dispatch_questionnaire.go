package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// This is an authoring projection of the existing dispatch arguments. The
// original RepairPlan and assignment Register Writer own all dispatch facts.
type repairDispatchChoice struct {
	AssignmentID  string `json:"assignment_id"`
	RoleFamily    string `json:"role_family"`
	AgentID       string `json:"agent_id,omitempty"`
	CheckLocation string `json:"check_location,omitempty"`
}

func repairDispatchCurrent(state map[string]any) bool {
	life := mapFieldCLI(state, "lifecycle")
	status := stringValue(repairPointerCLI(state)["status"])
	return stringValue(life["state"]) == "bug_resolution" && (status == "planning" || status == "reproducing" || status == "repairing")
}

func repairDispatchPaths(root string, state map[string]any) []string {
	if !repairDispatchCurrent(state) {
		return nil
	}
	path, err := repairLeadQuestionnairePath(root, state, "builder-dispatch.json")
	if err != nil {
		return nil
	}
	return []string{path}
}

func repairDispatchPlan(root string, state map[string]any) (repair.RepairPlan, error) {
	ref, err := repairPointerRef(repairPointerCLI(state), "plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return repair.RepairPlan{}, err
	}
	return repair.ValidateRepairPlan(root, ref)
}

func repairDispatchObservedPlan(root string, state map[string]any) (repair.RepairPlan, error) {
	pointer := repairPointerCLI(state)
	document, err := s9ReadListenerDocument(root, stringValue(pointer["plan_ref"]), stringValue(pointer["plan_sha256"]))
	if err != nil {
		return repair.RepairPlan{}, err
	}
	data, err := json.Marshal(document)
	var plan repair.RepairPlan
	if err == nil {
		err = json.Unmarshal(data, &plan)
	}
	return plan, err
}

func stageRepairDispatchContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) || !repairDispatchCurrent(snapshot.State) {
		return ""
	}
	if stringValue(repairPointerCLI(snapshot.State)["status"]) == "repairing" {
		return stageRepairExecutionPrepareContext(root, request, snapshot)
	}
	paths := repairDispatchPaths(root, snapshot.State)
	if len(paths) == 0 {
		return "S9 Main Builder division cannot prepare its questionnaire: the current Case, Session or Plan binding is missing. No Assignment or answer path was guessed."
	}
	path := paths[0]
	plan, err := repairDispatchObservedPlan(root, snapshot.State)
	if err != nil {
		return fmt.Sprintf("S9 Main Builder division at %s could not read the current RepairPlan: %v", path, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "S9 Main Builder division: %s. ", path)
	if questionnaireMissing(path) {
		b.WriteString("The questionnaire is not prepared yet; Main/system must prepare it when this responsibility starts. This event does not create a draft. ")
	}
	pointer := repairPointerCLI(snapshot.State)
	fmt.Fprintf(&b, "Approved Contract: %s; original RepairPlan: %s. ", resolveRootPath(root, stringValue(pointer["contract_ref"])), resolveRootPath(root, stringValue(pointer["plan_ref"])))
	for _, assignment := range plan.Assignments {
		owner := stringMapCLI(pointer["assignment_owners"])[assignment.AssignmentID]
		b.WriteString(repairDispatchAssignmentSummary(plan, assignment, owner))
		if owner != "" {
			b.WriteString(repairDispatchRegisteredContext(root, snapshot.State, assignment.AssignmentID, owner))
		} else {
			b.WriteString("This responsibility is awaiting Main's professional Builder selection; no owner has been selected yet. ")
		}
	}
	b.WriteString("Keep supplied assignment_id; choose role_family from backend-builder, frontend-builder or test-builder. agent_id is an optional engineering responsibility name, system-generated when omitted; it is not a Claude platform ID. Empty role rows stay pending. Save selected rows and pass the exact prepared brief to Claude Code native Agent with worktree isolation. The Builder saves its professional plan before implementation, continues work and self-checks, then saves its Result. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. No workspace prepare/launch or check_location selection is required for new native work; Main owns integration and actual necessary checks. Existing frozen executions retain their original recovery path.")
	return b.String()
}

func repairDispatchAssignmentSummary(plan repair.RepairPlan, assignment repair.RepairAssignment, owner string) string {
	if owner == "" {
		owner = "unselected"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Assignment %s owner=%s scope=%v. ", assignment.AssignmentID, owner, assignment.Scope)
	for _, id := range assignment.UnitIDs {
		for _, unit := range plan.Units {
			if unit.ID == id {
				fmt.Fprintf(&b, "Unit %s: %s. ", id, unit.Description)
			}
		}
	}
	if len(assignment.DependsOn) > 0 {
		fmt.Fprintf(&b, "depends_on=%v. ", assignment.DependsOn)
	}
	if len(assignment.ResourceLocks) > 0 {
		fmt.Fprintf(&b, "resource_locks=%v. ", assignment.ResourceLocks)
	}
	return b.String()
}

func repairDispatchHasProfessionalContent(path string, data []byte) bool {
	if filepath.Base(path) != "builder-dispatch.json" {
		return false
	}
	var rows []repairDispatchChoice
	if err := json.Unmarshal(data, &rows); err != nil {
		return true // Let the original save path report malformed author content.
	}
	for _, row := range rows {
		if strings.TrimSpace(row.RoleFamily) != "" || strings.TrimSpace(row.AgentID) != "" || strings.TrimSpace(row.CheckLocation) != "" {
			return true
		}
	}
	return false
}

func repairDispatchOwner(state map[string]any, row repairDispatchChoice) string {
	if row.AgentID != "" {
		return row.AgentID
	}
	pointer := repairPointerCLI(state)
	if owner := stringMapCLI(pointer["assignment_owners"])[row.AssignmentID]; owner != "" {
		return owner
	}
	digest := sha256.Sum256([]byte(stringValue(pointer["session_id"]) + "\x00" + stringValue(pointer["plan_sha256"])))
	return fmt.Sprintf("builder-s9-%s-%x", dispatchSlug(row.AssignmentID), digest[:6])
}

// Read only the existing Runtime projection here. Physical manifest/TASK and
// source validation belong to the original dispatch and PlanReport consumers.
func repairDispatchRegisteredContext(root string, state map[string]any, assignmentID, owner string) string {
	entities := mapFieldCLI(state, "entities")
	for _, agent := range objectSliceAny(entities["agents"]) {
		if stringValue(agent["id"]) != owner {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Registered Builder %s role=%s assignment=%s; platform spawn/resume remains Main's responsibility. Agent context: %s. ", owner, stringValue(agent["role"]), assignmentID, resolveRootPath(root, stringValue(agent["prompt_ref"])))
		for _, task := range objectSliceAny(entities["tasks"]) {
			for _, id := range stringSliceAny(agent["task_ids"]) {
				if stringValue(task["id"]) == id {
					fmt.Fprintf(&b, "TASK %s: %s. ", id, resolveRootPath(root, stringValue(task["path"])))
				}
			}
		}
		for _, team := range objectSliceAny(entities["teams"]) {
			if stringValue(team["id"]) == stringValue(agent["team_id"]) {
				fmt.Fprintf(&b, "Manifest: %s. ", resolveRootPath(root, stringValue(team["manifest_ref"])))
			}
		}
		return b.String()
	}
	return fmt.Sprintf("Assignment %s has registered owner %s but its original Runtime Agent projection is missing; preserve the original dispatch source. ", assignmentID, owner)
}

func repairDispatchSameRegistered(state map[string]any, row repairDispatchChoice, owner string) error {
	existing := stringMapCLI(repairPointerCLI(state)["assignment_owners"])[row.AssignmentID]
	if existing != owner {
		return fmt.Errorf("Assignment %s is already registered to %s; the changed owner %s was not applied", row.AssignmentID, existing, owner)
	}
	for _, agent := range objectSliceAny(mapFieldCLI(state, "entities")["agents"]) {
		if stringValue(agent["id"]) == owner {
			if stringValue(agent["role"]) != row.RoleFamily {
				return fmt.Errorf("Assignment %s is already registered with role %s; the changed role %s was not applied", row.AssignmentID, stringValue(agent["role"]), row.RoleFamily)
			}
			return nil
		}
	}
	return fmt.Errorf("Assignment %s has owner %s but its original Runtime Agent is missing", row.AssignmentID, owner)
}

func runSavedRepairDispatch(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	return runRepairDispatchQuestionnaire(root, request, stdout, stderr, true)
}

// Legacy registration-only callers retain this entry. Normal questionnaire
// saves, including observed scripts, use runSavedRepairDispatch.
func runObservedRepairDispatch(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	return runRepairDispatchQuestionnaire(root, request, stdout, stderr, false)
}

func runRepairDispatchQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer, prepareExecution bool) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	store := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl"))
	snapshot, err := store.Snapshot()
	if err != nil {
		return false, 0
	}
	paths := repairDispatchPaths(root, snapshot.State)
	if len(paths) != 1 || !samePath(path, paths[0]) {
		return false, 0
	}
	feedback := func(message string) (bool, int) { return true, repairLeadFeedback(stdout, path, message) }
	data, err := os.ReadFile(path)
	if err != nil {
		return feedback(fmt.Sprintf("S9 Main Builder division could not read %s: %v", path, err))
	}
	var rows []repairDispatchChoice
	if err := json.Unmarshal(data, &rows); err != nil {
		return feedback("S9 Builder division must be the supplied array of assignment_id, role_family and optional agent_id: " + err.Error())
	}
	plan, err := repairDispatchPlan(root, snapshot.State)
	if err != nil {
		return feedback("S9 Builder division could not read the current original Plan: " + err.Error())
	}
	known, seen := map[string]bool{}, map[string]bool{}
	for _, assignment := range plan.Assignments {
		known[assignment.AssignmentID] = true
	}
	for _, row := range rows {
		if !known[row.AssignmentID] || seen[row.AssignmentID] {
			return feedback("S9 Builder division contains an unknown or repeated supplied assignment_id: " + row.AssignmentID)
		}
		seen[row.AssignmentID] = true
		if row.RoleFamily != "" && row.RoleFamily != "backend-builder" && row.RoleFamily != "frontend-builder" && row.RoleFamily != "test-builder" {
			return feedback("S9 role_family must be backend-builder, frontend-builder or test-builder; invalid selection for " + row.AssignmentID)
		}
		if row.CheckLocation != "" && row.CheckLocation != "main" && row.CheckLocation != "worker" {
			return feedback("S9 check_location must be main or worker, or empty before execution; invalid selection for " + row.AssignmentID)
		}
		if err := repairExecutionCheckLocationConflict(snapshot.State, row); err != nil {
			return feedback("S9 Builder division conflict: " + err.Error())
		}
		if row.RoleFamily != "" && stringMapCLI(repairPointerCLI(snapshot.State)["assignment_owners"])[row.AssignmentID] != "" {
			if err := repairDispatchSameRegistered(snapshot.State, row, repairDispatchOwner(snapshot.State, row)); err != nil {
				return feedback("S9 Builder division conflict: " + err.Error())
			}
		}
	}
	var result strings.Builder
	selected := false
	for _, row := range rows {
		if row.RoleFamily == "" && row.CheckLocation == "" {
			continue
		}
		selected = true
		latest, err := store.Snapshot()
		if err != nil {
			return feedback(result.String() + " Current Runtime could not be read: " + err.Error())
		}
		owner := repairDispatchOwner(latest.State, row)
		if row.RoleFamily == "" {
			for _, agent := range objectSliceAny(mapFieldCLI(latest.State, "entities")["agents"]) {
				if stringValue(agent["id"]) == owner {
					row.RoleFamily = stringValue(agent["role"])
				}
			}
			if row.RoleFamily == "" {
				result.WriteString("Assignment " + row.AssignmentID + " is still awaiting its Builder role selection; check_location alone did not dispatch it. ")
				continue
			}
		}
		if stringMapCLI(repairPointerCLI(latest.State)["assignment_owners"])[row.AssignmentID] != "" {
			if err := repairDispatchSameRegistered(latest.State, row, owner); err != nil {
				return feedback(result.String() + " S9 Builder division conflict: " + err.Error())
			}
			result.WriteString("Existing original dispatch reused without a Runtime write. ")
		} else {
			var commandOut, commandErr bytes.Buffer
			if code := runRuntimeRepairDispatch([]string{"--root", root, "--assignment-id", row.AssignmentID, "--agent-id", owner, "--role-family", row.RoleFamily}, &commandOut, &commandErr); code != 0 {
				return feedback(result.String() + fmt.Sprintf(" Original dispatch for %s did not register: %s %s. Keep this answer and save again after resolving the named source; already registered rows will be reused.", row.AssignmentID, commandErr.String(), commandOut.String()))
			}
			latest, err = store.Snapshot()
			if err != nil {
				return feedback(result.String() + " Original dispatch completed but its Runtime projection could not be read: " + err.Error())
			}
		}
		result.WriteString(repairDispatchRegisteredContext(root, latest.State, row.AssignmentID, owner))
		status := stringValue(repairPointerCLI(latest.State)["status"])
		if prepareExecution && runtime.PacketEnabled(latest.State) && (status == "planning" || status == "reproducing" || status == "repairing" && len(nativeS9PlanPackets(root, latest.State)) > 0) {
			brief, err := repairDispatchPlanReportBrief(root, request, latest.State, row.AssignmentID, owner)
			result.WriteString(brief)
			if err != nil {
				return feedback(result.String() + fmt.Sprintf(" Original PlanReport preparation for Builder %s remains pending: %v. Its successful dispatch was retained; save this same division answer after resolving the named original source.", owner, err))
			}
		}
		if prepareExecution && stringValue(repairPointerCLI(latest.State)["status"]) == "repairing" {
			result.WriteString(prepareRepairExecutionFromChoice(root, latest.State, row, owner))
		}
	}
	if !selected {
		return feedback("S9 Builder division has no selected role_family yet; this normal pending draft registered no Builder and did not change Runtime.")
	}
	if stringValue(repairPointerCLI(snapshot.State)["status"]) == "repairing" {
		result.WriteString("Main continues the original native Agent or handles the returned delivery with the supplied exact references; registration did not start an Agent. Preserve real dependencies and frozen legacy recoveries.")
	} else if prepareExecution && runtime.PacketEnabled(snapshot.State) {
		result.WriteString("Main now uses Claude Code native Agent with the exact prepared S9 brief and native worktree isolation. Each Builder saves its own professional plan and Result; Stop receives them. Main does not write professional answers on the Builder's behalf. No execution workspace or Claude session was prepared.")
	} else {
		result.WriteString("Main must obtain each exact Main-root PlanReport path before the actual platform spawn/resume. Save the division answer through its normal questionnaire consumer to prepare missing original questions. Each Builder saves its own professional answer. Registration did not start a native agent or prepare an execution workspace.")
	}
	return feedback(result.String())
}

// Explicit division saves deliver the original pre-execution questionnaire in
// the same callback. Passive observations never call this named issuer path.
func repairDispatchPlanReportBrief(root string, request policy.Input, state map[string]any, assignmentID, owner string) (string, error) {
	find := func(state map[string]any) (s9ListenerQuestionnaire, bool, error) {
		packets, err := s9ListenerQuestionnairesContext(contextForHook(request), root, state, owner, false)
		if err != nil {
			return s9ListenerQuestionnaire{}, false, err
		}
		for _, packet := range packets {
			if packet.binding.RepairAssignmentID == assignmentID {
				return packet, true, nil
			}
		}
		return s9ListenerQuestionnaire{}, false, nil
	}
	packet, found, err := find(state)
	if err != nil {
		return "", err
	}
	accepted := false
	if found {
		fact, err := s9ListenerAcceptance(root, state, packet)
		if err != nil {
			return "", err
		}
		accepted = fact.kind == "plan"
	}
	if !accepted && (!found || !s8QuestionnaireFileExists(resolveRootPath(root, packet.view)) || !s8QuestionnaireFileExists(resolveRootPath(root, packet.answer))) {
		service := &submission.Service{
			Root:        root,
			StatePath:   filepath.Join(root, ".claude", "loop-state.json"),
			JournalPath: filepath.Join(root, ".claude", "loop-events.jsonl"),
			Validator:   semantic.RuntimeCandidateValidator{},
		}
		if _, err := service.IssueRepairPlanReports(contextForHook(request), owner); err != nil {
			return "", err
		}
		latest, err := runtime.NewStore(service.StatePath, service.JournalPath).WithContext(contextForHook(request)).Snapshot()
		if err != nil {
			return "", err
		}
		packet, found, err = find(latest.State)
		if err != nil {
			return "", err
		}
	}
	if !found {
		return "", fmt.Errorf("the original issuer returned no current PlanReport for Assignment %s owner=%s", assignmentID, owner)
	}
	// New responsibilities carry professional material directly into native
	// Agent execution. Existing installed Workers retain their frozen route.
	_, execution := repairExecutionForChoice(state, repairDispatchChoice{AssignmentID: assignmentID, AgentID: owner})
	if len(execution) == 0 || execution["purpose"] == "native_delivery" {
		if stringValue(execution["delivery_ref"]) != "" || execution["status"] == "complete" {
			return prepareRepairExecutionFromChoice(root, state, repairDispatchChoice{AssignmentID: assignmentID, AgentID: owner}, owner), nil
		}
		latest, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(contextForHook(request)).Snapshot()
		if err != nil {
			return "", err
		}
		original, found, err := runtime.FindRepairPlanReportPacketContext(runtime.WithPacketBodyRoot(contextForHook(request), root), latest.State, packet.id)
		if err != nil || !found {
			return "", fmt.Errorf("original S9 Plan question is missing: %v", err)
		}
		return prepareNativeS9Brief(contextForHook(request), root, original)
	}
	view, answer := resolveRootPath(root, packet.view), resolveRootPath(root, packet.answer)
	paths := fmt.Sprintf("Builder %s Main-root PlanReport view: %s; exact professional answer: %s. ", owner, view, answer)
	if accepted {
		return paths + "Your PlanReport is recorded; do not repeat it or save the division answer again. Main normally prepares your actual Worker location and supplies the exact RepairResult path; your work does not wait for all Builders’ plans. ", nil
	}
	return paths + fmt.Sprintf("Frozen execution task brief for %s: Read %s. Inspect the actual requirements and product files, perform real prechecks, and record pass/fail/blocked with evidence and unfinished reasons; personally save your PlanReport at %s. After your own Plan is recorded, Main normally prepares your actual Worker location and supplies its exact RepairResult path; do not wait for all Builders’ plans or save the division answer again. Implement or complete assigned tests in that Worker, self-check, and personally save the supplied RepairResult. Professional questionnaires and actual evidence are separate from product implementation scope; save them at their supplied paths. Main must not save your answer on your behalf. ", owner, view, answer), nil
}
