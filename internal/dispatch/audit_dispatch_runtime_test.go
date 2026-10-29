package dispatch

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// These tests exercise the S6 projection through Board.Load, rather than
// calling the platform adapter.  A dispatch projection is read-only: the
// fixtures below mutate only the in-memory Runtime snapshot and the evidence
// files that the projection is meant to consume.

func TestAuditDispatchCapacityAndEarlyRelease(t *testing.T) {
	root, state := boardFixture(t)

	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := board.Next, []string{"TASK-042-01", "TASK-042-02"}; !sameStrings(got, want) {
		t.Fatalf("capacity two selected %v, want %v", got, want)
	}

	// TASK-01 and TASK-02 have verified integration. TASK-03 is still active, so one
	// of two total slots remains available.  TASK-04 can start immediately;
	// the wave label does not create a barrier, while TASK-05 still waits for
	// the unintegrated TASK-03.
	auditDispatchAddIntegrated(t, root, state, "TASK-042-01", "builder-01", "assignment-01")
	auditDispatchAddIntegrated(t, root, state, "TASK-042-02", "builder-02", "assignment-02")
	auditDispatchAddRunning(state, "TASK-042-03")
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := board.Next, []string{"TASK-042-04"}; !sameStrings(got, want) {
		t.Fatalf("early release selected %v, want %v", got, want)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-05"), workstate.Pending, false, false, false, false)
	if row := auditDispatchRow(board, "TASK-042-05"); !strings.Contains(row.Reason, "TASK-042-03") {
		t.Fatalf("TASK-042-05 must wait for the live predecessor, got %+v", row)
	}
}

func TestAuditDispatchReportedAndAssignmentRecovery(t *testing.T) {
	root, state := boardFixture(t)
	auditDispatchAddReported(t, root, state, "TASK-042-01", "builder-01", "assignment-01", false)
	auditDispatchAddIntegrated(t, root, state, "TASK-042-02", "builder-02", "assignment-02")
	auditDispatchAddRunning(state, "TASK-042-03")

	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Active, false, true, false, false)
	if row := auditDispatchRow(board, "TASK-042-04"); row.State != workstate.Pending || row.Eligible || row.Dispatchable || !strings.Contains(row.Reason, "TASK-042-01") {
		t.Fatalf("consumer released before integration: %+v", row)
	}

	// The integration checkpoint is the recoverable transition.  Once it is
	// present, the same current result releases TASK-04.
	auditDispatchWriteCheckpoint(t, root, state, "TASK-042-01", "assignment-01", "2026-09-19T11:00:00Z")
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Done, true, true, false, false)
	if got, want := board.Next, []string{"TASK-042-04"}; !sameStrings(got, want) {
		t.Fatalf("checkpoint recovery selected %v, want %v", got, want)
	}

	// Reassignment/revocation makes the old worktree checkpoint stale.  The
	// projection must wait until the registered assignment points back to the
	// checkpoint that belongs to this result.
	auditDispatchSetAgentPrompt(state, "builder-01", ".claude/workgroups/audit/manifest.json#assignment-revoked")
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Active, false, true, false, false)
	if row := auditDispatchRow(board, "TASK-042-04"); row.State != workstate.Pending || row.Eligible || row.Dispatchable || !strings.Contains(row.Reason, "TASK-042-01") {
		t.Fatalf("consumer released after assignment revoke: %+v", row)
	}
	auditDispatchSetAgentPrompt(state, "builder-01", ".claude/workgroups/audit/manifest.json#assignment-01")
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Done, true, true, false, false)
}

