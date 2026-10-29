package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type s8ListenerFixture struct {
	root     string
	state    map[string]any
	document map[string]any
	packet   workpacket.HypothesisResultPacket
}

func newS8ListenerFixture(t *testing.T) s8ListenerFixture {
	t.Helper()
	root := t.TempDir()
	question := workpacket.HypothesisResultQuestion{
		HypothesisID: "hypothesis-cache-1", AssignmentID: "assignment-investigator-1", Statement: "A stale cache causes the response.",
		Invariant: "A fresh read reflects the persisted value.", Discriminator: "Compare cache bypass.",
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: "Bypass fixes it.", Refute: "Bypass leaves it stale."},
		SourceFindingIDs: []string{"finding-001"}, EvidenceRefs: []string{"test://probe"}, Status: "open",
	}
	questionSHA, _ := workpacket.HypothesisQuestionDigest(question)
	route := workpacket.HypothesisResultCaseRoute{Status: "investigating"}
	routeSHA, _ := workpacket.HypothesisCaseRouteDigest(route)
	binding := workpacket.HypothesisResultBinding{
		RuntimeID: "loop-REQ-001", BaselineGeneration: 1, ReviewRound: 1, CaseID: "investigation-case-001",
		CaseRef: ".claude/case.json", CaseSHA256: strings.Repeat("a", 64), CaseRevision: 1, CaseRouteSHA256: routeSHA,
		ObservationBatchID: "observation-batch-001", ObservationBatchRef: ".claude/batch.json", ObservationBatchSHA256: strings.Repeat("b", 64),
		BaselineDigest: strings.Repeat("c", 64), SourceFindingIDs: []string{"finding-001"}, HypothesisID: question.HypothesisID,
		HypothesisQuestionSHA256: questionSHA, HypothesisSourceFindingIDs: question.SourceFindingIDs, AssignmentID: question.AssignmentID,
		AssignmentSHA256: strings.Repeat("e", 64), TeamID: "workgroup-s8-1", TaskID: "TASK-001-01", TaskRef: "docs/tasks/TASK-001-01.md",
		TaskSHA256: strings.Repeat("f", 64), ManifestRef: ".claude/workgroups/workgroup-s8-1.json", ManifestSHA256: strings.Repeat("1", 64), OwnerAgentID: "investigator-01",
	}
	finding := json.RawMessage(`{"schema_version":"1.0.0","finding_id":"finding-001","claim_id":"claim-cache","lens":"qa","severity":"P1","expected":"Fresh value","authority_refs":["REQ-001"],"observed":"Stale value","observation_mode":"code_inspection","encounter":{"journey_summary":"Read path inspected","wall_action":"Read value","first_bad_checkpoint":"Cache stale","inspection_entry":"cache.go","symbol_trail":"Read -> Cache"},"reproducibility":"always","evidence_refs":["test://observation"]}`)
	packet, err := workpacket.BuildHypothesisResult(binding, workpacket.HypothesisResultInputs{Hypothesis: question, CaseRoute: route,
		Findings:     []workpacket.HypothesisResultFinding{{FindingID: "finding-001", Ref: ".claude/finding.json", SHA256: strings.Repeat("d", 64), Document: finding}},
		EvidenceRefs: []workpacket.HypothesisEvidenceRef{{Ref: "test://probe", Origin: "hypothesis", Attestation: "execution_anchor"}, {Ref: "test://observation", Origin: "finding", SourceFindingID: "finding-001", Attestation: "execution_anchor"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"], state["runtime_id"] = "1.2.0", binding.RuntimeID
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["baseline"].(map[string]any)["generation"] = 1
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "investigation"}
	state["review"] = map[string]any{"round": 1, "investigation": map[string]any{"case_id": binding.CaseID, "path": binding.CaseRef, "revision": 1, "status": "investigating"}}
	encoded, _ := json.Marshal(packet)
	var row map[string]any
	if err := json.Unmarshal(encoded, &row); err != nil {
		t.Fatal(err)
	}
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = []any{row}
	entities["agents"] = []any{map[string]any{"id": binding.OwnerAgentID, "role": "investigator", "state": "working", "prompt_ref": binding.ManifestRef + "#" + binding.AssignmentID}}
	encoded, _ = json.Marshal(question)
	var questionRow map[string]any
	if err := json.Unmarshal(encoded, &questionRow); err != nil {
		t.Fatal(err)
	}
	document := map[string]any{"case_id": binding.CaseID, "revision": 1, "status": "investigating", "hypotheses": []any{questionRow}, "hypothesis_results": []any{}}
	f := s8ListenerFixture{root: root, state: state, document: document, packet: packet}
	f.write(t)
	for _, ref := range []string{packet.ViewPath, packet.AnswerPath} {
		path := filepath.Join(root, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserved professional draft"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f s8ListenerFixture) write(t *testing.T) {
	t.Helper()
	data, _ := json.Marshal(f.document)
	path := filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	investigationPointer(f.state)["sha256"] = workpacket.Digest(data)
	data, _ = json.Marshal(f.state)
	if err := os.WriteFile(filepath.Join(f.root, ".claude/loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f s8ListenerFixture) accept(t *testing.T, answerSHA string) {
	t.Helper()
	b := f.packet.Binding
	f.document["hypothesis_results"] = []any{map[string]any{"receipt": map[string]any{
		"contract": "s8-hypothesis-result-receipt-v1", "packet_id": f.packet.ID, "answer_sha256": answerSHA, "execution_root": f.root,
		"runtime_id": b.RuntimeID, "baseline_generation": b.BaselineGeneration, "review_round": b.ReviewRound,
		"owner_agent_id": b.OwnerAgentID, "assignment_id": b.AssignmentID, "team_id": b.TeamID, "task_id": b.TaskID,
		"successor_case_ref": ".claude/case-r2.json", "successor_case_revision": 2,
	}}}
	f.write(t)
}

func TestS8ListenerProjectsCurrentQuestionnaireWithoutServiceOrReceiptRecovery(t *testing.T) {
	f := newS8ListenerFixture(t)
	path := filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath))
	before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	for _, event := range []string{"SessionStart", "SubagentStart", "PreCompact", "PreToolUse", "Stop", "SubagentStop", "TeammateIdle"} {
		for _, owner := range []string{"", f.packet.Binding.OwnerAgentID} {
			if event == "SubagentStart" && owner == "" {
				continue
			}
			request := policy.Input{Event: event, AgentID: owner, CWD: f.root, Runtime: policy.RuntimeContext{PacketMode: true}}
			result, observed := graphControlForHook(f.root, request)
			if !observed || result.QualityGate.Status != "" || result.Decision.Guidance == nil || result.Decision.Guidance.Blocked {
				t.Fatalf("%s: did not use current graph: %+v", event, result)
			}
			if strings.Contains(result.Decision.Guidance.Action, "runtime investigation") {
				t.Fatalf("%s: command-only guidance survived: %s", event, result.Decision.Guidance.Action)
			}
			context := currentQuestionnaireLifecycleContext(f.root, request)
			if !strings.Contains(context, path) || strings.Contains(context, "work_complete=true") {
				t.Fatalf("%s: current exact path lost or completed without receipt: %s", event, context)
			}
		}
	}
	after, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("ordinary reminders wrote Runtime")
	}
	if _, err := os.Stat(filepath.Join(f.root, ".claude/loop-events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("ordinary reminders touched journal: %v", err)
	}
	// A projected acceptance must still carry the original exact answer path;
	// only the original stop consumer can prove that the owner may actually end.
	f.accept(t, strings.Repeat("a", 64))
	context := currentQuestionnaireLifecycleContext(f.root, policy.Input{Event: "SubagentStop", AgentID: f.packet.Binding.OwnerAgentID})
	if !strings.Contains(context, path) || !strings.Contains(context, "work_complete=true") {
		t.Fatalf("accepted reminder lost original path: %s", context)
	}
}

func TestS8ListenerKeepsRegisteredPathWhenCaseSourceFails(t *testing.T) {
	f := newS8ListenerFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef)), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	context := currentQuestionnaireLifecycleContext(f.root, policy.Input{Event: "SessionStart", AgentID: f.packet.Binding.OwnerAgentID, Runtime: policy.RuntimeContext{PacketMode: true}})
	if !strings.Contains(context, filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath))) || !strings.Contains(context, "recorded Case source") {
		t.Fatalf("source failure hid issued path: %s", context)
	}
}

