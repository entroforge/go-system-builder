package assignment_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestRegisterWorkgroupAddsTaskTeamAndReadingAgents(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 6)
	seedReviewPlan(t, root, dir, state)
	writeJSON(t, statePath, state)

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	next, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 6,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(next.State)
	if err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeBytes(root, data); err != nil {
		t.Fatalf("registration produced invalid runtime: %v", err)
	}
	entities := next.State["entities"].(map[string]any)
	if len(entities["agents"].([]any)) != 5 || len(entities["teams"].([]any)) != 1 {
		t.Fatalf("unexpected registered entities: %#v", entities)
	}
	// The ReviewPlan projection records the dispatched agents and flips the
	// covered Claims to running (L3-S7 §8 Assignment generator).
	assignmentsProjection := next.State["review"].(map[string]any)["assignments"].(map[string]any)
	row := assignmentsProjection["assignment-ver-req-gap"].(map[string]any)
	if row["status"] != "dispatched" {
		t.Fatalf("assignment-ver-req-gap status = %v, want dispatched", row["status"])
	}
	claimsProjection := next.State["review"].(map[string]any)["claims"].(map[string]any)
	if claimsProjection["claim-dv-req-gap"].(map[string]any)["disposition"] != "running" {
		t.Fatalf("claim-dv-req-gap disposition = %v, want running", claimsProjection["claim-dv-req-gap"])
	}
}

func TestRegisterWorkgroupRejectsAuthoringPlaceholderAgentID(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 6)
	seedReviewPlan(t, root, dir, state)
	writeJSON(t, statePath, state)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["assignments"].([]any)[0].(map[string]any)["agent_id"] = "TODO(planner):agent-id-for-dv"
	writeJSON(t, manifestPath, manifest)

	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 6, ManifestPath: manifestPath, TaskID: "TASK-012", TaskPath: taskPath,
	}); err == nil || !strings.Contains(err.Error(), "authoring placeholder") {
		t.Fatalf("placeholder agent_id must be rejected at registration, got %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected placeholder registration must not mutate Runtime")
	}
}

// seedReviewPlan registers a ReviewPlan pointer in the state and writes the
// pinned plan file under <repoRoot>/.claude/review/plans/ (Register resolves
// the plan path against the repository root). The five delivery assignments
// mirror testdata/delivery-manifest.json.
func seedReviewPlan(t *testing.T, repoRoot string, dir string, state map[string]any) {
	t.Helper()
	subjects := []any{
		map[string]any{"path": "docs/tasks/TASK-012.md", "sha256": strings.Repeat("1", 64), "kind": "task"},
	}
	claim := func(id, target string) map[string]any {
		return map[string]any{
			"claim_id": id, "lens": "delivery", "target": target,
			"assertion": "covered", "oracle": "evidence exists", "method": "review",
			"applicability": "required", "source_refs": []any{"REQ-002"},
		}
	}
	assignment := func(id string, claimIDs ...string) map[string]any {
		return map[string]any{
			"assignment_id": id, "lens": "delivery", "claim_ids": claimIDs,
			"non_overlap_boundary": "owns its responsibility", "execution_wave": "static",
		}
	}
	plan := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": "review-plan-register-test",
		"review_round": 1, "baseline_generation": 1,
		"frozen_subjects": subjects,
		"claims": []any{
			claim("claim-dv-req-gap", "REQ-002"),
			claim("claim-dv-spec-gap", "REQ-002"),
			claim("claim-dv-module-complete", "REQ-002"),
			claim("claim-dv-integration", "REQ-002"),
			claim("claim-dv-regression", "REQ-002"),
			map[string]any{
				"claim_id": "claim-qa-min", "lens": "qa", "target": "n/a",
				"assertion": "qa minimum", "oracle": "n/a", "method": "n/a",
				"applicability": "not_applicable", "na_rationale": "covered by a separate workgroup",
				"source_refs": []any{"REQ-002"},
			},
		},
		"assignments": []any{
			assignment("assignment-ver-req-gap", "claim-dv-req-gap"),
			assignment("assignment-ver-spec-gap", "claim-dv-spec-gap"),
			assignment("assignment-ver-module-complete", "claim-dv-module-complete"),
			assignment("assignment-ver-integration", "claim-dv-integration"),
			assignment("assignment-ver-regression", "claim-dv-regression"),
		},
		"e2e_coverage_state":              "not_applicable",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "orchestrator",
		"created_at":                      "2026-01-01T00:00:00Z",
	}
	planData, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	planBytes := append(planData, '\n')
	planRel := ".claude/review/plans/review-plan-register-test.json"
	planAbs := filepath.Join(repoRoot, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planAbs, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(planAbs) })
	sha := sha256.Sum256(planBytes)

	reviewMap := map[string]any{
		"round": 1, "clean_round": nil,
		"plan": map[string]any{
			"plan_id": "review-plan-register-test", "path": planRel,
			"sha256": fmt.Sprintf("%x", sha[:]), "revision": 1, "review_round": 1,
			"status": "running", "e2e_coverage_state": "not_applicable",
			"verification_artifact_workspace": nil, "submitted_at": "2026-01-01T00:00:00Z",
		},
		"claims":            map[string]any{},
		"assignments":       map[string]any{},
		"observation_batch": nil,
	}
	claimsProjection := reviewMap["claims"].(map[string]any)
	assignmentsProjection := reviewMap["assignments"].(map[string]any)
	for _, raw := range plan["assignments"].([]any) {
		a := raw.(map[string]any)
		assignmentsProjection[a["assignment_id"].(string)] = map[string]any{
			"lens": "delivery", "claim_ids": a["claim_ids"], "status": "planned",
			"agent_id": nil, "result_ref": nil,
		}
		for _, c := range a["claim_ids"].([]string) {
			claimsProjection[c] = map[string]any{
				"lens": "delivery", "applicability": "required", "disposition": "planned",
				"assignment_id": a["assignment_id"], "result_id": nil, "finding_ids": []any{},
			}
		}
	}
	claimsProjection["claim-qa-min"] = map[string]any{
		"lens": "qa", "applicability": "not_applicable", "disposition": "not_applicable",
		"assignment_id": "", "result_id": nil, "finding_ids": []any{},
	}
	state["review"] = reviewMap
}

