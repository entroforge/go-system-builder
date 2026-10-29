package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This extends the mechanical source-proof fixture. Its assembled journal is
// not an original repair/prepare/approval Writer or a model execution chain.
type repairResultRuntimeFixture struct {
	plan      repairPlanReportRuntimeFixture
	packet    workpacket.RepairResultPacket
	execution map[string]any
	events    []map[string]any
}

func newRepairResultRuntimeFixture(t *testing.T) repairResultRuntimeFixture {
	t.Helper()
	plan := newRepairPlanReportRuntimeFixture(t)
	report := acceptRepairPlanReportFixture(t, &plan)
	b := plan.packet.Binding
	command := "go test ./internal/api -run TestPayloadRoundTrip"
	profiles, err := RepairResultCheckProfiles(stringValue(plan.state["review"].(map[string]any)["repair"].(map[string]any)["plan_report_sha256"]), []string{command}, "main")
	if err != nil {
		t.Fatal(err)
	}
	profileData, _ := json.Marshal(profiles)
	var profileRows any
	if err := json.Unmarshal(profileData, &profileRows); err != nil {
		t.Fatal(err)
	}
	execution := map[string]any{
		"assignment_id": b.ManifestAssignmentID, "runtime_id": b.RuntimeID, "baseline_generation": b.BaselineGeneration, "execution_generation": 1, "agent_id": b.OwnerAgentID,
		"worktree_path": filepath.Join(plan.root, ".worktrees", "source-proof-worker"), "branch": "codex/source-proof-worker", "target_branch": "req-source-proof", "base_commit": strings.Repeat("a", 40),
		"check_location": "main", "write_paths": []string{"internal/api"}, "required_checks": []string{command}, "inputs": map[string]string{b.PlanRef: b.PlanSHA256}, "bootstrap_sha256": strings.Repeat("b", 64), "check_profiles": profileRows, "status": "ready",
	}
	definition, err := ProjectRepairResultExecution(execution)
	if err != nil {
		t.Fatal(err)
	}
	rawReport, err := os.ReadFile(filepath.Join(plan.root, filepath.FromSlash(report.ReportRef)))
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildRepairResult(workpacket.RepairResultBinding{Sources: b, PlanReportRef: report.ReportRef, PlanReportSHA256: workpacket.Digest(rawReport), ExecutionSHA256: workpacket.Digest(definition), ReworkRef: "", ReworkSHA256: ""}, workpacket.RepairResultInputs{Sources: plan.packet.Inputs, PlanReport: rawReport, Execution: definition, ReworkReceipt: json.RawMessage("null")})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := controlproof.InspectJournalData(plan.journal)
	if err != nil {
		t.Fatal(err)
	}
	events := append([]map[string]any{}, journal.Events...)
	appendEvent := func(m Mutation, before, after int) {
		t.Helper()
		events = append(events, buildJournalEvent(m, b.RuntimeID, before, after, after, time.Date(2026, 10, 14, 12, after, 0, 0, time.UTC)))
	}
	appendEvent(Mutation{EventID: "evt-s9-execution-begin-r7", TransitionID: "PTR-BUG-11", Event: "transition_committed", Actor: "orchestrator", IdempotencyKey: "runtime:s9:execution:" + plan.packet.ReportID + ":6", RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "reproducing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}, EvidenceIDs: []string{report.ReportRef}}, 6, 7)
	appendEvent(Mutation{EventID: "workspace-r8", TransitionID: "WORKSPACE", Event: "transition_committed", Actor: "main", IdempotencyKey: "workspace-r8", RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}}, 7, 8)
	appendEvent(Mutation{EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed", Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}}, 8, 9)
	plan.state["lifecycle"].(map[string]any)["phase"] = "fixing"
	plan.state["review"].(map[string]any)["repair"].(map[string]any)["status"] = "repairing"
	plan.state["workspace"] = map[string]any{"binding_version": 1, "main_root": plan.root, "common_dir": filepath.Join(plan.root, ".git"), "integration_branch": "req-source-proof", "bound_head": strings.Repeat("a", 40), "executions": map[string]any{b.ManifestAssignmentID: execution}}
	encoded, _ := json.Marshal(packet)
	var packetRow map[string]any
	if err := json.Unmarshal(encoded, &packetRow); err != nil {
		t.Fatal(err)
	}
	entities := plan.state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), packetRow)
	plan.state["revision"] = 9
	plan.state["journal"].(map[string]any)["last_sequence"] = 9
	plan.state["journal"].(map[string]any)["last_event_id"] = packet.IssuanceEventID
	plan.journal = s8CheckpointJournalBytes(t, events...)
	writeRepairPlanReportJournal(t, &plan)
	return repairResultRuntimeFixture{plan: plan, packet: packet, execution: execution, events: events}
}

func TestRepairResultPacketSourcesAndCheckpointFiltering(t *testing.T) {
	t.Run("professional records do not replay planning qualifications", func(t *testing.T) {
		f := newRepairResultRuntimeFixture(t)
		var report map[string]any
		if err := json.Unmarshal(f.packet.Inputs.PlanReport, &report); err != nil {
			t.Fatal(err)
		}
		delete(report, "receipt")
		report["red_checks"] = []any{map[string]any{"name": "actual earlier observation", "command": "different precheck", "result": "blocked", "evidence_refs": []string{"local observation"}}}
		data, _ := json.Marshal(report)
		if err := os.WriteFile(filepath.Join(f.plan.root, f.packet.Binding.PlanReportRef), data, 0600); err != nil {
			t.Fatal(err)
		}
		// Neither Plan issuance/receipt nor Begin chronology is required to
		// associate these records. This check reads no journal or source closure.
		if err := VerifyRepairResultSources(f.plan.root, f.plan.state, f.packet, 0); err != nil {
			t.Fatalf("readable professional records rejected: %v", err)
		}
		delete(f.execution, "required_checks")
		delete(f.execution, "check_profiles")
		f.packet.Inputs.Execution, _ = ProjectRepairResultExecution(f.execution)
		if err := VerifyRepairResultSources(f.plan.root, f.plan.state, f.packet, 0); err != nil {
			t.Fatalf("missing Main check configuration prevented recording: %v", err)
		}
		if _, accepted, err := VerifiedRepairResultCandidateReceipt(f.plan.root, f.plan.state, f.packet.ID, f.packet.Binding.Sources.OwnerAgentID); accepted {
			t.Fatalf("source association manufactured a completed candidate: %v", err)
		}
	})
	f := newRepairResultRuntimeFixture(t)
	if err := VerifyRepairResultPacketIssue(f.plan.root, f.plan.state, f.packet); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairResultPacketCurrent(f.plan.root, f.plan.state, f.packet); err != nil {
		t.Fatal(err)
	}
	selected, err := CurrentRepairResultPackets(f.plan.root, f.plan.state, f.packet.Binding.Sources.OwnerAgentID)
	if err != nil || len(selected) != 1 {
		t.Fatalf("current source selection: %v %+v", err, selected)
	}
	before, _ := json.Marshal(f.plan.state)
	if err := ValidateCheckpointIndices(f.plan.root, f.plan.state, f.plan.journal); err != nil {
		t.Fatalf("mixed original PlanReport/result checkpoint: %v", err)
	}
	after, _ := json.Marshal(f.plan.state)
	if string(before) != string(after) {
		t.Fatal("checkpoint filtering mutated caller's original registry")
	}
	if _, ok, err := VerifiedRepairResultCandidateReceipt(f.plan.root, f.plan.state, f.packet.ID, f.packet.Binding.Sources.OwnerAgentID); err != nil || ok {
		t.Fatalf("issuance projected candidate reception: %t %v", ok, err)
	}
}

func TestRepairResultPacketIssueAndCurrentUsesBothProofs(t *testing.T) {
	t.Run("current issued packet", func(t *testing.T) {
		f := newRepairResultRuntimeFixture(t)
		if err := VerifyRepairResultPacketIssueAndCurrent(f.plan.root, f.plan.state, f.packet); err != nil {
			t.Fatalf("combined issue/current verification rejected exact current packet: %v", err)
		}
	})
	t.Run("unregistered packet", func(t *testing.T) {
		f := newRepairResultRuntimeFixture(t)
		binding := f.packet.Binding
		owner := binding.Sources.OwnerAgentID
		binding.Resume = &workpacket.RepairResultResume{
			PredecessorPacketID: f.packet.ID, PredecessorPacketSHA256: strings.Repeat("c", 64),
			BlockerRef: ".claude/evidence/s9/blocker.json", BlockerSHA256: strings.Repeat("d", 64),
			ResolutionMessageRef: ".claude/evidence/s9/resolution.json", ResolutionMessageSHA256: strings.Repeat("e", 64),
			ResolutionEventID:        fmt.Sprintf("evt-agent-%s-blocker_resolved-r9", owner),
			ResolutionIdempotencyKey: fmt.Sprintf("runtime:agent:%s:blocker_resolved:8", owner),
			ResolutionBeforeRevision: 8, ResolutionAfterRevision: 9, ResolutionSequence: 10,
		}
		packet, err := workpacket.BuildRepairResult(binding, f.packet.Inputs)
		if err != nil {
			t.Fatalf("build a valid but unregistered packet: %v", err)
		}
		if err := VerifyRepairResultPacketIssueAndCurrent(f.plan.root, f.plan.state, packet); err == nil {
			t.Fatal("combined issue/current verification accepted an unregistered packet")
		}
	})
	t.Run("stale phase", func(t *testing.T) {
		f := newRepairResultRuntimeFixture(t)
		f.plan.state["lifecycle"].(map[string]any)["phase"] = "planning"
		if err := VerifyRepairResultPacketIssueAndCurrent(f.plan.root, f.plan.state, f.packet); err == nil {
			t.Fatal("combined issue/current verification accepted a packet outside its current phase")
		}
	})
}

func TestRepairResultExecutionRejectsDraftDigestAndMutableProjection(t *testing.T) {
	for _, variant := range []string{"execution digest", "mutable status"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultRuntimeFixture(t)
			if _, err := RepairResultExecution(f.plan.state, f.packet); err != nil {
				t.Fatalf("original registered definition is not valid: %v", err)
			}
			before, err := json.Marshal(f.plan.state)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "execution digest" {
				f.packet.Binding.ExecutionSHA256 = strings.Repeat("0", 64)
			} else {
				var definition map[string]any
				if err := json.Unmarshal(f.packet.Inputs.Execution, &definition); err != nil {
					t.Fatal(err)
				}
				definition["status"] = "ready"
				f.packet.Inputs.Execution, err = json.Marshal(definition)
				if err != nil {
					t.Fatal(err)
				}
				f.packet.Binding.ExecutionSHA256 = workpacket.Digest(f.packet.Inputs.Execution)
			}
			if _, err := RepairResultExecution(f.plan.state, f.packet); err == nil || !strings.Contains(err.Error(), "registered execution definition differs") {
				t.Fatalf("%s borrowed original execution authority: %v", variant, err)
			}
			after, err := json.Marshal(f.plan.state)
			if err != nil || string(before) != string(after) {
				t.Fatalf("source lookup changed Runtime: %v", err)
			}
		})
	}
}

