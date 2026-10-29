package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookTransportFailureDegradesHealthBelowPlatformThreshold(t *testing.T) {
	root := t.TempDir()
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		if err := ObserveHookTransportFailure(root, event, "deadline", 150, 150); err != nil {
			t.Fatal(err)
		}
	}
	if degraded, err := HealthDegraded(root); err != nil || !degraded {
		t.Fatalf("degraded=%v err=%v", degraded, err)
	}
	for i := 0; i < 2; i++ {
		if err := CompactObservations(root); err != nil {
			t.Fatal(err)
		}
		snapshot, err := NewStore(root).Read()
		if err != nil || len(snapshot.HookTransportFailures) != 3 {
			t.Fatalf("failure observations lost: %+v err=%v", snapshot.HookTransportFailures, err)
		}
		for _, value := range snapshot.HookTransportFailures {
			if value.Count != 1 || value.SumMS != 150 || value.ProcessBudgetMinMS == nil || *value.ProcessBudgetMinMS != 150 || value.ProcessBudgetMaxMS == nil || *value.ProcessBudgetMaxMS != 150 {
				t.Fatalf("compaction changed actual outer facts: %+v", value)
			}
		}
	}
	out, err := FormatHealth(root)
	if err != nil || !strings.Contains(out, "process_budget_min_ms=150 process_budget_max_ms=150") || !strings.Contains(out, "reception and successor status unknown") || strings.Contains(out, "loop_hook_evaluation_duration_ms{event=") {
		t.Fatalf("outer process fabricated child completion or lost budget: %s err=%v", out, err)
	}
}

func TestHookTransportFailureRejectsUnknownOutcome(t *testing.T) {
	if err := ObserveHookTransportFailure(t.TempDir(), "Stop", "accepted", 1, 8_000); err == nil {
		t.Fatal("transport observation claimed professional acceptance")
	}
}

func TestHookTransportBudgetsAreStatisticsNotSeriesDimensions(t *testing.T) {
	root := t.TempDir()
	for i, budget := range []int64{0, 1, 8000, 28000} {
		if err := ObserveHookTransportFailure(root, "SubagentStop", "deadline", int64(10+i), budget); err != nil {
			t.Fatal(err)
		}
	}
	assert := func(count, sum, maximum int64) {
		t.Helper()
		snap, err := NewStore(root).Read()
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.HookTransportFailures) != 1 {
			t.Fatalf("process budgets created new series: %+v", snap.HookTransportFailures)
		}
		for raw, stats := range snap.HookTransportFailures {
			var label map[string]any
			if err := json.Unmarshal([]byte(raw), &label); err != nil {
				t.Fatal(err)
			}
			if len(label) != 2 || label["event"] != "SubagentStop" || label["outcome"] != "deadline" {
				t.Fatalf("noncategorical series label: %s", raw)
			}
			if stats.Count != count || stats.SumMS != sum || stats.ProcessBudgetMinMS == nil || *stats.ProcessBudgetMinMS != 0 || stats.ProcessBudgetMaxMS == nil || *stats.ProcessBudgetMaxMS != maximum {
				t.Fatalf("lost actual elapsed/budget observations: %+v", stats)
			}
		}
	}
	assert(4, 46, 28000)
	if err := CompactObservations(root); err != nil {
		t.Fatal(err)
	}
	assert(4, 46, 28000)
	if err := ObserveHookTransportFailure(root, "SubagentStop", "deadline", 7, 200); err != nil {
		t.Fatal(err)
	}
	if err := ObserveHookTransportFailure(root, "SubagentStop", "deadline", 8, 30000); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := CompactObservations(root); err != nil {
			t.Fatal(err)
		}
		assert(6, 61, 30000)
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude/hook-metrics/summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary observationSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Totals) != 1 {
		t.Fatalf("compaction retained per-budget totals: %s", data)
	}
	for _, total := range summary.Totals {
		if total.Count != 6 || total.Observation.ProcessBudgetMinMS == nil || *total.Observation.ProcessBudgetMinMS != 0 || total.Observation.ProcessBudgetMaxMS == nil || *total.Observation.ProcessBudgetMaxMS != 30000 {
			t.Fatalf("summary lost range/count: %+v", total)
		}
	}
}

func TestHookTransportUnknownEventsShareBoundedBucket(t *testing.T) {
	root := t.TempDir()
	for _, event := range []string{"arbitrary-event-1", "arbitrary-event-2", ""} {
		if err := ObserveHookTransportFailure(root, event, "worker", 1, 8000); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := NewStore(root).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.HookTransportFailures) != 1 {
		t.Fatalf("unknown event strings created unbounded labels: %+v", snap.HookTransportFailures)
	}
	for raw, value := range snap.HookTransportFailures {
		label, err := parseHookTransportLabel(raw)
		if err != nil || label.Event != "unknown" || value.Count != 3 {
			t.Fatalf("unknown event facts lost: %s %+v %v", raw, value, err)
		}
	}
}

func TestHookTransportCompactionRecoversBudgetRangeAfterCommittedSummary(t *testing.T) {
	root := t.TempDir()
	if err := ObserveHookTransportFailure(root, "Stop", "deadline", 10, 100); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude/hook-metrics")
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("original spool: %v %v", files, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var original observation
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	key, _ := json.Marshal([]string{original.Kind, original.Label})
	summary := observationSummary{Version: 1, Totals: map[string]countedObservation{string(key): {Observation: original, Count: 1}}, Consumed: map[string]bool{files[0].Name(): true}}
	if err := writeObservationSummary(dir, summary); err != nil {
		t.Fatal(err)
	}
	if err := ObserveHookTransportFailure(root, "Stop", "deadline", 20, 200); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		snap, err := NewStore(root).Read()
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.HookTransportFailures) != 1 {
			t.Fatal(snap.HookTransportFailures)
		}
		for _, stats := range snap.HookTransportFailures {
			if stats.Count != 2 || stats.SumMS != 30 || stats.ProcessBudgetMinMS == nil || *stats.ProcessBudgetMinMS != 100 || stats.ProcessBudgetMaxMS == nil || *stats.ProcessBudgetMaxMS != 200 {
				t.Fatalf("interrupted deletion lost/doubled observations: %+v", stats)
			}
		}
		if err := CompactObservations(root); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHookTransportRejectsInvalidBudgetRanges(t *testing.T) {
	label := `{"event":"Stop","outcome":"deadline"}`
	negative, low, high := int64(-1), int64(100), int64(200)
	for _, record := range []observation{
		{Kind: "hook_transport_failure", Label: label, ProcessBudgetMinMS: &low},
		{Kind: "hook_transport_failure", Label: label, ProcessBudgetMinMS: &high, ProcessBudgetMaxMS: &low},
		{Kind: "hook_transport_failure", Label: label, ProcessBudgetMinMS: &negative, ProcessBudgetMaxMS: &low},
	} {
		snap := emptySnapshot()
		if err := applyObservation(&snap, record, 1); err == nil {
			t.Fatal("invalid observed budget range accepted")
		}
	}
	if err := ObserveHookTransportFailure(t.TempDir(), "Stop", "deadline", 1, -1); err == nil {
		t.Fatal("negative actual budget accepted")
	}
}
