package cli

import (
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
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type s9ListenerFixture struct {
	root, worker string
	state        map[string]any
	row          map[string]any
	packet       s9ListenerQuestionnaire
}

func newS9ListenerFixture(t *testing.T, result bool) s9ListenerFixture {
	t.Helper()
	root, worker := t.TempDir(), t.TempDir()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"], state["runtime_id"] = "1.2.0", "loop-s9-listener"
	mapFieldCLI(state, "configuration")["optimization"] = map[string]any{"packet": true}
	mapFieldCLI(state, "baseline")["generation"] = 1
	phase, status, template, stage := "planning", "planning", "s9-plan-report-v1", "S9-plan"
	if result {
		phase, status, template, stage = "fixing", "repairing", "s9-repair-result-v1", "S9-result"
	}
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": phase}
	state["review"] = map[string]any{"round": 1, "repair": map[string]any{
		"status": status, "session_id": "repair-session-listener", "case_id": "investigation-case-listener",
		"plan_ref": ".claude/repair-plan.json", "plan_sha256": strings.Repeat("a", 64),
		"assignment_owners": map[string]any{"repair-assignment-listener": "builder-listener"},
	}}
	binding := workpacket.RepairPlanReportBinding{
		RuntimeID: "loop-s9-listener", BaselineGeneration: 1, ReviewRound: 1,
		SessionID: "repair-session-listener", PlanRef: ".claude/repair-plan.json", PlanSHA256: strings.Repeat("a", 64),
		RepairAssignmentID: "repair-assignment-listener", ManifestAssignmentID: "assignment-s9-listener", OwnerAgentID: "builder-listener",
	}
	prefix := ".claude/submissions/loop-s9-listener/g1/" + stage + "/r1/repair-assignment-listener/packet-listener/builder-listener/"
	row := map[string]any{"packet_id": "packet-listener", "template_id": template, "view_path": prefix + "view.json", "answer_path": prefix + "answer.json"}
	bindingBytes, _ := json.Marshal(binding)
	var bindingRow map[string]any
	_ = json.Unmarshal(bindingBytes, &bindingRow)
	row["binding"] = bindingRow
	entities := mapFieldCLI(state, "entities")
	entities["agents"] = []any{map[string]any{"id": binding.OwnerAgentID, "role": "backend-builder", "prompt_ref": ".claude/manifest.json#" + binding.ManifestAssignmentID}}
	if result {
		execution := map[string]any{
			"assignment_id": binding.ManifestAssignmentID, "runtime_id": binding.RuntimeID, "baseline_generation": 1, "execution_generation": 1,
			"agent_id": binding.OwnerAgentID, "worktree_path": worker, "status": "ready", "rework_ref": "", "check_location": "worker",
		}
		projected, err := runtime.ProjectRepairResultExecution(execution)
		if err != nil {
			t.Fatal(err)
		}
		row["binding"] = map[string]any{"sources": bindingRow, "execution_sha256": workpacket.Digest(projected), "rework_ref": ""}
		originalExecution := map[string]any{}
		for key, value := range execution {
			originalExecution[key] = value
		}
		row["inputs"] = map[string]any{"execution": originalExecution}
		state["workspace"] = map[string]any{"executions": map[string]any{binding.ManifestAssignmentID: execution}}
	}
	entities["work_packets"] = []any{row}
	packets, err := s9ListenerQuestionnaires(state, "", result)
	if err != nil || len(packets) != 1 {
		t.Fatalf("listener fixture packets=%v err=%v", packets, err)
	}
	f := s9ListenerFixture{root: root, worker: worker, state: state, row: row, packet: packets[0]}
	if !result {
		f.worker = root
	}
	for _, ref := range []string{f.packet.view, f.packet.answer} {
		path := filepath.Join(f.worker, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserved professional answer"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.write(t)
	return f
}

func (f s9ListenerFixture) write(t *testing.T) {
	t.Helper()
	data, _ := json.Marshal(f.state)
	if err := os.MkdirAll(filepath.Join(f.root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".claude/loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f s9ListenerFixture) artifact(t *testing.T, path string, document map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(document)
	full := filepath.Join(f.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0600); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"path": path, "sha256": workpacket.Digest(data)}
}

func TestS9ListenerExistingPacketsReadOnlyWithoutIssueRecoveryOrReceiptProof(t *testing.T) {
	for _, result := range []bool{false, true} {
		f := newS9ListenerFixture(t, result)
		ctx, cancel := context.WithCancel(s9ListenerLiveReadOnlyWitness(t, f.root))
		cancel()
		before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		for _, event := range []string{"SessionStart", "SubagentStart", "PreCompact", "PreToolUse", "Stop", "SubagentStop", "TeammateIdle"} {
			for _, owner := range []string{"Main", "builder-listener"} {
				request := policy.Input{Event: event, AgentID: owner, CWD: f.root}
				if owner == "Main" && (event == "SubagentStart" || event == "SubagentStop" || event == "TeammateIdle") {
					// Native child events cannot borrow Main identity even when a
					// malformed payload claims it explicitly. Keep Worker coverage
					// for these events below and Main coverage on top-level events.
					got := s9QuestionnaireContext(ctx, f.root, request, &submission.Service{}, runtime.Snapshot{State: f.state}, owner, result)
					if strings.Contains(got, f.packet.answer) || strings.Contains(got, f.packet.view) {
						t.Fatalf("child event %s claiming Main inherited Builder responsibility: %s", event, got)
					}
					continue
				}
				// A canceled context and unusable service would expose a repeated
				// named Issue call. Source/journal files needed by full proofs do
				// not exist in this reminder-only fixture.
				got := s9QuestionnaireContext(ctx, f.root, request, &submission.Service{}, runtime.Snapshot{State: f.state}, owner, result)
				if !strings.Contains(got, filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))) || strings.Contains(got, "pending") || strings.Contains(got, "canceled") {
					t.Fatalf("result=%t %s owner=%s: %s", result, event, owner, got)
				}
			}
		}
		got := currentQuestionnaireLifecycleContext(f.root, policy.Input{Event: "SessionStart", AgentID: "builder-listener", Runtime: policy.RuntimeContext{PacketMode: true}})
		if !strings.Contains(got, f.packet.answer) || strings.Contains(got, "S5 accepted") {
			t.Fatalf("S9 entry used generic service/recovery: %s", got)
		}
		after, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		if string(after) != string(before) {
			t.Fatal("reminder changed Runtime")
		}
		if _, err := os.Stat(filepath.Join(f.root, ".claude/loop-events.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("reminder created journal: %v", err)
		}
	}
}

func TestS9ListenerMissingCurrentQuestionOnlyReportsPreparationGap(t *testing.T) {
	for _, result := range []bool{false, true} {
		f := newS9ListenerFixture(t, result)
		answer := filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))
		before, _ := os.ReadFile(answer)
		if err := os.Remove(filepath.Join(f.worker, filepath.FromSlash(f.packet.view))); err != nil {
			t.Fatal(err)
		}
		ctx := s9ListenerLiveReadOnlyWitness(t, f.root)
		request := policy.Input{Event: "Stop", AgentID: "Main", CWD: f.root}
		got := s9QuestionnaireContext(ctx, f.root, request, &submission.Service{}, runtime.Snapshot{State: f.state}, "Main", result)
		if strings.Contains(got, "canceled") || !strings.Contains(got, answer) || !strings.Contains(got, "not prepared") {
			t.Fatalf("Stop prepared missing files: %s", got)
		}
		request.Event = "PreToolUse"
		got = s9QuestionnaireContext(ctx, f.root, request, &submission.Service{}, runtime.Snapshot{State: f.state}, "Main", result)
		if strings.Contains(got, "context canceled") || !strings.Contains(got, answer) || !strings.Contains(got, "Main/system") {
			t.Fatalf("missing view lost Main/system preparation responsibility and exact path: %s", got)
		}
		if _, err := os.Stat(filepath.Join(f.worker, filepath.FromSlash(f.packet.view))); !os.IsNotExist(err) {
			t.Fatalf("ordinary reminder restored missing view: %v", err)
		}
		after, _ := os.ReadFile(answer)
		if string(after) != string(before) {
			t.Fatal("missing view replaced professional answer")
		}
	}
}

func TestS9ListenerMissingResultSelectsOnlyItsRegisteredOwner(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "supplied_sibling", true: "accepted_sibling"}[accepted], func(t *testing.T) {
			f := newS9ListenerFixture(t, true)
			firstAnswer := filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))
			firstBytes, err := os.ReadFile(firstAnswer)
			if err != nil {
				t.Fatal(err)
			}
			if accepted {
				candidate := f.artifact(t, ".claude/candidate-first.json", map[string]any{"provenance": map[string]any{
					"packet_id": f.packet.id, "owner_agent_id": f.packet.binding.OwnerAgentID, "execution_root": f.worker,
				}})
				execution := mapFieldCLI(mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions"), f.packet.binding.ManifestAssignmentID)
				execution["delivery_ref"], execution["delivery_sha256"] = candidate["path"], candidate["sha256"]
			}
			encoded, err := json.Marshal(f.row)
			if err != nil {
				t.Fatal(err)
			}
			var second map[string]any
			if err := json.Unmarshal(encoded, &second); err != nil {
				t.Fatal(err)
			}
			const owner, assignment, manifest = "builder-second", "repair-assignment-second", "assignment-s9-second"
			second["packet_id"] = "packet-second"
			for _, key := range []string{"view_path", "answer_path"} {
				second[key] = strings.NewReplacer(f.packet.binding.OwnerAgentID, owner, f.packet.binding.RepairAssignmentID, assignment, f.packet.id, "packet-second").Replace(stringValue(second[key]))
			}
			sources := mapFieldCLI(mapFieldCLI(second, "binding"), "sources")
			sources["owner_agent_id"], sources["repair_assignment_id"], sources["manifest_assignment_id"] = owner, assignment, manifest
			execution := mapFieldCLI(mapFieldCLI(second, "inputs"), "execution")
			secondRoot := t.TempDir()
			execution["agent_id"], execution["assignment_id"], execution["worktree_path"] = owner, manifest, secondRoot
			delete(execution, "delivery_ref")
			delete(execution, "delivery_sha256")
			projection, err := runtime.ProjectRepairResultExecution(execution)
			if err != nil {
				t.Fatal(err)
			}
			mapFieldCLI(second, "binding")["execution_sha256"] = workpacket.Digest(projection)
			mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions")[manifest] = execution
			mapFieldCLI(repairPointerCLI(f.state), "assignment_owners")[assignment] = owner
			entities := mapFieldCLI(f.state, "entities")
			entities["agents"] = append(entities["agents"].([]any), map[string]any{"id": owner, "role": "backend-builder", "prompt_ref": ".claude/second-manifest.json#" + manifest})
			entities["work_packets"] = append(entities["work_packets"].([]any), second)
			f.write(t)
			packets, err := s9ListenerQuestionnaires(f.state, "", true)
			if err != nil || len(packets) != 2 {
				t.Fatalf("two registered Result questions: %v %v", packets, err)
			}
			if selected := s9ListenerPreparationOwner(f.root, f.state, packets, true); selected != owner {
				t.Fatalf("selected supplied/accepted sibling instead of actual missing owner: %s", selected)
			}
			ctx := s9ListenerLiveReadOnlyWitness(t, f.root)
			before := repairDispatchRuntimeBytes(t, f.root)
			secondAnswer := filepath.Join(secondRoot, filepath.FromSlash(stringValue(second["answer_path"])))
			got := s9QuestionnaireContext(ctx, f.root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: f.root}, &submission.Service{}, runtime.Snapshot{State: f.state}, "Main", true)
			if strings.Contains(got, "context canceled") || !strings.Contains(got, secondAnswer) || !strings.Contains(got, "Main/system") {
				t.Fatalf("named missing-owner preparation lost its exact target: %s", got)
			}
			actual, err := os.ReadFile(firstAnswer)
			if err != nil || string(actual) != string(firstBytes) {
				t.Fatalf("missing sibling preparation replaced the first professional answer: %v", err)
			}
			repairDispatchAssertUnchanged(t, f.root, before)
			for _, key := range []string{"view_path", "answer_path"} {
				path := filepath.Join(secondRoot, filepath.FromSlash(stringValue(second[key])))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("supplied second questionnaire"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if selected := s9ListenerPreparationOwner(f.root, f.state, packets, true); selected != "" {
				t.Fatalf("supplied questions selected another owner for reissuance: %s", selected)
			}
		})
	}
}