func TestRepairResultCurrentRejectsExecutionSourceAndAttemptDrift(t *testing.T) {
	for _, variant := range []string{"owner", "phase", "base", "profile", "bootstrap", "attempt", "report_index"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultRuntimeFixture(t)
			switch variant {
			case "owner":
				f.plan.state["review"].(map[string]any)["repair"].(map[string]any)["assignment_owners"].(map[string]any)[f.packet.Binding.Sources.RepairAssignmentID] = "another-owner"
			case "phase":
				f.plan.state["lifecycle"].(map[string]any)["phase"] = "planning"
			case "base":
				f.execution["base_commit"] = strings.Repeat("c", 40)
			case "profile":
				f.execution["check_profiles"].([]any)[0].(map[string]any)["reuse_policy"] = "same_input_same_responsibility"
			case "bootstrap":
				f.execution["bootstrap_sha256"] = strings.Repeat("c", 64)
			case "attempt":
				f.execution["rework_ref"] = ".claude/evidence/new-attempt.json"
			case "report_index":
				pointer := f.plan.state["review"].(map[string]any)["repair"].(map[string]any)
				delete(pointer, "plan_report_ref")
				delete(pointer, "plan_report_sha256")
				pointer["plan_report_refs"] = []any{}
			}
			if err := VerifyRepairResultPacketCurrent(f.plan.root, f.plan.state, f.packet); err == nil {
				t.Fatalf("%s drift authorized unanswered result", variant)
			}
		})
	}
}

