package workpacket

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// This is a mechanical workpacket fixture. It exercises neutral packet
// structure only; it does not claim that the source documents were issued,
// accepted, or read from a live Runtime/workspace authority.
func repairResultTestFixture(t *testing.T) (RepairResultBinding, RepairResultInputs) {
	t.Helper()
	sources, sourceInputs := repairPlanReportTestFixture(t)
	planReportRef := ".claude/review/repair/plan-reports/repair-plan-report-s9-result-1.json"
	planReport := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_plan_report", "report_id": "repair-plan-report-s9-result-1",
		"session_id": sources.SessionID, "plan_id": sources.PlanID, "assignment_id": sources.RepairAssignmentID,
		"assertion_ids": []string{"root-1"}, "agent_id": sources.OwnerAgentID,
		"plan":           "Reproduce the original invariant failure and make the smallest scoped correction.",
		"red_checks":     []any{map[string]any{"name": "original failure", "command": "go test ./internal/api", "result": "fail", "evidence_refs": []string{"test://api/red"}}},
		"proposed_paths": []string{"internal/api/payload.go"}, "status": "reported", "reported_at": "2026-10-14T00:00:00Z",
		"receipt": map[string]any{
			"contract": "s9-plan-report-receipt-v1", "packet_id": "packet-" + strings.Repeat("a", 64),
			"packet_sha256": strings.Repeat("a", 64), "answer_sha256": strings.Repeat("b", 64),
			"submitted_answer": map[string]any{"plan": "do the repair", "red_checks": []any{map[string]any{"name": "red", "command": "go test ./internal/api", "result": "fail", "evidence_refs": []string{"test://api/red"}}}, "proposed_paths": []string{"internal/api/payload.go"}},
			"owner_agent_id":   sources.OwnerAgentID, "runtime_id": sources.RuntimeID, "baseline_generation": sources.BaselineGeneration,
			"review_round": sources.ReviewRound, "case_id": sources.CaseID, "case_ref": sources.CaseRef, "case_sha256": sources.CaseSHA256,
			"contract_id": sources.ContractID, "contract_ref": sources.ContractRef, "contract_sha256": sources.ContractSHA256,
			"session_id": sources.SessionID, "session_ref": sources.SessionRef, "session_sha256": sources.SessionSHA256,
			"plan_id": sources.PlanID, "plan_ref": sources.PlanRef, "plan_sha256": sources.PlanSHA256,
			"repair_assignment_id": sources.RepairAssignmentID, "repair_assignment_sha256": sources.RepairAssignmentSHA256,
			"manifest_assignment_id": sources.ManifestAssignmentID, "manifest_assignment_sha256": sources.ManifestAssignmentSHA256,
			"team_id": sources.TeamID, "task_id": sources.TaskID, "task_ref": sources.TaskRef, "task_sha256": sources.TaskSHA256,
			"manifest_ref": sources.ManifestRef, "manifest_sha256": sources.ManifestSHA256,
			"execution_root": "/tmp/worker", "report_id": "repair-plan-report-s9-result-1", "report_ref": planReportRef,
			"work_complete": false, "issuance_event_id": "evt-repair-plan-report-packet-packet-" + strings.Repeat("a", 64),
			"accepted_event_id":               "evt-s9-plan-report-repair-plan-report-s9-result-1-r4",
			"accepted_idempotency_key":        "runtime:s9:plan-report:repair-plan-report-s9-result-1:3",
			"accepted_after_runtime_revision": 4, "accepted_sequence": 4,
		},
	}
	reportBytes := repairPlanReportTestRaw(t, planReport)
	execution := map[string]any{
		"assignment_record":        map[string]any{"runtime_id": sources.RuntimeID, "baseline_generation": sources.BaselineGeneration, "assignment_id": sources.ManifestAssignmentID},
		"assignment_record_sha256": strings.Repeat("c", 64), "worker_checks": []any{}, "check_profiles": []any{},
		"purpose": "repair", "work_packet_id": "packet-repair-plan", "stage_entry": 1, "check_location": "main",
		"platform_session_id": "session-platform-builder-1", "assignment_id": sources.ManifestAssignmentID,
		"runtime_id": sources.RuntimeID, "baseline_generation": sources.BaselineGeneration, "execution_generation": 2,
		"agent_id": sources.OwnerAgentID, "worktree_path": "/tmp/worker", "branch": "fix/api-payload", "target_branch": "main",
		"base_commit": strings.Repeat("d", 40), "write_paths": []string{"internal/api"}, "required_checks": []string{},
		"inputs": map[string]string{"plan": sources.PlanSHA256}, "bootstrap_sha256": strings.Repeat("e", 64),
	}
	executionBytes, err := canonicalJSON(mustJSON(t, execution))
	if err != nil {
		t.Fatal(err)
	}
	binding := RepairResultBinding{
		Sources: sources, PlanReportRef: planReportRef, PlanReportSHA256: strings.Repeat("f", 64),
		ExecutionSHA256: Digest(executionBytes), ReworkRef: "", ReworkSHA256: "",
	}
	inputs := RepairResultInputs{
		Sources: sourceInputs, PlanReport: reportBytes, Execution: json.RawMessage(executionBytes), ReworkReceipt: json.RawMessage("null"),
	}
	return binding, inputs
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func repairResultCompleteAnswer() map[string]any {
	return map[string]any{
		"verdict": "blocked",
		"before_fix_results": []any{map[string]any{
			"assertion_id": "root-1", "observed_red": "The read path returned no payload after the commit was accepted.", "evidence_refs": []string{"test://api/red-case"},
		}},
		"unit_results": []any{map[string]any{
			"unit_id": "unit-api", "status": "blocked", "evidence_refs": []string{"test://api/implementation"},
			"implementation_summary": "The scoped read path is updated, but the original environment check remains blocked.", "restored_invariant_refs": []string{"root-1"},
		}},
		"detection_gap_artifacts": []string{}, "evidence_refs": []string{"test://api/red-case", "file://internal/api/payload.go"},
		"scope_deviations": []string{}, "residual_risks": []string{"The downstream compatibility check remains unverified."},
		"migration_ref": "", "rollback_ref": "",
	}
}

