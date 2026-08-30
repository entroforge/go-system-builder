package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func writeLegacyPlanningState(t *testing.T, dir string, phase any, revision int) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = "loop-legacy"
	state["revision"] = revision
	state["lifecycle"] = map[string]any{
		"state":          "planning",
		"phase":          phase,
		"phase_revision": 0,
	}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyPlanningMapsPhaseFromArtifacts(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyPlanningState(t, claude, nil, 3)
	if err := os.WriteFile(filepath.Join(claude, "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "contracts", "CONTRACTS-legacy.md"), []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := testWriter(filepath.Join(claude, "loop-state.json"), filepath.Join(claude, "loop-events.jsonl"))
	migrated, err := store.MigrateLegacyPlanning(root)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("expected migration from legacy planning")
	}

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 4 {
		t.Fatalf("revision = %d, want 4", snapshot.Revision)
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["phase"] != "contracts" {
		t.Fatalf("phase = %v, want contracts", lifecycle["phase"])
	}

	event, err := readLastJournalEvent(filepath.Join(claude, "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if event["event"] != "planning_phase_migrated" {
		t.Fatalf("journal event = %v, want planning_phase_migrated", event["event"])
	}
	if event["before_revision"] != float64(3) || event["after_revision"] != float64(4) {
		t.Fatalf("revision audit = %v/%v, want 3/4", event["before_revision"], event["after_revision"])
	}

	migratedAgain, err := store.MigrateLegacyPlanning(root)
	if err != nil {
		t.Fatal(err)
	}
	if migratedAgain {
		t.Fatal("second migrate must be no-op")
	}
	snapshot2, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot2.Revision != 4 {
		t.Fatalf("revision after no-op = %d, want 4", snapshot2.Revision)
	}
}

func TestMigrateLegacyPlanningNoOpForCurrentFormalPhase(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyPlanningState(t, claude, "design", 2)
	if err := os.WriteFile(filepath.Join(claude, "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	store := testWriter(filepath.Join(claude, "loop-state.json"), filepath.Join(claude, "loop-events.jsonl"))
	migrated, err := store.MigrateLegacyPlanning(root)
	if err != nil {
		t.Fatal(err)
	}
	if migrated {
		t.Fatal("formal design phase with no artifacts must not migrate")
	}
}