func TestRepairResultCurrentRetainsLegacyBootstrapRequirement(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	// A newly issued legacy packet must include its bootstrap in the original
	// execution. Try to construct one with an otherwise matching definition.
	delete(f.execution, "bootstrap_sha256")
	definition, err := ProjectRepairResultExecution(f.execution)
	if err != nil {
		t.Fatal(err)
	}
	binding := f.packet.Binding
	binding.ExecutionSHA256 = workpacket.Digest(definition)
	inputs := f.packet.Inputs
	inputs.Execution = definition
	if _, err := workpacket.BuildRepairResult(binding, inputs); err == nil || !strings.Contains(err.Error(), "bootstrap_sha256") {
		t.Fatalf("legacy packet without bootstrap was not rejected by its original schema: %v", err)
	}
	// The original valid packet still binds the bootstrap. Losing it from the
	// live legacy execution must fail the original frozen-source comparison.
	if err := VerifyRepairResultPacketCurrent(f.plan.root, f.plan.state, f.packet); err == nil || !strings.Contains(err.Error(), "registered execution definition differs from issued result") {
		t.Fatalf("legacy execution that lost its frozen bootstrap was accepted: %v", err)
	}
}

func TestRepairResultCheckpointRejectsOrphanTypedIssue(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	entities := f.plan.state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	entities["work_packets"] = rows[:len(rows)-1]
	if err := ValidateCheckpointIndices(f.plan.root, f.plan.state, f.plan.journal); err == nil {
		t.Fatal("orphan typed result issue event accepted")
	}
}