func TestRegisterRepairBindingRejectsManifestOwnerDrift(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "bug_resolution", "planning", 6)
	state["review"].(map[string]any)["repair"] = map[string]any{
		"session_id":        "repair-session-owner",
		"assignment_owners": map[string]any{},
	}
	writeJSON(t, statePath, state)
	originalState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	protocol, err := os.ReadFile(filepath.Join(root, "docs", "agent-protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(protocol)
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": "team-manifest-repair-owner", "version": "1.0.0",
		"runtime_id": "loop-REQ-002", "req_id": "REQ-002", "baseline_generation": 1, "review_round": nil,
		"platform_team_id": "platform-s9-repair", "workgroup_id": "workgroup-repair-owner", "workgroup_kind": "builder", "status": "planned",
		"documents": []any{map[string]any{"id": "agent-protocol", "path": "docs/agent-protocol.md", "version": "current", "sha256": hex.EncodeToString(sum[:])}},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one bounded repair assignment",
			"assignment_ids": []string{"assignment-s9-unit-1"}, "na_rationale": nil, "evidence_ref": "docs/agent-protocol.md",
		}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-s9-unit-1", "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
			"scope": []string{"internal/api"}, "agent_id": "manifest-owner", "agent_definition_ref": "agents/backend-builder.md",
			"skill_refs": []string{"code-quality"}, "read_paths": []string{"docs/agent-protocol.md"}, "write_paths": []string{"internal/api"},
			"output_paths": []string{".claude/evidence"}, "depends_on": []string{}, "reuse_decision": "create",
			"grouping_rationale": "one Builder owns one repair assignment", "status": "planned",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "one Builder for one repair assignment", "validation": map[string]any{
			"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-08-26T00:00:00Z",
		},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, append(manifestData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(dir, "TASK-REPAIR-OWNER.md")
	if err := os.WriteFile(taskPath, []byte("# repair owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 6, ManifestPath: manifestPath, TaskID: "TASK-REPAIR-OWNER", TaskPath: taskPath,
		RepairAssignmentID: "repair-assignment-unit-1", RepairOwnerAgentID: "requested-owner",
	})
	if err == nil || !strings.Contains(err.Error(), "manifest owner manifest-owner does not match requested Agent requested-owner") {
		t.Fatalf("owner drift must be rejected at the registration boundary, got %v", err)
	}
	updated, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(updated) != string(originalState) {
		t.Fatal("rejected repair binding must not mutate Runtime")
	}
}

func TestRegisterWorkgroupCleansActivationEnvelopesWhenApplyFails(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 6)
	seedReviewPlan(t, root, dir, state)
	writeJSON(t, statePath, state)

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	// The manifest remains semantically valid: two assignments may share an
	// Agent when no separation edge forbids it. Register's Apply then reaches
	// the third row and rejects the duplicate after the first two activation
	// envelopes have already been staged.
	assignments := manifest["assignments"].([]any)
	assignments[1].(map[string]any)["agent_id"] = assignments[2].(map[string]any)["agent_id"]
	manifest["planned_agent_count"] = float64(4)
	updatedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(updatedManifest, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(dir, "TASK-013-CLEANUP.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-013-CLEANUP\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 6,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-013-CLEANUP",
		TaskPath:         taskPath,
	})
	if err == nil || !strings.Contains(err.Error(), "Agent agent-ver-module-complete is already registered") {
		t.Fatalf("expected duplicate Agent rejection after staging, got %v", err)
	}

	for _, agentID := range []string{"agent-ver-req-gap", "agent-ver-module-complete"} {
		activationPath := filepath.Join(root, ".claude", "evidence", "workgroup-delivery-round-1", "TASK-013-CLEANUP", "activation-"+agentID+".json")
		if _, statErr := os.Stat(activationPath); !os.IsNotExist(statErr) {
			t.Fatalf("failed registration left activation envelope %s (stat err=%v)", activationPath, statErr)
		}
	}
	finalState, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(finalState) == "" {
		t.Fatal("runtime state unexpectedly empty after rejected registration")
	}
}

func TestRegisterWorkgroupRejectsWrongLoopStateWithoutMutation(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "planning", "design", 2)
	writeJSON(t, statePath, state)
	before, _ := os.ReadFile(statePath)

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 2,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	}); err == nil {
		t.Fatal("expected wrong-state rejection")
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("rejected registration mutated runtime")
	}
}

func copyManifest(t *testing.T, root, target string) {
	t.Helper()
	data, err := os.ReadFile("testdata/delivery-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func activeState(t *testing.T, root, state string, phase any, revision int) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["revision"] = revision
	value["runtime_id"] = "loop-REQ-002"
	value["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}}
	value["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	lifecycle := value["lifecycle"].(map[string]any)
	lifecycle["state"] = state
	lifecycle["phase"] = phase
	return value
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