func repairResultFilledAnswerFile(t *testing.T, packet RepairResultPacket, answer map[string]any) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(mustJSON(t, packet.Draft), &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["answer"] = answer
	return mustJSON(t, envelope)
}

func TestBuildRepairResultIsStableAndSchemaValid(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	packet, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if packet.TemplateID != "s9-repair-result-v1" || packet.ResultID != "repair-result-"+strings.TrimPrefix(packet.ID, "packet-") || packet.IssuanceEventID != "evt-repair-result-packet-"+packet.ID {
		t.Fatalf("S9 packet identity mismatch: %#v", packet)
	}
	base := ".claude/work-packets/loop-REQ-001/g1/S9-result/r1/repair-assignment-unit-1/" + packet.ID + ".json"
	if packet.ViewPath != base || packet.AnswerPath != ".claude/submissions/loop-REQ-001/g1/S9-result/r1/repair-assignment-unit-1/"+packet.ID+"/builder-s9-1/answer.json" {
		t.Fatalf("S9 result paths mismatch: %q %q", packet.ViewPath, packet.AnswerPath)
	}
	for name, value := range map[string]any{
		"repair-result-work-packet.schema.json": packet,
		"stage-answer-s9-result.schema.json":    packet.Draft,
	} {
		data := mustJSON(t, value)
		if err := schema.NewEmbeddedValidator().ValidateBytes(name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	digest, err := RepairResultPacketDigest(packet)
	if err != nil || !digestPattern.MatchString(digest) {
		t.Fatalf("packet digest = %q, %v", digest, err)
	}
	encoded, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var restored RepairResultPacket
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packet, restored) {
		t.Fatal("packet JSON round trip changed its canonical projection")
	}
	if restoredDigest, err := RepairResultPacketDigest(restored); err != nil || restoredDigest != digest {
		t.Fatalf("restored digest = %q, want %q, err=%v", restoredDigest, digest, err)
	}

	inputs.PlanReport = json.RawMessage(" \n" + string(inputs.PlanReport) + " \n")
	inputs.Execution = json.RawMessage(" \n" + string(inputs.Execution) + " \n")
	other, err := BuildRepairResult(binding, inputs)
	if err != nil || other.ID != packet.ID {
		t.Fatalf("semantic source whitespace changed packet identity: %q (%v)", other.ID, err)
	}
}

func TestRepairResultResumeIsOptionalForLegacyIdentityAndClosedWhenPresent(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	original, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	verifiedDigest, err := RepairResultPacketDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest, err := RepairResultPacketContentDigest(original)
	if err != nil || contentDigest != verifiedDigest {
		t.Fatalf("canonical content digest=%q, verified digest=%q, err=%v", contentDigest, verifiedDigest, err)
	}
	if strings.Contains(string(originalBytes), `"resume"`) {
		t.Fatal("nil Resume changed the legacy packet wire bytes")
	}

	predecessorSHA, err := RepairResultPacketDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	binding.Resume = &RepairResultResume{
		PredecessorPacketID: original.ID, PredecessorPacketSHA256: predecessorSHA,
		BlockerRef: ".claude/evidence/blocked.json", BlockerSHA256: strings.Repeat("a", 64),
		ResolutionMessageRef: ".claude/evidence/resolution.json", ResolutionMessageSHA256: strings.Repeat("b", 64),
		ResolutionEventID:        "evt-agent-" + binding.Sources.OwnerAgentID + "-blocker_resolved-r11",
		ResolutionIdempotencyKey: "runtime:agent:" + binding.Sources.OwnerAgentID + ":blocker_resolved:10",
		ResolutionBeforeRevision: 10, ResolutionAfterRevision: 11, ResolutionSequence: 11,
	}
	resumed, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatalf("build packet with exact Resume shape: %v", err)
	}
	if resumed.ID == original.ID {
		t.Fatal("Resume tuple did not create a distinct deterministic answer identity")
	}
	data, err := json.Marshal(resumed)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-work-packet.schema.json", data); err != nil {
		t.Fatalf("Resume packet schema: %v", err)
	}
	digest, err := RepairResultResumeSHA256(binding.Resume)
	if err != nil || !digestPattern.MatchString(digest) {
		t.Fatalf("Resume digest = %q, %v", digest, err)
	}

	bad := *binding.Resume
	bad.ResolutionEventID = "opaque-original-resolution-event"
	if err := ValidateRepairResultResume(binding.Sources.OwnerAgentID, &bad); err != nil {
		t.Fatalf("a shape reader inferred authority from event-name spelling: %v", err)
	}
	if changed, err := RepairResultResumeSHA256(&bad); err != nil || changed == digest {
		t.Fatalf("actual Resume association change lost its frozen identity: %q %v", changed, err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	wire["binding"].(map[string]any)["resume"].(map[string]any)["unexpected"] = true
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-work-packet.schema.json", mustJSON(t, wire)); err == nil {
		t.Fatal("unknown Resume property was accepted by the closed packet schema")
	}
}