func TestS9ListenerAcceptedOriginalArtifactsProjectWithoutFullReceiptProof(t *testing.T) {
	for _, kind := range []string{"plan", "candidate", "result", "blocker", "contract_conflict"} {
		t.Run(kind, func(t *testing.T) {
			f := newS9ListenerFixture(t, kind != "plan")
			receipt := map[string]any{"packet_id": f.packet.id, "owner_agent_id": f.packet.binding.OwnerAgentID, "execution_root": f.worker}
			pointer := repairPointerCLI(f.state)
			switch kind {
			case "plan":
				pointer["plan_report_refs"] = []any{f.artifact(t, ".claude/report.json", map[string]any{"assignment_id": f.packet.binding.RepairAssignmentID, "receipt": receipt})}
			case "result":
				pointer["result_refs"] = []any{f.artifact(t, ".claude/result.json", map[string]any{"receipt": map[string]any{"provenance": receipt}})}
			case "candidate":
				ref := f.artifact(t, ".claude/candidate.json", map[string]any{"provenance": receipt})
				execution := mapFieldCLI(mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions"), f.packet.binding.ManifestAssignmentID)
				execution["delivery_ref"], execution["delivery_sha256"] = ref["path"], ref["sha256"]
			case "blocker", "contract_conflict":
				ref := f.artifact(t, ".claude/blocker.json", map[string]any{"packet_id": f.packet.id, "binding": f.row["binding"], "execution_root": f.worker, "submitted_answer": map[string]any{"verdict": kind}})
				ref["kind"], ref["baseline_generation"], ref["scope_refs"] = "repair_result_blocker", 1, []any{f.packet.id, f.packet.binding.RepairAssignmentID}
				f.state["evidence"] = []any{ref}
			}
			if err := os.Remove(filepath.Join(f.worker, filepath.FromSlash(f.packet.view))); err != nil {
				t.Fatal(err)
			}
			if s9ListenerNeedsPreparation(f.root, f.state, []s9ListenerQuestionnaire{f.packet}, kind != "plan") {
				t.Fatal("accepted question was selected for reissuance")
			}
			got := s9QuestionnaireContext(context.Background(), f.root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: f.root}, nil, runtime.Snapshot{State: f.state}, "Main", kind != "plan")
			if !strings.Contains(got, filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))) || !strings.Contains(got, "recorded") || strings.Contains(got, "work_complete=true") {
				t.Fatalf("accepted fact not safely projected: %s", got)
			}
			if kind == "plan" && (!strings.Contains(got, "same native Builder") || strings.Contains(got, "runtime workspace prepare") || strings.Contains(got, "runtime workspace bind")) {
				t.Fatalf("recorded native Plan was redirected into legacy workspace startup: %s", got)
			}
			if kind == "blocker" && !strings.Contains(got, "main-resolution.json") {
				t.Fatalf("blocker omitted Main's exact resolution path: %s", got)
			}
			if kind == "contract_conflict" && (strings.Contains(got, "main-resolution.json") || !strings.Contains(got, "S8 RepairContract")) {
				t.Fatalf("Contract conflict was sent to a same-Contract retry: %s", got)
			}
		})
	}
}

