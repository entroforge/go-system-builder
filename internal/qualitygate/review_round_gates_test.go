package qualitygate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// memFiles is a minimal FileView for the gate tests.
type memFiles map[string][]byte

func (m memFiles) ReadFile(path string) ([]byte, error) {
	data, ok := m[path]
	if !ok {
		return nil, errMissingFile(path)
	}
	return append([]byte(nil), data...), nil
}

type missingFileError string

func (e missingFileError) Error() string { return "missing file: " + string(e) }

func errMissingFile(path string) error { return missingFileError(path) }

func sha256HexLocal(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// batchEnvelopeFile writes the gate-consumable sealed batch envelope and
// returns (indexEntry, files) so the base evidence requirement qualifies.
func batchEnvelopeFile(id string, round int) (map[string]any, memFiles) {
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             id,
		"kind":                    "observation_batch",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"review_round":            round,
		"producer_agent_id":       "round-consumer",
		"producer_responsibility": "Orchestrator",
		"subject_refs":            []any{},
		"conclusion":              "sealed",
	}
	data, _ := json.Marshal(envelope)
	path := "evidence/" + id + ".json"
	entry := map[string]any{
		"id": id, "kind": "observation_batch", "path": path, "sha256": sha256HexLocal(data),
		"status": "valid", "baseline_generation": 1, "review_round": round,
		"produced_by": []any{"round-consumer"}, "invalidated_by": nil,
		"responsibility_id": "Orchestrator", "scope_refs": []any{},
	}
	return entry, memFiles{path: data}
}

// The S7 exit gates recompute the ReviewPlan projection instead of trusting
// evidence presence (L3-S7 §10, §3.7). These tests pin the exact-set
// semantics: the batch gate names the unsealed/mismatched facts and the
// clean gate recomputes every check.

func s7GateInput(t *testing.T, gateID, transitionID, phase string, review map[string]any, evidence []any) Input {
	t.Helper()
	return Input{
		Snapshot: runtime.Snapshot{
			Revision: 1,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "verification", "phase": phase},
				"baseline":   map[string]any{"generation": 1},
				"review":     review,
				"evidence":   evidence,
				"entities":   map[string]any{"bugs": []any{}, "findings": []any{}},
			},
		},
		GateID:       gateID,
		TransitionID: transitionID,
	}
}

func sealedBatchReview(round int, batchFindingIDs []string, findings []string, drain string, planStatus string) map[string]any {
	ids := make([]any, 0, len(batchFindingIDs))
	for _, id := range batchFindingIDs {
		ids = append(ids, id)
	}
	return map[string]any{
		"round":       round,
		"clean_round": nil,
		"plan": map[string]any{
			"plan_id": "review-plan-g", "path": ".claude/review/plans/review-plan-g.json",
			"sha256": "aaaa", "revision": 1, "review_round": round,
			"status": planStatus, "e2e_coverage_state": "regression_available",
			"submitted_at": "2026-01-01T00:00:00Z",
		},
		"claims": map[string]any{
			"claim-qa-1": map[string]any{
				"lens": "qa", "applicability": "required", "disposition": "finding",
				"assignment_id": "assignment-qa-1", "result_id": "ev-r1", "finding_ids": ids,
			},
		},
		"assignments": map[string]any{},
		"observation_batch": map[string]any{
			"batch_id": "observation-batch-r1", "path": "evidence/batch.json",
			"sha256": "bbbb", "finding_ids": ids, "drain_policy": drain,
			"sealed_at": "2026-01-01T00:00:00Z",
		},
	}
}

func findingRows(ids ...string) []any {
	rows := []any{}
	for _, id := range ids {
		rows = append(rows, map[string]any{"finding_id": id, "review_round": float64(1)})
	}
	return rows
}

