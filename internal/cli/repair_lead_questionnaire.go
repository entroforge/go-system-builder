package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// stageRepairLeadQuestionnaireContext gives Main or the dispatched independent
// verifier the exact existing S9 DTO path for the current responsibility.
func stageRepairLeadQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	lifecycle := mapFieldCLI(snapshot.State, "lifecycle")
	if stringValue(lifecycle["state"]) != "bug_resolution" {
		return ""
	}
	if !mainQuestionnaireViewer(request) && request.EffectiveAgentID() == "" {
		return ""
	}
	phase, phaseErr := observedRepairLeadPhase(root, snapshot.State)
	if phaseErr != nil {
		return "S9 current TargetedReverification obligations cannot be resolved: " + phaseErr.Error() + repairLeadExistingQuestionnaires(root, snapshot.State)
	}
	owner := request.EffectiveAgentID()
	main := owner == "" || owner == "Main"

	switch phase {
	case "repair_readback":
		if !main {
			return ""
		}
		state := snapshot.State
		pointer := repairPointerCLI(state)
		contract := repair.ContractRef{Path: stringValue(pointer["contract_ref"])}
		session := repair.ArtifactRef{Path: stringValue(pointer["path"])}
		plan := repair.ArtifactRef{Path: stringValue(pointer["plan_ref"])}
		if contract.Path == "" || session.Path == "" || plan.Path == "" {
			return "S9 repair_readback is missing a current approved Contract, RepairSession, or RepairPlan reference. Main/system must restore the original approved-Contract entry preparation; do not invent Session/Plan IDs or fall back to legacy startup commands."
		}
		contractPath, _ := absoluteQuestionnairePath(root, contract.Path)
		sessionPath, _ := absoluteQuestionnairePath(root, session.Path)
		planPath, _ := absoluteQuestionnairePath(root, plan.Path)
		return fmt.Sprintf("S9 repair_readback: review the approved Contract at %s, current RepairSession at %s, and Contract-derived RepairPlan at %s. Session/Plan were produced by their original Runtime APIs; the current RepairAssignments and their exact Builder PlanReport/RepairResult questions are delivered by the original listeners. Main owns dispatch and integration; do not hand-author another plan.", contractPath, sessionPath, planPath)

	case "impact_reconciliation":
		if !main {
			return ""
		}
		path, err := repairLeadQuestionnairePath(root, snapshot.State, "change-impact.json")
		if err != nil {
			return "S9 ChangeImpact questionnaire path is unavailable from current Runtime: " + err.Error()
		}
		if questionnaireMissing(path) {
			return "S9 ChangeImpact questionnaire is not prepared at " + path + "; Main/system resumes its original preparation with " + repairImpactPreparationRecovery(root) + ". Accepted contributions remain recorded; do not resubmit Builder work or hand-author required_reverification_ids."
		}
		return "S9 ChangeImpact answer: " + path + ". Runtime/REQ/generation, source Case, the accepted contributions, and required_reverification_ids (system-derived obligation IDs stable per RepairSession, not assertion IDs) are supplied from the original sources. Keep these supplied IDs; assertion_results in the separate Targeted answer carry the Contract assertion IDs. Complete the actual change types and professional impact/evidence dispositions with their rationales, then save this exact request. If the supplied contribution has no product changes, change_types=[] and decisions=[] are valid; do not invent changed files or a change type. The original CreateChangeImpact and CommitChangeImpact consumers run on save." + repairLeadPreviousQuestionnaires(path) + repairLeadAnswerHelp(root, snapshot.State, false)

	case "targeted_reverification":
		pointer := repairPointerCLI(snapshot.State)
		blockedResume := stringValue(pointer["status"]) == "blocked" && stringValue(pointer["failure_route"]) == "blocked"
		selections, selectionErr := repairTargetedSelections(root, snapshot.State)
		if selectionErr != nil {
			return "S9 independent TargetedReverification source is unavailable: " + selectionErr.Error() + repairLeadExistingQuestionnaires(root, snapshot.State)
		}
		missing, missingErr := repairObservedMissingTargetedIDs(root, repairPointerCLI(snapshot.State))
		if missingErr != nil {
			return "S9 independent TargetedReverification obligations are unavailable: " + missingErr.Error() + repairLeadExistingQuestionnaires(root, snapshot.State)
		}
		if main {
			if len(selections) == 0 {
				return "S9 targeted_reverification has no current independently dispatched verifier in Runtime assignment_owners. Main must resolve the original repair dispatch; no verifier identity or answer path was guessed."
			}
			if err := repairTargetedRequestMappingError(selections, missing); err != nil {
				return "S9 targeted_reverification has no current independently assigned verifier: " + err.Error() + ". Main must select and actually notify the independent verifier using the existing verifier division answer."
			}
			var b strings.Builder
			for _, selection := range repairTargetedOutstandingSelections(selections, missing) {
				path, pathErr := repairTargetedQuestionnairePath(root, snapshot.State, selection.performingAssignmentID, contextForHook(request))
				if pathErr != nil {
					fmt.Fprintf(&b, "S9 independent verifier %s assignment=%s has no resolvable answer path: %v\n", selection.verifierOwner, selection.performingAssignmentID, pathErr)
					continue
				}
				if questionnaireMissing(path) {
					fmt.Fprintf(&b, "S9 independent verifier %s assignment=%s: TargetedReverificationRequest is not prepared at %s; Main/system must prepare this original request when the verifier responsibility starts.\n", selection.verifierOwner, selection.performingAssignmentID, path)
					continue
				}
				readOnlyNote := repairLeadPreviousQuestionnaires(path)
				if blockedResume {
					fmt.Fprintf(&b, "S9 independent verifier %s assignment=%s: resolve the recorded environmental blocker, then update the same TargetedReverificationRequest at %s with the actual assertion observations, results, evidence and any unfinished-work reason. Save this exact original answer; PreToolUse invokes the original Resume/Create/Commit flow for a ready original answer, with successful PostToolUse and Stop/SubagentStop fallback; Main does not author the independent result.%s\n", selection.verifierOwner, selection.performingAssignmentID, path, readOnlyNote)
				} else {
					fmt.Fprintf(&b, "S9 independent verifier %s assignment=%s: fill and save its TargetedReverificationRequest at %s and keep the original answer path for automatic reception via PreToolUse, with successful PostToolUse and Stop/SubagentStop fallback; report results to Main when finished; ready original answers use the same natural-event listener. Main does not author the independent result.%s\n", selection.verifierOwner, selection.performingAssignmentID, path, readOnlyNote)
				}
			}
			return strings.TrimSpace(b.String()) + repairLeadAnswerHelp(root, snapshot.State, true)
		}
		for _, selection := range repairTargetedOutstandingSelections(selections, missing) {
			if selection.verifierOwner != owner {
				continue
			}
			if err := repairTargetedRequestMappingError(selections, missing); err != nil {
				return "S9 TargetedReverification assignment cannot be mapped to the current ChangeImpact obligation: " + err.Error() + ". Main must reconcile the original source; no answer was claimed."
			}
			path, err := repairTargetedQuestionnairePath(root, snapshot.State, selection.performingAssignmentID, contextForHook(request))
			if err != nil {
				return fmt.Sprintf("S9 independent verifier %s assignment=%s has no resolvable answer path: %v", owner, selection.performingAssignmentID, err)
			}
			if questionnaireMissing(path) {
				return "S9 independent TargetedReverification answer is not prepared at " + path + "; Main/system must prepare this original request when the verifier responsibility starts."
			}
			readOnlyNote := repairLeadPreviousQuestionnaires(path)
			if blockedResume {
				return fmt.Sprintf("S9 independent verifier %s assignment=%s: resolve the recorded environmental blocker, then update the same TargetedReverificationRequest at %s with the actual assertion observations, results, evidence and any unfinished-work reason, and save. Save this exact original answer; PreToolUse invokes the original Resume/Create/Commit flow for a ready original answer, with successful PostToolUse and Stop/SubagentStop fallback; Main does not author the independent result.%s", owner, selection.performingAssignmentID, path, readOnlyNote) + repairLeadAnswerHelp(root, snapshot.State, true)
			}
			return fmt.Sprintf("S9 independent verifier %s assignment=%s: record the actual assertion observations, results, evidence, verdict, and any non-pass reason in %s, then save the ready original answer. PreToolUse listens for it; successful PostToolUse and Stop/SubagentStop provide fallback. Report results to Main when finished. The request is bound to the dispatched repair and verifier assignments; the original TargetedReverification consumer checks the independent work.%s", owner, selection.performingAssignmentID, path, readOnlyNote) + repairLeadAnswerHelp(root, snapshot.State, true)
		}
		return ""

	case "ready_for_full_review":
		if !main {
			return ""
		}
		return repairHandoffProjectionContext(root, snapshot.State)
	}
	return ""
}

