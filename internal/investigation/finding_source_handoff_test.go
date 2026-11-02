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
		writeInvestigationJSON(t, f.statePath, state)
	})
	owner := owners[0]
	writeInvestigationBytes(t, f.root, "observation.txt", []byte("changed\n"))
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original typed Finding source issue: %d %v", len(packets), err)
	}
	p := packets[0]
	count := 0
	for _, source := range p.Inputs.EvidenceRefs {
		if source.Origin == "finding" {
			if source.Attestation != "reference" || source.SourceFindingID != "finding-1" || source.Status != "" || source.Kind != "" || source.BaselineGeneration != nil {
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
	writeInvestigationBytes(t, f.root, "observation.txt", []byte("changed after issue\n"))
	actor := "opaque-native-finding-source"
	transcript := s8NativeTaskTranscript(t, actor, "Investigate original "+filepath.Join(f.root, p.ViewPath))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": actor, "agent_transcript_path": transcript, "cwd": f.root, "last_assistant_message": filepath.Join(f.root, answerPath)})
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
	// A new professional question may cite an unindexed path without turning
	// that path into a machine-authenticated Finding or an extra evidence row.
	pointer := investigationPointerFromState(t, state.State)
	_, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{ExpectedRevision: -1, ExpectedCaseRevision: s8TestInt(pointer["revision"]), CaseID: pointer["case_id"].(string), HypothesisID: "hypothesis-unbound-path", AssignmentID: "assignment-unbound-path", Statement: "a different question", Invariant: "the value survives", Discriminator: "compare a new observation", ExpectedOutcomes: map[string]any{"support": "lost", "refute": "survives"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"path:unrelated.txt#sha256=" + digest}})
	if err != nil {
		t.Fatalf("new professional question was blocked by an evidence index: %v", err)
	}
	// The causal route reuses this same original S7 path with its sealed source
	// authority. Neither another capture nor an index alias is needed.
	seedCausalClosure(t, f, investigationPointer(t, f), func(doc map[string]any) error {
		doc["detection_gap"] = map[string]any{"gap_type": "test", "evidence_refs": []any{refs[0]}}
		return nil
	})
	routeRequest := investigation.RouteRequest{ExpectedRevision: -1, CaseID: currentResultsCase, Route: "s9_repair", RouteReason: "Original S7 evidence establishes the detection gap.", NoCompetingHypothesis: "The accepted boundary search found no other credible mechanism."}
	writeInvestigationBytes(t, f.root, "observation.txt", []byte("drift at route\n"))
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
			setFixtureAcceptedFindings(t, f, []string{"finding-1", "finding-2"}, 2)
			setContractLifecycle(t, f)
			current, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: 0, GroupingRationale: "investigate original frozen observations"})
			if err != nil {
				t.Fatal(err)
			}
			pointer := investigationPointerFromState(t, current.State)
			request := investigation.HypothesisRequest{ExpectedRevision: -1, ExpectedCaseRevision: s8TestInt(pointer["revision"]), CaseID: pointer["case_id"].(string), HypothesisID: "hypothesis-first-source", AssignmentID: "assignment-first-source", Statement: "the boundary loses the value", Invariant: "the value survives", Discriminator: "compare the original boundary observation", ExpectedOutcomes: map[string]any{"support": "lost", "refute": "survives"}, SourceFindingIDs: []string{"finding-2"}, EvidenceRefs: []string{ref}}
			// A Finding that was never received stays outside the Case's source
			// set, so a question may not name it. This source relation is saved
			// with the original registration (L3-S8:227) and is still enforced.
			beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			unreceived := request
			unreceived.HypothesisID = "hypothesis-unreceived-source"
			unreceived.AssignmentID = "assignment-unreceived-source"
			unreceived.SourceFindingIDs = []string{"finding-unreceived"}
			if _, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, unreceived); err == nil {
				t.Fatal("question named a Finding that was never received")
			}
			if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
				t.Fatal("rejected first question changed runtime/journal")
			}
			// L3-S8:227 keeps the source relation as the registration saved with
			// the original record and does not extend it into platform identity
			// authentication. The machine therefore does not restrict the
			// question's evidence to the Findings the question declares, and
			// L3-S8:243/L3-S8:245 keep the original Finding's frozen reference
			// and its original semantics.
			current, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, request)
			if err != nil {
				t.Fatalf("first question reusing an original frozen Finding source: %v", err)
			}
			// L3-S8:43 removes hash/SHA computation, registration and
			// verification from the Hook and its consumers, and L3-S8:249 states
			// that Main need not prove every material matches the current
			// template before creating the first question. A changed byte count
			// of the cited observation is therefore no longer a gate.
			if refKind != "env" {
				writeInvestigationBytes(t, f.root, "observation.txt", []byte("drift"))
				pointer = investigationPointerFromState(t, current.State)
				drifted := request
				drifted.HypothesisID = "hypothesis-first-source-drift"
				drifted.AssignmentID = "assignment-first-source-drift"
				drifted.ExpectedCaseRevision = s8TestInt(pointer["revision"])
				if _, err := investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, drifted); err != nil {
					t.Fatalf("first question refused over a byte-level change: %v", err)
				}
				writeInvestigationBytes(t, f.root, "observation.txt", content)
			}
			owner := s8OwnerFixture{hypothesisID: request.HypothesisID, assignmentID: request.AssignmentID, ownerID: "agent-first-source", teamID: "workgroup-first-source", taskID: "TASK-842-01", dispatchMode: "plan_checkpoint"}
			writeInvestigationBytes(t, f.root, ".claude/agents/investigator.md", mustRead(t, filepath.Join("..", "..", "agents", "investigator.md")))
			pointer = investigationPointerFromState(t, current.State)
			current = registerS8Owner(t, f, currentS8Snapshot(t, f), owner, pointer["path"].(string))
			packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, owner.ownerID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("first question issue: %v %d", err, len(packets))
			}
			p := packets[0]
			// The original frozen Finding source keeps its original provenance
			// on the issued question (L3-S8:245: the original Finding frozen
			// reference keeps its original semantics).
			foundProvenance := false
			for _, item := range p.Inputs.EvidenceRefs {
				if item.Origin == "finding" && item.SourceFindingID == "finding-1" && item.Ref == ref {
					foundProvenance = true
				}
			}
			if !foundProvenance {
				t.Fatalf("lost original provenance: %+v", p.Inputs.EvidenceRefs)
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
			// L3-S8:162: an Investigator's `new_hypotheses` entry is only a
			// suggestion that the system preserves as authored. The suggestion's
			// own source list is the author's professional content, so the
			// machine does not reject it for naming a different Finding, and it
			// does not turn it into a new responsibility either.
			newQuestion["source_finding_ids"] = []string{"finding-2"}
			writeInvestigationJSON(t, filepath.Join(f.root, answerPath), answer)
			current, err = investigation.SubmitIssuedHypothesisResult(f.root, f.statePath, f.journalPath, investigation.IssuedHypothesisResultRequest{ExpectedRevision: -1, PacketID: p.ID, OwnerAgentID: owner.ownerID, AnswerPath: answerPath})
			if err != nil {
				t.Fatalf("authored follow-up suggestion was refused: %v", err)
			}
			document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
			rows := document["hypothesis_results"].([]any)
			suggestions, _ := rows[len(rows)-1].(map[string]any)["new_hypotheses"].([]any)
			if len(suggestions) != 1 {
				t.Fatalf("authored follow-up suggestion was not preserved: %v", rows[len(rows)-1])
			}
			suggested, _ := suggestions[0].(map[string]any)["source_finding_ids"].([]any)
			if len(suggested) != 1 || suggested[0] != "finding-2" {
				t.Fatalf("authored follow-up source list was rewritten: %v", suggested)
			}
			for _, raw := range document["hypotheses"].([]any) {
				if raw.(map[string]any)["statement"] == "a second boundary may cause propagation" {
					t.Fatal("suggested follow-up was registered as a new Hypothesis responsibility")
				}
			}
			for _, raw := range current.State["evidence"].([]any) {
				if raw.(map[string]any)["id"] == ref {
					t.Fatal("source gained index alias")
				}
			}
		})
	}
}