func acceptRepairResultCompletionFixture(t *testing.T, f *repairResultRuntimeFixture) RepairResultCompletionReport {
	t.Helper()
	b := f.packet.Binding.Sources
	id := "ev-completion-" + b.TaskID + fmt.Sprintf("-g%d", b.BaselineGeneration)
	ref := fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/%s.json", b.RuntimeID, b.BaselineGeneration, b.OwnerAgentID, id)
	doc := map[string]any{"schema_version": "1.0.0", "evidence_id": id, "kind": "completion_report", "runtime_id": b.RuntimeID, "baseline_generation": b.BaselineGeneration, "producer_agent_id": b.OwnerAgentID, "producer_responsibility": "BUILD-WORK-PACKAGE", "task_id": b.TaskID, "conclusion": "completed"}
	data, _ := json.Marshal(doc)
	if err := os.MkdirAll(filepath.Join(f.plan.root, filepath.Dir(filepath.FromSlash(ref))), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.plan.root, filepath.FromSlash(ref)), data, 0600); err != nil {
		t.Fatal(err)
	}
	sha := workpacket.Digest(data)
	f.plan.state["evidence"] = append(objectValuesForCompletionFixture(f.plan.state["evidence"]), map[string]any{"id": id, "path": ref, "sha256": sha, "kind": "completion_report", "responsibility_id": "BUILD-WORK-PACKAGE", "baseline_generation": b.BaselineGeneration, "produced_by": []string{b.OwnerAgentID}, "status": "valid"})
	entities := f.plan.state["entities"].(map[string]any)
	for _, agent := range objectRows(entities["agents"]) {
		if agent["id"] == b.OwnerAgentID {
			agent["state"] = "reported"
			agent["completion_reported_ref"] = ref
		}
	}
	for _, task := range objectRows(entities["tasks"]) {
		if task["id"] == b.TaskID {
			task["completion_report_ref"] = ref
			task["state"] = "review"
		}
	}
	eventID := fmt.Sprintf("evt-complete-%s-%s-r10", b.OwnerAgentID, b.TaskID)
	event := buildJournalEvent(Mutation{EventID: eventID, TransitionID: "BUILDER-RESULT", Event: "completion_reported", Actor: "orchestrator", IdempotencyKey: fmt.Sprintf("runtime:complete:%s:%s:9", b.OwnerAgentID, b.TaskID), RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}, EvidenceIDs: []string{id}}, b.RuntimeID, 9, 10, 10, time.Date(2026, 10, 14, 12, 10, 0, 0, time.UTC))
	f.events = append(f.events, event)
	f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
	f.plan.state["revision"] = 10
	f.plan.state["journal"].(map[string]any)["last_sequence"] = 10
	f.plan.state["journal"].(map[string]any)["last_event_id"] = eventID
	writeRepairPlanReportJournal(t, &f.plan)
	return RepairResultCompletionReport{Ref: ref, SHA256: sha, EventID: eventID}
}

