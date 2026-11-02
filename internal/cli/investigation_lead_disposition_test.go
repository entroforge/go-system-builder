package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestS8InvestigationLeadDispositionWaitsForActualResultsWithoutRepeatedQuestionWork(t *testing.T) {
	root := investigationLeadTestRoot(t)
	_, questions := investigationLeadTestIngest(t, root)
	writeRepairLeadJSON(t, questions, []investigationLeadQuestion{investigationLeadTestQuestion("the boundary drops authority")})
	investigationLeadTestSave(t, root, questions)
	current := investigationLeadTestSnapshot(t, root)
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	if !strings.Contains(context, questions) || !strings.Contains(context, "awaiting Investigator results") || strings.Contains(context, "fill statement") || !strings.Contains(context, "need no repeated save") {
		t.Fatalf("Main did not receive concrete waiting obligations: %s", context)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("waiting reminder wrote Runtime")
	}
}

func TestS8InvestigationLeadDispositionAvailableBeforeAnyHypothesisResult(t *testing.T) {
	root := investigationLeadTestRoot(t)
	investigationLeadTestIngest(t, root)
	current := investigationLeadTestSnapshot(t, root)
	paths := investigationLeadDispositionPaths(root, current.State)
	if len(paths) != 1 {
		t.Fatalf("route entry added an all-result prerequisite: %#v", paths)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("early professional route answer was not prepared: %v", err)
	}
	writeRepairLeadJSON(t, paths[0], investigationLeadRouteAnswer{Route: "s9_repair", RouteReason: "fixture requests repair before causal closure"})
	feedback := investigationLeadTestSave(t, root, paths[0])
	// The original consumer owns the qualification. The current feedback
	// names the missing professional field of the route answer instead of
	// the removed machine wording, and it must not write Runtime.
	if !strings.Contains(feedback, "original route validation") || !strings.Contains(feedback, "primary_root_cause") || investigationLeadTestSnapshot(t, root).Revision != current.Revision {
		t.Fatalf("s9 qualification did not stay with the original consumer: %s", feedback)
	}
	writeRepairLeadJSON(t, paths[0], investigationLeadRouteAnswer{Route: "s2_spec_rework", RouteReason: "the specification contradicts the established source authority"})
	feedback = investigationLeadTestSave(t, root, paths[0])
	next := investigationLeadTestSnapshot(t, root)
	if stringValue(mapFieldCLI(next.State, "lifecycle")["state"]) != "planning" {
		t.Fatalf("early spec disposition was blocked by UI result requirements: %s", feedback)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	feedback = investigationLeadTestSave(t, root, paths[0])
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !strings.Contains(feedback, "consumption is recorded") || !bytes.Equal(before, after) || !bytes.Equal(journal, journalAfter) {
		t.Fatalf("explicit consumed spec-route replay was not a read-only original fact: %s", feedback)
	}
	writeRepairLeadJSON(t, paths[0], investigationLeadRouteAnswer{Route: "s2_spec_rework", RouteReason: "an edited already consumed answer"})
	feedback = investigationLeadTestSave(t, root, paths[0])
	if !strings.Contains(feedback, "conflicts") || investigationLeadTestSnapshot(t, root).Revision != next.Revision {
		t.Fatalf("consumed-route answer conflict was lost after leaving S8: %s", feedback)
	}
}

func TestS8InvestigationLeadDispositionPreCompactDoesNotClaimMissingDraftReady(t *testing.T) {
	root, _ := investigationLeadDispositionResults(t, "inconclusive")
	current := investigationLeadTestSnapshot(t, root)
	paths := investigationLeadDispositionPaths(root, current.State)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreCompact"}, current)
	if len(paths) != 1 || !strings.Contains(context, paths[0]) || !strings.Contains(context, "Not prepared yet") || !strings.Contains(context, "next Main SessionStart or PreToolUse") {
		t.Fatalf("PreCompact claimed an unprepared route answer was ready: %s", context)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("PreCompact prepared a route questionnaire: %v", err)
	}
}

func TestS8InvestigationLeadDispositionFreshS7OldAnswerIsReadOnlyWithoutAcceptanceClaim(t *testing.T) {
	root := investigationLeadTestRoot(t)
	investigationLeadTestIngest(t, root)
	current := investigationLeadTestSnapshot(t, root)
	path := investigationLeadDispositionPaths(root, current.State)[0]
	writeRepairLeadJSON(t, path, investigationLeadRouteAnswer{Route: "s7_no_change", RouteReason: "the authority needs no artifact change"})
	investigationLeadTestSave(t, root, path)
	current = investigationLeadTestSnapshot(t, root)
	if investigationPointer(current.State) != nil || stringValue(mapFieldCLI(current.State, "lifecycle")["state"]) != "verification" {
		t.Fatal("original fresh S7 consumer did not clear the old Case pointer")
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	feedback := investigationLeadTestSave(t, root, path)
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !strings.Contains(feedback, "no longer the current S8 responsibility") || !strings.Contains(feedback, "does not assert a historical acceptance") || !bytes.Equal(before, after) || !bytes.Equal(journal, journalAfter) {
		t.Fatalf("old S7-exited route save was not truthful/read-only: %s", feedback)
	}
}

func TestS8InvestigationLeadDispositionInlineCausalAnswerOriginalRouteAndReplay(t *testing.T) {
	root, _ := investigationLeadDispositionResults(t, "supported", "refuted")
	current := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	paths := investigationLeadDispositionPaths(root, current.State)
	if len(paths) != 1 || !strings.Contains(filepath.Base(paths[0]), "route-") || !strings.Contains(context, "causal_model") || strings.Contains(context, "fill statement") {
		t.Fatalf("completed investigators did not hand Main the single route answer: %s", context)
	}
	path := paths[0]
	answer := investigationLeadRouteAnswer{Route: "s9_repair", RouteReason: "the supported mechanism explains the exact Finding set", PrimaryRootCause: "the first boundary drops source authority", CausalModel: map[string]any{"trigger": "request", "violated_invariant": "source authority", "faulty_mechanism": "boundary discards authority", "propagation": "downstream reads a fallback", "symptoms": []any{"finding-1"}}, BlastRadius: map[string]any{"paths": []any{"internal/example"}}, DetectionGap: map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://gap"}}}
	writeRepairLeadJSON(t, path, answer)
	feedback := investigationLeadTestSave(t, root, path)
	if !strings.Contains(feedback, "s9_repair") || !strings.Contains(feedback, "not approval") {
		t.Fatalf("original route did not preserve the Contract boundary: %s", feedback)
	}
	accepted := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, accepted.State)
	if stringValue(document["route"]) != "s9_repair" || document["causal_model"] == nil || stringValue(investigationPointer(accepted.State)["status"]) != "investigating" {
		t.Fatalf("original Case route/model not persisted: %#v", document)
	}
	if next := investigationLeadDispositionPaths(root, accepted.State); len(next) != 1 || next[0] != path {
		t.Fatalf("committed route lost its original source-bound replay path: %#v", next)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	investigationLeadTestSave(t, root, path)
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !bytes.Equal(before, after) || !bytes.Equal(journal, journalAfter) {
		t.Fatal("route replay created another domain write")
	}
	answer.PrimaryRootCause = "a different professional conclusion"
	writeRepairLeadJSON(t, path, answer)
	if feedback := investigationLeadTestSave(t, root, path); !strings.Contains(feedback, "conflicts") {
		t.Fatalf("changed route was silently accepted: %s", feedback)
	}
	if investigationLeadTestSnapshot(t, root).Revision != accepted.Revision {
		t.Fatal("changed professional conclusion rewrote Runtime")
	}
}

func TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit(t *testing.T) {
	for _, route := range []string{"investigate_more", "s2_spec_rework", "human_req_change"} {
		t.Run(route, func(t *testing.T) {
			root, questions := investigationLeadDispositionResults(t, "inconclusive")
			current := investigationLeadTestSnapshot(t, root)
			context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
			paths := investigationLeadDispositionPaths(root, current.State)
			// An unresolved explanation is continued by appending the next
			// discriminating question to the original hypotheses answer; no
			// route registration is a prerequisite for that.
			if len(paths) != 1 || !strings.Contains(context, "directly append") || !strings.Contains(context, "No investigate_more route registration is a prerequisite") {
				t.Fatalf("received inconclusive was treated as unfinished work: %s", context)
			}
			writeRepairLeadJSON(t, paths[0], investigationLeadRouteAnswer{Route: route, RouteReason: "the original evidence requires this professional disposition"})
			feedback := investigationLeadTestSave(t, root, paths[0])
			next := investigationLeadTestSnapshot(t, root)
			switch route {
			case "investigate_more":
				if stringValue(investigationPointer(next.State)["route_consumed_at"]) != "" || !strings.Contains(feedback, questions) {
					t.Fatalf("investigate_more was consumed or lost the current Main path: %s", feedback)
				}
			case "s2_spec_rework":
				if stringValue(mapFieldCLI(next.State, "lifecycle")["state"]) != "planning" || stringValue(investigationPointer(next.State)["route_consumed_at"]) == "" {
					t.Fatalf("original route did not consume the specification exit: %s", feedback)
				}
			case "human_req_change":
				if stringValue(mapFieldCLI(next.State, "lifecycle")["state"]) != "bug_resolution" || !strings.Contains(feedback, "actual human") || stringValue(investigationPointer(next.State)["route_consumed_at"]) != "" {
					t.Fatalf("REQ answer bypassed the actual human boundary: %s", feedback)
				}
			}
		})
	}
}

func investigationLeadDispositionResults(t *testing.T, verdicts ...string) (string, string) {
	t.Helper()
	root := investigationLeadTestRoot(t)
	_, questions := investigationLeadTestIngest(t, root)
	var authored []investigationLeadQuestion
	for index, verdict := range verdicts {
		authored = append(authored, investigationLeadTestQuestion(fmt.Sprintf("test mechanism %s #%d", verdict, index+1)))
	}
	writeRepairLeadJSON(t, questions, authored)
	investigationLeadTestSave(t, root, questions)
	readRepairLeadJSON(t, questions, &authored)
	for index, q := range authored {
		current := investigationLeadTestSnapshot(t, root)
		var explains []string
		if verdicts[index] == "supported" {
			explains = []string{"finding-1"}
		}
		_, err := investigation.SubmitHypothesisResult(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), investigation.HypothesisResultRequest{ExpectedRevision: current.Revision, CaseID: stringValue(investigationPointer(current.State)["case_id"]), HypothesisID: q.HypothesisID, AssignmentID: "assignment-" + q.HypothesisID, Method: "inspect the frozen first boundary", EvidenceRefs: []string{"evidence://" + verdicts[index]}, SourceBoundaryRefs: []string{"finding:finding-1"}, Observed: "actual fixture observation", Counterfactual: "distinguish the competing mechanism", Result: verdicts[index], ExplainsFindingIDs: explains})
		if err != nil {
			t.Fatal(err)
		}
	}
	return root, questions
}

func TestS8InvestigationLeadDispositionExplainsOriginalCausalAnswerShape(t *testing.T) {
	root, _ := investigationLeadDispositionResults(t, "supported")
	current := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	// The route authoring guidance must still name the professional objects
	// the original consumer reads. The removed machine forms (the sealed
	// S7 Finding/Batch source closure, the sha256 evidence form and the
	// per-hypothesis registration restriction) are no longer required, and
	// the recommended free-object field names (L3-S8 §9.2) are not a
	// mechanical required-field list.
	for _, field := range []string{"primary_root_cause", "causal_model", "blast_radius", "detection_gap", "Explain important unknowns and credible alternatives honestly", "Suggested new_hypotheses remain suggestions", "No count of results", "proves causal closure", "Use actual source and file references without evidence recapture"} {
		if !strings.Contains(context, field) {
			t.Fatalf("route authoring guidance omitted original professional shape %q: %s", field, context)
		}
	}
	for _, removed := range []string{"#sha256=", "ObservationBatch", "sealed S7 Finding"} {
		if strings.Contains(context, removed) {
			t.Fatalf("route authoring guidance still uses the removed machine form %q: %s", removed, context)
		}
	}
	for _, hypothesisOnly := range []string{"For Hypothesis.evidence_refs only", "not evidence_refs values", "values are registered evidence IDs or real existing execution anchors"} {
		if strings.Contains(context, hypothesisOnly) {
			t.Fatalf("Hypothesis registration restriction was applied to the route: %s", context)
		}
	}
	paths := investigationLeadDispositionPaths(root, current.State)
	if len(paths) != 1 {
		t.Fatalf("expected one current route questionnaire: %#v", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if investigationLeadQuestionnaireHasProfessionalContent(paths[0], data) {
		t.Fatal("explanatory text was written into the blank professional draft")
	}
	if investigationLeadTestSnapshot(t, root).Revision != current.Revision {
		t.Fatal("explaining original route fields changed Case facts")
	}
}

func TestS8UnresolvedResultsAppendDirectlyWithoutRoutePrerequisite(t *testing.T) {
	root, questions := investigationLeadDispositionResults(t, "inconclusive")
	current := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	// The unresolved handoff must name the original hypotheses answer and
	// state that no route registration is a prerequisite. The recommended
	// free-object field names are not a mechanical required-field list
	// (L3-S8 §9.2).
	for _, value := range []string{questions, "directly append", "No investigate_more route registration is a prerequisite"} {
		if !strings.Contains(context, value) {
			t.Fatalf("unresolved handoff omitted %q: %s", value, context)
		}
	}
	var rows []investigationLeadQuestion
	readRepairLeadJSON(t, questions, &rows)
	rows = append(rows, investigationLeadTestQuestion("another discriminating check after inconclusive result"))
	writeRepairLeadJSON(t, questions, rows)
	feedback := investigationLeadTestSave(t, root, questions)
	current = investigationLeadTestSnapshot(t, root)
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil || len(objectSliceAny(document["hypotheses"])) != 2 || stringValue(document["route"]) != "" {
		t.Fatalf("direct append required route or lost original: %v %s", err, feedback)
	}
	// Legacy budget configuration does not constrain continued investigation.
	path := investigationLeadDispositionPaths(root, current.State)[0]
	mapFieldCLI(mapFieldCLI(current.State, "configuration"), "repair")["max_investigation_reentries"] = 1
	repairLeadWriteState(t, root, current.State)
	writeRepairLeadJSON(t, path, investigationLeadRouteAnswer{Route: "investigate_more", RouteReason: "synthetic unresolved mechanism"})
	investigationLeadTestSave(t, root, path)
	current = investigationLeadTestSnapshot(t, root)
	context = stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	for _, value := range []string{questions, "may choose an earlier"} {
		if !strings.Contains(context, value) {
			t.Fatalf("exhausted handoff omitted %q: %s", value, context)
		}
	}
	if strings.Contains(context, "budget") || strings.Contains(context, "Reentry limit reached") {
		t.Fatalf("context still imposes a route budget: %s", context)
	}
	readRepairLeadJSON(t, questions, &rows)
	rows = append(rows, investigationLeadTestQuestion("a necessary discriminator after route budget exhaustion"))
	writeRepairLeadJSON(t, questions, rows)
	feedback = investigationLeadTestSave(t, root, questions)
	current = investigationLeadTestSnapshot(t, root)
	_, document, _, err = investigationLeadSource(root, current.State)
	if err != nil || len(objectSliceAny(document["hypotheses"])) != 3 {
		t.Fatalf("budget exhausted legitimate questions: %v %s", err, feedback)
	}
}

func TestS8InvestigationLeadDispositionCompetingAssessmentOriginalRouteAndReplay(t *testing.T) {
	root, _ := investigationLeadDispositionResults(t, "supported", "supported")
	current := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	paths := investigationLeadDispositionPaths(root, current.State)
	if len(paths) != 1 || !strings.Contains(filepath.Base(paths[0]), "route-") || !strings.Contains(context, "causal_model") || strings.Contains(context, "fill statement") {
		t.Fatalf("completed investigators did not hand Main the single route answer: %s", context)
	}
	path := paths[0]
	answer := investigationLeadRouteAnswer{Route: "s9_repair", RouteReason: "the supported mechanism explains the exact Finding set", PrimaryRootCause: "the first boundary drops source authority", CausalModel: map[string]any{"trigger": "request", "violated_invariant": "source authority", "faulty_mechanism": "boundary discards authority", "propagation": "downstream reads a fallback", "symptoms": []any{"finding-1"}}, BlastRadius: map[string]any{"paths": []any{"internal/example"}}, DetectionGap: map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://gap"}}}
	_, sourceCase, _, err := investigationLeadSource(root, current.State)
	if err != nil {
		t.Fatal(err)
	}
	hypothesis := objectSliceAny(sourceCase["hypotheses"])[0]
	answer.CompetingHypothesisAssessment = map[string]any{"hypothesis_ids": []any{hypothesis["hypothesis_id"]}, "conclusion": "The discriminator located the gap while retaining the credible contrast as propagation."}
	writeRepairLeadJSON(t, path, answer)
	feedback := investigationLeadTestSave(t, root, path)
	if !strings.Contains(feedback, "s9_repair") || !strings.Contains(feedback, "not approval") {
		t.Fatalf("original route did not preserve the Contract boundary: %s", feedback)
	}
	accepted := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, accepted.State)
	if stringValue(document["route"]) != "s9_repair" || document["causal_model"] == nil || stringValue(investigationPointer(accepted.State)["status"]) != "investigating" {
		t.Fatalf("original Case route/model not persisted: %#v", document)
	}
	if next := investigationLeadDispositionPaths(root, accepted.State); len(next) != 1 || next[0] != path {
		t.Fatalf("committed route lost its original source-bound replay path: %#v", next)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	investigationLeadTestSave(t, root, path)
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !bytes.Equal(before, after) || !bytes.Equal(journal, journalAfter) {
		t.Fatal("route replay created another domain write")
	}
	answer.PrimaryRootCause = "a different professional conclusion"
	writeRepairLeadJSON(t, path, answer)
	if feedback := investigationLeadTestSave(t, root, path); !strings.Contains(feedback, "conflicts") {
		t.Fatalf("changed route was silently accepted: %s", feedback)
	}
	if investigationLeadTestSnapshot(t, root).Revision != accepted.Revision {
		t.Fatal("changed professional conclusion rewrote Runtime")
	}
}

func TestS8OriginalRouteAnswerContinuesSameMaterialAndReplays(t *testing.T) {
	root, _ := investigationLeadDispositionResults(t, "supported", "refuted")
	current := investigationLeadTestSnapshot(t, root)
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	path := investigationLeadDispositionPaths(root, current.State)[0]
	answer := investigationLeadRouteAnswer{Route: "investigate_more", RouteReason: "Main is still weighing the accepted observations"}
	for _, reason := range []string{answer.RouteReason, "Main reconsiders the same observations"} {
		answer.RouteReason = reason
		writeRepairLeadJSON(t, path, answer)
		feedback := investigationLeadTestSave(t, root, path)
		current = investigationLeadTestSnapshot(t, root)
		_, document, _, err := investigationLeadSource(root, current.State)
		if err != nil || stringValue(document["route_reason"]) != reason {
			t.Fatalf("original nonterminal answer rejected: %v %s", err, feedback)
		}
		if paths := investigationLeadDispositionPaths(root, current.State); len(paths) != 1 || paths[0] != path {
			t.Fatalf("original answer moved: %#v", paths)
		}
		before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
		journal, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		investigationLeadTestSave(t, root, path)
		after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
		journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		if !bytes.Equal(before, after) || !bytes.Equal(journal, journalAfter) {
			t.Fatal("unchanged nonterminal answer replay wrote facts")
		}
	}
	answer = investigationLeadRouteAnswer{Route: "s9_repair", RouteReason: "the same material now supports the causal judgment", PrimaryRootCause: "the first boundary drops source authority", CausalModel: map[string]any{"trigger": "request", "violated_invariant": "source authority", "faulty_mechanism": "boundary discards authority", "propagation": "downstream reads a fallback", "symptoms": []any{"finding-1"}}, BlastRadius: map[string]any{"paths": []any{"internal/example"}}, DetectionGap: map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://gap"}}}
	writeRepairLeadJSON(t, path, answer)
	feedback := investigationLeadTestSave(t, root, path)
	current = investigationLeadTestSnapshot(t, root)
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil || stringValue(document["route"]) != "s9_repair" || !strings.Contains(feedback, "not approval") {
		t.Fatalf("same-material judgment did not reach original consumer: %v %s", err, feedback)
	}
	acceptedRevision := current.Revision
	investigationLeadTestSave(t, root, path)
	if investigationLeadTestSnapshot(t, root).Revision != acceptedRevision {
		t.Fatal("terminal replay wrote facts")
	}
	answer.Route = "investigate_more"
	writeRepairLeadJSON(t, path, answer)
	feedback = investigationLeadTestSave(t, root, path)
	if !strings.Contains(feedback, "conflicts") || investigationLeadTestSnapshot(t, root).Revision != acceptedRevision {
		t.Fatalf("terminal disposition overwritten: %s", feedback)
	}
}
