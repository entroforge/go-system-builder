// Package assignment_test covers TASK-015 RegisterBug / RegisterTask
// runtime contracts and the GTR-004 repair-limit bridge.
//
// Closing Contract coverage:
//   - register_bug_dedup_by_finding_fingerprint
//   - register_bug_invalid_severity_rejected
//   - register_bug_missing_evidence_rejected
//   - register_task_missing_path_or_owner_rejected
//   - register_task_duplicate_id_rejected
//   - repair_limit_bridge_structurally_defined_AND_unit_tested
package assignment_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/adapter"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// seedRuntimeBugBatchEvidence indexes a valid bug_batch_record at
// id=runtime:bug:<bugID> so DispatchRepairLimitExceeded → GTR-004 can pass
// validateCurrentEvidence after BUG-039-23 (bug_batch_record is current
// evidence, not a generated kind).
func seedRuntimeBugBatchEvidence(t *testing.T, root, statePath, bugID string) {
	t.Helper()
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	evID := "runtime:bug:" + bugID
	// Align review.round with evidence.review_round (schema min=1).
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             evID,
		"kind":                    "bug",
		"runtime_id":              state["runtime_id"],
		"baseline_generation":     1,
		"review_round":            1,
		"producer_agent_id":       "orchestrator-1",
		"producer_responsibility": "Orchestrator",
		"conclusion":              "accepted",
		"created_at":              "2026-07-30T00:00:00Z",
	}
	data, _ := json.Marshal(envelope)
	rel := filepath.Join("evidence", bugID+"-batch.json")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	entry := map[string]any{
		"id":                  evID,
		"kind":                "bug",
		"path":                rel,
		"sha256":              hex.EncodeToString(sum[:]),
		"status":              "valid",
		"baseline_generation": 1,
		"review_round":        1,
		"produced_by":         []any{"orchestrator-1"},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "Orchestrator",
		"scope_refs":          []any{},
	}
	ev, _ := state["evidence"].([]any)
	state["evidence"] = append(ev, entry)
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// freshState builds a minimal but schema-valid runtime state in a temp
// directory and returns (root, statePath, journalPath). The loop must be in
// a state where RegisterBug/RegisterTask can operate; bug_resolution and
// planning are both safe choices.
func freshState(t *testing.T, state, phase string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	// Copy loop-definition.json (the registry lookups depend on it).
	defSrc := filepath.Join("..", "..", "docs", "loop-definition.json")
	defData, err := os.ReadFile(defSrc)
	if err != nil {
		t.Fatal(err)
	}
	defDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defDir, "loop-definition.json"), defData, 0o644); err != nil {
		t.Fatal(err)
	}
	exampleData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state0 map[string]any
	if err := json.Unmarshal(exampleData, &state0); err != nil {
		t.Fatal(err)
	}
	state0["runtime_id"] = "loop-REQ-002-test"
	state0["revision"] = 0
	state0["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs":   []any{},
		"teams":  []any{},
	}
	lifecycle := state0["lifecycle"].(map[string]any)
	lifecycle["state"] = state
	lifecycle["phase"] = phase
	lifecycle["phase_revision"] = 0
	state0["lifecycle"] = lifecycle
	// Ensure journal object is present (example already has it; defensive).
	state0["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(state0, "", "  ")
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, statePath, journalPath
}