func objectValuesForCompletionFixture(value any) []any { rows, _ := value.([]any); return rows }

func acceptRepairResultCandidateFixture(t *testing.T, f *repairResultRuntimeFixture) RepairResultCandidateReceipt {
	t.Helper()
	p := f.packet
	b := p.Binding.Sources
	completion := acceptRepairResultCompletionFixture(t, f)
	answer := map[string]any{
		"verdict": "completed", "before_fix_results": []any{map[string]any{"assertion_id": "root-1", "observed_red": "The mechanical fixture recorded the original missing payload.", "evidence_refs": []string{"test://s9/before-fix"}}},
		"unit_results":            []any{map[string]any{"unit_id": "unit-api", "status": "pass", "implementation_summary": "Restore the scoped payload path.", "restored_invariant_refs": []string{}, "evidence_refs": []string{"test://s9/implementation"}}},
		"detection_gap_artifacts": []string{}, "evidence_refs": []string{"test://s9/implementation"}, "scope_deviations": []string{}, "residual_risks": []string{}, "migration_ref": "", "rollback_ref": "",
	}
	draft := p.Draft
	draft.Answer = repairPlanReportTestCanonicalMap(t, answer)
	encoded, _ := json.Marshal(draft)
	canonical, problems := workpacket.ValidateRepairResultAnswer(p, encoded)
	if len(problems) > 0 {
		t.Fatalf("mechanical candidate answer invalid: %+v", problems)
	}
	packetSHA, err := workpacket.RepairResultPacketDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	provenance := RepairResultCandidateProvenance{
		Contract: "s9-repair-result-candidate-v1", PacketID: p.ID, PacketSHA256: packetSHA, IssuanceEventID: p.IssuanceEventID, AnswerSHA256: workpacket.Digest(canonical), SubmittedAnswer: canonical,
		OwnerAgentID: b.OwnerAgentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, ReviewRound: b.ReviewRound,
		ExecutionAssignmentID: b.ManifestAssignmentID, ExecutionGeneration: 1, ExecutionSHA256: p.Binding.ExecutionSHA256, ExecutionRoot: stringValue(f.execution["worktree_path"]), SourceHead: strings.Repeat("c", 40), BaseCommit: strings.Repeat("a", 40),
		PlanReportRef: p.Binding.PlanReportRef, PlanReportSHA256: p.Binding.PlanReportSHA256, ReworkRef: "", ReworkSHA256: "", WorkerCheckReceipts: []checkexec.ToolReceipt{}, OwnedCheckReceipts: []RepairResultOwnedCheckReceipt{},
		CompletionReportRef: completion.Ref, CompletionReportSHA256: completion.SHA256, CompletionReportEventID: completion.EventID,
	}
	proposal := map[string]any{}
	for key, value := range answer {
		proposal[key] = value
	}
	proposal["result"] = "pass"
	proposal["result_id"] = p.ResultID
	proposal["producer_agent_id"] = b.OwnerAgentID
	proposal["assignment_id"] = b.RepairAssignmentID
	for _, source := range []struct{ key, ref, sha string }{{"contract", b.ContractRef, b.ContractSHA256}, {"session", b.SessionRef, b.SessionSHA256}, {"plan", b.PlanRef, b.PlanSHA256}, {"plan_report", p.Binding.PlanReportRef, p.Binding.PlanReportSHA256}} {
		proposal[source.key] = map[string]any{"path": source.ref, "sha256": source.sha}
	}
	var report map[string]any
	if err := json.Unmarshal(p.Inputs.PlanReport, &report); err != nil {
		t.Fatal(err)
	}
	proposal["before_fix_checks"] = report["red_checks"]
	proposal["checks"] = []any{}
	proposal["changed_artifacts"] = []any{map[string]any{"path": "internal/api/payload.go", "sha256": strings.Repeat("d", 64), "status": "added"}}
	document := map[string]any{"version": 1, "record_type": "repair_workspace_candidate", "runtime_id": b.RuntimeID, "baseline_generation": b.BaselineGeneration, "execution_generation": 1, "assignment_id": b.ManifestAssignmentID, "source_head": provenance.SourceHead, "base_commit": provenance.BaseCommit, "session_id": b.SessionID, "plan_id": b.PlanID, "contract_sha256": b.ContractSHA256, "proposal": proposal, "provenance": provenance}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	sha := workpacket.Digest(raw)
	path := ".claude/review/repair/candidates/" + sha + ".json"
	if err := os.MkdirAll(filepath.Join(f.plan.root, filepath.Dir(filepath.FromSlash(path))), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.plan.root, filepath.FromSlash(path)), raw, 0600); err != nil {
		t.Fatal(err)
	}
	eventID := "evt-s9-issued-repair-candidate-" + p.ID
	key := "runtime:s9:issued-repair-candidate:" + p.ID
	event := buildJournalEvent(Mutation{EventID: eventID, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: b.OwnerAgentID, IdempotencyKey: key, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "fixing"}, To: map[string]any{"state": "bug_resolution", "phase": "fixing"}, EvidenceIDs: []string{path, p.ID}}, b.RuntimeID, 10, 11, 11, time.Date(2026, 10, 14, 12, 11, 0, 0, time.UTC))
	f.events = append(f.events, event)
	f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
	f.plan.state["revision"] = 11
	f.plan.state["journal"].(map[string]any)["last_sequence"] = 11
	f.plan.state["journal"].(map[string]any)["last_event_id"] = eventID
	f.execution["delivery_ref"] = path
	f.execution["delivery_sha256"] = sha
	writeRepairPlanReportJournal(t, &f.plan)
	return RepairResultCandidateReceipt{Provenance: provenance, CandidateRef: path, CandidateSHA256: sha, AcceptedEventID: eventID, AcceptedIdempotencyKey: key, AcceptedAfterRuntimeRevision: 11, AcceptedSequence: 11}
}

