package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// Explicit evaluation export only: synthetic S7 entry, never canonical S6 proof.
// Original domain writers issue unanswered questionnaires for real model review.
func TestExportS7BrowserEvaluation(t *testing.T) {
	root, product := os.Getenv("GSB_S7_EVAL_ROOT"), os.Getenv("GSB_S7_PRODUCT_ROOT")
	if root == "" || product == "" {
		t.Skip("requires explicit GSB_S7_EVAL_ROOT and GSB_S7_PRODUCT_ROOT")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	product, err = filepath.Abs(product)
	if err != nil {
		t.Fatal(err)
	}
	if root == sourceRepositoryRoot(t) || root == product || strings.HasPrefix(root, product+string(filepath.Separator)) {
		t.Fatal("export root must be separate from product root")
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	for _, path := range []string{statePath + ".commit-pending.json", statePath + ".fingerprint-pending.json", statePath + ".rollover-pending.json", statePath + ".journal-rotation-pending.json", filepath.Join(root, ".claude/loop-pending.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("refuse existing pending control operation: %s (%v)", path, err)
		}
	}
	if data, err := os.ReadFile(journalPath); err == nil {
		if len(bytes.TrimSpace(data)) != 0 {
			t.Fatal("refuse nonempty installed Runtime journal")
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(statePath); err == nil {
		var installed map[string]any
		if err := json.Unmarshal(data, &installed); err != nil {
			t.Fatal(err)
		}
		if err := loopruntime.ValidateFreshInactiveState(installed); err != nil {
			t.Fatalf("only original fresh inactive installation may be replaced by synthetic entry: %v", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	// Install the frozen candidate into root first. Do not copy another Runtime.
	for _, path := range []string{"docs/control/loop-definition.json", ".claude/agents/delivery-verifier.md", ".claude/agents/qa.md", ".claude/agents/e2e-tester.md", ".claude/skills/integration-verification/SKILL.md", ".claude/skills/testing-strategy/SKILL.md", ".claude/skills/e2e-browser-testing/SKILL.md", ".claude/skills/playwright-e2e/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("candidate installation missing %s: %v", path, err)
		}
	}
	var subjects []review.FrozenSubject
	err = filepath.WalkDir(product, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(product, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "dist", "build", "output", "artifacts", "screenshots", "test-results", "playwright-report":
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("product must contain regular source files, got %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeFixtureFile(t, root, filepath.ToSlash(rel), data)
		subjects = append(subjects, review.FrozenSubject{Path: filepath.ToSlash(rel), SHA256: digestHex(data), Kind: "product_code"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const reqPath, taskPath = "docs/requirements/REQ-001.md", "docs/dev/tasks/TASK-001.md"
	now := time.Now().UTC().Format(time.RFC3339)
	doc := func(id, kind, path string) map[string]any {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("product contract required at %s: %v", path, err)
		}
		return map[string]any{"id": id, "kind": kind, "path": path, "version": "v1.0.0", "sha256": digestHex(data), "status": "locked", "generation": 1}
	}
	req, task := doc("REQ-001", "req", reqPath), doc("TASK-001", "task", taskPath)
	if _, err := os.Stat(filepath.Join(root, "docs/design/prototypes/settings/cases.json")); err != nil {
		t.Fatalf("actual browser CASE inventory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		gitFixture(t, root, "init", "-b", "main")
	}
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "-c", "user.name=S7 evaluation fixture", "-c", "user.email=s7-eval@example.invalid", "commit", "--allow-empty", "-m", "Freeze synthetic browser evaluation product and installed sources")
	branch := gitFixture(t, root, "symbolic-ref", "--short", "HEAD")
	head := gitFixture(t, root, "rev-parse", "HEAD")
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"], state["revision"], state["runtime_id"] = "1.2.0", 1, "loop-REQ-001"
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "planned", "phase_revision": 0}
	state["baseline"] = map[string]any{"generation": 1, "captured_at": now}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["bound_req"] = map[string]any{"id": "REQ-001", "path": reqPath, "version": "v1.0.0", "sha256": req["sha256"], "status": "locked", "approved_by": "evaluation-fixture", "approved_at": now, "metadata": map[string]any{"ui_impact": "changed"}}
	state["bound_req"].(map[string]any)["workspace"] = map[string]any{"project_root": root, "dev_branch": branch, "release_upstream": branch, "bound_commit": head}
	state["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "teams": []any{}, "bugs": []any{}}
	state["documents"], state["evidence"] = []any{req, task}, []any{}
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	writeJSONFixture(t, statePath, state)
	workspace := "e2e-workspace/review-plan-browser-eval"
	plan := &review.Plan{SchemaVersion: "1.0.0", ReviewPlanID: "review-plan-browser-eval", ReviewRound: 1, BaselineGeneration: 1, FrozenSubjects: subjects, E2ECoverageState: "cold_start", VerificationArtifactWorkspace: &workspace, DispatchCapacityPolicy: "coverage_complete", CreatedBy: "evaluation-fixture", CreatedAt: now}
	for _, item := range []struct{ id, lens, target, assertion, oracle, method, wave string }{
		{"dv", "delivery", "docs/design/prototypes/settings", "Settings delivery matches the locked CASE/PATH contract", "Trace both CASEs to the actual UI and service behavior; do not infer PASS from fixture setup", "traceability inspection", "static"},
		{"qa", "qa", "settings implementation", "Validation and persistence boundaries preserve the stated contract", "Inspect validation, state updates and error boundaries against REQ-001", "code inspection", "static"},
		{"e2e", "e2e", "settings browser flows", "CASE-001/PATH-001 and CASE-002/PATH-002 match their declared oracles, with safe recovery after the negative case", "Execute nonempty save, blank-input rejection and a subsequent valid save in the actual browser; preserve observations and actual evidence", "real-browser execution", "behavior"},
	} {
		claimID, assignmentID := "claim-browser-"+item.id, "assignment-browser-"+item.id
		plan.Claims = append(plan.Claims, review.Claim{ClaimID: claimID, Lens: item.lens, Target: item.target, Assertion: item.assertion, Oracle: item.oracle, Method: item.method, Applicability: "required", SourceRefs: []string{"REQ-001", "TASK-001", "docs/design/prototypes/settings/cases.json"}, FocusKey: item.id})
		plan.Assignments = append(plan.Assignments, review.PlanAssignment{AssignmentID: assignmentID, Lens: item.lens, ClaimIDs: []string{claimID}, NonOverlapBoundary: item.id + " independent professional responsibility", ExecutionWave: item.wave})
	}
	// Use the original inventory's existing executable assets, not cold-start
	// authoring, when this prepared product already contains reusable specs.
	inventoryPlan, notes := review.DraftPlanForRoot(root, state, 1)
	if inventoryPlan == nil || len(inventoryPlan.E2EAssets) == 0 {
		t.Fatalf("prepared browser product has no original reusable E2E assets: %v", notes)
	}
	plan.E2ECoverageState, plan.VerificationArtifactWorkspace = "regression_available", nil
	plan.E2EAssets = inventoryPlan.E2EAssets
	planPath := filepath.Join(root, ".claude/submissions/loop-REQ-001/g1/S7/r1/review-plan.json")
	writeJSONFixture(t, planPath, plan)
	registered, err := review.RegisterPlan(root, statePath, journalPath, review.PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	for _, planned := range plan.Assignments {
		manifest, _, err := review.DraftManifest(root, registered.State, planned.AssignmentID)
		if err != nil {
			t.Fatal(err)
		}
		manifest.PlatformTeamID = "evaluation-browser-session"
		manifest.Assignments[0].AgentDefinitionRef = filepath.ToSlash(filepath.Join(".claude/agents", filepath.Base(manifest.Assignments[0].AgentDefinitionRef)))
		switch planned.Lens {
		case "delivery":
			manifest.Assignments[0].SkillRefs = []string{"integration-verification"}
		case "qa":
			manifest.Assignments[0].SkillRefs = []string{"testing-strategy"}
		case "e2e":
			manifest.Assignments[0].SkillRefs = []string{"e2e-browser-testing", "playwright-e2e"}
		}
		manifest.Assignments[0].AgentID = "reviewer-browser-" + strings.TrimPrefix(planned.AssignmentID, "assignment-browser-")
		manifestPath := filepath.Join(root, ".claude/workgroups", manifest.ManifestID+".json")
		writeJSONFixture(t, manifestPath, manifest)
		if planned.ExecutionWave == "behavior" {
			t.Logf("After real static dispositions, original E2E registration: %s runtime register-workgroup --root %s --manifest %s --task-id TASK-001 --task %s", quote(filepath.Join(root, ".claude/bin/loop-harness")), quote(root), quote(manifestPath), quote(taskPath))
			continue
		}
		registered, err = assignment.Register(root, statePath, journalPath, assignment.Request{ExpectedRevision: registered.Revision, ManifestPath: manifestPath, TaskID: "TASK-001", TaskPath: filepath.Join(root, taskPath)})
		if err != nil {
			t.Fatal(err)
		}
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := service.IssueReviewResults(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, packet := range packets {
		t.Logf("Unanswered %s view=%s answer=%s", packet.Binding.OwnerAgentID, filepath.Join(root, packet.ViewPath), filepath.Join(root, packet.AnswerPath))
	}
	t.Log("Synthetic S7 evaluation entry only; no canonical S6 proof, checks, verdicts, Findings, readbacks or PLAN reports were created. Dispatch independent real Reviewers now; E2E remains subject to the original static wave.")
}
