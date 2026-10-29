package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func receiveSavedQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	// Script/MCP tools can return the actual saved path. This is the same
	// existing path selector, not a shell parser or a native identity mapping.
	if _, saved := savedQuestionnairePath(request); !saved {
		if path, ok := request.ToolResponse["file_path"].(string); ok {
			located := request
			located.ToolInput = map[string]any{"file_path": path}
			if actual, ok := savedQuestionnairePath(located); ok && (isS9RepairPlanReportPath(actual) || isS9RepairResultPath(actual) || isS9RepairResultResolutionPath(actual) || (strings.Contains(filepath.ToSlash(actual), "/.claude/submissions/s9/") || strings.Contains(filepath.ToSlash(actual), "/.loop-submissions/s9/"))) {
				request = located
			}
		}
	}
	for _, receive := range []func(string, policy.Input, io.Writer, io.Writer) (bool, int){
		runSavedEntryQuestionnaire, runSavedS5ManifestQuestionnaire, runSavedS7BudgetDecisionQuestionnaire, runSavedRepairDispatch, runSavedRepairVerifierDispatch,
		runSavedInvestigationLeadQuestionnaire, runSavedRepairLeadQuestionnaire,
		runSavedReviewPlanQuestionnaire, runSavedAuditQuestionnaire, runSavedHumanReleaseQuestionnaire,
		runSavedBuilderQuestionnaire, runSavedStageAnswerHook,
	} {
		var output bytes.Buffer
		if handled, code := receive(root, request, &output, stderr); handled {
			// A real Main save may reach the existing budget wait through S9
			// final reception or S10 review_required. Prepare its original
			// application here, never on an ordinary Pre/Stop/status read.
			if code == 0 && request.Event == "PostToolUse" && request.Error == "" && mainQuestionnaireViewer(request) {
				failed, _ := request.ToolResponse["is_error"].(bool)
				if snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot(); !failed && err == nil && s7BudgetGateRequired(snapshot.State, root) {
					var message string
					if _, err := prepareS7BudgetDecisionQuestionnaire(root, snapshot.State); err != nil {
						message = "S7 budget preparation failed; Main/system restores the original application: " + err.Error()
					} else {
						message = stageS7BudgetDecisionQuestionnaireContext(root, request, snapshot)
					}
					var envelope map[string]any
					if json.Unmarshal(output.Bytes(), &envelope) == nil {
						if hookOutput, ok := envelope["hookSpecificOutput"].(map[string]any); ok {
							hookOutput["additionalContext"] = strings.TrimSpace(stringValue(hookOutput["additionalContext"]) + "\n" + message)
							output.Reset()
							_ = json.NewEncoder(&output).Encode(envelope)
						}
					}
				}
			}
			_, _ = io.Copy(stdout, &output)
			return true, code
		}
	}
	return false, 0
}

type observedQuestionnaire struct {
	professionalWorker  bool
	path                string
	draft               any
	s6Result            bool
	s7Plan              bool
	s8Packet            *workpacket.HypothesisResultPacket
	s8AcceptedAnswerSHA string
	s8Case              map[string]any
	s9Kind              string
	s9Packet            *s9ListenerQuestionnaire
}