func TestObservationBatchGateRequiresSealedPlan(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	evaluator := NewEvaluator(mustRegistry(t, catalog))

	// plan still cannot_clean -> the batch is not sealed yet.
	entry, files := batchEnvelopeFile("observation-batch-r1", 1)
	input := s7GateInput(t, "GATE-VERIFY-BLOCKING-FINDING", "TR-008", "observation_sealed",
		sealedBatchReview(1, []string{"finding-1"}, []string{"finding-1"}, "complete_required_claims", "cannot_clean"),
		[]any{entry})
	input.Files = files
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == StatusSatisfied {
		t.Fatal("gate must not satisfy while the plan is not observation_sealed")
	}
	if !containsToken(result.Missing, "batch:plan_status=cannot_clean") {
		t.Fatalf("missing must name the plan status, got %v", result.Missing)
	}
}

func TestObservationBatchGateExactFindingSet(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	evaluator := NewEvaluator(mustRegistry(t, catalog))

	// Batch carries finding-1 but a second current-round finding exists.
	entry, files := batchEnvelopeFile("observation-batch-r1", 1)
	input := s7GateInput(t, "GATE-VERIFY-BLOCKING-FINDING", "TR-008", "observation_sealed",
		sealedBatchReview(1, []string{"finding-1"}, []string{"finding-1"}, "complete_required_claims", "observation_sealed"),
		[]any{entry})
	input.Files = files
	input.Snapshot.State["entities"].(map[string]any)["findings"] = findingRows("finding-1", "finding-2")
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == StatusSatisfied {
		t.Fatal("a batch missing a current-round finding must not satisfy")
	}
	if !containsToken(result.Missing, "batch:finding_set:finding-2:missing_from_batch") {
		t.Fatalf("missing must name the unbatched finding, got %v", result.Missing)
	}
}

func TestObservationBatchGateSatisfied(t *testing.T) {
	// The exact-set check is the subject; the base evidence requirement
	// (envelope file readability) is covered by evaluator_test.go. The
	// finding's evidence row + readable hash-matching file are part of the
	// satisfied shape (post-seal tamper detection).
	files := memFiles{}
	data := []byte(`{"finding_id":"finding-1","lens":"qa"}`)
	files["evidence/finding-1.json"] = data
	input := s7GateInput(t, "GATE-VERIFY-BLOCKING-FINDING", "TR-008", "observation_sealed",
		sealedBatchReview(1, []string{"finding-1"}, []string{"finding-1"}, "complete_required_claims", "observation_sealed"),
		[]any{map[string]any{
			"id": "finding-1", "kind": "finding", "path": "evidence/finding-1.json",
			"sha256": sha256HexLocal(data), "status": "valid", "baseline_generation": 1, "review_round": 1,
		}})
	input.Files = files
	input.Snapshot.State["entities"].(map[string]any)["findings"] = findingRows("finding-1")
	eval := &Evaluation{GateID: "GATE-VERIFY-BLOCKING-FINDING"}
	applyObservationBatchGate(input, eval)
	if len(eval.Missing) != 0 {
		t.Fatalf("matching sealed batch must add no missing tokens, got %v", eval.Missing)
	}
	if eval.Status == StatusNotReady {
		t.Fatal("matching sealed batch must stay satisfiable")
	}
}