func TestAuditDispatchVerifiedAtRefreshAndInvalidation(t *testing.T) {
	root, state := boardFixture(t)
	auditDispatchAddIntegrated(t, root, state, "TASK-042-01", "builder-01", "assignment-01")
	auditDispatchAddIntegrated(t, root, state, "TASK-042-02", "builder-02", "assignment-02")
	auditDispatchAddRunning(state, "TASK-042-03")

	// A legitimately refreshed Result gets a later created_at.  The previous
	// checkpoint no longer proves it; a new checkpoint at the later verification
	// time restores integration.
	auditDispatchRewriteReport(t, root, state, "TASK-042-01", "2026-09-19T12:00:00Z", "normal-refresh")
	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Active, false, true, false, false)
	if row := auditDispatchRow(board, "TASK-042-04"); row.State != workstate.Pending || row.Eligible || row.Dispatchable || !strings.Contains(row.Reason, "TASK-042-01") {
		t.Fatalf("consumer released after Result refresh: %+v", row)
	}
	auditDispatchWriteCheckpoint(t, root, state, "TASK-042-01", "assignment-01", "2026-09-19T13:00:00Z")
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Done, true, true, false, false)

	// Evidence invalidation has the same recovery shape: it blocks the
	// consumer, and clearing the invalidation only restores the already hashed
	// current artifact.
	auditDispatchSetEvidenceInvalidated(state, "ev-TASK-042-02", "review-revocation")
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-02"), workstate.Active, false, true, false, false)
	auditDispatchSetEvidenceInvalidated(state, "ev-TASK-042-02", nil)
	board, err = Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-02"), workstate.Done, true, true, false, false)
}

// Content changes invalidate integration even when created_at is retained.
func TestAuditDispatchMutableResultInvalidatesOldCheckpoint(t *testing.T) {
	root, state := boardFixture(t)
	auditDispatchAddIntegrated(t, root, state, "TASK-042-01", "builder-01", "assignment-01")
	auditDispatchAddIntegrated(t, root, state, "TASK-042-02", "builder-02", "assignment-02")
	auditDispatchAddRunning(state, "TASK-042-03")

	// Keep created_at at the checkpoint's already-verified time while changing
	// a semantically meaningful Result field and registering the new hash.
	auditDispatchRewriteReport(t, root, state, "TASK-042-01", "2026-09-19T10:00:00Z", "same-time-content-refresh")
	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditDispatchFacts(t, auditDispatchRow(board, "TASK-042-01"), workstate.Active, false, true, false, false)
}

// TestAuditDispatchRuntimeWriterRefreshPath reaches the same Runtime API used
// by controller/cycle.go: a mutable completion report is edited, then
// RefreshEvidenceFingerprints durably updates the Runtime evidence sha256.
// The test asserts that the writer really refreshed the index; the opt-in
// companion asserts the downstream checkpoint must then be rejected.
func TestAuditDispatchRuntimeWriterRefreshPath(t *testing.T) {
	fixture := auditDispatchRuntimeRefreshFixture(t)
	updatedReport := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-runtime-refresh", "kind": "completion_report",
		"runtime_id": "loop-audit-refresh", "baseline_generation": 1, "producer_agent_id": "builder",
		"producer_responsibility": "BUILD-WORK-PACKAGE", "review_round": 0,
		"subject_refs": []any{}, "conclusion": "completed", "requested_event": "",
		"invalidated_by": "", "task_id": "TASK-001",
		"checks":           []any{map[string]any{"name": "unit", "command": "go test", "result": "pass"}},
		"scope_deviations": []any{}, "changed_paths": []any{"same-created-at-refresh"},
		"created_at": "2026-09-19T10:00:00Z",
	}
	updatedBytes := auditDispatchWriteJSON(t, fixture.root, fixture.reportPath, updatedReport)
	result, err := fixture.writer.RefreshEvidenceFingerprints(
		fixture.root,
		map[string]bool{"completion_report": true},
		func(path string) ([]byte, error) {
			return os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(path)))
		},
		func(string) bool { return true },
	)
	if err != nil {
		t.Fatalf("runtime RefreshEvidenceFingerprints: %v", err)
	}
	if !containsString(result.Updated, fixture.reportPath) {
		t.Fatalf("runtime refresh did not update %s: %+v", fixture.reportPath, result)
	}
	snapshot, err := fixture.writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	entry := snapshot.State["evidence"].([]any)[0].(map[string]any)
	wantSHA := fmt.Sprintf("%x", sha256.Sum256(updatedBytes))
	if entry["sha256"] != wantSHA {
		t.Fatalf("runtime evidence sha=%v, want %s", entry["sha256"], wantSHA)
	}
	progress := qualitygate.PlannedBuilderProgress(qualitygate.Input{
		Root: fixture.root, Snapshot: snapshot, Files: fileview.Disk{Root: fixture.root},
	})["TASK-001"]
	t.Logf("production Writer refresh leaves current projection=%s (%s)", progress.State, progress.Reason)
}

