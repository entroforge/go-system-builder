package assignment_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestBUG103BugEventCommitsSchemaValidRuntime(t *testing.T) {
	root, statePath, journalPath := writeBUG103Runtime(t, map[string]any{
		"bugs": []any{map[string]any{
			"id":                          "BUG-103",
			"state":                       "draft",
			"path":                        "docs/reports/bugs/BUG-103.md",
			"severity":                    "P1",
			"attempt_count":               0,
			"same_contract_failure_count": 0,
			"original_finder_agent_ids":   []any{"agent-finder"},
		}},
	})

	if _, err := assignment.AdvanceBug(root, statePath, journalPath, assignment.BugEventRequest{
		ExpectedRevision: 1,
		BugID:            "BUG-103",
		Event:            "investigation_started",
	}); err != nil {
		t.Fatalf("BUG event should commit: %v", err)
	}

	assertBUG103RuntimeFilesValid(t, statePath, journalPath)
}

func TestBUG103TaskEventCommitsSchemaValidRuntime(t *testing.T) {
	root, statePath, journalPath := writeBUG103Runtime(t, map[string]any{
		"tasks": []any{map[string]any{
			"id":              "TASK-103",
			"state":           "candidate",
			"path":            "docs/tasks/TASK-103.md",
			"sha256":          "0000000000000000000000000000000000000000000000000000000000000000",
			"owner_agent_ids": []any{"agent-builder"},
		}},
	})

	if _, err := assignment.AdvanceTask(root, statePath, journalPath, assignment.TaskEventRequest{
		ExpectedRevision: 1,
		TaskID:           "TASK-103",
		Event:            "internal_review_passed",
	}); err != nil {
		t.Fatalf("TASK event should commit: %v", err)
	}

	assertBUG103RuntimeFilesValid(t, statePath, journalPath)
}

func TestBUG103TaskLifecycleReferencesRemainSchemaValid(t *testing.T) {
	cases := []struct {
		name         string
		initialState string
		event        string
		params       map[string]any
		field        string
		want         string
	}{
		{name: "document_pass_lock", initialState: "reviewed", event: "document_pass_lock", params: map[string]any{"document_pass_ref": "ev-document-pass"}, field: "document_pass_ref", want: "ev-document-pass"},
		{name: "builder_activated", initialState: "locked", event: "builder_activated", params: map[string]any{"activation_ref": "ev-activation"}, field: "activation_ref", want: "ev-activation"},
		{name: "builder_reported", initialState: "in_progress", event: "builder_reported", params: map[string]any{"completion_report_ref": "ev-completion"}, field: "completion_report_ref", want: "ev-completion"},
		{name: "task_closing_contract_passed", initialState: "review", event: "task_closing_contract_passed", params: map[string]any{"verification_evidence": "ev-verification"}, field: "verification_evidence", want: "ev-verification"},
		{name: "task_blocked", initialState: "in_progress", event: "task_blocked", params: map[string]any{"blocker_evidence": "ev-blocker"}, field: "blocker_evidence", want: "ev-blocker"},
		{name: "task_cancelled", initialState: "candidate", event: "task_cancelled", params: map[string]any{"cancellation_reason": "scope withdrawn"}, field: "cancellation_reason", want: "scope withdrawn"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath, journalPath := writeBUG103Runtime(t, map[string]any{
				"tasks": []any{map[string]any{
					"id":              "TASK-103",
					"state":           tc.initialState,
					"path":            "docs/tasks/TASK-103.md",
					"sha256":          "0000000000000000000000000000000000000000000000000000000000000000",
					"owner_agent_ids": []any{"agent-builder"},
				}},
			})
			if _, err := assignment.AdvanceTask(root, statePath, journalPath, assignment.TaskEventRequest{
				ExpectedRevision: 1,
				TaskID:           "TASK-103",
				Event:            tc.event,
				Params:           tc.params,
			}); err != nil {
				t.Fatalf("TASK event should commit: %v", err)
			}

			stateBytes, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(stateBytes, &state); err != nil {
				t.Fatal(err)
			}
			task := state["entities"].(map[string]any)["tasks"].([]any)[0].(map[string]any)
			if task[tc.field] != tc.want {
				t.Fatalf("task[%q] = %v, want %q", tc.field, task[tc.field], tc.want)
			}
			assertBUG103RuntimeFilesValid(t, statePath, journalPath)
		})
	}
}

func writeBUG103Runtime(t *testing.T, rootEntities map[string]any) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}

	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-BUG-103"
	state["revision"] = 1
	state["lifecycle"] = map[string]any{
		"state":          "bug_resolution",
		"phase":          "fixing",
		"phase_revision": 0,
	}
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs":   []any{},
		"teams":  []any{},
	}
	for key, value := range rootEntities {
		state["entities"].(map[string]any)[key] = value
	}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}

	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(claudeDir, "loop-state.json")
	journalPath := filepath.Join(claudeDir, "loop-events.jsonl")
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, statePath, journalPath
}

func assertBUG103RuntimeFilesValid(t *testing.T, statePath, journalPath string) {
	t.Helper()
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	validator := schema.NewEmbeddedValidator()
	if err := validator.ValidateBytes("loop-state.schema.json", stateBytes); err != nil {
		t.Fatalf("committed runtime state is not schema-valid: %v", err)
	}

	journalFile, err := os.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer journalFile.Close()
	scanner := bufio.NewScanner(journalFile)
	lines := 0
	for scanner.Scan() {
		lines++
		if err := validator.ValidateBytes("loop-event.schema.json", scanner.Bytes()); err != nil {
			t.Fatalf("journal line %d is not schema-valid: %v", lines, err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != 1 {
		t.Fatalf("expected one committed journal event, got %d", lines)
	}
}
