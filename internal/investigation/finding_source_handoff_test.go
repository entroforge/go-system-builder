package investigation_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
)

// The synthetic sealed-S7 fixture is established before Case intake. All S8
// registration, issuance, immutable sources and native Stop consumption below
// use the original mechanisms; no source/index alias is registered.
func TestS8TypedFindingSourceHandoffNeedsNoIndexAlias(t *testing.T) {
	content := []byte("SIMULATION_ONLY\n")
	digest := hash(content)
	refs := []string{
		"path:observation.txt#sha256=" + digest,
		fmt.Sprintf("command_output:observation.txt#sha256=%s (bytes=%d, truncated=false)", digest, len(content)),
		"command_output:withheld.txt (withheld: redaction)",
		"artifact:deleted.txt (deleted)",
		"env:SECRET (present; value never captured)",
	}
	f, owners := prepareS8GenerationTwoFixture(t, false, func(f *intakeFixture) {
		writeInvestigationBytes(t, f.root, "observation.txt", content)
		state := map[string]any{}
		if err := json.Unmarshal(mustRead(t, f.statePath), &state); err != nil {
			t.Fatal(err)
		}
		finding := state["entities"].(map[string]any)["findings"].([]any)[0].(map[string]any)
		path := filepath.Join(f.root, finding["path"].(string))
		document := map[string]any{}
		if err := json.Unmarshal(mustRead(t, path), &document); err != nil {
			t.Fatal(err)
		}
		document["evidence_refs"] = refs
		writeInvestigationJSON(t, path, document)
		finding["sha256"] = hash(mustRead(t, path))
		writeInvestigationJSON(t, f.statePath, state)
	})
	owner := owners[0]
	beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	writeInvestigationBytes(t, f.root, "observation.txt", []byte("changed\n"))
	if _, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID); err == nil || !strings.Contains(err.Error(), "original Finding") {
		t.Fatalf("issuance adopted changed Finding bytes: %v", err)
	}
	if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("failed source preparation changed Runtime/journal")
	}
	writeInvestigationBytes(t, f.root, "observation.txt", content)
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original typed Finding source issue: %d %v", len(packets), err)
	}
	p := packets[0]
	count := 0
	for _, source := range p.Inputs.EvidenceRefs {
		if source.Origin == "finding" {
			if source.Attestation != "finding_source" || source.SourceFindingID != "finding-1" || source.Status != "" || source.Kind != "" || source.BaselineGeneration != nil || source.ReviewRound != nil {
				t.Fatalf("Finding source gained fabricated index metadata: %+v", source)
			}
			count++
		}
	}
	if count != len(refs) {
		t.Fatalf("lost original Finding sources: %d", count)
	}
	answerPath := writeS8ProfessionalAnswer(t, f.root, p, "SIMULATION: the original frozen observation supports this discriminator.")
	var answer map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, answerPath)), &answer); err != nil {
		t.Fatal(err)
	}
	answer["answer"].(map[string]any)["evidence_refs"] = []string{refs[0]}
	writeInvestigationJSON(t, filepath.Join(f.root, answerPath), answer)
	beforeState, beforeJournal = mustRead(t, f.statePath), mustRead(t, f.journalPath)
	writeInvestigationBytes(t, f.root, "observation.txt", []byte("changed after issue\n"))
	if _, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: p.ID, OwnerAgentID: owner.ownerID, AnswerPath: answerPath}); err == nil {
		t.Fatal("accepted a changed frozen Finding source")
	}
	if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("failed source consumption changed Runtime/journal")
	}
	writeInvestigationBytes(t, f.root, "observation.txt", content)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": "opaque-native-finding-source", "cwd": f.root, "last_assistant_message": filepath.Join(f.root, answerPath)})
	var output, diagnostic bytes.Buffer
	code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStop"}, bytes.NewReader(body), &output, &diagnostic)
	if code != 0 || !strings.Contains(output.String()+diagnostic.String(), "original consumer") {
		t.Fatalf("native Stop failed: %d %s %s", code, output.String(), diagnostic.String())
	}
	state := currentS8Snapshot(t, f)
	if _, accepted, err := investigation.IssuedHypothesisResultReceipt(f.root, state.State, p.ID, owner.ownerID); err != nil || !accepted {
		t.Fatalf("original receipt: accepted=%t err=%v", accepted, err)
	}
	for _, raw := range state.State["evidence"].([]any) {
		row := raw.(map[string]any)
		for _, ref := range refs {
			if row["id"] == ref {
				t.Fatal("consumer created a Finding source index alias")
			}
		}
	}
	// Unrelated evidence cannot become a Finding source simply by using the
	// same prefix. Only exact references from selected Findings are reusable.
	pointer := investigationPointerFromState(t, state.State)
	_, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{ExpectedRevision: -1, ExpectedCaseRevision: s8TestInt(pointer["revision"]), ExpectedCaseSHA256: pointer["sha256"].(string), CaseID: pointer["case_id"].(string), HypothesisID: "hypothesis-unbound-path", AssignmentID: "assignment-unbound-path", Statement: "a different question", Invariant: "the value survives", Discriminator: "compare a new observation", ExpectedOutcomes: map[string]any{"support": "lost", "refute": "survives"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"path:unrelated.txt#sha256=" + digest}})
	if err == nil {
		t.Fatal("new Hypothesis silently accepted an unrelated unindexed path")
	}
	// The causal route reuses this same original S7 path with its sealed source
	// authority. Neither another capture nor an index alias is needed.
	seedCausalClosure(t, f, investigationPointer(t, f), func(doc map[string]any) error {
		doc["detection_gap"] = map[string]any{"gap_type": "test", "evidence_refs": []any{refs[0]}}
		return nil
	})
	routeRequest := investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "Original S7 evidence establishes the detection gap.", NoCompetingHypothesis: "The accepted boundary search found no other credible mechanism."}
	beforeState, beforeJournal = mustRead(t, f.statePath), mustRead(t, f.journalPath)
	writeInvestigationBytes(t, f.root, "observation.txt", []byte("drift at route\n"))
	if _, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, routeRequest); err == nil {
		t.Fatal("causal route accepted a drifted original S7 source")
	}
	if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("failed route changed Runtime")
	}
	writeInvestigationBytes(t, f.root, "observation.txt", content)
	if _, err := investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, routeRequest); err != nil {
		t.Fatalf("route rejected original S7 source: %v", err)
	}

}