func TestValidateRepairResultAnswerPreservesNineProfessionalFields(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	packet, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	draft := mustJSON(t, packet.Draft)
	if _, fields := ValidateRepairResultAnswer(packet, draft); len(fields) == 0 || fields[0].Pointer != "/answer/verdict" {
		t.Fatalf("draft should not imply a professional verdict: %#v", fields)
	}
	answer := repairResultCompleteAnswer()
	complete := repairResultFilledAnswerFile(t, packet, answer)
	projection, fields := ValidateRepairResultAnswer(packet, complete)
	if len(fields) != 0 {
		t.Fatalf("complete professional answer rejected: %#v", fields)
	}
	var projected map[string]any
	if err := json.Unmarshal(projection, &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected) != 9 || projected["verdict"] != "blocked" || projected["before_fix_results"] == nil || projected["unit_results"] == nil {
		t.Fatalf("canonical projection must preserve exactly nine professional fields: %#v", projected)
	}
	for key := range projected {
		switch key {
		case "verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs", "scope_deviations", "residual_risks", "migration_ref", "rollback_ref":
		default:
			t.Fatalf("machine-owned field %q leaked into answer projection", key)
		}
	}

	for _, verdict := range []string{"completed", "blocked", "contract_conflict", "stale"} {
		changed := repairResultCompleteAnswer()
		changed["verdict"] = verdict
		if _, fields := ValidateRepairResultAnswer(packet, repairResultFilledAnswerFile(t, packet, changed)); len(fields) != 0 {
			t.Fatalf("neutral answer validator rejected preserved verdict %q: %#v", verdict, fields)
		}
	}
}

