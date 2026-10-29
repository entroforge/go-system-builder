package qualitygate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestVerifiedTaskPlanningPacketSourcesProjectsAcceptedInputsAndActualBatch(t *testing.T) {
	fixture := newTaskPlanningPacketSubjectFixture(t)
	facts, accepted, err := verifiedTaskPlanningPacketFacts(fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Fatal("accepted S4 answer was treated as unfilled")
	}

	registered := map[string]bool{fixture.packet.Binding.Requirement.Path: true}
	for _, contract := range fixture.packet.Inputs.Contracts {
		registered[contract.Path] = true
	}
	wantSources := map[string]workpacket.Subject{}
	for _, source := range fixture.packet.Inputs.Sources {
		if !registered[source.Path] {
			wantSources[source.Path] = source
		}
	}
	if !containsTaskPlanningSource(fixture.packet.Inputs.Sources, "docs/architecture/ARCHITECTURE-001.md") ||
		!containsTaskPlanningSource(fixture.packet.Inputs.Sources, "docs/design/decisions/ADR-001.md") ||
		!containsTaskPlanningSource(fixture.packet.Inputs.Sources, "docs/dev/tasks/TASK-template.md") ||
		!containsTaskPlanningSource(fixture.packet.Inputs.Sources, "docs/rules/dispatch-plan.md") {
		t.Fatal("S4 fixture did not include the transitive source/template/dispatch closure")
	}

	var envelope struct {
		SubjectRefs []workpacket.Subject `json:"subject_refs"`
	}
	if err := json.Unmarshal(fixture.envelopeBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	envelopeSubjects := map[string]workpacket.Subject{}
	for _, subject := range envelope.SubjectRefs {
		envelopeSubjects[subject.Path] = subject
	}
	gotSources, gotTasks := map[string]documentFact{}, map[string]documentFact{}
	var indexFact *documentFact
	for _, fact := range facts {
		switch fact.Kind {
		case "source":
			gotSources[fact.Path] = fact
		case "task":
			gotTasks[fact.Path] = fact
		case "task_index":
			copy := fact
			indexFact = &copy
		default:
			t.Fatalf("projected an unrelated fact kind: %#v", fact)
		}
	}
	if len(gotSources) != len(wantSources) {
		t.Fatalf("projected %d source facts for %d unregistered frozen sources", len(gotSources), len(wantSources))
	}
	for path, source := range wantSources {
		fact, ok := gotSources[path]
		if !ok {
			t.Errorf("omitted frozen source %s", path)
			continue
		}
		if fact.Status != "frozen" || fact.Generation != fixture.packet.Binding.Generation || fact.SHA256 != source.SHA256 || fact.Version != envelopeSubjects[path].Version {
			t.Errorf("frozen source fact for %s = %#v", path, fact)
		}
	}
	for path := range registered {
		if _, ok := gotSources[path]; ok {
			t.Errorf("reprojected already-registered REQ/contract %s", path)
		}
	}
	if indexFact == nil || indexFact.Path != *fixture.answer.IndexPath || indexFact.Status != "complete" || indexFact.SHA256 != fixture.artifacts[0].SHA256 {
		t.Fatalf("validated dispatch index fact = %#v", indexFact)
	}
	if len(gotTasks) != 2 {
		t.Fatalf("projected %d TASK facts; want both complete and cancelled batch members", len(gotTasks))
	}
	for _, ref := range fixture.artifacts[1:] {
		fact, ok := gotTasks[ref.Path]
		if !ok || fact.SHA256 != ref.SHA256 || fact.Version != ref.Version || fact.Generation != fixture.packet.Binding.Generation {
			t.Errorf("actual TASK fact for %s = %#v", ref.Path, fact)
		}
	}
	if gotTasks["docs/dev/tasks/TASK-REQ-001-01.md"].Status != "complete" || gotTasks["docs/dev/tasks/TASK-REQ-001-02.md"].Status != "cancelled" {
		t.Fatalf("TASK statuses were not preserved from the actual validated batch: %#v", gotTasks)
	}
}

func TestVerifiedTaskPlanningPacketSourcesReadsRecordedFactsWithoutHistoricalQualification(t *testing.T) {
	fixture := newTaskPlanningPacketSubjectFixture(t)
	fixture.submissionRow["content_sha256"] = workpacket.Digest([]byte("historical digest not replayed"))
	writeContractPacketSubjectFile(t, fixture.root, "docs/dev/tasks/TASK-template.md", "# changed current template\n")
	writeContractPacketSubjectFile(t, fixture.root, "docs/dev/tasks/TASK-REQ-001-02.md", "> Status: cancelled\n> Version: v1.0.0\nchanged\n")
	fixture.input.Files = taskPlanningReadOnlyView{root: fixture.root}
	facts, accepted, err := verifiedTaskPlanningPacketFacts(fixture.input)
	if err != nil || !accepted || len(facts) == 0 {
		t.Fatalf("record projection replayed old qualification: %v %v", accepted, err)
	}
}

func TestVerifiedTaskPlanningPacketSourcesReportsUnreadableRecordedEnvelope(t *testing.T) {
	fixture := newTaskPlanningPacketSubjectFixture(t)
	for _, raw := range fixture.input.Snapshot.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == fixture.submissionRow["evidence_id"] {
			if err := os.Remove(filepath.Join(fixture.root, row["path"].(string))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, accepted, err := verifiedTaskPlanningPacketFacts(fixture.input); !accepted || err == nil || !strings.Contains(err.Error(), "envelope") {
		t.Fatalf("missing recorded envelope not reported: accepted=%v err=%v", accepted, err)
	}
}

func TestTaskPlanningGateTreatsUnacceptedPacketAsMissingEvidence(t *testing.T) {
	fixture := newTaskPlanningPacketSubjectFixture(t)
	entities := fixture.input.Snapshot.State["entities"].(map[string]any)
	rows := entities["submissions"].([]any)
	kept := make([]any, 0, len(rows)-1)
	for _, raw := range rows {
		row := raw.(map[string]any)
		if row["packet_id"] != fixture.packet.ID {
			kept = append(kept, raw)
		}
	}
	entities["submissions"] = kept

	input := fixture.input
	input.GateID = "GATE-PLANNING-TASKS-COMPLETE"
	input.TransitionID = "TR-002"
	evaluator := NewEvaluator(&Registry{gates: map[string]GateSpec{
		input.GateID: {ID: input.GateID, TransitionID: input.TransitionID, CursorState: "planning", CursorPhase: "tasks", SemanticVersion: "1.0.0"},
	}})
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNotReady || len(result.Missing) != 1 || result.Missing[0] != "evidence:planning_task_record" || len(result.Conflicts) != 0 {
		t.Fatalf("unaccepted S4 packet result = status %q, missing %#v, conflicts %#v", result.Status, result.Missing, result.Conflicts)
	}
}

func TestTaskPlanningGateReportsUnreadableRecordedOutputAsConflict(t *testing.T) {
	fixture := newTaskPlanningPacketSubjectFixture(t)
	if err := os.Remove(filepath.Join(fixture.root, "docs/dev/tasks/TASK-REQ-001-01.md")); err != nil {
		t.Fatal(err)
	}
	input := fixture.input
	input.GateID = "GATE-PLANNING-TASKS-COMPLETE"
	input.TransitionID = "TR-002"
	evaluator := NewEvaluator(&Registry{gates: map[string]GateSpec{
		input.GateID: {ID: input.GateID, TransitionID: input.TransitionID, CursorState: "planning", CursorPhase: "tasks", SemanticVersion: "1.0.0"},
	}})
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusUnknown || result.ErrorCode != ErrorGateUnknown || len(result.Conflicts) == 0 || !strings.Contains(result.Conflicts[0], "TASK-REQ-001-01.md") {
		t.Fatalf("unreadable S4 output was hidden by the disk fallback: status %q code %q conflicts %#v", result.Status, result.ErrorCode, result.Conflicts)
	}
}

type taskPlanningPacketSubjectFixture struct {
	root          string
	input         Input
	packet        workpacket.TaskPlanningPacket
	answer        workpacket.TaskPlanningAnswer
	artifacts     []workpacket.Reference
	envelopeBytes []byte
	submissionRow map[string]any
}

func newTaskPlanningPacketSubjectFixture(t *testing.T) *taskPlanningPacketSubjectFixture {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".claude/evidence", "docs/requirements", "docs/architecture", "docs/design/decisions", "docs/dev/contracts", "docs/dev/tasks", "docs/rules"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := fileview.Disk{Root: root}
	reqPath := "docs/requirements/REQ-001.md"
	designPath := "docs/architecture/ARCHITECTURE-001.md"
	adrPath := "docs/design/decisions/ADR-001.md"
	contractIndexPath := "docs/dev/contracts/CONTRACTS-001.md"
	contractPath := "docs/dev/contracts/FE-001.md"
	req := []byte("# REQ-001\n> Status: locked\n> Version: v1.0.0\n")
	design := []byte("# Architecture\n> Status: locked\n> Version: v1.0.0\n")
	adr := []byte("# ADR-001\n> Status: accepted\n> Version: v1.0.0\n")
	contractIndex := []byte("# CONTRACTS-001\n> Requirement: REQ-001\n> Status: locked\n> Version: v1.0.0\n\n## 合同清单\n| Contract ID | Type | Name | Status | Version | Notes |\n|:---|:---|:---|:---|:---|:---|\n| FE-001 | frontend | ui | locked | v1.0.0 | entry |\n\n## 需求覆盖矩阵\n| Contract | Clause | Purpose |\n|:---|:---|:---|\n| FE-001 §1 | user view | visible |\n")
	contract := []byte("# FE-001\n> Status: locked\n> Version: v1.0.0\n")
	taskTemplate := []byte("# Task template\n")
	dispatchRules := []byte("# Dispatch plan rules\n")
	writeContractPacketSubjectFile(t, root, reqPath, string(req))
	writeContractPacketSubjectFile(t, root, designPath, string(design))
	writeContractPacketSubjectFile(t, root, adrPath, string(adr))
	writeContractPacketSubjectFile(t, root, contractIndexPath, string(contractIndex))
	writeContractPacketSubjectFile(t, root, contractPath, string(contract))
	writeContractPacketSubjectFile(t, root, "docs/dev/tasks/TASK-template.md", string(taskTemplate))
	writeContractPacketSubjectFile(t, root, "docs/rules/dispatch-plan.md", string(dispatchRules))

	reqRef := workpacket.Reference{ID: "REQ-001", Path: reqPath, Version: "v1.0.0", SHA256: workpacket.Digest(req)}
	designRef := workpacket.Reference{ID: "ARCHITECTURE-001", Path: designPath, Version: "v1.0.0", SHA256: workpacket.Digest(design)}
	indexRef := workpacket.Reference{ID: "CONTRACTS-001", Path: contractIndexPath, Version: "v1.0.0", SHA256: workpacket.Digest(contractIndex)}
	contractRef := workpacket.Reference{ID: "FE-001", Path: contractPath, Version: "v1.0.0", SHA256: workpacket.Digest(contract)}
	s3Binding := workpacket.PlanningBinding{RuntimeID: "runtime-qualitygate-task-sources", Generation: 1, StageEntry: 2, Stage: "S3", Owner: "Main", Requirement: reqRef}
	s3Packet, err := workpacket.BuildContracts(s3Binding, workpacket.ContractInputs{
		Designs:     []workpacket.Reference{designRef},
		Sources:     []workpacket.Subject{{Path: adrPath, Version: "v1.0.0", SHA256: workpacket.Digest(adr)}},
		Directories: map[string][]string{"docs/design/decisions": {"ADR-001.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s3Answer := workpacket.ContractAnswer{
		Conclusion: stringPointer("pass"), IndexPath: stringPointer(contractIndexPath), ContractPaths: []string{contractPath},
		Applicability: map[string]workpacket.ContractApplicability{
			"FE":   {Disposition: "required", Rationale: "A user-facing contract is required."},
			"BE":   {Disposition: "not_applicable", Rationale: "No backend behavior is in this REQ."},
			"SYNC": {Disposition: "not_applicable", Rationale: "No shared integration behavior is in this REQ."},
		},
	}
	s3Artifacts := []workpacket.Reference{indexRef, contractRef}
	s3Evidence, err := workpacket.ContractEvidence(s3Packet, "E-S3-001", s3Artifacts, s3Answer, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s3EvidenceBytes, err := json.Marshal(s3Evidence)
	if err != nil {
		t.Fatal(err)
	}
	s3EvidencePath := ".claude/evidence/E-S3-001.json"
	writeContractPacketSubjectFile(t, root, s3EvidencePath, string(s3EvidenceBytes))
	s3AnswerHash, err := workpacket.ContractAnswerDigest(s3Answer)
	if err != nil {
		t.Fatal(err)
	}
	s3Key := workpacket.Digest([]byte("submission:" + s3Packet.ID))
	s3PacketRow := jsonObject(t, s3Packet)
	s3SubmissionRow := map[string]any{
		"submission_key": s3Key, "packet_id": s3Packet.ID, "owner": "Main", "content_sha256": s3AnswerHash,
		"evidence_id": "E-S3-001", "followup_event_id": workpacket.Digest([]byte(s3Key + ":after-domain")),
		"control_status": "advanced", "origin_state": "planning", "origin_phase": "contracts", "origin_entry": 2,
		"transition_event_id": "evt-PTR-PLAN-02",
	}
	s3EvidenceRow := map[string]any{
		"id": "E-S3-001", "kind": "planning_contract", "runtime_id": s3Binding.RuntimeID, "path": s3EvidencePath,
		"sha256": workpacket.Digest(s3EvidenceBytes), "status": "valid", "baseline_generation": 1,
		"responsibility_id": "Orchestrator", "produced_by": []any{"Main"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "review_round": nil,
	}

	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": s3Binding.RuntimeID,
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"baseline":      map[string]any{"generation": 1},
		"lifecycle":     map[string]any{"state": "planning", "phase": "tasks", "phase_revision": 3},
		"bound_req": map[string]any{
			"id": "REQ-001", "path": reqPath, "version": "v1.0.0", "sha256": workpacket.Digest(req), "status": "locked",
			"workspace": map[string]any{"project_root": root},
		},
		"documents": []any{
			map[string]any{"id": designRef.ID, "kind": "design", "path": designRef.Path, "version": designRef.Version, "sha256": designRef.SHA256, "status": "locked", "generation": 1},
			map[string]any{"id": indexRef.ID, "kind": "contract", "path": indexRef.Path, "version": indexRef.Version, "sha256": indexRef.SHA256, "status": "locked", "generation": 1},
			map[string]any{"id": contractRef.ID, "kind": "contract", "path": contractRef.Path, "version": contractRef.Version, "sha256": contractRef.SHA256, "status": "locked", "generation": 1},
		},
		"entities": map[string]any{
			"work_packets": []any{s3PacketRow}, "submissions": []any{s3SubmissionRow},
		},
		"evidence": []any{s3EvidenceRow},
	}
	inputs, err := runtimepkg.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtimepkg.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	packetRow := jsonObject(t, packet)
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), packetRow)

	taskOnePath := "docs/dev/tasks/TASK-REQ-001-01.md"
	taskTwoPath := "docs/dev/tasks/TASK-REQ-001-02.md"
	indexPath := "docs/dev/tasks/index-REQ-001.md"
	taskOne := "> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n"
	taskTwo := "> Status: cancelled\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n"
	dispatchIndex := "# REQ-001 dispatch\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1 First wave\n\n- [ ] [TASK-REQ-001-01](TASK-REQ-001-01.md)\n"
	writeContractPacketSubjectFile(t, root, taskOnePath, taskOne)
	writeContractPacketSubjectFile(t, root, taskTwoPath, taskTwo)
	writeContractPacketSubjectFile(t, root, indexPath, dispatchIndex)
	answer := workpacket.TaskPlanningAnswer{
		Conclusion: stringPointer("pass"), IndexPath: stringPointer(indexPath), TaskPaths: []string{taskTwoPath, taskOnePath},
	}
	artifacts, err := runtimepkg.TaskPlanningArtifacts(root, state, packet, answer, files)
	if err != nil {
		t.Fatal(err)
	}
	evidenceID := "E-S4-001"
	evidence, err := workpacket.TaskPlanningEvidence(packet, evidenceID, artifacts, answer, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	envelopeBytes, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidencePath := ".claude/evidence/E-S4-001.json"
	writeContractPacketSubjectFile(t, root, evidencePath, string(envelopeBytes))
	answerHash, err := workpacket.TaskPlanningAnswerDigest(answer)
	if err != nil {
		t.Fatal(err)
	}
	key := workpacket.Digest([]byte("submission:" + packet.ID))
	submissionRow := map[string]any{
		"submission_key": key, "packet_id": packet.ID, "owner": "Main", "content_sha256": answerHash,
		"evidence_id": evidenceID, "followup_event_id": workpacket.Digest([]byte(key + ":after-domain")),
		"control_status": "pending", "origin_state": "planning", "origin_phase": "tasks", "origin_entry": packet.Binding.StageEntry,
	}
	evidenceRow := map[string]any{
		"id": evidenceID, "kind": "planning_task", "runtime_id": packet.Binding.RuntimeID, "path": evidencePath,
		"sha256": workpacket.Digest(envelopeBytes), "status": "valid", "baseline_generation": packet.Binding.Generation,
		"responsibility_id": "Orchestrator", "produced_by": []any{"Main"}, "invalidated_by": nil,
	}
	entities["submissions"] = append(entities["submissions"].([]any), submissionRow)
	state["evidence"] = append(state["evidence"].([]any), evidenceRow)

	return &taskPlanningPacketSubjectFixture{
		root: root, input: Input{Root: root, Snapshot: runtimepkg.Snapshot{State: state}, Files: files},
		packet: packet, answer: answer, artifacts: artifacts, envelopeBytes: envelopeBytes, submissionRow: submissionRow,
	}
}

func containsTaskPlanningSource(sources []workpacket.Subject, path string) bool {
	for _, source := range sources {
		if source.Path == path {
			return true
		}
	}
	return false
}

type taskPlanningReadOnlyView struct{ root string }

func (v taskPlanningReadOnlyView) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(v.root, filepath.FromSlash(path)))
}
