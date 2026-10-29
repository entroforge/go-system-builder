package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// repairPlanReportRuntimeFixture is a mechanical checkpoint/source-proof
// fixture. It contains the original typed S9 authorities and exact journal
// coordinates, but it is not a real approval/dispatch writer or a model test.
type repairPlanReportRuntimeFixture struct {
	root          string
	packet        workpacket.RepairPlanReportPacket
	state         map[string]any
	journal       []byte
	approvalEvent map[string]any
	sessionEvent  map[string]any
	planEvent     map[string]any
	registerEvent map[string]any
	issueEvent    map[string]any
}

func newRepairPlanReportRuntimeFixture(t *testing.T) repairPlanReportRuntimeFixture {
	t.Helper()
	root := t.TempDir()
	const (
		runtimeID            = "loop-REQ-001"
		generation           = 2
		round                = 1
		caseID               = "investigation-case-s9-001"
		contractID           = "repair-contract-s9-001"
		sessionID            = "repair-session-s9-001"
		planID               = "repair-plan-s9-001"
		repairAssignmentID   = "repair-assignment-unit-api"
		manifestAssignmentID = "assignment-s9-unit-api"
		taskID               = "TASK-901"
		teamID               = "workgroup-s9-session-001-unit-api"
		manifestID           = "team-manifest-s9-session-001-unit-api"
		ownerID              = "builder-s9-001"
		approvalID           = "EVID-S9-CONTRACT-APPROVAL-001"
	)
	const (
		caseRef     = ".claude/review/investigation/cases/investigation-case-s9-001-r2.json"
		contractRef = ".claude/review/investigation/contracts/repair-contract-s9-001-r1.json"
		sessionRef  = ".claude/review/repair/sessions/repair-session-s9-001.json"
		planRef     = ".claude/review/repair/plans/repair-plan-s9-001.json"
		manifestRef = ".claude/workgroups/REQ-001/TASK-901/manifest.json"
		taskRef     = ".claude/workgroups/REQ-001/TASK-901/TASK-901.json"
	)
	write := func(ref string, value any) []byte {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		path := filepath.Join(root, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return data
	}

	prospective := []string{"internal/api"}
	forbidden := []string{"internal/api/generated"}
	assignment := map[string]any{
		"assignment_id": repairAssignmentID, "unit_ids": []string{"unit-api"}, "assertion_ids": []string{"root-1"},
		"depends_on": []string{}, "resource_locks": []string{}, "status": "queued", "scope": []string{"internal/api"},
		"contract_ref": contractRef,
	}
	contractDoc := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": contractID, "case_id": caseID, "revision": 1, "status": "approved",
		"source_finding_ids": []string{"finding-s9-001"}, "root_cause_statement": "the response loses the stored value",
		"violated_invariant": "stored values remain readable", "causal_model_ref": "test://causal-model",
		"architecture_intent": "preserve values through the API boundary",
		"repair_units":        []any{map[string]any{"id": "unit-api", "description": "repair the API", "scope": []string{"internal/api"}, "assertion_ids": []string{"root-1"}, "depends_on": []string{}, "resource_locks": []string{}}},
		"prospective_scope":   prospective, "forbidden_scope": forbidden,
		"symptom_assertions": []string{"symptom"}, "root_invariant_assertions": []string{"root"},
		"detection_gap_assertions": []string{"gap"}, "stop_escalation_conditions": []string{"stop"},
		"approval_evidence_id": approvalID, "approved_by": "human-reviewer", "approved_at": "2026-10-14T12:00:00Z",
		"approver_id": "human-reviewer", "approval_hash": strings.Repeat("a", 64),
	}
	contractBytes := write(contractRef, contractDoc)
	caseDoc := map[string]any{
		"schema_version": "1.0.0", "case_id": caseID, "revision": 2, "status": "contract_approved",
		"source_finding_ids": []string{"finding-s9-001"}, "observation_batch_id": "observation-batch-s9-001",
		"observation_batch_ref": ".claude/evidence/observation-batch-s9-001.json", "observation_batch_sha256": strings.Repeat("b", 64),
		"baseline_generation": generation, "baseline_digest": strings.Repeat("c", 64), "grouping_rationale": "one bounded failure",
		"unexplained_finding_ids": []string{}, "failure_boundary_refs": []string{}, "cross_layer_trace": nil,
		"evidence_gaps": []string{}, "hypotheses": []any{}, "hypothesis_results": []any{}, "causal_model": nil,
		"primary_root_cause": "the response drops the value", "blast_radius": nil, "detection_gap": nil,
		"route": "s9_repair", "route_reason": "approved bounded repair", "repair_contract_ref": contractRef,
		"repair_contract_sha256": workpacket.Digest(contractBytes),
	}
	caseBytes := write(caseRef, caseDoc)
	sessionDoc := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_session", "session_id": sessionID,
		"contract_ref": contractRef, "contract_sha256": workpacket.Digest(contractBytes), "runtime_id": runtimeID,
		"req_id": "REQ-001", "baseline_generation": generation, "baseline_artifacts": []any{},
		"baseline_digest": strings.Repeat("d", 64), "status": "planned", "created_by": "main", "created_at": "2026-10-14T12:01:00Z",
	}
	sessionBytes := write(sessionRef, sessionDoc)
	planDoc := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_plan", "plan_id": planID, "session_id": sessionID,
		"contract_id": contractID, "contract_ref": contractRef, "contract_sha256": workpacket.Digest(contractBytes),
		"units":       []any{map[string]any{"id": "unit-api", "description": "repair the API", "scope": []string{"internal/api"}, "assertion_ids": []string{"root-1"}, "depends_on": []string{}, "resource_locks": []string{}}},
		"assignments": []any{assignment}, "execution_policy": "coverage_complete", "prospective_scope": prospective,
		"forbidden_scope": forbidden, "created_by": "main", "created_at": "2026-10-14T12:02:00Z",
	}
	planBytes := write(planRef, planDoc)
	taskDoc := map[string]any{
		"task_id": taskID, "session_id": sessionID, "plan_id": planID, "assignment_id": repairAssignmentID,
		"unit_ids": []string{"unit-api"}, "scope": []string{"internal/api"}, "depends_on": []string{}, "resource_locks": []string{},
		"state": "reviewed", "objective": "restore the approved API invariant", "instruction": "read the approved sources and report a failing check",
		"next_action": "submit the pre-execution PlanReport",
	}
	taskBytes := write(taskRef, taskDoc)
	manifestAssignment := map[string]any{
		"assignment_id": manifestAssignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"scope": []string{"internal/api"}, "agent_id": ownerID, "agent_definition_ref": ".claude/agents/backend-builder.md",
		"skill_refs": []string{"testing-strategy"}, "read_paths": []string{contractRef, sessionRef, planRef},
		"write_paths": []string{"internal/api"}, "output_paths": []string{".claude/evidence"}, "depends_on": []string{},
		"reuse_decision": "create", "grouping_rationale": "one Builder owns one frozen S9 repair unit", "status": "planned", "dispatch_mode": "plan_checkpoint",
	}
	manifestDoc := map[string]any{
		"schema_version": "1.0.0", "manifest_id": manifestID, "version": "1.0.0", "runtime_id": runtimeID,
		"req_id": "REQ-001", "baseline_generation": generation, "review_round": round,
		"platform_team_id": "platform-s9-repair", "workgroup_id": teamID, "workgroup_kind": "builder", "status": "planned",
		"documents": []any{
			map[string]any{"id": "repair-contract", "path": contractRef, "version": "approved", "sha256": workpacket.Digest(contractBytes)},
			map[string]any{"id": "repair-session", "path": sessionRef, "version": "planned", "sha256": workpacket.Digest(sessionBytes)},
			map[string]any{"id": "repair-plan", "path": planRef, "version": "compiled", "sha256": workpacket.Digest(planBytes)},
		},
		"risk_tags": []any{}, "responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one approved RepairAssignment owns the bounded unit",
			"assignment_ids": []string{manifestAssignmentID}, "na_rationale": nil, "evidence_ref": planRef,
		}},
		"assignments": []any{manifestAssignment}, "separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "One registered Builder per original RepairAssignment.",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-10-14T12:03:00Z"},
	}
	manifestBytes := write(manifestRef, manifestDoc)
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		t.Fatalf("synthetic S9 Builder manifest is schema-invalid: %v", err)
	}
	canonicalAssignment := repairPlanReportTestCanonicalMap(t, assignment)
	canonicalManifestAssignment := repairPlanReportTestCanonicalMap(t, manifestAssignment)
	binding := workpacket.RepairPlanReportBinding{
		RuntimeID: runtimeID, BaselineGeneration: generation, ReviewRound: round,
		CaseID: caseID, CaseRef: caseRef, CaseSHA256: workpacket.Digest(caseBytes),
		ContractID: contractID, ContractRef: contractRef, ContractSHA256: workpacket.Digest(contractBytes),
		SessionID: sessionID, SessionRef: sessionRef, SessionSHA256: workpacket.Digest(sessionBytes),
		PlanID: planID, PlanRef: planRef, PlanSHA256: workpacket.Digest(planBytes),
		RepairAssignmentID: repairAssignmentID, RepairAssignmentSHA256: workpacket.Digest(canonicalAssignment),
		ManifestAssignmentID: manifestAssignmentID, ManifestAssignmentSHA256: workpacket.Digest(canonicalManifestAssignment),
		TeamID: teamID, TaskID: taskID, TaskRef: taskRef, TaskSHA256: workpacket.Digest(taskBytes),
		ManifestRef: manifestRef, ManifestSHA256: workpacket.Digest(manifestBytes), OwnerAgentID: ownerID,
	}
	inputs := workpacket.RepairPlanReportInputs{
		Case: json.RawMessage(caseBytes), Contract: json.RawMessage(contractBytes), Session: json.RawMessage(sessionBytes),
		Plan: json.RawMessage(planBytes), Task: json.RawMessage(taskBytes),
		RepairAssignment: json.RawMessage(canonicalAssignment), ManifestAssignment: json.RawMessage(canonicalManifestAssignment),
		ProspectiveScope: prospective, ForbiddenScope: forbidden,
	}
	packet, err := workpacket.BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatalf("build S9 checkpoint packet fixture: %v", err)
	}
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var packetRow map[string]any
	if err := json.Unmarshal(packetBytes, &packetRow); err != nil {
		t.Fatal(err)
	}
	issueTime := time.Date(2026, 10, 14, 12, 5, 0, 0, time.UTC)
	cursorReadback := map[string]any{"state": "bug_resolution", "phase": "repair_readback"}
	cursorPlanning := map[string]any{"state": "bug_resolution", "phase": "planning"}
	approvalEvent := buildJournalEvent(Mutation{
		EventID: "evt-repair-contract-approved-" + contractID + "-r1", TransitionID: "S8-REPAIR-CONTRACT-APPROVAL", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "runtime:investigation-contract-approve:" + contractID + ":0", RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursorReadback, To: cursorReadback, EvidenceIDs: []string{contractID, caseID, approvalID},
	}, runtimeID, 0, 1, 1, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	sessionEvent := buildJournalEvent(Mutation{
		EventID: "evt-s9-session-open-" + sessionID + "-r2", TransitionID: "S9-SESSION-OPEN", Event: "transition_committed",
		Actor: "main", IdempotencyKey: "runtime:s9:session-open:" + sessionID + ":1", RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursorReadback, To: cursorReadback, EvidenceIDs: []string{sessionID, contractID},
	}, runtimeID, 1, 2, 2, time.Date(2026, 10, 14, 12, 1, 0, 0, time.UTC))
	planEvent := buildJournalEvent(Mutation{
		EventID: "evt-s9-plan-" + planID + "-r3", TransitionID: "PTR-BUG-09", Event: "transition_committed",
		Actor: "main", IdempotencyKey: "runtime:s9:plan:" + planID + ":2", RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursorReadback, To: cursorPlanning, EvidenceIDs: []string{planID},
	}, runtimeID, 2, 3, 3, time.Date(2026, 10, 14, 12, 2, 0, 0, time.UTC))
	registerEvent := buildJournalEvent(Mutation{
		EventID: "evt-register-" + teamID + "-r4", TransitionID: "ENTITY-REGISTER", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "runtime:register:" + manifestID + ":3", RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursorPlanning, To: cursorPlanning, EvidenceIDs: []string{manifestRef, taskRef},
	}, runtimeID, 3, 4, 4, time.Date(2026, 10, 14, 12, 3, 0, 0, time.UTC))
	issueEvent := buildJournalEvent(Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursorPlanning, To: cursorPlanning,
	}, runtimeID, 4, 5, 5, issueTime)
	journal := s8CheckpointJournalBytes(t, approvalEvent, sessionEvent, planEvent, registerEvent, issueEvent)
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	decisionRef := ".claude/evidence/loop-REQ-001/g2/human/contract-approval.json"
	decisionBytes := write(decisionRef, map[string]any{
		"decision": "approve_contract", "decision_id": approvalID, "runtime_id": runtimeID,
		"case_id": caseID, "contract_id": contractID, "approval_hash": strings.Repeat("a", 64),
	})
	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": runtimeID, "revision": 5,
		"baseline": map[string]any{"generation": generation}, "lifecycle": map[string]any{"state": "bug_resolution", "phase": "planning"},
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"journal":       map[string]any{"last_sequence": 5, "last_event_id": packet.IssuanceEventID},
		"entities": map[string]any{
			"work_packets": []any{packetRow}, "submissions": []any{},
			"teams":  []any{map[string]any{"id": teamID, "kind": "builder", "manifest_ref": manifestRef, "manifest_sha256": workpacket.Digest(manifestBytes)}},
			"tasks":  []any{map[string]any{"id": taskID, "path": taskRef, "sha256": workpacket.Digest(taskBytes)}},
			"agents": []any{map[string]any{"id": ownerID, "role": "backend-builder", "team_id": teamID, "prompt_ref": manifestRef + "#" + manifestAssignmentID, "task_ids": []any{taskID}}},
		},
		"evidence": []any{map[string]any{
			"id": approvalID, "kind": "human_decision", "path": decisionRef, "sha256": workpacket.Digest(decisionBytes),
			"consumed_by": "S8-REPAIR-CONTRACT-APPROVAL", "consumed_at": "2026-10-14T12:00:00Z",
			"scope_refs": []any{"s8_contract_approval:" + runtimeID}, "produced_by": []any{"human-reviewer"},
		}},
		"review": map[string]any{
			"round":         round,
			"investigation": map[string]any{"case_id": caseID, "path": caseRef, "sha256": workpacket.Digest(caseBytes), "revision": 2, "status": "contract_approved"},
			"repair": map[string]any{
				"case_id": caseID, "contract_id": contractID, "contract_ref": contractRef, "contract_sha256": workpacket.Digest(contractBytes),
				"session_id": sessionID, "path": sessionRef, "sha256": workpacket.Digest(sessionBytes), "plan_ref": planRef, "plan_sha256": workpacket.Digest(planBytes),
				"status": "planning", "assignment_owners": map[string]any{repairAssignmentID: ownerID},
			},
		},
	}
	return repairPlanReportRuntimeFixture{root: root, packet: packet, state: state, journal: journal, approvalEvent: approvalEvent, sessionEvent: sessionEvent, planEvent: planEvent, registerEvent: registerEvent, issueEvent: issueEvent}
}

