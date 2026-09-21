package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// TestFinalAuditBugEventAnchorsDefaultRuntimePair proves that a bug-event
// issued from outside the authority root still reads and commits the default
// .claude/loop-state.json + .claude/loop-events.jsonl pair under --root.
// This is intentionally a CLI test: calling assignment.AdvanceBug directly
// would not cover the command's default path resolution.
func TestFinalAuditBugEventAnchorsDefaultRuntimePair(t *testing.T) {
	root := writeFinalAuditBugRuntime(t, "pending_approval", 4, 0, 2)
	outside := t.TempDir()
	t.Chdir(outside)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "bug-event", "--root", root,
		"--expected-revision", "4",
		"--bug-id", "BUG-900",
		"--event", "bug_accepted",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bug-event with default runtime paths failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Revision":5`) {
		t.Fatalf("bug-event did not return revision 5: %s", stdout.String())
	}

	state := readFinalAuditBugState(t, filepath.Join(root, ".claude", "loop-state.json"))
	bug := finalAuditBug(t, state, "BUG-900")
	if bug["state"] != "accepted" {
		t.Fatalf("root state was not updated by bug-event: %#v", bug)
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journal), `"transition_id":"BUG-LIFECYCLE"`) || !strings.Contains(string(journal), `"idempotency_key":"bug:BUG-900:bug_accepted:4"`) {
		t.Fatalf("root journal missing bug lifecycle commit: %s", journal)
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude", "loop-state.json")); !os.IsNotExist(err) {
		t.Fatalf("bug-event wrote a cwd-relative state pair: err=%v", err)
	}
}

// TestFinalAuditBugEventAnchorsRepairLimitBridge exercises the same default
// path contract through the typed repair-limit failure. The CLI must dispatch
// GTR-004 against the root-owned pair, leaving a paused runtime and its pause
// checkpoint in that pair.
func TestFinalAuditBugEventAnchorsRepairLimitBridge(t *testing.T) {
	root := writeFinalAuditBugRuntime(t, "retesting", 4, 1, 1)
	seedFinalAuditBugBatchEvidence(t, root)
	outside := t.TempDir()
	t.Chdir(outside)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "bug-event", "--root", root,
		"--expected-revision", "4",
		"--bug-id", "BUG-900",
		"--event", "closing_contract_failed",
		"--params", `{"failure_evidence":"docs/reports/bugs/BUG-900.md#failure"}`,
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("repair-limit bug-event unexpectedly succeeded: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "repair_limit_exceeded") || !strings.Contains(stderr.String(), "runtime paused via GTR-004") {
		t.Fatalf("repair-limit bridge output missing typed error/paused acknowledgement: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	state := readFinalAuditBugState(t, filepath.Join(root, ".claude", "loop-state.json"))
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "paused" {
		t.Fatalf("repair-limit bridge did not pause root runtime: %#v", lifecycle)
	}
	if pause, _ := state["pause"].(map[string]any); pause == nil {
		t.Fatalf("repair-limit bridge did not persist pause checkpoint: %#v", state["pause"])
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude", "loop-events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("repair-limit bridge unexpectedly wrote an unrelated cwd journal: err=%v", err)
	}
}

// TestFinalAuditBugEventValidatesRootAnchoredMessage keeps the CLI path seam
// on the normal regression path. run.go resolves --message against --root;
// the domain layer must consume that absolute path without joining root a
// second time, and must reject the malformed envelope before committing.
func TestFinalAuditBugEventValidatesRootAnchoredMessage(t *testing.T) {
	root := writeFinalAuditBugRuntime(t, "pending_approval", 4, 0, 2)
	messagePath := filepath.Join(root, "invalid-bug-message.json")
	if err := os.WriteFile(messagePath, []byte(`{"not":"an Agent message"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "bug-event", "--root", root,
		"--expected-revision", "4", "--bug-id", "BUG-900", "--event", "bug_accepted",
		"--message", messagePath,
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("invalid --message was accepted; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeState, afterState) {
		t.Fatal("invalid --message changed runtime state")
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("invalid --message appended a journal event")
	}
}

func writeFinalAuditBugRuntime(t *testing.T, bugState string, revision, sameContractFailures, maxSameContractFailures int) string {
	t.Helper()
	root := t.TempDir()
	definition, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-final-audit-bug"
	state["revision"] = float64(revision)
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "fixing", "phase_revision": 1}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs": []any{map[string]any{
			"id":                          "BUG-900",
			"state":                       bugState,
			"path":                        "docs/reports/bugs/BUG-900.md",
			"severity":                    "P1",
			"attempt_count":               float64(0),
			"same_contract_failure_count": float64(sameContractFailures),
			"original_finder_agent_ids":   []any{"agent-finder"},
		}},
		"teams": []any{},
	}
	state["configuration"] = map[string]any{"repair": map[string]any{
		"max_attempts_per_bug":       float64(3),
		"max_same_contract_failures": float64(maxSameContractFailures),
		"max_full_review_rounds":     float64(5),
	}}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func seedFinalAuditBugBatchEvidence(t *testing.T, root string) {
	t.Helper()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readFinalAuditBugState(t, statePath)
	const evidenceID = "runtime:bug:BUG-900"
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": evidenceID, "kind": "bug",
		"runtime_id": state["runtime_id"], "baseline_generation": 1, "review_round": 1,
		"producer_agent_id": "orchestrator-1", "producer_responsibility": "Orchestrator",
		"conclusion": "accepted", "created_at": "2026-07-30T00:00:00Z",
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("evidence", "BUG-900-batch.json")
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	entry := map[string]any{
		"id": evidenceID, "kind": "bug", "path": rel, "sha256": hex.EncodeToString(sum[:]),
		"status": "valid", "baseline_generation": 1, "review_round": 1,
		"produced_by": []any{"orchestrator-1"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": "Orchestrator", "scope_refs": []any{},
	}
	state["evidence"] = []any{entry}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFinalAuditBugState(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func finalAuditBug(t *testing.T, state map[string]any, id string) map[string]any {
	t.Helper()
	entities, _ := state["entities"].(map[string]any)
	bugs, _ := entities["bugs"].([]any)
	for _, raw := range bugs {
		bug, _ := raw.(map[string]any)
		if bug["id"] == id {
			return bug
		}
	}
	t.Fatalf("BUG %s missing from state", id)
	return nil
}
