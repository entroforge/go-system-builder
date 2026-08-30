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

	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
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

// §14.1: review-plan registered from a worktree with --root pointed at
// the project root → .claude/loop-state.json + .claude/review/plans/...
// land ONLY under the project root. The worktree has its own checked-out
// .claude/ but no plan / state mutation occurs there.
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

	// Write the plan in the worktree (simulating the Worker authoring
	// it in its own checkout) and run review-plan with --root=root.
	planPath := minimalReviewPlan(t, worktreePath, "review-plan-worktree")

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "review-plan",
		"--root", root,
		"--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "1",
		"--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review-plan from worktree failed: code=%d stderr=%s stdout=%s",
			code, stderr.String(), stdout.String())
	}

	// The shared control plane lives under the project root.
	planFile := filepath.Join(root, ".claude", "review", "plans", "review-plan-worktree.json")
	if _, err := os.Stat(planFile); err != nil {
		t.Fatalf("shared plan file missing under project root: %v", err)
	}
	rootState, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rootMap map[string]any
	if err := json.Unmarshal(rootState, &rootMap); err != nil {
		t.Fatal(err)
	}
	review, _ := rootMap["review"].(map[string]any)
	if review == nil {
		t.Fatal("project-root state.review missing after worktree-driven register")
	}
	planPointer, _ := review["plan"].(map[string]any)
	if planPointer == nil {
		t.Fatal("project-root state.review.plan missing — register did not pin under root")
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

// §14.1: review-result submit from a worktree with --root pointed at the
// project root → the consumed Result row lands under root/.claude/,
// NEVER under the worktree's .claude/.
func TestReviewResultFromWorktreeWritesToSharedControlPlane(t *testing.T) {
	root := worktreeProjectRoot(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, root, "worktree", "add", "-b", "wt/test", worktreePath, "HEAD")

	// Register a plan under the project root first.
	planPath := minimalReviewPlan(t, worktreePath, "review-plan-worktree-result")
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"runtime", "review-plan",
		"--root", root,
		"--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "1",
		"--file", planPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("register plan: code=%d stderr=%s", code, stderr.String())
	}

	// Simulate the Worker dispatching the DV Assignment. We hand-update
	// the shared state — this is exactly what runtime register-workgroup
	// would do, but we're testing the control plane, not the dispatch
	// validator (that's covered in internal/assignment/review_bind_lock_test.go).
	markAssignmentDispatched(t, root, "assignment-dv-1", "agent-wt")

	// Now simulate writing the Result in the worktree and submitting
	// through the shared control plane.
	digest := subjectDigestFor(t, root)
	result := map[string]any{
		"schema_version":      "1.0.0",
		"result_id":           "review-result-worktree",
		"assignment_id":       "assignment-dv-1",
		"assignment_revision": 1,
		"review_plan_id":      "review-plan-worktree-result",
		"review_round":        1,
		"baseline_generation": 1,
		"producer_agent_id":   "agent-wt",
		"subject_digest":      digest,
		"claim_results": []any{
			map[string]any{
				"claim_id":      "claim-dv-1",
				"conclusion":    "pass",
				"observed":      "trace passes",
				"evidence_refs": []string{typedEvidenceRef(t, root, "dv.md")},
			},
		},
		"verdict": "pass",
	}
	resultBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wtResultPath := filepath.Join(worktreePath, "review-result.json")
	if err := os.WriteFile(wtResultPath, append(resultBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"runtime", "review-result", "submit",
		"--root", root,
		"--state", ".claude/loop-state.json",
		"--journal", ".claude/loop-events.jsonl",
		"--expected-revision", "2",
		"--assignment-id", "assignment-dv-1",
		"--result", wtResultPath,
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("review-result submit from worktree: code=%d stderr=%s stdout=%s",
			code, stderr.String(), stdout.String())
	}

	// The Result row + claim disposition must live under the project
	// root state, never under the worktree's.
	rootState, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rootMap map[string]any
	if err := json.Unmarshal(rootState, &rootMap); err != nil {
		t.Fatal(err)
	}
	rootReview := rootMap["review"].(map[string]any)
	rootClaims := rootReview["claims"].(map[string]any)
	dv := rootClaims["claim-dv-1"].(map[string]any)
	if dv["disposition"] != "pass" {
		t.Fatalf("claim-dv-1 disposition = %v, want pass (Result must land under project root)", dv["disposition"])
	}

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
	// Register the agent entity so submit's producer check finds it.
	agents, _ := state["entities"].(map[string]any)["agents"].([]any)
	agents = append(agents, map[string]any{
		"id":                  agentID,
		"role":                "delivery-verifier",
		"state":               "working",
		"task_ids":            []any{},
		"team_id":             "team-wt-test",
		"definition_ref":      "agents/delivery.md",
		"prompt_ref":          ".claude/workgroups/wt/m.json#" + agentID,
		"readback_ref":        nil,
		"activation_ref":      nil,
		"activation_revision": nil,
		"updated_at":          "2026-08-23T00:00:00Z",
	})
	state["entities"].(map[string]any)["agents"] = agents
	assignments := state["review"].(map[string]any)["assignments"].(map[string]any)
	row := assignments[assignmentID].(map[string]any)
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