func repairPlanReportTestCanonicalMap(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestRepairPlanReportPacketIssueSourceClosureAndCheckpointFiltering(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	before, err := json.Marshal(f.state)
	if err != nil {
		t.Fatal(err)
	}
	packet, found, err := FindRepairPlanReportPacket(f.state, f.packet.ID)
	if err != nil || !found || packet.ID != f.packet.ID {
		t.Fatalf("FindRepairPlanReportPacket found=%t err=%v", found, err)
	}
	current, err := CurrentRepairPlanReportPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 || current[0].ID != f.packet.ID {
		t.Fatalf("CurrentRepairPlanReportPackets returned %d packets, err=%v", len(current), err)
	}
	if err := VerifyRepairPlanReportCurrent(f.root, f.state, f.packet); err != nil {
		t.Fatalf("fresh S9 phase/owner projection rejected: %v", err)
	}
	if err := VerifyRepairPlanReportPacketIssue(f.root, f.state, f.packet); err != nil {
		t.Fatalf("synthetic exact S9 source proof rejected: %v", err)
	}
	filtered, err := validateRepairPlanReportCheckpointIndices(f.root, f.state, f.journal)
	if err != nil {
		t.Fatalf("S9 typed checkpoint packet rejected: %v", err)
	}
	rows := filtered["entities"].(map[string]any)["work_packets"].([]any)
	if len(rows) != 0 {
		t.Fatalf("S9 packet subtype was not filtered for legacy recovery: %#v", rows)
	}
	if err := ValidateCheckpointIndices(f.root, f.state, f.journal); err != nil {
		t.Fatalf("full Runtime checkpoint index validation rejected valid synthetic S9 closure: %v", err)
	}
	after, _ := json.Marshal(f.state)
	if string(before) != string(after) {
		t.Fatal("lookup, verification, and checkpoint filtering mutated durable state")
	}
}

func TestRepairPlanReportPacketLookupRejectsMalformedRecordsWithoutLegacyFlagRequirement(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	entities := f.state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	duplicate := s8CheckpointCloneMap(t, rows[0].(map[string]any))
	entities["work_packets"] = append(rows, duplicate)
	if _, err := RepairPlanReportPackets(f.state); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate typed packet ID accepted: %v", err)
	}

	f = newRepairPlanReportRuntimeFixture(t)
	issued, err := RepairPlanReportPackets(f.state)
	if err != nil || len(issued) != 1 {
		t.Fatalf("original issued record is not readable: %v", err)
	}
	f.state["configuration"].(map[string]any)["optimization"].(map[string]any)["packet"] = false
	before, _ := json.Marshal(f.state)
	recorded, err := RepairPlanReportPackets(f.state)
	want, _ := json.Marshal(issued)
	got, _ := json.Marshal(recorded)
	after, _ := json.Marshal(f.state)
	if err != nil || string(want) != string(got) || string(before) != string(after) {
		t.Fatalf("legacy mode change erased issued facts or lookup changed state: %v", err)
	}
	// Closed typed structure remains mandatory; reading history grants no
	// authority to add an unsupported contract field or issue new work.
	rows = f.state["entities"].(map[string]any)["work_packets"].([]any)
	rows[0].(map[string]any)["unsupported_contract_field"] = true
	if _, err := RepairPlanReportPackets(f.state); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown contract field accepted by the original typed reader: %v", err)
	}
}

