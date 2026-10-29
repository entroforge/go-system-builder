package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This is the original budget decision's authoring projection. Known bindings
// are prepared once; the human only chooses decision/new max/reason. It is not
// a second approval or a new budget registry.
type s7BudgetDecisionApplication struct {
	Decision                    string `json:"decision"`
	RuntimeID                   string `json:"runtime_id"`
	ReviewRound                 int    `json:"review_round"`
	PreviousMaxFullReviewRounds int    `json:"previous_max_full_review_rounds"`
	NewMaxFullReviewRounds      *int   `json:"new_max_full_review_rounds"`
	Reason                      string `json:"reason"`
	AuthorizedBy                string `json:"authorized_by"`
	DecisionID                  string `json:"decision_id"`
}

func s7BudgetDecisionApplicationForState(root string, state map[string]any) (string, s7BudgetDecisionApplication, error) {
	app := s7BudgetDecisionApplication{
		RuntimeID: stringValue(state["runtime_id"]), ReviewRound: integerValue(nestedStateValue(state, "review", "round")),
		PreviousMaxFullReviewRounds: s7MaxRounds(state), AuthorizedBy: "user",
	}
	generation := integerValue(nestedStateValue(state, "baseline", "generation"))
	if app.RuntimeID == "" || generation < 1 || app.ReviewRound < 1 || app.PreviousMaxFullReviewRounds < 1 {
		return "", app, fmt.Errorf("the original Runtime, generation, round or budget limit is missing; Main/system restores preparation")
	}
	identity, _ := json.Marshal([]any{app.RuntimeID, generation, app.ReviewRound, app.PreviousMaxFullReviewRounds})
	app.DecisionID = "hd-s7-budget-" + workpacket.Digest(identity)[:32]
	return filepath.Join(root, ".claude", "decisions", app.DecisionID+".application.json"), app, nil
}

// Called only at the actual budget-wait entry. Status/Pre/Stop projections
// below are read-only and never manufacture an application or human choice.
func prepareS7BudgetDecisionQuestionnaire(root string, state map[string]any) (string, error) {
	if !s7BudgetGateRequired(state, root) {
		return "", nil
	}
	path, app, err := s7BudgetDecisionApplicationForState(root, state)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(app, "", "  ")
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	_, err = workpacket.PublishBounded(root, filepath.ToSlash(rel), append(data, '\n'), 1<<20)
	return path, err
}

func s7BudgetDecisionQuestionnairePaths(root string, state map[string]any) []string {
	if !s7BudgetGateRequired(state, root) {
		return nil
	}
	path, _, err := s7BudgetDecisionApplicationForState(root, state)
	if err != nil {
		return nil
	}
	return []string{path}
}

func stageS7BudgetDecisionQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) {
		return ""
	}
	if lifecycleState(snapshot.State) == "acceptance" && s7MaxRounds(snapshot.State) > 0 && integerValue(nestedStateValue(snapshot.State, "review", "round")) >= s7MaxRounds(snapshot.State) {
		needed, err := s7AcceptedReviewRequiresNewRound(root, snapshot.State)
		if err != nil {
			return "S7 budget decision is not inferred from unavailable source: " + err.Error() + ". No budget increase or human choice was inferred."
		}
		if !needed {
			return ""
		}
	} else if !s7BudgetGateRequired(snapshot.State, root) {
		return ""
	}
	path, app, err := s7BudgetDecisionApplicationForState(root, snapshot.State)
	if err != nil {
		return "S7 budget application preparation gap: " + err.Error()
	}
	message := fmt.Sprintf("S7 actual full-review budget is exhausted: round %d, current limit %d. Current human decision application: %s. Only after the human actually chooses, Main fills decision (increase_budget or return_to_governance), reason, and new_max_full_review_rounds only for an actual increase above %d. Preserve the supplied machine fields. Save this same application; the original consumer records the human decision and follows its original successor. Empty choice does not authorize anything. Do not compute/copy Runtime metadata, register evidence, supply an actor command, or automatically increase the budget.", app.ReviewRound, app.PreviousMaxFullReviewRounds, path, app.PreviousMaxFullReviewRounds)
	if _, err := os.Stat(path); err != nil {
		message += fmt.Sprintf(" The actual stage-entry application is missing or unreadable (%v); Main/system restores preparation at this exact path. Ordinary reading does not create it.", err)
	}
	return message
}

