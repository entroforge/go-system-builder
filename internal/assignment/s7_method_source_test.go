package assignment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// Registration retains the actual manifest path and original responsibility.
func TestS7RegistrationKeepsMethodSelectionSource(t *testing.T) {
	root := assignmentTestRoot(t)
	dir := t.TempDir()
	statePath, journalPath := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 6)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	seedReviewPlan(t, root, dir, state)
	writeJSON(t, statePath, state)
	manifestPath := filepath.Join(dir, "manifest.json")
	copyManifest(t, root, manifestPath)
	taskPath := filepath.Join(dir, "TASK-012.md")
	if err := os.WriteFile(taskPath, []byte("# TASK-012\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := assignment.Register(root, statePath, journalPath, assignment.Request{
		ExpectedRevision: 6, ManifestPath: manifestPath, TaskID: "TASK-012", TaskPath: taskPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	team := next.State["entities"].(map[string]any)["teams"].([]any)[0].(map[string]any)
	if team["manifest_ref"] == "" || team["manifest_ref"] == nil {
		t.Fatalf("registered selection lost its manifest path: %v", team)
	}
}
