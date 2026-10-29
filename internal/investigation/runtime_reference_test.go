package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS8OriginalHypothesisConsumesS7RuntimeReferenceWithoutAlias(t *testing.T) {
	f := readyCaseFixture(t, []string{"finding-1"})
	if err := os.WriteFile(filepath.Join(f.root, "observation.json"), []byte(`{"observation":"fixture boundary"}`), 0600); err != nil {
		t.Fatal(err)
	}
	round := 1
	before, err := runtime.RecordEvidence(f.root, f.statePath, f.journalPath, runtime.EvidenceRequest{
		ExpectedRevision: -1, ID: "ev-qa-observation", Kind: "qa_review", Path: "observation.json",
		ProducedBy: []string{"qa-reviewer"}, ReviewRound: &round, Validator: semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := "runtime:ev-qa-observation"
	registered, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{
		ExpectedRevision: -1, CaseID: "investigation-case-observation-batch-r1", HypothesisID: "hypothesis-runtime-reference",
		Statement: "the recorded boundary drops the value", Invariant: "the value survives the boundary",
		Discriminator: "compare the recorded observations", ExpectedOutcomes: map[string]any{"support": "value is dropped", "refute": "value survives"},
		SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{ref}, AssignmentID: "assignment-runtime-reference",
	})
	if err != nil {
		t.Fatalf("original S8 rejected S7 runtime reference: %v", err)
	}
	beforeEvidence, _ := json.Marshal(before.State["evidence"])
	afterEvidence, _ := json.Marshal(registered.State["evidence"])
	if !bytes.Equal(beforeEvidence, afterEvidence) {
		t.Fatalf("S8 rewrote evidence or registered a prefixed alias: before=%s after=%s", beforeEvidence, afterEvidence)
	}
	pointer := investigationPointerFromState(t, registered.State)
	document := readCaseDocument(t, f.root, pointer["path"].(string))
	hypothesis := document["hypotheses"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(hypothesis["evidence_refs"], []any{ref}) {
		t.Fatalf("S7 reference was rewritten: %v", hypothesis["evidence_refs"])
	}
}

func TestExecutionAttestationRejectsRuntimeReferenceWithSchemeInID(t *testing.T) {
	for _, ref := range []string{"runtime:ev-observation", "runtime:test://unregistered"} {
		if err := investigation.ValidateEvidenceRefs([]string{ref}, investigation.EvidenceAttestationOptions{RequireExecution: true}); err == nil {
			t.Fatalf("runtime reference became execution evidence: %s", ref)
		}
	}
	if err := investigation.ValidateEvidenceRefs([]string{"test://actual-run"}, investigation.EvidenceAttestationOptions{RequireExecution: true}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeS8StopConsumesRuntimeReferenceAndReplaysOriginalReceipt(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	writeInvestigationBytes(t, f.root, "observation.json", []byte(`{"observation":"fixture boundary"}`))
	round := 1
	current, err := runtime.RecordEvidence(f.root, f.statePath, f.journalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: "ev-native-observation", Kind: "qa_review", Path: "observation.json", ProducedBy: []string{owner.ownerID}, ReviewRound: &round, Validator: semantic.RuntimeCandidateValidator{}})
	if err != nil {
		t.Fatal(err)
	}
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, current.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original question preparation: %d %v", len(packets), err)
	}
	p := packets[0]
	path := writeS8ProfessionalAnswer(t, f.root, p, "SIMULATION fixture observation for the registered discriminator.")
	var draft map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, path)), &draft); err != nil {
		t.Fatal(err)
	}
	draft["answer"].(map[string]any)["evidence_refs"] = []string{"runtime:ev-native-observation"}
	writeInvestigationJSON(t, filepath.Join(f.root, path), draft)
	stop := func(actor string) (int, string) {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": actor, "agent_type": "investigator", "session_id": "native-reference-parent", "cwd": f.root, "last_assistant_message": filepath.Join(f.root, path)})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic)
		return code, out.String() + diagnostic.String()
	}
	if code, message := stop("opaque-native-reference-actor"); code != 0 || !strings.Contains(message, "original consumer") {
		t.Fatalf("original native Stop rejected typed reference: %d %s", code, message)
	}
	accepted := currentS8Snapshot(t, f)
	if _, found, err := investigation.IssuedHypothesisResultReceipt(f.root, accepted.State, p.ID, owner.ownerID); err != nil || !found {
		t.Fatalf("original receipt missing: %t %v", found, err)
	}
	for _, raw := range accepted.State["evidence"].([]any) {
		if raw.(map[string]any)["id"] == "runtime:ev-native-observation" {
			t.Fatal("Stop registered an alias")
		}
	}
	beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	if code, message := stop("opaque-actor-after-native-resume"); code != 0 {
		t.Fatalf("original receipt replay failed: %d %s", code, message)
	}
	if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("same delivery after native resume wrote another result")
	}
}

