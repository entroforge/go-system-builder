package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
)

const currentResultsCase = "investigation-case-review-plan-r1"

func registerCurrentQuestion(t *testing.T, f *intakeFixture, id string) {
	t.Helper()
	_, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{
		ExpectedRevision: -1, CaseID: currentResultsCase, HypothesisID: id, AssignmentID: "assignment-" + id,
		Statement: "The decoder loses a required field at " + id, Invariant: "the required field survives the boundary",
		Discriminator:    "Compare the actual boundary evidence for " + id,
		ExpectedOutcomes: map[string]any{"support": "the boundary loses the field", "refute": "the boundary preserves the field"},
		SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"evidence://boundary"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func submitCurrentResult(t *testing.T, f *intakeFixture, id, assignment, result string, evidence ...string) {
	t.Helper()
	var explains []string
	if result == "supported" {
		explains = []string{"finding-1"}
	}
	if len(evidence) == 0 {
		evidence = []string{"evidence://boundary"}
	}
	_, err := investigation.SubmitHypothesisResult(f.root, f.statePath, f.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision: -1, CaseID: currentResultsCase, HypothesisID: id, AssignmentID: assignment,
		Method: "read-only comparison of the recorded boundary evidence", EvidenceRefs: evidence,
		SourceBoundaryRefs: []string{"service.go:87"}, Observed: "The source comparison yielded " + result + " for " + id,
		Counterfactual: "a preserving decoder would keep the field at the same boundary", Result: result, ExplainsFindingIDs: explains,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppendedInvestigationResultsResolveEarlierUnknownWithoutRewritingIt(t *testing.T) {
	for _, freshReference := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-evidence", true: "new-evidence"}[freshReference], func(t *testing.T) { testAppendedInvestigationResults(t, freshReference) })
	}
}

func testAppendedInvestigationResults(t *testing.T, freshReference bool) {
	t.Helper()
	f := readyCaseFixture(t, []string{"finding-1"})
	registerCurrentQuestion(t, f, "hypothesis-initial")
	submitCurrentResult(t, f, "hypothesis-initial", "assignment-hypothesis-initial", "inconclusive")
	_, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "investigate_more", RouteReason: "The first observation cannot distinguish transport loss from decoder loss; inspect the recorded boundary source."})
	if err != nil {
		t.Fatal(err)
	}
	pointer := investigationPointer(t, f)
	oldPath := pointer["path"].(string)
	oldBytes := mustRead(t, f.root+"/"+oldPath)
	registerCurrentQuestion(t, f, "hypothesis-decoder")
	submitCurrentResult(t, f, "hypothesis-decoder", "assignment-hypothesis-decoder", "supported")
	registerCurrentQuestion(t, f, "hypothesis-transport")
	if freshReference {
		submitCurrentResult(t, f, "hypothesis-transport", "assignment-hypothesis-transport", "refuted", "evidence://cache-inspection")
	} else {
		submitCurrentResult(t, f, "hypothesis-transport", "assignment-hypothesis-transport", "refuted")
	}
	seedCausalClosure(t, f, investigationPointer(t, f), nil)
	_, err = investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "The later decoder and transport comparisons of the original source resolve the earlier discriminator gap: transport preserves the field and decoder mapping drops it."})
	if err != nil {
		t.Fatalf("new accepted results on the original evidence could not close the original Case: %v", err)
	}
	doc := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	results := doc["hypothesis_results"].([]any)
	if len(results) != 3 || results[0].(map[string]any)["result"] != "inconclusive" || len(doc["hypotheses"].([]any)) != 3 {
		t.Fatalf("closure rewrote or removed the initial work: %v", results)
	}
	if !bytes.Equal(oldBytes, mustRead(t, f.root+"/"+oldPath)) {
		t.Fatal("later closure rewrote the earlier immutable Case")
	}
}

func TestCurrentUnansweredQuestionCannotBorrowCompleteCausalMaterials(t *testing.T) {
	f := readyCaseFixture(t, []string{"finding-1"})
	pointer, _, _ := discriminatedCaseFixture(t, f)
	seedCausalClosure(t, f, pointer, nil)
	registerCurrentQuestion(t, f, "hypothesis-still-open")
	before, journal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	// L3-S8:307 states that the unexplained set, hypothesis states and source
	// association are navigation aids and do not become machine gates for
	// per-question completion. L3-S8:231 adds that saving is not gated on
	// proving every source, every peer answer or rebuildable history. An open
	// current question therefore must not block the route, and the machine must
	// not fabricate an answer for it.
	_, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "The materials look complete but a real current discriminator is unanswered."})
	if err != nil {
		t.Fatalf("machine refused the route over an unanswered current question: %v", err)
	}
	if bytes.Equal(before, mustRead(t, f.statePath)) {
		t.Fatal("route did not advance the Runtime")
	}
	if bytes.Equal(journal, mustRead(t, f.journalPath)) {
		t.Fatal("route did not record the Runtime journal")
	}
	document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	for _, raw := range document["hypothesis_results"].([]any) {
		if raw.(map[string]any)["hypothesis_id"] == "hypothesis-still-open" {
			t.Fatal("machine fabricated a result for the unanswered current question")
		}
	}
}

