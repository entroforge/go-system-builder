package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// The fixture boundary is an existing registered S5 document baseline. Every
// workgroup, activation, packet, save, domain reception and route below uses
// the production CLI/Writer; no accepted review or working agent is seeded.
type stageAnswerFixture struct {
	root, taskID, taskPath string
	packets                []workpacket.Packet
}

func stageJSON(t *testing.T, root, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func stageCLI(t *testing.T, args ...string) string {
	t.Helper()
	var out, errout bytes.Buffer
	if code := cli.Run(args, nil, &out, &errout); code != 0 {
		t.Fatalf("CLI %v: code %d: %s\n%s", args, code, errout.String(), out.String())
	}
	return out.String()
}

func stageSnapshot(t *testing.T, root string) runtime.Snapshot {
	t.Helper()
	s, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newStageAnswerFixture(t *testing.T) stageAnswerFixture {
	return newStageAnswerFixtureMode(t, true)
}

func newStageAnswerFixtureMode(t *testing.T, packet bool, configure ...func(string, map[string]any)) stageAnswerFixture {
	t.Helper()
	return newStageAnswerFixtureIssuance(t, packet, true, configure...)
}

func newStageAnswerFixtureIssuance(t *testing.T, packet, issue bool, configure ...func(string, map[string]any)) stageAnswerFixture {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "document_verification", "", 0)
	state["review"].(map[string]any)["round"] = 0
	fixtures.SeedDocumentPassS5(t, root, state, "unused-spec", "unused-task")
	for _, raw := range state["documents"].([]any) {
		d := raw.(map[string]any)
		if d["kind"] == "req" {
			bound := state["bound_req"].(map[string]any)
			bound["sha256"], bound["version"] = d["sha256"], d["version"]
		}
	}
	state["evidence"] = []any{}
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": packet}
	for _, rel := range []string{"agents/document-verifier.md", "skills/document-verification/SKILL.md", "skills/agent-dispatch/SKILL.md", "skills/specification-planning/SKILL.md", "skills/api-contracts/SKILL.md", "skills/testing-strategy/SKILL.md"} {
		data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, ".claude", rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// New-context fixtures ship the same protocol/rule authority as a real
	// installation; they still seed no activation or professional conclusion.
	for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules"} {
		source := filepath.Join(fixtures.RepoRoot(t), rel)
		if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(fixtures.RepoRoot(t), path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			dest := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0600)
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, setup := range configure {
		setup(root, state)
	}
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	// The generic fixture binding has a deliberate marker SHA. Replace it
	// before real preparation with this repository's actual committed HEAD.
	snap := stageSnapshot(t, root)
	head, err := workspace.Git(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	snap.State["bound_req"].(map[string]any)["workspace"].(map[string]any)["bound_commit"] = head
	stageJSON(t, root, ".claude/loop-state.json", snap.State)
	asset, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(asset, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = "team-manifest-stage-answer"
	manifest["runtime_id"] = state["runtime_id"]
	manifest["req_id"] = "REQ-039"
	manifest["baseline_generation"] = 1
	manifest["review_round"] = nil
	manifest["workgroup_id"] = "workgroup-stage-answer"
	manifest["workgroup_kind"] = "document_verifier"
	manifest["planned_agent_count"] = 2
	manifest["max_parallel_agents"] = 2
	manifest["risk_tags"] = []any{}
	manifest["quantity_rationale"] = "Two independent mandatory document review responsibilities."
	docs := []any{}
	f := stageAnswerFixture{root: root}
	for _, raw := range state["documents"].([]any) {
		d := raw.(map[string]any)
		docs = append(docs, map[string]any{"id": d["id"], "path": d["path"], "version": d["version"], "sha256": d["sha256"]})
		if d["kind"] == "task" {
			f.taskID = d["id"].(string)
			f.taskPath = d["path"].(string)
		}
	}
	manifest["documents"] = docs
	base := manifest["assignments"].([]any)[0].(map[string]any)
	assignments, dispositions := []any{}, []any{}
	for i, resp := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		owner, id := []string{"reviewer-spec", "reviewer-task"}[i], []string{"assignment-spec", "assignment-task"}[i]
		a := map[string]any{}
		for k, v := range base {
			a[k] = v
		}
		a["assignment_id"], a["agent_id"], a["responsibility_id"] = id, owner, resp
		a["role_family"], a["agent_definition_ref"] = "document-verifier", ".claude/agents/document-verifier.md"
		a["skill_refs"] = []string{"document-verification"}
		if resp == "DV-SPEC-CONSISTENCY" {
			a["skill_refs"] = []string{"document-verification", "specification-planning", "api-contracts"}
		} else {
			a["skill_refs"] = []string{"document-verification", "specification-planning", "testing-strategy"}
		}
		a["read_paths"] = []string{f.taskPath}
		a["write_paths"] = []string{}
		a["output_paths"] = []string{"docs/reports/review/" + owner + ".json"}
		a["required_checks"] = []string{}
		a["done_when"] = []string{"the original domain gate consumes this independent conclusion"}
		a["target_branch"] = "test-development"
		a["dispatch_mode"] = "plan_checkpoint"
		assignments = append(assignments, a)
		dispositions = append(dispositions, map[string]any{"responsibility_id": resp, "disposition": "assigned", "trigger": "mandatory S5", "assignment_ids": []string{id}, "na_rationale": nil, "evidence_ref": nil})
	}
	manifest["assignments"], manifest["responsibility_dispositions"] = assignments, dispositions
	manifest["separation_edges"] = []any{map[string]any{"left_assignment_id": "assignment-spec", "right_assignment_id": "assignment-task", "reason": "different_conclusion"}}
	manifestPath := ".claude/workgroups/REQ-039/review/manifest.json"
	stageJSON(t, root, manifestPath, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestPath, "--task-id", f.taskID, "--task", f.taskPath)
	if !issue {
		return f
	}
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	f.packets, err = s.Issue("")
	wantPackets := 0
	if packet {
		wantPackets = 2
	}
	if err != nil || len(f.packets) != wantPackets {
		t.Fatalf("issuance: %v packets=%d want=%d", err, len(f.packets), wantPackets)
	}
	return f
}

func (f stageAnswerFixture) begin(t *testing.T, p workpacket.Packet, worker *workspace.Execution) {
	t.Helper()
	asset, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(asset, &messages); err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	for _, m := range messages {
		if m["message_type"] == "plan_report" {
			plan = m
			break
		}
	}
	plan["runtime_id"], plan["agent_id"], plan["assignment_id"] = p.Binding.RuntimeID, p.Binding.Owner, p.Binding.AssignmentID
	plan["agent_definition_ref"] = ".claude/agents/document-verifier.md"
	plan["team_id"], plan["task_id"], plan["assignment_revision"] = "workgroup-stage-answer", f.taskID, 1
	plan["planned_paths"] = []string{p.AnswerPath}
	plan["objective"] = "Independently verify the frozen document responsibility."
	path := ".claude/evidence/plan-" + p.Binding.Owner + ".json"
	if worker != nil {
		registry, _, err := workspace.Load(f.root)
		if err != nil {
			t.Fatalf("read registered Worker submission paths: %v", err)
		}
		if registry == nil {
			t.Fatal("Worker plan fixture requires the registered workspace")
		}
		submissionPaths, err := registry.WorkerSubmissionPaths(*worker)
		if err != nil {
			t.Fatalf("resolve frozen Worker submission paths: %v", err)
		}
		if submissionPaths.Plan == "" {
			return
		}
		stageJSON(t, worker.Path, submissionPaths.Plan, plan)
		stageCLI(t, "runtime", "workspace", "begin", "--root", worker.Path, "--assignment", worker.AssignmentID, "--agent", worker.AgentID)
	} else {
		stageJSON(t, f.root, path, plan)
		stageCLI(t, "runtime", "agent-begin", "--root", f.root, "--agent-id", p.Binding.Owner, "--plan", path)
	}
}

// Frozen teammate save compatibility; native delivery uses nativeReviewEvent/Stop.
func saveStageHook(t *testing.T, root, cwd, owner, path string, answer workpacket.Draft, binary ...string) submission.Receipt {
	t.Helper()
	stageJSON(t, cwd, path, answer)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(cwd, path)}, "teammate_name": owner, "cwd": cwd})
	return saveStageHookPayload(t, root, body, binary...)
}