func TestFirstS8QuestionReusesOnlySelectedFrozenFindingSource(t *testing.T) {
	for _, refKind := range []string{"path", "command_output", "env"} {
		t.Run(refKind, func(t *testing.T) {
			f := newIntakeFixture(t, []string{"finding-1", "finding-2"})
			content := []byte("original first observation\n")
			digest := hash(content)
			ref := "path:observation.txt#sha256=" + digest
			if refKind == "command_output" {
				ref = fmt.Sprintf("command_output:observation.txt#sha256=%s (bytes=%d, truncated=false)", digest, len(content))
			}
			if refKind == "env" {
				ref = "env:SECRET (present; value never captured)"
			}
			writeInvestigationBytes(t, f.root, "observation.txt", content)
			var state map[string]any
			if err := json.Unmarshal(mustRead(t, f.statePath), &state); err != nil {
				t.Fatal(err)
			}
			boundREQ, _ := state["bound_req"].(map[string]any)
			if boundREQ == nil {
				boundREQ = map[string]any{}
				state["bound_req"] = boundREQ
			}
			boundREQ["workspace"] = bindS8FixtureGitRoot(t, f.root)
			state["schema_version"] = "1.2.0"
			// registerS8Owner creates generation-two, round-one manifests.
			// Align the initial sealed S7 fixture before any Case or S8 work exists.
			state["baseline"].(map[string]any)["generation"] = 2
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			for _, raw := range state["entities"].(map[string]any)["findings"].([]any) {
				row := raw.(map[string]any)
				if row["finding_id"] != "finding-1" {
					continue
				}
				path := filepath.Join(f.root, row["path"].(string))
				var doc map[string]any
				if err := json.Unmarshal(mustRead(t, path), &doc); err != nil {
					t.Fatal(err)
				}
				doc["evidence_refs"] = []string{ref}
				writeInvestigationJSON(t, path, doc)
				row["sha256"] = hash(mustRead(t, path))
			}
			writeInvestigationJSON(t, f.statePath, state)
			writeObservationBatch(t, f, []string{"finding-1", "finding-2"}, 2)
			setContractLifecycle(t, f)
			current, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: 0, GroupingRationale: "investigate original frozen observations"})
			if err != nil {
				t.Fatal(err)
			}
			pointer := investigationPointerFromState(t, current.State)
			request := investigation.HypothesisRequest{ExpectedRevision: -1, ExpectedCaseRevision: s8TestInt(pointer["revision"]), ExpectedCaseSHA256: pointer["sha256"].(string), CaseID: pointer["case_id"].(string), HypothesisID: "hypothesis-first-source", AssignmentID: "assignment-first-source", Statement: "the boundary loses the value", Invariant: "the value survives", Discriminator: "compare the original boundary observation", ExpectedOutcomes: map[string]any{"support": "lost", "refute": "survives"}, SourceFindingIDs: []string{"finding-2"}, EvidenceRefs: []string{ref}}
			beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			if _, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, request); err == nil {
				t.Fatal("borrowed source from unselected Finding")
			}
			request.SourceFindingIDs = []string{"finding-1"}
			if refKind != "env" {
				writeInvestigationBytes(t, f.root, "observation.txt", []byte("drift"))
				if _, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, request); err == nil {
					t.Fatal("first question accepted drift")
				}
				writeInvestigationBytes(t, f.root, "observation.txt", content)
			}
			if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
				t.Fatal("rejected first question changed runtime/journal")
			}
			current, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, request)
			if err != nil {
				t.Fatalf("first question without prior S8 evidence or index alias: %v", err)
			}
			owner := s8OwnerFixture{hypothesisID: request.HypothesisID, assignmentID: request.AssignmentID, ownerID: "agent-first-source", teamID: "workgroup-first-source", taskID: "TASK-842-01", dispatchMode: "plan_checkpoint"}
			writeInvestigationBytes(t, f.root, ".claude/agents/investigator.md", mustRead(t, filepath.Join("..", "..", "agents", "investigator.md")))
			pointer = investigationPointerFromState(t, current.State)
			current = registerS8Owner(t, f, current, owner, pointer["path"].(string), pointer["sha256"].(string))
			packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("first question issue: %v %d", err, len(packets))
			}
			p := packets[0]
			if p.Inputs.EvidenceRefs[0].Attestation != "finding_source" || p.Inputs.EvidenceRefs[0].Origin != "hypothesis" {
				t.Fatalf("lost original provenance: %+v", p.Inputs.EvidenceRefs[0])
			}
			answerPath := writeS8ProfessionalAnswer(t, f.root, p, "original boundary evidence explains the discrepancy")
			var answer map[string]any
			if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, answerPath)), &answer); err != nil {
				t.Fatal(err)
			}
			a := answer["answer"].(map[string]any)
			a["evidence_refs"] = []string{ref}
			a["new_hypotheses"] = []any{map[string]any{"statement": "a second boundary may cause propagation", "invariant": "the value survives propagation", "discriminator": "compare both boundary values", "expected_outcomes": map[string]any{"support": "values differ", "refute": "values agree"}, "source_finding_ids": []string{"finding-1"}, "evidence_refs": []string{ref}}}
			newQuestion := a["new_hypotheses"].([]any)[0].(map[string]any)
			newQuestion["source_finding_ids"] = []string{"finding-2"}
			writeInvestigationJSON(t, filepath.Join(f.root, answerPath), answer)
			beforeState, beforeJournal = mustRead(t, f.statePath), mustRead(t, f.journalPath)
			if _, err := investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: p.ID, OwnerAgentID: owner.ownerID, AnswerPath: answerPath}); err == nil {
				t.Fatal("follow-up borrowed source from unselected Finding")
			}
			if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
				t.Fatal("rejected follow-up changed runtime/journal")
			}
			newQuestion["source_finding_ids"] = []string{"finding-1"}
			writeInvestigationJSON(t, filepath.Join(f.root, answerPath), answer)
			current, err = investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: p.ID, OwnerAgentID: owner.ownerID, AnswerPath: answerPath})
			if err != nil {
				t.Fatalf("first source answer and follow-up consumption: %v", err)
			}
			for _, raw := range current.State["evidence"].([]any) {
				if raw.(map[string]any)["id"] == ref {
					t.Fatal("source gained index alias")
				}
			}
		})
	}
}