// Help is a read-only projection of the existing DTO/source files, never an
// answer template with professional conclusions or another submission envelope.
func repairLeadAnswerHelp(root string, state map[string]any, targeted bool) string {
	var b strings.Builder
	pointer := repairPointerCLI(state)
	b.WriteString("\nFill professional fields only; system supplies machine bindings, IDs, times and known artifacts. Examples show shape, not executed evidence or a suggested verdict.\n")
	if targeted {
		b.WriteString(`Targeted fields: assertion_results=[{"assertion_id":"<supplied slot>","result":"blocked","evidence_refs":["logs/actual-blocker.txt"]}]; result=pass|fail|blocked|scope_changed. Record actual observations and evidence for every necessary supplied Contract assertion; identify each unfinished observation or blocker honestly. failure_class for a non-pass result is fail_same_cause|fail_new_cause|blocked|scope_changed|stale; keep it empty for pass. Assertion result choices are pass|fail|blocked. continuity_reason and scope_compliance are optional legacy metadata, not additional required reports. Select only actual results and real evidence locations; no unsupported PASS or fabricated observation.`)
		b.WriteString(repairStopConditionsContext(root, state))
	} else {
		b.WriteString(`This is the original flat ChangeImpact request: professional fields stay at the top level, without an answer wrapper or invented questions. Correct an unreceived draft at its exact supplied path; never overwrite a registered immutable artifact. changed_artifacts accepts actual path strings or objects with path; no SHA is required. Optional integration_notes can record actual integration and checks, without a required format. Impact fields: change_types=[requirement|design|contract|task|implementation|test|configuration|database|dependency|documentation_only]; escalation_level=assignment|workgroup|review_round|baseline; invalidated_evidence_ids, superseded_evidence_ids and retained_evidence_ids are references to actual evidence (or []). decisions is an array of complete objects. source_id identifies the actual change/source being analyzed (such as the supplied Case or changed artifact); target_id identifies its affected consumer, assertion or evidence; relation describes that dependency, not the disposition. decision is invalidate|supersede|retain|reverify. rule_id may identify an actual existing rule; do not invent a rule for reception; responsibility_id is supplied by the system or null. Example shape: {"source_id":"<actual source>","target_id":"<actual affected target>","relation":"invalidates","rule_id":"IM-DEPENDENCY","decision":"reverify","responsibility_id":null,"scope":["<actual changed path>"],"rationale":"<actual dependency and disposition basis>","recovery_evidence":["test://actual-run"]}. Do not copy placeholders or invent evidence. Reference an existing rule only when applicable. Cite real evidence from the supplied sources. An accepted whole batch can have changed_artifacts=[] and change_types=[]; decisions=[] is valid when there is no actual disposition; do not invent a decision or reason for reception. Independent Contract reverification and fresh S7 remain necessary.`)
	}
	b.WriteString("\nActual source/evidence candidates (read their contents; a path alone is not proof):\n")
	seen := map[string]bool{}
	show := func(label, ref string) string {
		if ref == "" || seen[ref] {
			return ""
		}
		seen[ref] = true
		path, err := absoluteQuestionnairePath(root, ref)
		if err != nil {
			fmt.Fprintf(&b, "%s: source preparation gap for %q: %v\n", label, ref, err)
			return ""
		}
		fmt.Fprintf(&b, "%s: %s\n", label, path)
		return path
	}
	for _, key := range []string{"path", "plan_ref", "contract_ref", "impact_ref", "result_ref"} {
		show(key, stringValue(pointer[key]))
	}
	boundReq := mapFieldCLI(state, "bound_req")
	show("locked REQ", stringValue(boundReq["path"]))
	if !targeted {
		fmt.Fprintf(&b, "Original evidence ID/status/source index: %s#/evidence. Consult the entries affected by the actual changes when choosing dispositions; unrelated entries need no repeated analysis.\n", filepath.Join(root, ".claude", "loop-state.json"))
	}
	for _, ref := range repairArtifactRefs(pointer, "result_refs", "result_ref") {
		// The candidate paths are already displayed above; these are the actual
		// evidence anchors retained by the original Result, not new registrations.
		path, err := absoluteQuestionnairePath(root, ref.Path)
		if err != nil {
			continue
		}
		show("RepairResult evidence source", ref.Path)
		var value any
		if err := repairLeadIndexedSource(root, ref, &value); err != nil {
			fmt.Fprintf(&b, "Evidence source preparation gap at %s: %v\n", path, err)
			continue
		}
		repairLeadEvidenceHelp(root, &b, value)
	}
	contractPath, err := absoluteQuestionnairePath(root, stringValue(pointer["contract_ref"]))
	if err != nil {
		return b.String()
	}
	var contract map[string]json.RawMessage
	if err := repairLeadIndexedSource(root, repair.ArtifactRef{Path: stringValue(pointer["contract_ref"])}, &contract); err != nil {
		fmt.Fprintf(&b, "Contract assertion preparation gap at %s: %v\n", contractPath, err)
		return b.String()
	}
	for _, group := range []struct{ field, prefix string }{{"symptom_assertions", "symptom"}, {"root_invariant_assertions", "root"}, {"detection_gap_assertions", "gap"}} {
		var assertions []json.RawMessage
		if err := json.Unmarshal(contract[group.field], &assertions); err != nil {
			fmt.Fprintf(&b, "Contract %s#/%s preparation gap: %v\n", contractPath, group.field, err)
			continue
		}
		for index, assertion := range assertions {
			fmt.Fprintf(&b, "%s-%d: %s#/%s/%d = %s\n", group.prefix, index+1, contractPath, group.field, index, assertion)
		}
	}
	return b.String()
}

// Conditions remain professional Contract text, not another mandatory answer
// array or machine assertion slot. Read the original pinned source once.
func repairStopConditionsContext(root string, state map[string]any) string {
	pointer := repairPointerCLI(state)
	ref := repair.ArtifactRef{Path: stringValue(pointer["contract_ref"])}
	var contract struct {
		Conditions []string `json:"stop_escalation_conditions"`
	}
	body, err := workpacket.ReadFile(root, ref.Path, 2<<20)
	if err == nil {
		err = json.Unmarshal(body, &contract)
	}
	if err != nil {
		return "\nApproved Contract stop/escalation conditions unavailable; Main restores the original source, never invents conditions or asks the verifier to rewrite valid work: " + err.Error() + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nApproved Contract stop/escalation conditions from %s:\n", resolveRootPath(root, ref.Path))
	for _, condition := range contract.Conditions {
		fmt.Fprintf(&b, "- %s\n", condition)
	}
	b.WriteString("Evaluate these original conditions alongside the necessary assertions. If observations require scope/Contract changes, report scope_changed with actual evidence through the original Targeted result for S8 reassessment/approval; environmental inability is blocked. Explain the triggered condition in the existing result/reason/evidence, never hide it as residual risk or manufacture PASS. No condition IDs, Contract hashes or second assessment array need to be authored.\n")
	return b.String()
}

func repairLeadEvidenceHelp(root string, b *strings.Builder, value any) {
	doc, _ := value.(map[string]any)
	receipt, _ := doc["receipt"].(map[string]any)
	provenance, _ := receipt["provenance"].(map[string]any)
	sourceRoot := stringValue(provenance["execution_root"])
	captures := map[string]map[string]any{}
	if rows, ok := provenance["local_evidence"].([]any); ok {
		for _, row := range rows {
			if row, ok := row.(map[string]any); ok {
				captures[stringValue(row["authored_ref"])] = row
			}
		}
	}
	var walk func(any, bool)
	walk = func(value any, mainCheck bool) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "receipt" || key == "provenance" {
					continue
				}
				if key != "evidence_refs" {
					walk(child, mainCheck || key == "checks")
					continue
				}
				refs, _ := child.([]any)
				if text, ok := child.(string); ok {
					refs = []any{text}
				}
				for _, ref := range refs {
					text, ok := ref.(string)
					if !ok || text == "" {
						continue
					}
					parsed, _ := url.Parse(text)
					if !strings.HasPrefix(text, "path:") && parsed != nil && parsed.Scheme != "" {
						fmt.Fprintf(b, "Recorded evidence candidate: %s\n", text)
						continue
					}
					if !mainCheck {
						if captured := captures[text]; captured != nil {
							path := stringValue(captured["path"])
							fmt.Fprintf(b, "Recorded Builder evidence candidate: %s (original ref %q; Builder root %s; source %s)\n", filepath.Join(root, path), text, sourceRoot, stringValue(captured["source_path"]))
						} else if sourceRoot != "" {
							fmt.Fprintf(b, "Recorded Builder evidence candidate: %s (Builder root %s; historical/unarchived reference; resolve at its original source, not Main)\n", text, sourceRoot)
						} else {
							fmt.Fprintf(b, "Recorded evidence candidate: %s (historical source root unspecified; unarchived reference, do not assume Main)\n", text)
						}
						continue
					}
					if sourceRoot == "" {
						fmt.Fprintf(b, "Recorded check evidence candidate: %s (historical source root unspecified; preserve the original reference, do not assume Main)\n", text)
						continue
					}
					fmt.Fprintf(b, "Recorded Main check evidence candidate: %s (Main root %s; original reference)\n", text, root)
				}
			}
		case []any:
			for _, child := range value {
				walk(child, mainCheck)
			}
		}
	}
	walk(value, false)
}

