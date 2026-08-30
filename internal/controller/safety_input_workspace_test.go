package controller

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// The reviewer product-write rule exempts the ReviewPlan's verification
// artifact workspace (L3-S7 §8). buildSafetyInput must project that
// workspace from the snapshot — without it the wire path hard-denies the
// only write surface an E2E cold-start Reviewer is allowed to use
// (2026-08-22 E2E cold-start dogfood finding).
func TestBuildSafetyInputProjectsVerificationWorkspace(t *testing.T) {
	snapshot := runtime.Snapshot{
		State: map[string]any{
			"runtime_id": "loop-REQ-1",
			"lifecycle":  map[string]any{"state": "verification", "phase": "running"},
			"review": map[string]any{
				"round": 1,
				"plan": map[string]any{
					"status":                          "running",
					"verification_artifact_workspace": "e2e-workspace/review-plan-1",
				},
			},
		},
	}
	input := buildSafetyInput(ControlRequest{Root: t.TempDir()}, snapshot, nil)
	if input.Runtime.VerificationWorkspace != "e2e-workspace/review-plan-1" {
		t.Fatalf("VerificationWorkspace not projected: %q", input.Runtime.VerificationWorkspace)
	}
	if input.Runtime.CurrentState != "verification" {
		t.Fatalf("CurrentState not projected: %q", input.Runtime.CurrentState)
	}
}

// A state without a registered plan must leave the workspace empty rather
// than inventing one.
func TestBuildSafetyInputWithoutPlanLeavesWorkspaceEmpty(t *testing.T) {
	snapshot := runtime.Snapshot{
		State: map[string]any{
			"runtime_id": "loop-REQ-1",
			"lifecycle":  map[string]any{"state": "building", "phase": "implementation"},
		},
	}
	input := buildSafetyInput(ControlRequest{Root: t.TempDir()}, snapshot, nil)
	if input.Runtime.VerificationWorkspace != "" {
		t.Fatalf("expected empty workspace, got %q", input.Runtime.VerificationWorkspace)
	}
}
