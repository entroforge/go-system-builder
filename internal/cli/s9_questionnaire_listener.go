package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// S9 reminders read the current question and indexed work facts. They do not
// reconstruct receipt, baseline, execution-end or recovery proofs.
type s9ListenerQuestionnaire struct {
	id, view, answer    string
	originalRoot        string
	executionAssignment string
	binding             workpacket.RepairPlanReportBinding
	result              *workpacket.RepairResultBinding
	draft               any
}

type s9ListenerFact struct {
	kind, sourceRoot, ref string
	// unknown records a budget boundary, never absence or source corruption.
	unknown       bool
	registeredRef string
}

func s9PacketQuestionnaireLifecycleContext(root string, request policy.Input, snapshot runtime.Snapshot) (string, bool) {
	life := mapFieldCLI(snapshot.State, "lifecycle")
	if !runtime.PacketEnabled(snapshot.State) || life["state"] != "bug_resolution" {
		return "", false
	}
	result := life["phase"] == "fixing"
	if mainQuestionnaireViewer(request) && repairDispatchCurrent(snapshot.State) {
		return stageRepairDispatchContext(root, request, snapshot), true
	}
	if !result && life["phase"] != "planning" && life["phase"] != "reproducing" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(contextForHook(request), 4*time.Second)
	defer cancel()
	return s9QuestionnaireContext(ctx, root, request, nil, snapshot, request.EffectiveAgentID(), result), true
}

func s9PlanQuestionnaireContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	return s9QuestionnaireContext(ctx, root, request, service, snapshot, owner, false)
}

func s9ResultQuestionnaireContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	return s9QuestionnaireContext(ctx, root, request, service, snapshot, owner, true)
}

