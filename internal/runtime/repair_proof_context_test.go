package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

func TestS9PlanSourceProofsReuseJournalParsingAndHonorCancellation(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	ctx, timings := metrics.WithCallTimings(controlproof.WithJournalReadCache(t.Context()))
	for i := 0; i < 2; i++ {
		if err := VerifyRepairPlanReportPacketIssueContext(ctx, f.root, f.state, f.packet); err != nil {
			t.Fatal(err)
		}
	}
	issuedAfter, err := integerField(f.issueEvent, "after_revision")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairPlanReportSources(f.root, f.state, f.packet, issuedAfter, ctx); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairPlanReportCurrent(f.root, f.state, f.packet, ctx); err != nil {
		t.Fatal(err)
	}
	spans := timings.Snapshot()
	if spans["runtime/journal_decode_build"].Count != 1 || spans["runtime/journal_decode_hit"].Count < 3 {
		t.Fatalf("same S9 proof journal was reparsed: %+v", spans)
	}
	// Reused parsing never grants a source verdict: a changed actual Plan still
	// fails the original immutable source check under the same request/cache.
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.PlanRef)), []byte(`{"changed":"after journal cache warmup"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairPlanReportPacketIssueContext(ctx, f.root, f.state, f.packet); err == nil {
		t.Fatal("journal cache hid mutable source drift")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := VerifyRepairPlanReportPacketIssueContext(canceled, f.root, f.state, f.packet); !errors.Is(err, context.Canceled) {
		t.Fatalf("issue proof lost cancellation: %v", err)
	}
	if err := VerifyRepairPlanReportCurrent(f.root, f.state, f.packet, canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("current proof lost cancellation: %v", err)
	}
	if _, _, err := VerifiedRepairPlanReportReceipt(f.root, f.state, f.packet.ID, f.packet.Binding.OwnerAgentID, canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("receipt proof lost cancellation: %v", err)
	}
}

func TestS9ResultAndResumeProofsReuseRequestJournalParsing(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	ctx, timings := metrics.WithCallTimings(controlproof.WithJournalReadCache(t.Context()))
	if err := VerifyRepairResultPacketIssueContext(ctx, f.plan.root, f.plan.state, f.packet); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairResultPacketIssueAndCurrentContext(ctx, f.plan.root, f.plan.state, f.packet); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRepairResultPacketCurrentContext(ctx, f.plan.root, f.plan.state, f.packet); err != nil {
		t.Fatal(err)
	}
	if _, err := repairResultResumeJournal(f.plan.root, f.plan.state, ctx); err != nil {
		t.Fatal(err)
	}
	spans := timings.Snapshot()
	if spans["runtime/journal_decode_build"].Count != 1 || spans["runtime/journal_decode_hit"].Count < 3 {
		t.Fatalf("S9 Result/Resume proof journal was reparsed: %+v", spans)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := VerifyRepairResultPacketIssueContext(canceled, f.plan.root, f.plan.state, f.packet); !errors.Is(err, context.Canceled) {
		t.Fatalf("Result issue proof lost cancellation: %v", err)
	}
	if _, err := repairResultResumeJournal(f.plan.root, f.plan.state, canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume proof lost cancellation: %v", err)
	}
}
