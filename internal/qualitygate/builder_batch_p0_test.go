// builder_batch_p0_test.go pins the L3-S6 P0-1 semantics of
// GATE-BUILDER-BATCH-READY: the completeness check evaluates the TR-003
// exact execution batch (registered task documents), consumes the
// completion content (checks), and verifies a durable
// integration checkpoint per TASK.
package qualitygate_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// batchP0Input builds a building-cursor gate input over a two-task
// registered batch. Every knob mirrors a real production fact:
// documents[] registered by TR-003, completion envelopes, and durable
// worktree checkpoints under .claude/evidence/<runtime>/g<gen>/worktree/.
type batchP0 struct {
	input qualitygate.Input
	files listingFiles
}

func newBatchP0Input(t *testing.T) batchP0 {
	t.Helper()
	taskOne := []byte("# TASK 1\n")
	taskTwo := []byte("# TASK 2\n")
	files := listingFiles{
		"docs/dev/tasks/TASK-TEST-01.md": taskOne,
		"docs/dev/tasks/TASK-TEST-02.md": taskTwo,
	}
	documents := []any{
		map[string]any{
			"id": "TASK-TEST-01", "kind": "task", "path": "docs/dev/tasks/TASK-TEST-01.md",
			"version": "v1", "sha256": sha256Hex(taskOne), "status": "complete", "generation": 1,
		},
		map[string]any{
			"id": "TASK-TEST-02", "kind": "task", "path": "docs/dev/tasks/TASK-TEST-02.md",
			"version": "v1", "sha256": sha256Hex(taskTwo), "status": "complete", "generation": 1,
		},
	}
	state := map[string]any{
		"runtime_id": "loop-test",
		"lifecycle":  map[string]any{"state": "building", "phase": nil},
		"baseline":   map[string]any{"generation": 1},
		"review":     map[string]any{"round": 0},
		"documents":  documents,
		"evidence":   []any{},
		"entities": map[string]any{
			// TASK-TEST-02 sits in `reviewed` — the entity state the
			// workgroup register path creates. The old scan skipped it.
			"tasks": []any{
				map[string]any{"id": "TASK-TEST-01", "state": "review"},
				map[string]any{"id": "TASK-TEST-02", "state": "reviewed"},
			},
		},
	}
	return batchP0{
		input: qualitygate.Input{
			Snapshot:     runtime.Snapshot{Revision: 7, State: state},
			GateID:       "GATE-BUILDER-BATCH-READY",
			TransitionID: "TR-006",
			Files:        files,
		},
		files: files,
	}
}

func (b *batchP0) addCompletion(t *testing.T, taskID string, mutate func(map[string]any)) {
	t.Helper()
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-completion-" + taskID,
		"kind":                    "completion_report",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "builder-1",
		"producer_responsibility": "BUILD-WORK-PACKAGE",
		"subject_refs": []any{
			map[string]any{"path": "docs/dev/tasks/" + taskID + ".md", "version": "v1", "sha256": sha256Hex(b.files["docs/dev/tasks/"+taskID+".md"])},
		},
		"conclusion": "completed",
		"task_id":    taskID,
	}
	if mutate != nil {
		mutate(envelope)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	path := "evidence/" + envelope["evidence_id"].(string) + ".json"
	b.files[path] = data
	items, _ := b.input.Snapshot.State["evidence"].([]any)
	b.input.Snapshot.State["evidence"] = append(items, map[string]any{
		"id": envelope["evidence_id"], "kind": "completion_report", "path": path,
		"sha256": sha256Hex(data), "status": "valid", "baseline_generation": 1, "review_round": nil,
		"produced_by": []any{"builder-1"}, "invalidated_by": nil,
		"responsibility_id": "BUILD-WORK-PACKAGE", "scope_refs": []any{},
	})
}

func (b *batchP0) addCheckpoint(t *testing.T, taskID, state string) {
	t.Helper()
	checkpoint := map[string]any{
		"assignment_id":       "assignment-" + taskID,
		"task_id":             taskID,
		"source_branch":       "wt/" + taskID,
		"target_branch":       "develop",
		"baseline_generation": 1,
		"state":               state,
		"revision":            1,
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	b.files[".claude/evidence/loop-test/g1/worktree/assignment-"+taskID+"/checkpoint.json"] = data
}

func (b *batchP0) evaluate(t *testing.T) qualitygate.Evaluation {
	t.Helper()
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	result, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), b.input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return result
}

// An old completion/checkpoint pair cannot invent a current Assignment.
// Current accepted Result, unfinished work and Main integration are covered by
// TestCanonicalBuilderBatchProgressReusePreservesExactBatch and the real TR-006 integration tests.
func TestLegacyBuilderBatchCannotReplaceCurrentResponsibilities(t *testing.T) {
	b := newBatchP0Input(t)
	for _, id := range []string{"TASK-TEST-01", "TASK-TEST-02"} {
		b.addCompletion(t, id, nil)
		b.addCheckpoint(t, id, "verified")
	}
	result := b.evaluate(t)
	if result.Status != qualitygate.StatusNotReady || !contains(result.Missing, "builder_responsibility:TASK-TEST-01") || !contains(result.Missing, "builder_responsibility:TASK-TEST-02") {
		t.Fatalf("old delivery invented current original responsibilities: %+v", result)
	}
}

// TestBuilderBatchEmptyRegisteredBatchNotReady: reaching building with an
// empty registered task batch is a lost-registry anomaly, not a vacuous
// pass.
func TestBuilderBatchEmptyRegisteredBatchNotReady(t *testing.T) {
	b := newBatchP0Input(t)
	b.input.Snapshot.State["documents"] = []any{}

	result := b.evaluate(t)
	// The base completion_report requirement already fails; the empty-batch
	// token rides along so the missing matrix names the registry loss.
	found := false
	for _, missing := range result.Missing {
		if missing == "batch:execution_batch_empty" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing = %#v, want batch:execution_batch_empty", result.Missing)
	}
}
