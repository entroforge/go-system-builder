package dispatch

import (
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"testing"
)

func TestConfiguredPressureHysteresisAndGeneration(t *testing.T) {
	c := runtime.DispatchBackpressureConfig{MaxUnreceivedResults: 3, ResumeUnreceivedResults: 1}
	p := pressureFromCount("loop", 1, c, 3, nil)
	if !p.Paused {
		t.Fatal("high threshold failed")
	}
	old := map[string]any{"runtime_id": "loop", "generation": 1, "paused": p.Paused}
	if !pressureFromCount("loop", 1, c, 2, old).Paused {
		t.Fatal("middle count prematurely resumed")
	}
	if pressureFromCount("loop", 1, c, 1, old).Paused {
		t.Fatal("original receive did not resume")
	}
	if pressureFromCount("loop", 2, c, 2, old).Paused || pressureFromCount("other", 1, c, 2, old).Paused {
		t.Fatal("old binding imposed hysteresis")
	}
}

func TestConfiguredPressureCountsAssignmentsAndFreshAcknowledgement(t *testing.T) {
	rows := []integration.PendingIntegration{{AssignmentID: "first", TaskID: "same", State: "reported"}, {AssignmentID: "second", TaskID: "same", State: "verified"}, {AssignmentID: "third", State: "preserved"}, {AssignmentID: "cleanup", State: "cleanup_pending", Acknowledged: true, CurrentReport: true}, {AssignmentID: "stale", State: "complete", Acknowledged: true, CurrentReport: false}, {AssignmentID: "reviewer", State: "reported"}}
	builders := map[string]bool{"first": true, "second": true, "third": true, "cleanup": true, "stale": true}
	if got := countUnreceived(rows, builders); got != 4 {
		t.Fatalf("counted TASK/Agent or stale/cleanup incorrectly: %d", got)
	}
	rows[1].Acknowledged, rows[1].CurrentReport = true, true
	if got := countUnreceived(rows, builders); got != 3 {
		t.Fatal("verified checkpoint with original current ACK still waits for cleanup", got)
	}
}

func TestConfiguredPressureBlocksRecommendationsWithoutInventingCapacity(t *testing.T) {
	b := receptionBoard()
	b.Pressure = &Pressure{Paused: true, Unreceived: 2, Max: 3, Resume: 1}
	capacity := 4
	a := AdviseReception(b, nil, &capacity, nil)
	if !a.Backpressure || *a.DispatchBudget != 0 || len(a.RecommendedTasks) != 0 {
		t.Fatal(a)
	}
	a = AdviseReception(b, nil, nil, nil)
	if a.DispatchBudget != nil || len(a.RecommendedTasks) != 0 {
		t.Fatal("unknown capacity invented slots", a)
	}
}

func TestConfiguredPressureUnresolvedOriginalBuilderIsNotZero(t *testing.T) {
	state := map[string]any{"schema_version": "1.2.0", "runtime_id": "loop", "revision": 1, "baseline": map[string]any{"generation": 1}, "lifecycle": map[string]any{"state": "building"}, "configuration": map[string]any{"optimization": map[string]any{"packet": false, "dispatch_backpressure": map[string]any{"max_unreceived_results": 3, "resume_unreceived_results": 1}}}, "entities": map[string]any{"agents": []any{map[string]any{"id": "builder", "role": "backend-builder", "state": "reported", "task_ids": []any{"TASK-042-01"}}}, "tasks": []any{map[string]any{"id": "TASK-042-01", "state": "review", "owner_agent_ids": []any{"builder"}}}}}
	if p, err := ProjectPressure(t.TempDir(), state); err == nil || p != nil {
		t.Fatal("unresolved source was interpreted as zero pending results", p, err)
	}
}