func TestS9ListenerRecordedProgressDoesNotRebuildHashOrExecutionProof(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	candidate := f.artifact(t, ".claude/candidate-recorded.json", map[string]any{"provenance": map[string]any{
		"packet_id": f.packet.id, "owner_agent_id": f.packet.binding.OwnerAgentID, "execution_root": f.worker,
	}})
	execution := mapFieldCLI(mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions"), f.packet.binding.ManifestAssignmentID)
	execution["delivery_ref"], execution["delivery_sha256"] = candidate["path"], strings.Repeat("b", 64)
	mapFieldCLI(f.row, "binding")["execution_sha256"] = strings.Repeat("c", 64)
	if err := os.Remove(filepath.Join(f.worker, filepath.FromSlash(f.packet.view))); err != nil {
		t.Fatal(err)
	}
	f.write(t)
	before := repairDispatchRuntimeBytes(t, f.root)
	packets, err := s9ListenerQuestionnaires(f.state, "", true)
	if err != nil || len(packets) != 1 || s9ListenerPreparationOwner(f.root, f.state, packets, true) != "" {
		t.Fatalf("reminder reconstructed execution/artifact proof or reissued a recorded candidate: packets=%v err=%v", packets, err)
	}
	ctx := s9ListenerLiveReadOnlyWitness(t, f.root)
	got := s9QuestionnaireContext(ctx, f.root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: f.root}, &submission.Service{}, runtime.Snapshot{State: f.state}, "Main", true)
	if !strings.Contains(got, filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))) || !strings.Contains(got, "Candidate reception is recorded") || !strings.Contains(got, "current professional completion is unknown") || strings.Contains(got, "canceled") || strings.Contains(got, "work_complete=true") {
		t.Fatalf("recorded professional progress required a second proof or lost its exact path: %s", got)
	}
	repairDispatchAssertUnchanged(t, f.root, before)
}

