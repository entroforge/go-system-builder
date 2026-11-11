package investigation_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The late Finding row models an already received original S7 result. The
// separate CLI natural-entry test exercises the actual S7 and Main listeners.
func TestCaseSelectsAcceptedLateFindingWithoutLosingOldInvestigator(t *testing.T) {
	for _, alreadyIssued := range []bool{false, true} {
		t.Run(map[bool]string{false: "registered", true: "issued"}[alreadyIssued], func(t *testing.T) {
			f, owners := prepareS8GenerationTwoFixture(t, false)
			oldOwner := owners[0]
			var oldPacket workpacket.HypothesisResultPacket
			if alreadyIssued {
				packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, oldOwner.ownerID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("issue prior responsibility: %d %v", len(packets), err)
				}
				oldPacket = packets[0]
			}
			current := currentS8Snapshot(t, f)
			state := current.State
			oldPointer := investigationPointerFromState(t, state)
			oldDoc := readCaseDocument(t, f.root, oldPointer["path"].(string))
			oldHypotheses := oldDoc["hypotheses"]
			oldPath := filepath.Join(f.root, filepath.FromSlash(oldPointer["path"].(string)))
			oldBytes := mustRead(t, oldPath)
			rows := state["entities"].(map[string]any)["findings"].([]any)
			original := rows[0].(map[string]any)
			raw, _ := json.Marshal(original)
			var late map[string]any
			_ = json.Unmarshal(raw, &late)
			late["finding_id"] = "finding-late"
			late["path"] = ".claude/evidence/findings/finding-late.json"
			lateDoc := map[string]any{}
			if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, original["path"].(string))), &lateDoc); err != nil {
				t.Fatal(err)
			}
			lateDoc["finding_id"] = "finding-late"
			writeInvestigationJSON(t, filepath.Join(f.root, late["path"].(string)), lateDoc)
			state["entities"].(map[string]any)["findings"] = append(rows, late)
			writeInvestigationJSON(t, f.statePath, state)
			occupied := filepath.Join(f.root, ".claude", "review", "investigation", "cases", fmt.Sprintf("%s-r%d.json", oldPointer["case_id"], s8TestInt(oldPointer["revision"])+1))
			if err := os.WriteFile(occupied, []byte("unowned user content"), 0600); err != nil {
				t.Fatal(err)
			}
			request := investigation.HypothesisRequest{ExpectedRevision: -1, CaseID: oldPointer["case_id"].(string), HypothesisID: "hypothesis-late", AssignmentID: "assignment-late", Statement: "the late observation has another mechanism", Invariant: "the boundary keeps authority", Discriminator: "inspect the late path", ExpectedOutcomes: map[string]any{"support": "the boundary loses authority", "refute": "the boundary keeps authority"}, SourceFindingIDs: []string{"finding-late"}, EvidenceRefs: []string{"finding:finding-late"}}
			updated, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, request)
			if err != nil {
				t.Fatalf("select late Finding: %v", err)
			}
			next := investigationPointerFromState(t, updated.State)
			if got, err := os.ReadFile(occupied); err != nil || string(got) != "unowned user content" || filepath.Join(f.root, next["path"].(string)) == occupied {
				t.Fatalf("unowned Case path was claimed or changed: %q %v", got, err)
			}
			doc := readCaseDocument(t, f.root, next["path"].(string))
			if !strings.Contains(toJSON(t, doc["source_finding_ids"]), "finding-late") || !strings.Contains(toJSON(t, doc["unexplained_finding_ids"]), "finding-late") {
				t.Fatalf("late source missing: %v", doc)
			}
			if len(doc["hypotheses"].([]any)) != 2 || !reflect.DeepEqual(doc["hypotheses"].([]any)[0], oldHypotheses.([]any)[0]) {
				t.Fatal("existing question was changed")
			}
			if !reflect.DeepEqual(oldBytes, mustRead(t, oldPath)) {
				t.Fatal("previous Case revision changed")
			}
			if !alreadyIssued {
				packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, oldOwner.ownerID)
				if err != nil || len(packets) != 1 {
					t.Fatalf("issue previous registered question after adding Finding: %d %v", len(packets), err)
				}
				oldPacket = packets[0]
			}
			nextOccupied := filepath.Join(f.root, ".claude", "review", "investigation", "cases", fmt.Sprintf("%s-r%d.json", next["case_id"], s8TestInt(next["revision"])+1))
			if err := os.WriteFile(nextOccupied, []byte("unowned result-path content"), 0600); err != nil {
				t.Fatal(err)
			}
			path := writeS8ProfessionalAnswer(t, f.root, oldPacket, "The original question remains answerable.")
			accepted, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: oldPacket.ID, AnswerPath: path, OwnerAgentID: oldOwner.ownerID, SourceRoot: f.root})
			if err != nil {
				t.Fatalf("receive old original after late source: %v", err)
			}
			receipt, ok, err := investigation.IssuedHypothesisResultReceipt(f.root, accepted.State, oldPacket.ID, oldOwner.ownerID)
			if err != nil || !ok {
				t.Fatalf("old original receipt: %t %v", ok, err)
			}
			acceptedPath := investigationPointerFromState(t, accepted.State)["path"].(string)
			if receipt.SuccessorCaseRef != acceptedPath || filepath.Join(f.root, acceptedPath) == nextOccupied {
				t.Fatalf("receipt points to %q but actual Case is %q", receipt.SuccessorCaseRef, acceptedPath)
			}
			if got, err := os.ReadFile(nextOccupied); err != nil || string(got) != "unowned result-path content" {
				t.Fatalf("unowned result path changed: %q %v", got, err)
			}
			before := mustRead(t, f.statePath)
			request.HypothesisID = "hypothesis-unknown"
			request.AssignmentID = "assignment-unknown"
			request.SourceFindingIDs = []string{"finding-not-received"}
			if _, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, request); err == nil || !strings.Contains(err.Error(), "has not been received") {
				t.Fatalf("unknown source: %v", err)
			}
			if !reflect.DeepEqual(before, mustRead(t, f.statePath)) {
				t.Fatal("unknown source changed current Case")
			}
		})
	}
}
func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCaseWriterConcurrentQuestionsKeepEachResponsibility(t *testing.T) {
	f := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, f)
	caseID := pointer["case_id"].(string)
	nextRevision := s8TestInt(pointer["revision"]) + 1
	occupied := filepath.Join(f.root, ".claude", "review", "investigation", "cases", fmt.Sprintf("%s-r%d.json", caseID, nextRevision))
	if err := os.WriteFile(occupied, []byte("unowned concurrent path"), 0600); err != nil {
		t.Fatal(err)
	}
	requests := make([]investigation.HypothesisRequest, 2)
	for i := range requests {
		requests[i] = investigation.HypothesisRequest{ExpectedRevision: 1, ExpectedCaseRevision: nextRevision - 1, CaseID: caseID, HypothesisID: fmt.Sprintf("hypothesis-concurrent-%d", i), AssignmentID: fmt.Sprintf("assignment-concurrent-%d", i), Statement: fmt.Sprintf("question %d", i), Invariant: "the value survives", Discriminator: "inspect the boundary", ExpectedOutcomes: map[string]any{"support": "lost", "refute": "survives"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"finding:finding-1"}}
	}
	start := make(chan struct{})
	results := make(chan struct {
		index int
		err   error
	}, 2)
	for i := range requests {
		go func(i int) {
			<-start
			_, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, requests[i])
			results <- struct {
				index int
				err   error
			}{i, err}
		}(i)
	}
	close(start)
	failed := -1
	successes := 0
	for range requests {
		result := <-results
		if result.err == nil {
			successes++
		} else if strings.Contains(result.err.Error(), "stale runtime revision") || strings.Contains(result.err.Error(), "stale Case revision") {
			// G18 §3.1: the Runtime write count is no longer the comparison
			// that refuses the loser. The loser is refused by the domain
			// version that actually moved — the Case's own revision — or by
			// the explicit write-count binding it still holds.
			failed = result.index
		} else {
			t.Fatalf("concurrent writer failed unexpectedly: %v", result.err)
		}
	}
	if successes != 1 || failed < 0 {
		t.Fatalf("CAS must publish one winner, got %d winners, retry index %d", successes, failed)
	}
	requests[failed].ExpectedRevision = -1
	requests[failed].ExpectedCaseRevision = 0
	current, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, requests[failed])
	if err != nil {
		t.Fatalf("retry original losing question: %v", err)
	}
	p := investigationPointerFromState(t, current.State)
	doc := readCaseDocument(t, f.root, p["path"].(string))
	if len(doc["hypotheses"].([]any)) != 2 {
		t.Fatalf("concurrent questions lost: %v", doc["hypotheses"])
	}
	if got, err := os.ReadFile(occupied); err != nil || string(got) != "unowned concurrent path" {
		t.Fatalf("unknown occupied file changed: %q %v", got, err)
	}
}

