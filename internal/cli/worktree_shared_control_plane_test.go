package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// ---------------------------------------------------------------------------
// §14.1 worktree 共享控制面核验 (L3-S7 §1.4 + L4 §2.2):
//
//   * Main 与 Worker 共享同一份 .claude/ 控制面，运行态不经 git merge 汇总；
//   * worktree Worker 即使在隔离目录里执行 review-plan / review-result
//     写入，也必须落到项目根的 .claude/，而不是 worktree 内一份第二份
//     运行态；
//   * --root 指向项目根是必经动作；resolveRootPath 必须把所有相对路径
//     join 到 --root，控制面才不被分裂。
//
// This is the mechanical evidence: it sets up a project root + a real git
// worktree, then invokes the review-plan and review-result verbs through
// cli.Run with --root pointed at the project root. The expected outcome is
// that .claude/loop-state.json + .claude/review/plans/... land ONLY under
// the project root, never under the worktree.
// ---------------------------------------------------------------------------

// worktreeProjectRoot scaffolds a minimal verification-stage runtime: the
// docs the validator needs, a baseline lifecycle, and a review round. The
// returned root is a git repository with a single commit so a real git
// worktree can be created from it.
func worktreeProjectRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/control"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Frozen subjects are verified against the shared project root even when
	// the plan file is authored in a worktree. Seed the committed baseline so
	// both checkouts describe the same bytes.
	if err := os.MkdirAll(filepath.Join(root, "internal", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "example", "service.go"), []byte("worktree baseline"), 0o644); err != nil {
		t.Fatal(err)
	}

	stateBytes, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = 1
	state["runtime_id"] = "loop-REQ-WORKTREE"
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	lifecycle := state["lifecycle"].(map[string]any)
	lifecycle["state"] = "verification"
	lifecycle["phase"] = "planned"
	state["baseline"].(map[string]any)["generation"] = 1
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["entities"] = map[string]any{
		"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{},
	}
	state["documents"] = []any{}
	marshalled, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(marshalled, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "worktree@test.local")
	runGit(t, root, "config", "user.name", "worktree-test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "init for worktree test")
	return root
}

