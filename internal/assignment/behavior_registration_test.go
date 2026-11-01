package assignment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
)

func TestBehaviorSurfaceRegisterPlanThenDispatch(t *testing.T) {
	for _, surface := range []string{"non_browser", "browser", "unspecified"} {
		t.Run(surface, func(t *testing.T) {
			root := assignmentTestRoot(t)
			dir := filepath.Join(root, ".claude")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			state := activeState(t, root, "verification", "planned", 1)
			state["review"] = map[string]any{"round": 1, "clean_round": nil}
			state["documents"] = []any{}
			state["baseline"].(map[string]any)["generation"] = 1
			source := "docs/control/agent-protocol.md"
			bound := state["bound_req"].(map[string]any)
			bound["id"], bound["path"], bound["version"] = "REQ-002", source, "v1.0.0"
			bound["metadata"] = map[string]any{"ui_impact": "none"}
			statePath, journal := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
			writeJSON(t, statePath, state)
			justification, workspace := "Documentation-only baseline: exercise the documented command contract; no executable implementation changed.", "e2e-workspace/behavior"
			plan := review.Plan{SchemaVersion: "1.0.0", ReviewPlanID: "review-plan-behavior", BaselineGeneration: 1, FrozenSubjects: []review.FrozenSubject{{Path: source, Kind: "contract"}}, Claims: []review.Claim{{ClaimID: "claim-command", Lens: "e2e", Target: "documented command behavior", Assertion: "documented command yields declared exit status", Oracle: "actual exit status and persisted state match the source contract", Method: "actual command and state observation", Applicability: "required", SourceRefs: []string{source}, FocusKey: "command"}}, Assignments: []review.PlanAssignment{{AssignmentID: "assignment-command", Lens: "e2e", ClaimIDs: []string{"claim-command"}, NonOverlapBoundary: "owns documented command behavior only", ExecutionWave: "behavior"}}, E2ECoverageState: "cold_start", VerificationArtifactWorkspace: &workspace, DispatchCapacityPolicy: "coverage_complete", CoverageJustification: &justification, CreatedBy: "Main", CreatedAt: "2026-10-24T00:00:00Z"}
			path := filepath.Join(dir, "answer.json")
			writeJSON(t, path, plan)
			if surface != "unspecified" {
				plan.Assignments[0].BehaviorSurface = surface
			}
			writeJSON(t, path, plan)
			registered, err := review.RegisterPlan(root, statePath, journal, review.PlanRequest{ExpectedRevision: 1, PlanPath: path})
			if err != nil {
				t.Fatal(err)
			}
			draft, _, err := review.DraftManifest(root, registered.State, "assignment-command")
			if err != nil {
				t.Fatal(err)
			}
			if surface == "non_browser" && draft.Assignments[0].ResponsibilityID != "E2E-BEHAVIOR" {
				t.Fatal("non-browser responsibility lost")
			}
			manifestPath := filepath.Join(dir, "manifest.json")
			writeJSON(t, manifestPath, draft)
			taskPath := filepath.Join(dir, "TASK-012.md")
			if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0644); err != nil {
				t.Fatal(err)
			}
			dispatched, err := assignment.Register(root, statePath, journal, assignment.Request{ExpectedRevision: registered.Revision, ManifestPath: manifestPath, TaskID: "TASK-012", TaskPath: taskPath})
			if err != nil {
				t.Fatal(err)
			}
			row := dispatched.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-command"].(map[string]any)
			if row["status"] != "dispatched" {
				t.Fatalf("not dispatched: %v", row)
			}
		})
	}
}