// repairLeadQuestionnairePaths returns only the deterministic paths for the
// current S9 answer phase. It is read-only so Hook fallback observers can
// inspect a saved questionnaire without creating one or parsing prose.
func repairLeadQuestionnairePaths(root string, state map[string]any, contexts ...context.Context) []string {
	lifecycle := mapFieldCLI(state, "lifecycle")
	if stringValue(lifecycle["state"]) != "bug_resolution" {
		return nil
	}
	phase, err := observedRepairLeadPhase(root, state)
	if err != nil {
		return nil
	}
	var paths []string
	switch phase {
	case "impact_reconciliation":
		if path, err := repairLeadQuestionnairePath(root, state, "change-impact.json"); err == nil {
			paths = append(paths, path)
		}
	case "targeted_reverification":
		selections, err := repairTargetedSelections(root, state)
		if err != nil {
			return nil
		}
		missing, err := repairObservedMissingTargetedIDs(root, repairPointerCLI(state))
		if err != nil || repairTargetedRequestMappingError(selections, missing) != nil {
			return nil
		}
		for _, selection := range repairTargetedOutstandingSelections(selections, missing) {
			if path, pathErr := repairTargetedQuestionnairePath(root, state, selection.performingAssignmentID, contexts...); pathErr == nil {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func currentRepairLeadPhase(root string, state map[string]any) (string, error) {
	return repairLeadPhase(root, state, repairMissingTargetedIDs)
}

func observedRepairLeadPhase(root string, state map[string]any) (string, error) {
	return repairLeadPhase(root, state, repairObservedMissingTargetedIDs)
}

func repairLeadPhase(root string, state map[string]any, missingIDs func(string, map[string]any) ([]string, error)) (string, error) {
	pointer := repairPointerCLI(state)
	switch stringValue(pointer["status"]) {
	case "impact_reconciliation", "targeted_reverification", "ready_for_full_review":
		return stringValue(pointer["status"]), nil
	case "repairing":
		contributions, err := repair.ReceivedRepairContributions(root, state)
		if err != nil {
			return "", err
		}
		if len(contributions) > 0 {
			return "impact_reconciliation", nil
		}
	case "blocked":
		if pointer["failure_route"] == "blocked" {
			return "targeted_reverification", nil
		}
	}
	if lifecycle := mapFieldCLI(state, "lifecycle"); lifecycle["phase"] == "repair_readback" {
		return "repair_readback", nil
	}
	return "", nil
}

func repairMissingTargetedIDs(root string, pointer map[string]any) ([]string, error) {
	return repairTargetedMissingIDs(root, pointer, true)
}

func repairObservedMissingTargetedIDs(root string, pointer map[string]any) ([]string, error) {
	return repairTargetedMissingIDs(root, pointer, false)
}

func repairTargetedMissingIDs(root string, pointer map[string]any, verify bool) ([]string, error) {
	// Existing IDs are display data; completion is the recorded domain phase.
	return stringSliceAny(pointer["required_reverification_ids"]), nil
}

// Read the actual object already named by the current index, without replaying
// source hashes or historical qualification.
func repairLeadIndexedSource(root string, ref repair.ArtifactRef, value any) error {
	path, err := absoluteQuestionnairePath(root, ref.Path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func repairLeadPreviousQuestionnaires(path string) string {
	// The current original path is sufficient; ordinary observation must not
	// scan unrelated questionnaire histories to construct extra guidance.
	return ""
}

func repairLeadExistingQuestionnaires(root string, state map[string]any) string {
	path, err := repairLeadQuestionnairePath(root, state, "change-impact.json")
	if err != nil {
		return "; Main/system must restore the missing source before an exact answer path can be resolved"
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(path)), "*", "*.json"))
	if len(paths) == 0 {
		return "; no questionnaire exists under the current source; Main/system owns preparation"
	}
	return "; preserve existing questionnaires at " + strings.Join(paths, ", ") + "; do not continue under a changed source binding"
}

func repairLeadPreparationEvent(event string) bool {
	return event == "SessionStart" || event == "PreToolUse" || event == "SubagentStart"
}

func repairLeadReadOnlyEvent(event string) bool {
	return event == "Stop" || event == "SubagentStop" || event == "TeammateIdle" || event == "Idle"
}

// prepareRepairEntry is called after the actual S8 approval transaction.
func prepareRepairEntry(root, statePath, journalPath string, state map[string]any) (map[string]any, error) {
	if stringValue(repairPointerCLI(state)["plan_ref"]) == "" {
		prepared, err := prepareRepairReadback(root, statePath, journalPath, state)
		if err != nil {
			return nil, err
		}
		state = prepared
	}
	plan, err := repairDispatchPlan(root, state)
	if err != nil {
		return nil, err
	}
	builders := make([]repairDispatchChoice, 0, len(plan.Assignments))
	verifiers := make([]repairVerifierDispatchChoice, 0, len(plan.Assignments))
	for _, assignment := range plan.Assignments {
		builders = append(builders, repairDispatchChoice{AssignmentID: assignment.AssignmentID})
		verifiers = append(verifiers, repairVerifierDispatchChoice{AssignmentID: assignment.AssignmentID})
	}
	for name, draft := range map[string]any{"builder-dispatch.json": builders, "independent-verifier-dispatch.json": verifiers} {
		path, err := repairLeadQuestionnairePath(root, state, name)
		if err != nil {
			return nil, err
		}
		if err := writeRepairLeadDraft(path, draft); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func prepareRepairReadback(root, statePath, journalPath string, state map[string]any) (map[string]any, error) {
	pointer := repairPointerCLI(state)
	runtimeID, generation, contract, err := repairReadbackContractSource(root, state)
	if err != nil {
		return nil, err
	}
	suffix, err := repairReadbackIDSuffix(runtimeID, generation, contract.CaseID, contract.ContractID, contract.Ref)
	if err != nil {
		return nil, err
	}
	sameContract := stringValue(pointer["case_id"]) == contract.CaseID && stringValue(pointer["contract_id"]) == contract.ContractID &&
		stringValue(pointer["contract_ref"]) == contract.Ref.Path
	if stringValue(pointer["contract_id"]) == "" && stringValue(pointer["contract_ref"]) == contract.Ref.Path {
		sameContract = stringValue(pointer["case_id"]) == contract.CaseID
	}
	if stringValue(pointer["path"]) == "" || !sameContract {
		_, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{
			RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"},
			SessionID:      "repair-session-" + suffix, CreatedBy: "Main",
		})
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return nil, err
	}
	pointer = repairPointerCLI(snapshot.State)
	if stringValue(pointer["plan_ref"]) == "" && stringValue(pointer["status"]) == "contract_ready" {
		runtimeID := strings.TrimSpace(stringValue(snapshot.State["runtime_id"]))
		generation := integerValue(mapFieldCLI(snapshot.State, "baseline")["generation"])
		suffix, err := repairReadbackIDSuffix(runtimeID, generation, stringValue(pointer["case_id"]), stringValue(pointer["contract_id"]), repair.ContractRef{
			Path: stringValue(pointer["contract_ref"]),
		})
		if err != nil {
			return nil, err
		}
		_, _, _, err = repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{
			RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"},
			PlanID:         "repair-plan-" + suffix, CreatedBy: "Main",
		})
		if err != nil {
			return nil, err
		}
		snapshot, err = runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil {
			return nil, err
		}
	}
	return snapshot.State, nil
}

func repairReadbackContractSource(root string, state map[string]any) (string, int, repair.ApprovedContract, error) {
	runtimeID := strings.TrimSpace(stringValue(state["runtime_id"]))
	generation := integerValue(mapFieldCLI(state, "baseline")["generation"])
	if runtimeID == "" || generation < 1 {
		return "", 0, repair.ApprovedContract{}, fmt.Errorf("Runtime id and positive baseline generation are required before S9 can read back")
	}
	investigation := mapFieldCLI(state, "review")["investigation"]
	current, _ := investigation.(map[string]any)
	if current == nil || stringValue(current["status"]) != "contract_approved" {
		return "", 0, repair.ApprovedContract{}, fmt.Errorf("current approved InvestigationCase and RepairContract are required before S9 can read back")
	}
	contract, err := repair.ValidateApprovedContractRef(root, repair.ContractRef{
		Path: stringValue(current["repair_contract_ref"]),
	})
	if err != nil {
		return "", 0, repair.ApprovedContract{}, err
	}
	if contract.CaseID == "" || contract.ContractID == "" || stringValue(current["case_id"]) != contract.CaseID {
		return "", 0, repair.ApprovedContract{}, fmt.Errorf("current InvestigationCase and approved RepairContract identities do not match")
	}
	return runtimeID, generation, contract, nil
}

func repairReadbackIDSuffix(runtimeID string, generation int, caseID, contractID string, contractRef repair.ContractRef) (string, error) {
	if strings.TrimSpace(runtimeID) == "" || generation < 1 || strings.TrimSpace(caseID) == "" || strings.TrimSpace(contractID) == "" || strings.TrimSpace(contractRef.Path) == "" {
		return "", fmt.Errorf("Runtime, baseline generation, Case, and approved Contract reference are required for S9 Session/Plan IDs")
	}
	return repairResponsibilityKey(runtimeID, fmt.Sprint(generation), caseID, contractID, contractRef.Path), nil
}

// repairResponsibilityKey is a reversible encoding of existing responsibility
// coordinates, not a fingerprint of author content.
func repairResponsibilityKey(values ...string) string {
	data, _ := json.Marshal(values)
	return base64.RawURLEncoding.EncodeToString(data)
}

// repairArtifactIdentityToken distinguishes a new artifact identity from the
// already saved ones it replaces. It is taken from the saved artifact paths,
// which are domain facts, so the Runtime write count never takes part in it.
func repairArtifactIdentityToken(paths []string) string {
	sum := sha256.Sum256([]byte(strings.Join(paths, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// repairArtifactCarriesID reports whether a saved artifact path carries the
// given identity, so a later delivery of the same responsibility is told apart
// from the earlier ones.
func repairArtifactCarriesID(path, id string) bool {
	name := filepath.Base(path)
	if !strings.HasSuffix(name, ".json") {
		return false
	}
	saved := strings.TrimSuffix(name, ".json")
	return saved == id || strings.HasPrefix(saved, id+"-")
}

func repairLeadQuestionnairePath(root string, state map[string]any, name string) (string, error) {
	pointer := repairPointerCLI(state)
	rel := strings.TrimSpace(stringValue(pointer["questionnaire_root"]))
	if rel == "" {
		return "", fmt.Errorf("the original S9 questionnaire path was not retained; Main/system must locate the actual existing responsibility")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.Base(name) != name || name == "" {
		return "", fmt.Errorf("the original S9 questionnaire location is invalid")
	}
	planPath := strings.TrimSpace(stringValue(pointer["plan_ref"]))
	if planPath == "" {
		return "", fmt.Errorf("the current RepairPlan path is not available for the original questionnaire")
	}
	// A genuine Plan revision is a different original responsibility. Use its
	// saved path as a reversible location, without reopening/rehashing content.
	planKey := repairResponsibilityKey(planPath)
	parts := []string{root, clean, "plan"}
	for len(planKey) > 120 {
		parts = append(parts, planKey[:120])
		planKey = planKey[120:]
	}
	parts = append(parts, planKey)
	if len(name) > 200 {
		parts = append(parts, "n")
		for len(name) > 120 {
			parts = append(parts, name[:120])
			name = name[120:]
		}
	}
	parts = append(parts, name)
	return filepath.Abs(filepath.Join(parts...))
}

func repairTargetedQuestionnaireName(assignmentID string) string {
	return "targeted-reverification-" + repairResponsibilityKey(assignmentID) + ".json"
}

func repairTargetedQuestionnairePath(root string, state map[string]any, assignmentID string, contexts ...context.Context) (string, error) {
	if len(contexts) > 0 && contexts[0] != nil {
		if err := contexts[0].Err(); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(assignmentID) == "" {
		return "", fmt.Errorf("current verifier assignment id is empty")
	}
	legacy, err := repairLeadQuestionnairePath(root, state, repairTargetedQuestionnaireName(assignmentID))
	if err != nil {
		return "", err
	}
	owners := stringMapCLI(repairPointerCLI(state)["assignment_owners"])
	original := ""
	for candidate := range owners {
		if repair.VerificationAssignmentID(candidate) == assignmentID {
			if original != "" {
				return "", fmt.Errorf("ambiguous original Targeted assignment")
			}
			original = candidate
		}
	}
	if original == "" {
		return legacy, nil
	}
	normal, err := repair.UsesSharedTargetedAuthoring(root, state, original, assignmentID, owners[assignmentID], contexts...)
	if err != nil {
		return "", err
	}
	if !normal {
		return legacy, nil
	}
	return repair.TargetedSharedAnswerPath(root, legacy)
}

func prepareRepairImpactQuestionnaire(root string, state map[string]any) (string, error) {
	path, err := repairLeadQuestionnairePath(root, state, "change-impact.json")
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("original ChangeImpact questionnaire path %s is not a regular file", path)
		}
		return path, nil // Preserve the existing authoring view without replaying contributors.
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	pointer := repairPointerCLI(state)
	sessionID := stringValue(pointer["session_id"])
	if sessionID == "" {
		return "", fmt.Errorf("current repair responsibility has no Session")
	}
	// Available contributions can inform Main's newly prepared view. Their
	// absence or unavailability cannot prevent an original impact handoff.
	contributions, _ := repair.ReceivedRepairContributions(root, state)
	changedPaths := []repair.ArtifactRef{}
	seenPaths := map[string]bool{}
	for _, contribution := range contributions {
		for _, changed := range contribution.ChangedArtifacts {
			if changed.Path == "" || seenPaths[changed.Path] {
				continue
			}
			seenPaths[changed.Path] = true
			changedPaths = append(changedPaths, repair.ArtifactRef{Path: changed.Path, Status: changed.Status})
		}
	}
	boundReq := mapFieldCLI(state, "bound_req")
	caseID := stringValue(pointer["case_id"])
	if caseID == "" {
		return "", fmt.Errorf("current S9 Runtime pointer has no source Case id")
	}
	reverificationIDs, err := repairImpactReverificationIDs(state)
	if err != nil {
		return "", err
	}
	request := repair.ChangeImpactRequest{
		ImpactID:  "impact-" + repairResponsibilityKey(sessionID),
		RuntimeID: stringValue(state["runtime_id"]), ReqID: stringValue(boundReq["id"]),
		BaselineGeneration: integerValue(mapFieldCLI(state, "baseline")["generation"]),
		SourceCaseIDs:      []string{caseID}, ChangeTypes: []string{}, ChangedArtifacts: changedPaths,
		Decisions: []repair.ImpactDecision{}, EscalationLevel: "assignment",
		InvalidatedEvidenceIDs: []string{}, SupersededEvidenceIDs: []string{}, RetainedEvidenceIDs: []string{},
		RequiredReverificationIDs: reverificationIDs, AnalyzedBy: "Main",
	}
	return path, writeRepairLeadDraft(path, request)
}

type repairTargetedSelection struct {
	originalAssignmentID   string
	performingAssignmentID string
	repairOwner            string
	verifierOwner          string
	impact                 repair.ChangeImpact
}

func repairImpactReverificationIDs(state map[string]any) ([]string, error) {
	sessionID := stringValue(repairPointerCLI(state)["session_id"])
	if sessionID == "" {
		return nil, fmt.Errorf("current RepairSession is required to identify the Contract reverification obligation")
	}
	impactID := "impact-" + repairResponsibilityKey(sessionID)
	if existing := stringSliceAny(repairPointerCLI(state)["required_reverification_ids"]); len(existing) > 0 {
		return existing, nil
	}
	return []string{"reverify-" + repairResponsibilityKey(impactID)}, nil
}

func repairTargetedSelections(root string, state map[string]any) ([]repairTargetedSelection, error) {
	pointer := repairPointerCLI(state)
	planRef := repair.ArtifactRef{Path: stringValue(pointer["plan_ref"])}
	var plan repair.RepairPlan
	if err := repairLeadIndexedSource(root, planRef, &plan); err != nil {
		return nil, fmt.Errorf("read current Plan: %w", err)
	}
	impactRef := repair.ArtifactRef{Path: stringValue(pointer["impact_ref"])}
	var impact repair.ChangeImpact
	if err := repairLeadIndexedSource(root, impactRef, &impact); err != nil {
		return nil, fmt.Errorf("read current Impact: %w", err)
	}
	owners := stringMapCLI(pointer["assignment_owners"])
	selections := []repairTargetedSelection{}
	for _, assignment := range plan.Assignments {
		originalOwner := owners[assignment.AssignmentID]
		performingID := repair.VerificationAssignmentID(assignment.AssignmentID)
		verifierOwner := owners[performingID]
		if verifierOwner == "" {
			continue
		}
		selections = append(selections, repairTargetedSelection{
			originalAssignmentID:   assignment.AssignmentID,
			performingAssignmentID: performingID,
			repairOwner:            originalOwner,
			verifierOwner:          verifierOwner,
			impact:                 impact,
		})
	}
	return selections, nil
}

func repairTargetedRequestMappingError(selections []repairTargetedSelection, missing []string) error {
	if len(selections) == 0 {
		return fmt.Errorf("Main must select and actually notify an independent verifier in the supplied verifier division questionnaire")
	}
	return nil
}

func repairTargetedOutstandingSelections(selections []repairTargetedSelection, missing []string) []repairTargetedSelection {
	return selections
}

func repairTargetedIDForSelection(selection repairTargetedSelection, selections []repairTargetedSelection, missing []string) (string, error) {
	// Each existing verification responsibility owns its own report artifact.
	return "reverify-" + repairResponsibilityKey(selection.impact.ImpactID, selection.performingAssignmentID), nil
}

func prepareRepairTargetedQuestionnaire(root string, state map[string]any, selection repairTargetedSelection, contexts ...context.Context) (string, repair.TargetedReverificationRequest, error) {
	parent := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		parent = contexts[0]
	}
	prepareContext, cancelPrepare := context.WithTimeout(parent, 3*time.Second)
	defer cancelPrepare()
	path, err := repairTargetedQuestionnairePath(root, state, selection.performingAssignmentID, prepareContext)
	if err != nil {
		return "", repair.TargetedReverificationRequest{}, err
	}
	assertions, err := repairContractAssertionResults(root, repairPointerCLI(state))
	if err != nil {
		return "", repair.TargetedReverificationRequest{}, err
	}
	if len(assertions) == 0 {
		return "", repair.TargetedReverificationRequest{}, fmt.Errorf("current approved Contract has no assertion surface")
	}
	reverificationID, err := repairTargetedIDForSelection(selection, nil, nil)
	if err != nil {
		return "", repair.TargetedReverificationRequest{}, err
	}
	request := repair.TargetedReverificationRequest{
		ReverificationID: reverificationID, RuntimeID: stringValue(state["runtime_id"]),
		CaseID:               stringValue(repairPointerCLI(state)["case_id"]),
		BaselineGeneration:   integerValue(mapFieldCLI(state, "baseline")["generation"]),
		OriginalAssignmentID: selection.originalAssignmentID, PerformingAssignmentID: selection.performingAssignmentID,
		ImpactID: selection.impact.ImpactID, AssertionResults: assertions,
	}
	draftBytes, err := json.Marshal(request)
	if err != nil {
		return "", repair.TargetedReverificationRequest{}, err
	}
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		return "", repair.TargetedReverificationRequest{}, err
	}
	delete(draft, "continuity_reason")
	delete(draft, "scope_compliance")
	if err := writeRepairLeadDraft(path, draft); err != nil {
		return "", repair.TargetedReverificationRequest{}, err
	}
	return path, request, nil
}

func repairContractAssertionResults(root string, pointer map[string]any) ([]repair.AssertionResult, error) {
	data, err := os.ReadFile(resolveRootPath(root, stringValue(pointer["contract_ref"])))
	if err != nil {
		return nil, err
	}
	var assertions struct {
		Symptom []json.RawMessage `json:"symptom_assertions"`
		Root    []json.RawMessage `json:"root_invariant_assertions"`
		Gaps    []json.RawMessage `json:"detection_gap_assertions"`
	}
	if err := json.Unmarshal(data, &assertions); err != nil {
		return nil, err
	}
	var results []repair.AssertionResult
	for index := range assertions.Symptom {
		results = append(results, repair.AssertionResult{AssertionID: fmt.Sprintf("symptom-%d", index+1)})
	}
	for index := range assertions.Root {
		results = append(results, repair.AssertionResult{AssertionID: fmt.Sprintf("root-%d", index+1)})
	}
	for index := range assertions.Gaps {
		results = append(results, repair.AssertionResult{AssertionID: fmt.Sprintf("gap-%d", index+1)})
	}
	return results, nil
}

func repairHandoffRequestFromRuntime(root string, state map[string]any) (repair.HandoffRequest, error) {
	pointer := repairPointerCLI(state)
	ref := func(pathKey, label string) (repair.ArtifactRef, error) {
		ref := repair.ArtifactRef{Path: stringValue(pointer[pathKey])}
		if ref.Path == "" {
			return ref, fmt.Errorf("%s has no recorded path", label)
		}
		return ref, nil
	}
	session, err := ref("path", "current RepairSession")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	plan, err := ref("plan_ref", "current RepairPlan")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	contract, err := ref("contract_ref", "current RepairContract")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	impact, err := ref("impact_ref", "current ChangeImpact")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	results := repairArtifactRefs(pointer, "targeted_reverification_artifacts", "")
	if len(results) == 0 {
		return repair.HandoffRequest{}, fmt.Errorf("current Runtime has no committed TargetedReverification artifact")
	}
	// Recorded Final and Changeset references remain useful when they exist. Their absence is not a new handoff prerequisite, and building a
	// replacement here would re-read unrelated authors' accepted work.
	optionalRef := func(pathKey string) repair.ArtifactRef {
		path := stringValue(pointer[pathKey])
		if path == "" {
			return repair.ArtifactRef{}
		}
		return repair.ArtifactRef{Path: path}
	}
	result := optionalRef("result_ref")
	changesetRef := optionalRef("changeset_ref")
	sessionID := stringValue(pointer["session_id"])
	request := repair.HandoffRequest{
		HandoffID: "repair-handoff-" + repairResponsibilityKey(sessionID),
		Session:   session, Plan: plan, Contract: repair.ContractRef{Path: contract.Path},
		Result: result, Changeset: changesetRef, ChangeImpact: impact,
		TargetedReverifications: results, HandedOffBy: "Main",
		NextAction: "start a new independent S7 review from the current repair baseline",
	}
	return request, nil
}

func completeRepairHandoff(root string) (repair.ArtifactRef, runtime.Snapshot, error) {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return repair.ArtifactRef{}, runtime.Snapshot{}, err
	}
	pointer := repairPointerCLI(snapshot.State)
	if registered := stringValue(pointer["handoff_ref"]); registered != "" {
		ref := repair.ArtifactRef{Path: registered}
		data, readErr := os.ReadFile(resolveRootPath(root, registered))
		var handoff repair.RepairHandoff
		if readErr != nil || json.Unmarshal(data, &handoff) != nil {
			return repair.ArtifactRef{}, snapshot, fmt.Errorf("read recorded Handoff: %v", readErr)
		}
		return ref, snapshot, nil
	}
	if stringValue(pointer["status"]) != "ready_for_full_review" {
		return repair.ArtifactRef{}, snapshot, fmt.Errorf("current repair pointer status is %q, want ready_for_full_review", stringValue(pointer["status"]))
	}
	request, err := repairHandoffRequestFromRuntime(root, snapshot.State)
	if err != nil {
		return repair.ArtifactRef{}, snapshot, err
	}
	handoff, ref, err := repair.CreateRepairHandoff(root, request)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return repair.ArtifactRef{}, snapshot, err
		}
		rel := repair.ArtifactStoragePath("handoffs", request.HandoffID)
		ref = repair.ArtifactRef{ID: request.HandoffID, Path: rel}
		data, readErr := os.ReadFile(resolveRootPath(root, ref.Path))
		if readErr != nil {
			return repair.ArtifactRef{}, snapshot, readErr
		}
		if err := json.Unmarshal(data, &handoff); err != nil {
			return repair.ArtifactRef{}, snapshot, err
		}
	}
	if handoff.HandoffID != request.HandoffID {
		return repair.ArtifactRef{}, snapshot, fmt.Errorf("existing RepairHandoff id %q does not match current S9 session %q", handoff.HandoffID, request.HandoffID)
	}
	committed, err := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, Handoff: ref,
	})
	if err != nil {
		return ref, snapshot, fmt.Errorf("commit RepairHandoff %s through the original Runtime consumer: %w", ref.Path, err)
	}
	return ref, committed, nil
}

func repairPointerCLI(state map[string]any) map[string]any {
	return mapFieldCLI(mapFieldCLI(state, "review"), "repair")
}

func repairPointerRef(pointer map[string]any, pathKey, label string) (repair.ArtifactRef, error) {
	ref := repair.ArtifactRef{Path: stringValue(pointer[pathKey])}
	if ref.Path == "" {
		return repair.ArtifactRef{}, fmt.Errorf("%s is missing from current Runtime", label)
	}
	return ref, nil
}

func questionnaireMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func writeRepairLeadDraft(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return prepareQuestionnaireText(path, string(data)+"\n")
}

func persistRepairLeadQuestionnaire(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// runSavedRepairLeadQuestionnaire sends an exact saved DTO through its
// original S9 Create/Commit and Runtime Writer functions.
func runSavedRepairLeadQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	pointer := repairPointerCLI(snapshot.State)
	if expected, pathErr := repairLeadQuestionnairePath(root, snapshot.State, "change-impact.json"); pathErr == nil && samePath(path, expected) {
		if !mainQuestionnaireViewer(request) {
			return false, 0 // Worker events do not receive Main's original answer.
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact answer at %s could not be read: %v", path, readErr))
		}
		unchanged, observationErr := s9ObservedQuestionnaireUnchangedContext(contextForHook(request), root, snapshot.State, observedQuestionnaire{path: path, s9Kind: "impact"}, data)
		if observationErr == nil && unchanged {
			return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact is already received. Main continues its actual work and any registered successor; the same original answer remains available for later judgments.")
		}
		return saveRepairImpactQuestionnaireBytes(root, snapshot, path, data, stdout, contextForHook(request))
	}
	if mainQuestionnaireViewer(request) {
		return false, 0 // Independent Targeted reception happens at its native Stop.
	}
	if selections, selectErr := repairTargetedSelections(root, snapshot.State); selectErr == nil {
		for _, selection := range selections {
			expected, pathErr := repairTargetedQuestionnairePath(root, snapshot.State, selection.performingAssignmentID, contextForHook(request))
			if pathErr != nil || !samePath(path, expected) {
				continue
			}
			receipt := lookupTargetedForSelection(root, pointer, selection, "pass")
			if receipt.found {
				return true, repairLeadFeedback(stdout, path, "S9 TargetedReverification artifact "+receipt.ref.Path+" is already registered. Replay did not write Runtime again.")
			}
			phase, phaseErr := currentRepairLeadPhase(root, snapshot.State)
			if phaseErr != nil || (phase != "targeted_reverification" && phase != "ready_for_full_review") {
				return false, 0
			}
			answer, answerErr := readRepairTargetedAnswerContext(contextForHook(request), path, nil)
			if answerErr != nil {
				if receipt.unknown || receipt.previous.Path != "" {
					return true, repairLeadFeedback(stdout, path, targetedHistoricalUnknownMessage(receipt))
				}
				return true, repairLeadFeedback(stdout, path, answerErr.Error())
			}
			if receipt.previousReport != nil && sameTargetedProfessionalAnswer(answer, receipt.previousReport) {
				return true, repairLeadFeedback(stdout, path, targetedHistoricalUnknownMessage(receipt))
			}
			ctx := contextForHook(request)
			if receipt.unknown {
				ctx = context.WithValue(ctx, targetedAssociationUnknownKey{}, true)
			}
			handled, code, _ := saveRepairTargetedAnswerContext(ctx, root, snapshot, path, selection, answer, stdout)
			return handled, code
		}
	}
	return false, 0
}

// Compare only author-owned content against the original immutable artifact.
// IDs, source bindings, timestamps and computed diff remain machine-owned.
func repairLeadImpactReplay(root, path string, ref repair.ArtifactRef) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var answer repair.ChangeImpactRequest
	if err := repair.DecodeOriginalChangeImpact(data, &answer); err != nil {
		return err
	}
	registeredBytes, err := os.ReadFile(resolveRootPath(root, ref.Path))
	if err != nil {
		return err
	}
	var registered repair.ChangeImpact
	if err := json.Unmarshal(registeredBytes, &registered); err != nil {
		return err
	}
	if !sameImpactProfessionalAnswer(answer, registered) {
		return errors.New("saved professional content differs from the accepted content")
	}
	return nil
}

// Both the observer and receiver compare the same author-owned content after
// the original consumer's defaults. Machine IDs and timestamps are excluded.
func sameImpactProfessionalAnswer(answer repair.ChangeImpactRequest, registered repair.ChangeImpact) bool {
	normalize := func(types []string, artifacts []repair.ArtifactRef, decisions []repair.ImpactDecision, escalation string, invalid, superseded, retained []string, notes string) any {
		if escalation == "" {
			escalation = "assignment"
		}
		changed := append([]repair.ArtifactRef{}, artifacts...)
		for i := range changed {
			if changed[i].ID == "" {
				changed[i].ID = "changed-" + strings.NewReplacer("/", "-", "\\", "-").Replace(changed[i].Path)
			}
		}
		if len(decisions) == 0 {
			decisions = []repair.ImpactDecision{}
		}
		return []any{repairLeadStringSet(types), changed, decisions, escalation, repairLeadStringSet(invalid), repairLeadStringSet(superseded), repairLeadStringSet(retained), notes}
	}
	return repairLeadCompareProfessional(
		normalize(answer.ChangeTypes, answer.ChangedArtifacts, answer.Decisions, answer.EscalationLevel, answer.InvalidatedEvidenceIDs, answer.SupersededEvidenceIDs, answer.RetainedEvidenceIDs, answer.IntegrationNotes),
		normalize(registered.ChangeTypes, registered.ChangedArtifacts, registered.Decisions, registered.EscalationLevel, registered.InvalidatedEvidenceIDs, registered.SupersededEvidenceIDs, registered.RetainedEvidenceIDs, registered.IntegrationNotes)) == nil

}

func repairLeadTargetedReplay(root, path string, ref repair.ArtifactRef) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var answer repair.TargetedReverificationRequest
	if err := json.Unmarshal(data, &answer); err != nil {
		return err
	}
	var registered repair.TargetedReverification
	err = repairLeadIndexedSource(root, ref, &registered)
	if err != nil {
		return err
	}
	if !sameTargetedProfessionalAnswer(answer, &registered) {
		return errors.New("saved professional content differs from the accepted content")
	}
	return nil
}

