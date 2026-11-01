package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
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
	root := investigationLeadTestRoot(t)
	initial := investigationLeadTestSnapshot(t, root)
	mapFieldCLI(initial.State, "configuration")["optimization"] = map[string]any{"packet": true}
	repairLeadWriteState(t, root, initial.State)
	_, path := investigationLeadTestIngest(t, root)
	question := investigationLeadTestQuestion("A stale cache causes the response.")
	question.AgentID = "investigator-01"
	writeRepairLeadJSON(t, path, []investigationLeadQuestion{question})
	feedback := investigationLeadTestSave(t, root, path)
	if !strings.Contains(feedback, "owner=investigator-01") {
		t.Fatalf("original Case and dispatch writer did not register the owner: %s", feedback)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	packets, err := investigation.IssueHypothesisResultPackets(root, statePath, journalPath, -1, "investigator-01")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original owner questionnaire: packets=%+v err=%v", packets, err)
	}
	current := investigationLeadTestSnapshot(t, root)
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil {
		t.Fatalf("read original Case: %v", err)
	}
	f := s8ListenerFixture{root: root, state: current.State, document: document, packet: packets[0]}
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
	data, _ = json.Marshal(f.state)
	if err := os.WriteFile(filepath.Join(f.root, ".claude/loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestS8ListenerProjectsCurrentQuestionnaireWithoutServiceOrReceiptRecovery(t *testing.T) {
	f := newS8ListenerFixture(t)
	path := filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath))
	before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	journalBefore, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
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
	journalAfter, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
	if !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("ordinary reminders touched journal")
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

}

func TestS8ObserverKeepsMainQuietForDelegatedAnswer(t *testing.T) {
	f := newS8ListenerFixture(t)
	path := filepath.Join(f.root, filepath.FromSlash(f.packet.AnswerPath))
	var out, diagnostic bytes.Buffer
	statePath := filepath.Join(f.root, ".claude/loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	request := policy.Input{Event: "PostToolUse", ToolName: "Write", CWD: f.root, ToolInput: map[string]any{"file_path": path}}
	if handled, _ := receiveSavedQuestionnaire(f.root, request, &out, &diagnostic); handled || out.Len() != 0 {
		t.Fatalf("Main's ordinary event received a delegated answer: %s %s", &out, &diagnostic)
	}
	if after, err := os.ReadFile(statePath); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("Main observation changed Runtime: %v", err)
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
		if event == "SubagentStart" && (!strings.Contains(out.String(), answer)) {
			t.Fatalf("Start did not deliver the registered responsibility: %s", out.String())
		}
	}
}

func TestS8ObservedRegisteredMainHypothesesSkipOnlyUnchangedCompleteDispatch(t *testing.T) {
	f := newS8ListenerFixture(t)
	b := f.packet.Binding
	entities := mapFieldCLI(f.state, "entities")
	agent := objectSliceAny(entities["agents"])[0]
	agent["team_id"], agent["task_ids"] = b.TeamID, []any{b.TaskID}
	entities["teams"] = []any{map[string]any{"id": b.TeamID, "kind": "investigator", "manifest_ref": b.ManifestRef}}
	entities["tasks"] = []any{map[string]any{"id": b.TaskID, "path": b.TaskRef, "owner_agent_ids": []any{b.OwnerAgentID}}}
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
			encoded, _ = json.Marshal(f.document)
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			document["revision"], document["route"], document["route_reason"] = 2, test.route, "Original professional disposition"
			document["revision_history"] = []any{map[string]any{"operation": "case_routed", "revision": float64(1)}}
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
				path = filepath.Join(filepath.Dir(path), "route-candidate.json")
			}
			if test.newEvidence {
				history, _ := document["revision_history"].([]any)
				document["revision_history"] = append(history, map[string]any{"operation": "hypothesis_result_submitted"})
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
	mapFieldCLI(f.state, "review")["round_entry"] = map[string]any{"plan_id": "review-plan-current"}
	investigationPointer(f.state)["source_finding_ids"] = []any{"finding-001"}
	mapFieldCLI(f.state, "entities")["findings"] = []any{map[string]any{"finding_id": "finding-001", "path": ".claude/original-finding.json", "review_plan_id": "review-plan-current"}}
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
		for _, ref := range []string{".claude/original-finding.json", f.packet.Binding.CaseRef, "docs/design/architecture.md"} {
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