// A deleted or mutated Finding file must fail the TR-008 gate even though the
// state projections still agree — post-seal tamper detection (verified as a
// real gap in the S7 round-3 sandbox review).
func TestObservationBatchGateDetectsFindingFileTampering(t *testing.T) {
	data := []byte(`{"finding_id":"finding-1","lens":"qa"}`)

	deleted := s7GateInput(t, "GATE-VERIFY-BLOCKING-FINDING", "TR-008", "observation_sealed",
		sealedBatchReview(1, []string{"finding-1"}, []string{"finding-1"}, "complete_required_claims", "observation_sealed"),
		[]any{map[string]any{
			"id": "finding-1", "kind": "finding", "path": "evidence/finding-1.json",
			"sha256": sha256HexLocal(data), "status": "valid", "baseline_generation": 1, "review_round": 1,
		}})
	deleted.Files = memFiles{} // file removed after sealing
	deleted.Snapshot.State["entities"].(map[string]any)["findings"] = findingRows("finding-1")
	eval := &Evaluation{GateID: "GATE-VERIFY-BLOCKING-FINDING"}
	applyObservationBatchGate(deleted, eval)
	if !containsToken(eval.Missing, "batch:finding_file:finding-1:unreadable") {
		t.Fatalf("deleted finding file must be named, got %v", eval.Missing)
	}

	mutated := s7GateInput(t, "GATE-VERIFY-BLOCKING-FINDING", "TR-008", "observation_sealed",
		sealedBatchReview(1, []string{"finding-1"}, []string{"finding-1"}, "complete_required_claims", "observation_sealed"),
		[]any{map[string]any{
			"id": "finding-1", "kind": "finding", "path": "evidence/finding-1.json",
			"sha256": sha256HexLocal(data), "status": "valid", "baseline_generation": 1, "review_round": 1,
		}})
	mutated.Files = memFiles{"evidence/finding-1.json": []byte(`{"finding_id":"finding-1","severity":"P0"}`)}
	mutated.Snapshot.State["entities"].(map[string]any)["findings"] = findingRows("finding-1")
	eval = &Evaluation{GateID: "GATE-VERIFY-BLOCKING-FINDING"}
	applyObservationBatchGate(mutated, eval)
	if !containsToken(eval.Missing, "batch:finding_file:finding-1:hash_mismatch") {
		t.Fatalf("mutated finding file must be named, got %v", eval.Missing)
	}
}

func TestCleanRoundGateRecomputesPlan(t *testing.T) {
	eval := &Evaluation{GateID: "GATE-VERIFY-CLEAN-ROUND-PASSED"}
	// No plan registered: the gate must name review_plan_clean.
	input := s7GateInput(t, "GATE-VERIFY-CLEAN-ROUND-PASSED", "TR-009", "clean",
		map[string]any{"round": 1, "clean_round": nil}, nil)
	applyCleanRoundGate(input, eval)
	if !containsToken(eval.Missing, "cleanround:review_plan_clean") {
		t.Fatalf("missing must name the plan check, got %v", eval.Missing)
	}
	if eval.Status != StatusNotReady {
		t.Fatalf("status = %v, want not_ready", eval.Status)
	}
}

func containsToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}

func mustRegistry(t *testing.T, catalog *transition.Catalog) *Registry {
	t.Helper()
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// A routing-verdict gate stays not_ready (never unknown) while ordinary
// results accumulate — conclusion mismatch is the normal state, not a
// naming conflict (the heuristic must not fire here).
func TestRoutingVerdictGateSkipsOrdinaryResults(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	evaluator := NewEvaluator(mustRegistry(t, catalog))

	entry, files := reviewResultEnvelope("ev-ordinary", "pass", 1)
	input := s7GateInput(t, "GATE-VERIFY-REQ-CHANGE-REQUIRED", "TR-010", "running",
		map[string]any{"round": 1, "clean_round": nil}, []any{entry})
	input.Files = files
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNotReady {
		t.Fatalf("status = %q, want not_ready (ordinary results are not a conflict)", result.Status)
	}
	if len(result.Conflicts) > 0 {
		t.Fatalf("ordinary results must not produce conflicts, got %v", result.Conflicts)
	}
	if !containsToken(result.Missing, "evidence:review_result_record") {
		t.Fatalf("missing must name the absent pause verdict, got %v", result.Missing)
	}
}

// reviewResultEnvelope builds a registered review_result evidence pair.
func reviewResultEnvelope(id, conclusion string, round int) (map[string]any, memFiles) {
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             id,
		"kind":                    "review_result",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"review_round":            round,
		"producer_agent_id":       "agent-qa-1",
		"producer_responsibility": "QA",
		"subject_refs":            []any{},
		"conclusion":              conclusion,
	}
	data, _ := json.Marshal(envelope)
	path := "evidence/" + id + ".json"
	entry := map[string]any{
		"id": id, "kind": "review_result", "path": path, "sha256": sha256HexLocal(data),
		"status": "valid", "baseline_generation": 1, "review_round": round,
		"produced_by": []any{"agent-qa-1"}, "invalidated_by": nil,
		"responsibility_id": "QA", "scope_refs": []any{},
	}
	return entry, memFiles{path: data}
}
