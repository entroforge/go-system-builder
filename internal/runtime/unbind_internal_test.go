package runtime

import (
	"strings"
	"testing"
)

// TestLifecycleApprovalScopeIsolation pins the receipt-isolation claim: an
// unbind-scoped human decision cannot authorize a rollover and vice versa —
// the scope prefix is part of the approval contract, not cosmetic.
func TestLifecycleApprovalScopeIsolation(t *testing.T) {
	state := map[string]any{
		"evidence": []any{
			map[string]any{
				"id":                  "ev-unbind",
				"kind":                "human_decision",
				"status":              "valid",
				"produced_by":         []any{"alice"},
				"scope_refs":          []any{"runtime_unbind:loop-REQ-1@3"},
				"sha256":              "x",
				"path":                ".claude/decisions/x.json",
				"baseline_generation": 1,
			},
		},
	}
	if err := validateLifecycleApproval(state, "alice", "ev-unbind", "loop-REQ-1", 3, "runtime_unbind"); err != nil {
		t.Fatalf("unbind-scoped evidence must authorize the matching verb: %v", err)
	}
	err := validateLifecycleApproval(state, "alice", "ev-unbind", "loop-REQ-1", 3, "runtime_rollover")
	if err == nil || !strings.Contains(err.Error(), "runtime_rollover") {
		t.Fatalf("unbind-scoped evidence must NOT authorize a rollover, got: %v", err)
	}
	err = validateLifecycleApproval(state, "bob", "ev-unbind", "loop-REQ-1", 3, "runtime_unbind")
	if err == nil {
		t.Fatal("evidence produced by another identity must be rejected")
	}
	err = validateLifecycleApproval(state, "alice", "ev-unbind", "loop-REQ-1", 4, "runtime_unbind")
	if err == nil {
		t.Fatal("evidence scoped to another revision must be rejected")
	}
}