func TestS8ListenerPreparesOnlyMissingUnacceptedQuestionnaires(t *testing.T) {
	f := newS8ListenerFixture(t)
	packets, err := s8ListenerPackets(f.state, f.document, "")
	if err != nil {
		t.Fatal(err)
	}
	if s8QuestionnairesNeedPreparation(f.root, f.state, f.document, packets) {
		t.Fatal("existing files requested another full issue")
	}
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(f.packet.ViewPath))); err != nil {
		t.Fatal(err)
	}
	if !s8QuestionnairesNeedPreparation(f.root, f.state, f.document, packets) {
		t.Fatal("missing view did not request original preparation")
	}
	f.accept(t, strings.Repeat("a", 64))
	if s8QuestionnairesNeedPreparation(f.root, f.state, f.document, packets) {
		t.Fatal("accepted work requested a fresh author draft")
	}
}

func TestS8ObserverKeepsMainQuietAndAdapterDetectsCanonicalChanges(t *testing.T) {
	f := newS8ListenerFixture(t)
	var draft map[string]any
	data, _ := json.Marshal(f.packet.Draft)
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	answer := draft["answer"].(map[string]any)
	answer["method"], answer["observed"], answer["counterfactual_or_discriminator"] = "Bypass cache", "Fresh value", "A stale bypass would refute it"
	answer["evidence_refs"], answer["source_boundary_refs"] = []any{"test://probe"}, []any{"finding-001.encounter.first_bad_checkpoint"}
	answer["result"], answer["explains_finding_ids"], answer["does_not_explain"], answer["new_hypotheses"] = "supported", []any{"finding-001"}, []any{}, []any{}
	data, _ = json.MarshalIndent(draft, "", "  ")
	canonical, issues := workpacket.ValidateHypothesisResultAnswer(f.packet, data)
	if len(issues) != 0 {
		t.Fatalf("test authored answer invalid: %+v", issues)
	}
	sameAnswer := append([]byte(nil), data...)
	f.accept(t, workpacket.Digest(canonical))
	path := filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	request := policy.Input{Event: "PostToolUse", ToolName: "Bash", CWD: f.root}
	statePath := filepath.Join(f.root, ".claude/loop-state.json")
	casePath := filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef))
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	caseBefore, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatal(err)
	}
	if handled, _ := runObservedQuestionnaireSaves(f.root, request, &out, &errout); handled || out.Len() != 0 {
		t.Fatalf("unchanged accepted answer was resubmitted: %s", out.String())
	}
	answer["observed"] = "Different professional observation"
	data, _ = json.Marshal(draft)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if handled, _ := runObservedQuestionnaireSaves(f.root, request, &out, &errout); handled || out.Len() != 0 {
		t.Fatalf("Main's passive event received a changed delegated answer: %s %s", &out, &errout)
	}
	request.ToolName = "Write"
	request.ToolInput = map[string]any{"file_path": path}
	if handled, _ := receiveSavedQuestionnaire(f.root, request, &out, &errout); handled || out.Len() != 0 {
		t.Fatalf("Main's explicit save received a delegated answer: %s %s", &out, &errout)
	}
	// The synthetic receipt/Case fixture tests the internal canonical filter,
	// not real acceptance. Native original-consumer conflict/recovery is
	// verified by the actual Case/Writer chain in investigation's native test.
	request.AgentID = f.packet.Binding.OwnerAgentID
	request.ToolName, request.ToolInput = "Bash", nil
	if err := os.WriteFile(path, sameAnswer, 0600); err != nil {
		t.Fatal(err)
	}
	if handled, _ := runObservedQuestionnaireSaves(f.root, request, &out, &errout); handled || out.Len() != 0 {
		t.Fatalf("canonical adapter replay was resubmitted: %s %s", &out, &errout)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if handled, _ := runObservedQuestionnaireSaves(f.root, request, &out, &errout); !handled || !strings.Contains(out.String(), "S8 SUBMISSION RECEIPT") {
		t.Fatalf("changed accepted answer did not reach original save feedback: %s %s", out.String(), errout.String())
	}
	out.Reset()
	errout.Reset()
	request.ToolName = "Write"
	request.ToolInput = map[string]any{"file_path": path}
	if handled, _ := receiveSavedQuestionnaire(f.root, request, &out, &errout); !handled || !strings.Contains(out.String(), "S8 SUBMISSION RECEIPT") {
		t.Fatalf("explicit save was skipped: %s %s", out.String(), errout.String())
	}
	for path, before := range map[string][]byte{statePath: stateBefore, casePath: caseBefore} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("filter/error feedback changed authority %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, ".claude/loop-events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("synthetic adapter fixture invented an accepted journal: %v", err)
	}
}

