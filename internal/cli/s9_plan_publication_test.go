package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9PlanOriginalWriterSkipsOccupiedPathAndReusesRecordedResult(t *testing.T) {
	root, worker, packet, _ := nativeS9PlanOnlyFixture(t)
	planPath, _ := nativeS9TestAuthorPaths(t, root, packet)
	answer, err := os.ReadFile(filepath.Join(worker, planPath))
	if err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(root, filepath.FromSlash(repair.ArtifactStoragePath("plan-reports", packet.ReportID)))
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("unrelated file")
	if err := os.WriteFile(occupied, original, 0600); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	request := repair.SubmitIssuedRepairPlanReportRequest{Context: context.Background(), ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID, SourceRoot: worker, AnswerBytes: answer}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = repair.SubmitIssuedRepairPlanReport(root, statePath, journalPath, request)
		}(i)
	}
	wg.Wait()
	snapshot := repairDispatchSnapshot(t, root)
	receipt, found, err := runtime.VerifiedRepairPlanReportReceipt(root, snapshot.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("original Writer did not record Plan: receipt=%+v found=%t err=%v concurrent=%v", receipt, found, err, errs)
	}
	if receipt.ReportRef == "" || filepath.Join(root, filepath.FromSlash(receipt.ReportRef)) == occupied {
		t.Fatalf("occupied path was claimed: %+v", receipt)
	}
	for i := range errs {
		_, report, err := repair.SubmitIssuedRepairPlanReport(root, statePath, journalPath, request)
		if err != nil || report.Receipt == nil || report.Receipt.ReportRef != receipt.ReportRef {
			t.Fatalf("replay %d: %+v %v", i, report.Receipt, err)
		}
	}
	if got, err := os.ReadFile(occupied); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("unknown file changed: %q %v", got, err)
	}
}
