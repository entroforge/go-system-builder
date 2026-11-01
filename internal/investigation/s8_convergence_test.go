package investigation_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS8OriginalAnswerKeepsSuggestionsWithoutCreatingResponsibilities(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v (%d)", err, len(packets))
	}
	p := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, f.root, p, "The actual observation supports this mechanism.")
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, answerPath)), &draft); err != nil {
		t.Fatal(err)
	}
	answer := draft["answer"].(map[string]any)
	answer["evidence_refs"] = []string{"real/observation.txt"}
	answer["source_boundary_refs"] = []string{"original source at service.go:42"}
	answer["new_hypotheses"] = []workpacket.HypothesisNewQuestion{{Statement: "A separate caller may also lose the value", Invariant: "The value crosses callers", Discriminator: "Compare the other caller", ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: "It loses the value", Refute: "It retains the value"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"real/other-observation.txt"}}}
	writeInvestigationJSON(t, filepath.Join(f.root, answerPath), draft)
	before := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	service := &submission.Service{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath}
	receipt, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, filepath.Join(f.root, answerPath), "", nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("accept original answer: %+v %v", receipt, err)
	}
	after := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	if len(after["hypotheses"].([]any)) != len(before["hypotheses"].([]any)) {
		t.Fatal("suggestion created a new responsibility")
	}
	rows := after["hypothesis_results"].([]any)
	row := rows[len(rows)-1].(map[string]any)
	suggestions := row["new_hypotheses"].([]any)
	if len(suggestions) != 1 || suggestions[0].(map[string]any)["hypothesis_id"] != nil {
		t.Fatalf("suggestion changed into an issued question: %v", suggestions)
	}
	if row["receipt"].(map[string]any)["submitted_answer"] != nil {
		t.Fatal("new receipt duplicates the professional body")
	}
	stateBefore, journalBefore := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	if err := os.Remove(filepath.Join(f.root, answerPath)); err != nil {
		t.Fatal(err)
	}
	replay, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, filepath.Join(f.root, answerPath), "", nil)
	if err != nil || !replay.Accepted {
		t.Fatalf("accepted missing private answer replay: %+v %v", replay, err)
	}
	if string(stateBefore) != string(mustRead(t, f.statePath)) || string(journalBefore) != string(mustRead(t, f.journalPath)) {
		t.Fatal("accepted replay persisted another result")
	}
}

func TestS8MainCanRouteWithAnUnansweredQuestionAndHonestUncertainty(t *testing.T) {
	f := readyCaseFixture(t, []string{"finding-1"})
	registerCurrentQuestion(t, f, "hypothesis-unanswered")
	_, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "Main judges the original observed mechanism sufficient; the unanswered historical question does not change this repair scope.", PrimaryRootCause: "The decoder drops the field", CausalModel: map[string]any{"mechanism": "decoder drops field", "unknown": "the other caller remains unexamined"}})
	if err != nil {
		t.Fatalf("Main route was replaced by a question-count gate: %v", err)
	}
}

func TestS8MinimalConclusionAndNaturalIncompleteContinuation(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v (%d)", err, len(packets))
	}
	p := packets[0]
	path := filepath.Join(f.root, p.AnswerPath)
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, path), &draft); err != nil {
		t.Fatal(err)
	}
	draft["answer"] = map[string]any{"result": "inconclusive"}
	writeInvestigationJSON(t, path, draft)
	service := &submission.Service{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath}
	first, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, path, "", nil)
	if err != nil || !first.Accepted || !first.WorkComplete {
		t.Fatalf("honest missing observations: %+v %v", first, err)
	}
	before := string(mustRead(t, f.journalPath))
	replay, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, path, "", nil)
	if err != nil || !replay.Accepted || !replay.WorkComplete || string(mustRead(t, f.journalPath)) != before {
		t.Fatalf("unchanged incomplete replay: %+v %v", replay, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	missing, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, path, "", nil)
	if err != nil || !missing.Accepted || !missing.WorkComplete || string(mustRead(t, f.journalPath)) != before {
		t.Fatalf("accepted inconclusive private file disappeared: %+v %v", missing, err)
	}
	queried, err := service.HypothesisResultReceipt(owner.ownerID, p.ID)
	if err != nil || !queried.Accepted || !queried.WorkComplete {
		t.Fatalf("receipt query lost inconclusive delivery: %+v %v", queried, err)
	}
	// A replay observes only the original accepted fact, even when a private
	// draft is malformed. No new author signal is needed to preserve delivery.
	if err := os.WriteFile(path, []byte("unfinished private edit"), 0600); err != nil {
		t.Fatal(err)
	}
	stopped, err := service.ObserveHypothesisResultFrom(context.Background(), owner.ownerID, path, "", nil, false)
	if err != nil || !stopped.Accepted || string(mustRead(t, f.journalPath)) != before {
		t.Fatalf("replay reopened an accepted private draft: %+v %v", stopped, err)
	}
	draft["answer"] = map[string]any{"result": "refuted", "observed": "The newly available trace contradicts this hypothesis", "evidence_refs": []string{"/actual/new-observation.log"}, "new_hypotheses": []any{map[string]any{"statement": "A different mechanism remains possible"}}}
	writeInvestigationJSON(t, path, draft)
	next, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, path, "", nil)
	if err != nil || !next.Accepted || !next.WorkComplete {
		t.Fatalf("natural continued result: %+v %v", next, err)
	}
	document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	rows := document["hypothesis_results"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["result"] != "inconclusive" || rows[1].(map[string]any)["result"] != "refuted" {
		t.Fatalf("accepted history lost: %v", rows)
	}
	if len(document["hypotheses"].([]any)) != 1 {
		t.Fatal("incomplete suggestion created a responsibility")
	}
}
