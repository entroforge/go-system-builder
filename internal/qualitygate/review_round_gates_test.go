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

func TestAcceptedFindingGateUsesOriginalResponsibility(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	evaluator := NewEvaluator(mustRegistry(t, catalog))
	input := s7GateInput(t, "GATE-VERIFY-BLOCKING-FINDING", "TR-008", "observation_sealed",
		map[string]any{"round_entry": map[string]any{"plan_id": "plan-current"}}, nil)
	for _, tc := range []struct {
		name, source string
		want         Status
	}{
		{"current accepted finding", "plan-current", StatusSatisfied},
		{"old finding", "plan-old", StatusNotReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input.Snapshot.State["entities"].(map[string]any)["findings"] = []any{map[string]any{"finding_id": "finding", "review_plan_id": tc.source}}
			result, err := evaluator.Evaluate(context.Background(), input)
			if err != nil || result.Status != tc.want {
				t.Fatalf("original Finding handoff: %+v %v", result, err)
			}
		})
	}
}

func TestCleanGateReportsOriginalUnfinishedResponsibility(t *testing.T) {
	eval := &Evaluation{GateID: "GATE-VERIFY-CLEAN-ROUND-PASSED"}
	input := s7GateInput(t, "GATE-VERIFY-CLEAN-ROUND-PASSED", "TR-009", "clean", map[string]any{"round_entry": map[string]any{"plan_id": "plan-current"}}, nil)
	applyCleanRoundGate(input, eval)
	if eval.Status != StatusNotReady || !containsToken(eval.Missing, "review:necessary_responsibilities_unfinished_or_finding") {
		t.Fatalf("unfinished original responsibility hidden: %+v", eval)
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
