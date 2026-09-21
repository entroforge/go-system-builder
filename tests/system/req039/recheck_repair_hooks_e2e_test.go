package req039_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestRecheckS9WorkerPlanReportIsImportedAtAuthority is a positive recheck for
// the real S9 domain CLI. The Builder authors the PlanReport in
// its Git worktree and invokes the documented command with the authority
// root. The CLI reads the worker-local request, writes the immutable report
// under the authority, and advances the shared Runtime.
func TestRecheckS9WorkerPlanReportIsImportedAtAuthority(t *testing.T) {
	root, sessionRef, planRef, assignmentID := seedRecheckS9Dispatch(t)

	dispatchOut, dispatchErr := bytes.Buffer{}, bytes.Buffer{}
	code := runCLI(t, []string{
		"runtime", "repair", "dispatch", "--root", root,
		"--assignment-id", assignmentID, "--agent-id", "builder-recheck-s9",
	}, strings.NewReader(""), &dispatchOut, &dispatchErr)
	if code != 0 {
		t.Fatalf("runtime repair dispatch failed: code=%d stdout=%s stderr=%s", code, dispatchOut.String(), dispatchErr.String())
	}
	var dispatch map[string]any
	if err := json.Unmarshal(dispatchOut.Bytes(), &dispatch); err != nil {
		t.Fatalf("decode dispatch response: %v; stdout=%s", err, dispatchOut.String())
	}
	manifestRel, _ := dispatch["manifest_path"].(string)
	if manifestRel == "" {
		t.Fatalf("dispatch response omitted manifest_path: %s", dispatchOut.String())
	}
	revision, ok := dispatch["revision"].(float64)
	if !ok {
		t.Fatalf("dispatch response omitted numeric revision: %s", dispatchOut.String())
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestRel)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	rows, _ := manifest["assignments"].([]any)
	if len(rows) != 1 {
		t.Fatalf("dispatch manifest assignments=%d, want 1", len(rows))
	}
	assignmentRow, _ := rows[0].(map[string]any)
	workerAssignmentID, _ := assignmentRow["assignment_id"].(string)
	if workerAssignmentID == "" {
		t.Fatalf("dispatch manifest assignment_id missing: %s", manifestBytes)
	}

	worktreeOut, worktreeErr := bytes.Buffer{}, bytes.Buffer{}
	code = runCLI(t, []string{
		"runtime", "worktree-create", "--root", root,
		"--assignment-id", workerAssignmentID,
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
		t.Fatalf("worktree response omitted worktree_path: %s", worktreeOut.String())
	}

	request := repair.PlanReportRequest{
		Session:      sessionRef,
		Plan:         planRef,
		AssignmentID: assignmentID,
		AssertionIDs: []string{"symptom-1"},
		AgentID:      "builder-recheck-s9",
		ReportID:     "repair-plan-report-recheck-s9",
		PlanText:     "read the approved repair inputs and restore the assigned invariant",
		RedChecks: []repair.RepairCheck{{
			Name:         "original failure",
			Command:      "go test ./internal/api",
			Result:       "fail",
			EvidenceRefs: []string{"test://recheck-s9/red"},
		}},
		ProposedPaths: []string{"internal/api"},
	}
	requestBytes, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	workerRequest := filepath.Join(workerPath, "plan-report.json")
	if err := os.WriteFile(workerRequest, append(requestBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	submitOut, submitErr := bytes.Buffer{}, bytes.Buffer{}
	code = runCLI(t, []string{
		"runtime", "repair", "plan-report", "submit",
		"--root", root,
		"--expected-revision", fmt.Sprintf("%d", int(revision)),
		"--actor", "builder-recheck-s9",
		"--file", workerRequest,
	}, strings.NewReader(""), &submitOut, &submitErr)
	if code != 0 {
		t.Fatalf("S9 domain submit from the real worker worktree failed: code=%d stdout=%s stderr=%s", code, submitOut.String(), submitErr.String())
	}
	var submit map[string]any
	if err := json.Unmarshal(submitOut.Bytes(), &submit); err != nil {
		t.Fatalf("decode PlanReport submit response: %v; stdout=%s", err, submitOut.String())
	}
	reportRef, _ := submit["artifact_ref"].(map[string]any)
	reportRel, _ := reportRef["path"].(string)
	if reportRel == "" || !strings.HasPrefix(filepath.ToSlash(reportRel), ".claude/review/repair/plan-reports/") {
		t.Fatalf("PlanReport was not imported into the authority artifact namespace: %s", submitOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(reportRel))); err != nil {
		t.Fatalf("authority PlanReport artifact %q is missing: %v", reportRel, err)
	}
	submitRevision, ok := submit["revision"].(float64)
	if !ok {
		t.Fatalf("PlanReport submit response omitted revision: %s", submitOut.String())
	}

	beginOut, beginErr := bytes.Buffer{}, bytes.Buffer{}
	code = runCLI(t, []string{
		"runtime", "repair", "execution", "begin",
		"--root", root,
		"--expected-revision", fmt.Sprintf("%d", int(submitRevision)),
		"--actor", "builder-recheck-s9",
	}, strings.NewReader(""), &beginOut, &beginErr)
	if code != 0 {
		t.Fatalf("S9 execution begin after the imported PlanReport failed: code=%d stdout=%s stderr=%s", code, beginOut.String(), beginErr.String())
	}
}

func seedRecheckS9Dispatch(t *testing.T) (string, repair.ArtifactRef, repair.ArtifactRef, string) {
	t.Helper()
	root := freshRoot(t)
	setupGitWorktreeFixture(t, root)
	writeAuditDispatchFile(t, root, "docs/control/agent-protocol.md", "# agent protocol\n")
	writeAuditDispatchFile(t, root, "agents/backend-builder.md", "# backend builder\n")

	contractRel := ".claude/review/investigation/contracts/repair-contract-recheck-s9.json"
	contractBytes := []byte("{\"schema_version\":\"1.0.0\",\"repair_contract_id\":\"repair-contract-recheck-s9\",\"case_id\":\"investigation-case-recheck-s9\",\"revision\":1,\"status\":\"approved\",\"source_finding_ids\":[\"finding-recheck-s9\"],\"root_cause_statement\":\"the worker must receive approved S9 inputs\",\"violated_invariant\":\"the Builder reads the approved inputs\",\"causal_model_ref\":\"case://recheck-s9/model\",\"architecture_intent\":\"preserve one formal S9 input source\",\"repair_units\":[{\"id\":\"unit-recheck-s9\",\"description\":\"recheck S9 input handoff\",\"scope\":[\"internal/api\"],\"assertion_ids\":[\"symptom-1\"]}],\"prospective_scope\":[\"internal/api\"],\"forbidden_scope\":[\"docs/requirements\"],\"symptom_assertions\":[\"worker reads approved inputs\"],\"root_invariant_assertions\":[\"formal inputs remain readable\"],\"detection_gap_assertions\":[\"worker-local report has no authority import\"],\"stop_escalation_conditions\":[\"input handoff is incomplete\"],\"approved_by\":\"human\",\"approved_at\":\"2026-08-26T00:00:00Z\",\"approval_hash\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}\n")
	contractPath := filepath.Join(root, filepath.FromSlash(contractRel))
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, contractBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	contractRef := repair.ContractRef{Path: contractRel, SHA256: auditSHA256(contractBytes)}
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-recheck-s9",
		RuntimeID: "loop-req039-recheck", ReqID: "REQ-039",
		BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contractRef, Session: sessionRef,
		PlanID: "repair-plan-recheck-s9", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	state := req039fixtures.BaseState(t, root, "bug_resolution", "planning", 4)
	review := state["review"].(map[string]any)
	review["repair"] = map[string]any{
		"session_id": session.SessionID, "case_id": "investigation-case-recheck-s9",
		"contract_id": "repair-contract-recheck-s9", "contract_ref": contractRel,
		"contract_sha256": contractRef.SHA256, "path": sessionRef.Path,
		"sha256": sessionRef.SHA256, "revision": 1, "status": "planning",
		"plan_ref": planRef.Path, "plan_sha256": planRef.SHA256,
		"plan_report_refs": []any{}, "result_refs": []any{},
		"next_action": "dispatch a Builder", "updated_at": "2026-08-26T00:00:00Z",
	}
	writeSystemState(t, root, state)
	return root, sessionRef, planRef, plan.Assignments[0].AssignmentID
}
