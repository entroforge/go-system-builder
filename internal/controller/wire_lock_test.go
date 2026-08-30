package controller

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// TestWireSafetyLocksArtifactsFromStage pins the S5 final-round F1 fix:
// the wire path's buildSafetyInput must thread LockedArtifacts + the
// current stage into the safety policy, so a registered contract is
// editable during the S5 repair loop and write-blocked from S6 on —
// both directions, through the same projection the controller uses.
func TestWireSafetyLocksArtifactsFromStage(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	snapshotFor := func(stage string) runtime.Snapshot {
		return runtime.Snapshot{
			Revision: 9,
			State: map[string]any{
				"runtime_id": "loop-wire",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil},
				"milestone":  map[string]any{"stage": stage},
				"baseline":   map[string]any{"generation": float64(1)},
				"documents": []any{map[string]any{
					"id": "BE-1", "kind": "contract", "path": "docs/contracts/BE-1.md",
					"version": "v1.0.0", "sha256": "a1b2c3", "status": "locked", "generation": float64(1),
				}},
			},
		}
	}
	run := func(stage string) policy.Decision {
		input := buildSafetyInput(ControlRequest{
			Root: ".", Event: "PreToolUse", ToolName: "Edit",
			ToolInput: map[string]any{"file_path": "docs/contracts/BE-1.md"},
		}, snapshotFor(stage), nil)
		decision, err := engine.Evaluate(input)
		if err != nil {
			t.Fatalf("evaluate at %s: %v", stage, err)
		}
		return decision
	}
	if d := run("S5"); d.Decision == "block" {
		t.Fatalf("S5 repair loop must stay writable, got block: %#v", d)
	}
	if d := run("S6"); d.Decision != "deny" || d.Reason != "locked_artifact_write" {
		t.Fatalf("S6+ must deny registered-artifact writes with locked_artifact_write, got %#v", d)
	}
	// Superseded non-req generations never reach this projection (the
	// loader keeps only the current generation for non-req kinds — the old
	// file moves to versions/g{N+1}/ on the rework path); the policy-level
	// always-lock branch for them stays as defense and is pinned in
	// tests/policy/minimal_policy_test.go.
}
