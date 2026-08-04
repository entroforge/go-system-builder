package metrics_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

func TestRecordGateEvaluationPersistsLabeledCounter(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordGateEvaluation(root, "not_ready"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordGateEvaluation(root, "not_ready"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordGateEvaluation(root, "satisfied"); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.GateEvaluations["not_ready"] != 2 {
		t.Fatalf("not_ready=%d want 2", snap.GateEvaluations["not_ready"])
	}
	if snap.GateEvaluations["satisfied"] != 1 {
		t.Fatalf("satisfied=%d want 1", snap.GateEvaluations["satisfied"])
	}
}

func TestRecordTransitionCommitPersistsTransitionLabel(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordTransitionCommit(root, "PLANNING-DESIGN-CONTRACTS"); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.TransitionCommits["PLANNING-DESIGN-CONTRACTS"] != 1 {
		t.Fatalf("transition count=%d want 1", snap.TransitionCommits["PLANNING-DESIGN-CONTRACTS"])
	}
}

func TestRecordCASConflictIncrementsTotal(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordCASConflict(root); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.CASConflicts != 1 {
		t.Fatalf("cas conflicts=%d want 1", snap.CASConflicts)
	}
}

func TestRecordMilestoneRefreshFailureIncrementsTotal(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordMilestoneRefreshFailure(root); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.MilestoneRefreshFailures != 1 {
		t.Fatalf("milestone refresh failures=%d want 1", snap.MilestoneRefreshFailures)
	}
}

func TestRecordRecoveryPacketIncrementsTotal(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordRecoveryPacket(root); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.RecoveryPackets != 1 {
		t.Fatalf("recovery packets=%d want 1", snap.RecoveryPackets)
	}
}

func TestRecordIntegrationDurationAggregatesByStatus(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordIntegrationDuration(root, "success", 120); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordIntegrationDuration(root, "success", 80); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordIntegrationDuration(root, "preserved", 50); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	success := snap.IntegrationDuration["success"]
	if success.Count != 2 || success.SumMS != 200 {
		t.Fatalf("success stats=%+v want count=2 sum_ms=200", success)
	}
	preserved := snap.IntegrationDuration["preserved"]
	if preserved.Count != 1 || preserved.SumMS != 50 {
		t.Fatalf("preserved stats=%+v want count=1 sum_ms=50", preserved)
	}
}

func TestMetricsAccumulateAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	if err := metrics.RecordGateEvaluation(root, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordGateEvaluation(root, "unknown"); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.GateEvaluations["unknown"] != 2 {
		t.Fatalf("cross-write count=%d want 2", snap.GateEvaluations["unknown"])
	}
	if _, err := os.Stat(filepath.Join(root, metrics.DefaultRelativePath)); err != nil {
		t.Fatalf("metrics file missing: %v", err)
	}
}

func TestConcurrentRecordGateEvaluationRaceSafe(t *testing.T) {
	root := t.TempDir()
	const workers = 32
	const perWorker = 25
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				if err := metrics.RecordGateEvaluation(root, "satisfied"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	want := int64(workers * perWorker)
	if snap.GateEvaluations["satisfied"] != want {
		t.Fatalf("concurrent count=%d want %d", snap.GateEvaluations["satisfied"], want)
	}
}

func TestFormatDoctorIncludesAllMetricFamilies(t *testing.T) {
	root := t.TempDir()
	_ = metrics.RecordGateEvaluation(root, "advanced")
	_ = metrics.RecordTransitionCommit(root, "T-1")
	_ = metrics.RecordCASConflict(root)
	_ = metrics.RecordMilestoneRefreshFailure(root)
	_ = metrics.RecordRecoveryPacket(root)
	_ = metrics.RecordIntegrationDuration(root, "success", 10)

	out, err := metrics.FormatDoctor(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"loop_gate_evaluations_total",
		"loop_transition_commits_total",
		"loop_cas_conflicts_total",
		"loop_milestone_refresh_failures_total",
		"loop_recovery_packets_total",
		"loop_integration_duration_ms",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestProcessCountersMirrorLegacySemantics(t *testing.T) {
	root := t.TempDir()
	metrics.RecordGateEvaluationProcess(root, "satisfied")
	metrics.RecordTransitionCommitProcess(root, "T-1")
	metrics.RecordCASConflictProcess(root)
	if metrics.ProcessGateEvaluations() != 1 {
		t.Fatalf("process gate=%d want 1", metrics.ProcessGateEvaluations())
	}
	if metrics.ProcessTransitionCommits() != 1 {
		t.Fatalf("process transition=%d want 1", metrics.ProcessTransitionCommits())
	}
	if metrics.ProcessCASConflicts() != 1 {
		t.Fatalf("process cas=%d want 1", metrics.ProcessCASConflicts())
	}
}