func TestS8QuestionPreparationAndNativeStopUseOriginalRuntimeReference(t *testing.T) {
	f, _ := prepareS8GenerationTwoFixture(t, false)
	writeInvestigationBytes(t, f.root, "question-observation.json", []byte(`{"observation":"source-boundary fixture"}`))
	round := 1
	current, err := runtime.RecordEvidence(f.root, f.statePath, f.journalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: "ev-question-source", Kind: "qa_review", Path: "question-observation.json", ProducedBy: []string{"qa-reviewer"}, ReviewRound: &round, Validator: semantic.RuntimeCandidateValidator{}})
	if err != nil {
		t.Fatal(err)
	}
	pointer := investigationPointerFromState(t, current.State)
	owner := s8OwnerFixture{hypothesisID: "hypothesis-runtime-question", assignmentID: "assignment-runtime-question", ownerID: "investigator-runtime-question", teamID: "workgroup-runtime-question", taskID: "TASK-842-99", dispatchMode: "plan_checkpoint"}
	current, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{ExpectedRevision: current.Revision, ExpectedCaseRevision: s8TestInt(pointer["revision"]), ExpectedCaseSHA256: pointer["sha256"].(string), CaseID: pointer["case_id"].(string), HypothesisID: owner.hypothesisID, AssignmentID: owner.assignmentID, Statement: "the observed value is lost at the decoder boundary", Invariant: "the required value survives the boundary", Discriminator: "compare the registered observation with the boundary output", ExpectedOutcomes: map[string]any{"support": "the value is lost", "refute": "the value survives"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"runtime:ev-question-source"}})
	if err != nil {
		t.Fatal(err)
	}
	pointer = investigationPointerFromState(t, current.State)
	current = registerS8Owner(t, f, current, owner, pointer["path"].(string), pointer["sha256"].(string))
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, current.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original question preparation rejected the valid reference: packets=%d err=%v", len(packets), err)
	}
	p := packets[0]
	found := false
	for _, ref := range p.Inputs.EvidenceRefs {
		if ref.Ref == "runtime:ev-question-source" {
			found = ref.Attestation == "runtime_index" && ref.Path == "question-observation.json" && ref.SHA256 != ""
		}
	}
	if !found {
		t.Fatal("prepared question lost original reference/provenance")
	}
	path := writeS8ProfessionalAnswer(t, f.root, p, "SIMULATION: the registered observation supports the actual boundary discriminator.")
	stop := func(actor string) (int, string) {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": actor, "cwd": f.root, "last_assistant_message": filepath.Join(f.root, path)})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic)
		return code, out.String() + diagnostic.String()
	}
	if code, output := stop("opaque-question-source-actor"); code != 0 || !strings.Contains(output, "original consumer") {
		t.Fatalf("native Stop: %d %s", code, output)
	}
	accepted := currentS8Snapshot(t, f)
	if _, ok, err := investigation.IssuedHypothesisResultReceipt(f.root, accepted.State, p.ID, owner.ownerID); err != nil || !ok {
		t.Fatalf("original receipt: %t %v", ok, err)
	}
	for _, raw := range accepted.State["evidence"].([]any) {
		if raw.(map[string]any)["id"] == "runtime:ev-question-source" {
			t.Fatal("receiver registered a prefixed alias")
		}
	}
	beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	if code, output := stop("opaque-question-source-resumed"); code != 0 {
		t.Fatalf("native replay: %d %s", code, output)
	}
	if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("native receipt replay changed Runtime")
	}
}
