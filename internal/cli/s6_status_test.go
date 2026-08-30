// s6_status_test.go pins the read-only board: it reflects the registered
// TR-003 batch, the latest completion envelope per task, and verified
// integration checkpoints — without writing anything.
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func s6Fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Registered batch: TASK-A and TASK-B at generation 1.
	state := map[string]any{
		"schema_version": "1.1.0",
		"runtime_id":     "loop-s6-status",
		"revision":       7,
		"lifecycle":      map[string]any{"state": "building", "phase": nil, "phase_revision": 0},
		"baseline":       map[string]any{"generation": 1, "captured_at": "2026-08-20T00:00:00Z"},
		"review":         map[string]any{"round": 0, "clean_round": nil},
		"documents": []any{
			map[string]any{"id": "TASK-A", "kind": "task", "path": "docs/tasks/TASK-A.md", "status": "complete", "generation": 1},
			map[string]any{"id": "TASK-B", "kind": "task", "path": "docs/tasks/TASK-B.md", "status": "complete", "generation": 1},
		},
		"entities": map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
		"evidence": []any{},
		"pause":    nil,
		"journal":  map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil},
	}

	// Completion envelope for TASK-A (registered with failing check).
	envelopeA := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-completion-TASK-A-g1", "kind": "completion_report",
		"runtime_id": "loop-s6-status", "baseline_generation": 1,
		"producer_agent_id": "builder-1", "producer_responsibility": "BUILD-WORK-PACKAGE",
		"conclusion": "completed", "task_id": "TASK-A",
		"checks": []any{
			map[string]any{"name": "unit", "command": "go test ./...", "result": "fail"},
		},
		"scope_deviations": []any{"internal/other/rogue.go"},
	}
	envABytes, _ := json.Marshal(envelopeA)
	envAPath := ".claude/evidence/loop-s6-status/g1/assignments/assignment-a/ev-completion-TASK-A-g1.json"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, envAPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, envAPath), append(envABytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	state["evidence"] = []any{
		map[string]any{
			"id": "ev-completion-TASK-A-g1", "kind": "completion_report", "path": envAPath,
			"sha256": sha256Hex(envABytes), "status": "valid", "baseline_generation": 1,
			"review_round": nil, "produced_by": []any{"builder-1"}, "invalidated_by": nil,
			"responsibility_id": "BUILD-WORK-PACKAGE", "scope_refs": []any{},
		},
	}

	// Verified checkpoint for TASK-A; TASK-B has none.
	checkpointDir := filepath.Join(root, ".claude", "evidence", "loop-s6-status", "g1", "worktree", "assignment-a")
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checkpoint := map[string]any{
		"assignment_id": "assignment-a", "task_id": "TASK-A",
		"state": "verified", "baseline_generation": 1,
	}
	checkpointBytes, _ := json.Marshal(checkpoint)
	if err := os.WriteFile(filepath.Join(checkpointDir, "checkpoint.json"), checkpointBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestS6StatusBoardReflectsBatch(t *testing.T) {
	root := s6Fixture(t)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s6 status failed: code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"S6 batch board (generation 1, 2 task(s) registered)",
		"TASK-A",
		"checks: unit=fail (not all pass)",
		"scope_deviations: internal/other/rogue.go",
		"integration: verified checkpoint present",
		"TASK-B",
		"completion: no Builder Result (run `runtime task-complete`)",
		"integration: no verified checkpoint (run `runtime task-integrate` after completion)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("board missing %q:\n%s", want, out)
		}
	}
}

func TestS6StatusEmptyBatchHintsTR003(t *testing.T) {
	root := s6Fixture(t)
	// Empty the registered batch.
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["documents"] = []any{}
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "status", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s6 status failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "empty batch") {
		t.Fatalf("empty batch must name TR-003, got:\n%s", stdout.String())
	}
}
