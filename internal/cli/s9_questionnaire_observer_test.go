package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s9Observe(t *testing.T, root, tool string, owners ...string) (bool, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	owner := "Main"
	if len(owners) != 0 {
		owner = owners[0]
	}
	handled, code := runObservedQuestionnaireSaves(root, policy.Input{Event: "PostToolUse", ToolName: tool, CWD: root, AgentID: owner}, &out, &diagnostic)
	if code != 0 {
		t.Fatalf("observer exit=%d output=%s diagnostic=%s", code, out.String(), diagnostic.String())
	}
	return handled, out.String()
}

func TestS9ObserverDispatchSavesOriginalChoicesWithoutPassivePreparation(t *testing.T) {
	root, snapshot, plan, path := repairDispatchFixture(t)
	stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot)
	stageRepairVerifierDispatchContext(root, policy.Input{Event: "PreToolUse"}, snapshot)
	before := repairDispatchRuntimeBytes(t, root)
	if handled, out := s9Observe(t, root, "Bash"); handled || out != "" {
		t.Fatalf("untouched Main choices were consumed: %s", out)
	}
	repairDispatchAssertUnchanged(t, root, before)
	rows := []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder", CheckLocation: "worker"}}
	writeRepairLeadJSON(t, path, rows)
	if handled, out := s9Observe(t, root, "Bash"); !handled || !strings.Contains(out, "Registered Builder") {
		t.Fatalf("script division did not reach original dispatch: %s", out)
	}
	registered := repairDispatchSnapshot(t, root)
	if len(mapFieldCLI(mapFieldCLI(registered.State, "workspace"), "executions")) != 0 {
		t.Fatal("passive dispatch attempted workspace preparation")
	}
	before = repairDispatchRuntimeBytes(t, root)
	if handled, out := s9Observe(t, root, "mcp__example__save"); handled || out != "" {
		t.Fatalf("registered division retried queued/missing execution: %s", out)
	}
	repairDispatchAssertUnchanged(t, root, before)
	verifierPath := repairVerifierDispatchPaths(root, registered.State)[0]
	writeRepairLeadJSON(t, verifierPath, []repairVerifierDispatchChoice{{AssignmentID: rows[0].AssignmentID, IndependentVerification: true}})
	if handled, out := s9Observe(t, root, "mcp__example__save"); !handled || !strings.Contains(out, "Registered independent verifier") {
		t.Fatalf("MCP choice did not reach original verifier dispatch: %s", out)
	}
	before = repairDispatchRuntimeBytes(t, root)
	if handled, out := s9Observe(t, root, "Bash"); handled || out != "" {
		t.Fatalf("unchanged registered verifier was resubmitted: %s", out)
	}
	rows[0].RoleFamily = "test-builder"
	writeRepairLeadJSON(t, path, rows)
	if handled, out := s9Observe(t, root, "Bash"); !handled || !strings.Contains(out, "conflict") {
		t.Fatalf("changed registered role bypassed original conflict: %s", out)
	}
	repairDispatchAssertUnchanged(t, root, before)
}