// Only compare the author's existing professional fields. Nil and empty
// observation lists have the same meaning here; this comparison is used to
// avoid replaying an unchanged old answer, never as an acceptance gate.
func sameTargetedProfessionalAnswer(answer repair.TargetedReverificationRequest, registered *repair.TargetedReverification) bool {
	if registered == nil {
		return false
	}
	normalized := func(values []repair.AssertionResult) []repair.AssertionResult {
		if len(values) == 0 {
			return nil
		}
		out := append([]repair.AssertionResult(nil), values...)
		for i := range out {
			if len(out[i].EvidenceRefs) == 0 {
				out[i].EvidenceRefs = nil
			}
		}
		return out
	}
	return repairLeadCompareProfessional(
		[]any{answer.ContinuityReason, normalized(answer.AssertionResults), answer.ScopeCompliance, answer.Result, answer.FailureClass},
		[]any{registered.ContinuityReason, normalized(registered.AssertionResults), registered.ScopeCompliance, registered.Result, registered.FailureClass}) == nil
}

func repairLeadCompareProfessional(answer, registered any) error {
	left, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	right, err := json.Marshal(registered)
	if err != nil {
		return err
	}
	if string(left) != string(right) {
		return errors.New("saved professional content differs from the accepted content")
	}
	return nil
}

