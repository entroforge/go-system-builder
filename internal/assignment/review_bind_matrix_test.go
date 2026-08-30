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
)

// ---------------------------------------------------------------------------
// §14.1 Assignment-bind matrix (L3-S7 §3.4/§8, §5.2-5.3): the
// register-workgroup binding enforces the static -> behavior wave gate, the
// lens partition, the exact Claim set and dispatch-once. The binding runs
// inside the register-workgroup CAS, so a rejected bind leaves no dispatch.
// ---------------------------------------------------------------------------

// reviewBindSpec customizes the seeded ReviewPlan projection.
type reviewBindSpec struct {
	planStatus        string            // default "running"
	behaviorWave      []string          // assignment ids switched to execution_wave=behavior
	lensOverride      map[string]string // assignment id -> projection lens
	statusOverride    map[string]string // assignment id -> projection status
	claimDispositions map[string]string // claim id -> disposition
	dependencies      map[string][]string
}

// seedReviewBindPlan mirrors register_test.go's seedReviewPlan with the
// overrides the matrix needs.
func seedReviewBindPlan(t *testing.T, repoRoot string, state map[string]any, spec reviewBindSpec) {
	t.Helper()
	subjects := []any{
		map[string]any{"path": "docs/tasks/TASK-012.md", "sha256": strings.Repeat("1", 64), "kind": "task"},
	}
	claim := func(id, target string) map[string]any {
		row := map[string]any{
			"claim_id": id, "lens": "delivery", "target": target,
			"assertion": "covered", "oracle": "evidence exists", "method": "review",
			"applicability": "required", "source_refs": []any{"REQ-002"},
		}
		if deps := spec.dependencies[id]; len(deps) > 0 {
			row["depends_on"] = deps
		}
		return row
	}
	behavior := map[string]bool{}
	for _, id := range spec.behaviorWave {
		behavior[id] = true
	}
	assignmentRow := func(id string, claimIDs ...string) map[string]any {
		wave := "static"
		if behavior[id] {
			wave = "behavior"
		}
		return map[string]any{
			"assignment_id": id, "lens": "delivery", "claim_ids": claimIDs,
			"non_overlap_boundary": "owns its responsibility", "execution_wave": wave,
		}
	}
	planAssignments := []any{
		assignmentRow("assignment-ver-req-gap", "claim-dv-req-gap"),
		assignmentRow("assignment-ver-spec-gap", "claim-dv-spec-gap"),
		assignmentRow("assignment-ver-module-complete", "claim-dv-module-complete"),
		assignmentRow("assignment-ver-integration", "claim-dv-integration"),
		assignmentRow("assignment-ver-regression", "claim-dv-regression"),
	}
	plan := map[string]any{
		"schema_version": "1.0.0", "review_plan_id": "review-plan-bind-test",
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
		"assignments":                     planAssignments,
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
	planRel := ".claude/review/plans/review-plan-bind-test.json"
	planAbs := filepath.Join(repoRoot, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planAbs, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(planAbs) })
	sha := sha256.Sum256(planBytes)

	status := spec.planStatus
	if status == "" {
		status = "running"
	}
	reviewMap := map[string]any{
		"round": 1, "clean_round": nil,
		"plan": map[string]any{
			"plan_id": "review-plan-bind-test", "path": planRel,
			"sha256": fmt.Sprintf("%x", sha[:]), "revision": 1, "review_round": 1,
			"status": status, "e2e_coverage_state": "not_applicable",
			"verification_artifact_workspace": nil, "submitted_at": "2026-01-01T00:00:00Z",
		},
		"claims":            map[string]any{},
		"assignments":       map[string]any{},
		"observation_batch": nil,
	}
	claimsProjection := reviewMap["claims"].(map[string]any)
	assignmentsProjection := reviewMap["assignments"].(map[string]any)
	for _, raw := range planAssignments {
		a := raw.(map[string]any)
		id := a["assignment_id"].(string)
		lens := "delivery"
		if override, ok := spec.lensOverride[id]; ok {
			lens = override
		}
		rowStatus := "planned"
		if override, ok := spec.statusOverride[id]; ok {
			rowStatus = override
		}
		assignmentsProjection[id] = map[string]any{
			"lens": lens, "claim_ids": a["claim_ids"], "status": rowStatus,
			"agent_id": nil, "result_ref": nil,
		}
		for _, c := range a["claim_ids"].([]string) {
			disposition := "planned"
			if override, ok := spec.claimDispositions[c]; ok {
				disposition = override
			}
			claimsProjection[c] = map[string]any{
				"lens": "delivery", "applicability": "required", "disposition": disposition,
				"assignment_id": id, "result_id": nil, "finding_ids": []any{},
			}
		}
	}
	claimsProjection["claim-qa-min"] = map[string]any{
		"lens": "qa", "applicability": "not_applicable", "disposition": "not_applicable",
		"assignment_id": "", "result_id": nil, "finding_ids": []any{},
	}
	state["review"] = reviewMap
}

