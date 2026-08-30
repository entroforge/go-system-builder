// builder_batch_p0_test.go pins the L3-S6 P0-1 semantics of
// GATE-BUILDER-BATCH-READY: the completeness check evaluates the TR-003
// exact execution batch (registered task documents), consumes the
// completion content (checks / scope deviations), and verifies a durable
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
		"docs/tasks/TASK-TEST-01.md": taskOne,
		"docs/tasks/TASK-TEST-02.md": taskTwo,
	}
	documents := []any{
		map[string]any{
			"id": "TASK-TEST-01", "kind": "task", "path": "docs/tasks/TASK-TEST-01.md",
			"version": "v1", "sha256": sha256Hex(taskOne), "status": "complete", "generation": 1,
		},
		map[string]any{
			"id": "TASK-TEST-02", "kind": "task", "path": "docs/tasks/TASK-TEST-02.md",
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
			map[string]any{"path": "docs/tasks/" + taskID + ".md", "version": "v1", "sha256": sha256Hex(b.files["docs/tasks/"+taskID+".md"])},
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

// TestBuilderBatchReviewedEntityTaskCannotSlipCompleteness closes the
// "reviewed TASK 可漏计" hole: a batch TASK whose entity row sits in
// `reviewed` (the state register-workgroup creates) is inside the TR-003
// registered batch and therefore must be proven like any other — the old
// entities.tasks scan skipped it entirely.
func TestBuilderBatchReviewedEntityTaskCannotSlipCompleteness(t *testing.T) {
	b := newBatchP0Input(t)
	b.addCompletion(t, "TASK-TEST-01", nil)
	b.addCheckpoint(t, "TASK-TEST-01", "verified")
	// TASK-TEST-02: no completion, no checkpoint, entity state `reviewed`.

	result := b.evaluate(t)
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("status = %q, want not_ready (reviewed-state batch task must not slip)", result.Status)
	}
	if !contains(result.Missing, "evidence:completion_report:TASK-TEST-02") {
		t.Fatalf("missing = %#v, want completion_report:TASK-TEST-02", result.Missing)
	}
	if !contains(result.Missing, "integration_checkpoint:TASK-TEST-02") {
		t.Fatalf("missing = %#v, want integration_checkpoint:TASK-TEST-02", result.Missing)
	}
}

// TestBuilderBatchSatisfiedWithoutTeamManifestEvidence proves the S7
// prematurity fix end-to-end at the gate: the exact batch complete +
// integrated satisfies the gate with no team_manifest evidence at all.
func TestBuilderBatchSatisfiedWithoutTeamManifestEvidence(t *testing.T) {
	b := newBatchP0Input(t)
	b.addCompletion(t, "TASK-TEST-01", nil)
	b.addCompletion(t, "TASK-TEST-02", nil)
	b.addCheckpoint(t, "TASK-TEST-01", "verified")
	b.addCheckpoint(t, "TASK-TEST-02", "complete")

	result := b.evaluate(t)
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("status = %q, want satisfied (missing=%v)", result.Status, result.Missing)
	}
}

// TestBuilderBatchConsumesCompletionContent: a completion envelope with a
// failing check or an unapproved scope deviation no longer counts as done
// even though the envelope exists and is qualified.
func TestBuilderBatchConsumesCompletionContent(t *testing.T) {
	b := newBatchP0Input(t)
	b.addCompletion(t, "TASK-TEST-01", func(envelope map[string]any) {
		envelope["checks"] = []any{
			map[string]any{"name": "unit", "command": "go test ./...", "result": "fail"},
			map[string]any{"name": "lint", "command": "go vet ./...", "result": "pass"},
		}
	})
	b.addCompletion(t, "TASK-TEST-02", func(envelope map[string]any) {
		envelope["scope_deviations"] = []any{"internal/unrelated/extra.go"}
	})
	b.addCheckpoint(t, "TASK-TEST-01", "verified")
	b.addCheckpoint(t, "TASK-TEST-02", "verified")

	result := b.evaluate(t)
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("status = %q, want not_ready", result.Status)
	}
	if !contains(result.Missing, "checks:TASK-TEST-01:unit=fail") {
		t.Fatalf("missing = %#v, want checks:TASK-TEST-01:unit=fail", result.Missing)
	}
	if !contains(result.Missing, "scope_deviations:TASK-TEST-02:internal/unrelated/extra.go") {
		t.Fatalf("missing = %#v, want scope_deviations entry for TASK-TEST-02", result.Missing)
	}
}

// TestBuilderBatchUnverifiedCheckpointBlocks: a merged-but-not-verified
// checkpoint (or a preserved one) must not satisfy the batch.
func TestBuilderBatchUnverifiedCheckpointBlocks(t *testing.T) {
	b := newBatchP0Input(t)
	b.addCompletion(t, "TASK-TEST-01", nil)
	b.addCompletion(t, "TASK-TEST-02", nil)
	b.addCheckpoint(t, "TASK-TEST-01", "verified")
	b.addCheckpoint(t, "TASK-TEST-02", "merged")

	result := b.evaluate(t)
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("status = %q, want not_ready", result.Status)
	}
	if !contains(result.Missing, "integration_checkpoint:TASK-TEST-02") {
		t.Fatalf("missing = %#v, want integration_checkpoint:TASK-TEST-02", result.Missing)
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

// TestBuilderBatchNewerEnvelopeSupersedes pins the legend's claim "the
// newer envelope supersedes the older one": a TASK whose first result
// recorded a failing check goes green after a corrected resubmission
// (evidence id -r2, appended later in the evidence array) — without
// removing the failed history.
func TestBuilderBatchNewerEnvelopeSupersedes(t *testing.T) {
	b := newBatchP0Input(t)
	// Failing first submission for TASK-TEST-01.
	b.addCompletion(t, "TASK-TEST-01", func(envelope map[string]any) {
		envelope["checks"] = []any{
			map[string]any{"name": "unit", "command": "go test ./...", "result": "fail"},
		}
	})
	// Corrected resubmission: same evidence base id + "-r2", registered
	// later in the array (appendCompletionEvidence order).
	b.addCompletion(t, "TASK-TEST-01", func(envelope map[string]any) {
		envelope["evidence_id"] = "ev-completion-TASK-TEST-01-r2"
		envelope["checks"] = []any{
			map[string]any{"name": "unit", "command": "go test ./...", "result": "pass"},
		}
	})
	b.addCompletion(t, "TASK-TEST-02", nil)
	b.addCheckpoint(t, "TASK-TEST-01", "verified")
	b.addCheckpoint(t, "TASK-TEST-02", "verified")

	result := b.evaluate(t)
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("status = %q, want satisfied after corrected resubmission (missing=%v)", result.Status, result.Missing)
	}
	if !contains(result.EvidenceRefs, "ev-completion-TASK-TEST-01-r2") {
		t.Fatalf("evidence refs = %v, want the superseding envelope qualified", result.EvidenceRefs)
	}
}