func TestS8NativeLifecycleDeliversExactQuestionnaireInHookEnvelope(t *testing.T) {
	f := newS8ListenerFixture(t)
	answer := filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath))
	for _, event := range []string{"SessionStart", "SubagentStart"} {
		request := policy.Input{Event: event, AgentID: f.packet.Binding.OwnerAgentID, CWD: f.root}
		data, _ := json.Marshal(request)
		var out, errout bytes.Buffer
		if code := evaluate(f.root, event, bytes.NewReader(data), &out, &errout, true); code != 0 {
			t.Fatalf("%s exit=%d stdout=%s stderr=%s", event, code, out.String(), errout.String())
		}
		if event == "SessionStart" && out.Len() != 0 {
			t.Fatalf("ordinary Worker session managed native work: %s", out.String())
		}
		if event == "SubagentStart" && (!strings.Contains(out.String(), "exact original questionnaire") || strings.Contains(out.String(), answer)) {
			t.Fatalf("Start guessed a responsibility from actor/role: %s", out.String())
		}
	}
}

func TestS8ObservedRegisteredMainHypothesesSkipOnlyUnchangedCompleteDispatch(t *testing.T) {
	f := newS8ListenerFixture(t)
	b := f.packet.Binding
	entities := mapFieldCLI(f.state, "entities")
	agent := objectSliceAny(entities["agents"])[0]
	agent["team_id"], agent["task_ids"] = b.TeamID, []any{b.TaskID}
	entities["teams"] = []any{map[string]any{"id": b.TeamID, "kind": "investigator", "manifest_ref": b.ManifestRef, "manifest_sha256": b.ManifestSHA256}}
	entities["tasks"] = []any{map[string]any{"id": b.TaskID, "path": b.TaskRef, "sha256": b.TaskSHA256, "owner_agent_ids": []any{b.OwnerAgentID}}}
	question := investigationLeadQuestionFromCase(objectSliceAny(f.document["hypotheses"])[0])
	path := filepath.Join(f.root, ".claude/submissions/s8/hypotheses.json")
	for _, test := range []struct {
		name   string
		change func([]investigationLeadQuestion, map[string]any) []investigationLeadQuestion
		want   bool
	}{
		{name: "same registered content and implicit original owner", want: true},
		{name: "same registered content and explicit original owner", want: true, change: func(q []investigationLeadQuestion, _ map[string]any) []investigationLeadQuestion {
			q[0].AgentID = b.OwnerAgentID
			return q
		}},
		{name: "append new professional question", change: func(q []investigationLeadQuestion, _ map[string]any) []investigationLeadQuestion {
			added := q[0]
			added.HypothesisID = ""
			added.Statement = "A second mechanism"
			return append(q, added)
		}},
		{name: "alter registered professional content", change: func(q []investigationLeadQuestion, _ map[string]any) []investigationLeadQuestion {
			q[0].Discriminator = "A different discriminator"
			return q
		}},
		{name: "change registered owner", change: func(q []investigationLeadQuestion, _ map[string]any) []investigationLeadQuestion {
			q[0].AgentID = "different-investigator"
			return q
		}},
		{name: "partial dispatch has no registered owner", change: func(q []investigationLeadQuestion, state map[string]any) []investigationLeadQuestion {
			mapFieldCLI(state, "entities")["agents"] = []any{}
			return q
		}},
		{name: "partial dispatch has no registered team", change: func(q []investigationLeadQuestion, state map[string]any) []investigationLeadQuestion {
			mapFieldCLI(state, "entities")["teams"] = []any{}
			return q
		}},
		{name: "partial dispatch has no registered task", change: func(q []investigationLeadQuestion, state map[string]any) []investigationLeadQuestion {
			mapFieldCLI(state, "entities")["tasks"] = []any{}
			return q
		}},
		{name: "partial dispatch task owner differs", change: func(q []investigationLeadQuestion, state map[string]any) []investigationLeadQuestion {
			objectSliceAny(mapFieldCLI(state, "entities")["tasks"])[0]["owner_agent_ids"] = []any{"different-investigator"}
			return q
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, _ := json.Marshal(f.state)
			var state map[string]any
			if err := json.Unmarshal(encoded, &state); err != nil {
				t.Fatal(err)
			}
			questions := []investigationLeadQuestion{question}
			if test.change != nil {
				questions = test.change(questions, state)
			}
			data, _ := json.Marshal(questions)
			before, _ := json.Marshal(state)
			if got := s8ObservedHypothesesAlreadyRegistered(path, data, state, f.document); got != test.want {
				t.Fatalf("skip=%v want=%v", got, test.want)
			}
			after, _ := json.Marshal(state)
			if !bytes.Equal(before, after) {
				t.Fatal("observer projection wrote original Runtime facts")
			}
		})
	}
	for _, data := range [][]byte{[]byte(`[{`), []byte(`[{"statement":"unrecognized","unknown_field":true}]`)} {
		if s8ObservedHypothesesAlreadyRegistered(path, data, f.state, f.document) {
			t.Fatal("invalid edited data was hidden from the original consumer")
		}
	}
	// Exercise the actual no-file_path observer. No manifest, TASK or Batch
	// bytes exist: the unchanged Main view must use only recorded facts.
	mapFieldCLI(f.state, "review")["observation_batch"] = map[string]any{"path": b.ObservationBatchRef, "sha256": b.ObservationBatchSHA256}
	f.write(t)
	paths := investigationLeadQuestionnairePaths(f.root, f.state)
	if len(paths) == 0 {
		t.Fatal("current Main questionnaire path missing from fixture")
	}
	path = paths[0]
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]investigationLeadQuestion{question})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(f.packet.Draft)
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath)), data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	request := policy.Input{Event: "PostToolUse", ToolName: "Bash", CWD: f.root}
	if handled, _ := runObservedQuestionnaireSaves(f.root, request, &out, &errout); handled || out.Len() != 0 {
		t.Fatalf("same registered Main questions were resubmitted: %s %s", out.String(), errout.String())
	}
	request.ToolName, request.ToolInput = "Write", map[string]any{"file_path": path}
	if handled, _ := receiveSavedQuestionnaire(f.root, request, &out, &errout); !handled || out.Len() == 0 {
		t.Fatal("explicit Main save bypassed original consumer feedback")
	}
}

