package review

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
)

// pinPlan writes the plan to disk and pins path+sha256 in state.review.plan
// so LoadPlan can hash-verify it.
func pinPlan(t *testing.T, root string, state map[string]any, plan map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, "plan.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	reviewMap := state["review"].(map[string]any)
	reviewMap["plan"] = map[string]any{
		"plan_id":      plan["review_plan_id"],
		"path":         path,
		"sha256":       fmt.Sprintf("%x", sum[:]),
		"revision":     1,
		"review_round": plan["review_round"],
		"status":       "running",
	}
	assignments := map[string]any{}
	for _, raw := range plan["assignments"].([]any) {
		assignment := raw.(map[string]any)
		assignments[assignment["assignment_id"].(string)] = map[string]any{
			"lens":      assignment["lens"],
			"status":    "planned",
			"claim_ids": assignment["claim_ids"],
		}
	}
	reviewMap["assignments"] = assignments
}

// manifestDraftFixturePlan carries focus keys and a QA assignment the draft
// binds.
func manifestDraftFixturePlan() map[string]any {
	return map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      "review-plan-md-1",
		"review_round":        1,
		"baseline_generation": 1,
		"frozen_subjects": []any{
			map[string]any{"path": "internal/example/service.go", "sha256": strings.Repeat("1", 64), "kind": "product_code"},
			map[string]any{"path": "internal/example/service_test.go", "sha256": strings.Repeat("2", 64), "kind": "test_code"},
		},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-qa-logic", "lens": "qa",
				"target": "internal/example", "assertion": "errors propagate", "oracle": "no dropped error",
				"method": "code review", "applicability": "required", "source_refs": []string{"TASK-001"},
				"focus_key": "logic-state-error",
			},
			map[string]any{
				"claim_id": "claim-qa-tests", "lens": "qa",
				"target": "internal/example", "assertion": "behavior asserted", "oracle": "valid oracle",
				"method": "test review", "applicability": "required", "source_refs": []string{"TASK-001"},
				"focus_key": "test-oracle",
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-qa-static", "lens": "qa",
				"claim_ids":            []string{"claim-qa-logic", "claim-qa-tests"},
				"focus_keys":           []string{"logic-state-error", "test-oracle"},
				"non_overlap_boundary": "owns static quality; DV owns traceability",
				"execution_wave":       "static",
			},
		},
		"e2e_coverage_state":       "not_applicable",
		"dispatch_capacity_policy": "coverage_complete",
		"created_by":               "orchestrator",
		"created_at":               "2026-08-22T00:00:00Z",
	}
}

func TestDraftManifestPrefillsControlPlaneFacts(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["documents"] = []any{
		map[string]any{
			"id": "TASK-001", "kind": "task", "path": "docs/tasks/TASK-001.md",
			"version": "v1.0.0", "sha256": strings.Repeat("3", 64),
			"status": "complete", "generation": 1,
		},
	}
	pinPlan(t, root, state, manifestDraftFixturePlan())

	draft, notes, err := DraftManifest(root, state, "assignment-qa-static")
	if err != nil {
		t.Fatalf("DraftManifest: %v", err)
	}

	// Control-plane facts land verbatim.
	if draft.RuntimeID != "loop-REQ-TEST" {
		t.Errorf("runtime_id = %q, want loop-REQ-TEST", draft.RuntimeID)
	}
	if draft.ReqID != "REQ-002" {
		t.Errorf("req_id = %q, want REQ-002 (bound REQ from the example state)", draft.ReqID)
	}
	if draft.BaselineGeneration != 1 || draft.ReviewRound != 1 {
		t.Errorf("baseline/round = %d/%d, want 1/1", draft.BaselineGeneration, draft.ReviewRound)
	}
	if draft.WorkgroupKind != "qa" {
		t.Errorf("workgroup_kind = %q, want qa", draft.WorkgroupKind)
	}

	if len(draft.Assignments) != 1 {
		t.Fatalf("assignments = %d rows, want 1", len(draft.Assignments))
	}
	row := draft.Assignments[0]
	if strings.Join(row.ClaimIDs, ",") != "claim-qa-logic,claim-qa-tests" {
		t.Errorf("claim_ids = %v, want the exact plan set", row.ClaimIDs)
	}
	if strings.Join(row.FocusKeys, ",") != "logic-state-error,test-oracle" {
		t.Errorf("focus_keys = %v, want the plan focus keys", row.FocusKeys)
	}
	if row.NonOverlapBoundary != "owns static quality; DV owns traceability" {
		t.Errorf("non_overlap_boundary = %q, want the plan boundary", row.NonOverlapBoundary)
	}
	if row.RoleFamily != "qa" || row.AgentDefinitionRef != "agents/qa.md" {
		t.Errorf("role/definition = %q/%q, want qa/agents/qa.md", row.RoleFamily, row.AgentDefinitionRef)
	}

	// Documents carry the bound REQ and the current-generation TASK.
	var ids []string
	for _, doc := range draft.Documents {
		ids = append(ids, doc.ID)
	}
	if strings.Join(ids, ",") != "REQ-002,TASK-001" {
		t.Errorf("documents = %v, want [REQ-002 TASK-001]", ids)
	}

	// The TODO surface is exactly the fields no fact decides.
	if !strings.HasPrefix(row.AgentID, "TODO(planner):") {
		t.Errorf("agent_id = %q, want a TODO(planner): marker", row.AgentID)
	}
	if len(notes) == 0 {
		t.Error("expected planner notes")
	}

	// The draft passes the same schema + semantic validation
	// register-workgroup runs (modulo the TODO markers, which are
	// schema-valid placeholders by construction).
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", data); err != nil {
		t.Fatalf("draft fails team-manifest schema: %v", err)
	}
	if err := team.ValidateBytes(data); err != nil {
		t.Fatalf("draft fails team manifest semantics: %v", err)
	}
}