func repairLeadStringSet(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// targetedReceiptLookup keeps an unresolved old receipt distinct from both a
// known current conclusion and a genuinely absent receipt. In particular,
// an unavailable historical report is not a reason to ask its author to
// resubmit, or to claim its conclusion applies to a later Impact.
type targetedReceiptLookup struct {
	ref            repair.ArtifactRef
	previous       repair.ArtifactRef
	previousReport *repair.TargetedReverification
	found          bool
	unknown        bool
}

type targetedAssociationUnknownKey struct{}

func targetedHistoricalUnknownMessage(receipt targetedReceiptLookup) string {
	if receipt.previous.Path != "" {
		return "An earlier Targeted conclusion remains accepted at " + receipt.previous.Path + "; it does not by itself establish coverage of later changes. Main handles the current responsibility using the actual facts; no repeated author save or Stop is required."
	}
	return "Earlier Targeted references remain accepted, but their original responsibility or conclusion cannot be determined from the available records. Main handles the actual uncertainty; the current author is not required to resubmit or repeat Stop."
}

func lookupTargetedForSelection(root string, pointer map[string]any, selection repairTargetedSelection, result string) targetedReceiptLookup {
	// The initial, system-issued Impact identity is unique within a Session.
	// Only after it changes do we need the old report's Impact association;
	// this is not an author field or a qualification for accepting new work.
	sessionID := stringValue(pointer["session_id"])
	initialImpact := "impact-" + repairResponsibilityKey(sessionID)
	legacyImpact := "impact-" + dispatchSlug(sessionID)
	withImpact := selection.impact.ImpactID != "" && sessionID != "" && selection.impact.ImpactID != initialImpact && selection.impact.ImpactID != legacyImpact
	rows := objectSliceAny(pointer["targeted_reverification_artifacts"])
	if !withImpact {
		for i := len(rows) - 1; i >= 0; i-- {
			row := rows[i]
			if stringValue(row["performing_assignment_id"]) != selection.performingAssignmentID {
				continue
			}
			if stringValue(row["result"]) == "" {
				break // An old row requires the original domain fact below.
			}
			return targetedReceiptLookup{ref: repair.ArtifactRef{Path: stringValue(row["path"])}, found: stringValue(row["result"]) == result}
		}
	}
	facts, err := repair.LoadTargetedReceiptFacts(context.Background(), root, pointer, selection.performingAssignmentID, withImpact)
	if err != nil {
		return targetedReceiptLookup{unknown: true}
	}
	unknown := len(facts.Unknown) > 0
	var previous repair.ArtifactRef
	var previousReport *repair.TargetedReverification
	for i := len(facts.Rows) - 1; i >= 0; i-- {
		fact := facts.Rows[i]
		if withImpact {
			if fact.ImpactID == "" {
				unknown = true
				continue
			}
			if fact.ImpactID != selection.impact.ImpactID {
				if previous.Path == "" && fact.Result == result {
					previous = fact.Ref
					previousReport = fact.Original
				}
				continue // Preserve old result without claiming it covers this Impact.
			}
		}
		return targetedReceiptLookup{ref: fact.Ref, previous: previous, previousReport: previousReport, found: fact.Result == result, unknown: unknown}
	}
	return targetedReceiptLookup{previous: previous, previousReport: previousReport, unknown: unknown}
}

func registeredTargetedForSelection(root string, pointer map[string]any, selection repairTargetedSelection) (repair.ArtifactRef, bool) {
	receipt := lookupTargetedForSelection(root, pointer, selection, "pass")
	return receipt.ref, receipt.found
}

// Recover only the original system timestamp after artifact creation was
// interrupted before Commit. The original Create consumer still compares the
// complete immutable bytes; reading this value never accepts changed content.
func saveRepairImpactQuestionnaire(root string, snapshot runtime.Snapshot, path string, stdout io.Writer) (bool, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact answer at %s could not be read: %v", path, err))
	}
	return saveRepairImpactQuestionnaireBytes(root, snapshot, path, data, stdout)
}

