package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Professional fields from the original RouteRequest, kept in one answer.
// No route or causal conclusion is selected by the listener.
type investigationLeadRouteAnswer struct {
	Route                         string         `json:"route"`
	RouteReason                   string         `json:"route_reason"`
	PrimaryRootCause              string         `json:"primary_root_cause"`
	CausalModel                   map[string]any `json:"causal_model"`
	BlastRadius                   map[string]any `json:"blast_radius"`
	DetectionGap                  map[string]any `json:"detection_gap"`
	CompetingHypothesisAssessment map[string]any `json:"competing_hypothesis_assessment,omitempty"`
	NoCompetingHypothesis         string         `json:"no_competing_hypothesis"`
	CanonicalCaseID               string         `json:"canonical_case_id,omitempty"`
}

func investigationLeadResultsReceived(document map[string]any) bool {
	// Results are inputs to Main's judgment, not a requirement that every
	// historical or suggested question independently finish before routing.
	return len(objectSliceAny(document["hypothesis_results"])) > 0
}

func investigationLeadHasResult(document, hypothesis map[string]any) bool {
	matched := 0
	for _, result := range objectSliceAny(document["hypothesis_results"]) {
		if result["hypothesis_id"] == hypothesis["hypothesis_id"] && result["assignment_id"] == hypothesis["assignment_id"] {
			matched++ // supported/refuted/inconclusive are all received work.
		}
	}
	return matched == 1
}

// A route answer names its original immutable Case source, using the existing
// revision history after a successful save. This enables exact save replay
// without a second receipt and avoids reusing a decision after new evidence.
func investigationLeadRoutePath(root string, state, document map[string]any) (string, error) {
	ref, _ := investigationLeadBatchReference(state)
	pointer := investigationPointer(state)
	if ref == "" || pointer == nil {
		return "", fmt.Errorf("current Case source is required")
	}
	revision := integerValue(pointer["revision"])
	history := objectSliceAny(document["revision_history"])
	if len(history) > 0 && stringValue(document["route"]) != "" {
		for index := len(history) - 1; index >= 0; index-- {
			if stringValue(history[index]["operation"]) != "case_routed" {
				break
			}
			revision = integerValue(history[index]["revision"])
		}
	}
	if revision < 1 {
		return "", fmt.Errorf("current Case revision is unavailable")
	}
	return filepath.Abs(filepath.Join(s8OriginalSourceDir(root, stringValue(state["runtime_id"]), integerValue(mapFieldCLI(state, "baseline")["generation"]), ref), fmt.Sprintf("route-r%d.json", revision)))
}

func investigationLeadDispositionPaths(root string, state map[string]any) []string {
	life := mapFieldCLI(state, "lifecycle")
	if stringValue(life["state"]) != "bug_resolution" || stringValue(life["phase"]) != "investigation" || investigationPointer(state) == nil {
		return nil
	}
	_, document, _, err := investigationLeadSource(root, state)
	if err != nil {
		return nil
	}
	path, err := investigationLeadRoutePath(root, state, document)
	if err != nil {
		return nil
	}
	return []string{path}
}

func investigationLeadAtRouteRevision(document map[string]any) bool {
	history := objectSliceAny(document["revision_history"])
	return len(history) > 0 && stringValue(history[len(history)-1]["operation"]) == "case_routed"
}

