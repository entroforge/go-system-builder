package recovery_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

const recoveryReqPath = "docs/requirements/REQ-039-loop-control-plane.md"

// TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState is the RED
// contract for RR-001 and RR-004. Both read-only commands must work without
// decoding the active Runtime first, and the plan must retain the damaged
// bytes for later apply/quarantine.
func TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "malformed-json", raw: []byte(`{"schema_version":`)},
		{name: "utf8-bom", raw: append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"schema_version":"1.1.0"}`)...)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := newRecoveryRoot(t)
			writeActiveRuntimeBytes(t, root, tc.raw)

			code, stdout, stderr := runRecovery(t, root,
				"runtime", "recover", "inspect",
				"--root", root, "--req", recoveryReqPath,
			)
			if code != 0 {
				t.Fatalf("inspect must survive %s: code=%d stdout=%s stderr=%s", tc.name, code, stdout, stderr)
			}
			if !containsAny(stdout+stderr, "LOOP_RUNTIME_MALFORMED", "malformed", "BOM", "utf-8") {
				t.Fatalf("inspect must report the damaged runtime, got stdout=%s stderr=%s", stdout, stderr)
			}

			code, stdout, stderr = runRecovery(t, root,
				"runtime", "recover", "plan",
				"--root", root, "--req", recoveryReqPath,
			)
			if code != 0 {
				t.Fatalf("plan must survive %s: code=%d stdout=%s stderr=%s", tc.name, code, stdout, stderr)
			}
			planPath := recoveryPlanPath(t, root)
			plan := readJSON(t, planPath)
			if !containsAny(stdout+stderr, "recovery", "plan") {
				t.Fatalf("plan must report a recovery plan, got stdout=%s stderr=%s", stdout, stderr)
			}
			if !planReferencesBytes(t, root, plan, tc.raw) {
				t.Fatalf("plan must content-address or retain damaged runtime bytes; plan=%s", planPath)
			}
		})
	}
}

// TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime is the RED
// contract for RR-002, RR-004, RR-008 and RR-010. Apply must create a new
// valid Runtime epoch, explicitly bind the selected REQ, and preserve the old
// bytes in immutable recovery storage.
func TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime(t *testing.T) {
	root := newRecoveryRoot(t)
	corrupt := []byte{0xef, 0xbb, 0xbf, '{', '"', 'b', 'r', 'o', 'k', 'e', 'n', '"', ':'}
	writeActiveRuntimeBytes(t, root, corrupt)

	planPath := createRecoveryPlan(t, root)
	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 {
		t.Fatalf("apply must recover corrupt Runtime: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	statePath := filepath.Join(root, ".claude", "loop-state.json")
	active, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("recovered active Runtime missing: %v", err)
	}
	if bytes.Equal(active, corrupt) {
		t.Fatal("apply must not leave the corrupt bytes as the active Runtime")
	}
	if err := semantic.ValidateRuntimeFile(root, ".claude/loop-state.json"); err != nil {
		t.Fatalf("recovered Runtime must be schema- and semantically-valid: %v", err)
	}

	state := decodeState(t, active)
	bound, ok := state["bound_req"].(map[string]any)
	if !ok {
		t.Fatalf("recovered Runtime must contain bound_req: %#v", state["bound_req"])
	}
	if got := bound["id"]; got != "REQ-039" {
		t.Fatalf("recovered Runtime bound_req.id = %v, want REQ-039", got)
	}
	if got := bound["path"]; got != recoveryReqPath {
		t.Fatalf("recovered Runtime bound_req.path = %v, want %s", got, recoveryReqPath)
	}
	if got := bound["status"]; got != "locked" {
		t.Fatalf("recovered Runtime bound_req.status = %v, want locked", got)
	}
	reqBytes, err := os.ReadFile(filepath.Join(root, recoveryReqPath))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := bound["sha256"], sha256Hex(reqBytes); got != want {
		t.Fatalf("recovered Runtime bound_req.sha256 = %v, want current REQ digest %s", got, want)
	}
	if runtimeID, _ := state["runtime_id"].(string); runtimeID == "" {
		t.Fatal("recovered Runtime must have a runtime_id")
	}

	quarantined, err := recoveryTreeContains(root, corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if !quarantined {
		t.Fatal("apply must preserve the exact corrupt Runtime bytes under quarantine")
	}
}

func TestRuntimeRecoveryApplyQuarantinesAndClearsLegacyPendingMarker(t *testing.T) {
	root := newRecoveryRoot(t)
	writeActiveRuntimeBytes(t, root, []byte(`{"runtime":"corrupt-with-pending"}`))
	pendingPath := filepath.Join(root, ".claude", "loop-state.json.commit-pending.json")
	pendingBytes := []byte(`{"legacy":"pending-commit"}`)
	if err := os.WriteFile(pendingPath, pendingBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	planPath := createRecoveryPlan(t, root)

	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 {
		t.Fatalf("apply with legacy pending marker failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatalf("legacy pending marker remains after recovery: %v", err)
	}
	contained, err := recoveryTreeContains(root, pendingBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !contained {
		t.Fatal("legacy pending marker bytes were not quarantined")
	}
	code, stdout, stderr = runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 || !strings.Contains(stdout+stderr, "LOOP_RECOVERY_ALREADY_APPLIED") {
		t.Fatalf("same pending-source plan must remain idempotent: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestRuntimeRecoveryApplyResumesFromDurableMarkerBeforeReadingCorruptPlanOrDriftedInputs(t *testing.T) {
	root := newRecoveryRoot(t)
	writeActiveRuntimeBytes(t, root, []byte(`{"runtime":"corrupt-before-marker-only-retry"}`))
	planPath := createRecoveryPlan(t, root)
	plan := readJSON(t, planPath)
	candidateStatePath := filepath.Join(root, filepath.FromSlash(plan["candidate_state_path"].(string)))
	candidateJournalPath := filepath.Join(root, filepath.FromSlash(plan["candidate_journal_path"].(string)))
	candidateState, err := os.ReadFile(candidateStatePath)
	if err != nil {
		t.Fatal(err)
	}
	candidateJournal, err := os.ReadFile(candidateJournalPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:                   root,
		StatePath:              filepath.Join(root, ".claude", "loop-state.json"),
		JournalPath:            filepath.Join(root, ".claude", "loop-events.jsonl"),
		PlanID:                 plan["plan_id"].(string),
		PlanSHA:                plan["document_sha256"].(string),
		CandidateState:         candidateState,
		CandidateJournal:       candidateJournal,
		CandidateStateSHA256:   plan["candidate_state_sha256"].(string),
		CandidateJournalSHA256: plan["candidate_journal_sha256"].(string),
		Approver:               "recovery-owner",
		OccurredAt:             time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		Validator:              semantic.RuntimeCandidateValidator{},
		FailureInjector: recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
			if step == runtime.RecoveryBeforeStateReplace {
				return errors.New("simulated process crash after durable marker")
			}
			return nil
		}),
	})
	if !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("pending recovery setup error = %v, want injected failure", err)
	}
	if err := os.WriteFile(planPath, []byte(`{"truncated":`), 0o600); err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(root, recoveryReqPath)
	reqBytes, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, append(reqBytes, []byte("\nchanged after pending marker\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 {
		t.Fatalf("marker-only CLI retry failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "loop-state.json.recovery-pending.json")); !os.IsNotExist(err) {
		t.Fatalf("recovery marker remains after CLI retry: %v", err)
	}
}

// TestRuntimeRecoveryPlanDoesNotJumpFromLateArtifacts is the RED contract for
// RR-005, RR-006 and RR-011. Late ACC/BUG files without prerequisite evidence
// must not be treated as proof of S9/S10 progress.
func TestRuntimeRecoveryPlanDoesNotJumpFromLateArtifacts(t *testing.T) {
	root := newRecoveryRoot(t)
	writeActiveRuntimeBytes(t, root, []byte(`{"runtime":"corrupt"}`))
	writeRecoveryFile(t, root, "docs/reports/acceptance/ACC-039.md", "# ACC-039\n\n> Status: draft\n")
	writeRecoveryFile(t, root, "docs/reports/bugs/BUG-039-99.md", "# BUG-039-99\n\n> Status: accepted\n")

	planPath := createRecoveryPlan(t, root)
	plan := readJSON(t, planPath)
	stage, ok := targetStage(plan)
	if !ok {
		t.Fatalf("recovery plan must expose target_cursor: %#v", plan)
	}
	if stage >= 9 {
		t.Fatalf("late ACC/BUG files without prerequisite evidence must not jump to S9 or later: target=%v plan=%#v", stage, plan)
	}
	if stage != 2 {
		t.Fatalf("REQ-only recovery should conservatively seed planning.design (S2), got target S%d", stage)
	}
}

func TestRuntimeRecoveryPlanJournalsImportedProjection(t *testing.T) {
	root := newRecoveryRoot(t)
	writeActiveRuntimeBytes(t, root, []byte(`{"runtime":"corrupt-before-import-audit"}`))
	planPath := createRecoveryPlan(t, root)
	plan := readJSON(t, planPath)
	journalPath := filepath.Join(root, filepath.FromSlash(plan["candidate_journal_path"].(string)))
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range bytes.Split(bytes.TrimSpace(journal), []byte("\n")) {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode candidate journal event: %v", err)
		}
		message, _ := event["message"].(string)
		if event["event"] == "milestone_refreshed" && strings.Contains(message, "Recovery projection") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("candidate journal does not audit imported recovery projection: %s", journal)
	}
}

func TestRuntimeRecoveryPlanReplaysTrustedPlanningEvidence(t *testing.T) {
	root := newRecoveryRoot(t)
	writeActiveRuntimeBytes(t, root, []byte(`{"runtime":"corrupt-before-replay"}`))
	documents := []struct {
		path           string
		kind           string
		responsibility string
		id             string
		content        string
	}{
		{
			path: "docs/design/architecture/ARCHITECTURE-039-recovery.md", kind: "planning_design",
			responsibility: "Architect", id: "ev-recovery-design",
			content: "# Architecture\n\n> REQ: REQ-039\n> Status: locked\n> Version: v1.0.0\n",
		},
		{
			path: "docs/contracts/CONTRACTS-039-recovery.md", kind: "planning_contract",
			responsibility: "Contract Planner", id: "ev-recovery-contract",
			content: "# Contracts\n\n> REQ: REQ-039\n> Status: locked\n> Version: v1.0.0\n\n" +
				"## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:--|:--|:--|:--|\n| REQ-039 | — | BE-039-RECOVERY §1 | — |\n",
		},
		{
			path: "docs/tasks/TASK-039-01.md", kind: "planning_task",
			responsibility: "Task Planner", id: "ev-recovery-task",
			content: "# Task\n\n> REQ: REQ-039\n> Status: complete\n> Version: v1.0.0\n> Primary contract: BE-039-RECOVERY\n\n" +
				"## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039-RECOVERY | §1 |\n\n" +
				"## 7. Closing Contract\n\n```text\nassert BE-039-RECOVERY §1 == satisfied\n```\n",
		},
	}
	for _, document := range documents {
		writeRecoveryFile(t, root, document.path, document.content)
		writeTrustedPlanningEvidence(t, root, document.id, document.kind, document.responsibility, document.path, "v1.0.0")
	}
	// The batch-quality guard (tasks_checked) resolves the primary contract on
	// disk and reconciles it against the index clause cell (L3-S4 v4.0.1).
	writeRecoveryFile(t, root, "docs/contracts/BE-039-RECOVERY.md",
		"# BE-039-RECOVERY\n\n> REQ: REQ-039\n> Status: locked\n> Version: v1.0.0\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n")

	planPath := createRecoveryPlan(t, root)
	plan := readJSON(t, planPath)
	stage, ok := targetStage(plan)
	if !ok || stage != 5 {
		t.Fatalf("trusted planning evidence must replay to document_verification (S5), target=%v plan=%#v", stage, plan)
	}
	if plan["confidence"] != "formal_replay" {
		t.Fatalf("replayed plan confidence = %v, want formal_replay", plan["confidence"])
	}
	candidatePath, _ := plan["candidate_state_path"].(string)
	candidate := decodeStateFile(t, filepath.Join(root, filepath.FromSlash(candidatePath)))
	lifecycle, _ := candidate["lifecycle"].(map[string]any)
	if lifecycle["state"] != "document_verification" {
		t.Fatalf("candidate lifecycle = %#v, want document_verification", lifecycle)
	}
}

// TestRuntimeRecoveryApplyNeverRevivesActiveAgents is the RED contract for
// RR-008. A damaged but parseable Runtime containing activated/working Agents
// must never make those transient leases active in the new epoch.
func TestRuntimeRecoveryApplyNeverRevivesActiveAgents(t *testing.T) {
	root := newRecoveryRoot(t)
	state := req039fixtures.BaseState(t, root, "planning", "design", 7)
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "agent-live", "role": "builder", "state": "working",
			"task_ids": []any{}, "team_id": nil,
			"definition_ref": "docs/agents/agent-live.json",
			"prompt_ref":     "docs/agents/agent-live.prompt.md",
			"readback_ref":   nil, "activation_ref": nil,
			"activation_revision": 7, "updated_at": "2026-08-13T00:00:00Z",
		}},
		"tasks": []any{}, "bugs": []any{}, "teams": []any{},
	}
	// Keep the JSON parseable while making the active projection schema-invalid;
	// this exercises recovery from a damaged state that still exposes stale
	// Agent records to the inventory phase.
	state["corrupt_marker"] = "manual-edit"
	req039fixtures.WriteState(t, root, state)

	planPath := createRecoveryPlan(t, root)
	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 {
		t.Fatalf("apply must recover stale Agent records: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	recovered := decodeStateFile(t, filepath.Join(root, ".claude", "loop-state.json"))
	entities, _ := recovered["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		state, _ := agent["state"].(string)
		if state == "activated" || state == "working" {
			t.Fatalf("recovery revived active Agent state %q: %#v", state, agent)
		}
	}
}

// TestRuntimeRecoveryApplyRejectsREQInputDriftWithoutMutation is the RED
// contract for RR-003 and LOOP_RECOVERY_INPUT_DRIFT. Changing the explicit REQ
// after plan creation must supersede the plan and leave corrupt active bytes
// untouched.
func TestRuntimeRecoveryApplyRejectsREQInputDriftWithoutMutation(t *testing.T) {
	root := newRecoveryRoot(t)
	corrupt := []byte(`{"runtime":"corrupt-before-drift"}`)
	writeActiveRuntimeBytes(t, root, corrupt)
	planPath := createRecoveryPlan(t, root)

	reqPath := filepath.Join(root, recoveryReqPath)
	reqBytes, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, append(reqBytes, []byte("\nchanged after plan\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code == 0 {
		t.Fatalf("apply must reject changed plan input: stdout=%s stderr=%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "LOOP_RECOVERY_INPUT_DRIFT") {
		t.Fatalf("apply must report LOOP_RECOVERY_INPUT_DRIFT, got stdout=%s stderr=%s", stdout, stderr)
	}
	active, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(active, corrupt) {
		t.Fatal("input drift must not overwrite the corrupt active Runtime")
	}
}

func TestRuntimeRecoveryApplyRejectsNewArtifactAfterPlan(t *testing.T) {
	root := newRecoveryRoot(t)
	corrupt := []byte(`{"runtime":"corrupt-before-new-artifact"}`)
	writeActiveRuntimeBytes(t, root, corrupt)
	planPath := createRecoveryPlan(t, root)
	writeRecoveryFile(t, root, "docs/design/architecture/ARCHITECTURE-039-late.md", "# Late\n\n> REQ: REQ-039\n> Status: locked\n> Version: v1.0.0\n")

	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code == 0 || !strings.Contains(stdout+stderr, "LOOP_RECOVERY_INPUT_DRIFT") {
		t.Fatalf("new artifact must supersede stale plan: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	active, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(active, corrupt) {
		t.Fatal("new artifact drift must not overwrite the corrupt active Runtime")
	}
}

func TestRuntimeRecoveryApplyRejectsActiveStateCreatedAfterMissingSourcePlan(t *testing.T) {
	root := newRecoveryRoot(t)
	planPath := createRecoveryPlan(t, root)
	createdAfterPlan := []byte(`{"runtime":"unexpected-created-source"}`)
	writeActiveRuntimeBytes(t, root, createdAfterPlan)

	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code == 0 || !strings.Contains(stdout+stderr, "LOOP_RECOVERY_INPUT_DRIFT") {
		t.Fatalf("new active state must supersede a missing-source plan: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	active, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(active, createdAfterPlan) {
		t.Fatal("missing-source drift must not overwrite the newly created active Runtime")
	}
}

// TestRuntimeRecoveryApplySamePlanIsIdempotent is the RED contract for RR-009
// and LOOP_RECOVERY_ALREADY_APPLIED. A retry with the same approved plan must
// return the existing result without a second Runtime epoch or journal event.
func TestRuntimeRecoveryApplySamePlanIsIdempotent(t *testing.T) {
	root := newRecoveryRoot(t)
	writeActiveRuntimeBytes(t, root, []byte(`{"runtime":"corrupt-idempotency"}`))
	planPath := createRecoveryPlan(t, root)

	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 {
		t.Fatalf("first apply failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	stateBefore, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	treeBefore, err := recoveryTreeSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr = runRecovery(t, root,
		"runtime", "recover", "apply",
		"--root", root, "--plan", planPath, "--approved-by", "recovery-owner",
	)
	if code != 0 {
		t.Fatalf("second identical apply must be idempotent: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "LOOP_RECOVERY_ALREADY_APPLIED") {
		t.Fatalf("second apply must report LOOP_RECOVERY_ALREADY_APPLIED, got stdout=%s stderr=%s", stdout, stderr)
	}
	stateAfter, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	treeAfter, err := recoveryTreeSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Fatal("idempotent apply changed active Runtime bytes")
	}
	if !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("idempotent apply appended or changed the recovery journal")
	}
	if !equalStringBytesMaps(treeBefore, treeAfter) {
		t.Fatal("idempotent apply changed the recovery plan/quarantine tree")
	}
}

func newRecoveryRoot(t *testing.T) string {
	t.Helper()
	root := req039fixtures.FreshRoot(t)
	writeRecoveryFile(t, root, recoveryReqPath, "# REQ-039\n\n> 状态：locked\n> 版本：v2.0.0\n> UI impact：none\n")
	return root
}

func writeRecoveryFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeActiveRuntimeBytes(t *testing.T, root string, data []byte) {
	t.Helper()
	path := filepath.Join(root, ".claude", "loop-state.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write corrupt Runtime: %v", err)
	}
}

func writeTrustedPlanningEvidence(t *testing.T, root, id, kind, responsibility, subjectPath, version string) {
	t.Helper()
	subject, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(subjectPath)))
	if err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             id,
		"kind":                    kind,
		"runtime_id":              "loop-REQ-039",
		"baseline_generation":     1,
		"review_round":            1,
		"producer_agent_id":       "recovery-" + id,
		"producer_responsibility": responsibility,
		"subject_refs": []any{map[string]any{
			"path": subjectPath, "version": version, "sha256": sha256Hex(subject),
		}},
		"conclusion": "pass",
		"created_at": "2026-08-13T00:00:00Z",
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	writeRecoveryFile(t, root, filepath.ToSlash(filepath.Join(".claude", "evidence", id+".json")), string(data))
}

func runRecovery(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, bytes.NewReader(nil), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func createRecoveryPlan(t *testing.T, root string) string {
	t.Helper()
	code, stdout, stderr := runRecovery(t, root,
		"runtime", "recover", "plan",
		"--root", root, "--req", recoveryReqPath,
	)
	if code != 0 {
		t.Fatalf("recovery plan failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	return recoveryPlanPath(t, root)
}

func recoveryPlanPath(t *testing.T, root string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, ".claude", "recovery", "*", "plan.json"))
	if err != nil {
		t.Fatalf("glob recovery plans: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want exactly one recovery plan, got %d (%v)", len(paths), paths)
	}
	return paths[0]
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSON %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode JSON %s: %v", path, err)
	}
	return value
}

func decodeState(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode recovered Runtime: %v", err)
	}
	return state
}

func decodeStateFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeState(t, data)
}

func planReferencesBytes(t *testing.T, root string, plan map[string]any, want []byte) bool {
	t.Helper()
	wantHash := sha256Hex(want)
	if recursiveContainsString(plan, wantHash) || recursiveContainsString(plan, "loop-state.json") {
		// The plan must contain both the damaged artifact identity and the
		// recovery source path; the exact bytes are checked at apply time.
		return recursiveContainsString(plan, wantHash)
	}
	contained, err := recoveryTreeContains(root, want)
	if err != nil {
		t.Fatalf("scan recovery tree for damaged bytes: %v", err)
	}
	return contained
}

func targetStage(plan map[string]any) (int, bool) {
	for _, key := range []string{"target_cursor", "targetCursor", "cursor"} {
		if value, ok := plan[key]; ok {
			if stage, ok := stageFromValue(value); ok {
				return stage, true
			}
		}
	}
	return 0, false
}

func stageFromValue(value any) (int, bool) {
	switch typed := value.(type) {
	case string:
		return parseStage(typed)
	case map[string]any:
		for _, key := range []string{"stage", "cursor", "phase", "lifecycle_phase", "target_cursor"} {
			if child, ok := typed[key]; ok {
				if stage, ok := stageFromValue(child); ok {
					return stage, true
				}
			}
		}
	}
	return 0, false
}

func parseStage(value string) (int, bool) {
	value = strings.ToLower(value)
	// Check specific cursors before their substrings (for example,
	// document_verification before verification) and keep ordering stable.
	for _, cursor := range []struct {
		phase string
		stage int
	}{
		{"planning.design", 2}, {"planning.contracts", 3}, {"planning.tasks", 4},
		{"document_verification", 5}, {"building", 6}, {"verification", 7},
		{"bug_resolution", 8}, {"acceptance", 10}, {"release_audit", 10},
		{"awaiting_human_release", 11},
	} {
		if strings.Contains(value, cursor.phase) {
			return cursor.stage, true
		}
	}
	for i := 0; i+1 < len(value); i++ {
		if value[i] != 's' || value[i+1] < '0' || value[i+1] > '9' {
			continue
		}
		j := i + 1
		for j < len(value) && value[j] >= '0' && value[j] <= '9' {
			j++
		}
		var stage int
		if _, err := fmt.Sscanf(value[i+1:j], "%d", &stage); err == nil {
			return stage, true
		}
	}
	return 0, false
}

func recoveryTreeContains(root string, want []byte) (bool, error) {
	found := false
	err := filepath.WalkDir(filepath.Join(root, ".claude", "recovery"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if found || entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(data, want) {
			found = true
		}
		return nil
	})
	if os.IsNotExist(err) {
		return false, nil
	}
	return found, err
}

func recoveryTreeSnapshot(root string) (map[string][]byte, error) {
	snapshot := map[string][]byte{}
	base := filepath.Join(root, ".claude", "recovery")
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(rel)] = append([]byte(nil), data...)
		return nil
	})
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	return snapshot, err
}

func equalStringBytesMaps(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	keys := make([]string, 0, len(left))
	for key := range left {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !bytes.Equal(left[key], right[key]) {
			return false
		}
	}
	return true
}

func recursiveContainsString(value any, want string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, want)
	case map[string]any:
		for _, child := range typed {
			if recursiveContainsString(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if recursiveContainsString(child, want) {
				return true
			}
		}
	}
	return false
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type recoveryFailureFunc func(runtime.RecoveryFailureStep) error

func (f recoveryFailureFunc) Inject(step runtime.RecoveryFailureStep) error {
	return f(step)
}
