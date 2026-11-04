package dispatch

import (
	"bytes"
	"encoding/json"
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

// pressureState builds a Runtime state whose dispatch backpressure is
// configured and whose dispatch is not paused: no unreceived original result
// is registered, so the pressure counts zero unreceived results.
func pressureState() map[string]any {
	return map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     "loop-REQ-002",
		"revision":       float64(1),
		"baseline":       map[string]any{"generation": float64(1)},
		"lifecycle":      map[string]any{"state": "building"},
		"configuration": map[string]any{"optimization": map[string]any{
			"packet":                false,
			"dispatch_backpressure": map[string]any{"max_unreceived_results": float64(3), "resume_unreceived_results": float64(1)},
		}},
		"entities": map[string]any{"agents": []any{}, "tasks": []any{}},
	}
}

// The read-only refusal judges the dispatch without touching the Runtime
// state. The store hands the locked state to the registration's Precheck, so
// a judgment that writes would silently alter the committed state.
func TestConfiguredBackpressureRefusalDoesNotWriteTheRuntimeState(t *testing.T) {
	root := t.TempDir()
	state := pressureState()
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfiguredBackpressureRefusal(root, state); err != nil {
		t.Fatalf("read-only backpressure refusal: %v", err)
	}
	after, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the read-only backpressure judgment wrote the Runtime state:\nbefore %s\nafter  %s", before, after)
	}
	if _, exists := state["dispatch_pressure"]; exists {
		t.Fatal("the read-only backpressure judgment recorded the dispatch pressure projection")
	}
}

// The existing exported judgment keeps its behaviour: it refuses a paused
// dispatch and records the dispatch pressure projection when the dispatch is
// not paused.
func TestCheckConfiguredBackpressureRecordsTheDispatchPressureProjection(t *testing.T) {
	root := t.TempDir()
	state := pressureState()
	if err := CheckConfiguredBackpressure(root, state); err != nil {
		t.Fatalf("configured backpressure check: %v", err)
	}
	projection, ok := state["dispatch_pressure"].(map[string]any)
	if !ok {
		t.Fatalf("the configured backpressure check did not record the dispatch pressure projection: %v", state["dispatch_pressure"])
	}
	if projection["runtime_id"] != "loop-REQ-002" || projection["generation"] != 1 || projection["paused"] != false {
		t.Fatalf("dispatch pressure projection = %v, want the runtime id, the generation and the unpaused dispatch", projection)
	}
}

// A paused dispatch is refused with the dispatch's own message, and the
// refusal is the same one the read-only judgment and the existing exported
// judgment answer with.
//
// A paused dispatch needs an unreceived original Builder result, and an
// unreceived result is only projected from a canonical Assignment Record, so
// the pressure itself is pinned here and the two exported judgments are
// pinned through the projection they share.
func TestConfiguredBackpressureRefusalRefusesThePausedDispatch(t *testing.T) {
	err := configuredBackpressureRefusal(&Pressure{RuntimeID: "loop-REQ-002", Generation: 1, Paused: true, Unreceived: 2, Max: 3, Resume: 1})
	if err == nil {
		t.Fatal("a paused dispatch must be refused")
	}
	want := "S6 Builder registration queued by explicit dispatch backpressure: 2 unreceived; receive original results until <= 1 (trigger 3)"
	if err.Error() != want {
		t.Fatalf("paused dispatch refusal = %q, want %q", err.Error(), want)
	}
	if err := configuredBackpressureRefusal(&Pressure{RuntimeID: "loop-REQ-002", Generation: 1, Paused: false, Unreceived: 0, Max: 3, Resume: 1}); err != nil {
		t.Fatalf("an unpaused dispatch must not be refused: %v", err)
	}
}