func TestConcurrentOriginalInvestigatorsKeepTheirActualReceipts(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, true)
	packets := make([]workpacket.HypothesisResultPacket, 2)
	paths := make([]string, 2)
	for i := range packets {
		issued, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owners[i].ownerID)
		if err != nil || len(issued) != 1 {
			t.Fatalf("issue owner %d: %v", i, err)
		}
		packets[i] = issued[0]
		paths[i] = writeS8ProfessionalAnswer(t, f.root, packets[i], fmt.Sprintf("original investigator %d observation", i))
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range packets {
		go func(i int) {
			<-start
			_, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: packets[i].ID, AnswerPath: paths[i], OwnerAgentID: owners[i].ownerID, SourceRoot: f.root})
			results <- err
		}(i)
	}
	close(start)
	for range packets {
		if err := <-results; err != nil {
			t.Fatalf("concurrent original reception: %v", err)
		}
	}
	current := currentS8Snapshot(t, f)
	for i, p := range packets {
		receipt, ok, err := investigation.IssuedHypothesisResultReceipt(f.root, current.State, p.ID, owners[i].ownerID)
		if err != nil || !ok || receipt.SuccessorCaseRef == "" {
			t.Fatalf("original receipt %d: %t %+v %v", i, ok, receipt, err)
		}
		if _, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(receipt.SuccessorCaseRef))); err != nil {
			t.Fatalf("receipt %d names an unwritten Case: %v", i, err)
		}
	}
}