func saveStageHookPayload(t *testing.T, root string, body []byte, binary ...string) submission.Receipt {
	t.Helper()
	var out, errout bytes.Buffer
	var code int
	if len(binary) > 0 {
		var stdoutText, stderrText string
		code, stdoutText, stderrText = factory.RunHook(t, binary[0], root, "PostToolUse", body)
		out.WriteString(stdoutText)
		errout.WriteString(stderrText)
	} else {
		code = cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout)
	}
	if code != 0 {
		t.Fatalf("save Hook: %d %s", code, errout.String())
	}
	var envelope struct {
		Specific struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("Hook envelope %s: %v", out.String(), err)
	}
	const prefix = "S5 SUBMISSION RECEIPT: "
	if len(envelope.Specific.Context) < len(prefix) {
		t.Fatalf("missing receipt: %s %s", out.String(), errout.String())
	}
	var r submission.Receipt
	receiptBytes := []byte(envelope.Specific.Context[len(prefix):])
	if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", receiptBytes); err != nil {
		t.Fatalf("public S5 receipt differs from the shared schema: %v\n%s", err, receiptBytes)
	}
	if err := json.Unmarshal(receiptBytes, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func completedDraft(p workpacket.Packet, conclusion string) workpacket.Draft {
	d := p.Draft
	d.Answer = workpacket.Answer{Conclusion: &conclusion, Findings: []workpacket.Finding{}}
	if conclusion != "pass" {
		d.Answer.Findings = []workpacket.Finding{{Path: p.Inputs[0].Path + "#scope", Expected: "unambiguous executable scope", Observed: "a concrete ambiguity"}}
	}
	return d
}

func TestMainS5ContextReturnsEveryCurrentQuestionnairePath(t *testing.T) {
	f := newStageAnswerFixture(t)
	body := `{"hook_event_name":"SessionStart","session_id":"session-s5-questionnaires","cwd":"` + f.root + `"}`
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "SessionStart", "--root", f.root}, strings.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("SessionStart Hook: code=%d stderr=%s", code, errout.String())
	}
	context := out.String()
	snapshot := stageSnapshot(t, f.root)
	for _, packet := range f.packets {
		view := filepath.Join(f.root, filepath.FromSlash(packet.ViewPath))
		answer := filepath.Join(f.root, filepath.FromSlash(packet.AnswerPath))
		if !strings.Contains(context, view) || !strings.Contains(context, answer) {
			t.Fatalf("Main context omitted exact S5 questionnaire paths for %s: %s", packet.Binding.Owner, context)
		}
		loaded, err := hookctx.FromSnapshot(f.root, packet.Binding.Owner, snapshot)
		if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil || !loaded.PolicyContext.Agent.DocumentReviewAssignment || loaded.PolicyContext.Agent.PlanReportedRef != "" {
			t.Fatalf("S5 current Assignment depended on generic PLAN for Hook context: loaded=%+v err=%v", loaded, err)
		}
		foundAnswer := false
		for _, path := range loaded.PolicyContext.WorkPacketAnswerPaths {
			if path == packet.AnswerPath {
				foundAnswer = true
			}
		}
		if !foundAnswer {
			t.Fatalf("S5 Hook context omitted exact issued answer path %s: %+v", packet.AnswerPath, loaded.PolicyContext.WorkPacketAnswerPaths)
		}
	}
	if strings.Contains(context, "PLAN_REPORT") || strings.Contains(context, "agent-begin") {
		t.Fatalf("normal S5 questionnaire context retained generic PLAN instructions: %s", context)
	}
}