func TestS9ListenerMissingSourceAndUnknownNativeOwnerKeepResponsibilityPaths(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	delete(f.state, "workspace")
	got := s9QuestionnaireContext(context.Background(), f.root, policy.Input{Event: "Stop", AgentID: "Main"}, nil, runtime.Snapshot{State: f.state}, "Main", true)
	if !strings.Contains(got, filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))) || !strings.Contains(got, "no Main-root fallback") {
		t.Fatalf("lost Worker source lost recorded exact path: %s", got)
	}
	if strings.Contains(got, filepath.Join(f.root, filepath.FromSlash(f.packet.answer))) {
		t.Fatalf("Worker source was replaced with Main root: %s", got)
	}
	before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	for _, request := range []policy.Input{{Event: "SubagentStart"}, {Event: "PreToolUse", AgentType: "backend-builder", CWD: f.root}, {Event: "PreToolUse", AgentID: "af085e21-platform-native-id", AgentType: "backend-builder", CWD: f.root}} {
		got = s9QuestionnaireContext(context.Background(), f.root, request, nil, runtime.Snapshot{State: f.state}, request.EffectiveAgentID(), true)
		if !strings.Contains(got, "not identified") || strings.Contains(got, f.packet.answer) || strings.Contains(got, "not currently prepared") {
			t.Fatalf("unknown native owner inherited Main/another worker or reported a false missing question: %s", got)
		}
		if request.AgentID != "" && (!strings.Contains(got, "private worktree outputs") || !strings.Contains(got, "PreToolUse")) {
			t.Fatalf("native platform ID did not retain the Worker's exact original output/return contract: %s", got)
		}
		if request.AgentID != "" {
			paths := s9ListenerKnownPaths(f.root, f.state, request.AgentID, true)
			if strings.Contains(paths, "not prepared") || strings.Contains(paths, f.packet.answer) || !strings.Contains(paths, "save your own ready professional output") || !strings.Contains(paths, "PreToolUse") {
				t.Fatalf("unknown native background guidance reported a false missing question: %s", paths)
			}
		}
	}
	after, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	if string(before) != string(after) {
		t.Fatal("unknown native reminder changed Runtime")
	}
}