func TestValidateRepairResultAnswerRejectsInvalidProfessionalFields(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	packet, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"wrong assertion": func(a map[string]any) {
			a["before_fix_results"] = []any{map[string]any{"assertion_id": "symptom-1", "observed_red": "red", "evidence_refs": []string{"test://red"}}}
		},
		"empty observation": func(a map[string]any) {
			a["before_fix_results"] = []any{map[string]any{"assertion_id": "root-1", "observed_red": "  ", "evidence_refs": []string{"test://red"}}}
		},
		"missing unit": func(a map[string]any) { a["unit_results"] = []any{} },
		"wrong unit": func(a map[string]any) {
			a["unit_results"] = []any{map[string]any{"unit_id": "unit-other", "status": "pass", "evidence_refs": []string{"test://unit"}, "implementation_summary": "implemented", "restored_invariant_refs": []string{}}}
		},
		"empty summary":              func(a map[string]any) { a["unit_results"].([]any)[0].(map[string]any)["implementation_summary"] = " " },
		"invented machine field":     func(a map[string]any) { a["checks"] = []any{} },
		"null evidence array":        func(a map[string]any) { a["evidence_refs"] = nil },
		"missing professional field": func(a map[string]any) { delete(a, "migration_ref") },
	} {
		t.Run(name, func(t *testing.T) {
			answer := repairResultCompleteAnswer()
			mutate(answer)
			if _, fields := ValidateRepairResultAnswer(packet, repairResultFilledAnswerFile(t, packet, answer)); len(fields) == 0 {
				t.Fatal("malformed or machine-owned answer was accepted")
			}
		})
	}

	complete := repairResultFilledAnswerFile(t, packet, repairResultCompleteAnswer())
	want, fields := ValidateRepairResultAnswer(packet, complete)
	if len(fields) != 0 {
		t.Fatalf("complete professional answer rejected: %#v", fields)
	}
	for _, metadata := range []map[string]any{
		{"answer": repairResultCompleteAnswer()},
		{"answer": repairResultCompleteAnswer(), "title": nil, "instruction": "edited prompt", "read": "changed", "questions": map[string]any{"verdict": map[string]any{"example": map[string]any{"note": "expected omitted"}}}},
	} {
		got, fields := ValidateRepairResultAnswer(packet, mustJSON(t, metadata))
		if len(fields) != 0 || string(got) != string(want) {
			t.Fatalf("display metadata changed professional acceptance: fields=%#v answer=%s", fields, got)
		}
	}
	if _, fields := ValidateRepairResultAnswer(packet, append(complete, []byte(` {"answer":{}}`)...)); len(fields) == 0 {
		t.Fatal("second top-level JSON value was accepted")
	}
}

func TestBuildRepairResultSeparatesDraftIdentityFromSourceAuthority(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	original, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest := binding
	wrongDigest.ExecutionSHA256 = strings.Repeat("0", 64)
	if changed, err := BuildRepairResult(wrongDigest, inputs); err != nil || changed.ID == original.ID {
		t.Fatalf("draft did not freeze its actual supplied association: %+v %v", changed.Binding, err)
	}

	var execution map[string]any
	if err := json.Unmarshal(inputs.Execution, &execution); err != nil {
		t.Fatal(err)
	}
	execution["status"] = "ready"
	inputs.Execution = json.RawMessage(mustJSON(t, execution))
	binding.ExecutionSHA256, _ = canonicalDigest(inputs.Execution)
	if _, err := BuildRepairResult(binding, inputs); err == nil || !strings.Contains(err.Error(), "registered schema") {
		t.Fatalf("mutable status entered the closed Execution definition: %v", err)
	}
	// The closed shape is a draft-format guarantee. The original Runtime getter
	// separately compares the supplied association with the registered source.

	binding, inputs = repairResultTestFixture(t)
	var planReport map[string]any
	if err := json.Unmarshal(inputs.PlanReport, &planReport); err != nil {
		t.Fatal(err)
	}
	planReport["agent_id"] = "other-owner"
	inputs.PlanReport = json.RawMessage(mustJSON(t, planReport))
	if _, err := BuildRepairResult(binding, inputs); err == nil {
		t.Fatal("PlanReport from a different owner was accepted")
	}

	binding, inputs = repairResultTestFixture(t)
	inputs.ReworkReceipt = nil
	if _, err := BuildRepairResult(binding, inputs); err == nil {
		t.Fatal("missing explicit first-attempt Rework null was accepted")
	}
}

