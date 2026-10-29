package cli

import (
	"context"
	"crypto/sha256"
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
	"github.com/entroforge/go-system-builder/internal/targetedauthoring"
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
		contract := repair.ContractRef{Path: stringValue(pointer["contract_ref"]), SHA256: stringValue(pointer["contract_sha256"])}
		session := repair.ArtifactRef{Path: stringValue(pointer["path"]), SHA256: stringValue(pointer["sha256"])}
		plan := repair.ArtifactRef{Path: stringValue(pointer["plan_ref"]), SHA256: stringValue(pointer["plan_sha256"])}
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
			return "S9 ChangeImpact questionnaire is not prepared at " + path + "; Main/system resumes its original preparation with " + repairImpactPreparationRecovery(root) + ". Accepted Final Results remain recorded; do not resubmit Builder work or hand-author required_reverification_ids."
		}
		return "S9 ChangeImpact answer: " + path + ". Runtime/REQ/generation, source Case, the accepted Final batch changes, and required_reverification_ids (system-derived obligation IDs stable per RepairSession, not assertion IDs) are supplied from the original sources. Keep these supplied IDs; assertion_results in the separate Targeted answer carry the Contract assertion IDs. Complete the actual change types and professional impact/evidence dispositions with their rationales, then save this exact request. If the supplied accepted Final batch has no product changes, keep change_types=[] and explain the original Case/Contract retention or reverification decision; do not invent changed files or a change type. The original CreateChangeImpact and CommitChangeImpact consumers run on save." + repairLeadPreviousQuestionnaires(path) + repairLeadAnswerHelp(root, snapshot.State, false)

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
		b.WriteString(`This is the original flat ChangeImpact request: professional fields stay at the top level, without an answer wrapper or invented questions. Correct an unreceived draft at its exact supplied path; never overwrite a registered immutable artifact. Impact fields: change_types=[requirement|design|contract|task|implementation|test|configuration|database|dependency|documentation_only]; escalation_level=assignment|workgroup|review_round|baseline; invalidated_evidence_ids, superseded_evidence_ids and retained_evidence_ids are references to actual evidence (or []). decisions is an array of complete objects. source_id identifies the actual change/source being analyzed (such as the supplied Case or changed artifact); target_id identifies its affected consumer, assertion or evidence; relation describes that dependency, not the disposition. decision is invalidate|supersede|retain|reverify. rule_id must match IM-[A-Z0-9-]+; responsibility_id is supplied by the system or null. Example shape: {"source_id":"<actual source>","target_id":"<actual affected target>","relation":"invalidates","rule_id":"IM-DEPENDENCY","decision":"reverify","responsibility_id":null,"scope":["<actual changed path>"],"rationale":"<actual dependency and disposition basis>","recovery_evidence":["test://actual-run"]}. Do not copy placeholders or invent evidence. Use an IM- rule label for the actual dependency judgment; it needs no extra registration. Cite real evidence from the supplied sources. An accepted whole batch can have changed_artifacts=[] and change_types=[]; decisions must still explain the actual Case/Contract impact and retained or required observations. Independent Contract reverification and fresh S7 remain necessary.`)
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
	for _, ref := range repairArtifactRefs(pointer, "result_refs", "result_ref", "result_sha256") {
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
	if err := repairLeadIndexedSource(root, repair.ArtifactRef{Path: stringValue(pointer["contract_ref"]), SHA256: stringValue(pointer["contract_sha256"])}, &contract); err != nil {
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
	ref := repair.ArtifactRef{Path: stringValue(pointer["contract_ref"]), SHA256: stringValue(pointer["contract_sha256"])}
	var contract struct {
		Conditions []string `json:"stop_escalation_conditions"`
	}
	body, err := workpacket.ReadFile(root, ref.Path, 2<<20)
	if err == nil && ref.SHA256 != "" && workpacket.Digest(body) != ref.SHA256 {
		err = fmt.Errorf("approved Contract source changed at %s", ref.Path)
	}
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
							path, digest := stringValue(captured["path"]), stringValue(captured["sha256"])
							fmt.Fprintf(b, "Recorded Builder evidence candidate: %s (original ref %q; Builder root %s; source %s; sha256=%s)\n", filepath.Join(root, path), text, sourceRoot, stringValue(captured["source_path"]), digest)
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
					original := text
					text = strings.TrimPrefix(text, "path:")
					text = strings.SplitN(text, "#sha256=", 2)[0]
					if !filepath.IsAbs(text) {
						if path, err := absoluteQuestionnairePath(root, text); err == nil {
							text = path
						}
					}
					fmt.Fprintf(b, "Recorded Main check evidence candidate: %s (original ref %q)\n", text, original)
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
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(path)), "*", filepath.Base(path)))
	var previous []string
	for _, candidate := range paths {
		if candidate != path {
			previous = append(previous, candidate)
		}
	}
	if len(previous) == 0 {
		return ""
	}
	return " Source binding changed; previous questionnaires remain at " + strings.Join(previous, ", ") + ". Do not continue under those old bindings; use the exact current path."
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
		stringValue(pointer["contract_ref"]) == contract.Ref.Path && stringValue(pointer["contract_sha256"]) == contract.Ref.SHA256
	if stringValue(pointer["contract_id"]) == "" && stringValue(pointer["contract_ref"]) == contract.Ref.Path && stringValue(pointer["contract_sha256"]) == contract.Ref.SHA256 {
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
			Path: stringValue(pointer["contract_ref"]), SHA256: stringValue(pointer["contract_sha256"]),
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
		Path: stringValue(current["repair_contract_ref"]), SHA256: stringValue(current["repair_contract_sha256"]),
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
	if strings.TrimSpace(runtimeID) == "" || generation < 1 || strings.TrimSpace(caseID) == "" || strings.TrimSpace(contractID) == "" || strings.TrimSpace(contractRef.Path) == "" || strings.TrimSpace(contractRef.SHA256) == "" {
		return "", fmt.Errorf("Runtime, baseline generation, Case, and approved Contract reference are required for S9 Session/Plan IDs")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s\x00%s", runtimeID, generation, caseID, contractID, contractRef.Path, contractRef.SHA256)))
	return hex.EncodeToString(digest[:]), nil
}

func repairLeadQuestionnairePath(root string, state map[string]any, name string) (string, error) {
	runtimeID := strings.TrimSpace(stringValue(state["runtime_id"]))
	generation := integerValue(mapFieldCLI(state, "baseline")["generation"])
	segment := dispatchSlug(runtimeID)
	if segment == "" || generation < 1 || name == "" {
		return "", fmt.Errorf("current Runtime id, baseline generation or questionnaire name is missing")
	}
	pointer := repairPointerCLI(state)
	source := []string{
		stringValue(pointer["case_id"]), stringValue(pointer["contract_id"]), stringValue(pointer["contract_sha256"]),
		stringValue(pointer["session_id"]), stringValue(pointer["plan_ref"]), stringValue(pointer["plan_sha256"]),
	}
	if source[0] == "" || source[3] == "" || source[4] == "" || source[5] == "" {
		return "", fmt.Errorf("current approved Case, RepairSession, and RepairPlan source are required to identify the S9 questionnaire")
	}
	sourceDigest := sha256.Sum256([]byte(strings.Join(source, "\x00")))
	path := filepath.Join(root, ".claude", "submissions", "s9", segment, fmt.Sprintf("g%d", generation), hex.EncodeToString(sourceDigest[:]), name)
	return filepath.Abs(path)
}

func repairTargetedQuestionnairePath(root string, state map[string]any, assignmentID string, contexts ...context.Context) (string, error) {
	if len(contexts) > 0 && contexts[0] != nil {
		if err := contexts[0].Err(); err != nil {
			return "", err
		}
	}
	segment := dispatchSlug(assignmentID)
	if segment == "" {
		return "", fmt.Errorf("current verifier assignment id is empty")
	}
	legacy, err := repairLeadQuestionnairePath(root, state, "targeted-reverification-"+segment+".json")
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
	if info, statErr := os.Stat(path); statErr == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("original ChangeImpact questionnaire path %s is not a regular file", path)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	pointer := repairPointerCLI(state)
	sessionID := stringValue(pointer["session_id"])
	if sessionID == "" {
		return "", fmt.Errorf("current repair responsibility has no Session")
	}
	changeset, err := repair.CurrentRepairResultChangeset(root, state)
	if err != nil {
		return "", fmt.Errorf("read accepted Final Result changeset: %w", err)
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
		ImpactID:  "impact-" + dispatchSlug(sessionID),
		RuntimeID: stringValue(state["runtime_id"]), ReqID: stringValue(boundReq["id"]),
		BaselineGeneration: integerValue(mapFieldCLI(state, "baseline")["generation"]),
		SourceCaseIDs:      []string{caseID}, ChangeTypes: []string{}, ChangedArtifacts: changeset.Artifacts,
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
	impactID := "impact-" + dispatchSlug(sessionID)
	digest := sha256.Sum256([]byte(impactID))
	return []string{"reverify-" + hex.EncodeToString(digest[:])}, nil
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
	digest := sha256.Sum256([]byte(selection.impact.ImpactID + "\x00" + selection.performingAssignmentID))
	return "reverify-" + hex.EncodeToString(digest[:]), nil
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
	if rel, relErr := filepath.Rel(root, path); relErr == nil && strings.HasPrefix(filepath.ToSlash(rel), ".loop-submissions/s9/") {
		if err := targetedauthoring.CheckNormalOriginal(prepareContext, root, filepath.ToSlash(rel)); err != nil {
			return "", repair.TargetedReverificationRequest{}, err
		}
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
	ref := func(pathKey, shaKey, label string) (repair.ArtifactRef, error) {
		ref := repair.ArtifactRef{Path: stringValue(pointer[pathKey]), SHA256: stringValue(pointer[shaKey])}
		if ref.Path == "" {
			return ref, fmt.Errorf("%s has no recorded path", label)
		}
		return ref, nil
	}
	session, err := ref("path", "sha256", "current RepairSession")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	plan, err := ref("plan_ref", "plan_sha256", "current RepairPlan")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	contract, err := ref("contract_ref", "contract_sha256", "current RepairContract")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	result, err := ref("result_ref", "result_sha256", "current RepairResult")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	impact, err := ref("impact_ref", "impact_sha256", "current ChangeImpact")
	if err != nil {
		return repair.HandoffRequest{}, err
	}
	results := repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "")
	if len(results) == 0 {
		return repair.HandoffRequest{}, fmt.Errorf("current Runtime has no committed TargetedReverification artifact")
	}
	changeset, err := repair.CurrentRepairResultChangeset(root, state)
	if err != nil {
		return repair.HandoffRequest{}, fmt.Errorf("read accepted Final Result Changeset: %w", err)
	}
	changesetRef, err := persistRepairChangeset(root, changeset)
	if err != nil {
		return repair.HandoffRequest{}, fmt.Errorf("persist original Session Changeset: %w", err)
	}
	sessionID := stringValue(pointer["session_id"])
	request := repair.HandoffRequest{
		HandoffID: "repair-handoff-" + dispatchSlug(sessionID),
		Session:   session, Plan: plan, Contract: repair.ContractRef{Path: contract.Path, SHA256: contract.SHA256},
		Result: result, Changeset: changesetRef, ChangeImpact: impact,
		TargetedReverifications: results, HandedOffBy: "Main",
		NextAction: "start the next complete S7 review round from the current repair baseline",
	}
	return request, nil
}

func persistRepairChangeset(root string, changeset repair.Changeset) (repair.ArtifactRef, error) {
	ref, err := repair.PersistChangeset(root, changeset)
	if err == nil {
		return ref, nil
	}
	rel := filepath.ToSlash(filepath.Join(".claude", "review", "repair", "changesets", changeset.ChangesetID+".json"))
	existingRef, readErr := artifactRef(root, rel, changeset.ChangesetID)
	if readErr != nil {
		return repair.ArtifactRef{}, err
	}
	data, readErr := os.ReadFile(resolveRootPath(root, existingRef.Path))
	var existing repair.Changeset
	if readErr != nil || json.Unmarshal(data, &existing) != nil || existing.Digest != changeset.Digest || existing.SessionID != changeset.SessionID {
		return repair.ArtifactRef{}, err
	}
	// Match PersistChangeset's original wire reference. The local lookup ID is
	// not part of the Handoff ref schema and must not appear only on retries.
	existingRef.ID = ""
	return existingRef, nil
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
		ref := repair.ArtifactRef{Path: registered, SHA256: stringValue(pointer["handoff_sha256"])}
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
		rel := filepath.ToSlash(filepath.Join(".claude", "review", "repair", "handoffs", request.HandoffID+".json"))
		ref, err = artifactRef(root, rel, request.HandoffID)
		if err != nil {
			return repair.ArtifactRef{}, snapshot, err
		}
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

func repairPointerRef(pointer map[string]any, pathKey, hashKey, label string) (repair.ArtifactRef, error) {
	ref := repair.ArtifactRef{Path: stringValue(pointer[pathKey]), SHA256: stringValue(pointer[hashKey])}
	if ref.Path == "" || ref.SHA256 == "" {
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
		if stringValue(pointer["impact_ref"]) != "" {
			ref := repair.ArtifactRef{Path: stringValue(pointer["impact_ref"]), SHA256: stringValue(pointer["impact_sha256"])}
			err := repairLeadImpactReplay(root, path, ref)
			if err != nil {
				return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact replay conflict: "+err.Error()+"; preserve the registered artifact and use the original Main recovery. Runtime was not changed.")
			}
			return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact at "+stringValue(pointer["impact_ref"])+" is already registered. Replay did not write Runtime or register it again; wait for the independent verifier.")
		}
		if stringValue(pointer["status"]) == "impact_reconciliation" {
			return saveRepairImpactQuestionnaire(root, snapshot, path, stdout)
		}
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
			if registered, ok := registeredTargetedForSelection(root, pointer, selection); ok {
				if err := repairLeadTargetedReplay(root, path, registered); err != nil {
					return true, repairLeadFeedback(stdout, path, "S9 TargetedReverification replay conflict: "+err.Error()+"; preserve the registered artifact and use the original recovery. Runtime was not changed.")
				}
				return true, repairLeadFeedback(stdout, path, "S9 TargetedReverification artifact "+registered.Path+" is already registered. Replay did not write Runtime again.")
			}
			phase, phaseErr := currentRepairLeadPhase(root, snapshot.State)
			if phaseErr != nil || phase != "targeted_reverification" {
				return false, 0
			}
			return saveRepairTargetedQuestionnaire(root, snapshot, path, selection, stdout)
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
	if err := json.Unmarshal(data, &answer); err != nil {
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
	professional := func(types []string, decisions []repair.ImpactDecision, escalation string, invalid, superseded, retained []string) any {
		if escalation == "" {
			escalation = "assignment" // Original CreateChangeImpact default.
		}
		return []any{repairLeadStringSet(types), decisions, escalation, repairLeadStringSet(invalid), repairLeadStringSet(superseded), repairLeadStringSet(retained)}
	}
	return repairLeadCompareProfessional(
		professional(answer.ChangeTypes, answer.Decisions, answer.EscalationLevel, answer.InvalidatedEvidenceIDs, answer.SupersededEvidenceIDs, answer.RetainedEvidenceIDs),
		professional(registered.ChangeTypes, registered.Decisions, registered.EscalationLevel, registered.InvalidatedEvidenceIDs, registered.SupersededEvidenceIDs, registered.RetainedEvidenceIDs))
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
	return repairLeadCompareProfessional(
		[]any{answer.ContinuityReason, answer.AssertionResults, answer.ScopeCompliance, answer.Result, answer.FailureClass},
		[]any{registered.ContinuityReason, registered.AssertionResults, registered.ScopeCompliance, registered.Result, registered.FailureClass})
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
		if value != "" {
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

func registeredTargetedForSelection(root string, pointer map[string]any, selection repairTargetedSelection) (repair.ArtifactRef, bool) {
	for _, ref := range repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "") {
		var value repair.TargetedReverification
		err := repairLeadIndexedSource(root, ref, &value)
		if err == nil && value.Result == "pass" && value.ImpactID == selection.impact.ImpactID && value.OriginalAssignmentID == selection.originalAssignmentID && value.PerformingAssignmentID == selection.performingAssignmentID {
			return ref, true
		}
	}
	return repair.ArtifactRef{}, false
}

// Recover only the original system timestamp after artifact creation was
// interrupted before Commit. The original Create consumer still compares the
// complete immutable bytes; reading this value never accepts changed content.
func repairLeadArtifactTime(root, relative, field string, fallback time.Time) (time.Time, error) {
	data, err := workpacket.ReadFile(root, filepath.ToSlash(relative), 20<<20)
	if os.IsNotExist(err) {
		return fallback, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return time.Time{}, err
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(data, &original); err != nil {
		return time.Time{}, err
	}
	var recorded time.Time
	if err := json.Unmarshal(original[field], &recorded); err != nil || recorded.IsZero() {
		return time.Time{}, fmt.Errorf("%s has no valid original %s", relative, field)
	}
	return recorded, nil
}

func saveRepairImpactQuestionnaire(root string, snapshot runtime.Snapshot, path string, stdout io.Writer) (bool, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact answer at %s could not be read: %v", path, err))
	}
	var answer repair.ChangeImpactRequest
	if err := json.Unmarshal(data, &answer); err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact answer at %s is not a valid original request DTO: %v", path, err))
	}
	if len(answer.Decisions) == 0 {
		return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact answer at "+path+" is incomplete: use the original flat request DTO with change_types and decisions at the top level, not an answer wrapper or a replacement questionnaire. Record professional dispositions and their actual rationales; record change types when the supplied Final artifacts actually changed. This unreceived authoring draft is corrected at the same exact path; fresh IDs/files apply to immutable registered artifacts, not this draft.")
	}
	pointer := repairPointerCLI(snapshot.State)
	sessionID := stringValue(pointer["session_id"])
	if sessionID == "" {
		return true, repairLeadFeedback(stdout, path, "current repair responsibility has no Session")
	}
	changeset, err := repair.CurrentRepairResultChangeset(root, snapshot.State)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact answer could not read the accepted Final Result changeset: "+err.Error())
	}
	if len(changeset.Artifacts) > 0 && len(answer.ChangeTypes) == 0 {
		return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact is incomplete: record the actual change_types for the supplied accepted Final artifacts at "+path)
	}
	reverificationIDs, err := repairImpactReverificationIDs(snapshot.State)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact answer could not derive the Contract reverification obligation from the current RepairSession: "+err.Error())
	}
	boundReq := mapFieldCLI(snapshot.State, "bound_req")
	answer.ImpactID = "impact-" + dispatchSlug(sessionID)
	answer.RuntimeID = stringValue(snapshot.State["runtime_id"])
	answer.ReqID = stringValue(boundReq["id"])
	answer.BaselineGeneration = integerValue(mapFieldCLI(snapshot.State, "baseline")["generation"])
	answer.SourceBugIDs = []string{}
	answer.SourceCaseIDs = []string{stringValue(pointer["case_id"])}
	answer.ChangedArtifacts = changeset.Artifacts
	answer.RequiredReverificationIDs = reverificationIDs
	answer.AnalyzedBy = "Main"
	answer.AnalyzedAt, err = repairLeadArtifactTime(root, filepath.Join(".claude/review/repair/change-impact", answer.ImpactID+".json"), "analyzed_at", time.Now().UTC())
	if err != nil {
		return true, repairLeadFeedback(stdout, path, "S9 ChangeImpact original artifact time is unavailable; preserve its immutable source: "+err.Error())
	}
	if err := persistRepairLeadQuestionnaire(path, answer); err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact answer at %s could not persist its original DTO: %v", path, err))
	}
	_, ref, err := repair.CreateChangeImpact(root, answer)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact request at %s did not create the original artifact: %v", path, err))
	}
	committed, err := repair.CommitChangeImpact(root, filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl"), repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, Impact: ref})
	if err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 ChangeImpact artifact %s was created but the original Runtime consumer did not commit it: %v. Preserve this immutable artifact and follow the named repair source.", ref.Path, err))
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
		return answer, nativeArtifactReadError("read S9 TargetedReverification answer "+path, err)
	}
	if capture != nil {
		capture.SetInput(path)
		capture.ObserveInput(path, data)
		capture.SetStage("targeted_request_read")
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return answer, &nativeAuthorError{fmt.Errorf("S9 TargetedReverification answer at %s is not a valid original request DTO: %w", path, err)}
	}
	if strings.TrimSpace(answer.Result) == "" {
		return answer, &nativeAuthorError{fmt.Errorf("S9 TargetedReverification answer at %s must record the actual independent verdict and assertion observations before automatic reception; ordinary saves can retain unfinished work", path)}
	}
	return answer, nil
}

func saveRepairTargetedAnswer(root string, snapshot runtime.Snapshot, path string, selection repairTargetedSelection, answer repair.TargetedReverificationRequest, stdout io.Writer) (bool, int) {
	handled, code, _ := saveRepairTargetedAnswerContext(context.Background(), root, snapshot, path, selection, answer, stdout)
	return handled, code
}

func saveRepairTargetedAnswerContext(ctx context.Context, root string, snapshot runtime.Snapshot, path string, selection repairTargetedSelection, answer repair.TargetedReverificationRequest, stdout io.Writer, captures ...*nativeS9AttemptCapture) (bool, int, error) {
	if strings.Contains(filepath.ToSlash(path), "/.loop-submissions/s9/") {
		normal, sourceErr := repair.UsesSharedTargetedAuthoring(root, snapshot.State, selection.originalAssignmentID, selection.performingAssignmentID, selection.verifierOwner, ctx)
		if sourceErr != nil {
			return true, repairLeadFeedback(stdout, path, sourceErr.Error()), sourceErr
		}
		if !normal {
			sourceErr = fmt.Errorf("Targeted ordinary original has no verified normal source contract")
			return true, repairLeadFeedback(stdout, path, sourceErr.Error()), sourceErr
		}
	}
	if rel, relErr := filepath.Rel(root, path); relErr == nil && strings.HasPrefix(filepath.ToSlash(rel), ".loop-submissions/s9/") {
		if err := targetedauthoring.CheckNormalOriginal(ctx, root, filepath.ToSlash(rel)); err != nil {
			return true, repairLeadFeedback(stdout, path, err.Error()), err
		}
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	var err error

	pointer := repairPointerCLI(snapshot.State)
	blockedResume := stringValue(pointer["status"]) == "blocked" && stringValue(pointer["failure_route"]) == "blocked"
	if blockedResume && answer.Result == "blocked" {
		if registered, ok := registeredBlockedTargetedForSelection(root, pointer, selection); ok {
			if err := repairLeadTargetedReplay(root, path, registered); err != nil {
				return true, repairLeadFeedback(stdout, path, "Targeted same-attempt replay conflict: "+err.Error()), nil
			}
			if capture := nativeS9Capture(captures); capture != nil {
				capture.ConsumerSucceeded()
				capture.SetStage("targeted_received")
			}
			return true, repairLeadFeedback(stdout, path, "S9 environmental blocker is already recorded by "+registered.Path+". Resolve it, then update the same TargetedReverification questionnaire with the actual assertion observations, results, evidence and any unfinished-work reason; Runtime was not changed."), nil
		}
	}
	answer.RuntimeID = stringValue(snapshot.State["runtime_id"])
	answer.CaseID = stringValue(pointer["case_id"])
	answer.BugID = ""
	answer.BaselineGeneration = integerValue(mapFieldCLI(snapshot.State, "baseline")["generation"])
	answer.OriginalAssignmentID = selection.originalAssignmentID
	answer.PerformingAssignmentID = selection.performingAssignmentID
	answer.ImpactID = selection.impact.ImpactID
	answer.ReverificationID, err = repairTargetedIDForSelection(selection, nil, nil)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, err.Error()), nil
	}
	// The author reports observations, not a timestamp. Record reception
	// here; an uncommitted artifact retains its original system time.
	answer.PerformedAt = time.Now().UTC()
	currentSnapshot := snapshot
	currentPointer := repairPointerCLI(currentSnapshot.State)
	if blockedResume {
		resumed, resumeErr := repair.ResumeTargetedReverification(root, statePath, journalPath, repair.ResumeTargetedRequest{
			RuntimeRequest: repair.RuntimeRequest{Context: ctx, ExpectedRevision: currentSnapshot.Revision, Actor: selection.verifierOwner, OccurredAt: answer.PerformedAt},
			Reason:         answer.ContinuityReason,
		})
		if resumeErr != nil {
			return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 recorded environmental blocker at %s could not be resumed through the original Runtime consumer: %v", path, resumeErr)), nil
		}
		currentSnapshot = resumed
		currentPointer = repairPointerCLI(currentSnapshot.State)
	}
	var resolvedAt time.Time
	artifactPath := filepath.Join(".claude/review/repair/reverification", answer.ReverificationID+".json")
	if resolvedAtText := stringValue(currentPointer["blocker_resolved_at"]); resolvedAtText != "" {
		resolvedAt, err = time.Parse(time.RFC3339Nano, resolvedAtText)
		if err != nil {
			return true, repairLeadFeedback(stdout, path, "S9 Runtime blocker_resolved_at is invalid; preserve the original Runtime source for reconciliation: "+err.Error()), nil
		}
		artifactPath = filepath.Join(".claude/review/repair/reverification", answer.ReverificationID+"-resume-"+resolvedAt.UTC().Format("20060102T150405.000000000Z")+".json")
	}
	answer.PerformedAt, err = repairLeadArtifactTime(root, artifactPath, "performed_at", answer.PerformedAt)
	if err != nil {
		return true, repairLeadFeedback(stdout, path, "S9 TargetedReverification original artifact time is unavailable; preserve its immutable source: "+err.Error()), nil
	}
	if err := persistRepairLeadQuestionnaire(path, answer); err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 TargetedReverification answer at %s could not persist its original DTO: %v", path, err)), nil
	}
	var value repair.TargetedReverification
	var ref repair.ArtifactRef
	var createErr error
	if !resolvedAt.IsZero() {
		value, ref, createErr = repair.CreateTargetedReverificationForResume(root, answer, resolvedAt)
	} else {
		value, ref, createErr = repair.CreateTargetedReverification(root, answer)
	}
	if createErr != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 TargetedReverification request at %s did not create the original artifact: %v", path, createErr)), nil
	}
	committed, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{Context: ctx, ExpectedRevision: currentSnapshot.Revision, Actor: selection.verifierOwner}, Reverification: ref})
	if err != nil {
		return true, repairLeadFeedback(stdout, path, fmt.Sprintf("S9 independent TargetedReverification %s at %s was created but original Runtime consumer did not commit it: %v. Preserve its immutable source and follow the specific consumer feedback.", value.ReverificationID, ref.Path, err)), nil
	}
	if capture := nativeS9Capture(captures); capture != nil {
		capture.ConsumerSucceeded()
		capture.SetStage("targeted_received")
	}
	var exitFailure error
	message := fmt.Sprintf("S9 independent TargetedReverification %s at %s was committed through the original Runtime Writer.", value.ReverificationID, ref.Path)
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
	var latest repair.ArtifactRef
	for _, ref := range repairArtifactRefs(pointer, "targeted_reverification_artifacts", "", "") {
		var value repair.TargetedReverification
		err := repairLeadIndexedSource(root, ref, &value)
		if err == nil && value.Result == "blocked" && value.FailureClass == "blocked" && value.ImpactID == selection.impact.ImpactID && value.OriginalAssignmentID == selection.originalAssignmentID && value.PerformingAssignmentID == selection.performingAssignmentID {
			latest = ref
		}
	}
	return latest, latest.Path != ""
}

func repairLeadFeedback(stdout io.Writer, path, message string) int {
	message = "Questionnaire: " + path + ". " + message
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": message}})
	return 0
}
