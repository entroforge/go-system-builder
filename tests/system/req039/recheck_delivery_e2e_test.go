package req039_test

// This file is intentionally independent of branch_delivery_regression_test.go.
// It drives the dispatch lifecycle through the real plan checkpoint paths: one
// worker is activated by PostToolUse(SendMessage), the other by the explicit
// runtime agent-begin recovery verb. No Agent row is hand-edited to working.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRecheckDeliveryRealActivationAndSuccessorRelease(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	registerRecheckTask(t, root, "shared", "TASK-042-01", "packages/validation/source", "packages/validation/generated/result.json")
	registerRecheckTask(t, root, "page", "TASK-042-02", "web/pages/source", "web/pages/generated/result.json")

	assertRecheckRegistration(t, root, "TASK-042-04", false)

	// The first worker follows the platform's actual PostToolUse(SendMessage)
	// path. Its plan is written inside the linked checkout and imported into the
	// authority before the auto-chain advances reading -> activated -> working.
	first := runRecheckWorker(t, root, recheckWorkerSpec{
		suffix:     "shared",
		taskID:     "TASK-042-01",
		agentID:    "builder-audit-shared",
		assignment: "assignment-audit-shared",
		output:     "packages/validation/generated/result.json",
		activation: "hook",
		integrate:  true,
	})
	assertRecheckRegistration(t, root, "TASK-042-04", false)

	// The second worker deliberately uses the explicit agent-begin recovery
	// command. This exercises the same hash-bound activation chain after a
	// transport/Hook gap, still without mutating Agent state by hand.
	_ = runRecheckWorker(t, root, recheckWorkerSpec{
		suffix:     "page",
		taskID:     "TASK-042-02",
		agentID:    "builder-audit-page",
		assignment: "assignment-audit-page",
		output:     "web/pages/generated/result.json",
		activation: "agent-begin",
		integrate:  true,
	})

	assertRecheckRegistration(t, root, "TASK-042-04", true)

	// A new canonical Result after cleanup must revoke successor eligibility.
	// The durable checkpoint is then re-verified against the new bytes without
	// creating a second merge or recreating the removed worktree.
	beforeResubmit := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	writeRecheckCompletion(t, first.messagePath, first.agentID, first.taskID, first.output)
	runRecheckCLI(t, root, "runtime", "task-complete", "--agent-id", first.agentID, "--message", first.messagePath)
	assertRecheckRegistration(t, root, "TASK-042-04", false)
	runRecheckTaskIntegrate(t, root, first.assignment)
	if after := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); after != beforeResubmit {
		t.Fatalf("resubmitted Result repeated the root merge: before=%s after=%s", beforeResubmit, after)
	}
	assertRecheckRegistration(t, root, "TASK-042-04", true)

	// The dependency gate is now open for the real successor registration.
	manifest := s4AuditManifest(t, root, "successor", "TASK-042-04", "web/features", "web/features/generated/result.json")
	if code, out, errOut := s4AuditRegister(t, root, manifest, "TASK-042-04"); code != 0 {
		t.Fatalf("successor registration failed after both verified integrations: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
}

// Inspect executes RequiredChecks against the worker checkout before the
// branch is merged, while Integrate executes them again after the merge
// against the authority root. A check that requires a newly delivered output
// therefore proves the complete pre- and post-merge delivery path.
func TestRecheckRequiredCheckRunsAgainstWorkerBeforeMerge(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	registerRecheckTaskWithCheck(t, root, "strict", "TASK-042-01", "packages/validation/source", "packages/validation/generated/result.json")
	result := runRecheckWorker(t, root, recheckWorkerSpec{
		suffix:     "strict",
		taskID:     "TASK-042-01",
		agentID:    "builder-audit-strict",
		assignment: "assignment-audit-strict",
		output:     "packages/validation/generated/result.json",
		activation: "agent-begin",
	})
	code, stdout, stderr := runTaskIntegrate(t, root, result.assignment)
	combined := stdout + stderr
	var outcome struct {
		State   string `json:"state"`
		Blocked bool   `json:"blocked"`
	}
	decodeErr := json.Unmarshal([]byte(stdout), &outcome)
	if code != 0 || decodeErr != nil || outcome.State != "integrated" || outcome.Blocked || strings.Contains(combined, "required check failed") {
		_, rootErr := os.Stat(filepath.Join(root, filepath.FromSlash(result.output)))
		_, workerErr := os.Stat(filepath.Join(root, ".worktrees", result.assignment, filepath.FromSlash(result.output)))
		state := readSystemState(t, root)
		t.Logf("pre-merge check evidence: authority output err=%v, preserved worker output err=%v, milestone=%#v", rootErr, workerErr, state["milestone"])
		t.Fatalf("required check must run against the delivered worker before merge and the authority root after merge: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.output))); err != nil {
		t.Fatalf("strict-check delivery did not reach authority root: %v", err)
	}
}

type recheckWorkerSpec struct {
	suffix, taskID, agentID, assignment, output, activation string
	integrate                                               bool
}

type recheckWorkerResult struct {
	agentID, taskID, assignment, output, messagePath string
}

func registerRecheckTask(t *testing.T, root, suffix, taskID, write, output string) {
	t.Helper()
	registerRecheckTaskWithCheck(t, root, suffix, taskID, write, output)
}

func registerRecheckTaskWithCheck(t *testing.T, root, suffix, taskID, write, output string) {
	t.Helper()
	manifest := s4AuditManifest(t, root, suffix, taskID, write, output)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	assignment := body["assignments"].([]any)[0].(map[string]any)
	checks := "test -s " + output
	assignment["required_checks"] = []string{"git rev-parse --verify HEAD", checks}
	data, err = json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, ".claude", "workgroups", "REQ-039", taskID, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(manifest, canonical); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := s4AuditRegister(t, root, canonical, taskID); code != 0 {
		t.Fatalf("register %s: code=%d stdout=%s stderr=%s", taskID, code, out, errOut)
	}
}

func runRecheckWorker(t *testing.T, root string, spec recheckWorkerSpec) recheckWorkerResult {
	t.Helper()
	runRecheckCLI(t, root, "runtime", "worktree-create", "--assignment-id", spec.assignment)
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	worker := ""
	for _, row := range loaded.Assignments {
		if row.AssignmentID == spec.assignment {
			worker = row.WorktreePath
			if !containsRecheckPath(row.WritePaths, spec.output) {
				t.Fatalf("assignment %s omitted output path from effective write union: %#v", spec.assignment, row.WritePaths)
			}
		}
	}
	if worker == "" {
		t.Fatalf("assignment %s has no real worktree after worktree-create", spec.assignment)
	}

	planPath := filepath.Join(worker, ".claude", "recheck-plan.json")
	writeRecheckPlan(t, planPath, spec)
	if spec.activation == "hook" {
		payload, err := json.Marshal(map[string]any{
			"hook_event_name": "PostToolUse",
			"session_id":      "recheck-plan-" + spec.suffix,
			"tool_name":       "SendMessage",
			"cwd":             worker,
			"tool_input": map[string]any{
				"message_type":  "plan_report",
				"agent_id":      spec.agentID,
				"teammate_name": spec.agentID,
				"plan_ref":      ".claude/recheck-plan.json",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runHook(t, root, "PostToolUse", string(payload))
		if code != 0 || !strings.Contains(stdout, "plan_report observed") || !strings.Contains(stderr, "auto-chain") {
			t.Fatalf("real PostToolUse plan activation failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
	} else {
		runRecheckCLI(t, root, "runtime", "agent-begin", "--agent-id", spec.agentID,
			"--plan", planPath)
	}
	assertRecheckAgentState(t, root, spec.agentID, "working")
	assertRecheckActivation(t, root, spec.agentID, spec.output)

	target := filepath.Join(worker, filepath.FromSlash(spec.output))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(fmt.Sprintf("result for %s\n", spec.taskID)), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, worker, "add", spec.output)
	runGitIn(t, worker, "commit", "-m", "deliver declared output through real worker")

	messagePath := filepath.Join(root, ".claude", "messages", spec.agentID+".json")
	writeRecheckCompletion(t, messagePath, spec.agentID, spec.taskID, spec.output)
	runRecheckCLI(t, root, "runtime", "task-complete", "--agent-id", spec.agentID, "--message", messagePath)
	assertRecheckAgentState(t, root, spec.agentID, "reported")
	state := readSystemState(t, root)
	ref := completionTaskRef(t, state, spec.taskID)
	loaded, err = hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range loaded.Assignments {
		if row.AssignmentID == spec.assignment && row.CompletionRef != ref {
			t.Fatalf("canonical Result split for %s: assignment=%q task=%q", spec.assignment, row.CompletionRef, ref)
		}
	}
	result := recheckWorkerResult{agentID: spec.agentID, taskID: spec.taskID, assignment: spec.assignment, output: spec.output, messagePath: messagePath}
	if !spec.integrate {
		return result
	}
	runRecheckTaskIntegrate(t, root, spec.assignment)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(spec.output))); err != nil {
		t.Fatalf("integrated output %s missing from authority root: %v", spec.output, err)
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("completed assignment %s left worktree behind: %v", spec.assignment, err)
	}
	checkpoint := filepath.Join(root, ".claude", "evidence", "loop-system-test", "g1", "worktree", spec.assignment, "checkpoint.json")
	data, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatalf("checkpoint for %s missing: %v", spec.assignment, err)
	}
	var cp map[string]any
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	if cp["state"] != "complete" || cp["completion_report_path"] != ref {
		t.Fatalf("checkpoint for %s did not close against canonical Result: %#v", spec.assignment, cp)
	}
	return result
}

func writeRecheckPlan(t *testing.T, path string, spec recheckWorkerSpec) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	for _, candidate := range messages {
		if candidate["message_type"] == "plan_report" {
			plan = candidate
			break
		}
	}
	if plan == nil {
		t.Fatal("agent-message examples contain no plan_report")
	}
	plan["message_id"] = "msg-recheck-plan-" + spec.suffix
	plan["correlation_id"] = "corr-recheck-plan-" + spec.suffix
	plan["runtime_id"] = "loop-system-test"
	plan["agent_id"] = spec.agentID
	plan["task_id"] = spec.taskID
	plan["team_id"] = "workgroup-audit-" + spec.suffix
	plan["assignment_id"] = spec.assignment
	plan["assignment_revision"] = 1
	plan["objective"] = "deliver one reviewed output through the S6 worktree"
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRecheckCompletion(t *testing.T, path, agentID, taskID, changedPath string) {
	t.Helper()
	writeAuditCompletionMessage(t, path, agentID, taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	body["changed_paths"] = []string{changedPath}
	data, err = json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runRecheckCLI(t *testing.T, root string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args = append(args, "--root", root)
	if code := runCLI(t, args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("CLI %v failed: code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
	}
	return stdout.String() + stderr.String()
}

func runRecheckTaskIntegrate(t *testing.T, root, assignment string) {
	t.Helper()
	code, stdout, stderr := runTaskIntegrate(t, root, assignment)
	if code != 0 || !strings.Contains(stderr, "integrated") {
		t.Fatalf("task-integrate %s failed: code=%d stdout=%s stderr=%s", assignment, code, stdout, stderr)
	}
}

func assertRecheckAgentState(t *testing.T, root, agentID, want string) {
	t.Helper()
	state := readSystemState(t, root)
	entities := state["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == agentID {
			if row["state"] != want {
				t.Fatalf("Agent %s state=%v, want %s", agentID, row["state"], want)
			}
			return
		}
	}
	t.Fatalf("Agent %s missing", agentID)
}

func assertRecheckActivation(t *testing.T, root, agentID, output string) {
	t.Helper()
	state := readSystemState(t, root)
	entities := state["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] != agentID {
			continue
		}
		readback, _ := row["readback_ref"].(string)
		if readback == "" {
			t.Fatalf("Agent %s reached working without a durable readback_ref", agentID)
		}
		activationRef, _ := row["activation_ref"].(string)
		if activationRef == "" {
			t.Fatalf("Agent %s reached working without an activation_ref", agentID)
		}
		if revision, ok := row["activation_revision"].(float64); !ok || revision < 0 {
			t.Fatalf("Agent %s has no activation revision: %#v", agentID, row["activation_revision"])
		}
		path := activationRef
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Agent %s activation envelope %q unreadable: %v", agentID, activationRef, err)
		}
		var envelope struct {
			AgentID           string   `json:"agent_id"`
			AllowedWritePaths []string `json:"allowed_write_paths"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("Agent %s activation envelope is invalid JSON: %v", agentID, err)
		}
		if envelope.AgentID != agentID || !containsRecheckPath(envelope.AllowedWritePaths, output) {
			t.Fatalf("Agent %s activation envelope is not bound to the requested output: %#v", agentID, envelope)
		}
		return
	}
	t.Fatalf("Agent %s missing", agentID)
}

func assertRecheckRegistration(t *testing.T, root, taskID string, wantReady bool) {
	t.Helper()
	err := dispatch.CheckRegistration(root, readSystemState(t, root), taskID)
	if (err == nil) != wantReady {
		t.Fatalf("TASK %s ready=%v, want %v; err=%v", taskID, err == nil, wantReady, err)
	}
}

func containsRecheckPath(paths []string, want string) bool {
	for _, path := range paths {
		if filepath.ToSlash(filepath.Clean(path)) == filepath.ToSlash(filepath.Clean(want)) {
			return true
		}
	}
	return false
}

// Registration may use a non-canonical manifest_ref. Every downstream S6
// reader must consume that registered path rather than guessing a task path.
func TestRecheckNonCanonicalManifestLocation(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	manifest := s4AuditManifest(t, root, "noncanonical", "TASK-042-01", "packages/validation/source", "packages/validation/generated/result.json")
	if code, out, errOut := s4AuditRegister(t, root, manifest, "TASK-042-01"); code != 0 {
		t.Fatalf("registration from non-canonical manifest path failed before downstream probe: code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	var out, errOut bytes.Buffer
	code := runCLI(t, []string{"runtime", "worktree-create", "--root", root, "--assignment-id", "assignment-audit-noncanonical"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("registered manifest_ref must remain consumable from its non-canonical path: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}
