package investigation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS8NewLocalEvidenceCapturesWorkerBytesAndRejectsDrift(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	source := registerS8WorkerExecution(t, fixture, currentS8Snapshot(t, fixture), owner)
	initial := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, initial.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v", err)
	}
	packet := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, source, packet, "Read the actual saved observation.")
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(source, answerPath)), &draft); err != nil {
		t.Fatal(err)
	}
	fields := draft["answer"].(map[string]any)
	rel := filepath.ToSlash(filepath.Join(filepath.Dir(answerPath), "observation.txt"))
	writeInvestigationBytes(t, source, rel, []byte("actual observation\n"))
	request := investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: answerPath, OwnerAgentID: owner.ownerID}
	for _, bad := range []string{"path:missing.txt", "path:../escape.txt", "path:" + rel + "#sha256=" + strings.Repeat("0", 64)} {
		fields["evidence_refs"] = []string{bad}
		writeInvestigationJSON(t, filepath.Join(source, answerPath), draft)
		if _, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, request); err == nil {
			t.Fatalf("accepted invalid evidence %s", bad)
		}
	}
	fields["evidence_refs"] = []string{"path:" + rel}
	fields["new_hypotheses"] = []map[string]any{{"statement": "A separate read boundary may independently lose the value.", "invariant": "Reads retain persisted values.", "discriminator": "Compare the already saved read observation with the writer; stop if same-call conditions are unavailable.", "expected_outcomes": map[string]string{"support": "Read lost value after successful write.", "refute": "Read retains written value."}, "source_finding_ids": packet.Binding.HypothesisSourceFindingIDs, "evidence_refs": []string{"path:" + rel}}}
	writeInvestigationJSON(t, filepath.Join(source, answerPath), draft)
	accepted, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found {
		t.Fatalf("receipt: %v", err)
	}
	var doc struct {
		Results []struct {
			Refs []string `json:"evidence_refs"`
		} `json:"hypothesis_results"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(fixture.root, receipt.SuccessorCaseRef)), &doc); err != nil {
		t.Fatal(err)
	}
	ref := doc.Results[len(doc.Results)-1].Refs[0]
	if !evidence.MatchesS8PathBinding(packet.ID, "path:"+rel, ref) {
		t.Fatalf("capture lost original path binding: %s", ref)
	}
	if err := evidence.ValidateFindingReference(fixture.root, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, request); err != nil {
		t.Fatalf("same answer retry: %v", err)
	}
	// Reuse the captured source without a Runtime alias, while retaining its
	// original generation, round, file and optional kind constraints.
	if err := investigation.ValidateEvidenceRefs([]string{ref}, investigation.EvidenceAttestationOptions{AllowPaths: true, State: accepted.State, Root: fixture.root, RequireSHA: true, RequireReviewRound: 1}); err != nil {
		t.Fatal(err)
	}
	if err := investigation.ValidateEvidenceRefs([]string{ref}, investigation.EvidenceAttestationOptions{AllowPaths: true, State: accepted.State, Root: fixture.root, RequireSHA: true, RequireReviewRound: 2}); err == nil {
		t.Fatal("path bypassed explicit round")
	}
	if err := investigation.ValidateEvidenceRefs([]string{ref}, investigation.EvidenceAttestationOptions{AllowPaths: true, State: accepted.State, Root: fixture.root, RequireSHA: true, RequireKind: []string{"qa_review"}}); err == nil {
		t.Fatal("path bypassed indexed kind constraint")
	}
	caseDoc := readCaseDocument(t, fixture.root, receipt.SuccessorCaseRef)
	hypotheses := caseDoc["hypotheses"].([]any)
	next := hypotheses[len(hypotheses)-1].(map[string]any)
	nextOwner := s8OwnerFixture{ownerID: "investigator-local-next", hypothesisID: next["hypothesis_id"].(string), assignmentID: next["assignment_id"].(string), taskID: "TASK-842-05", teamID: "workgroup-s8-local-next", dispatchMode: "plan_checkpoint"}
	pointer := investigationPointerFromState(t, accepted.State)
	registerS8Owner(t, fixture, accepted, nextOwner, pointer["path"].(string), pointer["sha256"].(string))
	runS8AgentBegin(t, fixture, nextOwner)
	current := currentS8Snapshot(t, fixture)
	followups, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, current.Revision, nextOwner.ownerID)
	if err != nil || len(followups) != 1 {
		t.Fatalf("followup captured evidence issuance: %v", err)
	}
	foundFile := false
	for _, source := range followups[0].Inputs.EvidenceRefs {
		if source.Ref == ref && source.Attestation == "file_source" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatal("followup lost frozen file source")
	}

	writeInvestigationBytes(t, source, rel, []byte("changed observation\n"))
	if _, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, request); err == nil {
		t.Fatal("signed changed source on replay")
	}
	if err := os.Remove(filepath.Join(source, rel)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, answerPath)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, currentS8Snapshot(t, fixture).State, packet.ID, owner.ownerID); err != nil || !found {
		t.Fatalf("readonly receipt depended on private source: %v", err)
	}
}

// Register historical bytes through the original Writer to model an issuance
// that predates the guidance update. The final restore/submit path is public.
func TestS8HistoricalIssuedTemplateRestoresAndReceivesOriginalAnswer(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	runS8AgentBegin(t, fixture, owner)
	initial := currentS8Snapshot(t, fixture)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, initial.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("bootstrap: %v", err)
	}
	old := packets[0]
	historical, err := schema.ReadAsset("stage-form-s8-template-before-guidance.json")
	if err != nil {
		t.Fatal(err)
	}
	old.TemplateSHA = workpacket.Digest(historical)
	old, err = workpacket.RebuildHypothesisResult(old)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(old)
	var oldRow map[string]any
	_ = json.Unmarshal(encoded, &oldRow)
	snapshot := currentS8Snapshot(t, fixture)
	life := snapshot.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	snapshot, err = runtime.NewWriter(fixture.statePath, fixture.journalPath, fixture.root, semantic.RuntimeCandidateValidator{}).Update(snapshot.Revision, runtime.Mutation{
		EventID: old.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "work_packet_issued", Actor: "orchestrator", IdempotencyKey: "packet:" + old.ID,
		RuntimeID: old.Binding.RuntimeID, BaselineGeneration: old.Binding.BaselineGeneration, From: cursor, To: cursor, RetainLastTransition: true,
		Apply: func(state map[string]any) error {
			entities := state["entities"].(map[string]any)
			rows := entities["work_packets"].([]any)
			for i, raw := range rows {
				if raw.(map[string]any)["packet_id"] == packets[0].ID {
					rows[i] = oldRow
				}
			}
			entities["work_packets"] = rows
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	answerPath := writeS8ProfessionalAnswer(t, fixture.root, old, "Historical guidance answer remains admissible.")
	authoredBeforeRestore := string(mustRead(t, filepath.Join(fixture.root, answerPath)))
	restored, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, snapshot.Revision, owner.ownerID)
	if err != nil || len(restored) != 1 || restored[0].ID != old.ID {
		t.Fatalf("restore old packet: %v", err)
	}
	if string(mustRead(t, filepath.Join(fixture.root, answerPath))) != authoredBeforeRestore {
		t.Fatal("historical restoration replaced authored answer")
	}
	accepted, err := submitS8Original(fixture, snapshot.Revision, old, answerPath, owner.ownerID)
	if err != nil {
		t.Fatalf("receive old answer: %v", err)
	}
	if _, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, old.ID, owner.ownerID); err != nil || !found {
		t.Fatalf("old receipt: %v", err)
	}
}