func s9QuestionnaireContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string, result bool) string {
	main := mainQuestionnaireViewer(request) && (owner == "" || owner == "Main")
	if owner == "" && (request.AgentType != "" || request.AgentTranscriptPath != "" || request.Event == "SubagentStart" || request.Event == "SubagentStop" || request.Event == "TeammateIdle") {
		return "S9 native Worker responsibility is not identified by this event. Main must give this Worker its exact original brief or Targeted answer reference in the actual task prompt. Save the ready original answer for PreToolUse reception, with successful PostToolUse and Stop/SubagentStop fallback; reception does not wait for a final message or returned reference. No Main responsibility was inferred."
	}
	if owner != "" && owner != "Main" && !s9ListenerOwnerPresent(snapshot.State, owner) {
		return "S9 native Worker responsibility is not identified in the Runtime registry by this event. Use the exact brief or Targeted answer reference Main already supplied in your actual task prompt. A Builder writes the brief's private worktree outputs; an independent verifier writes its original Targeted answer. Save the ready original answers for automatic reception via PreToolUse, with successful PostToolUse and Stop/SubagentStop fallback; ready original answers use the same natural-event listener; no extra submit or done is needed. Main must not author this Worker's professional answer. No Runtime owner or Main responsibility was inferred from the platform ID or role."
	}
	filter := owner
	if main {
		filter = ""
	}
	packets, err := s9CurrentQuestionnaires(root, snapshot.State, filter, result, ctx)
	if err != nil {
		known := ""
		if main {
			known = s9KnownMainHandoffsContext(ctx, root, snapshot)
		}
		return "S9 questionnaire projection is unavailable; " + s9ListenerKnownPaths(root, snapshot.State, filter, result) + ". Main owns the recorded packet preparation gap: " + err.Error() + ". " + known
	}
	var lines []string
	nativePackets := s9ListenerNativePlans(root, snapshot.State)
	for _, packet := range packets {
		native, isNative := s9ListenerNativePacket(snapshot.State, nativePackets, packet)
		fact, factErr := s9ListenerAcceptanceContext(ctx, root, snapshot.State, packet)
		source, execution, sourceErr := s9ListenerResultSource(root, snapshot.State, packet)
		if packet.result == nil {
			source, sourceErr = root, nil // Original pre-execution PlanReport source.
		}
		if fact.sourceRoot != "" {
			source = fact.sourceRoot // Original historical source survives Worker cleanup.
		}
		paths := s9ListenerPacketPaths(source, packet)
		prefix := fmt.Sprintf("S9 Builder %s assignment=%s: %s. ", packet.binding.OwnerAgentID, packet.binding.RepairAssignmentID, paths)
		var nativeAuthorErr error
		if isNative {
			brief, _, _ := nativeS9Paths(native)
			planPath, resultPath, pathErr := nativeS9AuthorPaths(root, native)
			nativeAuthorErr = pathErr
			prefix = fmt.Sprintf("S9 Builder %s assignment=%s: exact native brief %s; read-only professional question %s; private worktree-relative PlanReport %s and Result %s. ", packet.binding.OwnerAgentID, packet.binding.RepairAssignmentID, resolveRootPath(root, brief), resolveRootPath(root, native.ViewPath), planPath, resultPath)
			if pathErr != nil {
				prefix = fmt.Sprintf("S9 Builder %s assignment=%s: exact native brief %s; %s. ", packet.binding.OwnerAgentID, packet.binding.RepairAssignmentID, resolveRootPath(root, brief), s9NativeAuthorCoordinatesFailure(root, native, pathErr, main))
			}
			if packet.result != nil {
				prefix += "Original Result receiver reference (system-owned): " + resolveRootPath(source, packet.answer) + ". "
			}
		}
		if fact.unknown {
			note := "Original reception detail was not refreshed within this request; acceptance and professional completion are unknown. Preserve the original responsibility, exact question paths and any independently accepted work; no repeated Plan or Result submission is inferred."
			if fact.registeredRef != "" {
				note += " Registered candidate reference: " + resolveRootPath(root, fact.registeredRef) + "; this index alone does not establish acceptance or completion."
			}
			lines = append(lines, prefix+note)
			continue
		}
		if factErr != nil {
			lines = append(lines, prefix+"Original acceptance source is unavailable; preserve this questionnaire and the recorded artifact: "+factErr.Error())
			continue
		}
		if fact.kind != "" {
			candidateState := s9CandidateComplete
			next := "The PlanReport is recorded. Continue the same native Builder's repair, self-checks and private Result using its exact prepared brief; PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required; reception does not wait for a final message or returned reference. Main owns integration and necessary checks. Do not repeat the accepted Plan or add a workspace startup step."
			if isNative && nativeAuthorErr != nil {
				next = "The PlanReport is recorded; preserve the accepted Plan. Main owns the original preparation gap before the same Builder continues; no repeated Plan is due."
			}
			_, originalExecution := repairExecutionForChoice(snapshot.State, repairDispatchChoice{AssignmentID: packet.binding.RepairAssignmentID, AgentID: packet.binding.OwnerAgentID})
			if len(originalExecution) > 0 && originalExecution["purpose"] != "native_delivery" {
				next = fmt.Sprintf("The PlanReport is recorded for an existing frozen execution. Main preserves its original workspace recovery: runtime workspace prepare --root %q --assignment %q --agent %q --check-location %s; then supplies this Builder’s exact RepairResult questionnaire. Do not repeat the accepted Plan or division answer.", root, packet.binding.ManifestAssignmentID, packet.binding.OwnerAgentID, stringValue(originalExecution["check_location"]))
			}
			switch fact.kind {
			case "candidate":
				assignment := stringValue(execution["assignment_id"])
				if assignment == "" {
					assignment = packet.binding.ManifestAssignmentID
				}
				next = s9CandidateAction(root, assignment, packet.binding.OwnerAgentID)
				if packet.result != nil {
					if err := ctx.Err(); err != nil {
						candidateState = s9CandidateUnknown
						next = "Candidate reception is recorded; current professional completion is unknown: " + err.Error()
					} else if document, err := s9ObservedDocument(root, fact.ref); err != nil {
						candidateState = s9CandidateUnknown
						next = "Candidate reception is recorded; Main restores its original source: " + err.Error()
					} else {
						state, reason := s9ObservedCandidateProgress(document)
						candidateState = state
						if state == s9CandidateUnknown {
							next = "Candidate reception is recorded; current professional completion is unknown: " + reason
						} else if state == s9CandidateUnfinished {
							next = "Candidate reception is recorded; actual professional work remains unfinished: " + reason + ". Main follows these original omissions without repeating accepted content."
						}
					}
				}
			case "result":
				next = s9FinalAction()
			case "contract_conflict":
				next = "The original Contract-conflict answer is recorded; Main owns the original S8 RepairContract revision and human approval. Do not fill a same-Contract retry resolution."
				if ref := stringValue(repairPointerCLI(snapshot.State)["contract_ref"]); ref != "" {
					next += " Current Contract: " + filepath.Join(root, filepath.FromSlash(ref)) + "."
				}
			case "blocker":
				resolution, pathErr := workpacket.RepairResultResolutionAuthoringPath(workpacket.RepairResultPacket{AnswerPath: packet.answer, Binding: *packet.result})
				if pathErr == nil {
					next = "The original blocker is recorded; Main must fill and save its recovery decision at " + filepath.Join(root, filepath.FromSlash(resolution)) + ". This reminder does not grant execution completion."
				} else {
					next = "The original blocker is recorded; Main's recovery questionnaire path is unavailable: " + pathErr.Error()
				}
			}
			if !main && (fact.kind == "candidate" || fact.kind == "result") {
				next = "Your original professional answer is recorded. Main owns integration, necessary checks and remaining checkpoint ACK/cleanup; no further questionnaire submission is due from this completed Worker."
				if fact.kind == "candidate" && candidateState == s9CandidateUnknown {
					next = "Your candidate reception is recorded; current professional completion is unknown. Main restores the original status without repeating accepted work."
				}
				if fact.kind == "candidate" && candidateState == s9CandidateUnfinished {
					next = "Your original candidate is recorded; Main arranges any recorded unfinished professional work without repeating accepted content."
				}
			}
			lines = append(lines, prefix+next)
			continue
		}
		if isNative {
			if main && packet.result != nil {
				handoff := s9HandoffForBinding(ctx, root, snapshot, packet.binding)
				lines = append(lines, prefix+handoff.summary(root)+" "+s9UnreceivedActionContext(ctx, root, handoff))
				continue
			}
			note := "Use the brief's private outputs for professional fields. Save PlanReport before implementation, continue actual repair/self-checks, then save Result and report results to Main when finished; PreToolUse receives the ready original answers, with successful PostToolUse and Stop/SubagentStop fallback, without activation or a returned-reference prerequisite."
			if nativeAuthorErr != nil {
				note = "Original preparation is unavailable. Main owns this gap; preserve the original responsibility and independently accepted work without guessing private author coordinates."
			}
			if main && nativeAuthorErr == nil {
				note += " Main supplies this exact brief to the actual native Builder; the Main-root question is read-only input."
			}
			if ctx.Err() != nil {
				note += " Current filesystem preparation was not refreshed; preserve the original question and any independently accepted work."
			} else if !s8QuestionnaireFileExists(resolveRootPath(root, native.ViewPath)) {
				note += " Main/system must restore the missing original professional question before work continues."
			}
			lines = append(lines, prefix+note)
			continue
		}
		if sourceErr != nil {
			lines = append(lines, prefix+"Registered Worker source is not ready; Main owns this exact execution preparation gap: "+sourceErr.Error())
			continue
		}
		if ctx.Err() != nil {
			lines = append(lines, prefix+"The original questionnaire is registered at these exact paths. Filesystem preparation was not refreshed within this request; Main/system preserves the original preparation responsibility without issuing or repeating an answer.")
			continue
		}
		missing := ""
		if !s8QuestionnaireFileExists(filepath.Join(source, filepath.FromSlash(packet.view))) || !s8QuestionnaireFileExists(filepath.Join(source, filepath.FromSlash(packet.answer))) {
			missing = " The original view/answer is not prepared; Main/system must complete normal responsibility preparation at these exact paths before this answer can be filled."
		}
		note := "Fill only the issued professional answer and save this exact questionnaire for the original consumer."
		if main {
			note += " Main must give this exact view and answer path to this Builder in the actual native Worker task prompt."
		}
		lines = append(lines, prefix+note+missing)
	}
	if len(lines) == 0 {
		kind := "PlanReport"
		if result {
			kind = "RepairResult"
		}
		return fmt.Sprintf("S9 %s questionnaire is not currently prepared for owner=%s; %s. Main owns the current Assignment dispatch and registered Worker preparation; a native Worker must receive its actual exact path from Main.", kind, owner, s9ListenerKnownPaths(root, snapshot.State, filter, result))
	}
	return strings.Join(lines, "\n")
}