func TestS8ObservedPersistedRouteSkipsOnlyUnchangedCompletedRouteWork(t *testing.T) {
	f := newS8ListenerFixture(t)
	mapFieldCLI(f.state, "review")["observation_batch"] = map[string]any{"path": f.packet.Binding.ObservationBatchRef, "sha256": f.packet.Binding.ObservationBatchSHA256}
	for _, test := range []struct {
		name        string
		route       string
		consumed    bool
		alter       bool
		retired     bool
		newEvidence bool
		staleSource bool
		want        bool
	}{
		{name: "normal repair route needs no separate route consume", route: "s9_repair", want: true},
		{name: "human decision remains separate", route: "human_req_change", want: true},
		{name: "more investigation remains separate", route: "investigate_more", want: true},
		{name: "specification exit consume pending", route: "s2_spec_rework"},
		{name: "specification exit consume recorded", route: "s2_spec_rework", consumed: true, want: true},
		{name: "duplicate exit consume pending", route: "duplicate"},
		{name: "duplicate exit consume recorded", route: "duplicate", consumed: true, want: true},
		{name: "no change consume pending", route: "s7_no_change"},
		{name: "no change retired Case is no longer current", route: "s7_no_change", retired: true},
		{name: "professional route answer changed", route: "s9_repair", alter: true},
		{name: "new evidence requires current professional decision", route: "investigate_more", newEvidence: true},
		{name: "route belongs to earlier source", route: "s9_repair", staleSource: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, _ := json.Marshal(f.state)
			var state map[string]any
			if err := json.Unmarshal(encoded, &state); err != nil {
				t.Fatal(err)
			}
			document := map[string]any{"case_id": f.packet.Binding.CaseID, "revision": 2, "route": test.route, "route_reason": "Original professional disposition", "revision_history": []any{map[string]any{"operation": "case_routed", "sha256": strings.Repeat("9", 64)}}}
			pointer := investigationPointer(state)
			pointer["route"], pointer["revision"] = test.route, 2
			if test.consumed {
				pointer["route_consumed_at"], pointer["route_consumer"] = "2026-10-17T00:00:00Z", "S8-ROUTE-CONSUME"
			}
			path, err := investigationLeadRoutePath(f.root, state, document)
			if err != nil {
				t.Fatal(err)
			}
			if test.staleSource {
				path = filepath.Join(filepath.Dir(path), "route-"+strings.Repeat("8", 64)+".json")
			}
			if test.newEvidence {
				history, _ := document["revision_history"].([]any)
				document["revision_history"] = append(history, map[string]any{"operation": "hypothesis_result_submitted", "sha256": strings.Repeat("7", 64)})
			}
			if test.retired {
				mapFieldCLI(state, "review")["investigation"] = nil
			}
			answer := investigationLeadRouteAnswerFromCase(document)
			if test.alter {
				answer.RouteReason = "Changed professional judgment"
			}
			data, _ := json.Marshal(answer)
			before, _ := json.Marshal(state)
			if got := s8ObservedRouteAlreadyRegistered(f.root, path, data, state, document); got != test.want {
				t.Fatalf("skip=%v want=%v", got, test.want)
			}
			if s8ObservedRouteAlreadyRegistered(f.root, path, []byte(`{"route":`), state, document) {
				t.Fatal("invalid route edit was hidden from original consumer")
			}
			after, _ := json.Marshal(state)
			if !bytes.Equal(before, after) {
				t.Fatal("route observer wrote Runtime facts")
			}
		})
	}
}

