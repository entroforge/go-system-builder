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
	if err := metrics.RecordMilestoneRefreshFailure(root, "stale_revision"); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.MilestoneRefreshFailures != 1 {
		t.Fatalf("milestone refresh failures=%d want 1", snap.MilestoneRefreshFailures)
	}
	if snap.MilestoneRefreshFailureReasons["stale_revision"] != 1 {
		t.Fatalf("stale_revision failures=%d want 1", snap.MilestoneRefreshFailureReasons["stale_revision"])
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
	_ = metrics.RecordMilestoneRefreshFailure(root, "write_or_integrity")
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

func TestFormatHealthDistinguishesHistoricalRuntimeSignals(t *testing.T) {
	root := t.TempDir()
	if out, err := metrics.FormatHealth(root); err != nil || !strings.Contains(out, "runtime health: healthy") {
		t.Fatalf("empty metrics should be healthy, out=%q err=%v", out, err)
	}
	if err := metrics.RecordCASConflict(root); err != nil {
		t.Fatal(err)
	}
	if err := metrics.RecordGateEvaluation(root, "unknown"); err != nil {
		t.Fatal(err)
	}
	out, err := metrics.FormatHealth(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"runtime health: degraded",
		"historical runtime signals",
		"loop_cas_conflicts_total=1",
		"loop_gate_evaluations_total{status=\"unknown\"} 1",
		"next: inspect runtime state/journal with `loop-harness runtime reconcile`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("health output missing %q:\n%s", want, out)
		}
	}
}

func TestProcessCountersMirrorLegacySemantics(t *testing.T) {
	root := t.TempDir()
	gateBefore := metrics.ProcessGateEvaluations()
	transitionBefore := metrics.ProcessTransitionCommits()
	casBefore := metrics.ProcessCASConflicts()
	metrics.RecordGateEvaluationProcess(root, "satisfied")
	metrics.RecordTransitionCommitProcess(root, "T-1")
	metrics.RecordCASConflictProcess(root)
	if got := metrics.ProcessGateEvaluations() - gateBefore; got != 1 {
		t.Fatalf("process gate delta=%d want 1", got)
	}
	if got := metrics.ProcessTransitionCommits() - transitionBefore; got != 1 {
		t.Fatalf("process transition delta=%d want 1", got)
	}
	if got := metrics.ProcessCASConflicts() - casBefore; got != 1 {
		t.Fatalf("process cas delta=%d want 1", got)
	}
}

// TestPruneDeadFamiliesRemovesLegacyRoundLabels exercises the RC-17 family
// hygiene pass: a round-keyed S7 map carrying a label the round vocabulary
// cannot parse (legacy or corrupt writer) must not survive another mutation,
// because retainS7Rounds skips it when computing retention and FormatS7 can
// never render it — it would be dead weight in every future snapshot.
func TestPruneDeadFamiliesRemovesLegacyRoundLabels(t *testing.T) {
	root := t.TempDir()
	// Record a live round so the store exists with wired data.
	if err := metrics.RecordS7RoundShape(root, 2, 3, 4, 5); err != nil {
		t.Fatal(err)
	}
	// Corrupt the on-disk snapshot with a legacy unparseable round label.
	path := filepath.Join(root, metrics.DefaultRelativePath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Add a legacy unparseable round label alongside the live one (do not
	// remove the live entry — the prune must delete only the dead label).
	patched := strings.Replace(string(raw), `"2": 5`, `"2": 5,
		"round-legacy": 9`, 1)
	if patched == string(raw) {
		t.Fatalf("expected to patch the S7PlanRevision entry in:\n%s", raw)
	}
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	// Any later mutation must prune the dead label.
	if err := metrics.RecordS7RoundShape(root, 3, 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, legacy := snap.S7PlanRevision["round-legacy"]; legacy {
		t.Fatalf("legacy round label survived prune: %v", snap.S7PlanRevision)
	}
	if snap.S7PlanRevision["2"] != 5 {
		t.Fatalf("live round 2 entry lost in prune: %v", snap.S7PlanRevision)
	}
	if snap.S7PlanRevision["3"] != 1 {
		t.Fatalf("live round 3 entry lost in prune: %v", snap.S7PlanRevision)
	}
}

// TestTrackedFamilyBudget pins the RC-17 family budget: the durable snapshot
// must track at most maxTrackedFamilies loop_* families. Growing the set
// requires retiring a dead family in the same change (or extending the
// maxTrackedFamilies comment with the new producer/consumer pair).
func TestTrackedFamilyBudget(t *testing.T) {
	// The field names of the persisted Snapshot are observable through the
	// on-disk JSON; count loop_* keys in a snapshot that observed one round.
	root := t.TempDir()
	_ = metrics.RecordGateEvaluation(root, "satisfied")
	_ = metrics.RecordS7RoundShape(root, 1, 1, 1, 1)
	raw, err := os.ReadFile(filepath.Join(root, metrics.DefaultRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	families := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"loop_`) {
			families++
		}
	}
	if families > 16 {
		t.Fatalf("metrics snapshot tracks %d loop_* families, budget is 16:\n%s", families, raw)
	}
}
