package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Authoring projection of the original independent-verification dispatch
// arguments. Ownership and verification identities remain original Runtime facts.
type repairVerifierDispatchChoice struct {
	AssignmentID            string `json:"assignment_id"`
	IndependentVerification bool   `json:"independent_verification"`
	AgentID                 string `json:"agent_id,omitempty"`
}

func repairVerifierDispatchCurrent(state map[string]any) bool {
	if stringValue(mapFieldCLI(state, "lifecycle")["state"]) != "bug_resolution" {
		return false
	}
	status := stringValue(repairPointerCLI(state)["status"])
	return status == "planning" || status == "reproducing" || status == "targeted_reverification"
}

func repairVerifierDispatchPaths(root string, state map[string]any) []string {
	if !repairVerifierDispatchCurrent(state) {
		return nil
	}
	path, err := repairLeadQuestionnairePath(root, state, "independent-verifier-dispatch.json")
	if err != nil {
		return nil
	}
	return []string{path}
}

func repairVerifierDispatchSelection(state map[string]any, row repairVerifierDispatchChoice) (repairDispatchChoice, string) {
	choice := repairDispatchChoice{AssignmentID: repair.VerificationAssignmentID(row.AssignmentID), RoleFamily: "test-builder", AgentID: row.AgentID}
	owner := repairDispatchOwner(state, choice)
	if row.AgentID == "" && stringMapCLI(repairPointerCLI(state)["assignment_owners"])[choice.AssignmentID] == "" {
		owner = strings.Replace(owner, "builder-s9-", "verifier-s9-", 1)
	}
	return choice, owner
}

func repairVerifierDispatchContext(root string, state map[string]any, assignmentID, owner string) string {
	context := strings.Replace(repairDispatchRegisteredContext(root, state, assignmentID, owner), "Registered Builder", "Registered independent verifier", 1)
	context = strings.Replace(context, "platform spawn/resume remains Main's responsibility. ", "registration retained. ", 1)
	if stringValue(repairPointerCLI(state)["status"]) != "targeted_reverification" {
		return context + "Registration is complete; wait for the original Final integration and ChangeImpact before dispatching this verifier. The original Targeted responsibility prepares its answer when ready. "
	}
	path, err := repairTargetedQuestionnairePath(root, state, assignmentID)
	if err == nil {
		info, statErr := os.Stat(path)
		if statErr == nil && info.Mode().IsRegular() {
			return context + "Original Targeted answer: " + path + ". Main must actually spawn/resume the registered independent verifier with its real TASK and manifest context, carrying this exact answer path in the native prompt. The verifier saves its ready original answer for PreToolUse reception, with successful PostToolUse and Stop/SubagentStop fallback; no final message or returned reference is required for reception. "
		}
		context += "Original Targeted answer is not prepared as a regular file at " + path + ". "
	} else {
		context += "Original Targeted answer path is unavailable: " + err.Error() + ". "
	}
	if division, pathErr := repairLeadQuestionnairePath(root, state, "independent-verifier-dispatch.json"); pathErr == nil {
		context += "Restore the named source/filesystem failure, then save the registered division again at " + division + "; the original registration is reused and its Targeted answer is prepared. "
	}
	return context
}

func stageRepairVerifierDispatchContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) || !repairVerifierDispatchCurrent(snapshot.State) {
		return ""
	}
	paths := repairVerifierDispatchPaths(root, snapshot.State)
	if len(paths) == 0 {
		return "S9 independent verifier division cannot resolve its current Case/Session/Plan binding; no identity or path was guessed."
	}
	path := paths[0]
	plan, err := repairDispatchObservedPlan(root, snapshot.State)
	if err != nil {
		return fmt.Sprintf("S9 independent verifier division at %s could not read the original Plan: %v", path, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "S9 Main independent verifier division: %s. ", path)
	if questionnaireMissing(path) {
		b.WriteString("Main/system must prepare this draft when the current responsibility starts. ")
	}
	pointer := repairPointerCLI(snapshot.State)
	contractPath := resolveRootPath(root, stringValue(pointer["contract_ref"]))
	fmt.Fprintf(&b, "Approved Contract: %s; original Plan: %s. ", contractPath, resolveRootPath(root, stringValue(pointer["plan_ref"])))
	owners := stringMapCLI(pointer["assignment_owners"])
	for _, assignment := range plan.Assignments {
		b.WriteString(repairDispatchAssignmentSummary(plan, assignment, owners[assignment.AssignmentID]))
		verificationID := repair.VerificationAssignmentID(assignment.AssignmentID)
		if owner := owners[verificationID]; owner != "" {
			b.WriteString(repairVerifierDispatchContext(root, snapshot.State, verificationID, owner))
		} else if owners[assignment.AssignmentID] == "" {
			b.WriteString("This anchor is awaiting its ordinary Builder selection; select its Builder first, before registering an independent verifier. ")
		}
	}
	b.WriteString("Both Builder and verifier division forms intentionally use the same Builder assignment_id as their anchor. Do not write a verifier assignment ID: saving derives its separate verification assignment and TASK automatically after Builder selection. Keep supplied assignment_id; set independent_verification=true on the selected anchor, others false. agent_id is optional and system-generated when omitted. role_family=test-builder, with no product writes. One independent verifier can cover all current Contract assertions; a verifier is not required for every Builder row. Select someone who did not implement or merge the repair. Save to register the identity; dispatch only when the current Targeted responsibility and its exact original answer are ready, as described above. Follow the original Targeted result questionnaire; no Builder PlanReport or separate generic report is required.")
	return b.String()
}