func TestS9ListenerMissingPacketRequiresOriginalPreparedWorkerAndPlanReport(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	mapFieldCLI(f.state, "entities")["work_packets"] = []any{}
	if s9ListenerNeedsPreparation(f.root, f.state, nil, true) {
		t.Fatal("owner map alone caused Result issuance before accepted PlanReport")
	}
	repairPointerCLI(f.state)["plan_report_refs"] = []any{f.artifact(t, ".claude/report.json", map[string]any{
		"assignment_id": f.packet.binding.RepairAssignmentID, "agent_id": f.packet.binding.OwnerAgentID,
	})}
	if selected := s9ListenerPreparationOwner(f.root, f.state, nil, true); selected != f.packet.binding.OwnerAgentID {
		t.Fatal("original prepared Worker with accepted PlanReport did not select missing Result")
	}
	delete(f.state, "workspace")
	if s9ListenerNeedsPreparation(f.root, f.state, nil, true) {
		t.Fatal("unprepared sibling caused repeated Result issuance")
	}
}

func TestS9ListenerChangedSourcePreservesOriginalAnswerAndRecordedPath(t *testing.T) {
	f := newS9ListenerFixture(t, true)
	execution := mapFieldCLI(mapFieldCLI(mapFieldCLI(f.state, "workspace"), "executions"), f.packet.binding.ManifestAssignmentID)
	execution["execution_generation"] = 2
	packets, err := s9ListenerQuestionnaires(f.state, "", true)
	if err != nil || len(packets) != 0 {
		t.Fatalf("changed execution kept an old active packet: %v %v", packets, err)
	}
	repairPointerCLI(f.state)["plan_report_refs"] = []any{f.artifact(t, ".claude/report.json", map[string]any{
		"assignment_id": f.packet.binding.RepairAssignmentID, "agent_id": f.packet.binding.OwnerAgentID,
	})}
	before, _ := os.ReadFile(filepath.Join(f.worker, filepath.FromSlash(f.packet.answer)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := s9QuestionnaireContext(ctx, f.root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: f.root}, &submission.Service{}, runtime.Snapshot{State: f.state}, "Main", true)
	if strings.Contains(got, "context canceled") || !strings.Contains(got, filepath.Join(f.worker, filepath.FromSlash(f.packet.answer))) {
		t.Fatalf("changed-source preparation lost recorded path: %s", got)
	}
	after, _ := os.ReadFile(filepath.Join(f.worker, filepath.FromSlash(f.packet.answer)))
	if string(before) != string(after) {
		t.Fatal("changed source overwrote original professional answer")
	}
}

func TestS9ListenerPreparedBuilderAndIndependentVerifierDoNotReissuePlan(t *testing.T) {
	f := newS9ListenerFixture(t, false)
	pointer := repairPointerCLI(f.state)
	plan := f.artifact(t, ".claude/repair-plan.json", map[string]any{"plan_id": "repair-plan-listener", "assignments": []any{map[string]any{"assignment_id": f.packet.binding.RepairAssignmentID}}})
	pointer["plan_sha256"] = plan["sha256"]
	mapFieldCLI(f.row, "binding")["plan_sha256"] = plan["sha256"]
	verificationID := repair.VerificationAssignmentID(f.packet.binding.RepairAssignmentID)
	mapFieldCLI(pointer, "assignment_owners")[verificationID] = "independent-verifier"
	entities := mapFieldCLI(f.state, "entities")
	entities["agents"] = append(entities["agents"].([]any), map[string]any{"id": "independent-verifier", "role": "test-builder", "prompt_ref": ".claude/verifier-manifest.json#" + verificationID})
	f.write(t)
	packets, err := s9ListenerQuestionnaires(f.state, "", false)
	if err != nil || len(packets) != 1 || s9ListenerNeedsPreparation(f.root, f.state, packets, false) {
		t.Fatalf("independent verifier created a missing Builder question: %v %v", packets, err)
	}
	before, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	ctx, cancel := context.WithCancel(s9ListenerLiveReadOnlyWitness(t, f.root))
	cancel()
	got := s9QuestionnaireContext(ctx, f.root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: f.root}, &submission.Service{}, runtime.Snapshot{State: f.state}, "Main", false)
	if strings.Contains(got, "canceled") || !strings.Contains(got, filepath.Join(f.root, f.packet.answer)) {
		t.Fatalf("ordinary Pre invoked original Issue for verifier-only missing packet: %s", got)
	}
	after, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	if string(before) != string(after) {
		t.Fatal("ordinary Pre changed Runtime")
	}
	if selected := s9ListenerPreparationOwner(f.root, f.state, nil, false); selected != f.packet.binding.OwnerAgentID {
		t.Fatal("real missing Plan Assignment no longer prepares its Builder question")
	}
}

// Live context permits the file/provenance observations this test requests.
// The unusable Service passed by callers and absent journal prevent original
// Issue/recovery or full receipt proofs from succeeding. No packet body may be
// loaded, and the existing Runtime pair must stay byte-for-byte unchanged.
func s9ListenerLiveReadOnlyWitness(t *testing.T, root string) context.Context {
	t.Helper()
	journal := filepath.Join(root, ".claude/loop-events.jsonl")
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatalf("listener fixture unexpectedly has a full-proof journal: %v", err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	reads := 0
	ctx := packetbody.WithResolver(t.Context(), func(context.Context, packetbody.Reference) ([]byte, error) {
		reads++
		return nil, errors.New("ordinary listener must not load a packet body or rebuild acceptance proof")
	})
	t.Cleanup(func() {
		if reads != 0 {
			t.Errorf("ordinary listener loaded %d packet bodies", reads)
		}
		repairDispatchAssertUnchanged(t, root, before)
		if _, err := os.Stat(journal); !os.IsNotExist(err) {
			t.Errorf("ordinary listener created proof/recovery journal: %v", err)
		}
	})
	return ctx
}
