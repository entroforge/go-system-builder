package assignment_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
)

func TestBehaviorSurfaceRegisterPlanThenDispatch(t *testing.T) {
	for _, surface := range []string{"non_browser", "browser", "legacy"} {
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
			body, err := os.ReadFile(filepath.Join(root, source))
			if err != nil {
				t.Fatal(err)
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(body))
			bound := state["bound_req"].(map[string]any)
			bound["id"], bound["path"], bound["version"], bound["sha256"] = "REQ-002", source, "v1.0.0", sum
			bound["metadata"] = map[string]any{"ui_impact": "none"}
			statePath, journal := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
			writeJSON(t, statePath, state)
			justification, workspace := "Documentation-only baseline: exercise the documented command contract; no executable implementation changed.", "e2e-workspace/behavior"
			plan := review.Plan{SchemaVersion: "1.0.0", ReviewPlanID: "review-plan-behavior", ReviewRound: 1, BaselineGeneration: 1, FrozenSubjects: []review.FrozenSubject{{Path: source, SHA256: sum, Kind: "contract"}}, Claims: []review.Claim{{ClaimID: "claim-command", Lens: "e2e", Target: "documented command behavior", Assertion: "documented command yields declared exit status", Oracle: "actual exit status and persisted state match the source contract", Method: "actual command and state observation", Applicability: "required", SourceRefs: []string{source}, FocusKey: "command"}}, Assignments: []review.PlanAssignment{{AssignmentID: "assignment-command", Lens: "e2e", ClaimIDs: []string{"claim-command"}, NonOverlapBoundary: "owns documented command behavior only", ExecutionWave: "behavior"}}, E2ECoverageState: "cold_start", VerificationArtifactWorkspace: &workspace, DispatchCapacityPolicy: "coverage_complete", CoverageJustification: &justification, CreatedBy: "Main", CreatedAt: "2026-10-24T00:00:00Z"}
			path := filepath.Join(dir, "answer.json")
			writeJSON(t, path, plan)
			before, _ := os.ReadFile(statePath)
			if _, err := review.RegisterPlan(root, statePath, journal, review.PlanRequest{ExpectedRevision: 1, PlanPath: path}); err == nil || !strings.Contains(err.Error(), "behavior_surface") {
				t.Fatalf("new missing field must return to author: %v", err)
			}
			after, _ := os.ReadFile(statePath)
			if string(before) != string(after) {
				t.Fatal("incomplete Plan mutated Runtime")
			}
			if _, err := os.Stat(filepath.Join(dir, "review/plans/review-plan-behavior.json")); !os.IsNotExist(err) {
				t.Fatal("incomplete Plan was pinned")
			}
			plan.Assignments[0].BehaviorSurface = surface
			if surface == "legacy" {
				plan.Assignments[0].BehaviorSurface = "browser"
			}
			writeJSON(t, path, plan)
			registered, err := review.RegisterPlan(root, statePath, journal, review.PlanRequest{ExpectedRevision: 1, PlanPath: path})
			if err != nil {
				t.Fatal(err)
			}
			if surface == "legacy" {
				// Simulate the exact already-registered pre-field artifact and its original
				// pin. Recovery must not demand a revision or modify its frozen bytes.
				ptr := registered.State["review"].(map[string]any)["plan"].(map[string]any)
				plan.Assignments[0].BehaviorSurface = ""
				data, _ := json.Marshal(plan)
				pinned := filepath.Join(root, ptr["path"].(string))
				if err := os.WriteFile(pinned, data, 0644); err != nil {
					t.Fatal(err)
				}
				ptr["sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
				writeJSON(t, statePath, registered.State)
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