// A prepared native brief identifies the authoring route. Existing frozen
// executions retain their original questionnaire and recovery contract.
func s9ListenerNativePacket(state map[string]any, packets []workpacket.RepairPlanReportPacket, question s9ListenerQuestionnaire) (workpacket.RepairPlanReportPacket, bool) {
	_, execution := repairExecutionForChoice(state, repairDispatchChoice{AssignmentID: question.binding.RepairAssignmentID, AgentID: question.binding.OwnerAgentID})
	if len(execution) > 0 && execution["purpose"] != "native_delivery" {
		return workpacket.RepairPlanReportPacket{}, false
	}
	for _, packet := range packets {
		if packet.Binding.RepairAssignmentID == question.binding.RepairAssignmentID && packet.Binding.OwnerAgentID == question.binding.OwnerAgentID {
			return packet, true
		}
	}
	return workpacket.RepairPlanReportPacket{}, false
}

// Current questions follow each Assignment's recorded Plan, independently of
// another Builder having started implementation. This projection is read-only.
func s9CurrentQuestionnaires(root string, state map[string]any, owner string, result bool, contexts ...context.Context) ([]s9ListenerQuestionnaire, error) {
	ctx := context.Background()
	if len(contexts) != 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	if !result {
		return s9ListenerQuestionnairesContext(ctx, root, state, owner, false)
	}
	plans, err := s9ListenerQuestionnairesContext(ctx, root, state, owner, false)
	if err != nil {
		return nil, err
	}
	results, err := s9ListenerQuestionnairesContext(ctx, root, state, owner, true)
	if err != nil {
		return nil, err
	}
	var current []s9ListenerQuestionnaire
	for _, plan := range plans {
		fact, err := s9ListenerAcceptanceContext(ctx, root, state, plan)
		if fact.unknown {
			current = append(current, plan)
			for _, result := range results {
				if result.binding == plan.binding {
					current = append(current, result)
				}
			}
			continue
		}
		if err != nil || fact.kind == "" {
			current = append(current, plan)
			continue
		}
		found := false
		for _, result := range results {
			if result.binding.RepairAssignmentID == plan.binding.RepairAssignmentID {
				current = append(current, result)
				found = true
			}
		}
		if !found {
			current = append(current, plan)
		}
	}
	// Existing Result-only Runtime snapshots remain readable.
	for _, result := range results {
		found := false
		for _, plan := range plans {
			if plan.binding.RepairAssignmentID == result.binding.RepairAssignmentID {
				found = true
			}
		}
		if !found {
			current = append(current, result)
		}
	}
	return current, nil
}