// Exercise the professional projection with canonical packets and receipts
// produced by the original consumers. The old Result-only listener fixture
// intentionally lacked the PlanReport/source closure and cannot model a save.
func TestS9ObserverAcceptedProfessionalAnswerSkipsGenericFollowupProof(t *testing.T) {
	for _, kind := range []string{"plan", "candidate", "result"} {
		t.Run(kind, func(t *testing.T) {
			root, worker, plan, input, _ := nativeS9WorkerFixture(t)
			input.Event = "SubagentStop"
			var out, diagnostic bytes.Buffer
			if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
				t.Fatalf("original native reception: %t %d %s", handled, code, diagnostic.String())
			}
			state := repairDispatchSnapshot(t, root).State
			questions, err := s9ListenerQuestionnairesContext(context.Background(), root, state, plan.Binding.OwnerAgentID, kind != "plan")
			if err != nil || len(questions) != 1 {
				t.Fatalf("exact accepted question: %d %v", len(questions), err)
			}
			packet := questions[0]
			draft, err := s9ListenerQuestionnaireDraft(context.Background(), root, state, packet)
			if err != nil {
				t.Fatalf("canonical draft/source closure unavailable: %v", err)
			}
			path, source := filepath.Join(root, packet.answer), root
			if kind != "plan" {
				path, source = filepath.Join(worker, packet.answer), worker
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			q := observedQuestionnaire{path: path, draft: draft, s9Kind: "packet", s9Packet: &packet}
			before := repairDispatchRuntimeBytes(t, root)
			if !s9ObservedQuestionnaireUnchanged(root, state, q, data) {
				t.Fatal("original accepted professional answer lost immutable projection")
			}
			// Machine wrapper text is not a new professional answer. This
			// must remain a projection comparison, not full-envelope equality.
			var metadataOnly map[string]any
			if err := json.Unmarshal(data, &metadataOnly); err != nil {
				t.Fatal(err)
			}
			metadataOnly["title"] = "changed machine presentation; same professional answer"
			metadataBytes, err := json.Marshal(metadataOnly)
			if err != nil {
				t.Fatal(err)
			}
			if !s9ObservedQuestionnaireUnchanged(root, state, q, metadataBytes) {
				t.Fatal("machine title change caused professional resubmission")
			}
			if handled, feedback := s9Observe(t, root, "Bash", plan.Binding.OwnerAgentID); handled || feedback != "" {
				t.Fatalf("unchanged answer reran reception: %s", feedback)
			}
			repairDispatchAssertUnchanged(t, root, before)

			// A changed projection is distinct, but a native ordinary save is
			// not a delivery. Only the explicit original consumer tests conflict.
			var changed map[string]any
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			answer := mapFieldCLI(changed, "answer")
			if kind == "plan" {
				answer["plan"] = "changed professional judgment"
			} else {
				units := objectSliceAny(answer["unit_results"])
				if len(units) == 0 {
					t.Fatal("real accepted Result has no unit observations")
				}
				units[0]["implementation_summary"] = "changed professional judgment"
			}
			changedBytes, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if s9ObservedQuestionnaireUnchanged(root, state, q, changedBytes) {
				t.Fatal("changed judgment was mistaken for accepted content")
			}
			if err := os.WriteFile(path, changedBytes, 0600); err != nil {
				t.Fatal(err)
			}
			input.Event, input.ToolName = "PostToolUse", "Bash"
			out.Reset()
			diagnostic.Reset()
			if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 || out.Len() != 0 {
				t.Fatalf("native ordinary save became reception: %d %s %s", code, out.String(), diagnostic.String())
			}
			repairDispatchAssertUnchanged(t, root, before)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			var receipt submission.Receipt
			if kind == "plan" {
				receipt, err = service.SaveRepairPlanReportFrom(context.Background(), plan.Binding.OwnerAgentID, path, source, nil)
			} else {
				receipt, err = service.SaveRepairResultFrom(context.Background(), plan.Binding.OwnerAgentID, path, source, nil)
			}
			if !errors.Is(err, runtime.ErrSubmissionConflict) || receipt.Accepted || receipt.Error == "" {
				t.Fatalf("changed answer lost original conflict: %+v %v", receipt, err)
			}
			repairDispatchAssertUnchanged(t, root, before)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}

			if kind == "candidate" {
				// Losing a body does not erase the accepted professional fact.
				// A changed answer now has an unknown comparison, not rejection.
				var bodyRef packetbody.Reference
				for _, row := range objectSliceAny(mapFieldCLI(state, "entities")["work_packets"]) {
					if row["packet_id"] == packet.id {
						var external bool
						bodyRef, external, err = packetbody.ReferenceFromIndex(row)
						if err != nil || !external {
							t.Fatalf("real canonical packet body: %t %v", external, err)
						}
					}
				}
				if bodyRef.Path == "" {
					t.Fatal("accepted packet has no original body reference")
				}
				bodyPath := filepath.Join(root, bodyRef.Path)
				originalBody, err := os.ReadFile(bodyPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(bodyPath); err != nil {
					t.Fatal(err)
				}
				q.draft = nil
				if !s9ObservedQuestionnaireUnchanged(root, state, q, data) {
					t.Fatal("missing body erased accepted professional fact")
				}
				if _, err := s9ListenerQuestionnaireDraft(context.Background(), root, state, packet); err == nil {
					t.Fatal("missing original body was silently rebuilt")
				}
				if err := os.WriteFile(path, changedBytes, 0600); err != nil {
					t.Fatal(err)
				}
				handled, feedback := s9Observe(t, root, "mcp__example__save", plan.Binding.OwnerAgentID)
				if !handled || !strings.Contains(feedback, "S9 observation is incomplete") || !strings.Contains(feedback, "No answer rejection or missing questionnaire was inferred") || strings.Contains(feedback, "S9 SUBMISSION RECEIPT") {
					t.Fatalf("unknown original source misreported acceptance/rejection: %s", feedback)
				}
				repairDispatchAssertUnchanged(t, root, before)
				if err := os.WriteFile(bodyPath, originalBody, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}

			if kind == "result" {
				var integrate, integrateErr bytes.Buffer
				if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", plan.Binding.ManifestAssignmentID, "--agent", plan.Binding.OwnerAgentID, "--check-command", "test -f internal/api/one.go"}, &integrate, &integrateErr); code != 0 {
					t.Fatalf("original Final integration: %d %s %s", code, integrate.String(), integrateErr.String())
				}
				state = repairDispatchSnapshot(t, root).State
				if len(repairArtifactRefs(repairPointerCLI(state), "result_refs", "result_ref", "result_sha256")) == 0 {
					t.Fatal("original Final receipt missing")
				}
				before = repairDispatchRuntimeBytes(t, root)
				if !s9ObservedQuestionnaireUnchanged(root, state, q, data) {
					t.Fatal("Final lost its immutable professional projection after cleanup")
				}
				// Isolate Final's authority in this request-only projection;
				// a surviving candidate must not satisfy the Final assertion.
				finalBytes, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				var finalOnly map[string]any
				if err := json.Unmarshal(finalBytes, &finalOnly); err != nil {
					t.Fatal(err)
				}
				finalExecution := s9ObservedExecution(finalOnly, packet)
				finalExecution["delivery_ref"], finalExecution["delivery_sha256"] = "", ""
				if !s9ObservedQuestionnaireUnchanged(root, finalOnly, q, data) || !s9ObservedQuestionnaireUnchanged(root, finalOnly, q, metadataBytes) {
					t.Fatal("Final projection depended on candidate authority or machine title")
				}
				if handled, feedback := s9Observe(t, root, "Bash", plan.Binding.OwnerAgentID); handled || feedback != "" {
					t.Fatalf("Final history was resubmitted: %s", feedback)
				}
				input.Event = "SubagentStop"
				out.Reset()
				diagnostic.Reset()
				if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
					t.Fatalf("late native return after Final: %s", diagnostic.String())
				}
				repairDispatchAssertUnchanged(t, root, before)
			}
		})
	}
}

