package impact_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/impact"
)

func validEvidence(id string, scope ...string) map[string]any {
	scopes := make([]any, 0, len(scope))
	for _, s := range scope {
		scopes = append(scopes, s)
	}
	return map[string]any{
		"id":                  id,
		"kind":                "delivery_review",
		"path":                "docs/reports/review/REV-X.md",
		"sha256":              "abc",
		"status":              "valid",
		"baseline_generation": float64(1),
		"review_round":        float64(1),
		"produced_by":         []any{"agent-x"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "VER-REQ-GAP",
		"scope_refs":          scopes,
	}
}

func stateWith(evidence ...map[string]any) map[string]any {
	arr := make([]any, 0, len(evidence))
	for _, e := range evidence {
		arr = append(arr, e)
	}
	return map[string]any{
		"baseline": map[string]any{"generation": float64(1)},
		"evidence": arr,
	}
}

func TestComputeImpactREQChangeInvalidatesWholeBaseline(t *testing.T) {
	state := stateWith(
		validEvidence("ev-1", "docs/contracts/CONTRACTS-002.md"),
		validEvidence("ev-2", "docs/tasks/TASK-012.md"),
	)
	impacts := impact.ComputeImpact(state, []string{"docs/requirements/REQ-002.md"})
	if len(impacts) != 2 {
		t.Fatalf("expected 2 impacted evidence entries, got %d", len(impacts))
	}
	for _, item := range impacts {
		if item.Rule != "req_baseline_change" {
			t.Errorf("expected rule req_baseline_change, got %s", item.Rule)
		}
		if item.AlreadyInvalid {
			t.Errorf("expected ev-%s to be newly impacted", item.EvidenceID)
		}
	}
}

func TestComputeImpactContractChangeOnlyAffectsScopedEvidence(t *testing.T) {
	state := stateWith(
		validEvidence("ev-contract", "docs/contracts/CONTRACTS-002.md"),
		validEvidence("ev-unrelated", "docs/contracts/CONTRACTS-009.md"),
	)
	impacts := impact.ComputeImpact(state, []string{"docs/contracts/CONTRACTS-002.md"})
	if len(impacts) != 1 {
		t.Fatalf("expected 1 impacted entry, got %d", len(impacts))
	}
	if impacts[0].EvidenceID != "ev-contract" {
		t.Errorf("expected ev-contract impacted, got %s", impacts[0].EvidenceID)
	}
	if impacts[0].Rule != "contract_change" {
		t.Errorf("expected contract_change, got %s", impacts[0].Rule)
	}
}

func TestComputeImpactAlreadyInvalidReported(t *testing.T) {
	ev := validEvidence("ev-old", "docs/contracts/CONTRACTS-002.md")
	ev["status"] = "invalid"
	state := stateWith(ev)
	impacts := impact.ComputeImpact(state, []string{"docs/contracts/CONTRACTS-002.md"})
	if len(impacts) != 1 {
		t.Fatalf("expected 1 impacted entry, got %d", len(impacts))
	}
	if !impacts[0].AlreadyInvalid {
		t.Error("expected AlreadyInvalid=true for previously invalidated evidence")
	}
}

func TestInvalidateEvidenceMarksNewlyAffectedOnly(t *testing.T) {
	evFresh := validEvidence("ev-fresh", "docs/contracts/CONTRACTS-002.md")
	evAlready := validEvidence("ev-already", "docs/contracts/CONTRACTS-002.md")
	evAlready["status"] = "invalid"
	evAlready["invalidated_by"] = "PTR-BUG-05"
	evAlready["invalidation_rule"] = "previous"
	evAlready["invalidation_reason"] = "previous reason"
	state := stateWith(evFresh, evAlready)

	impacts := impact.ComputeImpact(state, []string{"docs/contracts/CONTRACTS-002.md"})
	newlyInvalidated := impact.InvalidateEvidence(state, impacts, "TR-007")

	if len(newlyInvalidated) != 1 || newlyInvalidated[0] != "ev-fresh" {
		t.Fatalf("expected only ev-fresh newly invalidated, got %v", newlyInvalidated)
	}
	evidence := state["evidence"].([]any)
	for _, raw := range evidence {
		entry := raw.(map[string]any)
		if entry["id"] == "ev-fresh" {
			if entry["status"] != "invalid" {
				t.Error("ev-fresh should be invalid after InvalidateEvidence")
			}
			if entry["invalidated_by"] != "TR-007" {
				t.Errorf("expected invalidated_by TR-007, got %v", entry["invalidated_by"])
			}
		}
		if entry["id"] == "ev-already" {
			if entry["invalidated_by"] != "PTR-BUG-05" {
				t.Error("ev-already should retain its original invalidation record")
			}
		}
	}
}

func TestComputeImpactEmptyChangedPathsReturnsNil(t *testing.T) {
	state := stateWith(validEvidence("ev-1", "docs/contracts/CONTRACTS-002.md"))
	if impacts := impact.ComputeImpact(state, nil); impacts != nil {
		t.Errorf("expected nil for empty changedPaths, got %v", impacts)
	}
}

func TestComputeImpactDifferentBaselineGenerationNotAffectedByREQChange(t *testing.T) {
	evOld := validEvidence("ev-gen0", "docs/contracts/CONTRACTS-002.md")
	evOld["baseline_generation"] = float64(0)
	state := stateWith(evOld)
	impacts := impact.ComputeImpact(state, []string{"docs/requirements/REQ-002.md"})
	// baseline 0 evidence should NOT be invalidated by a baseline-1 REQ change
	// unless its scope_refs directly reference the REQ path.
	if len(impacts) != 0 {
		t.Fatalf("expected 0 impacted entries for different generation, got %d", len(impacts))
	}
}