func TestStageAnswerSaveUsesS5QuestionnaireWithoutGenericPlan(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass"))
	if !r.Accepted || !r.WorkComplete {
		t.Fatalf("exact issued S5 professional answer did not reach the original consumer without generic PLAN: %+v", r)
	}
}

func TestStageAnswerConsumeSelectsCurrentPacketOwnerWithoutAgent(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	path := filepath.Join(f.root, filepath.FromSlash(p.AnswerPath))
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "consume", "--root", f.root, "--path", path}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("consume exact issued answer without --agent: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var receipt struct {
		Accepted     bool `json:"accepted"`
		WorkComplete bool `json:"work_complete"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
		t.Fatalf("decode original S5 receipt: %v: %s", err, stdout.String())
	}
	if !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("exact registered answer did not reach its original consumer: %+v", receipt)
	}
}

func TestS5SavedAnswerUsesRegisteredOwnerWithoutHookIdentityOrCWD(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata bool
	}{
		{name: "unrelated metadata", metadata: true},
		{name: "missing metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			p := f.packets[0]
			stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
			payload := map[string]any{
				"hook_event_name": "PostToolUse", "tool_name": "Write",
				"tool_input": map[string]any{"file_path": filepath.Join(f.root, filepath.FromSlash(p.AnswerPath))},
			}
			if tc.metadata {
				payload["agent_id"] = "unregistered-hook-actor"
				payload["teammate_name"] = "another-unregistered-actor"
				payload["cwd"] = t.TempDir()
				payload["session_id"] = "unregistered-session"
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			{
				// Actual native actor metadata selects the lifecycle boundary,
				// not the logical owner or a per-save reception callback.
				before := stageSnapshot(t, f.root)
				var out, diagnostic bytes.Buffer
				if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &diagnostic); code != 0 || strings.Contains(out.String(), "SUBMISSION RECEIPT") || tc.metadata && out.Len() != 0 || stageSnapshot(t, f.root).Revision != before.Revision {
					t.Fatalf("ordinary save received a native answer before Stop: %d %s %s", code, &out, &diagnostic)
				}
				payload["hook_event_name"] = "SubagentStop"
				payload["agent_id"] = "actual-native-reviewer"
				payload["last_assistant_message"] = filepath.Join(f.root, p.AnswerPath)
				body, _ = json.Marshal(payload)
				if code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStop"}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
					t.Fatalf("native Stop: %d %s %s", code, &out, &diagnostic)
				}
				record, found, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, p.ID)
				if err != nil || !found || record.Owner != p.Binding.Owner {
					t.Fatalf("native Stop did not preserve original packet owner: %+v %v", record, err)
				}
				return
			}
		})
	}
}

func TestStageAnswerLastSaveReceivesAndRoutesOriginalS5(t *testing.T) {
	for _, verdict := range []string{"pass", "fix_required", "req_change_required"} {
		t.Run(verdict, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			receivedCalls := int64(0)
			for _, p := range f.packets {
				f.begin(t, p, nil)
			}
			for _, p := range f.packets {
				if stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] != "document_verification" {
					break
				}
				r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, verdict))
				receivedCalls++
				t.Logf("receipt: %+v", r)
				if !r.Accepted || !r.WorkComplete {
					t.Fatalf("legitimate judgment did not end the owner delivery: %+v", r)
				}
				if err := schema.NewEmbeddedValidator().ValidateBytes("submission-receipt.schema.json", mustStageJSON(t, r)); err != nil {
					t.Fatal(err)
				}
				if r.CurrentStage != "S5" {
					before := stageSnapshot(t, f.root)
					replay := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, verdict))
					receivedCalls++
					if !replay.WorkComplete || stageSnapshot(t, f.root).Revision != before.Revision {
						t.Fatal("lost transition response caused a second reception or transition")
					}
					for _, waiting := range replay.WaitingOn {
						if waiting.Reason == "system_followup" {
							t.Fatalf("processed replay reported a false pending system followup: %+v", replay)
						}
					}
				}
			}
			snap := stageSnapshot(t, f.root)
			want := map[string]string{"pass": "building", "fix_required": "planning", "req_change_required": "document_verification"}[verdict]
			if snap.State["lifecycle"].(map[string]any)["state"] != want {
				result, _ := controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: f.root, CWD: f.root, Event: "PreToolUse", ToolName: "Read"})
				t.Logf("read-only probe: %+v error=%s", result.QualityGate, result.Error)
				t.Fatalf("route %s: %v; milestone=%v", verdict, snap.State["lifecycle"], snap.State["milestone"])
			}
			observed, err := metrics.NewStore(f.root).Read()
			if err != nil || len(observed.SubmissionSaves) != 1 || observed.SubmissionSaves["received_"+verdict] != receivedCalls || observed.SubmissionSaveDuration["received_"+verdict].Count != receivedCalls {
				t.Fatalf("professional outcome mixed with gate PASS/unique delivery: %+v %v", observed, err)
			}
		})
	}
}

func mustStageJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestIndependentStageAnswerFrozenSourceAndReceipt(t *testing.T) {
	testIndependentStageAnswerFrozenReceipt(t, false)
}

func TestIndependentStageAnswerFrozenSourceAndDeferredReceipt(t *testing.T) {
	testIndependentStageAnswerFrozenReceipt(t, true)
}

func testIndependentStageAnswerFrozenReceipt(t *testing.T, deferControl bool) {
	t.Helper()
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", p.Binding.AssignmentID, "--agent", p.Binding.Owner)
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	e := b.Executions[p.Binding.AssignmentID]
	if e.Purpose != workspace.DocumentReviewPurpose || len(e.WritePaths) != 0 || len(e.Checks) != 0 {
		t.Fatalf("review execution widened product authority: %+v", e)
	}
	before := stageSnapshot(t, f.root)
	f.begin(t, p, &e)
	answer := completedDraft(p, "pass")
	var r submission.Receipt
	if deferControl {
		// Force the original pending control boundary, keeping the actual
		// independent Worker source, domain Writer and Main recovery path.
		stageJSON(t, e.Path, workpacket.WorkerAnswerPath, answer)
		s, err := submission.New(f.root)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
		r, err = s.Save(ctx, p.Binding.Owner, filepath.Join(e.Path, workpacket.WorkerAnswerPath))
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	} else {
		r = saveStageHook(t, f.root, e.Path, p.Binding.Owner, workpacket.WorkerAnswerPath, answer)
	}
	if !r.Accepted || !r.WorkComplete {
		t.Fatalf("Worker answer: %+v", r)
	}
	first := stageSnapshot(t, f.root)
	firstRecord, ok, err := runtime.FindSubmission(first.State, p.ID)
	if err != nil || !ok {
		t.Fatalf("original Worker reception missing: %+v %v", firstRecord, err)
	}
	if deferControl && firstRecord.ControlStatus != "pending" {
		t.Fatalf("controlled Worker cancellation did not exercise pending recovery: %+v", firstRecord)
	}
	if firstRecord.ControlStatus == "pending" {
		// A received answer can legitimately defer the original control step.
		// Settle that Main responsibility before asserting a processed replay
		// leaves the revision unchanged; never resubmit the professional work.
		stageCLI(t, "runtime", "stage-answer", "recover", "--root", f.root)
		recovered := stageSnapshot(t, f.root)
		record, ok, err := runtime.FindSubmission(recovered.State, p.ID)
		if err != nil || !ok || record.Key != firstRecord.Key || record.EvidenceID != firstRecord.EvidenceID || record.ContentHash != firstRecord.ContentHash || record.ControlStatus != "no_transition" || len(first.State["evidence"].([]any)) != len(recovered.State["evidence"].([]any)) {
			t.Fatalf("Main recovery changed Worker delivery or failed its original gate: %+v %v", record, err)
		}
		first = recovered
	}
	r = saveStageHook(t, f.root, e.Path, p.Binding.Owner, workpacket.WorkerAnswerPath, answer)
	if !r.WorkComplete || stageSnapshot(t, f.root).Revision != first.Revision {
		t.Fatalf("same saved answer appended a second reception or processed control cycle: first=%+v receipt=%+v", firstRecord, r)
	}
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(context.Background(), p.Binding.Owner, p.AnswerPath); err == nil {
		t.Fatal("declared Worker fell back to shared Main authoring")
	}
	if before.Revision >= first.Revision {
		t.Fatal("no real domain reception")
	}
	if err := b.ValidateBootstrap(e); err != nil {
		t.Fatalf("authoring answer entered the immutable bootstrap receipt: %v", err)
	}
}

func (f stageAnswerFixture) prepare(t *testing.T, p workpacket.Packet) workspace.Execution {
	t.Helper()
	stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", p.Binding.AssignmentID, "--agent", p.Binding.Owner)
	b, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	return b.Executions[p.Binding.AssignmentID]
}

func stageHookCode(t *testing.T, root, cwd, owner, event, path string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "agent_id": owner, "cwd": cwd, "runtime_context": map[string]any{"runtime_id": "forged", "agent": map[string]any{"state": "working"}, "packet_mode": false}})
	var out, errout bytes.Buffer
	code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &errout)
	return code, errout.String() + out.String()
}

func TestIndependentReviewStopsOnlyItsOwnResponsibility(t *testing.T) {
	for _, verdict := range []string{"pass", "fix_required"} {
		t.Run(verdict, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			p := f.packets[0]
			e := f.prepare(t, p)
			before := stageSnapshot(t, f.root)
			if code, msg := stageHookCode(t, f.root, e.Path, e.AgentID, "Stop", ""); code != 2 || !strings.Contains(msg, filepath.Join(e.Path, workpacket.WorkerAnswerPath)) || !strings.Contains(msg, "/answer/conclusion") || strings.Contains(msg, "plan/activation") {
				t.Fatalf("unreceived Stop must remind with the exact questionnaire path: %d %s", code, msg)
			}
			if stageSnapshot(t, f.root).Revision != before.Revision {
				t.Fatal("blocked Stop mutated the control Runtime")
			}
			if code, msg := stageHookCode(t, f.root, e.Path, e.AgentID, "PreToolUse", filepath.Join(e.Path, workpacket.WorkerAnswerPath)); code != 0 || strings.Contains(msg, `"permissionDecision":"deny"`) {
				t.Fatalf("questionnaire listener denied the tool: %d %s", code, msg)
			}
			f.begin(t, p, &e)
			if code, msg := stageHookCode(t, f.root, e.Path, e.AgentID, "PreToolUse", filepath.Join(e.Path, "product.go")); code != 0 || strings.Contains(msg, `"permissionDecision":"deny"`) {
				t.Fatalf("listener must not enforce tool scope: %d %s", code, msg)
			}
			r := saveStageHook(t, f.root, e.Path, e.AgentID, workpacket.WorkerAnswerPath, completedDraft(p, verdict))
			if !r.WorkComplete {
				t.Fatalf("accepted professional judgment is incomplete: %+v", r)
			}
			before = stageSnapshot(t, f.root)
			if code, msg := stageHookCode(t, f.root, e.Path, e.AgentID, "Stop", ""); code != 0 {
				t.Fatalf("Main/peer pending reawakened completed reviewer: %d %s", code, msg)
			}
			for i := 0; i < 5; i++ {
				body, _ := json.Marshal(map[string]any{
					"hook_event_name": "PostToolUse", "tool_name": "Read",
					"session_id": "received-review-progress", "agent_id": e.AgentID, "cwd": e.Path,
					"tool_input": map[string]any{"file_path": filepath.Join(f.root, ".claude/loop-state.json")},
				})
				var stdout, stderr bytes.Buffer
				code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &stdout, &stderr)
				if code != 0 || strings.Contains(stdout.String(), "WORK PROGRESS:") || strings.Contains(stderr.String(), "work progress coverage incomplete") {
					t.Fatalf("progress disagreed with the original accepted review after %s: code=%d stdout=%s stderr=%s", verdict, code, stdout.String(), stderr.String())
				}
			}
			if stageSnapshot(t, f.root).Revision != before.Revision {
				t.Fatal("Worker Stop ran Main's unrelated control cycle")
			}
		})
	}
}

func TestIndependentReviewRejectsCorruptSourceWithoutReception(t *testing.T) {
	for _, change := range []string{"owner", "view", "frozen input", "symlink", "old entry"} {
		t.Run(change, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			p := f.packets[0]
			e := f.prepare(t, p)
			f.begin(t, p, &e)
			stageJSON(t, e.Path, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
			owner := p.Binding.Owner
			s, err := submission.New(f.root)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "owner":
				owner = "unregistered-reviewer"
			case "view":
				if err := os.Chmod(filepath.Join(e.Path, workpacket.WorkerViewPath), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.Path, workpacket.WorkerViewPath), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "frozen input":
				if err := os.Chmod(filepath.Join(e.Path, ".claude/inputs", p.Inputs[0].Path), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.Path, ".claude/inputs", p.Inputs[0].Path), []byte("drifted"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				path := filepath.Join(e.Path, workpacket.WorkerAnswerPath)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.root, p.AnswerPath), path); err != nil {
					t.Fatal(err)
				}
			case "old entry":
				snap := stageSnapshot(t, f.root)
				snap.State["lifecycle"].(map[string]any)["phase_revision"] = 1
				stageJSON(t, f.root, ".claude/loop-state.json", snap.State)
			}
			before := stageSnapshot(t, f.root)
			if r, err := s.Save(context.Background(), owner, filepath.Join(e.Path, workpacket.WorkerAnswerPath)); err == nil || r.Accepted || r.WorkComplete {
				t.Fatalf("bad source accepted: %+v %v", r, err)
			}
			after := stageSnapshot(t, f.root)
			if after.Revision != before.Revision || len(after.State["evidence"].([]any)) != 0 {
				t.Fatal("rejected source appended a domain fact")
			}
		})
	}
}

func TestConcurrentIndependentReviewersDoNotLoseAnswersOrDoubleAdvance(t *testing.T) {
	f := newStageAnswerFixture(t)
	executions := make([]workspace.Execution, len(f.packets))
	for i, p := range f.packets {
		executions[i] = f.prepare(t, p)
		f.begin(t, p, &executions[i])
		stageJSON(t, executions[i].Path, workpacket.WorkerAnswerPath, completedDraft(p, "pass"))
	}
	receipts := make([]submission.Receipt, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	for i, p := range f.packets {
		group.Add(1)
		go func(i int, p workpacket.Packet) {
			defer group.Done()
			s, err := submission.New(f.root)
			if err != nil {
				errs[i] = err
				return
			}
			receipts[i], errs[i] = s.Save(context.Background(), p.Binding.Owner, filepath.Join(executions[i].Path, workpacket.WorkerAnswerPath))
		}(i, p)
	}
	group.Wait()
	for i, r := range receipts {
		if errs[i] != nil || !r.Accepted || !r.WorkComplete {
			t.Fatalf("concurrent owner %d: %+v %v", i, r, errs[i])
		}
	}
	snap := stageSnapshot(t, f.root)
	if snap.State["lifecycle"].(map[string]any)["state"] != "building" || len(snap.State["entities"].(map[string]any)["submissions"].([]any)) != 2 || len(snap.State["evidence"].([]any)) != 2 {
		t.Fatalf("concurrent S5 reception lost domain facts or original PASS route: %v", snap.State["lifecycle"])
	}
	data, err := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var event map[string]any
		if json.Unmarshal(line, &event) == nil && event["transition_id"] == "TR-003" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d TR-003 commits for the two independent answers", count)
	}
}

// Cancellation observes the original committed reception, never a source-read
// count or candidate validation. The Writer has cleared its pending marker
// before this context interrupts the separate control followup.
type cancelAfterDurableReception struct {
	context.Context
	statePath, packetID string
	cancel              context.CancelFunc
}

func (c *cancelAfterDurableReception) Err() error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(c.statePath)
	var state map[string]any
	if err == nil && json.Unmarshal(data, &state) == nil {
		record, found, readErr := runtime.FindSubmission(state, c.packetID)
		if readErr == nil && found && record.ControlStatus == "pending" {
			if _, markerErr := os.Stat(c.statePath + ".commit-pending.json"); os.IsNotExist(markerErr) {
				c.cancel()
			}
		}
	}
	return c.Context.Err()
}

// This distinct injection cancels while the candidate is validated but no
// pending commit has been published. It must not claim a durable reception.
type cancelValidatedReception struct {
	runtime.CandidateValidator
	packetID string
	cancel   context.CancelFunc
}

func (v cancelValidatedReception) ValidateCandidate(root string, state map[string]any) error {
	if err := v.CandidateValidator.ValidateCandidate(root, state); err != nil {
		return err
	}
	if r, ok, err := runtime.FindSubmission(state, v.packetID); err != nil {
		return err
	} else if ok && r.ControlStatus == "pending" {
		v.cancel()
	}
	return nil
}

func TestStageAnswerCanceledBeforePendingDoesNotClaimReception(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	f.begin(t, p, nil)
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	service, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(f.root, ".claude/loop-state.json")
	journalPath := filepath.Join(f.root, ".claude/loop-events.jsonl")
	beforeState, beforeJournal := mustStageFile(t, statePath), mustStageFile(t, journalPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	originalValidator := service.Validator
	service.Validator = cancelValidatedReception{CandidateValidator: originalValidator, packetID: p.ID, cancel: cancel}
	received, err := service.Save(ctx, p.Binding.Owner, p.AnswerPath)
	if !errors.Is(err, context.Canceled) || received.Accepted || received.WorkComplete {
		t.Fatalf("pre-commit cancellation claimed reception: %+v %v", received, err)
	}
	if !bytes.Equal(beforeState, mustStageFile(t, statePath)) || !bytes.Equal(beforeJournal, mustStageFile(t, journalPath)) {
		t.Fatal("pre-commit cancellation changed the original pair")
	}
	if _, err := os.Stat(statePath + ".commit-pending.json"); !os.IsNotExist(err) {
		t.Fatalf("pre-commit cancellation published pending: %v", err)
	}
	service.Validator = originalValidator
	replay, err := service.Save(context.Background(), p.Binding.Owner, p.AnswerPath)
	if err != nil || !replay.Accepted || !replay.WorkComplete {
		t.Fatalf("unreceived original answer could not resume: %+v %v", replay, err)
	}
}

func mustStageFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStageAnswerDomainCommitSurvivesInterruptedFollowup(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	f.begin(t, p, nil)
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	r, err := s.Save(ctx, p.Binding.Owner, p.AnswerPath)
	if err != nil || !r.Accepted || !r.WorkComplete || !r.ConsumerPending || r.ConsumerOwner != "main" {
		t.Fatalf("interrupted followup lost durable owner completion: %+v %v", r, err)
	}
	snap := stageSnapshot(t, f.root)
	record, ok, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !ok || record.ControlStatus != "pending" || !strings.Contains(r.NextAction, record.FollowupID) {
		t.Fatalf("interruption did not leave the original durable pending event: %+v %v", record, err)
	}
	recovered, err := s.Recover(context.Background(), p.Binding.Owner, 2)
	if err != nil || len(recovered) != 1 || !recovered[0].WorkComplete || recovered[0].ConsumerPending {
		t.Fatalf("pending reception was not recovered: %+v %v", recovered, err)
	}
	first := stageSnapshot(t, f.root)
	if r, err := s.Save(context.Background(), p.Binding.Owner, p.AnswerPath); err != nil || !r.WorkComplete || stageSnapshot(t, f.root).Revision != first.Revision {
		t.Fatalf("recovery required another owner submission: %+v %v", r, err)
	}
}

func TestLastS5ReviewerControlInterruptionRecoversOriginalTR003WithoutResubmission(t *testing.T) {
	f := newStageAnswerFixture(t)
	for i, p := range f.packets {
		f.begin(t, p, nil)
		if i == 0 {
			if !saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass")).WorkComplete {
				t.Fatal("first original responsibility not received")
			}
			continue
		}
		stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
		s, err := submission.New(f.root)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
		r, err := s.Save(ctx, p.Binding.Owner, p.AnswerPath)
		cancel()
		if err != nil || !r.Accepted || !r.WorkComplete || r.CurrentStage != "S5" {
			t.Fatal("late cancellation lost original last responsibility", r, err)
		}
		pending := false
		for _, w := range r.WaitingOn {
			if w.Reason == "system_followup" && w.Owner == "Main" {
				pending = true
			}
		}
		if !pending {
			t.Fatal("control deferral was not exposed to Main", r)
		}
	}
	before := stageSnapshot(t, f.root)
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(context.Background(), "", 2); err != nil {
		t.Fatal(err)
	}
	after := stageSnapshot(t, f.root)
	if after.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatal("original pending last answer did not route through TR-003", after.State["lifecycle"])
	}
	beforeSubmissions := before.State["entities"].(map[string]any)["submissions"].([]any)
	afterSubmissions := after.State["entities"].(map[string]any)["submissions"].([]any)
	if len(beforeSubmissions) != 2 || len(afterSubmissions) != 2 || len(before.State["evidence"].([]any)) != len(after.State["evidence"].([]any)) {
		t.Fatal("recovery manufactured another result")
	}
	for _, p := range f.packets {
		r, err := s.ReceiptPacket(p.Binding.Owner, p.ID)
		if err != nil || !r.WorkComplete || r.CurrentStage != "S6" {
			t.Fatal(r, err)
		}
	}
}
