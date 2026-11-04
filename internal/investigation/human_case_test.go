package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Upstream review/release state is synthetic; TR-027, its decision bytes and
// consumption, subsequent Case revisions and receivers are the original APIs.
func humanDefectFixture(t *testing.T) (*intakeFixture, runtime.Snapshot) {
	t.Helper()
	f := newIntakeFixture(t, nil)
	var state map[string]any
	if err := json.Unmarshal(mustRead(t, f.statePath), &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["baseline"].(map[string]any)["generation"] = 2
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["bound_req"].(map[string]any)["workspace"] = bindS8FixtureGitRoot(t, f.root)
	product := "internal/service/decoder.go"
	// Install the repository's actual runtime ignore policy, rather than
	// treating later Task/packet authoring files as new product files.
	writeInvestigationBytes(t, f.root, ".gitignore", mustRead(t, filepath.Join("..", "..", ".gitignore")))
	writeInvestigationBytes(t, f.root, product, []byte("package service\n\nconst DecodeDropsValue = true\n"))
	for _, args := range [][]string{{"add", product, ".gitignore"}, {"commit", "-qm", "Synthetic human-case baseline"}} {
		cmd := exec.Command("git", append([]string{"-C", f.root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture Git: %v %s", err, output)
		}
	}
	plan := review.Plan{SchemaVersion: "1.0.0", ReviewPlanID: "review-plan-human-case", BaselineGeneration: 2,
		FrozenSubjects: []review.FrozenSubject{{Path: product}}}
	planBytes, _ := json.Marshal(plan)
	planRef := ".claude/review/plans/review-plan-human-case.json"
	writeInvestigationBytes(t, f.root, planRef, planBytes)
	state["review"].(map[string]any)["plan"] = map[string]any{"plan_id": plan.ReviewPlanID, "path": planRef, "sha256": hash(planBytes), "revision": 1, "review_round": 1, "status": "clean", "e2e_coverage_state": "not_applicable", "submitted_at": "2026-10-21T00:00:00Z"}
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": 1}
	writeInvestigationJSON(t, f.statePath, state)
	decision, err := transition.Apply(f.root, f.statePath, f.journalPath, transition.Request{TransitionID: "TR-027", ExpectedRevision: -1, Actor: "orchestrator", Params: map[string]any{
		"decision_id": "hd-human-defect", "decision_path": ".claude/decisions/hd-human-defect.json", "disposition": "reject_defect", "reason": "SIMULATION: the decoder drops a required field despite the earlier review; investigate this original report."}})
	if err != nil {
		t.Fatalf("actual TR-027 decision/consumption: %v", err)
	}
	// Even a tracked control decision must not become a product delta. Its
	// exact Runtime record, rather than the directory name, grants that scope.
	if output, err := exec.Command("git", "-C", f.root, "add", "-f", ".claude/decisions/hd-human-defect.json").CombinedOutput(); err != nil {
		t.Fatalf("track the original control source: %v %s", err, output)
	}
	return f, decision
}

// writeS8ActorTranscript writes the actor's own child transcript for a
// natural platform event. L4-hook-platform-wiring.md:81 states that S7/S8/S9
// read the person's own original volume through the already issued path and
// the existing responsibility association, and that the actual product
// location and input applicability belong to Main and the executor.
func writeS8ActorTranscript(t *testing.T, root, ownerID, answerPath string) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "parent-session.jsonl")
	child := filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents", "agent-"+ownerID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(child), 0700); err != nil {
		t.Fatal(err)
	}
	row, _ := json.Marshal(map[string]any{"type": "user", "isSidechain": true, "agentId": ownerID, "message": map[string]any{"role": "user", "content": "Answer " + answerPath}})
	if err := os.WriteFile(child, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte(`{"type":"user","message":{"role":"user","content":"Main's own unrelated conversation"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return parent
}

func humanRoutedContractFixture(t *testing.T) (*intakeFixture, runtime.Snapshot, string) {
	t.Helper()
	f, _ := humanDefectFixture(t)
	current, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "Investigate the original human report without inventing a review Finding."})
	if err != nil {
		t.Fatal(err)
	}
	pointer := investigationPointerFromState(t, current.State)
	caseID := pointer["case_id"].(string)
	initial := readCaseDocument(t, f.root, pointer["path"].(string))
	if len(initial["source_finding_ids"].([]any)) != 0 || initial["observation_batch_id"] != nil {
		t.Fatal("human intake invented a Finding or Batch")
	}
	if initial["baseline_generation"] != float64(2) {
		t.Fatal("human intake lost the current product generation")
	}
	current, err = investigation.UpdateCase(f.root, f.statePath, f.journalPath, investigation.CaseRevisionRequest{ExpectedRevision: -1, CaseID: caseID, Operation: "boundary_observed", Mutate: func(document map[string]any) error {
		document["failure_boundary_refs"] = []string{"test://decoder-boundary-observation"}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeInvestigationBytes(t, f.root, ".claude/agents/investigator.md", mustRead(t, filepath.Join("..", "..", "agents", "investigator.md")))
	owner := s8OwnerFixture{hypothesisID: "hypothesis-human-decoder", assignmentID: "assignment-human-decoder", ownerID: "investigator-human-decoder", teamID: "workgroup-human-decoder", taskID: "TASK-842-90", dispatchMode: "plan_checkpoint"}
	current, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{ExpectedRevision: -1, CaseID: caseID,
		HypothesisID: owner.hypothesisID, AssignmentID: owner.assignmentID, Statement: "the decoder omits the required input field", Invariant: "the required input field survives decoding", Discriminator: "compare the original human report and the decoder field mapping",
		ExpectedOutcomes: map[string]any{"support": "the field is absent from the decoder mapping", "refute": "the decoder preserves the field"}, SourceFindingIDs: []string{}, EvidenceRefs: []string{".claude/decisions/hd-human-defect.json"}})
	if err != nil {
		t.Fatal(err)
	}
	pointer = investigationPointerFromState(t, current.State)
	current = registerS8Owner(t, f, current, owner, pointer["path"].(string))
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, current.Revision, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("human source question: %d %v", len(packets), err)
	}
	p := packets[0]
	if len(p.Inputs.Findings) != 0 {
		t.Fatal("question fabricated a Batch/Finding")
	}
	foundReport := false
	for _, item := range p.Draft.Read {
		if item.Path == ".claude/decisions/hd-human-defect.json" {
			foundReport = true
		}
	}
	if !foundReport {
		t.Fatal("original human report was not supplied to the Investigator")
	}
	answer := workpacket.HypothesisResultAnswer{Method: "SIMULATION: inspect the actual decoder and original human report.", EvidenceRefs: []string{".claude/decisions/hd-human-defect.json"}, SourceBoundaryRefs: []string{"hd-human-defect"},
		Observed: "SIMULATION: the seeded decoder drops the required value.", CounterfactualOrDiscriminator: "A source-aligned decoder retains the reported field.", ExplainsFindingIDs: []string{}, DoesNotExplain: []string{}, NewHypotheses: []workpacket.HypothesisNewQuestion{}}
	supported := "supported"
	answer.Result = &supported
	p.Draft.Answer, _ = json.Marshal(answer)
	writeInvestigationJSON(t, filepath.Join(f.root, p.AnswerPath), p.Draft)
	// L3-S8:227 and L4-hook-platform-wiring.md:122 require the natural
	// PreToolUse / successful PostToolUse event to read the current original
	// answer and call the same original HypothesisResult consumer. The
	// diagnostic wording is not the contract, so delivery is proved by the
	// original receipt rather than by matching a message string.
	callStop := func() (int, string) {
		body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "agent_id": owner.ownerID, "agent_type": "investigator", "session_id": "native-human-parent", "cwd": f.root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(f.root, p.AnswerPath)}, "transcript_path": writeS8ActorTranscript(t, f.root, owner.ownerID, p.ViewPath)})
		var out, diagnostic bytes.Buffer
		code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic)
		return code, out.String() + diagnostic.String()
	}
	stop := func() {
		code, output := callStop()
		if code != 0 {
			t.Fatalf("native human-source return: %d %s", code, output)
		}
	}

	stop()
	current = currentS8Snapshot(t, f)
	if _, ok, err := investigation.IssuedHypothesisResultReceipt(f.root, current.State, p.ID, owner.ownerID); err != nil || !ok {
		t.Fatalf("original Case receipt: %t %v", ok, err)
	}
	beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	stop()
	if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("native replay wrote another result")
	}
	current, err = investigation.UpdateCaseRoute(f.root, f.statePath, f.journalPath, investigation.RouteRequest{ExpectedRevision: -1, CaseID: caseID, Route: "s9_repair", RouteReason: "repair the demonstrated decoder omission", PrimaryRootCause: "the field mapping omits a required input", NoCompetingHypothesis: "the seeded input observation and mapping exclude the alternate transport explanation",
		CausalModel: map[string]any{"trigger": "decode the reported input", "violated_invariant": "required fields survive", "faulty_mechanism": "field omission", "propagation": "missing value reaches caller", "symptoms": []string{"the human-reported value is absent"}}, BlastRadius: map[string]any{"paths": []string{"internal/service/decoder.go"}}, DetectionGap: map[string]any{"gap_type": "test", "evidence_refs": []string{"test://human-decoder-observation"}}})
	if err != nil {
		t.Fatal(err)
	}
	pointer = investigationPointerFromState(t, current.State)
	contractPath := writeContractDraft(t, f.root, []string{})
	finalDraftPath := filepath.Join(f.root, ".claude/review/investigation/contracts/human-decoder.json")
	if err := os.MkdirAll(filepath.Dir(finalDraftPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(contractPath, finalDraftPath); err != nil {
		t.Fatal(err)
	}
	contractPath = finalDraftPath
	var contract map[string]any
	if err := json.Unmarshal(mustRead(t, contractPath), &contract); err != nil {
		t.Fatal(err)
	}
	contract["case_id"], contract["revision"] = caseID, pointer["revision"]
	contract["causal_model_ref"] = "case://" + caseID + "/causal-model"
	contract["root_cause_statement"] = "the decoder mapping omits a required input field"
	contract["violated_invariant"] = "required input fields survive the decoder boundary"
	contract["architecture_intent"] = "the decoder preserves the required fields of valid input"
	contract["prospective_scope"] = []string{"internal/service/decoder.go"}
	contract["symptom_assertions"] = []string{"the original human-reported input retains its field"}
	contract["root_invariant_assertions"] = []string{"required decoder fields survive the boundary"}
	contract["detection_gap_assertions"] = []string{"a decoder regression test fails when a required field is omitted"}
	contract["repair_units"] = []any{map[string]any{"id": "repair-unit-human-decoder", "description": "restore the required field mapping", "scope": []string{"internal/service/decoder.go"}, "assertion_ids": []string{"all"}}}
	writeInvestigationJSON(t, contractPath, contract)
	return f, current, contractPath
}

func TestHumanDefectCaseNativeInvestigationAndApprovedRepairWithoutFindingOrBatch(t *testing.T) {
	f, current, contractPath := humanRoutedContractFixture(t)
	caseID := investigationPointerFromState(t, current.State)["case_id"].(string)

	if _, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, investigation.ContractRequest{ExpectedRevision: -1, CaseID: caseID, ContractPath: contractPath, ApprovedBy: "main-session"}); err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("missing approval was bypassed or masked: %v", err)
	}
	_, approvalID, _ := registerContractApprovalEvidence(t, f, contractPath)
	current, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, investigation.ContractRequest{ExpectedRevision: -1, CaseID: caseID, ContractPath: contractPath, ApprovedBy: "main-session", ApprovalEvidenceID: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	_, session, _, err := repair.OpenRepairSession(f.root, f.statePath, f.journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, SessionID: "repair-session-human-decoder", CreatedBy: "Main", ReqID: "REQ-002"})
	if err != nil {
		t.Fatalf("S9 session rejected the original approved human Case: %v", err)
	}
	_, plan, _, err := repair.CompileRepairPlan(f.root, f.statePath, f.journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, PlanID: "repair-plan-human-decoder", CreatedBy: "Main"})
	if err != nil || len(plan.Assignments) == 0 || session.ContractRef != investigationPointerFromState(t, current.State)["repair_contract_ref"] {
		t.Fatalf("original S9 preparation: %+v %v", plan, err)
	}
	final := currentS8Snapshot(t, f)
	rawFindings := final.State["entities"].(map[string]any)["findings"]
	findings, ok := rawFindings.([]any)
	if rawFindings != nil && !ok {
		t.Fatalf("unexpected Finding registry: %T", rawFindings)
	}
	if final.State["review"].(map[string]any)["observation_batch"] != nil || len(findings) != 0 {
		t.Fatal("human path fabricated source artifacts")
	}
	if _, err := os.Stat(filepath.Join(f.root, ".claude/evidence/findings/finding-1.json")); !os.IsNotExist(err) {
		t.Fatal("a fake Finding file was created")
	}
}

func TestHumanDefectIntakeRejectsChangedSourceOrBaselineWithoutWritingCase(t *testing.T) {
	for _, fault := range []string{"missing_source", "unconsumed", "wrong_consumer", "generation", "round", "invalidated", "bytes", "runtime", "decision_id", "scope", "missing_plan", "plan_bytes", "plan_generation", "plan_round", "undeclared_product", "undeclared_decision"} {
		t.Run(fault, func(t *testing.T) {
			f, _ := humanDefectFixture(t)
			var state map[string]any
			if err := json.Unmarshal(mustRead(t, f.statePath), &state); err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			for _, raw := range state["evidence"].([]any) {
				candidate := raw.(map[string]any)
				if candidate["id"] == "hd-human-defect" {
					row = candidate
				}
			}
			if row == nil {
				t.Fatal("real TR027 source absent")
			}
			switch fault {
			case "missing_source":
				state["evidence"] = []any{}
			case "unconsumed":
				delete(row, "consumed_by")
			case "wrong_consumer":
				row["consumed_by"] = "TR-028"
			case "generation":
				row["baseline_generation"] = 1
			case "round":
				row["review_round"] = 2
			case "invalidated":
				row["invalidated_by"] = "original-source-invalidated"
			case "bytes":
				writeInvestigationBytes(t, f.root, row["path"].(string), []byte("source changed"))
			case "runtime", "decision_id", "scope":
				path := row["path"].(string)
				var value map[string]any
				if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, path)), &value); err != nil {
					t.Fatal(err)
				}
				key := map[string]string{"runtime": "runtime_id", "decision_id": "decision_id", "scope": "scope"}[fault]
				value[key] = "foreign-source"
				data, _ := json.Marshal(value)
				writeInvestigationBytes(t, f.root, path, data)
				row["sha256"] = hash(data)
			case "missing_plan":
				delete(state["review"].(map[string]any), "plan")
			case "plan_bytes":
				writeInvestigationBytes(t, f.root, ".claude/review/plans/review-plan-human-case.json", []byte("changed plan"))
			case "undeclared_product":
				writeInvestigationBytes(t, f.root, "internal/service/unreviewed.go", []byte("package service\n"))
			case "undeclared_decision":
				writeInvestigationBytes(t, f.root, ".claude/decisions/unregistered.json", []byte("{}"))
				if output, err := exec.Command("git", "-C", f.root, "add", "-f", ".claude/decisions/unregistered.json").CombinedOutput(); err != nil {
					t.Fatalf("track an unregistered sibling: %v %s", err, output)
				}
			case "plan_generation", "plan_round":
				pointer := state["review"].(map[string]any)["plan"].(map[string]any)
				path := pointer["path"].(string)
				var value map[string]any
				if err := json.Unmarshal(mustRead(t, filepath.Join(f.root, path)), &value); err != nil {
					t.Fatal(err)
				}
				if fault == "plan_generation" {
					value["baseline_generation"] = 1
				} else {
					value["review_round"] = 2
				}
				data, _ := json.Marshal(value)
				writeInvestigationBytes(t, f.root, path, data)
				pointer["sha256"] = hash(data)
			}
			writeInvestigationJSON(t, f.statePath, state)
			beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			_, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "the original human report"})
			switch fault {
			case "round", "missing_plan", "plan_bytes", "plan_generation", "plan_round", "undeclared_product", "undeclared_decision":
				// L3-S9:15 and L4-workflow-convergence.md:285: the original
				// consumer does not compute, register or verify any digest, and
				// an old material whose hash is missing, empty, malformed or
				// inconsistent with the current file produces no eligibility
				// difference. These material comparisons are therefore no longer
				// intake gates; the intake still has to happen for real.
				if err != nil {
					t.Fatalf("intake was refused over a removed material comparison: %v", err)
				}
				cases, globErr := filepath.Glob(filepath.Join(f.root, ".claude/review/investigation/cases/*.json"))
				if globErr != nil || len(cases) == 0 {
					t.Fatalf("intake wrote no Case: %v %v", cases, globErr)
				}
				if bytes.Equal(beforeState, mustRead(t, f.statePath)) {
					t.Fatal("accepted intake wrote no Runtime fact")
				}
				return
			}
			// The real original source still has to exist and still has to be
			// the decision this Runtime actually recorded (L3-S8:231, L4-state-
			// transition-core.md:13.1). A missing, unreadable or foreign
			// source is refused, and a refused intake must not write a Case.
			if err == nil {
				t.Fatal("changed source was accepted")
			}
			if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
				t.Fatal("failed intake changed Runtime/journal")
			}
			cases, err := filepath.Glob(filepath.Join(f.root, ".claude/review/investigation/cases/*.json"))
			if err != nil || len(cases) != 0 {
				t.Fatalf("failed intake left an unusable Case: %v %v", cases, err)
			}
		})
	}
}

// TestHumanCaseOriginalRevisionCannotBeReplacedByCurrentProfessionalBoundary
// proves that a current professional boundary does not take over the original
// human Case identity. L3-S9:15 keeps an existing record's hash value as a
// historical original field only, and L4-workflow-convergence.md:285 states
// that a changed original material produces no eligibility difference and
// that the historical original record is not rewritten for compatibility.
// The byte-level comparison is therefore no longer a gate, but the original
// revision is still read under its own original identity: the current
// professional boundary must not become the original human Case's source.
func TestHumanCaseOriginalRevisionCannotBeReplacedByCurrentProfessionalBoundary(t *testing.T) {
	f, current := humanDefectFixture(t)
	current, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: -1, GroupingRationale: "original human source"})
	if err != nil {
		t.Fatal(err)
	}
	pointer := investigationPointerFromState(t, current.State)
	originalPath := pointer["path"].(string)
	caseID := pointer["case_id"].(string)
	current, err = investigation.UpdateCase(f.root, f.statePath, f.journalPath, investigation.CaseRevisionRequest{ExpectedRevision: -1, CaseID: caseID, Operation: "boundary_observed", Mutate: func(document map[string]any) error {
		document["failure_boundary_refs"] = []string{"test://later-observation"}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeInvestigationBytes(t, f.root, originalPath, []byte("changed original revision"))
	beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	_, err = investigation.RegisterHypothesis(f.root, f.statePath, f.journalPath, investigation.HypothesisRequest{ExpectedRevision: -1, CaseID: caseID, HypothesisID: "hypothesis-history-source", AssignmentID: "assignment-history-source", Statement: "a boundary drops the field", Invariant: "fields survive", Discriminator: "compare input and output", ExpectedOutcomes: map[string]any{"support": "field lost", "refute": "field retained"}, SourceFindingIDs: []string{}, EvidenceRefs: []string{"hd-human-defect"}})
	if err != nil {
		t.Fatalf("changed original revision blocked the current question: %v", err)
	}
	if bytes.Equal(beforeState, mustRead(t, f.statePath)) || bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
		t.Fatal("accepted current question wrote no Runtime or journal fact")
	}
	// The historical original revision keeps its own identity: the current
	// professional boundary did not become the human Case's original source.
	document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
	if document["failure_boundary_refs"] == nil {
		t.Fatal("current professional boundary was lost")
	}
	history := document["revision_history"].([]any)
	if len(history) == 0 {
		t.Fatal("original revision history was lost")
	}
	if s8TestString(history[0].(map[string]any)["path"]) != originalPath {
		t.Fatalf("current boundary replaced the original revision identity: %v", history[0])
	}
}

// TestHumanCaseContractApprovalRechecksOriginalSourceAndProductBaseline
// proves what the contract approval still rechecks after the blueprint
// removed the material-hash gates. L3-S9:15 keeps an existing record's hash
// value as a historical original field only, L4-workflow-convergence.md:285
// states that changed or missing original material produces no eligibility
// difference, and L3-S8:43 removes hash/SHA computation and verification from
// the Hook and its consumers. A changed original decision, a changed original
// Case revision, changed product bytes and a changed current generation are
// therefore no longer approval gates.
//
// The real approval itself is still required and is still bound: an approval
// whose evidence is not scoped to this contract approval remains refused, and
// a refused approval must not write Runtime or journal facts.
func TestHumanCaseContractApprovalRechecksOriginalSourceAndProductBaseline(t *testing.T) {
	for _, fault := range []string{"decision_bytes", "initial_revision", "product_bytes", "current_generation", "wrong_approval_scope"} {
		t.Run(fault, func(t *testing.T) {
			f, current, path := humanRoutedContractFixture(t)
			caseID := investigationPointerFromState(t, current.State)["case_id"].(string)
			_, approvalID, _ := registerContractApprovalEvidence(t, f, path)
			current = currentS8Snapshot(t, f)
			switch fault {
			case "decision_bytes":
				writeInvestigationBytes(t, f.root, ".claude/decisions/hd-human-defect.json", []byte("changed decision"))
			case "initial_revision":
				pointer := investigationPointerFromState(t, current.State)
				document := readCaseDocument(t, f.root, pointer["path"].(string))
				first := document["revision_history"].([]any)[0].(map[string]any)
				writeInvestigationBytes(t, f.root, first["path"].(string), []byte("changed initial Case"))
			case "product_bytes":
				writeInvestigationBytes(t, f.root, "internal/service/decoder.go", []byte("package service\nconst Different = true\n"))
			case "current_generation":
				current.State["baseline"].(map[string]any)["generation"] = 3
				writeInvestigationJSON(t, f.statePath, current.State)
			case "wrong_approval_scope":
				for _, raw := range current.State["evidence"].([]any) {
					row := raw.(map[string]any)
					if row["id"] == approvalID {
						row["scope_refs"] = []string{"foreign:scope"}
					}
				}
				writeInvestigationJSON(t, f.statePath, current.State)
			}
			beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			_, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, investigation.ContractRequest{ExpectedRevision: -1, CaseID: caseID, ContractPath: path, ApprovedBy: "main-session", ApprovalEvidenceID: approvalID})
			if fault == "wrong_approval_scope" {
				// The approval is still bound to its own evidence scope.
				if err == nil {
					t.Fatal("approval with a foreign evidence scope was accepted")
				}
				if !bytes.Equal(beforeState, mustRead(t, f.statePath)) || !bytes.Equal(beforeJournal, mustRead(t, f.journalPath)) {
					t.Fatal("failed approval changed Runtime/journal")
				}
				return
			}
			// Changed original material produces no eligibility difference.
			if err != nil {
				t.Fatalf("approval was refused over changed original material: %v", err)
			}
			document := readCaseDocument(t, f.root, investigationPointer(t, f)["path"].(string))
			if s8TestString(document["status"]) != "contract_approved" {
				t.Fatalf("approval did not reach the approved Case status: %v", document["status"])
			}
		})
	}
}