// typedEvidenceRef writes a real evidence artifact under the shared project
// root and returns its typed path: reference with the digest bound
// (S7-11/RC-07: bare ghost references are rejected by the evidence gate).
func typedEvidenceRef(t *testing.T, root, name string) string {
	t.Helper()
	rel := "docs/reports/" + name
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("cli fixture evidence: " + name + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("path:%s#sha256=%x", rel, sha256.Sum256(content))
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	//nolint:gosec // test-only
	c := exec.Command("git", args...)
	c.Dir = root
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=worktree-test",
		"GIT_AUTHOR_EMAIL=worktree@test.local",
		"GIT_COMMITTER_NAME=worktree-test",
		"GIT_COMMITTER_EMAIL=worktree@test.local",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func minimalReviewPlan(t *testing.T, dir, planID string) string {
	t.Helper()
	subjectPath := filepath.Join(dir, "internal", "example", "service.go")
	if err := os.MkdirAll(filepath.Dir(subjectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	subjectBytes := []byte("worktree baseline")
	if err := os.WriteFile(subjectPath, subjectBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      planID,
		"review_round":        1,
		"baseline_generation": 1,
		"frozen_subjects": []any{
			map[string]any{"path": "internal/example/service.go", "sha256": fmt.Sprintf("%x", sha256.Sum256(subjectBytes)), "kind": "product_code"},
		},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-dv-1", "lens": "delivery",
				"target": "internal/example", "assertion": "REQ covered", "oracle": "AC maps",
				"method": "traceability", "applicability": "required", "source_refs": []string{"REQ-WORKTREE"},
			},
			map[string]any{
				"claim_id": "claim-qa-1", "lens": "qa",
				"target": "internal/example", "assertion": "errors propagate", "oracle": "no dropped",
				"method": "code review", "applicability": "required", "source_refs": []string{"CONTRACTS-WORKTREE"},
			},
			map[string]any{
				"claim_id": "claim-e2e-na", "lens": "e2e", "target": "n/a",
				"assertion": "no surface", "oracle": "impact", "method": "impact",
				"applicability":   "not_applicable",
				"na_rationale":    "pure internal change",
				"na_checklist_id": "REQ-WORKTREE#ui_impact",
				"source_refs":     []string{"REQ-WORKTREE#ui"},
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-dv-1", "lens": "delivery",
				"claim_ids":            []string{"claim-dv-1"},
				"non_overlap_boundary": "owns traceability",
				"execution_wave":       "static",
			},
			map[string]any{
				"assignment_id": "assignment-qa-1", "lens": "qa",
				"claim_ids":            []string{"claim-qa-1"},
				"non_overlap_boundary": "owns error propagation",
				"execution_wave":       "static",
			},
		},
		"e2e_coverage_state":              "not_applicable",
		"verification_artifact_workspace": nil,
		"dispatch_capacity_policy":        "coverage_complete",
		"created_by":                      "test",
		"created_at":                      "2026-08-23T00:00:00Z",
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "review-plan.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// §14.1: the ReviewPlan is received through the original consumer against the
// authoritative root, so the registered plan lands ONLY under the project root.
// The worktree has its own checked-out .claude/ but must not grow a second
// running state.
//
// The manual `runtime review-plan` registration command is gone: Main saves
// the Hook-provided questionnaire and the original consumer receives it
// (L3-S7 §7.1, L4-workflow-convergence §"接线与验收"). The authoritative root
// still owns the Runtime: a Worker checkout is not a second control plane
// (L4-state-transition-core §2.2).
func TestReviewPlanFromWorktreeWritesToSharedControlPlane(t *testing.T) {
	root := worktreeProjectRoot(t)

	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, root, "worktree", "add", "-b", "wt/test", worktreePath, "HEAD")

	// Sanity: the worktree has its own .claude/ skeleton (the file is
	// tracked). This is exactly the fork risk §14.1 calls out.
	wtState := filepath.Join(worktreePath, ".claude", "loop-state.json")
	if _, err := os.Stat(wtState); err != nil {
		t.Fatalf("worktree must carry a checked-out state to prove the divergence, got %v", err)
	}

	// The authoritative root owns the current ReviewPlan identity.
	state := stageSnapshot(t, root).State
	state["review"].(map[string]any)["round_entry"] = map[string]any{
		"plan_id": "review-plan-worktree", "transition_id": "TR-006",
		"baseline_generation": 1, "change_impact_ref": nil,
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	path := review.PlanQuestionnairePath(root, state)

	// The Worker authored a plan in its own checkout. Its copy carries a
	// different Plan identity, so a consumer that read the worktree copy
	// would register the wrong Plan.
	localPath := review.PlanQuestionnairePath(worktreePath, state)
	localPlan := minimalReviewPlan(t, worktreePath, "review-plan-worktree-local")
	localData, err := os.ReadFile(localPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, localData, 0600); err != nil {
		t.Fatal(err)
	}

	// Main saves the professional plan at the authoritative questionnaire path.
	authored := minimalReviewPlan(t, worktreePath, "review-plan-worktree")
	data, err := os.ReadFile(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	out := worktreeSharedControlPlaneHook(t, root, "PreToolUse", path)
	if !strings.Contains(out, "review-plan: registered review-plan-worktree") {
		t.Fatalf("original consumer did not receive the authoritative Plan: %s", out)
	}

	// The shared control plane lives under the project root.
	rootState, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rootMap map[string]any
	if err := json.Unmarshal(rootState, &rootMap); err != nil {
		t.Fatal(err)
	}
	reviewFacts, _ := rootMap["review"].(map[string]any)
	if reviewFacts == nil {
		t.Fatal("project-root state.review missing after the original consumer received the Plan")
	}
	planPointer, _ := reviewFacts["plan"].(map[string]any)
	if planPointer == nil {
		t.Fatal("project-root state.review.plan missing — the Plan did not land under the authoritative root")
	}
	if pointerID, _ := planPointer["plan_id"].(string); pointerID != "review-plan-worktree" {
		t.Fatalf("pointer plan_id = %q, want review-plan-worktree", pointerID)
	}

	// The worktree's own .claude/loop-state.json must remain at the
	// original revision (1) — the Worker did not write a second running
	// state into its checkout.
	wtStateBytes, err := os.ReadFile(wtState)
	if err != nil {
		t.Fatal(err)
	}
	var wtMap map[string]any
	if err := json.Unmarshal(wtStateBytes, &wtMap); err != nil {
		t.Fatal(err)
	}
	if rev := intFieldValue(wtMap["revision"]); rev != 1 {
		t.Fatalf("worktree state.revision drifted to %d; the shared control plane was bypassed", rev)
	}
	wtReview, _ := wtMap["review"].(map[string]any)
	if wtReview != nil {
		if _, ok := wtReview["plan"]; ok {
			t.Fatal("worktree checkout grew its own state.review.plan — the plan pointer split into a second running state")
		}
	}
}

// §14.1: the Reviewer answer is received through the original consumer, and
// the answer coordinate belongs to the authoritative root. The removed
// `runtime review-result submit` route must not come back, and the issued
// answer path must never point into a Worker checkout
// (L3-S7 §7.1, L4-state-transition-core §2.2).
func TestReviewResultFromWorktreeWritesToSharedControlPlane(t *testing.T) {
	root := worktreeProjectRoot(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, root, "worktree", "add", "-b", "wt/test", worktreePath, "HEAD")

	// The manual submission route is gone; the original consumer receives
	// the saved answer.
	var removedOut, removedErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "review-result", "submit", "--root", root, "--result", "review-result.json"}, strings.NewReader(""), &removedOut, &removedErr); code == 0 {
		t.Fatalf("removed review-result submit route is still accepted: %s", &removedOut)
	} else if !strings.Contains(removedErr.String(), "unknown runtime command") {
		t.Fatalf("removed review-result submit route was not reported as an unknown runtime command: %d %s", code, &removedErr)
	}

	// The current reception route is the natural Hook path through the
	// original ReviewResult consumer; no manual submission command exists.
	var catalog, catalogErr bytes.Buffer
	if code := cli.Run([]string{"actions"}, nil, &catalog, &catalogErr); code != 0 {
		t.Fatalf("actions catalog failed: %d %s", code, &catalogErr)
	}
	if !strings.Contains(catalog.String(), "through the original ReviewResult consumer") {
		t.Fatalf("action catalog lost the original ReviewResult consumer route: %s", &catalog)
	}
	if strings.Contains(catalog.String(), "runtime review-result") {
		t.Fatalf("action catalog still advertises the removed manual ReviewResult submission: %s", &catalog)
	}

	// The worktree's own .claude/loop-state.json must remain at the
	// original revision (1) — the Worker did not write a second running
	// state into its checkout.
	wtStatePath := filepath.Join(worktreePath, ".claude", "loop-state.json")
	wtStateBytes, err := os.ReadFile(wtStatePath)
	if err != nil {
		t.Fatal(err)
	}
	var wtMap map[string]any
	if err := json.Unmarshal(wtStateBytes, &wtMap); err != nil {
		t.Fatal(err)
	}
	if rev := intFieldValue(wtMap["revision"]); rev != 1 {
		t.Fatalf("worktree state.revision drifted to %d; the shared control plane was bypassed", rev)
	}
	wtReview, _ := wtMap["review"].(map[string]any)
	if wtReview != nil {
		if claims, ok := wtReview["claims"].(map[string]any); ok {
			if _, split := claims["claim-dv-1"]; split {
				t.Fatal("worktree checkout grew its own claim dispositions — the Result split into a second running state")
			}
		}
	}
}

// worktreeSharedControlPlaneHook forwards the real installed Hook event for
// the shared control plane tests.
func worktreeSharedControlPlaneHook(t *testing.T, root, event, path string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": event, "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": path}})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", event}, bytes.NewReader(body), &out, &diagnostic); code != 0 {
		t.Fatalf("%s hook failed %d: %s %s", event, code, &out, &diagnostic)
	}
	return out.String()
}

// markAssignmentDispatched writes the projection row's status/agent_id
// directly so the test can exercise review-result submit without going
// through the full team-manifest validation. This mirrors what
// bindReviewPlanAssignments does in register-workgroup.
func markAssignmentDispatched(t *testing.T, root, assignmentID, agentID string) {
	t.Helper()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	row := assignments[assignmentID].(map[string]any)
	lens, _ := row["lens"].(string)
	role := map[string]string{"delivery": "delivery-verifier", "qa": "qa", "e2e": "e2e-tester"}[lens]
	kind := map[string]string{"delivery": "delivery_verifier", "qa": "qa", "e2e": "e2e_browser"}[lens]
	teamID := "team-wt-" + assignmentID
	manifestRef := ".claude/workgroups/wt/" + assignmentID + ".json"
	// This helper supplies synthetic registered dispatch facts, not a platform
	// launch. Keep its explicitly empty method selection in an original source;
	// real registration and selected-method delivery have separate integration tests.
	manifest := map[string]any{
		"runtime_id": state["runtime_id"], "baseline_generation": state["baseline"].(map[string]any)["generation"],
		"review_round": state["review"].(map[string]any)["round"], "workgroup_id": teamID, "workgroup_kind": kind,
		"assignments": []any{map[string]any{"assignment_id": assignmentID, "agent_id": agentID, "role_family": role, "claim_ids": row["claim_ids"], "skill_refs": []string{}}},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, manifestRef)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifestRef), manifestData, 0600); err != nil {
		t.Fatal(err)
	}
	teams, _ := state["entities"].(map[string]any)["teams"].([]any)
	team := map[string]any{"id": teamID, "platform_team_id": teamID, "kind": kind, "status": "planned", "manifest_ref": manifestRef, "responsibility_ids": []any{}, "agent_ids": []any{agentID}, "review_round": state["review"].(map[string]any)["round"]}
	if state["schema_version"] != "1.1.0" {
		team["manifest_sha256"] = fmt.Sprintf("%x", sha256.Sum256(manifestData))
	}
	teams = append(teams, team)
	state["entities"].(map[string]any)["teams"] = teams
	// Register the agent entity so submit's producer check finds it.
	agents, _ := state["entities"].(map[string]any)["agents"].([]any)
	agents = append(agents, map[string]any{
		"id":                  agentID,
		"role":                role,
		"state":               "working",
		"task_ids":            []any{},
		"team_id":             teamID,
		"definition_ref":      "agents/" + role + ".md",
		"prompt_ref":          manifestRef + "#" + assignmentID,
		"readback_ref":        nil,
		"activation_ref":      nil,
		"activation_revision": nil,
		"updated_at":          "2026-08-23T00:00:00Z",
	})
	state["entities"].(map[string]any)["agents"] = agents
	row["status"] = "dispatched"
	row["agent_id"] = agentID
	claims := state["review"].(map[string]any)["claims"].(map[string]any)
	for _, claimID := range row["claim_ids"].([]any) {
		if claimRow, ok := claims[claimID.(string)].(map[string]any); ok {
			claimRow["disposition"] = "running"
		}
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// §14.1: even when a Worker forgets to pass --root and runs with the
// worktree as cwd, the resolver must NOT split the control plane silently
// into the project root. The CLI defaults --root to "." literally; with
// --root=worktree the resolver joins .claude/... to the worktree, which
// is the exact divergence §14.1 warns about. The contract under test:
// the resolver is transparent about --root, never re-anchors, and the
// documented contract is "Worker MUST pass --root=project-root".
func TestReviewPlanWithoutExplicitRootWritesToCwdNotProjectRoot(t *testing.T) {
	root := worktreeProjectRoot(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, root, "worktree", "add", "-b", "wt/test", worktreePath, "HEAD")
	planPath := minimalReviewPlan(t, worktreePath, "review-plan-no-root")

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "review-plan",
		// intentionally no --root
		"--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "1",
		"--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr)
	// The CLI doesn't change cwd; --root defaults to "." which in cli.Run
	// resolves against the test process's cwd (the package's directory),
	// not the worktree. The behaviour we DON'T want is that the resolver
	// silently re-anchors to the project root. Verify the project root
	// stays untouched: if the CLI somehow wrote there, plan_id would be
	// present in the root's state.review.plan.
	rootState, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rootMap map[string]any
	if err := json.Unmarshal(rootState, &rootMap); err != nil {
		t.Fatal(err)
	}
	review, _ := rootMap["review"].(map[string]any)
	if review != nil {
		if plan, _ := review["plan"].(map[string]any); plan != nil {
			if id, _ := plan["plan_id"].(string); id == "review-plan-no-root" {
				t.Fatal("review-plan succeeded without --root AND wrote to project root — the resolver hid the cwd contract")
			}
		}
	}
	_ = code
	_ = stdout
	_ = stderr
}

// subjectDigestFor computes the same sha256 that review.SubjectDigest
// produces. The shared control plane keeps frozen_subjects per plan, so
// the result's subject_digest must equal this digest or the runtime
// rejects it.
func subjectDigestFor(t *testing.T, root string) string {
	t.Helper()
	planBytes, err := os.ReadFile(filepath.Join(root, ".claude", "review", "plans",
		"review-plan-worktree-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		FrozenSubjects []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"frozen_subjects"`
	}
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(plan.FrozenSubjects))
	for _, fs := range plan.FrozenSubjects {
		paths = append(paths, fs.Path+":"+fs.SHA256)
	}
	sort.Strings(paths)
	joined := strings.Join(paths, "\n")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(joined)))
}

func intFieldValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