func TestApprovedCaseIsNotAutomaticallyRewrittenByNewQuestion(t *testing.T) {
	f := newIntakeFixture(t, []string{"finding-1"})
	setContractLifecycle(t, f)
	if _, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "investigate the actual observation"}); err != nil {
		t.Fatal(err)
	}
	prepareCaseForContractApproval(t, f)
	contract := writeContractDraft(t, f.root, []string{"finding-1"})
	_, approvalID, _ := registerContractApprovalEvidence(t, f, contract)
	accepted, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, investigation.ContractRequest{ExpectedRevision: -1, CaseID: "investigation-case-review-plan-r1", ContractPath: contract, ApprovedBy: "main-session", ApprovalEvidenceID: approvalID})
	if err != nil {
		t.Fatalf("real approval: %v", err)
	}
	pointer := investigationPointerFromState(t, accepted.State)
	caseBytes := mustRead(t, filepath.Join(f.root, pointer["path"].(string)))
	stateBefore := mustRead(t, f.statePath)
	journalBefore := mustRead(t, f.journalPath)
	_, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{ExpectedRevision: -1, CaseID: pointer["case_id"].(string), HypothesisID: "hypothesis-after-approval", AssignmentID: "assignment-after-approval", Statement: "a later observation may have another cause", Invariant: "the original invariant", Discriminator: "investigate the new boundary", ExpectedOutcomes: map[string]any{"support": "violated", "refute": "preserved"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"finding:finding-1"}})
	if err == nil {
		t.Fatal("new question silently rewrote a real approved Case")
	}
	if !reflect.DeepEqual(stateBefore, mustRead(t, f.statePath)) || !reflect.DeepEqual(journalBefore, mustRead(t, f.journalPath)) || !reflect.DeepEqual(caseBytes, mustRead(t, filepath.Join(f.root, pointer["path"].(string)))) {
		t.Fatal("real approval or its original Case was changed")
	}
}

func TestConcurrentSameInvestigatorReplaysOneOriginalReceipt(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owners[0].ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original question: %v", err)
	}
	packet := packets[0]
	path := writeS8ProfessionalAnswer(t, f.root, packet, "One original observation.")
	start := make(chan struct{})
	results := make(chan error, 4)
	for range 4 {
		go func() {
			<-start
			_, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: path, OwnerAgentID: owners[0].ownerID, SourceRoot: f.root})
			results <- err
		}()
	}
	close(start)
	for range 4 {
		if err := <-results; err != nil {
			t.Fatalf("same responsibility replay: %v", err)
		}
	}
	current := currentS8Snapshot(t, f)
	pointer := investigationPointerFromState(t, current.State)
	doc := readCaseDocument(t, f.root, pointer["path"].(string))
	if got := len(doc["hypothesis_results"].([]any)); got != 1 {
		t.Fatalf("same original answer produced %d results", got)
	}
	receipt, ok, err := investigation.IssuedHypothesisResultReceipt(f.root, current.State, packet.ID, owners[0].ownerID)
	if err != nil || !ok || receipt.SuccessorCaseRef != pointer["path"] {
		t.Fatalf("original receipt: %+v %t %v", receipt, ok, err)
	}
}