func TestRepairResultCandidateReceiptChecksOriginalEventAndPointer(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	want := acceptRepairResultCandidateFixture(t, &f)
	got, ok, err := VerifiedRepairResultCandidateReceipt(f.plan.root, f.plan.state, f.packet.ID, f.packet.Binding.Sources.OwnerAgentID)
	if err != nil || !ok || !hypothesisJSONEqual(got, want) {
		t.Fatalf("candidate original facts: ok=%t err=%v got=%+v", ok, err, got)
	}
	if err := ValidateCheckpointIndices(f.plan.root, f.plan.state, f.plan.journal); err != nil {
		t.Fatalf("candidate original checkpoint: %v", err)
	}
	for _, variant := range []string{"missing_pointer", "wrong_pointer_sha", "wrong_actor", "wrong_generation", "wrong_event_key"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultRuntimeFixture(t)
			acceptRepairResultCandidateFixture(t, &f)
			event := f.events[len(f.events)-1]
			switch variant {
			case "missing_pointer":
				delete(f.execution, "delivery_ref")
				delete(f.execution, "delivery_sha256")
			case "wrong_pointer_sha":
				f.execution["delivery_sha256"] = strings.Repeat("e", 64)
			case "wrong_actor":
				event["actor"].(map[string]any)["id"] = "another-owner"
			case "wrong_generation":
				event["baseline_generation"] = 1
			case "wrong_event_key":
				event["idempotency_key"] = "other-key"
			}
			f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
			writeRepairPlanReportJournal(t, &f.plan)
			if _, _, err := VerifiedRepairResultCandidateReceipt(f.plan.root, f.plan.state, f.packet.ID, f.packet.Binding.Sources.OwnerAgentID); err == nil {
				t.Fatalf("candidate %s drift accepted", variant)
			}
		})
	}
}

