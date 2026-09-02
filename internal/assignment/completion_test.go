// completion_test.go pins the canonical S6 Builder Result registration
// (L3-S6 P1): `runtime task-complete` advances the Agent, applies the
// TASK side effect, and registers the derived completion evidence in ONE
// atomic revision — no second manual evidence write.
package assignment_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func anyStrings(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.(string))
	}
	return result
}

func completionRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func completionMessage(t *testing.T, dir, agentID, taskID string) string {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message["message_type"] != "completion_report" {
			continue
		}
		message["agent_id"] = agentID
		message["task_id"] = taskID
		message["runtime_id"] = "loop-REQ-002"
		message["status"] = "completed"
		message["checks"] = []any{
			map[string]any{"name": "unit", "command": "go test ./...", "result": "pass", "evidence_ref": nil},
		}
		message["scope_deviations"] = []any{}
		path := filepath.Join(dir, "completion.json")
		writeJSON(t, path, message)
		return path
	}
	t.Fatal("missing completion_report example")
	return ""
}

func TestCompleteTaskRegistersBuilderResultAtomically(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	state := activeState(t, root, "building", nil, 7)
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-08-20T00:00:00Z"}
	taskBytes := []byte("# TASK-001\n")
	state["documents"] = []any{map[string]any{
		"id": "TASK-001", "kind": "task", "path": "docs/tasks/TASK-001.md",
		"version": "v1", "sha256": semanticSha(t, taskBytes), "status": "complete", "generation": 1,
	}}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-1", "role": "backend-builder", "state": "working",
			"task_ids": []any{"TASK-001"}, "team_id": "workgroup-build-1",
			"definition_ref": ".claude/agents/backend-builder.md",
			"prompt_ref":     "manifest#assignment-builder-1",
			"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-20T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-001", "state": "in_progress", "path": "docs/tasks/TASK-001.md",
			"sha256": semanticSha(t, taskBytes), "owner_agent_ids": []any{"builder-1"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	writeJSON(t, statePath, state)
	messagePath := completionMessage(t, root, "builder-1", "TASK-001")

	snapshot, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1,
		AgentID:          "builder-1",
		MessagePath:      messagePath,
	})
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if snapshot.Revision != 8 {
		t.Fatalf("revision = %d, want exactly one atomic bump to 8", snapshot.Revision)
	}

	// Agent advanced to reported in the same mutation.
	agents := snapshot.State["entities"].(map[string]any)["agents"].([]any)
	agent := agents[0].(map[string]any)
	if agent["state"] != "reported" {
		t.Fatalf("agent state = %v, want reported", agent["state"])
	}
	// TASK advanced to review with the canonical envelope as its ref.
	tasks := snapshot.State["entities"].(map[string]any)["tasks"].([]any)
	task := tasks[0].(map[string]any)
	if task["state"] != "review" {
		t.Fatalf("task state = %v, want review", task["state"])
	}
	envelopeRel, _ := task["completion_report_ref"].(string)

	// Evidence index carries the derived completion envelope.
	items := snapshot.State["evidence"].([]any)
	if len(items) != 1 {
		t.Fatalf("evidence entries = %d, want the single derived envelope", len(items))
	}
	entry := items[0].(map[string]any)
	if entry["kind"] != "completion_report" || entry["id"] != "ev-completion-TASK-001-g1" {
		t.Fatalf("evidence entry = %v, want derived completion envelope", entry)
	}
	if entry["path"] != envelopeRel {
		t.Fatalf("task completion_report_ref %q must equal evidence path %q", envelopeRel, entry["path"])
	}
	if entry["responsibility_id"] != "BUILD-WORK-PACKAGE" {
		t.Fatalf("responsibility = %v, want BUILD-WORK-PACKAGE", entry["responsibility_id"])
	}

	// The envelope on disk is fingerprint-consistent and carries the
	// Builder's rich result content.
	envelopeData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(envelopeRel)))
	if err != nil {
		t.Fatalf("derived envelope missing on disk: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(envelopeData, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["task_id"] != "TASK-001" || envelope["conclusion"] != "completed" {
		t.Fatalf("envelope = %v", envelope)
	}
	checks := envelope["checks"].([]any)
	if len(checks) != 1 || checks[0].(map[string]any)["result"] != "pass" {
		t.Fatalf("envelope checks = %v, want the Builder's recorded check", checks)
	}
	if entry["sha256"] != semanticSha(t, envelopeData) {
		t.Fatalf("evidence sha must match the envelope bytes on disk")
	}

	// The produced runtime stays schema-valid.
	encoded, _ := json.Marshal(snapshot.State)
	if err := semantic.ValidateRuntimeBytes(root, encoded); err != nil {
		t.Fatalf("CompleteTask produced invalid runtime: %v", err)
	}
}

