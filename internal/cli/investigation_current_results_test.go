package cli

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// Original Main authoring/consumer chain with synthetic professional answers;
// native platform dispatch and product diagnosis are not claimed by this test.
func TestS8MainAppendsRealResultsOnSameEvidenceAndKeepsEarlierUnknown(t *testing.T) {
	root := investigationLeadTestRoot(t)
	_, path := investigationLeadTestIngest(t, root)
	questions := []investigationLeadQuestion{investigationLeadTestQuestion("the initial observation cannot distinguish the failing boundary")}
	writeRepairLeadJSON(t, path, questions)
	investigationLeadTestSave(t, root, path)
	readRepairLeadJSON(t, path, &questions)
	submit := func(question investigationLeadQuestion, result string) {
		t.Helper()
		current := investigationLeadTestSnapshot(t, root)
		var explains []string
		if result == "supported" {
			explains = []string{"finding-1"}
		}
		_, err := investigation.SubmitHypothesisResult(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), investigation.HypothesisResultRequest{
			ExpectedRevision: current.Revision, CaseID: stringValue(investigationPointer(current.State)["case_id"]), HypothesisID: question.HypothesisID, AssignmentID: "assignment-" + question.HypothesisID,
			Method: "read-only interpretation of the original recorded evidence", EvidenceRefs: []string{"evidence://finding-1"}, SourceBoundaryRefs: []string{"finding:finding-1"},
			Observed: "The actual boundary comparison is " + result, Counterfactual: "a preserving boundary would keep source authority", Result: result, ExplainsFindingIDs: explains,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	submit(questions[0], "inconclusive")
	current := investigationLeadTestSnapshot(t, root)
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	routes := investigationLeadDispositionPaths(root, current.State)
	if len(routes) != 1 {
		t.Fatal("Main has no exact original route entry")
	}
	writeRepairLeadJSON(t, routes[0], investigationLeadRouteAnswer{Route: "investigate_more", RouteReason: "The recorded occurrence needs a separate decoder/transport comparison before choosing repair."})
	investigationLeadTestSave(t, root, routes[0])
	questions = append(questions, investigationLeadTestQuestion("the decoder mapping drops authority"), investigationLeadTestQuestion("the transport drops authority"))
	writeRepairLeadJSON(t, path, questions)
	investigationLeadTestSave(t, root, path)
	readRepairLeadJSON(t, path, &questions)
	if len(questions) != 3 {
		t.Fatal("original Main authoring did not retain all questions")
	}
	submit(questions[1], "supported")
	submit(questions[2], "refuted")
	current = investigationLeadTestSnapshot(t, root)
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	routes = investigationLeadDispositionPaths(root, current.State)
	writeRepairLeadJSON(t, routes[0], investigationLeadRouteAnswer{
		Route: "s9_repair", RouteReason: "Later comparisons of the same recorded source resolve the original unknown: transport preserves authority, while the decoder drops it.",
		PrimaryRootCause: "the decoder mapping omits the source authority field",
		CausalModel:      map[string]any{"trigger": "a valid request reaches decoding", "violated_invariant": "source authority survives the boundary", "faulty_mechanism": "the decoder omits the field", "propagation": "the downstream consumer cannot identify the source", "symptoms": []string{"finding-1"}},
		BlastRadius:      map[string]any{"paths": []string{"internal/decoder"}}, DetectionGap: map[string]any{"gap_type": "test", "evidence_refs": []string{"evidence://finding-1"}},
	})
	feedback := investigationLeadTestSave(t, root, routes[0])
	_, document, _, err := investigationLeadSource(root, investigationLeadTestSnapshot(t, root).State)
	if err != nil || document["route"] != "s9_repair" || len(objectSliceAny(document["hypothesis_results"])) != 3 || objectSliceAny(document["hypothesis_results"])[0]["result"] != "inconclusive" {
		t.Fatalf("Main's original route did not preserve and resolve the earlier work: document=%v err=%v feedback=%s", document, err, feedback)
	}
}