func TestRepairResultCompletionReportRequiresOriginalAcceptance(t *testing.T) {
	for _, variant := range []string{"valid", "working", "missing_owner_pointer", "invalidated", "wrong_sha", "wrong_event_generation", "extra_event_ref", "duplicate_task"} {
		t.Run(variant, func(t *testing.T) {
			f := newRepairResultRuntimeFixture(t)
			want := acceptRepairResultCompletionFixture(t, &f)
			entities := f.plan.state["entities"].(map[string]any)
			for _, agent := range objectRows(entities["agents"]) {
				if agent["id"] != f.packet.Binding.Sources.OwnerAgentID {
					continue
				}
				if variant == "working" {
					agent["state"] = "working"
				}
				if variant == "missing_owner_pointer" {
					delete(agent, "completion_reported_ref")
				}
			}
			for _, row := range objectRows(f.plan.state["evidence"]) {
				if row["path"] != want.Ref {
					continue
				}
				if variant == "invalidated" {
					row["invalidated_by"] = "evt-expired"
				}
				if variant == "wrong_sha" {
					row["sha256"] = strings.Repeat("f", 64)
				}
			}
			event := f.events[len(f.events)-1]
			if variant == "wrong_event_generation" {
				event["baseline_generation"] = 1
			}
			if variant == "extra_event_ref" {
				event["evidence_ids"] = []string{"ev-completion-" + f.packet.Binding.Sources.TaskID + fmt.Sprintf("-g%d", f.packet.Binding.Sources.BaselineGeneration), "other-evidence"}
			}
			if variant == "duplicate_task" {
				row := map[string]any{"id": f.packet.Binding.Sources.TaskID, "state": "review", "completion_report_ref": want.Ref}
				entities["tasks"] = append(objectValuesForCompletionFixture(entities["tasks"]), row, row)
			}
			f.plan.journal = s8CheckpointJournalBytes(t, f.events...)
			writeRepairPlanReportJournal(t, &f.plan)
			got, err := CurrentRepairResultCompletionReport(f.plan.root, f.plan.state, f.packet)
			if variant == "valid" {
				if err != nil || got != want {
					t.Fatalf("original completion proof: %+v %v", got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s completion accepted", variant)
			}
		})
	}
}

func TestRepairResultHistoricalCandidateRetainsFrozenCompletion(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	want := acceptRepairResultCandidateFixture(t, &f)
	entities := f.plan.state["entities"].(map[string]any)
	for _, agent := range objectRows(entities["agents"]) {
		if agent["id"] == f.packet.Binding.Sources.OwnerAgentID {
			agent["state"] = "done"
			agent["completion_reported_ref"] = ".claude/later-report.json"
		}
	}
	if _, err := CurrentRepairResultCompletionReport(f.plan.root, f.plan.state, f.packet); err == nil {
		t.Fatal("historic Agent was granted fresh delivery permission")
	}
	got, ok, err := VerifiedRepairResultCandidateReceipt(f.plan.root, f.plan.state, f.packet.ID, f.packet.Binding.Sources.OwnerAgentID)
	if err != nil || !ok || !hypothesisJSONEqual(got, want) {
		t.Fatalf("historic original candidate lost its frozen completion: %t %v", ok, err)
	}
}
