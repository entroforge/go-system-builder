package repair_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func repairOperationFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contract, hash := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contract.Path, "repair_contract_sha256": hash}
	req039fixtures.WriteState(t, root, state)
	sp, jp := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if err := os.WriteFile(jp, nil, 0644); err != nil {
		t.Fatal(err)
	}
	return root, sp, jp
}

func TestRepairOperationsReplayOriginalArtifactsAfterProgress(t *testing.T) {
	root, sp, jp := repairOperationFixture(t)
	sessionRequest := repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{OperationID: "open-1", ExpectedRevision: 0}, SessionID: "repair-session-operation", CreatedBy: "main"}
	sessionSnapshot, _, sessionRef, err := repair.OpenRepairSession(root, sp, jp, sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	planRequest := repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{OperationID: "plan-1", ExpectedRevision: 1}, PlanID: "repair-plan-operation", CreatedBy: "main"}
	planSnapshot, _, planRef, err := repair.CompileRepairPlan(root, sp, jp, planRequest)
	if err != nil {
		t.Fatal(err)
	}
	reportRuntime := repair.RuntimeRequest{OperationID: "report-1", ExpectedRevision: 2}
	reportRequest := repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-operation", PlanText: "restore authority", RedChecks: []repair.RepairCheck{{Name: "before", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}}
	reportSnapshot, _, reportRef, err := repair.SubmitPlanReportDraftToRuntime(root, sp, jp, reportRuntime, reportRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.BeginRepairExecution(root, sp, jp, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}}); err != nil {
		t.Fatal(err)
	}
	product := []byte("package api\n")
	writeFile(t, root, "internal/api/payload.go", string(product))
	resultRequest := repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{OperationID: "result-1", ExpectedRevision: 4}, Result: repair.RepairResultRequest{ResultID: "repair-result-operation", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", SHA256: fileHash(product), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "after", Command: "go test ./internal/api", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}}
	resultSnapshot, _, resultRef, err := repair.SubmitRepairResultToRuntime(root, sp, jp, resultRequest)
	if err != nil {
		t.Fatal(err)
	}
	// Current status and product bytes have moved beyond these operations.
	// Replays report the original commit, without endorsing the new bytes.
	writeFile(t, root, "internal/api/payload.go", "package api\n// later edit\n")
	read := func(p string) []byte {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	beforeState, beforeJournal := read(sp), read(jp)
	check := func(want, got runtime.Snapshot, expectedRef, actualRef repair.ArtifactRef, err error) {
		t.Helper()
		if err != nil || !got.OperationReplayed || got.Revision != 5 || expectedRef != actualRef {
			t.Fatalf("replay: snapshot=%+v ref=%+v err=%v", got, actualRef, err)
		}
		one, _ := json.Marshal(want.Operation)
		two, _ := json.Marshal(got.Operation)
		if want.Operation == nil || !bytes.Equal(one, two) {
			t.Fatalf("receipt changed: %s != %s", one, two)
		}
	}
	s, _, ref, err := repair.OpenRepairSession(root, sp, jp, sessionRequest)
	check(sessionSnapshot, s, sessionRef, ref, err)
	s, _, ref, err = repair.CompileRepairPlan(root, sp, jp, planRequest)
	check(planSnapshot, s, planRef, ref, err)
	s, _, ref, err = repair.SubmitPlanReportDraftToRuntime(root, sp, jp, reportRuntime, reportRequest)
	check(reportSnapshot, s, reportRef, ref, err)
	s, _, ref, err = repair.SubmitRepairResultToRuntime(root, sp, jp, resultRequest)
	check(resultSnapshot, s, resultRef, ref, err)
	resultRequest.Actor = "different-actor"
	if _, _, _, err := repair.SubmitRepairResultToRuntime(root, sp, jp, resultRequest); !errors.Is(err, runtime.ErrOperationConflict) {
		t.Fatalf("changed authority accepted: %v", err)
	}
	reportRequest.PlanText = "different plan"
	if _, _, _, err := repair.SubmitPlanReportDraftToRuntime(root, sp, jp, reportRuntime, reportRequest); !errors.Is(err, runtime.ErrOperationConflict) {
		t.Fatalf("changed logical content accepted: %v", err)
	}
	if !bytes.Equal(beforeState, read(sp)) || !bytes.Equal(beforeJournal, read(jp)) {
		t.Fatal("retries or rejected conflicts changed state/journal")
	}
	if err := os.WriteFile(filepath.Join(root, resultRef.Path), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	resultRequest.Actor = ""
	if _, _, _, err := repair.SubmitRepairResultToRuntime(root, sp, jp, resultRequest); err == nil {
		t.Fatal("missing/drifted committed artifact accepted on replay")
	}
}