func stageInvestigationLeadDispositionContext(root string, request policy.Input, snapshot runtime.Snapshot) (context string) {

	if !mainQuestionnaireViewer(request) {
		return ""
	}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	if stringValue(life["state"]) != "bug_resolution" || stringValue(life["phase"]) != "investigation" || investigationPointer(snapshot.State) == nil {
		return ""
	}
	_, document, _, err := investigationLeadSource(root, snapshot.State)
	if err != nil {
		return "" // C1 reports the source failure with its exact current path.
	}
	route := stringValue(document["route"])
	path, err := investigationLeadRoutePath(root, snapshot.State, document)
	if err != nil {
		return "S8 Main route questionnaire could not resolve its original Case source: " + err.Error()
	}
	if route != "" && route != "investigate_more" {
		return investigationLeadRouteNextContext(root, path, snapshot.State, route)
	}
	// Every original disposition remains available before results are complete.
	// Receiving all results changes the reminder, never route qualification.
	if repairLeadPreparationEvent(request.Event) {
		draft := investigationLeadRouteAnswerFromCase(document)
		draft.Route, draft.RouteReason = "", ""
		if err := writeRepairLeadDraft(path, draft); err != nil {
			return fmt.Sprintf("S8 Main route questionnaire could not be prepared at %s: %v", path, err)
		}
	}
	preparationNote := ""
	if !repairLeadPreparationEvent(request.Event) && questionnaireMissing(path) {
		preparationNote = " Route draft is not prepared at " + path + "; this event does not prepare files. The next Main SessionStart or PreToolUse prepares it."
	}
	if route == "investigate_more" && investigationLeadAtRouteRevision(document) {
		return "" // Reuse C1 for further professional questions.
	}
	if !investigationLeadResultsReceived(document) {
		var waiting []string
		for _, h := range objectSliceAny(document["hypotheses"]) {
			if investigationLeadHasResult(document, h) {
				continue
			}
			owner := investigationLeadRegisteredOwner(snapshot.State, stringValue(h["assignment_id"]))
			if owner == "" {
				return "" // C1 still owns a missing real dispatch.
			}
			waiting = append(waiting, stringValue(h["hypothesis_id"])+" from "+owner)
		}
		if len(waiting) > 0 {
			paths := investigationLeadQuestionnairePathsForQuestions(root, snapshot.State)
			if len(paths) != 0 {
				return "S8 Main questionnaire: " + paths[0] + ". Main's registered questions are awaiting Investigator results: " + strings.Join(waiting, "; ") + ". Each Investigator owns its exact HypothesisResult answer delivered by the original listener. Registration does not establish platform launch; Main follows the actual platform execution. The current Main questions need no repeated save while waiting. Current professional route entry: " + path + ". Main may choose an earlier specification/REQ/duplicate/investigate_more disposition here; the original route consumer decides its actual qualification, including s9_repair causal closure." + preparationNote
			}
		}
		return ""
	}
	note := investigationLeadContinueQuestionsContext(root, snapshot.State)
	if !repairLeadPreparationEvent(request.Event) && questionnaireMissing(path) {
		note += "Not prepared yet; the next Main SessionStart or PreToolUse creates the draft. This event is read-only. "
	}
	routeHint := "investigate_more records an unresolved disposition; it is optional before adding questions. "
	return "S8 Main route questionnaire: " + path + ". " + note + "Current Case: " + resolveRootPath(root, stringValue(investigationPointer(snapshot.State)["path"])) + ". Main chooses route and route_reason from the accepted observations and actual remaining uncertainty. For s9_repair, fill primary_root_cause and causal_model as your professional judgment; blast_radius and detection_gap describe the actual impact and missed detection. Explain important unknowns and credible alternatives honestly. Suggested new_hypotheses remain suggestions until Main registers and actually dispatches a question. No count of results, per-Finding explanation IDs or refuted result proves causal closure. Use actual source and file references without evidence recapture. " + routeHint + "Choose s2_spec_rework for specification errors, human_req_change for an actual REQ change, s7_no_change when no artifact change is needed, or duplicate with canonical_case_id. Save this original answer once. Saving does not approve a Contract or authorize a REQ change."

}

func runSavedInvestigationLeadDisposition(root string, request policy.Input, stdout io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved || !strings.HasPrefix(filepath.Base(path), "route-") {
		return false, 0
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return false, 0
	}
	if investigationPointer(current.State) == nil {
		base, _ := filepath.Abs(filepath.Join(root, ".claude", "submissions", "s8"))
		if strings.HasPrefix(path, base+string(filepath.Separator)) {
			return true, repairLeadFeedback(stdout, path, "This is no longer the current S8 responsibility; no result was resubmitted or route consumed. Current Runtime no longer retains this Case pointer, so this response does not assert a historical acceptance.")
		}
		return false, 0
	}
	if stringValue(mapFieldCLI(current.State, "lifecycle")["state"]) != "bug_resolution" && stringValue(investigationPointer(current.State)["route_consumed_at"]) == "" {
		return false, 0
	}
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil {
		return false, 0
	}
	expected, err := investigationLeadRoutePath(root, current.State, document)
	if err != nil {
		return false, 0
	}
	feedback := func(message string) (bool, int) { return true, repairLeadFeedback(stdout, path, message) }
	if !samePath(path, expected) {
		if samePath(filepath.Dir(path), filepath.Dir(expected)) {
			return feedback("S8 route answer belongs to an earlier Case evidence revision. Current professional route entry: " + expected + ". Read the current Case before restating the disposition; the old answer was not applied to new evidence.")
		}
		return false, 0
	}
	rootAbs, _ := filepath.Abs(root)
	relative, err := filepath.Rel(rootAbs, path)
	if err != nil {
		return feedback(err.Error())
	}
	data, err := workpacket.ReadFile(root, filepath.ToSlash(relative), 20<<20)
	if err != nil {
		return feedback("S8 route answer could not be read: " + err.Error())
	}
	var answer investigationLeadRouteAnswer
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return feedback("S8 route answer: " + err.Error())
	}
	if err := investigationLeadDecode(data, &answer); err != nil {
		return feedback("S8 route answer: " + err.Error())
	}
	if strings.TrimSpace(answer.Route) == "" || strings.TrimSpace(answer.RouteReason) == "" {
		return feedback("S8 route and route_reason are incomplete. Main must choose the professional disposition; no route was inferred.")
	}
	route := stringValue(document["route"])
	if route != "" && investigationLeadAtRouteRevision(document) && (route != "investigate_more" || investigationLeadRouteMatches(answer, document)) {
		if !investigationLeadRouteMatches(answer, document) {
			return feedback("S8 route answer conflicts with the persisted Case disposition; no accepted professional conclusion was overwritten.")
		}
	} else {
		current, err = investigation.UpdateCaseRoute(root, statePath, journalPath, investigation.RouteRequest{ExpectedRevision: current.Revision, ExpectedCaseRevision: integerValue(document["revision"]), CaseID: stringValue(document["case_id"]), Route: strings.TrimSpace(answer.Route), RouteReason: answer.RouteReason, PrimaryRootCause: answer.PrimaryRootCause, CausalModel: answer.CausalModel, BlastRadius: answer.BlastRadius, DetectionGap: answer.DetectionGap, NoCompetingHypothesis: answer.NoCompetingHypothesis, CompetingHypothesisAssessment: answer.CompetingHypothesisAssessment, CanonicalCaseID: answer.CanonicalCaseID})
		if err != nil {
			return feedback("S8 original route validation: " + err.Error())
		}
		route = stringValue(investigationPointer(current.State)["route"])
	}
	if route == "s7_no_change" || route == "s2_spec_rework" || route == "duplicate" {
		if stringValue(investigationPointer(current.State)["route_consumed_at"]) == "" {
			current, err = investigation.ConsumeCaseRoute(root, statePath, journalPath, investigation.ConsumeRouteRequest{ExpectedRevision: current.Revision, CaseID: stringValue(document["case_id"]), Actor: "Main"})
			if err != nil {
				return feedback("S8 professional route is persisted; original downstream consumption remains outstanding: " + err.Error())
			}
		}
	}
	return feedback(investigationLeadRouteNextContext(root, path, current.State, route))
}