func TestS9ObserverLeadReplayUsesImmutableProfessionalProjection(t *testing.T) {
	root, snapshot, plan, _ := repairDispatchFixture(t)
	state := snapshot.State
	pointer := repairPointerCLI(state)
	assignment := plan.Assignments[0].AssignmentID
	verification := repair.VerificationAssignmentID(assignment)
	pointer["assignment_owners"] = map[string]any{assignment: "builder", verification: "verifier"}
	mapFieldCLI(state, "entities")["agents"] = []any{map[string]any{"id": "builder", "role": "backend-builder"}, map[string]any{"id": "verifier", "role": "test-builder"}}
	impact := repair.ChangeImpactRequest{ImpactID: "impact-observer", RuntimeID: stringValue(state["runtime_id"]), ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{stringValue(pointer["case_id"])}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-one", Path: "internal/api/one.go", SHA256: strings.Repeat("a", 64)}},
		Decisions: []repair.ImpactDecision{{SourceID: "changed-one", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-ROOT", Decision: "reverify", Scope: []string{"internal/api/one.go"}, Rationale: "actual impact", RecoveryEvidence: []string{"test://green"}}}, RequiredReverificationIDs: []string{"reverify-observer"}, AnalyzedBy: "Main"}
	_, impactRef, err := repair.CreateChangeImpact(root, impact)
	if err != nil {
		t.Fatal(err)
	}
	pointer["impact_ref"], pointer["impact_sha256"], pointer["status"] = impactRef.Path, impactRef.SHA256, "targeted_reverification"
	pointer["required_reverification_ids"] = []any{"reverify-observer"}
	impactPath, _ := repairLeadQuestionnairePath(root, state, "change-impact.json")
	if err := os.MkdirAll(filepath.Dir(impactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, impactPath, impact)
	targeted := repair.TargetedReverificationRequest{ReverificationID: "reverify-observer", RuntimeID: stringValue(state["runtime_id"]), CaseID: stringValue(pointer["case_id"]), BaselineGeneration: 1, OriginalAssignmentID: assignment, PerformingAssignmentID: verification, ImpactID: impact.ImpactID,
		ContinuityReason: "test://independent: original fixture evidence", AssertionResults: []repair.AssertionResult{{AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://green"}}}, ScopeCompliance: "pass", Result: "pass"}
	_, targetedRef, err := repair.CreateTargetedReverification(root, targeted)
	if err != nil {
		t.Fatal(err)
	}
	pointer["targeted_reverification_artifacts"] = []any{map[string]any{"path": targetedRef.Path, "sha256": targetedRef.SHA256}}
	pointer["status"] = "ready_for_full_review"
	mapFieldCLI(state, "lifecycle")["phase"] = "ready_for_full_review"
	targetPath, _ := repairTargetedQuestionnairePath(root, state, verification)
	targeted.PerformingAssignmentID = "machine-metadata-is-not-author-content"
	writeRepairLeadJSON(t, targetPath, targeted)
	putInitMap(t, filepath.Join(root, ".claude/loop-state.json"), state)
	before := repairDispatchRuntimeBytes(t, root)
	if handled, out := s9Observe(t, root, "Bash"); handled || out != "" {
		t.Fatalf("same indexed professional answers reran full consumers: %s", out)
	}
	impact.Decisions[0].Rationale = "changed impact judgment"
	writeRepairLeadJSON(t, impactPath, impact)
	if handled, out := s9Observe(t, root, "Bash"); handled || out != "" {
		t.Fatalf("ordinary event resubmitted a historical Impact: %s", out)
	}
	var out, diagnostic bytes.Buffer
	located := policy.Input{Event: "PostToolUse", ToolName: "mcp__example__save", AgentID: "native-platform-id", CWD: root, ToolResponse: map[string]any{"file_path": impactPath}}
	if handled, _ := receiveSavedQuestionnaire(root, located, &out, &diagnostic); !handled || !strings.Contains(out.String(), "replay conflict") {
		t.Fatalf("located script/MCP Impact save missed its original consumer: %s %s", out.String(), diagnostic.String())
	}
	impact.Decisions[0].Rationale = "actual impact"
	writeRepairLeadJSON(t, impactPath, impact)
	targeted.ContinuityReason = "test://new: changed independent observation"
	writeRepairLeadJSON(t, targetPath, targeted)
	if handled, out := s9Observe(t, root, "Bash"); handled || out != "" {
		t.Fatalf("ordinary event resubmitted a historical Targeted answer: %s", out)
	}
	out.Reset()
	located.ToolResponse["file_path"] = targetPath
	if handled, _ := receiveSavedQuestionnaire(root, located, &out, &diagnostic); !handled || !strings.Contains(out.String(), "replay conflict") {
		t.Fatalf("located script/MCP Targeted save missed its original consumer: %s %s", out.String(), diagnostic.String())
	}
	repairDispatchAssertUnchanged(t, root, before)
}

func TestS9ObserverResolutionDiscoveryUsesPureRegisteredPath(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	path, err := workpacket.RepairResultResolutionAuthoringPath(workpacket.RepairResultPacket{AnswerPath: f.packet.answer, Binding: *f.packet.result})
	if err != nil {
		t.Fatal(err)
	}
	full := resolveRootPath(f.root, path)
	repairPointerCLI(f.state)["status"] = "blocked"
	f.state["evidence"] = []any{map[string]any{"kind": "repair_result_blocker", "baseline_generation": 1, "scope_refs": []any{f.packet.id}}}
	found := false
	for _, q := range s9ObservableQuestionnaires(f.root, f.state, policy.Input{AgentID: "Main"}) {
		if q.path == full && q.s9Kind == "resolution" {
			found = true
			if !s9ObservedQuestionnaireUnchanged(f.root, f.state, q, []byte(`{"body":"","s9_resolution":{"decision":"retry","rationale":"","evidence_refs":[]}}`)) || s9ObservedQuestionnaireUnchanged(f.root, f.state, q, []byte(`{"body":"real resolution","s9_resolution":{"rationale":"actual recovery"}}`)) {
				t.Fatal("resolution professional-empty filter hid an authored decision")
			}
		}
	}
	if !found {
		t.Fatalf("no exact resolution path without domain/source checks: %s", full)
	}
}

func TestS9ObserverPassiveProbeStaysWithCurrentPhaseAndResponsibility(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	entities := mapFieldCLI(f.state, "entities")
	encoded, _ := json.Marshal(f.row)
	var sibling map[string]any
	if err := json.Unmarshal(encoded, &sibling); err != nil {
		t.Fatal(err)
	}
	sibling["packet_id"] = "packet-sibling"
	sibling["answer_path"], sibling["view_path"] = ".claude/sibling-answer.json", ".claude/sibling-view.json"
	sources := mapFieldCLI(mapFieldCLI(sibling, "binding"), "sources")
	sources["owner_agent_id"], sources["repair_assignment_id"], sources["manifest_assignment_id"] = "builder-sibling", "repair-assignment-sibling", "assignment-s9-sibling"
	execution := mapFieldCLI(mapFieldCLI(sibling, "inputs"), "execution")
	execution["agent_id"], execution["assignment_id"], execution["worktree_path"] = "builder-sibling", "assignment-s9-sibling", t.TempDir()
	projected, err := runtime.ProjectRepairResultExecution(execution)
	if err != nil {
		t.Fatal(err)
	}
	mapFieldCLI(sibling, "binding")["execution_sha256"] = workpacket.Digest(projected)
	mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions")["assignment-s9-sibling"] = execution
	mapFieldCLI(repairPointerCLI(f.state), "assignment_owners")["repair-assignment-sibling"] = "builder-sibling"
	entities["agents"] = append(entities["agents"].([]any), map[string]any{"id": "builder-sibling"})
	// A previous-generation Plan is historical. A current Assignment's pending
	// Plan remains visible even after a sibling has entered fixing.
	historicalBinding := map[string]any{}
	for key, value := range mapFieldCLI(f.row["binding"].(map[string]any), "sources") {
		historicalBinding[key] = value
	}
	historicalBinding["baseline_generation"] = 0
	entities["work_packets"] = append(entities["work_packets"].([]any), map[string]any{
		"packet_id": "historical-plan", "template_id": "s9-plan-report-v1", "answer_path": "historical-plan.json", "view_path": "historical-plan-view.json", "binding": historicalBinding,
	}, sibling)
	for _, scenario := range []struct {
		request policy.Input
		count   int
	}{
		{policy.Input{AgentID: f.packet.binding.OwnerAgentID}, 1},
		{policy.Input{AgentID: "Main", CWD: f.root}, 0},
		{policy.Input{AgentID: "native-platform-id", AgentType: "backend-builder", CWD: f.worker}, 1},
		{policy.Input{AgentID: "native-platform-id", AgentType: "backend-builder", CWD: f.root}, 2},
		{policy.Input{AgentType: "backend-builder", CWD: f.root}, 2},
	} {
		questions := s9ObservableQuestionnaires(f.root, f.state, scenario.request)
		packets := 0
		for _, q := range questions {
			if q.s9Kind != "packet" {
				continue
			}
			packets++
			if q.s9Packet.result == nil || scenario.count == 1 && q.path != resolveRootPath(f.worker, f.packet.answer) {
				t.Fatalf("passive event probed another phase or escaped its located checkout: %+v", q)
			}
		}
		if packets != scenario.count {
			t.Fatalf("current phase/checkout did not locate the expected candidates: %+v count=%d questions=%+v", scenario.request, scenario.count, questions)
		}
	}
}

// This directly exercises the retained save adapter. Public native Worker
// tools are intercepted before it; native reception is tested at SubagentStop.
func TestS9SaveAdapterFindsChangedCurrentPlanWithoutRewritingCaller(t *testing.T) {
	root, plan, _, _, path := repairExecutionPlanningFixture(t)
	writeRepairLeadFile(t, root, ".claude/agents/test-builder.md", "# Test Builder\n")
	var rows []repairDispatchChoice
	for _, assignment := range plan.Assignments {
		role := "backend-builder"
		if len(rows) == 0 {
			role = "test-builder"
		}
		rows = append(rows, repairDispatchChoice{AssignmentID: assignment.AssignmentID, RoleFamily: role})
	}
	if err := writeRepairLeadDraft(path, rows); err != nil {
		t.Fatal(err)
	}
	repairDispatchSave(t, root, path, rows)
	current := repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairPlanReportPackets(root, current.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("current original Plan questions: %+v %v", packets, err)
	}
	if packets[0].Binding.RepairAssignmentID != rows[0].AssignmentID {
		packets[0], packets[1] = packets[1], packets[0]
	}
	foundTestBuilder := false
	for _, agent := range objectSliceAny(mapFieldCLI(current.State, "entities")["agents"]) {
		if agent["id"] == packets[0].Binding.OwnerAgentID && agent["role"] == "test-builder" {
			foundTestBuilder = true
		}
	}
	if !foundTestBuilder {
		t.Fatal("no-change Plan author is not the registered test-builder")
	}
	request := policy.Input{Event: "PostToolUse", ToolName: "Bash", AgentID: "native-platform-id", AgentType: "test-builder", CWD: root}
	var out, diagnostic bytes.Buffer
	before := repairDispatchRuntimeBytes(t, root)
	if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); handled || out.Len() != 0 {
		t.Fatalf("unknown native event consumed untouched issued drafts: %s %s", out.String(), diagnostic.String())
	}
	repairDispatchAssertUnchanged(t, root, before)
	packet := packets[0]
	draft := packet.Draft
	draft.Answer, _ = json.Marshal(workpacket.RepairPlanReportProfessionalAnswer{
		Plan: "perform the assigned no-change tests and record actual assertion observations", ProposedPaths: []string{},
		RedChecks: []workpacket.RepairPlanReportCheck{{Name: "actual precheck", Command: "go test ./internal/api", Result: "pass", EvidenceRefs: []string{".claude/evidence/red.log"}}},
	})
	answerPath := resolveRootPath(root, packet.AnswerPath)
	writeRepairLeadJSON(t, answerPath, draft) // Models a script's actual file save; no host file_path is invented.
	if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); !handled || !strings.Contains(out.String(), "S9 SUBMISSION RECEIPT") {
		t.Fatalf("original save adapter required a matching platform ID or Write event: %s %s", out.String(), diagnostic.String())
	}
	after := repairDispatchSnapshot(t, root)
	refs := repairArtifactRefs(repairPointerCLI(after.State), "plan_report_refs", "plan_report_ref", "plan_report_sha256")
	if len(refs) != 1 {
		t.Fatalf("script save did not consume only the changed current answer: %+v", refs)
	}
	report, err := repair.ValidatePlanReport(root, refs[0])
	if err != nil || report.ProposedPaths == nil || len(report.ProposedPaths) != 0 || report.Receipt == nil {
		t.Fatalf("first no-change test Plan was not preserved in original report: %+v %v", report, err)
	}
	var acceptedAnswer workpacket.RepairPlanReportProfessionalAnswer
	if err := json.Unmarshal(report.Receipt.SubmittedAnswer, &acceptedAnswer); err != nil || acceptedAnswer.ProposedPaths == nil || len(acceptedAnswer.ProposedPaths) != 0 {
		t.Fatalf("original Plan receipt did not preserve proposed_paths=[]: %s %v", report.Receipt.SubmittedAnswer, err)
	}
	out.Reset()
	diagnostic.Reset()
	before = repairDispatchRuntimeBytes(t, root)
	if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); handled || out.Len() != 0 {
		t.Fatalf("same professional answer was resent on an unrelated tool: %s %s", out.String(), diagnostic.String())
	}
	repairDispatchAssertUnchanged(t, root, before)
	if request.AgentID != "native-platform-id" || len(request.ToolInput) != 0 {
		t.Fatal("passive routing rewrote the real native event identity/input")
	}
	if mapFieldCLI(after.State, "lifecycle")["phase"] != "fixing" {
		t.Fatal("first Plan did not start normal work")
	}
	second := packets[1]
	reminder, handled := s9PacketQuestionnaireLifecycleContext(root, policy.Input{Event: "PreToolUse", AgentID: second.Binding.OwnerAgentID, CWD: root}, after)
	brief, _, _ := nativeS9Paths(second)
	planPath, resultPath := nativeS9TestAuthorPaths(t, root, second)
	if !handled || !strings.Contains(reminder, "exact native brief "+resolveRootPath(root, brief)) || !strings.Contains(reminder, "private worktree-relative PlanReport "+planPath) || !strings.Contains(reminder, resultPath) || !strings.Contains(reminder, "Ordinary saves do not receive or activate this responsibility") {
		t.Fatalf("sibling implementation hid the original native Builder Plan/Result route: %s", reminder)
	}
	var secondScope []string
	for _, assignment := range plan.Assignments {
		if assignment.AssignmentID == second.Binding.RepairAssignmentID {
			secondScope = assignment.Scope
		}
	}
	secondDraft := second.Draft
	secondDraft.Answer, _ = json.Marshal(workpacket.RepairPlanReportProfessionalAnswer{Plan: "record this Builder's actual plan after sibling startup", ProposedPaths: secondScope, RedChecks: []workpacket.RepairPlanReportCheck{{Name: "actual precheck", Command: "go test ./internal/api", Result: "pass", EvidenceRefs: []string{".claude/evidence/check.log"}}}})
	writeRepairLeadJSON(t, resolveRootPath(root, second.AnswerPath), secondDraft)
	out.Reset()
	if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); !handled || !strings.Contains(out.String(), "S9 SUBMISSION RECEIPT") {
		t.Fatalf("late script Plan not saved: %s %s", out.String(), diagnostic.String())
	}
	final := repairDispatchSnapshot(t, root)
	if mapFieldCLI(final.State, "lifecycle")["phase"] != "fixing" || repairPointerCLI(final.State)["status"] != "repairing" {
		t.Fatal("late Plan regressed implementation")
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(journal), `"transition_id":"PTR-BUG-10"`) != 2 || strings.Contains(string(journal), `"transition_id":"PTR-BUG-11"`) {
		t.Fatalf("each original Plan must be recorded once without a separate Begin: %s", journal)
	}
	before = repairDispatchRuntimeBytes(t, root)
	out.Reset()
	if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); handled {
		t.Fatalf("late Plan replay repeated work: %s", out.String())
	}
	repairDispatchAssertUnchanged(t, root, before)

	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.SaveRepairPlanReportFrom(context.Background(), second.Binding.OwnerAgentID, resolveRootPath(root, second.AnswerPath), root, nil)
	if err != nil || !replay.Accepted || replay.ConsumerPending {
		t.Fatalf("late Plan direct replay lost acceptance: %+v %v", replay, err)
	}
	repairDispatchAssertUnchanged(t, root, before)

}

func TestS9NativeDivisionKeepsDependencyAndRejectsExtraWorkspacePreparation(t *testing.T) {
	root, plan, _, _, path := repairExecutionPlanningFixture(t)
	rows := make([]repairDispatchChoice, 0, len(plan.Assignments))
	for _, assignment := range plan.Assignments {
		rows = append(rows, repairDispatchChoice{AssignmentID: assignment.AssignmentID, RoleFamily: "backend-builder"})
	}
	repairDispatchSave(t, root, path, rows)
	current := repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairPlanReportPackets(root, current.State, "")
	if err != nil || len(packets) != len(rows) {
		t.Fatalf("normal original Plan questions: %+v %v", packets, err)
	}
	service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
	for _, packet := range packets {
		draft := packet.Draft
		draft.Answer, _ = json.Marshal(workpacket.RepairPlanReportProfessionalAnswer{
			Plan:          "synthetic fixture plan for the assigned dependency and original queue",
			ProposedPaths: []string{},
			RedChecks:     []workpacket.RepairPlanReportCheck{{Name: "synthetic fixture precheck", Command: "test -f internal/api/one.go", Result: "pass", EvidenceRefs: []string{"test://fixture-precheck"}}},
		})
		answerPath := resolveRootPath(root, packet.AnswerPath)
		writeRepairLeadJSON(t, answerPath, draft)
		receipt, err := service.SaveRepairPlanReportFrom(context.Background(), packet.Binding.OwnerAgentID, answerPath, root, nil)
		if err != nil || !receipt.Accepted {
			t.Fatalf("normal original Plan save: %+v %v", receipt, err)
		}
	}
	for i := range rows {
		rows[i].CheckLocation = "main"
	}
	writeRepairLeadJSON(t, path, rows)
	beforeSave := repairDispatchRuntimeBytes(t, root)
	if handled, out := s9Observe(t, root, "Bash"); handled || out != "" {
		t.Fatalf("registered script selection must not probe the preparation queue: %s", out)
	}
	selected := repairDispatchSnapshot(t, root)
	context := stageRepairExecutionPrepareContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main"}, selected)
	for _, packet := range packets {
		brief, _, _ := nativeS9Paths(packet)
		if !strings.Contains(context, resolveRootPath(root, brief)) {
			t.Fatalf("read-only context omitted the actual native brief: %s", context)
		}
	}
	if !strings.Contains(context, "depends_on") || strings.Contains(context, "runtime workspace bind --root") || strings.Contains(context, "runtime workspace prepare --root") {
		t.Fatalf("native reminder lost dependency responsibility or added a startup layer: %s", context)
	}
	repairDispatchAssertUnchanged(t, root, beforeSave)
	if selected.State["workspace"] != nil {
		t.Fatal("saving a location prepared a workspace inside the Hook")
	}
	owners := stringMapCLI(repairPointerCLI(selected.State)["assignment_owners"])
	// Native responsibilities cannot be launched through the retained legacy
	// workspace prepare API. The real frozen queue/dependency behavior remains
	// covered by TestS9RepairExecutionChoiceQueuedSameSaveAfterOriginalResult.
	var startupOut, startupErr bytes.Buffer
	manifestID, _ := repairExecutionForChoice(selected.State, rows[0])
	if code := runRuntimeWorkspace([]string{"prepare", "--root", root, "--assignment", manifestID, "--agent", owners[rows[0].AssignmentID], "--check-location", "main"}, &startupOut, &startupErr); code == 0 || !strings.Contains(startupErr.String(), "Claude Code Agent/worktree") {
		t.Fatalf("native responsibility entered legacy workspace preparation: %s %s", startupOut.String(), startupErr.String())
	}
	repairDispatchAssertUnchanged(t, root, beforeSave)
	for _, tool := range []string{"Bash", "mcp__example__save"} {
		if handled, out := s9Observe(t, root, tool); handled || out != "" {
			t.Fatalf("unchanged native division repeated work: %s", out)
		}
	}
	repairDispatchAssertUnchanged(t, root, beforeSave)
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("original professional division answer was lost: %v", err)
	}
}

func TestS9ObserverLocatedMainDivisionSaveDoesNotRequireCallerIdentity(t *testing.T) {
	root, snapshot, plan, builderPath := repairDispatchFixture(t)
	stageRepairVerifierDispatchContext(root, policy.Input{AgentID: "Main", Event: "PreToolUse"}, snapshot)
	writeRepairLeadJSON(t, builderPath, []repairDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, RoleFamily: "backend-builder"}})
	var out, diagnostic bytes.Buffer
	request := policy.Input{Event: "PostToolUse", ToolName: "mcp__example__save", AgentID: "native-platform-id", CWD: root, ToolResponse: map[string]any{"file_path": builderPath}}
	if handled, _ := receiveSavedQuestionnaire(root, request, &out, &diagnostic); !handled || !strings.Contains(out.String(), "Registered Builder") {
		t.Fatalf("located Main division was gated by platform caller identity: %s %s", out.String(), diagnostic.String())
	}
	registered := repairDispatchSnapshot(t, root)
	if registered.State["workspace"] != nil {
		t.Fatal("located division save prepared a workspace")
	}
	verifierPath := repairVerifierDispatchPaths(root, registered.State)[0]
	writeRepairLeadJSON(t, verifierPath, []repairVerifierDispatchChoice{{AssignmentID: plan.Assignments[0].AssignmentID, IndependentVerification: true}})
	request.ToolResponse["file_path"] = verifierPath
	out.Reset()
	if handled, _ := receiveSavedQuestionnaire(root, request, &out, &diagnostic); !handled || !strings.Contains(out.String(), "Registered independent verifier") {
		t.Fatalf("located Main verifier division was gated by platform caller identity: %s %s", out.String(), diagnostic.String())
	}
}