func TestRepairResultNativeDeliveryCannotRequireBootstrapOrCreateSession(t *testing.T) {
	for _, tc := range []struct {
		name, purpose, location  string
		bootstrap, session, want bool
	}{
		{"observed native Git source", "native_delivery", "main", false, false, true},
		{"legacy still needs bootstrap", "", "main", false, false, false},
		{"native forbids bootstrap", "native_delivery", "main", true, false, false},
		{"native forbids session", "native_delivery", "main", false, true, false},
		{"native retains Main checks", "native_delivery", "worker", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding, inputs := repairResultTestFixture(t)
			var source map[string]any
			if err := json.Unmarshal(inputs.Execution, &source); err != nil {
				t.Fatal(err)
			}
			delete(source, "bootstrap_sha256")
			delete(source, "platform_session_id")
			delete(source, "purpose")
			if tc.purpose != "" {
				source["purpose"] = tc.purpose
			}
			source["check_location"] = tc.location
			if tc.bootstrap {
				source["bootstrap_sha256"] = strings.Repeat("b", 64)
			}
			if tc.session {
				source["platform_session_id"] = "reserved-session"
			}
			inputs.Execution, _ = json.Marshal(source)
			binding.ExecutionSHA256 = Digest(inputs.Execution)
			_, err := BuildRepairResult(binding, inputs)
			if (err == nil) != tc.want {
				t.Fatalf("native source boundary: accepted=%t want=%t err=%v", err == nil, tc.want, err)
			}
		})
	}
}

func TestBuildRepairResultAcceptsOriginalS9ExecutionWithoutOptionalS6Fields(t *testing.T) {
	optional := []string{
		"assignment_record", "assignment_record_sha256", "worker_checks",
		"purpose", "work_packet_id", "stage_entry", "platform_session_id",
	}
	for _, mode := range []string{"omitted", "explicit null"} {
		t.Run(mode, func(t *testing.T) {
			binding, inputs := repairResultTestFixture(t)
			var execution map[string]any
			if err := json.Unmarshal(inputs.Execution, &execution); err != nil {
				t.Fatal(err)
			}
			for _, key := range optional {
				if mode == "omitted" {
					delete(execution, key)
				} else {
					execution[key] = nil
				}
			}
			inputs.Execution = json.RawMessage(mustJSON(t, execution))
			binding.ExecutionSHA256, _ = canonicalDigest(inputs.Execution)
			packet, err := BuildRepairResult(binding, inputs)
			if err != nil {
				t.Fatalf("native S9 Execution with absent S6-only fields rejected: %v", err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-work-packet.schema.json", mustJSON(t, packet)); err != nil {
				t.Fatalf("minimal native S9 packet fails its closed schema: %v", err)
			}

			// Optional means absent/null only; it does not open the projection to
			// future or mutable Execution fields.
			execution["unregistered_progress"] = "ready"
			unknownInputs := inputs
			unknownInputs.Execution = json.RawMessage(mustJSON(t, execution))
			unknownBinding := binding
			unknownBinding.ExecutionSHA256, _ = canonicalDigest(unknownInputs.Execution)
			if _, err := BuildRepairResult(unknownBinding, unknownInputs); err == nil {
				t.Fatal("Execution with an unknown field was accepted")
			}

			if mode == "omitted" {
				execution["assignment_record"] = nil // digest without its reference
				partialInputs := inputs
				partialInputs.Execution = json.RawMessage(mustJSON(t, execution))
				partialBinding := binding
				partialBinding.ExecutionSHA256, _ = canonicalDigest(partialInputs.Execution)
				if _, err := BuildRepairResult(partialBinding, partialInputs); err == nil {
					t.Fatal("one-sided null Assignment Record tuple was accepted")
				}
			}
		})
	}
}

func canonicalDigest(data []byte) (string, error) {
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}
	return Digest(canonical), nil
}
