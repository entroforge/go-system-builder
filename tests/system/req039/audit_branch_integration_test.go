package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/schema"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestL4AuditAssignmentAndTaskCompletionRefsShareCanonicalSource is an
// regression for the cross-stage S6 -> Integrator boundary. The
// canonical task-complete command derives one Builder Result envelope, but
// the Hook assignment projection may still expose the original wire message
// as CompletionRef. The Integrator consumes the assignment projection while
// the planned quality gate consumes task.completion_report_ref; these must
// identify the same bytes before a verified checkpoint can be trusted.
func TestL4AuditAssignmentAndTaskCompletionRefsShareCanonicalSource(t *testing.T) {

	root := freshRoot(t)
	repo := setupGitWorktreeFixture(t, root)
	state := systemPlanningState(t, root, "tasks", 12)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-ti", "role": "builder", "state": "working",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-ti",
			"definition_ref": ".claude/agents/backend-builder.md",
			"prompt_ref":     "manifest#assignment-ti",
			"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-20T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-039-01", "state": "in_progress",
			"path":            "docs/dev/tasks/TASK-039-01.md",
			"sha256":          "0000000000000000000000000000000000000000000000000000000000000001",
			"owner_agent_ids": []any{"builder-ti"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	taskPath := filepath.Join(root, "docs/dev/tasks/TASK-039-01.md")
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, []byte("# TASK-039-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSystemState(t, root, state)
	writeWorkgroupWithWorktree(t, root, "TASK-039-01", "assignment-ti", "builder-ti", repo.wtPath, repo.branch)

	messagePath := filepath.Join(root, ".claude/messages/completion-builder-ti.json")
	writeAuditCompletionMessage(t, messagePath, "builder-ti", "TASK-039-01")
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "task-complete", "--root", root,
		"--state", filepath.Join(root, ".claude/loop-state.json"),
		"--journal", filepath.Join(root, ".claude/loop-events.jsonl"),
		"--agent-id", "builder-ti", "--message", messagePath,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runtime task-complete failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stateAfter := readSystemState(t, root)
	taskRef := completionTaskRef(t, stateAfter, "TASK-039-01")
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var assignmentRef string
	for _, assignment := range loaded.Assignments {
		if assignment.AssignmentID == "assignment-ti" {
			assignmentRef = assignment.CompletionRef
			break
		}
	}
	if assignmentRef == "" {
		t.Fatalf("assignment-ti was not projected after task-complete: %#v", loaded.Assignments)
	}
	if assignmentRef != taskRef {
		t.Fatalf("canonical Result references diverged: assignment CompletionRef=%q, task completion_report_ref=%q", assignmentRef, taskRef)
	}

	// Continue through the real S6 -> Integrator boundary so the mismatch is
	// observable in the durable checkpoint, rather than only in projections.
	// A successful integration must bind the exact canonical task result that
	// the quality gate consumed; binding the original wire message is a source
	// split even when both files happen to exist.
	integrateCode, integrateStdout, integrateStderr := runTaskIntegrate(t, root, "assignment-ti")
	if integrateCode != 0 {
		t.Fatalf("runtime task-integrate after task-complete failed: code=%d stdout=%s stderr=%s", integrateCode, integrateStdout, integrateStderr)
	}
	checkpointPath, _ := readIntegrationCheckpoint(t, root)
	checkpointData, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatalf("read integration checkpoint %q: %v", checkpointPath, err)
	}
	var checkpoint map[string]any
	if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpointRef, _ := checkpoint["completion_report_path"].(string)
	if checkpointRef != taskRef {
		t.Fatalf("integration checkpoint bound a different completion source: assignment CompletionRef=%q, checkpoint completion_report_path=%q, task completion_report_ref=%q; stdout=%s stderr=%s", assignmentRef, checkpointRef, taskRef, integrateStdout, integrateStderr)
	}
}

// TestL4AuditControllerCheckpointProjectsIntoLoadedContext checks the other
// cross-module handoff: task-integrate persists its status into the Runtime
// milestone, while hookctx.LoadFull exposes IntegrationCheckpoint only from
// map-shaped entries. The current controller writes the schema-compatible
// string projection, so this opt-in probe records the missing read-side
// checkpoint after a real Git/CLI integration.
func TestL4AuditControllerCheckpointProjectsIntoLoadedContext(t *testing.T) {
	if os.Getenv("L4_AUDIT_REPRO") != "1" {
		t.Skip("set L4_AUDIT_REPRO=1 to run the known checkpoint projection probe")
	}

	root := freshRoot(t)
	seedIntegrableAssignment(t, root)
	code, stdout, stderr := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("runtime task-integrate failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.IntegrationCheckpoint == nil {
		state := readSystemState(t, root)
		milestone, _ := state["milestone"].(map[string]any)
		t.Fatalf("completed task-integrate checkpoint is absent from hookctx.LoadFull; milestone.integration=%#v", milestone["integration"])
	}
}

// TestL4AuditS9DispatchWorktreeCopiesFormalInputs is an opt-in failing probe
// for the S9 dispatch -> worker boundary. S9 dispatch writes Contract,
// Session, and Plan under the authority's .claude/review tree and records
// root-relative paths in the generated manifest. A real worktree-create is a
// Git checkout at the bound commit; it does not copy those runtime artifacts.
// The worker therefore starts with manifest read_paths that do not exist in
// its checkout, so it cannot perform the first required PlanReport step.
func TestL4AuditS9DispatchWorktreeCopiesFormalInputs(t *testing.T) {
	if os.Getenv("L4_AUDIT_REPRO") != "1" {
		t.Skip("set L4_AUDIT_REPRO=1 to run the known S9 input-handoff probe")
	}

	root := freshRoot(t)
	setupGitWorktreeFixture(t, root)
	writeAuditDispatchFile(t, root, "docs/control/agent-protocol.md", "# agent protocol\n")
	writeAuditDispatchFile(t, root, "agents/backend-builder.md", "# backend builder\n")

	contractRel := ".claude/review/investigation/contracts/repair-contract-audit-s9.json"
	contractBytes := []byte(`{
  "schema_version":"1.0.0","repair_contract_id":"repair-contract-audit-s9","case_id":"investigation-case-audit-s9","revision":1,"status":"approved",
  "source_finding_ids":["finding-audit-s9"],"root_cause_statement":"runtime input must cross the worker boundary","violated_invariant":"worker reads the approved S9 artifacts","causal_model_ref":"case://audit-s9/model","architecture_intent":"preserve one formal S9 input source",
  "repair_units":[{"id":"unit-audit-s9","description":"audit S9 input handoff","scope":["internal/api"],"assertion_ids":["symptom-1"]}],
  "prospective_scope":["internal/api"],"forbidden_scope":["docs/requirements"],"symptom_assertions":["worker can read the approved inputs"],
  "root_invariant_assertions":["formal S9 inputs remain readable"],"detection_gap_assertions":["worktree creation does not copy runtime artifacts"],"stop_escalation_conditions":["input handoff is incomplete"],"approved_by":"human","approved_at":"2026-08-26T00:00:00Z",
  "approval_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
`)
	contractPath := filepath.Join(root, filepath.FromSlash(contractRel))
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, contractBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	contractRef := repair.ContractRef{Path: contractRel, SHA256: auditSHA256(contractBytes)}
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-audit-s9", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-audit-s9", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	state := req039fixtures.BaseState(t, root, "bug_resolution", "planning", 4)
	review := state["review"].(map[string]any)
	review["repair"] = map[string]any{
		"session_id": session.SessionID, "case_id": "investigation-case-audit-s9", "contract_id": "repair-contract-audit-s9",
		"contract_ref": contractRel, "contract_sha256": contractRef.SHA256, "path": sessionRef.Path, "sha256": sessionRef.SHA256, "revision": 1,
		"status": "planning", "plan_ref": planRef.Path, "plan_sha256": planRef.SHA256, "plan_report_refs": []any{}, "result_refs": []any{},
		"next_action": "dispatch a Builder", "updated_at": "2026-08-26T00:00:00Z",
	}
	writeSystemState(t, root, state)

	var dispatchOut, dispatchErr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "repair", "dispatch", "--root", root,
		"--assignment-id", plan.Assignments[0].AssignmentID, "--agent-id", "builder-audit-s9",
	}, strings.NewReader(""), &dispatchOut, &dispatchErr)
	if code != 0 {
		t.Fatalf("runtime repair dispatch failed: code=%d stdout=%s stderr=%s", code, dispatchOut.String(), dispatchErr.String())
	}
	var dispatchResponse map[string]any
	if err := json.Unmarshal(dispatchOut.Bytes(), &dispatchResponse); err != nil {
		t.Fatalf("decode dispatch response: %v; stdout=%s", err, dispatchOut.String())
	}
	manifestRel, _ := dispatchResponse["manifest_path"].(string)
	manifestPath := filepath.Join(root, filepath.FromSlash(manifestRel))
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("dispatch manifest %q is missing: %v", manifestRel, err)
	}
	var manifest struct {
		Assignments []struct {
			AssignmentID string   `json:"assignment_id"`
			ReadPaths    []string `json:"read_paths"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Assignments) != 1 {
		t.Fatalf("dispatch manifest assignments=%d, want 1", len(manifest.Assignments))
	}
	workerAssignmentID := manifest.Assignments[0].AssignmentID
	if workerAssignmentID == "" {
		t.Fatal("dispatch manifest has no worker assignment id")
	}

	var worktreeOut, worktreeErr bytes.Buffer
	code = runCLI(t, []string{
		"runtime", "worktree-create", "--root", root, "--assignment-id", workerAssignmentID,
	}, strings.NewReader(""), &worktreeOut, &worktreeErr)
	if code != 0 {
		t.Fatalf("runtime worktree-create failed: code=%d stdout=%s stderr=%s", code, worktreeOut.String(), worktreeErr.String())
	}
	var worktree map[string]any
	if err := json.Unmarshal(worktreeOut.Bytes(), &worktree); err != nil {
		t.Fatalf("decode worktree response: %v; stdout=%s", err, worktreeOut.String())
	}
	workerPath, _ := worktree["worktree_path"].(string)
	if workerPath == "" {
		t.Fatalf("worktree response has no worktree_path: %s", worktreeOut.String())
	}
	missing := []string{}
	for _, readPath := range manifest.Assignments[0].ReadPaths {
		if readPath == "docs/control/agent-protocol.md" {
			continue // committed repository input; this one is expected to travel with Git.
		}
		if _, err := os.Stat(filepath.Join(workerPath, filepath.FromSlash(readPath))); os.IsNotExist(err) {
			missing = append(missing, readPath)
		} else if err != nil {
			t.Fatalf("stat worker read_path %q: %v", readPath, err)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("S9 worker input handoff is incomplete after real dispatch→worktree-create: worker=%s missing root-relative read_paths=%v; authority manifest=%s", workerPath, missing, manifestRel)
	}
}

func writeAuditDispatchFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func auditSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeAuditCompletionMessage starts from the repository's schema-valid
// completion example so this probe reaches the cross-module source binding
// rather than failing on message shape.
func writeAuditCompletionMessage(t *testing.T, path, agentID, taskID string) {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	for _, candidate := range messages {
		if candidate["message_type"] == "completion_report" {
			message = candidate
			break
		}
	}
	if message == nil {
		t.Fatal("schema examples do not contain a completion_report")
	}
	message["runtime_id"] = "loop-system-test"
	message["agent_id"] = agentID
	message["task_id"] = taskID
	message["team_id"] = "team-ti"
	message["status"] = "completed"
	message["summary"] = "record a cross-stage source binding probe"
	message["changed_paths"] = []any{"internal/feature.go"}
	message["reviewed_paths"] = []any{}
	message["checks"] = []any{map[string]any{
		"name": "probe", "command": "true", "result": "pass", "evidence_ref": nil,
	}}
	message["evidence_refs"] = []any{}
	message["finding_refs"] = []any{}
	message["remaining_risks"] = []any{}
	message["scope_deviations"] = []any{}
	message["requested_event"] = "completion_reported"
	encoded, err := json.MarshalIndent(message, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSystemState(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func completionTaskRef(t *testing.T, state map[string]any, taskID string) string {
	t.Helper()
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task["id"] == taskID {
			ref, _ := task["completion_report_ref"].(string)
			return ref
		}
	}
	t.Fatalf("TASK %s missing completion_report_ref", taskID)
	return ""
}