// saveRepairImpactQuestionnaireBytes shares the current event's single read
// with the original Create/Commit consumers. It does not reconstruct other
// authors' contributions or independently qualify Main's professional work.
func saveRepairImpactQuestionnaireBytes(root string, snapshot runtime.Snapshot, path string, data []byte, stdout io.Writer, contexts ...context.Context) (bool, int) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	// Preserve the original responsibility across retries. A later repair may
	// occupy the same Runtime while this event is awaiting its Writer CAS.
	originalRuntime := stringValue(snapshot.State["runtime_id"])
	originalPointer := repairPointerCLI(snapshot.State)
	originalSession := stringValue(originalPointer["session_id"])
	originalCase := stringValue(originalPointer["case_id"])
	originalPath := path
	var committed runtime.Snapshot
	var ref repair.ArtifactRef
	var commitErr error
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			commitErr = ctx.Err()
			break
		}
		if attempt > 0 {
			var err error
			snapshot, err = runtime.NewStore(statePath, journalPath).WithContext(ctx).Snapshot()
			if err != nil {
				commitErr = err
				break
			}
			currentPointer := repairPointerCLI(snapshot.State)
			currentPath, pathErr := repairLeadQuestionnairePath(root, snapshot.State, "change-impact.json")
			if originalSession == "" || originalCase == "" ||
				stringValue(snapshot.State["runtime_id"]) != originalRuntime ||
				stringValue(currentPointer["session_id"]) != originalSession ||
				stringValue(currentPointer["case_id"]) != originalCase ||
				pathErr != nil || !samePath(originalPath, currentPath) {
				commitErr = fmt.Errorf("the original S9 Impact responsibility changed while coordinating this event; Main/system must continue its original Session %s, Case %s and answer %s", originalSession, originalCase, originalPath)
				break
			}
			data, err = os.ReadFile(path)
			if err != nil {
				commitErr = err
				break
			}
			unchanged, err := s9ObservedQuestionnaireUnchangedContext(ctx, root, snapshot.State, observedQuestionnaire{path: path, s9Kind: "impact"}, data)
			if err != nil {
				commitErr = err
				break
			}
			if unchanged {
				message := "S9 ChangeImpact is already received."
				if lifecycleState(snapshot.State) == "bug_resolution" && stringValue(currentPointer["status"]) == "targeted_reverification" {
					if err := ctx.Err(); err != nil {
						message += " The original independent-verification successor remains pending: " + err.Error()
					} else {
						// The earlier native route may have observed the snapshot before
						// the concurrent Impact was accepted. Continue this original
						// successor now; completed verifier receipts are skipped.
						var diagnostic strings.Builder
						continueRegisteredS9Verification(root, snapshot.State, &diagnostic)
						if detail := strings.TrimSpace(diagnostic.String()); detail != "" {
							message += " The original independent-verification successor needs Main/system attention: " + detail
						}
					}
				}
				return true, repairLeadFeedback(stdout, path, message)
			}
		}
		var answer repair.ChangeImpactRequest
		if err := repair.DecodeOriginalChangeImpact(data, &answer); err != nil {
			return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact answer at %s is not a valid original request DTO: %v", path, err))
		}
		pointer := repairPointerCLI(snapshot.State)
		sessionID := stringValue(pointer["session_id"])
		if sessionID == "" {
			return true, repairLeadFeedback(stdout, path, "current repair responsibility has no Session")
		}
		reverificationIDs, err := repairImpactReverificationIDs(snapshot.State)
		if err != nil {
			return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact answer could not derive the Contract reverification obligation from the current RepairSession: "+err.Error())
		}
		boundReq := mapFieldCLI(snapshot.State, "bound_req")
		answer.ImpactID = "impact-" + repairResponsibilityKey(sessionID)
		if impactRef := stringValue(pointer["impact_ref"]); impactRef != "" {
			// A second Impact of the same Session needs its own identity. The
			// discriminator is the already saved first Impact artifact path, a
			// domain fact, so the Runtime write count never takes part in it.
			answer.ImpactID += "-" + repairArtifactIdentityToken([]string{impactRef})
		}
		answer.RuntimeID = stringValue(snapshot.State["runtime_id"])
		answer.ReqID = stringValue(boundReq["id"])
		answer.BaselineGeneration = integerValue(mapFieldCLI(snapshot.State, "baseline")["generation"])
		answer.SourceBugIDs = []string{}
		answer.SourceCaseIDs = []string{stringValue(pointer["case_id"])}
		answer.RequiredReverificationIDs = reverificationIDs
		answer.AnalyzedBy = "Main"
		answer.AnalyzedAt = time.Now().UTC()
		var record repair.ChangeImpact
		record, ref, err = repair.CreateChangeImpact(root, answer)
		if err != nil {
			commitErr = err
			if errors.Is(err, os.ErrExist) {
				continue
			}
			break
		}
		committed, err = repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{Context: ctx, ExpectedRevision: snapshot.Revision, Actor: "Main"}, Impact: ref, Record: &record})
		if err == nil {
			commitErr = nil
			break
		}
		commitErr = err
		if !errors.Is(err, runtime.ErrStaleRevision) {
			break
		}
	}
	if commitErr != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact reception remains pending after a system write/coordination failure: %v. Preserve the original answer and any already received facts; Main/system continues the original reception.", commitErr))
	}
	if stringValue(repairPointerCLI(committed.State)["status"]) != "targeted_reverification" {
		return true, repairLeadFeedback(stdout, path, "S9 original ChangeImpact is recorded. Main handles any actual later contribution or outstanding responsibility using the existing accepted facts.")
	}
	selections, err := repairTargetedSelections(root, committed.State)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact committed. Current independent verifier responsibility: "+err.Error())
	}
	var next strings.Builder
	if len(selections) == 0 {
		for _, division := range repairVerifierDispatchPaths(root, committed.State) {
			next.WriteString(" Main must select an independent verifier in " + division + ".")
		}
	}
	for _, selection := range selections {
		answerPath, _, err := prepareRepairTargetedQuestionnaire(root, committed.State, selection)
		if err != nil {
			return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact committed; independent Targeted questionnaire preparation: "+err.Error())
		}
		next.WriteString(" Independent verifier " + selection.verifierOwner + " must complete and save " + answerPath + ".")
	}
	return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact at %s was committed through the original Runtime Writer.%s", ref.Path, next.String()))
}