func TestRepairPlanReportPacketRejectsRegisteredSourceAndPhaseDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *repairPlanReportRuntimeFixture)
	}{
		{"case bytes", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.CaseRef, func(doc map[string]any) { doc["grouping_rationale"] = "changed case source" })
		}},
		{"approved contract bytes", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.ContractRef, func(doc map[string]any) { doc["root_cause_statement"] = "different cause" })
		}},
		{"session bytes", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.SessionRef, func(doc map[string]any) { doc["created_by"] = "other" })
		}},
		{"plan bytes", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.PlanRef, func(doc map[string]any) { doc["created_by"] = "other" })
		}},
		{"task bytes", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.TaskRef, func(doc map[string]any) { doc["objective"] = "changed objective" })
		}},
		{"manifest assignment bytes", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			mutateRepairPlanReportSource(t, f, f.packet.Binding.ManifestRef, func(doc map[string]any) {
				objectRows(doc["assignments"])[0]["grouping_rationale"] = "changed assignment"
			})
		}},
		{"team registered manifest sha", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			delete(objectRows(f.state["entities"].(map[string]any)["teams"])[0], "manifest_sha256")
		}},
		{"task registry tuple", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			objectRows(f.state["entities"].(map[string]any)["tasks"])[0]["sha256"] = strings.Repeat("0", 64)
		}},
		{"missing original approval event", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			f.journal = s8CheckpointJournalBytes(t, f.sessionEvent, f.planEvent, f.registerEvent, f.issueEvent)
			writeRepairPlanReportJournal(t, f)
		}},
		{"approval not consumed", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			objectRows(f.state["evidence"])[0]["consumed_by"] = "OTHER-GATE"
		}},
		{"approval artifact changed", func(t *testing.T, f *repairPlanReportRuntimeFixture) {
			ref := objectRows(f.state["evidence"])[0]["path"].(string)
			path := filepath.Join(f.root, filepath.FromSlash(ref))
			if err := os.WriteFile(path, []byte(`{"decision":"deny"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepairPlanReportRuntimeFixture(t)
			tc.mutate(t, &f)
			if err := VerifyRepairPlanReportPacketIssue(f.root, f.state, f.packet); err == nil {
				t.Fatal("source/journal tampering was accepted")
			}
		})
	}

	for _, tc := range []struct {
		name   string
		mutate func(*repairPlanReportRuntimeFixture)
	}{
		{"current lifecycle phase", func(f *repairPlanReportRuntimeFixture) { f.state["lifecycle"].(map[string]any)["phase"] = "repairing" }},
		{"current assignment owner", func(f *repairPlanReportRuntimeFixture) {
			f.state["review"].(map[string]any)["repair"].(map[string]any)["assignment_owners"].(map[string]any)[f.packet.Binding.RepairAssignmentID] = "builder-other"
		}},
		{"current manifest owner", func(f *repairPlanReportRuntimeFixture) {
			objectRows(f.state["entities"].(map[string]any)["agents"])[0]["prompt_ref"] = "other#assignment"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepairPlanReportRuntimeFixture(t)
			tc.mutate(&f)
			if err := VerifyRepairPlanReportCurrent(f.root, f.state, f.packet); err == nil {
				t.Fatal("fresh current phase/owner drift was accepted")
			}
		})
	}
}

func writeRepairPlanReportJournal(t *testing.T, f *repairPlanReportRuntimeFixture) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, ".claude", "loop-events.jsonl"), f.journal, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mutateRepairPlanReportSource(t *testing.T, f *repairPlanReportRuntimeFixture, ref string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	mutate(doc)
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRepairPlanReportPacketRejectsOriginalApprovalAndRegistrationEventTampering(t *testing.T) {
	for _, eventName := range []string{"approval", "session", "plan", "registration", "issue"} {
		t.Run(eventName, func(t *testing.T) {
			f := newRepairPlanReportRuntimeFixture(t)
			idx := map[string]int{"approval": 0, "session": 1, "plan": 2, "registration": 3, "issue": 4}[eventName]
			changes := []struct {
				name string
				edit func(map[string]any)
			}{
				{"idempotency key", func(e map[string]any) { e["idempotency_key"] = "wrong" }},
				{"generation", func(e map[string]any) { e["baseline_generation"] = 3 }},
			}
			if eventName == "approval" || eventName == "registration" || eventName == "issue" {
				changes = append(changes,
					struct {
						name string
						edit func(map[string]any)
					}{"actor", func(e map[string]any) { e["actor"] = map[string]any{"type": "agent", "id": "intruder"} }},
				)
				cursorEdit := func(e map[string]any) { e["from"] = map[string]any{"state": "bug_resolution", "phase": "repairing"} }
				if eventName == "approval" {
					cursorEdit = func(e map[string]any) { e["to"] = map[string]any{"state": "bug_resolution", "phase": "repairing"} }
				}
				changes = append(changes, struct {
					name string
					edit func(map[string]any)
				}{"cursor", cursorEdit})
			}
			if eventName == "session" {
				changes = append(changes,
					struct {
						name string
						edit func(map[string]any)
					}{"actor type/id", func(e map[string]any) { e["actor"] = map[string]any{"type": "orchestrator", "id": "main"} }},
					struct {
						name string
						edit func(map[string]any)
					}{"cursor", func(e map[string]any) { e["to"] = map[string]any{"state": "bug_resolution", "phase": "repairing"} }},
				)
			}
			if eventName == "plan" {
				changes = append(changes,
					struct {
						name string
						edit func(map[string]any)
					}{"actor type/id", func(e map[string]any) { e["actor"] = map[string]any{"type": "orchestrator", "id": "main"} }},
					struct {
						name string
						edit func(map[string]any)
					}{"cursor", func(e map[string]any) { e["to"] = map[string]any{"state": "bug_resolution", "phase": "repairing"} }},
				)
			}
			for _, change := range changes {
				t.Run(change.name, func(t *testing.T) {
					corrupt := s8CheckpointChangeJournalEvent(t, f.journal, idx, change.edit)
					inspection, err := controlproof.InspectJournalData(corrupt)
					if err == nil {
						err = verifyRepairPlanReportPacketIssue(f.root, f.state, f.packet, inspection)
					}
					if err == nil {
						t.Fatal("tampered original authority event accepted")
					}
				})
			}
		})
	}
}

func TestRepairPlanReportCheckpointRejectsOrphanIssuanceEvent(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	inspection, err := controlproof.InspectJournalData(s8CheckpointJournalBytes(t, f.approvalEvent, f.sessionEvent, f.planEvent, f.registerEvent))
	if err != nil {
		t.Fatal(err)
	}
	state := s8CheckpointCloneMap(t, f.state)
	state["entities"].(map[string]any)["work_packets"] = []any{}
	if _, err := validateRepairPlanReportCheckpointIndices(f.root, state, s8CheckpointJournalBytes(t, f.approvalEvent, f.sessionEvent, f.planEvent, f.registerEvent, f.issueEvent)); err == nil {
		t.Fatal("original packet issue event without a registered packet was accepted")
	}
	if inspection.EventIndex[f.packet.IssuanceEventID] == 0 {
		// The truncated journal intentionally contains no issue event. This is
		// only a sanity check that the orphan test used the full journal below.
		t.Log("registration-only journal has no issuance event, as expected")
	}
}

func TestRepairPlanReportFixtureUsesGenerationTwoAndExactReportPaths(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	if f.packet.Binding.BaselineGeneration != 2 || f.packet.Binding.RuntimeID != "loop-REQ-001" || !strings.Contains(f.packet.AnswerPath, "/g2/S9-plan/r1/") {
		t.Fatalf("fixture does not exercise the exact generation-two S9 tuple: %+v", f.packet.Binding)
	}
	if got := fmt.Sprintf("evt-register-%s-r4", f.packet.Binding.TeamID); f.registerEvent["event_id"] != got {
		t.Fatalf("fixture registration event is %v, want %s", f.registerEvent["event_id"], got)
	}
}

// The original issuer permits a later Builder to receive its Plan packet
// after a sibling has entered fixing. Verification must recognize that exact
// issuance while retaining the same actor, approval and source closure gates.
func TestRepairPlanReportPacketIssueAllowsOriginalFixingPhase(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	f.registerEvent["from"] = map[string]any{"state": "bug_resolution", "phase": "fixing"}
	f.registerEvent["to"] = map[string]any{"state": "bug_resolution", "phase": "fixing"}
	f.issueEvent["from"] = map[string]any{"state": "bug_resolution", "phase": "fixing"}
	f.issueEvent["to"] = map[string]any{"state": "bug_resolution", "phase": "fixing"}
	f.journal = s8CheckpointJournalBytes(t, f.approvalEvent, f.sessionEvent, f.planEvent, f.registerEvent, f.issueEvent)
	writeRepairPlanReportJournal(t, &f)
	if err := VerifyRepairPlanReportPacketIssue(f.root, f.state, f.packet); err != nil {
		t.Fatalf("original issuer fixing packet was rejected: %v", err)
	}
	f.issueEvent["actor"] = map[string]any{"type": "agent", "id": "intruder"}
	f.journal = s8CheckpointJournalBytes(t, f.approvalEvent, f.sessionEvent, f.planEvent, f.registerEvent, f.issueEvent)
	writeRepairPlanReportJournal(t, &f)
	if err := VerifyRepairPlanReportPacketIssue(f.root, f.state, f.packet); err == nil {
		t.Fatal("fixing phase granted packet authority to another actor")
	}
}