func registerFixtureWorkgroup(t *testing.T, root, dir string, state map[string]any, mutateManifest func(body map[string]any)) error {
	t.Helper()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeJSON(t, statePath, state)

	manifestPath := filepath.Join(dir, "manifest.json")
	if mutateManifest == nil {
		copyManifest(t, root, manifestPath)
	} else {
		data, err := os.ReadFile("testdata/delivery-manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		mutateManifest(body)
		out, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	revision, _ := state["revision"].(int)
	_, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: revision,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	})
	return err
}

// §14.1: behavior E2E 在静态 Claims 未完成前启动 —— DAG/gate 阻止。
func TestReviewBindBlocksBehaviorWaveBeforeStaticSettles(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	state := activeState(t, root, "verification", "running", 6)
	seedReviewBindPlan(t, root, state, reviewBindSpec{behaviorWave: []string{"assignment-ver-regression"}})

	err := registerFixtureWorkgroup(t, root, dir, state, nil)
	if err == nil || !strings.Contains(err.Error(), "behavior-wave") {
		t.Fatalf("behavior-wave dispatch before the static set settles must be rejected, got %v", err)
	}
}

// §14.1: DV/QA ordinary finding 后静态 Claims 已完整 disposition ——
// behavior-wave Assignment 解锁；cannot_clean 不得被误作“停止发现”。
func TestReviewBindUnlocksBehaviorWaveWithStaticFinding(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	state := activeState(t, root, "verification", "running", 6)
	seedReviewBindPlan(t, root, state, reviewBindSpec{
		planStatus:   "cannot_clean",
		behaviorWave: []string{"assignment-ver-regression"},
		claimDispositions: map[string]string{
			"claim-dv-req-gap":         "finding", // ordinary finding: settled, not a stop
			"claim-dv-spec-gap":        "pass",
			"claim-dv-module-complete": "pass",
			"claim-dv-integration":     "pass",
		},
	})

	if err := registerFixtureWorkgroup(t, root, dir, state, nil); err != nil {
		t.Fatalf("behavior-wave dispatch must unlock once the static set settled (finding counts): %v", err)
	}
}

// §14.1: 合并不同 role Lens —— workgroup kind 与 Assignment lens 不一致即
// 拒绝。
func TestReviewBindRejectsLensMismatch(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	state := activeState(t, root, "verification", "running", 6)
	seedReviewBindPlan(t, root, state, reviewBindSpec{
		lensOverride: map[string]string{"assignment-ver-req-gap": "qa"},
	})

	err := registerFixtureWorkgroup(t, root, dir, state, nil)
	if err == nil || !strings.Contains(err.Error(), "never mixes lenses") {
		t.Fatalf("lens mismatch must be rejected, got %v", err)
	}
}

// §14.1: Result 必须逐 Claim 给结论 —— manifest 绑定的 Claim set 必须与
// 计划 Assignment 完全一致。
func TestReviewBindRejectsClaimSetMismatch(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	state := activeState(t, root, "verification", "running", 6)
	seedReviewBindPlan(t, root, state, reviewBindSpec{})

	err := registerFixtureWorkgroup(t, root, dir, state, func(body map[string]any) {
		for _, raw := range body["assignments"].([]any) {
			item := raw.(map[string]any)
			if item["assignment_id"] == "assignment-ver-regression" {
				item["claim_ids"] = []any{} // drop the exact claim set
			}
		}
	})
	if err == nil || !strings.Contains(err.Error(), "assignment-ver-regression") {
		t.Fatalf("claim set mismatch must be rejected, got %v", err)
	}
}

// 一个计划 Assignment 只派发一次 —— 重复派发被拒绝，required work 只能
// queued，不能被第二份 manifest 覆盖。
func TestReviewBindRejectsRedispatch(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	state := activeState(t, root, "verification", "running", 6)
	seedReviewBindPlan(t, root, state, reviewBindSpec{
		statusOverride: map[string]string{"assignment-ver-req-gap": "dispatched"},
	})

	err := registerFixtureWorkgroup(t, root, dir, state, nil)
	if err == nil || !strings.Contains(err.Error(), "dispatches once") {
		t.Fatalf("re-dispatch of a dispatched assignment must be rejected, got %v", err)
	}
}

func TestReviewBindBlocksAssignmentUntilDependenciesSettle(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	state := activeState(t, root, "verification", "running", 6)
	seedReviewBindPlan(t, root, state, reviewBindSpec{
		dependencies: map[string][]string{
			"claim-dv-spec-gap": {"claim-dv-req-gap"},
		},
	})

	err := registerFixtureWorkgroup(t, root, dir, state, nil)
	if err == nil || !strings.Contains(err.Error(), "upstream Result") {
		t.Fatalf("dependent Assignment must wait for upstream Result consumption, got %v", err)
	}
}
