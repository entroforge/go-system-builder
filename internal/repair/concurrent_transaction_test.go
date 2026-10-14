package repair_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestConcurrentRepairSameOperationReturnsOneReceipt(t *testing.T) {
	root, sp, jp := repairOperationFixture(t)
	request := repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{OperationID: "concurrent-open", ExpectedRevision: 0}, SessionID: "repair-session-one-operation", CreatedBy: "main"}
	var group sync.WaitGroup
	snapshots := make([]runtime.Snapshot, 3)
	errorsByIndex := make([]error, 3)
	for i := range snapshots {
		group.Go(func() {
			snapshots[i], _, _, errorsByIndex[i] = repair.OpenRepairSession(root, sp, jp, request)
		})
	}
	group.Wait()
	first, _ := json.Marshal(snapshots[0].Operation)
	for i, snapshot := range snapshots {
		receipt, _ := json.Marshal(snapshot.Operation)
		if errorsByIndex[i] != nil || snapshot.Operation == nil || !bytes.Equal(first, receipt) {
			t.Fatalf("concurrent caller %d did not return the original receipt: %v %s", i, errorsByIndex[i], receipt)
		}
	}
	journal, err := os.ReadFile(jp)
	if err != nil || bytes.Count(journal, []byte("\n")) != 1 {
		t.Fatalf("same operation committed more than once: %v %s", err, journal)
	}
}

func TestConcurrentRepairReportsRejectStaleWithoutPublishingAndRetryWithoutLostRefs(t *testing.T) {
	root, sp, jp := repairOperationFixture(t)
	snapshot, err := runtime.NewStore(sp, jp).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	contract := writeScopedRuntimeContract(t, root)
	casePointer := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)
	casePointer["case_id"], casePointer["repair_contract_ref"], casePointer["repair_contract_sha256"] = "investigation-case-scoped", contract.Path, contract.SHA256
	req039fixtures.WriteState(t, root, snapshot.State)
	_, _, sessionRef, err := repair.OpenRepairSession(root, sp, jp, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0}, SessionID: "repair-session-concurrent", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, plan, planRef, err := repair.CompileRepairPlan(root, sp, jp, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1}, PlanID: "repair-plan-concurrent", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]repair.PlanReportRequest, 2)
	for i, assignment := range plan.Assignments {
		requests[i] = repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: assignment.AssignmentID, AgentID: "builder-" + assignment.AssignmentID, ReportID: "repair-plan-report-" + assignment.AssignmentID, PlanText: "restore assigned authority", RedChecks: []repair.RepairCheck{{Name: "before", Command: "go test ./...", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: assignment.Scope}
	}
	var group sync.WaitGroup
	errorsByIndex := make([]error, 2)
	for i := range requests {
		group.Go(func() {
			_, _, _, errorsByIndex[i] = repair.SubmitPlanReportDraftToRuntime(root, sp, jp, repair.RuntimeRequest{ExpectedRevision: 2, OperationID: requests[i].ReportID}, requests[i])
		})
	}
	group.Wait()
	count := 0
	err = filepath.WalkDir(filepath.Join(root, ".claude/review/repair/plan-reports"), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return err
	})
	if err != nil || count != 1 {
		t.Fatalf("rejected report published an artifact: count=%d err=%v", count, err)
	}
	stale := 0
	for i, err := range errorsByIndex {
		if err == nil {
			continue
		}
		if !errors.Is(err, runtime.ErrStaleRevision) {
			t.Fatal(err)
		}
		stale++
		if _, _, _, err := repair.SubmitPlanReportDraftToRuntime(root, sp, jp, repair.RuntimeRequest{ExpectedRevision: -1, OperationID: requests[i].ReportID}, requests[i]); err != nil {
			t.Fatalf("stale retry collided: %v", err)
		}
	}
	final, err := runtime.NewStore(sp, jp).Snapshot()
	if err != nil || stale != 1 || final.Revision != 4 {
		t.Fatalf("expected exactly one stale then one retry: stale=%d revision=%d err=%v", stale, final.Revision, err)
	}
	pointer := final.State["review"].(map[string]any)["repair"].(map[string]any)
	if len(pointer["plan_report_refs"].([]any)) != 2 || len(pointer["assignment_owners"].(map[string]any)) != 2 {
		t.Fatal("concurrent reports lost references or owners")
	}
	staged, _ := filepath.Glob(filepath.Join(root, ".claude/operations/staging/*/*.data"))
	if len(staged) != 0 {
		t.Fatalf("completed/rejected reports leaked private staging: %v", staged)
	}
}

func TestRepairPhysicalIdentityIncludesRuntimeAndSession(t *testing.T) {
	root, _, _ := repairOperationFixture(t)
	contract, _ := writeRuntimeContract(t, root)
	var refs []repair.ArtifactRef
	for _, ids := range [][2]string{{"loop-one", "repair-session-one"}, {"loop-two", "repair-session-one"}, {"loop-one", "repair-session-two"}} {
		_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{Contract: contract, RuntimeID: ids[0], SessionID: ids[1], ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main"})
		if err != nil {
			t.Fatal(err)
		}
		_, ref, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contract, Session: sessionRef, PlanID: "repair-plan-same-name", CreatedBy: "main"})
		if err != nil {
			t.Fatal(err)
		}
		for _, prior := range refs {
			if prior.Path == ref.Path {
				t.Fatal("cross-Runtime/Session collision")
			}
		}
		refs = append(refs, ref)
	}
	for _, ref := range refs {
		if _, err := os.Stat(filepath.Join(root, ref.Path)); err != nil {
			t.Fatalf("overwrote prior scoped artifact: %v", err)
		}
	}
}