func writeFindingFile(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "reports", "bugs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "FIND-TEST.md")
	if err := os.WriteFile(path, []byte("# Finding\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return "docs/reports/bugs/FIND-TEST.md"
}

func writeEvidenceFile(t *testing.T, root, rel string) string {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("# Evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestRegisterBugHappyPath(t *testing.T) {
	root, statePath, journalPath := freshState(t, "bug_resolution", "investigation")
	finding := writeFindingFile(t, root)
	ev := writeEvidenceFile(t, root, "docs/reports/review/EV-A.md")

	snap, err := assignment.RegisterBug(root, statePath, journalPath, assignment.RegisterBugRequest{
		ExpectedRevision: 0,
		BugID:            "BUG-100",
		Severity:         "P0",
		FindingSource:    finding,
		EvidenceRefs:     []string{ev},
		ReporterAgentID:  "agent-dv",
	})
	if err != nil {
		t.Fatalf("RegisterBug happy path failed: %v", err)
	}
	bugs, _ := snap.State["entities"].(map[string]any)["bugs"].([]any)
	if len(bugs) != 1 {
		t.Fatalf("expected 1 BUG, got %d", len(bugs))
	}
	bug := bugs[0].(map[string]any)
	if bug["id"] != "BUG-100" {
		t.Errorf("BUG id = %v; want BUG-100", bug["id"])
	}
	if bug["state"] != "draft" {
		t.Errorf("BUG state = %v; want draft", bug["state"])
	}
	if bug["severity"] != "P0" {
		t.Errorf("BUG severity = %v; want P0", bug["severity"])
	}
}

func TestRegisterBugDedupByFindingFingerprint(t *testing.T) {
	root, statePath, journalPath := freshState(t, "bug_resolution", "investigation")
	finding := writeFindingFile(t, root)
	ev := writeEvidenceFile(t, root, "docs/reports/review/EV-A.md")

	if _, err := assignment.RegisterBug(root, statePath, journalPath, assignment.RegisterBugRequest{
		ExpectedRevision: 0,
		BugID:            "BUG-100",
		Severity:         "P0",
		FindingSource:    finding,
		EvidenceRefs:     []string{ev},
		ReporterAgentID:  "agent-dv",
	}); err != nil {
		t.Fatal(err)
	}
	// Second registration with the same fingerprint must fail with ErrDuplicateBug.
	snap, err := assignment.RegisterBug(root, statePath, journalPath, assignment.RegisterBugRequest{
		ExpectedRevision: 1,
		BugID:            "BUG-101",
		Severity:         "P1",
		FindingSource:    finding,
		EvidenceRefs:     []string{ev},
		ReporterAgentID:  "agent-dv",
	})
	if err == nil {
		t.Fatalf("expected ErrDuplicateBug, got snapshot revision=%d state=%v", snap.Revision, snap.State)
	}
	var dup *assignment.ErrDuplicateBug
	if !errors.As(err, &dup) {
		t.Fatalf("expected *ErrDuplicateBug, got %T: %v", err, err)
	}
	if dup.ExistingBugID != "BUG-100" {
		t.Errorf("ExistingBugID = %q; want BUG-100", dup.ExistingBugID)
	}
}

func TestRegisterBugInvalidSeverityRejected(t *testing.T) {
	root, statePath, journalPath := freshState(t, "bug_resolution", "investigation")
	finding := writeFindingFile(t, root)
	ev := writeEvidenceFile(t, root, "docs/reports/review/EV-A.md")
	_, err := assignment.RegisterBug(root, statePath, journalPath, assignment.RegisterBugRequest{
		ExpectedRevision: 0,
		BugID:            "BUG-100",
		Severity:         "CRITICAL", // not in {P0..P3}
		FindingSource:    finding,
		EvidenceRefs:     []string{ev},
		ReporterAgentID:  "agent-dv",
	})
	if err == nil {
		t.Fatal("expected ErrInvalidBugSeverity")
	}
	var ise *assignment.ErrInvalidBugSeverity
	if !errors.As(err, &ise) {
		t.Fatalf("expected *ErrInvalidBugSeverity, got %T: %v", err, err)
	}
	if ise.Severity != "CRITICAL" {
		t.Errorf("ErrInvalidBugSeverity.Severity = %q; want CRITICAL", ise.Severity)
	}
}

func TestRegisterBugMissingEvidenceRejected(t *testing.T) {
	root, statePath, journalPath := freshState(t, "bug_resolution", "investigation")
	finding := writeFindingFile(t, root)
	_, err := assignment.RegisterBug(root, statePath, journalPath, assignment.RegisterBugRequest{
		ExpectedRevision: 0,
		BugID:            "BUG-100",
		Severity:         "P0",
		FindingSource:    finding,
		EvidenceRefs:     []string{}, // empty
		ReporterAgentID:  "agent-dv",
	})
	if err == nil {
		t.Fatal("expected ErrMissingEvidence")
	}
	var me *assignment.ErrMissingEvidence
	if !errors.As(err, &me) {
		t.Fatalf("expected *ErrMissingEvidence, got %T: %v", err, err)
	}

	// Also test missing-on-disk evidence.
	_, err = assignment.RegisterBug(root, statePath, journalPath, assignment.RegisterBugRequest{
		ExpectedRevision: 0,
		BugID:            "BUG-100",
		Severity:         "P0",
		FindingSource:    finding,
		EvidenceRefs:     []string{"docs/reports/review/DOES-NOT-EXIST.md"},
		ReporterAgentID:  "agent-dv",
	})
	if err == nil {
		t.Fatal("expected ErrMissingEvidence for non-existent file")
	}
	if !errors.As(err, &me) {
		t.Fatalf("expected *ErrMissingEvidence for missing file, got %T: %v", err, err)
	}
}

func TestRegisterTaskHappyPath(t *testing.T) {
	root, statePath, journalPath := freshState(t, "planning", "tasks")
	taskRel := "docs/tasks/TASK-100.md"
	if err := os.MkdirAll(filepath.Join(root, "docs/tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskRel), []byte("# TASK-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Register an agent first so the owner_agent_ids check passes. Re-write
	// the state with a single seeded agent while keeping the journal block.
	stateBytes, _ := os.ReadFile(statePath)
	var s map[string]any
	json.Unmarshal(stateBytes, &s)
	s["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id":                  "agent-builder-1",
			"role":                "builder",
			"state":               "reading",
			"task_ids":            []any{},
			"team_id":             nil,
			"definition_ref":      "agents/builder.md",
			"prompt_ref":          "manifest#assignment-1",
			"readback_ref":        nil,
			"activation_ref":      nil,
			"activation_revision": nil,
			"updated_at":          "2026-06-20T00:00:00Z",
		}},
		"tasks": []any{},
		"bugs":  []any{},
		"teams": []any{},
	}
	s["revision"] = 0
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(statePath, append(data, '\n'), 0o644)

	snap, err := assignment.RegisterTask(root, statePath, journalPath, assignment.RegisterTaskRequest{
		ExpectedRevision: 0,
		TaskID:           "TASK-100",
		Path:             taskRel,
		OwnerAgentIDs:    []string{"agent-builder-1"},
		SourceContractID: "BUG-003",
	})
	if err != nil {
		t.Fatalf("RegisterTask happy path failed: %v", err)
	}
	tasks, _ := snap.State["entities"].(map[string]any)["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 TASK, got %d", len(tasks))
	}
}

func TestRegisterTaskMissingPathOrOwnerRejected(t *testing.T) {
	root, statePath, journalPath := freshState(t, "planning", "tasks")
	// Empty Path.
	_, err := assignment.RegisterTask(root, statePath, journalPath, assignment.RegisterTaskRequest{
		ExpectedRevision: 0,
		TaskID:           "TASK-100",
		Path:             "",
		OwnerAgentIDs:    []string{"agent-builder-1"},
		SourceContractID: "BUG-003",
	})
	if err == nil {
		t.Fatal("expected ErrMissingTaskPath for empty path")
	}
	var mp *assignment.ErrMissingTaskPath
	if !errors.As(err, &mp) {
		t.Fatalf("expected *ErrMissingTaskPath, got %T: %v", err, err)
	}

	// Non-existent Path.
	_, err = assignment.RegisterTask(root, statePath, journalPath, assignment.RegisterTaskRequest{
		ExpectedRevision: 0,
		TaskID:           "TASK-100",
		Path:             "docs/tasks/DOES-NOT-EXIST.md",
		OwnerAgentIDs:    []string{"agent-builder-1"},
		SourceContractID: "BUG-003",
	})
	if !errors.As(err, &mp) {
		t.Fatalf("expected *ErrMissingTaskPath for missing file, got %T: %v", err, err)
	}

	// Empty OwnerAgentIDs.
	taskRel := "docs/tasks/TASK-100.md"
	if err := os.MkdirAll(filepath.Join(root, "docs/tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskRel), []byte("# TASK-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = assignment.RegisterTask(root, statePath, journalPath, assignment.RegisterTaskRequest{
		ExpectedRevision: 0,
		TaskID:           "TASK-100",
		Path:             taskRel,
		OwnerAgentIDs:    []string{},
		SourceContractID: "BUG-003",
	})
	if err == nil {
		t.Fatal("expected ErrMissingTaskOwner")
	}
	var mo *assignment.ErrMissingTaskOwner
	if !errors.As(err, &mo) {
		t.Fatalf("expected *ErrMissingTaskOwner, got %T: %v", err, err)
	}
}

func TestRegisterTaskDuplicateIDRejected(t *testing.T) {
	root, statePath, journalPath := freshState(t, "planning", "tasks")
	taskRel := "docs/tasks/TASK-100.md"
	if err := os.MkdirAll(filepath.Join(root, "docs/tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskRel), []byte("# TASK-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed a builder agent so the owner check passes; preserve journal.
	stateBytes, _ := os.ReadFile(statePath)
	var s map[string]any
	json.Unmarshal(stateBytes, &s)
	s["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id":                  "agent-builder-1",
			"role":                "builder",
			"state":               "reading",
			"task_ids":            []any{},
			"team_id":             nil,
			"definition_ref":      "agents/builder.md",
			"prompt_ref":          "manifest#assignment-1",
			"readback_ref":        nil,
			"activation_ref":      nil,
			"activation_revision": nil,
			"updated_at":          "2026-06-20T00:00:00Z",
		}},
		"tasks": []any{},
		"bugs":  []any{},
		"teams": []any{},
	}
	s["revision"] = 0
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(statePath, append(data, '\n'), 0o644)

	if _, err := assignment.RegisterTask(root, statePath, journalPath, assignment.RegisterTaskRequest{
		ExpectedRevision: 0,
		TaskID:           "TASK-100",
		Path:             taskRel,
		OwnerAgentIDs:    []string{"agent-builder-1"},
		SourceContractID: "BUG-003",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := assignment.RegisterTask(root, statePath, journalPath, assignment.RegisterTaskRequest{
		ExpectedRevision: 1,
		TaskID:           "TASK-100",
		Path:             taskRel,
		OwnerAgentIDs:    []string{"agent-builder-1"},
		SourceContractID: "BUG-003",
	})
	if err == nil {
		t.Fatal("expected ErrDuplicateTaskID on duplicate task id")
	}
	var dup *assignment.ErrDuplicateTaskID
	if !errors.As(err, &dup) {
		t.Fatalf("expected *ErrDuplicateTaskID, got %T: %v", err, err)
	}
}

// TestRepairLimitBridgeFiresAboveThreshold is the structural unit test for
// the GTR-004 bridge. It exercises CheckRepairLimit + DispatchRepairLimitExceeded
// end-to-end and verifies:
//  1. Below threshold: CheckRepairLimit returns nil.
//  2. At threshold: CheckRepairLimit returns *RepairLimitError.
//  3. DispatchRepairLimitExceeded dispatches transition.Apply(GTR-004)
//     which transitions the runtime to paused with capture_pause_checkpoint.
//  4. The bridge never dispatches when err is not a *RepairLimitError.
func TestRepairLimitBridgeFiresAboveThreshold(t *testing.T) {
	root := t.TempDir()
	// Copy loop-definition.json so the catalog can load.
	defSrc := filepath.Join("..", "..", "docs", "loop-definition.json")
	defData, err := os.ReadFile(defSrc)
	if err != nil {
		t.Fatal(err)
	}
	defDir := filepath.Join(root, "docs")
	os.MkdirAll(defDir, 0o755)
	os.WriteFile(filepath.Join(defDir, "loop-definition.json"), defData, 0o644)

	// Build a runtime in bug_resolution.fixing with a BUG whose
	// attempt_count equals the configured limit.
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	os.MkdirAll(filepath.Dir(statePath), 0o755)
	exampleData, _ := schema.ReadAsset("loop-state.example.json")
	var state0 map[string]any
	json.Unmarshal(exampleData, &state0)
	state0["runtime_id"] = "loop-REQ-002-test"
	state0["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "fixing", "phase_revision": 0}
	state0["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs": []any{
			map[string]any{
				"id":                          "BUG-007",
				"state":                       "fixed",
				"path":                        "docs/reports/bugs/BUG-007.md",
				"severity":                    "P0",
				"attempt_count":               float64(3),
				"same_contract_failure_count": float64(0),
				"original_finder_agent_ids":   []any{"agent-finder"},
			},
		},
		"teams": []any{},
	}
	state0["configuration"] = map[string]any{
		"repair": map[string]any{
			"max_attempts_per_bug":       float64(3),
			"max_same_contract_failures": float64(2),
			"max_full_review_rounds":     float64(5),
		},
	}
	state0["revision"] = 4
	state0["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	data, _ := json.MarshalIndent(state0, "", "  ")
	os.WriteFile(statePath, append(data, '\n'), 0o644)
	os.WriteFile(journalPath, []byte{}, 0o644)

	// Step 1: Below threshold returns nil.
	belowBug := map[string]any{
		"id":            "BUG-007",
		"attempt_count": float64(2),
	}
	if rle := transition.CheckRepairLimit(state0, belowBug); rle != nil {
		t.Errorf("CheckRepairLimit at attempts=2 should return nil, got %v", rle)
	}

	// Step 2: At threshold returns *RepairLimitError.
	atBug := map[string]any{
		"id":            "BUG-007",
		"attempt_count": float64(3),
	}
	rle := transition.CheckRepairLimit(state0, atBug)
	if rle == nil {
		t.Fatal("CheckRepairLimit at attempts=3 should return *RepairLimitError")
	}
	if rle.BugID != "BUG-007" || rle.Attempts != 3 || rle.Max != 3 {
		t.Errorf("RepairLimitError = %+v; want BUG-007/3/3", rle)
	}

	// Step 3: DispatchRepairLimitExceeded routes to transition.Apply(GTR-004).
	// BUG-039-23: bug_batch_record must be indexed current evidence.
	seedRuntimeBugBatchEvidence(t, root, statePath, "BUG-007")
	snap, err := adapter.DispatchRepairLimitExceeded(root, statePath, journalPath, 4, rle)
	if err != nil {
		t.Fatalf("DispatchRepairLimitExceeded failed: %v", err)
	}
	if stateName, _ := snap.State["lifecycle"].(map[string]any)["state"].(string); stateName != "paused" {
		t.Errorf("expected lifecycle.state == paused, got %q", stateName)
	}
	if pause, ok := snap.State["pause"].(map[string]any); !ok || pause == nil {
		t.Errorf("expected pause checkpoint to be set, got %v", snap.State["pause"])
	}

	// Step 4: Non-RepairLimitError is returned unchanged (no dispatch).
	snap, err = adapter.DispatchRepairLimitExceeded(root, statePath, journalPath, snap.Revision, fmt.Errorf("unrelated"))
	if err == nil {
		t.Fatal("expected non-RepairLimitError to be returned unchanged")
	}
	if !strings.Contains(err.Error(), "unrelated") {
		t.Errorf("expected the original error message, got %v", err)
	}
	if snap.Revision != 0 {
		// No successful dispatch → no new snapshot.
		t.Errorf("expected zero snapshot when dispatch is skipped, got revision %d", snap.Revision)
	}

	// Step 5: nil err is a no-op.
	snap, err = adapter.DispatchRepairLimitExceeded(root, statePath, journalPath, 0, nil)
	if err != nil {
		t.Errorf("expected nil error for nil err input, got %v", err)
	}
	if snap.Revision != 0 || snap.State != nil {
		t.Errorf("expected zero snapshot for nil err input, got revision=%d state=%v", snap.Revision, snap.State)
	}
}