func TestDraftManifestE2EPrefillsForcedSkills(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	plan := manifestDraftFixturePlan()
	workspace := ".claude/e2e-workspace/review-plan-md-1"
	plan["e2e_coverage_state"] = "cold_start"
	plan["verification_artifact_workspace"] = workspace
	plan["claims"] = append(plan["claims"].([]any), map[string]any{
		"claim_id": "claim-e2e-flow", "lens": "e2e",
		"target": "console flow", "assertion": "flows behave", "oracle": "trace shows it",
		"method": "real-browser execution", "applicability": "required", "source_refs": []string{"TASK-001"},
	})
	plan["assignments"] = append(plan["assignments"].([]any), map[string]any{
		"assignment_id": "assignment-e2e-flows", "lens": "e2e",
		"claim_ids":            []string{"claim-e2e-flow"},
		"non_overlap_boundary": "owns the declared flows",
		"execution_wave":       "behavior",
	})
	pinPlan(t, root, state, plan)

	draft, notes, err := DraftManifest(root, state, "assignment-e2e-flows")
	if err != nil {
		t.Fatalf("DraftManifest: %v", err)
	}
	row := draft.Assignments[0]
	if row.RoleFamily != "e2e-tester" || row.AgentDefinitionRef != "agents/e2e-tester.md" {
		t.Errorf("role/definition = %q/%q, want e2e-tester/agents/e2e-tester.md", row.RoleFamily, row.AgentDefinitionRef)
	}
	if len(row.DoneWhen) != 3 || !strings.Contains(row.DoneWhen[0], "assignment-e2e-flows") {
		t.Errorf("done_when = %v, want concrete assignment result contract", row.DoneWhen)
	}
	// E2E-USER-FLOW forces e2e-browser-testing + playwright-e2e at
	// registration; the draft pre-fills them instead of a TODO.
	if strings.Join(row.SkillRefs, ",") != "e2e-browser-testing,playwright-e2e" {
		t.Errorf("skill_refs = %v, want the schema-forced E2E skills", row.SkillRefs)
	}
	found := false
	for _, path := range row.WritePaths {
		if path == workspace {
			found = true
		}
	}
	if !found {
		t.Errorf("write_paths = %v, want the verification artifact workspace included", row.WritePaths)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "behavior-wave") {
		t.Errorf("notes should warn about the static-before-behavior gate, got: %s", joined)
	}

	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", data); err != nil {
		t.Fatalf("draft fails team-manifest schema: %v", err)
	}
	if err := team.ValidateBytes(data); err != nil {
		t.Fatalf("draft fails team manifest semantics: %v", err)
	}
}

func TestDraftManifestRejectsUnknownAssignment(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	pinPlan(t, root, state, manifestDraftFixturePlan())

	_, _, err := DraftManifest(root, state, "assignment-nope")
	if err == nil {
		t.Fatal("expected an error for an unknown assignment")
	}
	if !strings.Contains(err.Error(), "assignment-qa-static") {
		t.Errorf("error should list known assignment ids, got: %v", err)
	}
}

func TestDraftManifestRejectsConsumedAssignment(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	pinPlan(t, root, state, manifestDraftFixturePlan())
	row := state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-static"].(map[string]any)
	row["status"] = "dispatched"

	if _, _, err := DraftManifest(root, state, "assignment-qa-static"); err == nil {
		t.Fatal("expected an error for an already-dispatched assignment")
	}
}