func saveRepairTargetedQuestionnaire(root string, snapshot runtime.Snapshot, path string, selection repairTargetedSelection, stdout io.Writer) (bool, int) {
	answer, err := readRepairTargetedAnswer(path)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, err.Error())
	}
	return saveRepairTargetedAnswer(root, snapshot, path, selection, answer, stdout)
}

func readRepairTargetedAnswer(path string) (repair.TargetedReverificationRequest, error) {
	return readRepairTargetedAnswerContext(context.Background(), path, nil)
}

func readRepairTargetedAnswerContext(ctx context.Context, path string, capture *nativeS9AttemptCapture) (repair.TargetedReverificationRequest, error) {
	var answer repair.TargetedReverificationRequest
	if err := ctx.Err(); err != nil {
		return answer, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if capture != nil {
			capture.ObserveInputError(path, err)
		}
		if os.IsNotExist(err) {
			return answer, nativeS9ProgressPending
		}
		return answer, nativeArtifactReadError("read S9 TargetedReverification answer "+path, err)
	}
	if capture != nil {
		capture.SetInput(path)
		capture.ObserveInput(path, data)
		capture.SetStage("targeted_request_read")
	}
	if !json.Valid(data) {
		return answer, nativeS9ProgressPending
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return answer, &nativeAuthorError{fmt.Errorf("S9 TargetedReverification answer at %s is not a valid original request DTO: %w", path, err)}
	}
	if strings.TrimSpace(answer.Result) == "" {
		return answer, nativeS9ProgressPending
	}

	return answer, nil
}