// A successful script or MCP tool can save a questionnaire without exposing a
// file_path. Read the graph's existing exact files, never parse command text or
// invent a tool policy. Unchanged issued drafts do not invoke a domain consumer.
func runObservedQuestionnaireSaves(root string, request policy.Input, stdout, stderr io.Writer, progress ...bool) (bool, int) {
	// A successful Post can finish an already recorded S4 responsibility even
	// when it did not save a file. Keep its real tool identity; this is the
	// original pending followup, never a Read promoted into a new submission.
	if request.Event == "PostToolUse" && request.Error == "" {
		failed, _ := request.ToolResponse["is_error"].(bool)
		if owner := request.EffectiveAgentID(); !failed && (owner == "" || owner == "Main") {
			snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
			if err == nil && mapFieldCLI(snapshot.State, "lifecycle")["phase"] == "tasks" {
				row, rowErr := registeredPlanningRow(snapshot.State)
				if rowErr == nil && row != nil {
					record, found, findErr := runtime.FindSubmission(snapshot.State, stringValue(row["packet_id"]))
					if findErr == nil && found && (record.ControlStatus == "pending" || record.ControlStatus == "no_transition") {
						path := filepath.Join(root, stringValue(row["answer_path"]))
						service, serviceErr := submission.New(root)
						if serviceErr == nil {
							ctx, cancel := context.WithTimeout(contextForHook(request), 12*time.Second)
							defer cancel()
							// Continue the durable original reception, without reading or
							// receiving the author's answer again on an unrelated Post.
							recovered, saveErr := service.Recover(ctx, "Main", 2)
							receipt, receiptErr := service.TaskPlanningReceipt("", record.PacketID)
							for _, candidate := range recovered {
								if candidate.PacketID == record.PacketID {
									receipt = candidate
									receiptErr = nil
									break
								}
							}
							if saveErr == nil {
								saveErr = receiptErr
							}
							if saveErr != nil {
								fmt.Fprintf(stderr, "S4 original followup pending: %v\n", saveErr)
								if receipt.Error == "" {
									receipt.Error = saveErr.Error()
								}
							}
							continueS5PlanningHandoff(contextForHook(request), root, &receipt)
							return true, renderStageAnswerHook(stdout, path, receipt, "S4")
						}
					}
				}
			}
		}
	}
	if len(progress) == 2 && progress[0] && !progress[1] && !planningChangeFrom(request).Active {
		return false, 0
	}
	switch request.ToolName {
	case "Read", "Glob", "Grep":
		return false, 0
	}
	if request.Event != "PostToolUse" || request.Error != "" {
		return false, 0
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return false, 0
	}
	if _, explicit := savedQuestionnairePath(request); explicit && (len(progress) != 2 || !progress[0]) && !planningChangeFrom(request).Active {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	strictPlanning := life["state"] == "planning" && (life["phase"] == "design" || life["phase"] == "contracts")
	change := planningChangeFrom(request)
	if strictPlanning && (!change.Known || len(change.SavedAnswers) == 0) {
		return false, 0
	}
	var contexts []string
	seen := map[string]bool{}
	var leadPaths []string
	if currentAuditKind(snapshot.State) == "" {
		leadPaths = investigationLeadQuestionnairePaths(root, snapshot.State)
		leadPaths = append(leadPaths, repairDelegationApplicationPaths(root, snapshot.State)...)
	}
	questionnaires := observableQuestionnaires(root, snapshot)
	if lifecycle := mapFieldCLI(snapshot.State, "lifecycle"); lifecycle["state"] == "bug_resolution" && lifecycle["phase"] != "investigation" && !s7BudgetGateRequired(snapshot.State, root) {
		questionnaires = s9ObservableQuestionnaires(root, snapshot.State, request)
	}
questionnaires:
	for _, q := range questionnaires {
		if strictPlanning && !change.SavedAnswers[filepath.Clean(q.path)] {
			continue
		}
		// Main tools observe Main forms only. A ready Worker answer is not a
		// save by Main and must wait for its actual native SubagentStop.
		if mainQuestionnaireViewer(request) && q.professionalWorker {
			continue
		}

		if q.path == "" || seen[q.path] {
			continue
		}
		seen[q.path] = true
		info, err := os.Stat(q.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 20<<20 {
			continue
		}
		data, err := os.ReadFile(q.path)
		if err != nil {
			continue
		}
		if q.s9Kind != "" && s9ObservedQuestionnaireUnchanged(root, snapshot.State, q, data) {
			continue
		}
		if q.s9Packet != nil && q.draft == nil {
			draft, draftErr := s9ListenerQuestionnaireDraft(contextForHook(request), root, snapshot.State, *q.s9Packet)
			if draftErr != nil {
				contexts = append(contexts, "S9 observation is incomplete for "+q.path+": its recorded responsibility and paths remain known; comparison with the original draft is unknown within this request: "+draftErr.Error()+". Preserve the professional answer. No answer rejection or missing questionnaire was inferred.")
				continue
			}
			q.draft = draft
			if s9ObservedQuestionnaireUnchanged(root, snapshot.State, q, data) {
				continue
			}
		}
		if strings.HasPrefix(filepath.Base(q.path), "hd-s7-budget-") {
			var app s7BudgetDecisionApplication
			if json.Unmarshal(data, &app) == nil && app.Decision == "" {
				continue
			}
		}
		for _, leadPath := range leadPaths {
			if !samePath(q.path, leadPath) {
				continue
			}
			if !investigationLeadQuestionnaireHasProfessionalContent(q.path, data) ||
				repairDelegationApplicationAlreadyRecorded(root, snapshot.State, q.path, data) ||
				s8ObservedHypothesesAlreadyRegistered(q.path, data, snapshot.State, q.s8Case) ||
				s8ObservedRouteAlreadyRegistered(root, q.path, data, snapshot.State, q.s8Case) ||
				s8ObservedContractAlreadyProjected(root, q.path, data, snapshot.State, q.s8Case) {
				continue questionnaires
			}
		}
		if q.s8Packet != nil && q.s8AcceptedAnswerSHA != "" {
			answer, issues := workpacket.ValidateHypothesisResultAnswer(*q.s8Packet, data)
			if len(issues) == 0 && workpacket.Digest(answer) == q.s8AcceptedAnswerSHA {
				continue // Existing exact accepted content; an actual changed save still reaches the original conflict check.
			}
		}
		var answer any
		if json.Unmarshal(data, &answer) == nil {
			if q.s6Result && !s6ObservedResultHasProfessionalContent(data) {
				continue
			}
			if q.s7Plan && !s7ObservedPlanHasProfessionalContent(data) {
				continue
			}
			if q.s9Kind == "" && q.draft != nil && reflect.DeepEqual(answer, q.draft) {
				continue
			}
			if q.draft == nil && q.s9Kind == "" {
				fields, _ := answer.(map[string]any)
				if _, targeted := fields["performing_assignment_id"]; targeted && stringValue(fields["result"]) == "" {
					continue
				}
				changeTypes, _ := fields["change_types"].([]any)
				decisions, _ := fields["decisions"].([]any)
				if _, impact := fields["changed_artifacts"]; impact && len(changeTypes) == 0 && len(decisions) == 0 {
					continue
				}
			}
		}
		// Normalize an observed file save only for the existing adapters. The
		// actual host event and audit log keep their original tool identity.
		observed := request
		observed.ToolName = "Write"
		observed.ToolInput = map[string]any{"file_path": q.path}
		var out bytes.Buffer
		if handled, _ := receiveSavedQuestionnaire(root, observed, &out, stderr); !handled {
			continue
		}
		var envelope struct {
			Output struct {
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if json.Unmarshal(out.Bytes(), &envelope) == nil && envelope.Output.Context != "" {
			contexts = append(contexts, envelope.Output.Context)
		}
	}
	if len(contexts) == 0 {
		return false, 0
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": strings.Join(contexts, "\n")}})
	return true, 0
}

// DraftPlanForRoot always leaves the QA/delivery oracles for Main to answer.
// Inspect those existing professional fields instead of rebuilding the product
// baseline just to distinguish a machine draft from an observed script save.
// The original consumer still validates incomplete or invalid authored plans.
func s7ObservedPlanHasProfessionalContent(data []byte) bool {
	var plan review.Plan
	if json.Unmarshal(data, &plan) != nil {
		return true
	}
	placeholder := false
	for _, claim := range plan.Claims {
		oracle := strings.TrimSpace(claim.Oracle)
		if claim.Applicability == "required" && (claim.Lens == "qa" || claim.Lens == "delivery") && oracle != "" && !strings.Contains(oracle, "TODO(planner)") {
			return true
		}
		for _, field := range []string{claim.Target, claim.Assertion, claim.Oracle, claim.Method} {
			placeholder = placeholder || strings.Contains(field, "TODO(planner)")
		}
	}
	// A custom complete plan can use other lenses; leave its validity to the
	// original consumer rather than imposing another planning contract here.
	return !placeholder
}

func observableQuestionnaires(root string, snapshot runtime.Snapshot) []observedQuestionnaire {
	state := snapshot.State
	if paths := s7BudgetDecisionQuestionnairePaths(root, state); len(paths) > 0 {
		return []observedQuestionnaire{{path: paths[0]}}
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	stage, _, _ := projectNext(stringValue(lifecycle["state"]), stringValue(lifecycle["phase"]), root)
	if lifecycle["state"] == "inactive" {
		if path, err := bindingQuestionnairePath(root); err == nil {
			out := []observedQuestionnaire{{path: path}}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				for _, req := range scanRequirements(root) {
					if req.Status == "locked" {
						out = append(out, observedQuestionnaire{path: filepath.Join(root, filepath.FromSlash(req.Path))})
					}
				}
			}
			return out
		}
		return nil
	}
	if stage == "S0" || stage == "S1" {
		return nil
	}
	if lifecycle["state"] == "awaiting_human_release" {
		if path, _, err := humanReleaseQuestionnairePath(root, state); err == nil {
			return []observedQuestionnaire{{path: path}}
		}
		return nil
	}
	var out []observedQuestionnaire
	var s8Case map[string]any
	if stage == "S8" {
		s8Case, _ = readS8ListenerCase(root, state)
		paths := append(investigationLeadQuestionnairePaths(root, state), repairDelegationApplicationPaths(root, state)...)
		for _, path := range paths {
			out = append(out, observedQuestionnaire{path: path, s8Case: s8Case})
		}
	}
	if stage == "S7" {
		facts, _ := state["review"].(map[string]any)
		if ptr := review.PlanPointerFromState(state); ptr == nil || ptr.ReviewRound != tolerantInt(facts["round"]) || repairSeedAwaitingPlan(state) {
			out = append(out, observedQuestionnaire{path: reviewPlanQuestionnairePath(root, state), s7Plan: true})
		}
	}
	if stage == "S9" {
		return nil // S9 passive probes need the actual current responsibility.
	}
	if kind := currentAuditKind(state); kind != "" {
		if path, err := auditQuestionnairePath(root, state, kind); err == nil {
			return []observedQuestionnaire{{path: filepath.Join(root, filepath.FromSlash(path))}}
		}
		return nil
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	registry, _ := workspace.Decode(state)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		template := stringValue(row["template_id"])
		if !strings.HasPrefix(template, strings.ToLower(stage)+"-") {
			continue
		}
		path, source := stringValue(row["answer_path"]), root
		binding, _ := row["binding"].(map[string]any)
		if template == "s8-hypothesis-result-v1" {
			pointer := investigationPointer(state)
			baseline, _ := state["baseline"].(map[string]any)
			if pointer == nil || binding["case_id"] != pointer["case_id"] || binding["runtime_id"] != state["runtime_id"] || tolerantInt(binding["baseline_generation"]) != tolerantInt(baseline["generation"]) {
				continue
			}
		}
		if stage == "S2" || stage == "S3" || stage == "S4" {
			current, issued, verified, err := currentIssuedPlanningAnswerPath(state, stringValue(lifecycle["phase"]))
			if err != nil || !issued || !verified || path != current {
				continue
			}
		}
		if round := tolerantInt(binding["review_round"]); round > 0 {
			facts, _ := state["review"].(map[string]any)
			if round != tolerantInt(facts["round"]) {
				continue
			}
		}
		owner, assignment := stringValue(binding["owner_agent_id"]), stringValue(binding["assignment_id"])
		var acceptedPacket *workpacket.HypothesisResultPacket
		var acceptedAnswerSHA string
		switch stringValue(row["template_id"]) {
		case "s7-review-result-v1", "s8-hypothesis-result-v1":
			if template == "s7-review-result-v1" {
				facts, _ := state["review"].(map[string]any)
				assignments, _ := facts["assignments"].(map[string]any)
				assignmentRow, _ := assignments[assignment].(map[string]any)
				if activity := workstate.ReviewAssignmentActivity(assignmentRow); activity == "consumed" || activity == "result_submitted" {
					continue
				}
			}
			var err error
			source, err = questionnaireSourceRoot(root, state, assignment, owner, stringValue(binding["runtime_id"]), tolerantInt(binding["baseline_generation"]))
			if err != nil {
				continue
			}
			if template == "s8-hypothesis-result-v1" && s8Case != nil {
				data, _ := json.Marshal(row)
				var packet workpacket.HypothesisResultPacket
				if json.Unmarshal(data, &packet) == nil {
					if receipt, accepted := s8ListenerReceipt(s8Case, packet); accepted && filepath.Clean(receipt.ExecutionRoot) == filepath.Clean(source) {
						acceptedPacket, acceptedAnswerSHA = &packet, receipt.AnswerSHA256
					}
				}
			}
		case "s5-document-review-v1":
			data, _ := json.Marshal(row)
			var packet workpacket.Packet
			if json.Unmarshal(data, &packet) != nil {
				continue
			}
			if execution, exists, err := workspace.ReviewExecution(state, packet); err != nil {
				continue
			} else if exists {
				if registry == nil || execution.Status != "ready" {
					continue
				}
				paths, err := registry.WorkerSubmissionPaths(execution)
				if err != nil {
					continue
				}
				source, path = execution.Path, paths.Answer
			}
		}
		if path != "" {
			out = append(out, observedQuestionnaire{path: filepath.Join(source, filepath.FromSlash(path)), draft: row["draft"], s8Packet: acceptedPacket, s8AcceptedAnswerSHA: acceptedAnswerSHA, professionalWorker: template == "s5-document-review-v1" || template == "s7-review-result-v1" || template == "s8-hypothesis-result-v1"})
		}
	}
	if stage == "S6" {
		questions, _ := projectS6BuilderQuestionnaires(root, "", snapshot)
		for _, q := range questions {
			activity := workstate.AgentActivity(contextAgent(state, q.AgentID))
			if activity == "reported" || activity == "stopped" {
				continue
			}
			data, err := os.ReadFile(q.ContextPath)
			if err != nil {
				continue
			}
			var context map[string]any
			if json.Unmarshal(data, &context) != nil {
				continue
			}
			if activity != "working" {
				out = append(out, observedQuestionnaire{path: q.PlanPath, draft: context["plan_draft"], professionalWorker: true})
			}
			out = append(out, observedQuestionnaire{path: q.ResultPath, draft: context["result_draft"], s6Result: q.CanonicalRecord, professionalWorker: true})
		}
	}
	return out
}

// PLAN acceptance can change only machine-owned fields in a canonical S6
// Result. A generic successful-tool observer must not submit that projection
// as if the Builder had authored an answer. Explicit path saves still use the
// same original Result consumer and its normal validation.
func s6ObservedResultHasProfessionalContent(data []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return true // Keep malformed changed files on the original validation path.
	}
	if s6NonNull(fields["summary"]) || s6NonNull(fields["risks_blockers"]) || s6NonNull(fields["scope_deviations"]) {
		return true
	}
	if raw := fields["assertion_evidence"]; len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		var rows []struct {
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return true
		}
		for _, row := range rows {
			if len(row.EvidenceRefs) > 0 {
				return true
			}
		}
	}
	if raw := fields["closing_assessment"]; len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		var assessment struct {
			Disposition json.RawMessage `json:"disposition"`
			Conditions  []struct {
				Judgment  json.RawMessage `json:"judgment"`
				Rationale json.RawMessage `json:"rationale"`
			} `json:"conditions"`
		}
		if err := json.Unmarshal(raw, &assessment); err != nil {
			return true
		}
		if s6NonNull(assessment.Disposition) {
			return true
		}
		for _, condition := range assessment.Conditions {
			if s6NonNull(condition.Judgment) || s6NonNull(condition.Rationale) {
				return true
			}
		}
	}
	return false
}

func s6NonNull(raw json.RawMessage) bool {
	return len(raw) > 0 && string(bytes.TrimSpace(raw)) != "null"
}

// Unchanged Main questions are already represented by the original Case and
// Runtime dispatch. This is an observer projection, never an acceptance check;
// explicit saves and any changed or incomplete registration keep the original
// consumer's complete validation and conflict feedback.
func s8ObservedHypothesesAlreadyRegistered(path string, data []byte, state, document map[string]any) bool {
	if filepath.Base(path) != "hypotheses.json" || document == nil || workpacket.ValidateUniqueJSON(data) != nil {
		return false
	}
	var questions []investigationLeadQuestion
	if investigationLeadDecode(data, &questions) != nil || len(questions) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, q := range questions {
		if q.HypothesisID == "" || seen[q.HypothesisID] {
			return false
		}
		seen[q.HypothesisID] = true
		var registered map[string]any
		for _, h := range objectSliceAny(document["hypotheses"]) {
			if h["hypothesis_id"] == q.HypothesisID {
				if registered != nil {
					return false
				}
				registered = h
			}
		}
		if registered == nil || !investigationLeadSameQuestion(q, investigationLeadQuestionFromCase(registered)) {
			return false
		}
		assignment := stringValue(registered["assignment_id"])
		owner := investigationLeadRegisteredOwner(state, assignment)
		if assignment == "" || owner == "" || q.AgentID != "" && q.AgentID != owner || !s8ObservedDispatchRecorded(state, assignment, owner) {
			return false
		}
	}
	return true
}

func s8ObservedDispatchRecorded(state map[string]any, assignment, owner string) bool {
	entities := mapFieldCLI(state, "entities")
	var agent map[string]any
	for _, row := range objectSliceAny(entities["agents"]) {
		if row["id"] == owner {
			if agent != nil {
				return false
			}
			agent = row
		}
	}
	if agent == nil || agent["role"] != "investigator" {
		return false
	}
	manifest, fragment, found := strings.Cut(stringValue(agent["prompt_ref"]), "#")
	if !found || manifest == "" || fragment != assignment {
		return false
	}
	teamFound := false
	for _, team := range objectSliceAny(entities["teams"]) {
		if team["id"] == agent["team_id"] && team["kind"] == "investigator" && team["manifest_ref"] == manifest && len(stringValue(team["manifest_sha256"])) == 64 {
			if teamFound {
				return false
			}
			teamFound = true
		}
	}
	if !teamFound {
		return false
	}
	for _, task := range objectSliceAny(entities["tasks"]) {
		if slices.Contains(stringSliceAny(agent["task_ids"]), stringValue(task["id"])) && slices.Contains(stringSliceAny(task["owner_agent_ids"]), owner) && stringValue(task["path"]) != "" && len(stringValue(task["sha256"])) == 64 {
			return true
		}
	}
	return false
}

// A persisted route is already represented by its original Case revision.
// Routes with a separate downstream consumer remain observable until that
// existing consumer records its acknowledgement; an unchanged save can then
// recover an interrupted handoff instead of being silently dropped.
func s8ObservedRouteAlreadyRegistered(root, path string, data []byte, state, document map[string]any) bool {
	if document == nil || !strings.HasPrefix(filepath.Base(path), "route-") || !investigationLeadAtRouteRevision(document) || workpacket.ValidateUniqueJSON(data) != nil {
		return false
	}
	expected, err := investigationLeadRoutePath(root, state, document)
	if err != nil || !samePath(path, expected) {
		return false
	}
	var answer investigationLeadRouteAnswer
	if investigationLeadDecode(data, &answer) != nil || !investigationLeadRouteMatches(answer, document) {
		return false
	}
	pointer := investigationPointer(state)
	route := stringValue(document["route"])
	if pointer == nil || pointer["case_id"] != document["case_id"] || stringValue(pointer["route"]) != route {
		return false
	}
	switch route {
	case "s9_repair", "human_req_change", "investigate_more":
		return true
	case "s2_spec_rework", "duplicate", "s7_no_change":
		return stringValue(pointer["route_consumed_at"]) != "" && pointer["route_consumer"] == "S8-ROUTE-CONSUME"
	}
	return false
}

// Equal current projection bytes already exist in the original final draft.
// This skips passive resubmission only; it records no acceptance or approval.
func s8ObservedContractAlreadyProjected(root, path string, data []byte, state, document map[string]any) bool {
	if document == nil || document["route"] != "s9_repair" || !strings.HasPrefix(filepath.Base(path), "contract-") {
		return false
	}
	pointer := investigationPointer(state)
	if pointer == nil || pointer["case_id"] != document["case_id"] || tolerantInt(pointer["revision"]) != tolerantInt(document["revision"]) || stringValue(pointer["sha256"]) == "" {
		return false
	}
	routePath, err := investigationLeadRoutePath(root, state, document)
	if err != nil || !samePath(path, filepath.Join(filepath.Dir(routePath), "contract-"+stringValue(pointer["sha256"])+".json")) {
		return false
	}
	finalPath, finalBytes, err := investigationLeadContractProjectionBytes(root, data, document)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, finalPath)
	if err != nil {
		return false
	}
	actual, err := workpacket.ReadFile(root, filepath.ToSlash(relative), 20<<20)
	return err == nil && bytes.Equal(actual, finalBytes)
}
