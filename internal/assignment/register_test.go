package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestRegisterWorkgroupAddsTaskTeamAndReadingAgents(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "delivery", 6)
	writeJSON(t, statePath, state)

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	next, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 6,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(next.State)
	if err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeBytes(root, data); err != nil {
		t.Fatalf("registration produced invalid runtime: %v", err)
	}
	entities := next.State["entities"].(map[string]any)
	if len(entities["agents"].([]any)) != 5 || len(entities["teams"].([]any)) != 1 {
		t.Fatalf("unexpected registered entities: %#v", entities)
	}
}

func TestRegisterWorkgroupRejectsWrongLoopStateWithoutMutation(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "planning", "design", 2)
	writeJSON(t, statePath, state)
	before, _ := os.ReadFile(statePath)

	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 2,
		ManifestPath:     manifestPath,
		TaskID:           "TASK-012",
		TaskPath:         taskPath,
	}); err == nil {
		t.Fatal("expected wrong-state rejection")
	}
	after, _ := os.ReadFile(statePath)
	if string(before) != string(after) {
		t.Fatal("rejected registration mutated runtime")
	}
}

func copyManifest(t *testing.T, root, target string) {
	t.Helper()
	data, err := os.ReadFile("testdata/delivery-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func activeState(t *testing.T, root, state string, phase any, revision int) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value["revision"] = revision
	value["runtime_id"] = "loop-REQ-002"
	value["entities"] = map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}}
	lifecycle := value["lifecycle"].(map[string]any)
	lifecycle["state"] = state
	lifecycle["phase"] = phase
	return value
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