func saveRepairTargetedAnswer(root string, snapshot runtime.Snapshot, path string, selection repairTargetedSelection, answer repair.TargetedReverificationRequest, stdout io.Writer) (bool, int) {
	handled, code, _ := saveRepairTargetedAnswerContext(context.Background(), root, snapshot, path, selection, answer, stdout)
	return handled, code
}

func saveRepairTargetedAnswerContext(ctx context.Context, root string, snapshot runtime.Snapshot, path string, selection repairTargetedSelection, answer repair.TargetedReverificationRequest, stdout io.Writer, captures ...*nativeS9AttemptCapture) (bool, int, error) {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	var err error

	pointer := repairPointerCLI(snapshot.State)
	blockedResume := stringValue(pointer["status"]) == "blocked" && stringValue(pointer["failure_route"]) == "blocked"
	if blockedResume && answer.Result == "blocked" {
		facts, lookupErr := repair.LoadTargetedReceiptFacts(ctx, root, pointer, selection.performingAssignmentID, true)
		if lookupErr != nil {
			return true, repairLeadFeedback(stdout, path, "S9 Targeted historical observation could not be inspected: "+lookupErr.Error()), lookupErr
		}
		for i := len(facts.Rows) - 1; i >= 0; i-- {
			fact := facts.Rows[i]
			if fact.Result != "blocked" {
				break
			}
			if fact.Original != nil && sameTargetedProfessionalAnswer(answer, fact.Original) {
				if capture := nativeS9Capture(captures); capture != nil {
					capture.ConsumerSucceeded()
					capture.SetStage("targeted_received")
				}
				return true, repairLeadFeedback(stdout, path, "S9 environmental blocker remains recorded by "+fact.Ref.Path+"; its unchanged original answer is already received. Main continues the actual unfinished work without another save or Stop."), nil
			}
			break // A changed original blocked answer is a new truthful handoff.
		}
		if len(facts.Unknown) != 0 {
			ctx = context.WithValue(ctx, targetedAssociationUnknownKey{}, true)
		}
	}
	answer.RuntimeID = stringValue(snapshot.State["runtime_id"])
	answer.CaseID = stringValue(pointer["case_id"])
	answer.BugID = ""
	answer.BaselineGeneration = integerValue(mapFieldCLI(snapshot.State, "baseline")["generation"])
	answer.OriginalAssignmentID = selection.originalAssignmentID
	answer.PerformingAssignmentID = selection.performingAssignmentID
	// Keep the association originally supplied on this answer. An empty
	// association remains unknown; the current Impact is not proof that the
	// author observed later changes.
	idSelection := selection
	idSelection.impact.ImpactID = answer.ImpactID
	answer.ReverificationID, err = repairTargetedIDForSelection(idSelection, nil, nil)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, err.Error()), nil
	}
	candidatePath := repair.ArtifactStoragePath("reverification", answer.ReverificationID)
	acceptedPaths := []string{}
	for _, accepted := range repairArtifactRefs(pointer, "targeted_reverification_artifacts", "") {
		if accepted.Path == candidatePath || repairArtifactCarriesID(accepted.Path, answer.ReverificationID) {
			acceptedPaths = append(acceptedPaths, accepted.Path)
		}
	}
	if len(acceptedPaths) > 0 {
		// A new delivery of the same reverification needs its own identity. The
		// discriminator is the already accepted artifact path of this same
		// responsibility, a domain fact, so the Runtime write count never takes
		// part in it.
		answer.ReverificationID += "-" + repairArtifactIdentityToken(acceptedPaths)
	}
	// The author reports observations, not a timestamp. Record reception
	// here; an uncommitted artifact retains its original system time.
	answer.PerformedAt = time.Now().UTC()
	// A new honest answer after a blocker is received directly. It does not
	// assert that the prior blocker was resolved or require a reason/resume.
	value, ref, createErr := repair.CreateTargetedReverification(root, answer)
	if createErr != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 TargetedReverification request at %s did not create the original artifact: %v", path, createErr)), nil
	}
	committed, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{Context: ctx, ExpectedRevision: -1, Actor: selection.verifierOwner}, Reverification: ref, Record: &value})
	if err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 independent TargetedReverification %s at %s was created but original Runtime consumer did not commit it: %v. Preserve its immutable source and follow the specific consumer feedback.", value.ReverificationID, ref.Path, err)), nil
	}
	if capture := nativeS9Capture(captures); capture != nil {
		capture.ConsumerSucceeded()
		capture.SetStage("targeted_received")
	}
	var exitFailure error
	message := fmt.Sprintf("S9 independent TargetedReverification %s at %s was committed through the original Runtime Writer.", value.ReverificationID, ref.Path)
	if stringValue(repairPointerCLI(committed.State)["status"]) == "closed" {
		message += " This late original conclusion remains associated with its original repair; Main assesses its actual effect on current work."
	}
	if unknown, _ := ctx.Value(targetedAssociationUnknownKey{}).(bool); unknown || value.ImpactID == "" {
		message += " Earlier accepted facts are preserved; the original Impact association is unknown and Main should assess it. No additional author save or Stop is required."
	}
	if stringValue(repairPointerCLI(committed.State)["status"]) == "ready_for_full_review" {
		exitMessage, completionErr := completeRepairExit(root)
		if completionErr != nil {
			exitFailure = &repairExitFailure{cause: completionErr, message: exitMessage}
		}
		message += " " + exitMessage
	}
	return true, repairLeadFeedback(stdout, path, message), exitFailure
}

func registeredBlockedTargetedForSelection(root string, pointer map[string]any, selection repairTargetedSelection) (repair.ArtifactRef, bool) {
	receipt := lookupTargetedForSelection(root, pointer, selection, "blocked")
	return receipt.ref, receipt.found
}

func repairLeadFeedback(stdout io.Writer, path, message string) int {
	message = "Questionnaire: " + path + ". " + message
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": message}})
	return 0
}
