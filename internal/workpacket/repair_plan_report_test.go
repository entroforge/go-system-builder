package workpacket

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func repairPlanReportTestFixture(t *testing.T) (RepairPlanReportBinding, RepairPlanReportInputs) {
	t.Helper()
	const (
		caseID      = "investigation-case-s9-1"
		contractID  = "repair-contract-s9-1"
		sessionID   = "repair-session-s9-1"
		planID      = "repair-plan-s9-1"
		assignID    = "repair-assignment-unit-1"
		manifestID  = "assignment-s9-unit-1"
		taskID      = "TASK-S9-UNIT-1"
		teamID      = "workgroup-s9-session-1-unit-1"
		ownerID     = "builder-s9-1"
		contractRef = ".claude/review/investigation/contracts/repair-contract-s9-1-r1.json"
		sessionRef  = ".claude/review/repair/sessions/repair-session-s9-1.json"
		planRef     = ".claude/review/repair/plans/repair-plan-s9-1.json"
	)
	prospective := []string{"internal/api"}
	forbidden := []string{"internal/api/generated"}
	assignment := map[string]any{
		"assignment_id": assignID, "unit_ids": []string{"unit-api"}, "assertion_ids": []string{"root-1"},
		"depends_on": []string{}, "resource_locks": []string{}, "status": "queued",
		"scope": []string{"internal/api"}, "contract_ref": contractRef,
	}
	manifestAssignment := map[string]any{
		"assignment_id": manifestID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"scope": []string{"internal/api"}, "agent_id": ownerID, "agent_definition_ref": ".claude/agents/backend-builder.md",
		"skill_refs": []string{"testing-strategy"}, "read_paths": []string{planRef}, "write_paths": []string{"internal/api"},
		"output_paths": []string{".claude/evidence"}, "depends_on": []string{}, "reuse_decision": "create",
		"grouping_rationale": "one bounded repair unit", "status": "planned", "dispatch_mode": "plan_checkpoint",
	}
	caseDoc := map[string]any{
		"schema_version": "1.0.0", "case_id": caseID, "revision": 2, "status": "contract_approved",
		"source_finding_ids": []string{"finding-s9-1"}, "observation_batch_id": "observation-batch-s9-1",
		"observation_batch_ref": ".claude/evidence/observation-batch-s9-1.json", "observation_batch_sha256": strings.Repeat("a", 64),
		"baseline_generation": 1, "baseline_digest": strings.Repeat("b", 64), "grouping_rationale": "one causal failure",
		"unexplained_finding_ids": []string{}, "failure_boundary_refs": []string{}, "cross_layer_trace": nil,
		"evidence_gaps": []string{}, "hypotheses": []any{}, "hypothesis_results": []any{}, "causal_model": nil,
		"primary_root_cause": nil, "blast_radius": nil, "detection_gap": nil, "route": nil, "route_reason": nil,
		"repair_contract_ref": contractRef, "repair_contract_sha256": strings.Repeat("c", 64),
	}
	contractDoc := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": contractID, "case_id": caseID, "revision": 1, "status": "approved",
		"source_finding_ids": []string{"finding-s9-1"}, "root_cause_statement": "the API drops a committed value",
		"violated_invariant": "a committed value remains readable", "causal_model_ref": "test://causal-model",
		"architecture_intent": "preserve the committed payload", "repair_units": []any{map[string]any{"id": "unit-api", "description": "repair the API", "scope": []string{"internal/api"}}},
		"prospective_scope": prospective, "forbidden_scope": forbidden, "symptom_assertions": []string{"symptom"},
		"root_invariant_assertions": []string{"root"}, "detection_gap_assertions": []string{"gap"}, "stop_escalation_conditions": []string{"stop"},
	}
	sessionDoc := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_session", "session_id": sessionID,
		"contract_ref": contractRef, "contract_sha256": strings.Repeat("c", 64), "runtime_id": "loop-REQ-001", "req_id": "REQ-001",
		"baseline_generation": 1, "baseline_artifacts": []any{}, "baseline_digest": strings.Repeat("d", 64),
		"status": "planned", "created_by": "main", "created_at": "2026-10-14T00:00:00Z",
	}
	planDoc := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_plan", "plan_id": planID, "session_id": sessionID,
		"contract_id": contractID, "contract_ref": contractRef, "contract_sha256": strings.Repeat("c", 64),
		"units":       []any{map[string]any{"id": "unit-api", "description": "repair the API", "scope": []string{"internal/api"}, "assertion_ids": []string{"root-1"}, "depends_on": []string{}, "resource_locks": []string{}}},
		"assignments": []any{assignment}, "execution_policy": "coverage_complete", "prospective_scope": prospective,
		"forbidden_scope": forbidden, "created_by": "main", "created_at": "2026-10-14T00:00:00Z",
	}
	taskDoc := map[string]any{
		"task_id": taskID, "session_id": sessionID, "plan_id": planID, "assignment_id": assignID,
		"unit_ids": []string{"unit-api"}, "scope": []string{"internal/api"}, "depends_on": []string{}, "resource_locks": []string{},
		"state": "reviewed", "objective": "restore the API invariant", "instruction": "read the frozen repair sources",
		"next_action": "submit the pre-execution PlanReport",
	}
	binding := RepairPlanReportBinding{
		RuntimeID: "loop-REQ-001", BaselineGeneration: 1, ReviewRound: 1,
		CaseID: caseID, CaseRef: ".claude/review/investigation/cases/investigation-case-s9-1-r2.json", CaseSHA256: strings.Repeat("e", 64),
		ContractID: contractID, ContractRef: contractRef, ContractSHA256: strings.Repeat("c", 64),
		SessionID: sessionID, SessionRef: sessionRef, SessionSHA256: strings.Repeat("d", 64),
		PlanID: planID, PlanRef: planRef, PlanSHA256: strings.Repeat("f", 64),
		RepairAssignmentID: assignID, ManifestAssignmentID: manifestID,
		TeamID: teamID, TaskID: taskID, TaskRef: ".claude/workgroups/REQ-001/TASK-S9-UNIT-1/TASK-S9-UNIT-1.json", TaskSHA256: strings.Repeat("1", 64),
		ManifestRef: ".claude/workgroups/REQ-001/TASK-S9-UNIT-1/manifest.json", ManifestSHA256: strings.Repeat("2", 64), OwnerAgentID: ownerID,
	}
	inputs := RepairPlanReportInputs{
		Case: repairPlanReportTestRaw(t, caseDoc), Contract: repairPlanReportTestRaw(t, contractDoc),
		Session: repairPlanReportTestRaw(t, sessionDoc), Plan: repairPlanReportTestRaw(t, planDoc),
		Task: repairPlanReportTestRaw(t, taskDoc), RepairAssignment: repairPlanReportTestRaw(t, assignment),
		ManifestAssignment: repairPlanReportTestRaw(t, manifestAssignment),
		ProspectiveScope:   append([]string{}, prospective...), ForbiddenScope: append([]string{}, forbidden...),
	}
	canonicalAssignment, err := canonicalJSON(inputs.RepairAssignment)
	if err != nil {
		t.Fatal(err)
	}
	binding.RepairAssignmentSHA256 = Digest(canonicalAssignment)
	canonicalManifestAssignment, err := canonicalJSON(inputs.ManifestAssignment)
	if err != nil {
		t.Fatal(err)
	}
	binding.ManifestAssignmentSHA256 = Digest(canonicalManifestAssignment)
	return binding, inputs
}

func repairPlanReportTestRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(data)
}

func TestBuildRepairPlanReportIsStableAndSchemaValid(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	packet, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if packet.TemplateID != "s9-plan-report-v1" || packet.ReportID != "repair-plan-report-"+strings.TrimPrefix(packet.ID, "packet-") || packet.IssuanceEventID != "evt-repair-plan-report-packet-"+packet.ID {
		t.Fatalf("S9 packet identity mismatch: %#v", packet)
	}
	base := ".claude/work-packets/loop-REQ-001/g1/S9-plan/r1/repair-assignment-unit-1/" + packet.ID + ".json"
	if packet.ViewPath != base || packet.AnswerPath != strings.TrimSuffix(strings.Replace(base, ".claude/work-packets/", ".claude/submissions/", 1), ".json")+"/builder-s9-1/answer.json" {
		t.Fatalf("S9 packet paths differ from the stable owner projection: %q %q", packet.ViewPath, packet.AnswerPath)
	}
	for name, value := range map[string]any{
		"repair-plan-report-work-packet.schema.json": packet,
		"stage-answer-s9-plan.schema.json":           packet.Draft,
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes(name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	first, err := RepairPlanReportPacketDigest(packet)
	if err != nil || !digestPattern.MatchString(first) {
		t.Fatalf("packet digest = %q, %v", first, err)
	}
	encoded, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var restored RepairPlanReportPacket
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packet, restored) {
		t.Fatal("canonical packet bytes should survive JSON round trip")
	}
	second, err := RepairPlanReportPacketDigest(restored)
	if err != nil || first != second {
		t.Fatalf("restored digest = %q, want %q; err=%v", second, first, err)
	}

	inputs.Case = json.RawMessage(" \n" + string(inputs.Case) + " \n")
	other, err := BuildRepairPlanReport(binding, inputs)
	if err != nil || other.ID != packet.ID {
		t.Fatalf("semantically identical frozen documents changed packet identity: %q (%v)", other.ID, err)
	}
}

func TestValidateRepairPlanReportAnswerKeepsOnlyProfessionalFields(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	packet, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	draftBytes, err := json.Marshal(packet.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, fields := ValidateRepairPlanReportAnswer(packet, draftBytes); len(fields) == 0 || fields[0].Pointer != "/answer/plan" {
		t.Fatalf("null draft should be incomplete at plan: %#v", fields)
	}
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	answer := draft["answer"].(map[string]any)
	answer["plan"] = "First reproduce the failure, then change only the API boundary and rerun the original assertion."
	answer["red_checks"] = []any{map[string]any{
		"name": "original failure", "command": "go test ./internal/api -run TestPayloadRoundTrip", "result": "fail", "evidence_refs": []string{"test://repair-plan/before-fix"},
	}}
	answer["proposed_paths"] = []string{"internal/api/payload.go"}
	completeBytes, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	projection, fields := ValidateRepairPlanReportAnswer(packet, completeBytes)
	if len(fields) != 0 {
		t.Fatalf("complete plan report rejected: %#v", fields)
	}
	var projected map[string]any
	if err := json.Unmarshal(projection, &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected) != 3 || projected["plan"] == nil || projected["red_checks"] == nil || projected["proposed_paths"] == nil {
		t.Fatalf("answer projection must contain only three professional fields: %#v", projected)
	}

	for _, metadata := range []map[string]any{
		{"answer": answer},
		{"answer": answer, "title": nil, "instruction": "edited prompt", "read": "changed", "questions": map[string]any{"plan": map[string]any{"example": map[string]any{"note": "expected omitted"}}}},
	} {
		changed, _ := json.Marshal(metadata)
		got, fields := ValidateRepairPlanReportAnswer(packet, changed)
		if len(fields) != 0 || !bytes.Equal(got, projection) {
			t.Fatalf("display metadata changed professional acceptance: fields=%#v answer=%s", fields, got)
		}
	}

	for name, mutate := range map[string]func(map[string]any){
		"noncanonical": func(a map[string]any) { a["proposed_paths"] = []any{"internal/api/../api/payload.go"} },
		"empty-reference": func(a map[string]any) {
			a["red_checks"] = []any{map[string]any{"name": "red", "command": "go test ./...", "result": "fail", "evidence_refs": []string{""}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var candidate map[string]any
			if err := json.Unmarshal(completeBytes, &candidate); err != nil {
				t.Fatal(err)
			}
			candidateAnswer := candidate["answer"].(map[string]any)
			mutate(candidateAnswer)
			candidateBytes, _ := json.Marshal(candidate)
			if _, fields := ValidateRepairPlanReportAnswer(packet, candidateBytes); len(fields) == 0 {
				t.Fatal("invalid S9 answer was accepted")
			}
		})
	}
	for _, paths := range [][]string{{}, nil} {
		var candidate map[string]any
		if err := json.Unmarshal(completeBytes, &candidate); err != nil {
			t.Fatal(err)
		}
		candidate["answer"].(map[string]any)["proposed_paths"] = paths
		data, _ := json.Marshal(candidate)
		_, fields := ValidateRepairPlanReportAnswer(packet, data)
		if paths != nil && len(fields) != 0 {
			t.Fatalf("explicit no-change paths rejected: %#v", fields)
		}
		if paths == nil && (len(fields) == 0 || fields[0].Pointer != "/answer/proposed_paths") {
			t.Fatalf("null paths must request an explicit array: %#v", fields)
		}
	}
	for _, path := range []string{"docs/requirements/REQ-001.md", "internal/api/generated/client.go"} {
		var candidate map[string]any
		if err := json.Unmarshal(completeBytes, &candidate); err != nil {
			t.Fatal(err)
		}
		answer := candidate["answer"].(map[string]any)
		answer["proposed_paths"] = []string{path}
		answer["red_checks"] = []any{map[string]any{"name": "actual precheck", "command": "go test ./internal/api", "result": "pass", "evidence_refs": []string{".claude/evidence/precheck.log"}}}
		data, _ := json.Marshal(candidate)
		if _, fields := ValidateRepairPlanReportAnswer(packet, data); len(fields) != 0 {
			t.Fatalf("actual pass and proposed path must be recorded: %#v", fields)
		}
	}
	for _, ref := range []string{"evidence-red-1", ".claude/evidence/red.log", "test://repair-plan/before-fix"} {
		t.Run(ref, func(t *testing.T) {
			var candidate map[string]any
			if err := json.Unmarshal(completeBytes, &candidate); err != nil {
				t.Fatal(err)
			}
			candidate["answer"].(map[string]any)["red_checks"] = []any{map[string]any{"name": "red", "command": "go test ./...", "result": "fail", "evidence_refs": []string{ref}}}
			candidateBytes, _ := json.Marshal(candidate)
			if _, fields := ValidateRepairPlanReportAnswer(packet, candidateBytes); len(fields) != 0 {
				t.Fatalf("non-empty evidence reference was rejected by format: %#v", fields)
			}
		})
	}
}

func TestBuildRepairPlanReportRejectsCrossSourceDrift(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	if _, err := BuildRepairPlanReport(binding, inputs); err != nil {
		t.Fatal(err)
	}
	var task map[string]any
	if err := json.Unmarshal(inputs.Task, &task); err != nil {
		t.Fatal(err)
	}
	task["assignment_id"] = "repair-assignment-other"
	inputs.Task = repairPlanReportTestRaw(t, task)
	if _, err := BuildRepairPlanReport(binding, inputs); err == nil || !strings.Contains(err.Error(), "TASK") {
		t.Fatalf("TASK/RepairAssignment tuple drift was accepted: %v", err)
	}

	binding, inputs = repairPlanReportTestFixture(t)
	var plan map[string]any
	if err := json.Unmarshal(inputs.Plan, &plan); err != nil {
		t.Fatal(err)
	}
	plan["contract_sha256"] = strings.Repeat("a", 64)
	inputs.Plan = repairPlanReportTestRaw(t, plan)
	if _, err := BuildRepairPlanReport(binding, inputs); err == nil {
		t.Fatal("plan bound to another Contract digest was accepted")
	}

	binding, inputs = repairPlanReportTestFixture(t)
	inputs.ForbiddenScope = nil
	if _, err := BuildRepairPlanReport(binding, inputs); err == nil {
		t.Fatal("omitted forbidden_scope array was silently treated as empty")
	}
}

func TestRepairPlanReportInputRawSourcesAreCanonicalized(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	first, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(inputs.Task, &doc); err != nil {
		t.Fatal(err)
	}
	var reversed bytes.Buffer
	// Reverse only the outer key order. Values and the physical Task SHA pin
	// remain source-authority concerns; this DTO uses canonical document JSON.
	ordered := []string{"next_action", "instruction", "objective", "state", "resource_locks", "depends_on", "scope", "unit_ids", "assignment_id", "plan_id", "session_id", "task_id"}
	reversed.WriteByte('{')
	for i, key := range ordered {
		if i > 0 {
			reversed.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(key)
		valueJSON, _ := json.Marshal(doc[key])
		reversed.Write(keyJSON)
		reversed.WriteByte(':')
		reversed.Write(valueJSON)
	}
	reversed.WriteByte('}')
	inputs.Task = reversed.Bytes()
	second, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("Task JSON key order changed packet identity: %s != %s", first.ID, second.ID)
	}
}

func TestRepairPlanReportPreservesApprovedDirectoryScopeBoundary(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	inputs.ForbiddenScope = []string{"internal/api/generated/"}
	for _, target := range []*json.RawMessage{&inputs.Contract, &inputs.Plan} {
		var doc map[string]any
		if err := json.Unmarshal(*target, &doc); err != nil {
			t.Fatal(err)
		}
		doc["forbidden_scope"] = inputs.ForbiddenScope
		*target = repairPlanReportTestRaw(t, doc)
	}
	before := append([]byte(nil), inputs.Contract...)
	packet, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, inputs.Contract) || !reflect.DeepEqual(packet.Inputs.ForbiddenScope, inputs.ForbiddenScope) {
		t.Fatal("approved scope bytes changed")
	}
	for _, path := range []string{"internal/api/payload.go", "internal/api/generated/output.go", "internal/api-other/output.go", "internal/api/../api/payload.go"} {
		var draft map[string]any
		data, _ := json.Marshal(packet.Draft)
		if err := json.Unmarshal(data, &draft); err != nil {
			t.Fatal(err)
		}
		answer := draft["answer"].(map[string]any)
		answer["plan"] = "Repair only the authorized API boundary."
		answer["red_checks"] = []any{map[string]any{"name": "original failure", "command": "go test ./internal/api", "result": "fail", "evidence_refs": []string{"test://repair-plan/before-fix"}}}
		answer["proposed_paths"] = []string{path}
		data, _ = json.Marshal(draft)
		_, fields := ValidateRepairPlanReportAnswer(packet, data)
		if (len(fields) == 0) != (path != "internal/api/../api/payload.go") {
			t.Fatalf("scope %s feedback: %+v", path, fields)
		}
	}
	// Professional proposed paths are observations, not execution permission.
	// The original consumer still owns scope enforcement; the packet must keep
	// its exact approved source instead of rewriting it to normalize spelling.
	var changed map[string]any
	if err := json.Unmarshal(inputs.Contract, &changed); err != nil {
		t.Fatal(err)
	}
	changed["forbidden_scope"] = []string{"internal/api/generated"}
	inputs.Contract = repairPlanReportTestRaw(t, changed)
	if _, err := BuildRepairPlanReport(binding, inputs); err == nil {
		t.Fatal("different approved Contract bytes accepted as the same scope source")
	}
	for _, invalid := range [][]string{{"internal/api", "internal/api/"}, {"../outside/"}, {"/outside/"}, {"internal/api//"}, {"all/"}} {
		if err := canonicalRepairPlanReportScope(invalid, true); err == nil {
			t.Fatalf("invalid or duplicate scope accepted: %v", invalid)
		}
	}
}