// Exercise the production Writer refresh API before checking the content binding.
func TestAuditDispatchRuntimeWriterMutableResultInvalidatesOldCheckpoint(t *testing.T) {
	fixture := auditDispatchRuntimeRefreshFixture(t)
	updatedReport := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-runtime-refresh", "kind": "completion_report",
		"runtime_id": "loop-audit-refresh", "baseline_generation": 1, "producer_agent_id": "builder",
		"producer_responsibility": "BUILD-WORK-PACKAGE", "review_round": 0,
		"subject_refs": []any{}, "conclusion": "completed", "requested_event": "",
		"invalidated_by": "", "task_id": "TASK-001",
		"checks":           []any{map[string]any{"name": "unit", "command": "go test", "result": "pass"}},
		"scope_deviations": []any{}, "changed_paths": []any{"same-created-at-refresh"},
		"created_at": "2026-09-19T10:00:00Z",
	}
	updatedBytes := auditDispatchWriteJSON(t, fixture.root, fixture.reportPath, updatedReport)
	if _, err := fixture.writer.RefreshEvidenceFingerprints(
		fixture.root,
		map[string]bool{"completion_report": true},
		func(path string) ([]byte, error) {
			return os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(path)))
		},
		func(string) bool { return true },
	); err != nil {
		t.Fatalf("runtime RefreshEvidenceFingerprints: %v", err)
	}
	snapshot, err := fixture.writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	entry := snapshot.State["evidence"].([]any)[0].(map[string]any)
	if entry["sha256"] != fmt.Sprintf("%x", sha256.Sum256(updatedBytes)) {
		t.Fatalf("Writer did not persist the refreshed evidence sha: %v", entry["sha256"])
	}
	progress := qualitygate.PlannedBuilderProgress(qualitygate.Input{
		Root: fixture.root, Snapshot: snapshot, Files: fileview.Disk{Root: fixture.root},
	})["TASK-001"]
	if progress.State != workstate.Active || progress.Integrated || !progress.DeliveryRecorded {
		t.Fatalf("Writer-refreshed mutable Result reused old checkpoint: %+v", progress)
	}
}

type auditDispatchRuntimeRefreshCase struct {
	root, reportPath string
	writer           *runtime.Store
}