func investigationLeadRouteMatches(answer investigationLeadRouteAnswer, document map[string]any) bool {
	answer.Route, answer.RouteReason = strings.TrimSpace(answer.Route), strings.TrimSpace(answer.RouteReason)
	answer.PrimaryRootCause = strings.TrimSpace(answer.PrimaryRootCause)
	answer.NoCompetingHypothesis = strings.TrimSpace(answer.NoCompetingHypothesis)
	answer.CanonicalCaseID = strings.TrimSpace(answer.CanonicalCaseID)
	return reflect.DeepEqual(answer, investigationLeadRouteAnswerFromCase(document))
}

func investigationLeadRouteAnswerFromCase(document map[string]any) investigationLeadRouteAnswer {
	causal, _ := document["causal_model"].(map[string]any)
	blast, _ := document["blast_radius"].(map[string]any)
	gap, _ := document["detection_gap"].(map[string]any)
	return investigationLeadRouteAnswer{Route: stringValue(document["route"]), RouteReason: stringValue(document["route_reason"]), PrimaryRootCause: stringValue(document["primary_root_cause"]), CausalModel: causal, BlastRadius: blast, DetectionGap: gap, NoCompetingHypothesis: stringValue(document["no_competing_hypothesis"]), CompetingHypothesisAssessment: mapFieldCLI(document, "competing_hypothesis_assessment"), CanonicalCaseID: stringValue(document["canonical_case_id"])}
}

func investigationLeadRouteNextContext(root, path string, state map[string]any, route string) string {
	switch route {
	case "s9_repair":
		return "S8 Main questionnaire: " + path + ". The original Case has the professional s9_repair disposition. Contract authoring and actual approval remain outstanding; this route is not approval and S9 has not been authorized."
	case "human_req_change":
		return "S8 Main questionnaire: " + path + ". The original human_req_change route is persisted. Wait for the actual human pause/REQ amendment decision through the existing human boundary; no approval was inferred from this answer."
	case "investigate_more":
		paths := investigationLeadQuestionnairePathsForQuestions(root, state)
		if len(paths) > 0 {
			path = paths[0]
		}
		return "S8 Main questionnaire: " + path + ". The original investigate_more route remains in S8. Main may continue questions in this hypotheses answer or revise the original route answer when professional judgment supports a disposition; the original route was not consumed as an exit."
	}
	life := mapFieldCLI(state, "lifecycle")
	return fmt.Sprintf("S8 Main questionnaire: %s. Original %s route consumption is recorded; continue at %s.%s. Repeated save does not create a second handoff.", path, route, stringValue(life["state"]), stringValue(life["phase"]))
}

func investigationLeadRouteHasProfessionalContent(data []byte) bool {
	var answer investigationLeadRouteAnswer
	if investigationLeadDecode(data, &answer) != nil {
		return true
	}
	return strings.TrimSpace(answer.Route) != "" || strings.TrimSpace(answer.RouteReason) != "" || strings.TrimSpace(answer.PrimaryRootCause) != "" || len(answer.CompetingHypothesisAssessment) != 0 || len(answer.CausalModel) != 0 || len(answer.BlastRadius) != 0 || len(answer.DetectionGap) != 0 || strings.TrimSpace(answer.NoCompetingHypothesis) != "" || strings.TrimSpace(answer.CanonicalCaseID) != ""
}