func runSavedS7BudgetDecisionQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	if !mainQuestionnaireViewer(request) || request.Event != "PostToolUse" || request.Error != "" {
		return false, 0
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return false, 0
	}
	actual, saved := savedQuestionnairePath(request)
	if !saved {
		if path, ok := request.ToolResponse["file_path"].(string); ok {
			located := request
			located.ToolInput = map[string]any{"file_path": path}
			actual, saved = savedQuestionnairePath(located)
		}
	}
	if !saved || !strings.HasPrefix(filepath.Base(actual), "hd-s7-budget-") || !strings.HasSuffix(actual, ".application.json") {
		return false, 0
	}
	feedback := func(message string) (bool, int) {
		return true, auditQuestionnaireFeedback(stdout, "S7 budget decision at "+actual+": "+message)
	}
	rel, err := filepath.Rel(root, actual)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(rel), ".claude/decisions/hd-s7-budget-") || strings.Contains(filepath.ToSlash(rel), "../") {
		return feedback("The saved location is not the original decision application; Main/system restores its exact source.")
	}
	data, err := workpacket.ReadFile(root, filepath.ToSlash(rel), 1<<20)
	if err != nil {
		return feedback("The actual application cannot be read: " + err.Error())
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return feedback("The application JSON is invalid: " + err.Error())
	}
	var app s7BudgetDecisionApplication
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&app); err != nil {
		return feedback("The supplied application shape is invalid; only decision, new maximum and reason are professional edits: " + err.Error())
	}
	if app.Decision == "" {
		return feedback("Draft only: no actual human decision. No Runtime decision or budget change was recorded.")
	}
	if app.Decision != runtime.S7BudgetDecisionIncrease && app.Decision != runtime.S7BudgetDecisionGovernance {
		return feedback("The actual human choice must be increase_budget or return_to_governance; no decision was recorded.")
	}
	if strings.TrimSpace(app.Reason) == "" {
		return feedback("The actual human reason is missing; no decision was recorded.")
	}
	if app.AuthorizedBy != "user" || app.DecisionID == "" || filepath.Base(actual) != app.DecisionID+".application.json" {
		return feedback("The supplied original machine binding was changed or lost; Main/system restores the prepared application, without asking the author to rebuild metadata.")
	}
	newMax := 0
	if app.NewMaxFullReviewRounds != nil {
		newMax = *app.NewMaxFullReviewRounds
	}
	if app.Decision == runtime.S7BudgetDecisionIncrease && newMax <= app.PreviousMaxFullReviewRounds {
		return feedback("An actual increase must provide a new maximum strictly above the displayed original limit; the system does not choose or increment it.")
	}
	if app.Decision == runtime.S7BudgetDecisionGovernance && newMax != 0 {
		return feedback("Return to governance does not change the maximum; leave new_max_full_review_rounds null.")
	}
	decision := runtime.S7BudgetDecision{Decision: app.Decision, RuntimeID: app.RuntimeID, ReviewRound: app.ReviewRound, PreviousMaxFullReviewRounds: app.PreviousMaxFullReviewRounds, NewMaxFullReviewRounds: newMax, Reason: app.Reason, AuthorizedBy: "user", DecisionID: app.DecisionID}
	canonical, _ := json.MarshalIndent(decision, "", "  ")
	canonical = append(canonical, '\n')
	canonicalRel := filepath.ToSlash(filepath.Join(".claude", "decisions", app.DecisionID+".json"))
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return feedback("The original Runtime is unavailable; preserve the human answer: " + err.Error())
	}
	if entry := evidenceEntryByID(snapshot.State, app.DecisionID); entry != nil {
		scope := "runtime_budget:"
		if app.Decision == runtime.S7BudgetDecisionGovernance {
			scope = "runtime_governance:"
		}
		stored, readErr := workpacket.ReadFile(root, canonicalRel, 1<<20)
		if readErr != nil || entry["kind"] != "human_decision" || entry["status"] != "valid" || entry["path"] != canonicalRel || entry["sha256"] != workpacket.Digest(canonical) || !bytes.Equal(stored, canonical) || !evidenceIncludesScope(entry, scope+app.RuntimeID) {
			return feedback("The original accepted decision differs or its source is unavailable; preserve the original evidence and restore its exact content. A changed answer cannot re-sign an accepted budget decision.")
		}
		if app.Decision == runtime.S7BudgetDecisionGovernance && !runtime.HumanDecisionEvidenceConsumed(entry) {
			_, err := transition.Apply(root, statePath, journalPath, transition.Request{TransitionID: "GTR-006", ExpectedRevision: snapshot.Revision, Actor: "user", Evidence: map[string]string{"human_decision_record": app.DecisionID}})
			if err != nil {
				return feedback("The decision is already recorded; Main/system restores its original GTR-006 handoff without asking for another decision: " + err.Error())
			}
			return feedback("The original recorded human decision completed GTR-006 to planning; no second approval was recorded.")
		}
		if !runtime.HumanDecisionEvidenceConsumed(entry) {
			return feedback("The original budget evidence has no completed consumption; Main/system restores its original receipt before any retry.")
		}
		if app.Decision == runtime.S7BudgetDecisionIncrease && app.RuntimeID == stringValue(snapshot.State["runtime_id"]) && app.ReviewRound == integerValue(nestedStateValue(snapshot.State, "review", "round")) && newMax == s7MaxRounds(snapshot.State) && lifecycleState(snapshot.State) == "bug_resolution" && stringValue(repairPointerCLI(snapshot.State)["status"]) == "ready_for_full_review" {
			message, _ := completeRepairExit(root)
			return feedback("The same original human decision is already recorded and consumed. Reusing the accepted decision grants no new budget; resuming its original pending TR-012 successor. " + message)
		}
		return feedback("The same original human decision is already recorded and consumed. Read-only replay did not change the current budget, record another approval or repeat a completed exit.")
	}
	expectedPath, expected, err := s7BudgetDecisionApplicationForState(root, snapshot.State)
	if err != nil || !s7BudgetGateRequired(snapshot.State, root) || !samePath(actual, expectedPath) || app.RuntimeID != expected.RuntimeID || app.ReviewRound != expected.ReviewRound || app.PreviousMaxFullReviewRounds != expected.PreviousMaxFullReviewRounds || app.DecisionID != expected.DecisionID {
		return feedback("The original round/limit decision object is stale or is not actually awaiting a new review budget. Main/system restores the current application; an old answer cannot authorize a new scope or current budget.")
	}
	published, err := workpacket.PublishBounded(root, canonicalRel, canonical, 1<<20)
	if err != nil || !bytes.Equal(published, canonical) {
		return feedback(fmt.Sprintf("The original internal decision source could not be fixed (%v). Preserve the human answer and restore the original source; do not author a new envelope.", err))
	}
	// Reuse the original command's domain reception and exact successor. All
	// arguments are system supplied, never another task for the human/agent.
	var result, diagnostic bytes.Buffer
	code := runRuntimeS7BudgetDecision([]string{"--root", root, "--file", canonicalRel, "--actor", "user", "--expected-revision", strconv.Itoa(snapshot.Revision)}, &result, &diagnostic)
	if code != 0 {
		message := strings.SplitN(diagnostic.String(), "; retry `", 2)[0]
		return feedback("Original budget reception/successor was not confirmed: " + strings.TrimSpace(message) + ". Main/system preserves this exact application and resumes the original consumer; no metadata copying or new decision is required.")
	}
	if app.Decision == runtime.S7BudgetDecisionGovernance {
		return feedback("The original Writer recorded the actual human choice and followed GTR-006 to planning. No new review budget was granted.")
	}
	// Normal Pre/status events remain read-only. Resume the original pending
	// round-opening consumer at this actual successful decision-save outlet,
	// rather than promising that another observation will eventually do it.
	var recorded struct {
		Pending string `json:"pending_transition"`
	}
	_ = json.Unmarshal(result.Bytes(), &recorded)
	if recorded.Pending == "TR-012" {
		message, _ := completeRepairExit(root)
		return feedback(fmt.Sprintf("The original Writer recorded the actual human increase to %d; original pending TR-012 completion: %s", newMax, message))
	}
	control := runControlCycleForHook(root, request)
	if control.QualityGate.TransitionCommitted {
		message := fmt.Sprintf("The original Writer recorded the actual human increase to %d, then the original consumer committed %s; current cursor %s.", newMax, control.QualityGate.CandidateTransition, control.QualityGate.NextCursor)
		if lifecycleState(control.Snapshot.State) == "verification" {
			message += " " + stageReviewPlanQuestionnaireContext(root, request, control.Snapshot)
		}
		return feedback(message)
	}
	return feedback(fmt.Sprintf("The original Writer recorded the actual human increase to %d. Original successor %s has not committed; Main/system resolves its actual missing=%v, conflicts=%v, error=%s. Preserve the accepted decision; do not ask for another approval, copy metadata or wait for status reads to retry.", newMax, recorded.Pending, control.QualityGate.Missing, control.QualityGate.Conflicts, control.Error))
}