func TestS8GuidanceReadsProfessionalQuestionsAndActualSourcesBeforeRecovery(t *testing.T) {
	f := newS8ListenerFixture(t)
	review := mapFieldCLI(f.state, "review")
	review["observation_batch"] = map[string]any{"batch_id": f.packet.Binding.ObservationBatchID, "path": f.packet.Binding.ObservationBatchRef, "sha256": f.packet.Binding.ObservationBatchSHA256, "finding_ids": []any{"finding-001"}, "drain_policy": "complete_required_claims"}
	mapFieldCLI(f.state, "entities")["findings"] = []any{map[string]any{"finding_id": "finding-001", "path": ".claude/original-finding.json"}}
	f.state["documents"] = []any{map[string]any{"kind": "architecture", "status": "locked", "generation": 1, "path": "docs/design/architecture.md"}}
	question := investigationLeadQuestionnairePathsForQuestions(f.root, f.state)[0]
	for _, test := range []struct {
		owner string
		event string
		first string
	}{
		{owner: "", event: "SessionStart", first: question},
		{owner: f.packet.Binding.OwnerAgentID, event: "PreToolUse", first: filepath.Join(f.root, filepath.FromSlash(f.packet.ViewPath))},
	} {
		g := graphGuidance(f.root, f.state, policy.Input{Event: test.event, AgentID: test.owner})
		if len(g.ReadOrder) == 0 || g.ReadOrder[0] != test.first {
			t.Fatalf("professional questionnaire was not first: %#v", g.ReadOrder)
		}
		joined := strings.Join(g.ReadOrder, "\n")
		for _, ref := range []string{f.packet.Binding.ObservationBatchRef, ".claude/original-finding.json", f.packet.Binding.CaseRef, "docs/design/architecture.md"} {
			if !strings.Contains(joined, filepath.Join(f.root, filepath.FromSlash(ref))) {
				t.Fatalf("actual source %s absent from read order: %#v", ref, g.ReadOrder)
			}
		}
		if strings.Contains(joined, "loop-state.json") || strings.Contains(joined, "loop-harness.md") || strings.Contains(joined, "LOOP RECOVERY packet") {
			t.Fatalf("recovery became a normal reading prerequisite: %#v", g.ReadOrder)
		}
		if !strings.Contains(formatGuidanceInstruction(g), "Only if blocked") {
			t.Fatal("manual recovery remained mandatory")
		}
		if test.owner == "" && !strings.HasPrefix(g.Action, "Main questionnaire:") {
			t.Fatalf("Main questionnaire ownership was not explicit: %s", g.Action)
		}
		for _, line := range g.Automation {
			if strings.Contains(line, "entered via TR-008") {
				t.Fatalf("Batch pointer falsely proved transition: %s", line)
			}
		}
	}
	for _, request := range []policy.Input{{Event: "SubagentStart"}, {Event: "SubagentStart", AgentID: "unmapped-native-id"}} {
		g := graphGuidance(f.root, f.state, request)
		if strings.Contains(strings.Join(g.ReadOrder, "\n"), question) || !strings.Contains(g.ReadOrder[0], "supplied by Main") {
			t.Fatalf("unknown native owner was given Main's responsibility: %#v", g.ReadOrder)
		}
	}
	// S7 retains its existing recovery order and rendering.
	f.state["lifecycle"] = map[string]any{"state": "verification", "phase": "running"}
	expected := BuildGuidanceForState(f.root, f.state, "PreToolUse", policy.Input{Event: "SessionStart"})
	got := graphGuidance(f.root, f.state, policy.Input{Event: "SessionStart"})
	if !reflect.DeepEqual(got.ReadOrder, expected.ReadOrder) || !strings.Contains(formatGuidanceInstruction(got), "for the harness recovery procedure") {
		t.Fatalf("S7 reading behavior changed: %#v %#v", expected.ReadOrder, got.ReadOrder)
	}
}
