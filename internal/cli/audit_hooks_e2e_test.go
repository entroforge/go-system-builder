package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A valid worker-local plan must be handed to the authority control plane
// without requiring the worker to write into it.
func TestAuditHooksE2EPlanRefUsesAuthorityRoot(t *testing.T) {
	fixture := newRuntimeFixture(t)
	fixture.addAgent("agent-s9", "backend-builder", "reading", "workgroup-s9", []string{"TASK-123"})
	fixture.addTask("TASK-123", "in_progress", []string{"agent-s9"})
	fixture.addTeam("workgroup-s9", "active", []string{"agent-s9"})

	manifest := decodePlanCheckpointAsset(t, "team-manifest.example.json")
	manifest["manifest_id"] = "team-manifest-s9"
	manifest["runtime_id"] = "loop-REQ-039"
	manifest["req_id"] = "REQ-039"
	manifest["workgroup_id"] = "workgroup-s9"
	manifest["workgroup_kind"] = "builder"
	manifest["planned_agent_count"] = 1
	manifest["max_parallel_agents"] = 1
	manifest["separation_edges"] = []any{}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["assignment_id"] = "assignment-s9-unit"
	assignment["role_family"] = "backend-builder"
	assignment["agent_id"] = "agent-s9"
	manifest["assignments"] = []any{assignment}
	manifestPath := filepath.Join(fixture.root, ".claude", "workgroups", "REQ-039", "workgroup-s9", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writePlanCheckpointJSON(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), manifest)
	fixture.persist(t)

	plan := decodePlanCheckpointMessage(t)
	plan["runtime_id"] = "loop-REQ-039"
	plan["agent_id"] = "agent-s9"
	plan["task_id"] = "TASK-123"
	plan["team_id"] = "workgroup-s9"
	plan["assignment_id"] = "assignment-s9-unit"
	plan["assignment_revision"] = 1
	planPathName := filepath.Join(".claude", "plans", "plan.json")
	worker := filepath.Join(fixture.root, ".worktrees", "assignment-s9-unit")
	if err := os.MkdirAll(filepath.Dir(worker), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-qb", "audit-dev"},
		{"config", "user.name", "Audit"},
		{"config", "user.email", "audit@example.test"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "--allow-empty", "-qm", "audit baseline"},
		{"worktree", "add", "-b", "audit-worker", worker, "HEAD"},
	} {
		if out, err := runGit(t, fixture.root, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	assignment["worktree_path"] = filepath.ToSlash(filepath.Join(".worktrees", "assignment-s9-unit"))
	assignment["branch"] = "audit-worker"
	assignment["target_branch"] = "audit-dev"
	writePlanCheckpointJSON(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), manifest)
	workerPlan := filepath.Join(worker, planPathName)
	if err := os.MkdirAll(filepath.Dir(workerPlan), 0o755); err != nil {
		t.Fatal(err)
	}
	writePlanCheckpointJSON(t, filepath.Dir(workerPlan), filepath.Base(workerPlan), plan)

	payload := func(sessionID, cwd, ref string) string {
		raw, err := json.Marshal(map[string]any{
			"hook_event_name": "PostToolUse",
			"session_id":      sessionID,
			"tool_name":       "SendMessage",
			"tool_use_id":     "audit-plan-" + sessionID,
			"cwd":             cwd,
			"tool_input": map[string]any{
				"message_type":  "plan_report",
				"agent_id":      "agent-s9",
				"teammate_name": "agent-s9",
				"plan_ref":      ref,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	// The authority has a stale same-name file. The actual worker report must
	// win because the payload cwd is a registered assignment worktree.
	rootPlan := filepath.Join(fixture.root, planPathName)
	if err := os.MkdirAll(filepath.Dir(rootPlan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPlan, []byte("stale root plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"hook", "--event", "PostToolUse", "--root", fixture.root}, strings.NewReader(payload("audit-worker-plan", worker, planPathName)), &stdout, &stderr)
	if code != 0 || strings.Contains(stderr.String(), "plan_report rejected") {
		t.Fatalf("registered worker plan_ref should be accepted: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "plan_report observed for agent-s9") {
		t.Fatalf("worker plan_ref was not observed: %s", stdout.String())
	}
	var recordedState map[string]any
	stateBytes, err := os.ReadFile(filepath.Join(fixture.root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stateBytes, &recordedState); err != nil {
		t.Fatal(err)
	}
	agents := recordedState["entities"].(map[string]any)["agents"].([]any)
	recordedRef, _ := agents[0].(map[string]any)["plan_reported_ref"].(string)
	digest := sha256.Sum256(mustReadAudit(t, workerPlan))
	wantRef := filepath.ToSlash(filepath.Join(planHandoffDir, hex.EncodeToString(digest[:])+".json"))
	if recordedRef != wantRef {
		t.Fatalf("worker plan_ref was not normalized/imported: got=%q want=%q", recordedRef, wantRef)
	}
	imported, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(recordedRef)))
	if err != nil {
		t.Fatal(err)
	}
	if string(imported) != string(mustReadAudit(t, workerPlan)) {
		t.Fatalf("imported plan differs from worker bytes")
	}
	if info, err := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(recordedRef))); err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("imported plan must be frozen 0444: info=%v err=%v", info, err)
	}

	// Replaying the same Worker report is idempotent: the immutable path and
	// durable registration stay identical and no second handoff file appears.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"hook", "--event", "PostToolUse", "--root", fixture.root}, strings.NewReader(payload("audit-worker-plan-replay", worker, planPathName)), &stdout, &stderr)
	if code != 0 || strings.Contains(stderr.String(), "rejected") || strings.Contains(stderr.String(), "registration failed") {
		t.Fatalf("same worker report must be idempotent: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var replayState map[string]any
	replayBytes, err := os.ReadFile(filepath.Join(fixture.root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replayBytes, &replayState); err != nil {
		t.Fatal(err)
	}
	if got := replayState["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["plan_reported_ref"]; got != recordedRef {
		t.Fatalf("replay changed durable plan ref: got=%v want=%s", got, recordedRef)
	}

	// A different immutable report cannot replace the first durable
	// checkpoint, even when it comes from the same registered worker.
	changedPlan := filepath.Join(worker, ".claude", "plans", "changed.json")
	changed := decodePlanCheckpointMessage(t)
	changed["runtime_id"] = "loop-REQ-039"
	changed["agent_id"] = "agent-s9"
	changed["task_id"] = "TASK-123"
	changed["team_id"] = "workgroup-s9"
	changed["assignment_id"] = "assignment-s9-unit"
	changed["assignment_revision"] = 1
	changed["objective"] = "changed report"
	writePlanCheckpointJSON(t, filepath.Dir(changedPlan), filepath.Base(changedPlan), changed)
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"hook", "--event", "PostToolUse", "--root", fixture.root}, strings.NewReader(payload("audit-worker-plan-changed", worker, filepath.ToSlash(filepath.Join(".claude", "plans", "changed.json")))), &stdout, &stderr)
	if code != 0 || !strings.Contains(stderr.String(), "registration failed") || !strings.Contains(stdout.String(), "plan_report registration failed") {
		t.Fatalf("different worker report must be rejected after first registration: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}

	// An unregistered cwd is rejected even if it contains a valid plan.
	unknownWorker := t.TempDir()
	unknownPlan := filepath.Join(unknownWorker, planPathName)
	if err := os.MkdirAll(filepath.Dir(unknownPlan), 0o755); err != nil {
		t.Fatal(err)
	}
	writePlanCheckpointJSON(t, filepath.Dir(unknownPlan), filepath.Base(unknownPlan), plan)
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"hook", "--event", "PostToolUse", "--root", fixture.root}, strings.NewReader(payload("audit-unknown-worker", unknownWorker, planPathName)), &stdout, &stderr)
	if code != 0 || !strings.Contains(stderr.String(), "outside registered Assignment worker") || strings.Contains(stdout.String(), "plan_report observed for agent-s9") {
		t.Fatalf("unknown worker must be rejected: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}

	// A symlinked worker plan is rejected even when its target is valid.
	outsidePlan := filepath.Join(t.TempDir(), "outside-plan.json")
	writePlanCheckpointJSON(t, filepath.Dir(outsidePlan), filepath.Base(outsidePlan), plan)
	symlinkPlan := filepath.Join(worker, ".claude", "plans", "symlink.json")
	if err := os.Symlink(outsidePlan, symlinkPlan); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"hook", "--event", "PostToolUse", "--root", fixture.root}, strings.NewReader(payload("audit-symlink-plan", worker, filepath.ToSlash(filepath.Join(".claude", "plans", "symlink.json")))), &stdout, &stderr)
	if code != 0 || !strings.Contains(stderr.String(), "uses a symlink") || strings.Contains(stdout.String(), "plan_report observed for agent-s9") {
		t.Fatalf("symlink worker plan must be rejected: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
}

func mustReadAudit(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAuditHooksE2ERejectsSymlinkedHandoffStore(t *testing.T) {
	root := t.TempDir()
	handoffParent := filepath.Join(root, ".claude", "evidence")
	if err := os.MkdirAll(handoffParent, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(handoffParent, "plan-handoffs")); err != nil {
		t.Fatal(err)
	}
	if _, err := importPlanReportBytes(root, []byte("controlled handoff bytes")); err == nil || !strings.Contains(err.Error(), "plan handoff store rejected") {
		t.Fatalf("symlinked handoff store must be rejected: %v", err)
	}
}

func TestAuditHooksE2ERejectsAssignmentIDTraversal(t *testing.T) {
	if got := registeredAssignmentSidecarPath(t.TempDir(), "../escape"); got != "" {
		t.Fatalf("assignment traversal unexpectedly produced sidecar path %q", got)
	}
}