// The low-level Case revision API is also an original consumer. This scope
// test does not claim that Main's normal authoring form can reassign an issued
// question or that an Investigator dispatch was created for the new tuple.
// L3-S8:307 keeps the machine from certifying causal closure: an old
// conclusion bound to a different Assignment is not a machine gate on the
// route. L3-S8:235 keeps the accepted facts: old results are retained rather
// than overwritten, so the historical conclusion must still be readable and
// must not be rewritten by the later current result.
func TestOriginalCaseRevisionCannotBorrowHistoricalAssignmentConclusions(t *testing.T) {
	for _, id := range []string{"hypothesis-boundary", "hypothesis-cache"} {
		t.Run(id, func(t *testing.T) {
			f := readyCaseFixture(t, []string{"finding-1"})
			pointer, _, _ := discriminatedCaseFixture(t, f)
			seedCausalClosure(t, f, pointer, nil)
			_, err := investigation.UpdateCase(f.root, f.statePath, f.journalPath, investigation.CaseRevisionRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Operation: "case_revised", Mutate: func(doc map[string]any) error {
				for _, raw := range doc["hypotheses"].([]any) {
					h := raw.(map[string]any)
					if h["hypothesis_id"] == id {
						h["assignment_id"], h["status"] = "assignment-revised-current", "open"
					}
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			before, journal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			_, err = investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "Historical support/refutation cannot satisfy a different current Assignment."})
			if err != nil {
				t.Fatalf("machine refused the route over a historical Assignment conclusion: %v", err)
			}
			if bytes.Equal(before, mustRead(t, f.statePath)) {
				t.Fatal("route did not advance the Runtime")
			}
			if bytes.Equal(journal, mustRead(t, f.journalPath)) {
				t.Fatal("route did not record the Runtime journal")
			}
			// The accepted historical conclusion stays in the record; the later
			// current result is added beside it and does not overwrite it.
			result := "inconclusive"
			if id == "hypothesis-boundary" {
				result = "refuted"
			}
			submitCurrentResult(t, f, id, "assignment-revised-current", result)
			document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
			assignments := map[string]string{}
			for _, raw := range document["hypothesis_results"].([]any) {
				row := raw.(map[string]any)
				if row["hypothesis_id"] == id {
					assignments[s8TestString(row["assignment_id"])] = s8TestString(row["result"])
				}
			}
			if assignments["assignment-revised-current"] != result {
				t.Fatalf("current result for %s was not recorded: %v", id, assignments)
			}
			if _, ok := assignments["assignment-boundary"]; !ok {
				if _, ok := assignments["assignment-cache"]; !ok {
					t.Fatalf("historical conclusion for %s was overwritten: %v", id, assignments)
				}
			}
		})
	}
}

func TestContractCannotApproveWithAnUnansweredCurrentQuestion(t *testing.T) {
	f := readyCaseFixture(t, []string{"finding-1"})
	pointer, _, _ := discriminatedCaseFixture(t, f)
	seedCausalClosure(t, f, pointer, nil)
	_, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "The initial accepted discriminators close the original mechanism."})
	if err != nil {
		t.Fatal(err)
	}
	registerCurrentQuestion(t, f, "hypothesis-new-current")
	path := writeContractDraft(t, f.root, []string{"finding-1"})
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, path), &draft); err != nil {
		t.Fatal(err)
	}
	draft["revision"] = integerPointerRevision(investigationPointer(t, f))
	body, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	// Supply the original valid approval fixture as well, so acceptance cannot
	// be explained by a missing decision, bad draft version or unpinned bytes.
	_, approvalID, _ := registerContractApprovalEvidence(t, f, path)
	before, journal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	// L3-S8:307 keeps the machine from certifying per-question completion, and
	// L3-S8:231 states that saving is not gated on proving every source or
	// every peer answer. A still-unanswered current question therefore must not
	// block the approval, while the real approval itself must still happen.
	approved, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, investigation.ContractRequest{ExpectedRevision: -1, CaseID: currentResultsCase, ContractPath: path, ApprovedBy: "main-session", ApprovalEvidenceID: approvalID})
	if err != nil {
		t.Fatalf("real contract approval was refused over an unanswered current question: %v", err)
	}
	if approved.Revision <= 0 {
		t.Fatalf("approved contract produced no Runtime revision: %+v", approved)
	}
	if bytes.Equal(before, mustRead(t, f.statePath)) {
		t.Fatal("approved contract did not advance the Runtime")
	}
	if bytes.Equal(journal, mustRead(t, f.journalPath)) {
		t.Fatal("approved contract did not record the Runtime journal")
	}
	document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	if s8TestString(document["status"]) != "contract_approved" {
		t.Fatalf("approved contract did not reach the approved Case status: %v", document["status"])
	}
	for _, raw := range document["hypothesis_results"].([]any) {
		if raw.(map[string]any)["hypothesis_id"] == "hypothesis-new-current" {
			t.Fatal("machine fabricated a result for the unanswered current question")
		}
	}
}