func auditDispatchRuntimeRefreshFixture(t *testing.T) auditDispatchRuntimeRefreshCase {
	t.Helper()
	root := t.TempDir()
	auditDispatchWriteLegacyManifest(t, root)
	claude := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(claude, "loop-state.json")
	journalPath := filepath.Join(claude, "loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-audit-refresh"
	state["revision"] = 1
	state["lifecycle"].(map[string]any)["state"] = "building"
	state["lifecycle"].(map[string]any)["phase"] = nil
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["last_transition"] = nil
	reportPath := ".claude/evidence/loop-audit-refresh/g1/assignments/assignment-001/result.json"
	report := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-runtime-refresh", "kind": "completion_report",
		"runtime_id": "loop-audit-refresh", "baseline_generation": 1, "producer_agent_id": "builder",
		"producer_responsibility": "BUILD-WORK-PACKAGE", "review_round": 0,
		"subject_refs": []any{}, "conclusion": "completed", "requested_event": "",
		"invalidated_by": "", "task_id": "TASK-001",
		"checks":           []any{map[string]any{"name": "unit", "command": "go test", "result": "pass"}},
		"scope_deviations": []any{}, "changed_paths": []any{},
		"created_at": "2026-09-19T10:00:00Z",
	}
	reportBytes := auditDispatchWriteJSON(t, root, reportPath, report)
	state["entities"].(map[string]any)["tasks"] = []any{map[string]any{
		"id": "TASK-001", "state": "review", "path": "docs/dev/tasks/TASK-001.md",
		"sha256": strings.Repeat("0", 64), "owner_agent_ids": []any{"builder"},
		"completion_report_ref": reportPath,
	}}
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "builder", "role": "backend-builder", "state": "reported", "task_ids": []any{"TASK-001"},
		"team_id": "team-builder", "definition_ref": "agents/backend-builder.md", "prompt_ref": ".claude/workgroups/audit/manifest.json#assignment-001",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-09-19T10:00:00Z",
	}}
	state["entities"].(map[string]any)["teams"] = []any{map[string]any{
		"id": "team-builder", "platform_team_id": "platform-team-builder", "kind": "builder", "status": "active",
		"agent_ids": []any{"builder"}, "manifest_ref": ".claude/workgroups/audit/manifest.json", "responsibility_ids": []any{"BUILD-WORK-PACKAGE"}, "review_round": nil,
	}}
	state["evidence"] = []any{map[string]any{
		"id": "ev-runtime-refresh", "kind": "completion_report", "path": reportPath,
		"sha256": fmt.Sprintf("%x", sha256.Sum256(reportBytes)), "status": "valid", "baseline_generation": 1,
		"review_round": nil, "produced_by": []any{"builder"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": "BUILD-WORK-PACKAGE", "scope_refs": []any{},
	}}
	if err := os.WriteFile(statePath, append(mustAuditDispatchJSON(t, state), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	auditDispatchWriteJSON(t, root, ".claude/evidence/loop-audit-refresh/g1/worktree/assignment-001/checkpoint.json", map[string]any{
		"assignment_id": "assignment-001", "task_id": "TASK-001", "state": "verified",
		"baseline_generation": 1, "verified_at": "2026-09-19T11:00:00Z",
		"completion_report_path": reportPath, "completion_report_sha256": fmt.Sprintf("%x", sha256.Sum256(reportBytes)),
	})
	return auditDispatchRuntimeRefreshCase{
		root: root, reportPath: reportPath,
		writer: runtime.NewWriter(statePath, journalPath, root, auditDispatchRuntimeValidator{}),
	}
}

func mustAuditDispatchJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type auditDispatchRuntimeValidator struct{}

func (auditDispatchRuntimeValidator) ValidateCandidate(_ string, state map[string]any) error {
	if state == nil || state["runtime_id"] == nil {
		return errors.New("reject empty runtime validator probe")
	}
	if lifecycle, ok := state["lifecycle"].(map[string]any); ok && lifecycle["phase"] == "invalid_semantic_phase" {
		return errors.New("reject semantic validator probe")
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAuditDispatchConflictRecoveryAndLegacyProjection(t *testing.T) {
	plan := &semantic.DispatchPlan{Tasks: []semantic.DispatchTask{
		{ID: "TASK-A", Wave: 1, Writes: []string{"shared/output"}},
		{ID: "TASK-B", Wave: 1, Writes: []string{"shared/output"}},
	}}
	rows, next := semantic.NextDispatch(plan, map[string]semantic.DispatchFact{
		"TASK-A": {State: workstate.Active, Reason: "active"},
	}, 1)
	if len(next) != 0 || auditDispatchSemanticRow(rows, "TASK-B").State != workstate.Pending || auditDispatchSemanticRow(rows, "TASK-B").Eligible || auditDispatchSemanticRow(rows, "TASK-B").Dispatchable {
		t.Fatalf("active write conflict was released: rows=%+v next=%v", rows, next)
	}
	rows, next = semantic.NextDispatch(plan, map[string]semantic.DispatchFact{
		"TASK-A": {State: workstate.Done, Reason: "verified", Integrated: true, DeliveryRecorded: true},
	}, 1)
	if !sameStrings(next, []string{"TASK-B"}) {
		t.Fatalf("conflict recovery did not release TASK-B: rows=%+v next=%v", rows, next)
	}
	assertAuditDispatchFacts(t, auditDispatchSemanticRow(rows, "TASK-A"), workstate.Done, true, true, false, false)
	assertAuditDispatchFacts(t, auditDispatchSemanticRow(rows, "TASK-B"), workstate.Pending, false, false, true, true)

	root, state := boardFixture(t)
	docs := state["documents"].([]any)
	kept := make([]any, 0, len(docs))
	for _, raw := range docs {
		doc := raw.(map[string]any)
		if doc["kind"] != "dispatch_plan" {
			kept = append(kept, raw)
		}
	}
	state["documents"] = kept
	board, err := Load(root, state, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !board.Legacy || len(board.Next) != 0 {
		t.Fatalf("legacy state was not recoverable read-only projection: %+v", board)
	}
}

func auditDispatchAddRunning(state map[string]any, taskID string) {
	auditDispatchUpsertEntity(state, "tasks", taskID, map[string]any{"id": taskID, "state": "in_progress"})
}

func auditDispatchAddReported(t *testing.T, root string, state map[string]any, taskID, agentID, assignment string, checkpoint bool) {
	t.Helper()
	auditDispatchAddReport(t, root, state, taskID, agentID, assignment)
	if checkpoint {
		auditDispatchWriteCheckpoint(t, root, state, taskID, assignment, "2026-09-19T11:00:00Z")
	}
}

func auditDispatchAddIntegrated(t *testing.T, root string, state map[string]any, taskID, agentID, assignment string) {
	t.Helper()
	auditDispatchAddReported(t, root, state, taskID, agentID, assignment, true)
}

func auditDispatchAddReport(t *testing.T, root string, state map[string]any, taskID, agentID, assignment string) {
	t.Helper()
	auditDispatchWriteLegacyManifest(t, root)
	evidenceID := "ev-" + taskID
	reportPath := ".claude/evidence/run/g1/assignments/" + assignment + "/" + evidenceID + ".json"
	subject := auditDispatchTaskSubject(t, state, taskID)
	report := map[string]any{
		"schema_version": "1.0.0", "evidence_id": evidenceID, "kind": "completion_report",
		"runtime_id": "run", "baseline_generation": 1, "producer_agent_id": agentID,
		"producer_responsibility": "BUILD-WORK-PACKAGE", "review_round": 0,
		"subject_refs": []any{subject}, "conclusion": "completed", "requested_event": "",
		"invalidated_by": "", "task_id": taskID,
		"checks":           []any{map[string]any{"name": "unit", "command": "go test", "result": "pass"}},
		"scope_deviations": []any{}, "changed_paths": []any{},
		"created_at": "2026-09-19T10:00:00Z",
	}
	b := auditDispatchWriteJSON(t, root, reportPath, report)
	auditDispatchUpsertEntity(state, "tasks", taskID, map[string]any{
		"id": taskID, "state": "review", "owner_agent_ids": []any{agentID}, "completion_report_ref": reportPath,
	})
	auditDispatchUpsertEntity(state, "agents", agentID, map[string]any{
		"id": agentID, "role": "backend-builder", "state": "reported", "task_ids": []any{taskID},
		"team_id": "team-" + agentID, "definition_ref": "agents/backend-builder.md", "prompt_ref": ".claude/workgroups/audit/manifest.json#" + assignment,
	})
	auditDispatchUpsertEntity(state, "teams", "team-"+agentID, map[string]any{
		"id": "team-" + agentID, "platform_team_id": "platform-team-" + agentID, "kind": "builder", "status": "active",
		"agent_ids": []any{agentID}, "manifest_ref": ".claude/workgroups/audit/manifest.json", "responsibility_ids": []any{"BUILD-WORK-PACKAGE"}, "review_round": nil,
	})
	auditDispatchUpsertEvidence(state, map[string]any{
		"id": evidenceID, "kind": "completion_report", "path": reportPath,
		"sha256": fmt.Sprintf("%x", sha256.Sum256(b)), "status": "valid", "baseline_generation": 1,
		"invalidated_by": nil, "produced_by": []any{agentID},
	})
}

func auditDispatchWriteLegacyManifest(t *testing.T, root string) {
	t.Helper()
	auditDispatchWriteJSON(t, root, ".claude/workgroups/audit/manifest.json", map[string]any{"schema_version": "1.0.0"})
}

func auditDispatchTaskSubject(t *testing.T, state map[string]any, taskID string) map[string]any {
	t.Helper()
	for _, raw := range state["documents"].([]any) {
		doc := raw.(map[string]any)
		if doc["kind"] == "task" && doc["id"] == taskID {
			return map[string]any{
				"path": doc["path"], "version": doc["version"], "sha256": doc["sha256"],
			}
		}
	}
	t.Fatalf("missing registered TASK document %s", taskID)
	return nil
}

func auditDispatchRewriteReport(t *testing.T, root string, state map[string]any, taskID, createdAt, marker string) {
	t.Helper()
	entities := state["entities"].(map[string]any)
	var reportPath string
	for _, raw := range entities["tasks"].([]any) {
		task := raw.(map[string]any)
		if task["id"] == taskID {
			reportPath = task["completion_report_ref"].(string)
			break
		}
	}
	if reportPath == "" {
		t.Fatalf("missing report for %s", taskID)
	}
	data, err := os.ReadFile(filepath.Join(root, reportPath))
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	report["created_at"] = createdAt
	report["changed_paths"] = []any{marker}
	b := auditDispatchWriteJSON(t, root, reportPath, report)
	for _, raw := range state["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["path"] == reportPath {
			entry["sha256"] = fmt.Sprintf("%x", sha256.Sum256(b))
		}
	}
}

func auditDispatchWriteCheckpoint(t *testing.T, root string, state map[string]any, taskID, assignment, verifiedAt string) {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(".claude", "evidence", "run", "g1", "worktree", assignment, "checkpoint.json"))
	reportPath := ""
	for _, raw := range state["entities"].(map[string]any)["tasks"].([]any) {
		task := raw.(map[string]any)
		if task["id"] == taskID {
			reportPath, _ = task["completion_report_ref"].(string)
		}
	}
	reportBytes, err := os.ReadFile(filepath.Join(root, reportPath))
	if err != nil {
		t.Fatal(err)
	}
	mergeCommit := auditDispatchCreateMergeReceipt(t, root, assignment)
	auditDispatchWriteJSON(t, root, path, map[string]any{
		"assignment_id": assignment, "task_id": taskID, "state": "verified",
		"baseline_generation": 1, "verified_at": verifiedAt,
		"completion_report_path": reportPath, "completion_report_sha256": fmt.Sprintf("%x", sha256.Sum256(reportBytes)),
		"target_branch": "dev", "merge_commit": mergeCommit,
	})
}

func auditDispatchCreateMergeReceipt(t *testing.T, root, assignment string) string {
	t.Helper()
	branch := "audit-receipt-" + strings.NewReplacer("/", "-", "\\", "-").Replace(assignment)
	auditDispatchGit(t, root, "checkout", "-b", branch)
	auditDispatchGit(t, root, "commit", "--allow-empty", "-m", "record delivery receipt for "+assignment)
	auditDispatchGit(t, root, "checkout", "dev")
	auditDispatchGit(t, root, "merge", "--no-ff", branch, "-m", "integrate delivery receipt for "+assignment)
	mergeCommit := strings.TrimSpace(auditDispatchGit(t, root, "rev-parse", "HEAD"))
	auditDispatchGit(t, root, "branch", "-D", branch)
	return mergeCommit
}

func auditDispatchGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func auditDispatchSetAgentPrompt(state map[string]any, agentID, prompt string) {
	entities := state["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == agentID {
			agent["prompt_ref"] = prompt
		}
	}
}

func auditDispatchSetEvidenceInvalidated(state map[string]any, evidenceID string, value any) {
	for _, raw := range state["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == evidenceID {
			entry["invalidated_by"] = value
		}
	}
}

func auditDispatchUpsertEntity(state map[string]any, collection, id string, entity map[string]any) {
	entities := state["entities"].(map[string]any)
	items, _ := entities[collection].([]any)
	for i, raw := range items {
		current := raw.(map[string]any)
		if current["id"] == id {
			items[i] = entity
			entities[collection] = items
			return
		}
	}
	entities[collection] = append(items, entity)
}

func auditDispatchUpsertEvidence(state map[string]any, entry map[string]any) {
	items, _ := state["evidence"].([]any)
	for i, raw := range items {
		current := raw.(map[string]any)
		if current["id"] == entry["id"] {
			items[i] = entry
			state["evidence"] = items
			return
		}
	}
	state["evidence"] = append(items, entry)
}

func auditDispatchWriteJSON(t *testing.T, root, rel string, value map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func auditDispatchRow(board Board, taskID string) semantic.DispatchRow {
	for _, row := range board.Rows {
		if row.Task.ID == taskID {
			return row
		}
	}
	return semantic.DispatchRow{Task: semantic.DispatchTask{ID: taskID}, State: "missing"}
}

func assertAuditDispatchFacts(t *testing.T, row semantic.DispatchRow, state workstate.Status, integrated, deliveryRecorded, eligible, dispatchable bool) {
	t.Helper()
	if row.State != state || row.Integrated != integrated || row.DeliveryRecorded != deliveryRecorded || row.Eligible != eligible || row.Dispatchable != dispatchable {
		t.Fatalf("dispatch row facts = %+v, want state=%s integrated=%t delivery_recorded=%t eligible=%t dispatchable=%t", row, state, integrated, deliveryRecorded, eligible, dispatchable)
	}
}

func auditDispatchSemanticRow(rows []semantic.DispatchRow, taskID string) semantic.DispatchRow {
	for _, row := range rows {
		if row.Task.ID == taskID {
			return row
		}
	}
	return semantic.DispatchRow{Task: semantic.DispatchTask{ID: taskID}, State: "missing"}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