func s9ListenerQuestionnaires(state map[string]any, owner string, result bool) ([]s9ListenerQuestionnaire, error) {
	return s9ListenerQuestionnairesContext(context.Background(), "", state, owner, result)
}
func s9ListenerQuestionnairesContext(ctx context.Context, root string, state map[string]any, owner string, result bool) ([]s9ListenerQuestionnaire, error) {
	// This is a pure projection of already-read facts. Cancellation forbids
	// new I/O; it does not erase known responsibilities, paths or progress.
	pointer := repairPointerCLI(state)
	baseline, review := mapFieldCLI(state, "baseline"), mapFieldCLI(state, "review")
	owners := mapFieldCLI(pointer, "assignment_owners")
	rows := mapFieldCLI(state, "entities")["work_packets"]
	if rows == nil {
		return nil, nil
	}
	array, ok := rows.([]any)
	if !ok {
		return nil, fmt.Errorf("Runtime work_packets is not an array")
	}
	template := "s9-plan-report-v1"
	if result {
		template = "s9-repair-result-v1"
	}
	var out []s9ListenerQuestionnaire
	seen := map[string]bool{}
	for _, raw := range array {
		row, _ := raw.(map[string]any)
		_, external, formatErr := packetbody.ReferenceFromIndex(row)
		if formatErr != nil {
			return nil, formatErr
		}
		if row["template_id"] != template {
			continue
		}
		binding := mapFieldCLI(row, "binding")
		if result {
			binding = mapFieldCLI(binding, "sources")
		}
		if owner != "" && binding["owner_agent_id"] != owner || binding["runtime_id"] != state["runtime_id"] ||
			tolerantInt(binding["baseline_generation"]) != tolerantInt(baseline["generation"]) || tolerantInt(binding["review_round"]) != tolerantInt(review["round"]) ||
			binding["session_id"] != pointer["session_id"] || binding["plan_ref"] != pointer["plan_ref"] ||
			owners[stringValue(binding["repair_assignment_id"])] != binding["owner_agent_id"] {
			continue
		}
		packet := s9ListenerQuestionnaire{id: stringValue(row["packet_id"]), view: stringValue(row["view_path"]), answer: stringValue(row["answer_path"]), draft: row["draft"]}
		encoded, err := json.Marshal(binding)
		if err == nil {
			err = json.Unmarshal(encoded, &packet.binding)
		}
		if result && err == nil {
			var resultBinding workpacket.RepairResultBinding
			encoded, err = json.Marshal(row["binding"])
			if err == nil {
				err = json.Unmarshal(encoded, &resultBinding)
			}
			packet.result = &resultBinding
			original := mapFieldCLI(mapFieldCLI(row, "inputs"), "execution")
			if external {
				original = mapFieldCLI(row, "execution_locator")
			}
			packet.originalRoot = stringValue(original["worktree_path"])
			packet.executionAssignment = stringValue(original["assignment_id"])
		}
		if err != nil {
			return out, err
		}
		if packet.id == "" || packet.view == "" || packet.answer == "" {
			return out, fmt.Errorf("current S9 packet has no exact identity/view/answer path")
		}
		if packet.result != nil {
			if s9ListenerResolutionRecorded(state, packet) {
				continue
			}
			execution := s9ObservedExecution(state, packet)
			if len(execution) > 0 {
				if stringValue(execution["rework_ref"]) != packet.result.ReworkRef {
					continue
				}
				original := mapFieldCLI(mapFieldCLI(row, "inputs"), "execution")
				if external {
					original = mapFieldCLI(row, "execution_locator")
				}
				if tolerantInt(execution["execution_generation"]) != tolerantInt(original["execution_generation"]) || stringValue(execution["worktree_path"]) != packet.originalRoot {
					continue
				}
			}
		}
		if seen[packet.binding.RepairAssignmentID] {
			return out, fmt.Errorf("current S9 Assignment has ambiguous questionnaire packets")
		}
		seen[packet.binding.RepairAssignmentID] = true
		out = append(out, packet)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

// This is a source-path projection, not workspace.Decode/ValidateAuthority:
// reminders do not revalidate every unrelated execution and check receipt.
func s9ListenerResultSource(root string, state map[string]any, packet s9ListenerQuestionnaire) (string, map[string]any, error) {
	if packet.result == nil {
		return root, nil, nil
	}
	known := packet.originalRoot
	if !filepath.IsAbs(known) || samePath(known, root) {
		known = ""
	}
	selected := s9ObservedExecution(state, packet)
	path := stringValue(selected["worktree_path"])
	if !filepath.IsAbs(path) || samePath(path, root) {
		return known, selected, fmt.Errorf("exact registered Worker root is unavailable; no Main-root fallback")
	}
	if known == "" {
		return "", selected, fmt.Errorf("original registered Worker source locator is unavailable; no Main-root fallback")
	}
	if !samePath(path, known) {
		return known, selected, fmt.Errorf("current Worker root differs from the recorded original source; no source replacement was inferred")
	}
	return known, selected, nil
}

func s9ListenerPacketPaths(root string, packet s9ListenerQuestionnaire) string {
	if root == "" {
		return "recorded questionnaire-relative path " + packet.answer + "; view-relative path " + packet.view + " (registered Worker root unavailable)"
	}
	return "questionnaire " + filepath.Join(root, filepath.FromSlash(packet.answer)) + "; read " + filepath.Join(root, filepath.FromSlash(packet.view))
}

func s9ListenerKnownPaths(root string, state map[string]any, owner string, result bool) string {
	if owner != "" && owner != "Main" && !s9ListenerOwnerPresent(state, owner) {
		return "this platform event cannot locate a registered Runtime responsibility; use the exact native brief or Targeted answer already supplied by Main, save your own ready professional output for automatic reception via PreToolUse, with successful PostToolUse and Stop/SubagentStop fallback"
	}
	var paths []string
	nativePackets := s9ListenerNativePlans(root, state)
	template := "s9-plan-report-v1"
	if result {
		template = "s9-repair-result-v1"
	}
	for _, row := range objectSliceAny(mapFieldCLI(state, "entities")["work_packets"]) {
		if row["template_id"] != template {
			continue
		}
		binding := mapFieldCLI(row, "binding")
		if result {
			binding = mapFieldCLI(binding, "sources")
		}
		if owner != "" && binding["owner_agent_id"] != owner || binding["runtime_id"] != state["runtime_id"] || tolerantInt(binding["baseline_generation"]) != tolerantInt(mapFieldCLI(state, "baseline")["generation"]) || binding["session_id"] != repairPointerCLI(state)["session_id"] {
			continue
		}
		packet := s9ListenerQuestionnaire{answer: stringValue(row["answer_path"]), view: stringValue(row["view_path"])}
		packet.binding.RepairAssignmentID = stringValue(binding["repair_assignment_id"])
		packet.binding.OwnerAgentID = stringValue(binding["owner_agent_id"])
		if packet.answer == "" {
			continue
		}
		if native, ok := s9ListenerNativePacket(state, nativePackets, packet); ok {
			brief, _, _ := nativeS9Paths(native)
			planPath, resultPath, pathErr := nativeS9AuthorPaths(root, native)
			if pathErr != nil {
				paths = append(paths, fmt.Sprintf("native brief %s; %s", resolveRootPath(root, brief), s9NativeAuthorCoordinatesFailure(root, native, pathErr, owner == "" || owner == "Main")))
			} else {
				paths = append(paths, fmt.Sprintf("native brief %s; private worktree-relative PlanReport %s and Result %s", resolveRootPath(root, brief), planPath, resultPath))
			}
			continue
		}
		source := root
		if result {
			// Frozen input is a recorded diagnostic location, not a new
			// authoring grant when the live execution is absent or changed.
			original := mapFieldCLI(mapFieldCLI(row, "inputs"), "execution")
			if _, external, _ := packetbody.ReferenceFromIndex(row); external {
				original = mapFieldCLI(row, "execution_locator")
			}
			source = stringValue(original["worktree_path"])
			if !filepath.IsAbs(source) || samePath(source, root) {
				source = ""
			}
		}
		paths = append(paths, s9ListenerPacketPaths(source, packet))
	}
	if len(paths) == 0 {
		pointer := repairPointerCLI(state)
		if plan := stringValue(pointer["plan_ref"]); plan != "" {
			return "exact questionnaire path is not prepared; current RepairPlan " + filepath.Join(root, filepath.FromSlash(plan))
		}
		return "exact questionnaire path is not prepared; current Runtime has no RepairPlan reference"
	}
	return strings.Join(paths, "; ")
}

func s9ReadListenerDocument(root, path, _ string) (map[string]any, error) {
	// The historical digest argument remains for existing observation callers.
	// This is a path read for reminders, never a source-authorization proof.
	return s9ObservedDocument(root, path)
}

func s9ListenerAcceptance(root string, state map[string]any, packet s9ListenerQuestionnaire) (s9ListenerFact, error) {
	return s9ListenerAcceptanceContext(context.Background(), root, state, packet)
}

func s9ListenerAcceptanceContext(ctx context.Context, root string, state map[string]any, packet s9ListenerQuestionnaire) (s9ListenerFact, error) {
	known := s9ListenerFact{}
	if packet.result != nil {
		_, execution, _ := s9ListenerResultSource(root, state, packet)
		known.registeredRef = stringValue(execution["delivery_ref"])
	}
	budgetUnknown := func(err error) (s9ListenerFact, error) { value := known; value.unknown = true; return value, err }
	pointer := repairPointerCLI(state)
	list, pathKey, shaKey := "plan_report_refs", "plan_report_ref", "plan_report_sha256"
	if packet.result != nil {
		list, pathKey, shaKey = "result_refs", "result_ref", "result_sha256"
	}
	for _, ref := range repairArtifactRefs(pointer, list, pathKey, shaKey) {
		if err := ctx.Err(); err != nil {
			return budgetUnknown(err)
		}
		document, err := s9ReadListenerDocument(root, ref.Path, ref.SHA256)
		if err != nil {
			return s9ListenerFact{}, err
		}
		receipt := mapFieldCLI(document, "receipt")
		kind := "plan"
		if packet.result != nil {
			receipt, kind = mapFieldCLI(receipt, "provenance"), "result"
		}
		if receipt["packet_id"] == packet.id && receipt["owner_agent_id"] == packet.binding.OwnerAgentID {
			return s9ListenerFact{kind: kind, sourceRoot: stringValue(receipt["execution_root"]), ref: ref.Path}, nil
		}
	}
	if packet.result == nil {
		return s9ListenerFact{}, nil
	}
	_, execution, _ := s9ListenerResultSource(root, state, packet)
	if ref := stringValue(execution["delivery_ref"]); ref != "" {
		if err := ctx.Err(); err != nil {
			return budgetUnknown(err)
		}
		document, err := s9ObservedDocument(root, ref)
		if err != nil {
			return s9ListenerFact{}, err
		}
		provenance := mapFieldCLI(document, "provenance")
		if provenance["packet_id"] == packet.id && provenance["owner_agent_id"] == packet.binding.OwnerAgentID {
			return s9ListenerFact{kind: "candidate", sourceRoot: stringValue(provenance["execution_root"]), ref: ref}, nil
		}
	}
	for _, row := range objectSliceAny(state["evidence"]) {
		if row["kind"] != "repair_result_blocker" || tolerantInt(row["baseline_generation"]) != packet.binding.BaselineGeneration || !slices.Contains(stringSliceAny(row["scope_refs"]), packet.id) {
			continue
		}
		ref := stringValue(row["path"])
		if err := ctx.Err(); err != nil {
			return budgetUnknown(err)
		}
		document, err := s9ReadListenerDocument(root, ref, stringValue(row["sha256"]))
		if err != nil {
			return s9ListenerFact{}, err
		}
		if document["packet_id"] == packet.id && mapFieldCLI(mapFieldCLI(document, "binding"), "sources")["owner_agent_id"] == packet.binding.OwnerAgentID {
			kind := "blocker"
			if mapFieldCLI(document, "submitted_answer")["verdict"] == "contract_conflict" {
				kind = "contract_conflict"
			}
			return s9ListenerFact{kind: kind, sourceRoot: stringValue(document["execution_root"]), ref: ref}, nil
		}
	}
	return s9ListenerFact{}, nil
}

func s9ListenerNeedsPreparation(root string, state map[string]any, packets []s9ListenerQuestionnaire, result bool) bool {
	return s9ListenerPreparationOwner(root, state, packets, result) != ""
}

// Describe only the owner of a real missing question. This projection does not
// prepare it or establish source/authority/readiness qualifications.
func s9ListenerPreparationOwner(root string, state map[string]any, packets []s9ListenerQuestionnaire, result bool) string {
	seen := map[string]bool{}
	for _, packet := range packets {
		seen[packet.binding.RepairAssignmentID] = true
		fact, err := s9ListenerAcceptance(root, state, packet)
		if err != nil || fact.kind != "" {
			continue
		}
		source, _, sourceErr := s9ListenerResultSource(root, state, packet)
		if sourceErr != nil {
			continue // A lost Worker root is not a missing Main-root question.
		}
		if !s8QuestionnaireFileExists(filepath.Join(source, filepath.FromSlash(packet.view))) || !s8QuestionnaireFileExists(filepath.Join(source, filepath.FromSlash(packet.answer))) {
			return packet.binding.OwnerAgentID
		}
	}
	pointer := repairPointerCLI(state)
	for assignment, raw := range mapFieldCLI(pointer, "assignment_owners") {
		owner, _ := raw.(string)
		if owner == "" || seen[assignment] {
			continue
		}
		if !result {
			// assignment_owners also contains independent verifier anchors.
			// Only a missing original Plan Assignment needs a Builder packet.
			plan, err := s9ReadListenerDocument(root, stringValue(pointer["plan_ref"]), stringValue(pointer["plan_sha256"]))
			if err != nil {
				return ""
			}
			for _, row := range objectSliceAny(plan["assignments"]) {
				if row["assignment_id"] == assignment {
					return owner
				}
			}
			continue
		}
		reportPresent := false
		for _, ref := range repairArtifactRefs(pointer, "plan_report_refs", "plan_report_ref", "plan_report_sha256") {
			document, err := s9ReadListenerDocument(root, ref.Path, ref.SHA256)
			if err == nil && document["assignment_id"] == assignment && document["agent_id"] == owner {
				reportPresent = true
			}
		}
		if !reportPresent {
			continue
		}
		// Only an original prepared Worker execution can be missing a Result
		// packet. The original issuer retains dependency/lock eligibility.
		for _, raw := range mapFieldCLI(mapFieldCLI(state, "workspace"), "executions") {
			execution, _ := raw.(map[string]any)
			if (execution["assignment_id"] == assignment || s9ListenerManifestAssignment(state, owner) == execution["assignment_id"]) && execution["agent_id"] == owner && execution["runtime_id"] == state["runtime_id"] && tolerantInt(execution["baseline_generation"]) == tolerantInt(mapFieldCLI(state, "baseline")["generation"]) && execution["status"] == "ready" {
				return owner
			}
		}
	}
	return ""
}

// The native prompt reference and resolution pointer are original Runtime
// facts; they are used here only to describe the current question attempt.
func s9ListenerManifestAssignment(state map[string]any, owner string) string {
	for _, agent := range objectSliceAny(mapFieldCLI(state, "entities")["agents"]) {
		if agent["id"] == owner {
			_, assignment, _ := strings.Cut(stringValue(agent["prompt_ref"]), "#")
			return assignment
		}
	}
	return ""
}

func s9ListenerResolutionRecorded(state map[string]any, packet s9ListenerQuestionnaire) bool {
	for _, row := range objectSliceAny(state["evidence"]) {
		if row["kind"] != "repair_result_resolution" || !slices.Contains(stringSliceAny(row["scope_refs"]), packet.id) {
			continue
		}
		for _, agent := range objectSliceAny(mapFieldCLI(state, "entities")["agents"]) {
			if agent["id"] == packet.binding.OwnerAgentID && agent["blocker_resolved_ref"] == row["path"] {
				return true
			}
		}
	}
	return false
}

// Presence only, for reminder wording. This neither maps a native ID to an
// owner nor decides whether any tool or saved answer is authorized.
func s9ListenerOwnerPresent(state map[string]any, owner string) bool {
	for _, agent := range objectSliceAny(mapFieldCLI(state, "entities")["agents"]) {
		if agent["id"] == owner {
			return true
		}
	}
	return false
}

func s9ListenerQuestionnaireDraft(ctx context.Context, root string, state map[string]any, packet s9ListenerQuestionnaire) (any, error) {
	if packet.draft != nil {
		return packet.draft, nil
	}
	ctx = runtime.WithPacketBodyRoot(ctx, root)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var draft any
	if packet.result != nil {
		original, found, err := runtime.FindRepairResultPacketContext(ctx, state, packet.id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("original S9 Result packet is unavailable")
		}
		draft = original.Draft
	} else {
		original, found, err := runtime.FindRepairPlanReportPacketContext(ctx, state, packet.id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("original S9 Plan packet is unavailable")
		}
		draft = original.Draft
	}
	data, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	var object any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return object, nil
}

// Native route reminders need only already-indexed identity and the existing
// brief path. They never reconstruct a packet body or execution proof.
func s9ListenerNativePlans(root string, state map[string]any) []workpacket.RepairPlanReportPacket {
	questions, err := s9ListenerQuestionnaires(state, "", false)
	if err != nil {
		return nil
	}
	var packets []workpacket.RepairPlanReportPacket
	for _, question := range questions {
		packet := workpacket.RepairPlanReportPacket{ID: question.id, Binding: question.binding, ViewPath: question.view, AnswerPath: question.answer}
		brief, _, _ := nativeS9Paths(packet)
		if s8QuestionnaireFileExists(resolveRootPath(root, brief)) {
			packets = append(packets, packet)
		}
	}
	return packets
}