func TestCompleteTaskProjectsChangedPathsIntoEvidenceScopeRefs(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	state := activeState(t, root, "building", nil, 7)
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-08-20T00:00:00Z"}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-1", "role": "backend-builder", "state": "working",
			"task_ids": []any{"TASK-001"}, "team_id": "workgroup-build-1",
			"definition_ref": ".claude/agents/backend-builder.md",
			"prompt_ref":     "manifest#assignment-builder-1",
			"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-20T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-001", "state": "in_progress", "path": "docs/tasks/TASK-001.md",
			"sha256": semanticSha(t, []byte("# TASK-001\n")), "owner_agent_ids": []any{"builder-1"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	writeJSON(t, statePath, state)
	messagePath := completionMessage(t, root, "builder-1", "TASK-001")
	data, err := os.ReadFile(messagePath)
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	message["changed_paths"] = []any{"internal/api/handler.go", "web/src/form.tsx"}
	writeJSON(t, messagePath, message)

	snapshot, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: 7, AgentID: "builder-1", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	entry := snapshot.State["evidence"].([]any)[0].(map[string]any)
	refs, ok := entry["scope_refs"].([]any)
	if !ok {
		t.Fatalf("scope_refs type = %T, want []any", entry["scope_refs"])
	}
	if got, want := strings.Join(anyStrings(refs), ","), "internal/api/handler.go,web/src/form.tsx"; got != want {
		t.Fatalf("scope_refs = %q, want %q", got, want)
	}
}

func TestCompleteTaskResubmissionEscalatesEvidenceID(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	state := activeState(t, root, "building", nil, 7)
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-08-20T00:00:00Z"}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-1", "role": "backend-builder", "state": "working",
			"task_ids": []any{"TASK-001"}, "team_id": "workgroup-build-1",
			"definition_ref": ".claude/agents/backend-builder.md",
			"prompt_ref":     "manifest#assignment-builder-1",
			"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-20T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-001", "state": "in_progress",
			"path": "docs/tasks/TASK-001.md", "sha256": semanticSha(t, []byte("# TASK-001\n")),
			"owner_agent_ids": []any{"builder-1"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	writeJSON(t, statePath, state)
	messagePath := completionMessage(t, root, "builder-1", "TASK-001")

	// First submission: base evidence id.
	first, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: 7, AgentID: "builder-1", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("first submission: %v", err)
	}
	// Reset the agent to working for the retry (the fix-and-resubmit path
	// runs after a failed gate, with the agent already reported).
	agents := first.State["entities"].(map[string]any)["agents"].([]any)
	agents[0].(map[string]any)["state"] = "working"
	first.State["entities"].(map[string]any)["agents"] = agents
	writeJSON(t, statePath, first.State)

	second, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: first.Revision, AgentID: "builder-1", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("resubmission must not collide with the first evidence id: %v", err)
	}
	items := second.State["evidence"].([]any)
	if len(items) != 2 {
		t.Fatalf("evidence entries = %d, want first + escalated retry", len(items))
	}
	ids := []string{items[0].(map[string]any)["id"].(string), items[1].(map[string]any)["id"].(string)}
	if ids[0] != "ev-completion-TASK-001-g1" || ids[1] != "ev-completion-TASK-001-g1-r2" {
		t.Fatalf("evidence ids = %v, want base then -r2", ids)
	}
	// The retry's envelope file exists on disk under its own name.
	rel := items[1].(map[string]any)["path"].(string)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("escalated envelope missing on disk: %v", err)
	}
}

func TestCompleteTaskRejectsBlockedStatus(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	state := activeState(t, root, "building", nil, 7)
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-1", "state": "working", "task_ids": []any{"TASK-001"},
		}},
		"tasks": []any{},
		"bugs":  []any{},
		"teams": []any{},
	}
	writeJSON(t, statePath, state)
	messagePath := completionMessage(t, root, "builder-1", "TASK-001")
	data, _ := os.ReadFile(messagePath)
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	message["status"] = "blocked"
	writeJSON(t, messagePath, message)

	if _, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: 7, AgentID: "builder-1", MessagePath: messagePath,
	}); err == nil {
		t.Fatal("blocked status belongs on the work_blocked path, not task-complete")
	}
}

func TestCompleteTaskRejectsForeignTask(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	state := activeState(t, root, "building", nil, 7)
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-1", "state": "working", "task_ids": []any{"TASK-001"},
		}},
		"tasks": []any{},
		"bugs":  []any{},
		"teams": []any{},
	}
	writeJSON(t, statePath, state)
	messagePath := completionMessage(t, root, "builder-1", "TASK-999")

	if _, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: 7, AgentID: "builder-1", MessagePath: messagePath,
	}); err == nil {
		t.Fatal("completion for a task outside the assignment must be rejected")
	}
}

func semanticSha(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