func repairVerifierDispatchHasProfessionalContent(path string, data []byte) bool {
	if filepath.Base(path) != "independent-verifier-dispatch.json" {
		return false
	}
	var rows []repairVerifierDispatchChoice
	if err := json.Unmarshal(data, &rows); err != nil {
		return true
	}
	for _, row := range rows {
		if row.IndependentVerification || strings.TrimSpace(row.AgentID) != "" {
			return true
		}
	}
	return false
}

func runSavedRepairVerifierDispatch(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	store := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl"))
	snapshot, err := store.Snapshot()
	if err != nil {
		return false, 0
	}
	paths := repairVerifierDispatchPaths(root, snapshot.State)
	if len(paths) != 1 || !samePath(path, paths[0]) {
		return false, 0
	}
	feedback := func(message string) (bool, int) { return true, repairLeadFeedback(stdout, path, message) }
	data, err := os.ReadFile(path)
	if err != nil {
		return feedback(fmt.Sprintf("S9 independent verifier division could not read %s: %v", path, err))
	}
	var rows []repairVerifierDispatchChoice
	if err := json.Unmarshal(data, &rows); err != nil {
		return feedback("S9 independent verifier division must be the supplied array of assignment_id, independent_verification and optional agent_id: " + err.Error())
	}
	plan, err := repairDispatchPlan(root, snapshot.State)
	if err != nil {
		return feedback("S9 independent verifier division could not read the original Plan: " + err.Error())
	}
	known, seen := map[string]bool{}, map[string]bool{}
	for _, assignment := range plan.Assignments {
		known[assignment.AssignmentID] = true
	}
	for _, row := range rows {
		if !known[row.AssignmentID] || seen[row.AssignmentID] {
			return feedback("S9 independent verifier division contains an unknown or repeated supplied assignment_id: " + row.AssignmentID)
		}
		seen[row.AssignmentID] = true
		if row.IndependentVerification {
			choice, owner := repairVerifierDispatchSelection(snapshot.State, row)
			if stringMapCLI(repairPointerCLI(snapshot.State)["assignment_owners"])[choice.AssignmentID] != "" {
				if err := repairDispatchSameRegistered(snapshot.State, choice, owner); err != nil {
					return feedback("S9 independent verifier division conflict: " + err.Error())
				}
			}
		}
	}
	var result strings.Builder
	selected := false
	for _, row := range rows {
		if !row.IndependentVerification {
			continue
		}
		selected = true
		latest, err := store.Snapshot()
		if err != nil {
			return feedback(result.String() + " Current Runtime is unavailable: " + err.Error())
		}
		choice, owner := repairVerifierDispatchSelection(latest.State, row)
		if stringMapCLI(repairPointerCLI(latest.State)["assignment_owners"])[choice.AssignmentID] != "" {
			if err := repairDispatchSameRegistered(latest.State, choice, owner); err != nil {
				return feedback(result.String() + " S9 independent verifier division conflict: " + err.Error())
			}
			result.WriteString("Existing original verification dispatch reused without a Runtime write. ")
		} else {
			var commandOut, commandErr bytes.Buffer
			if code := runRuntimeRepairDispatch([]string{"--root", root, "--assignment-id", row.AssignmentID, "--agent-id", owner, "--independent-verification"}, &commandOut, &commandErr); code != 0 {
				return feedback(result.String() + fmt.Sprintf(" Original independent dispatch for %s did not register: %s %s. Retain this answer and save again after resolving the named source; existing registrations will be reused.", row.AssignmentID, commandErr.String(), commandOut.String()))
			}
			latest, err = store.Snapshot()
			if err != nil {
				return feedback(result.String() + " Original verification dispatch completed but its Runtime projection is unavailable: " + err.Error())
			}
		}
		if stringValue(repairPointerCLI(latest.State)["status"]) == "targeted_reverification" {
			selections, err := repairTargetedSelections(root, latest.State)
			if err != nil {
				return feedback(result.String() + " Independent Targeted source: " + err.Error() + ". Restore the named source and save this same registered division again; its original registration is reused and Targeted preparation retried.")
			}
			for _, selection := range selections {
				if selection.performingAssignmentID != choice.AssignmentID || selection.verifierOwner != owner {
					continue
				}
				answerPath, _, err := prepareRepairTargetedQuestionnaire(root, latest.State, selection)
				if err != nil {
					return feedback(result.String() + " Original verifier dispatch recorded; Targeted questionnaire preparation: " + err.Error() + ". Restore the named source/filesystem failure and save this same division again; the original registration will be reused.")
				}
				result.WriteString(" Independent verifier answer: " + answerPath + ". ")
			}
		}
		result.WriteString(repairVerifierDispatchContext(root, latest.State, choice.AssignmentID, owner))
	}
	if !selected {
		return feedback("S9 independent verifier division has no selection yet; this normal pending draft registered no verifier and did not change Runtime.")
	}
	result.WriteString("This identity covers the current Contract assertions. Follow the readiness or original preparation recovery above; no native agent was started by this save.")
	return feedback(result.String())
}
