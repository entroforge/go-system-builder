package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageForIgnoresTemplateArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "docs", "contracts", "CONTRACTS-template.md"),
		filepath.Join(root, "docs", "tasks", "TASK-template.md"),
	} {
		if err := os.WriteFile(path, []byte("template"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cursor, label := StageFor("planning", "design", root)
	if cursor != "S2" || label != "planning.design" {
		t.Fatalf("template artifacts must not advance planning projection: got %s %s", cursor, label)
	}
}

func TestStageForUsesFormalPhaseWithConcretePlanningArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "docs", "contracts", "CONTRACTS-001.md"),
		filepath.Join(root, "docs", "tasks", "TASK-001.md"),
	} {
		if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cursor, label := StageFor("planning", "design", root)
	if cursor != "S2" || label != "planning.design" {
		t.Fatalf("formal planning phase must remain authoritative: got %s %s", cursor, label)
	}
}

func TestStageForFormalPlanningPhaseDoesNotScanArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "docs", "contracts", "CONTRACTS-001.md"),
		filepath.Join(root, "docs", "tasks", "TASK-001.md"),
	} {
		if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range []struct {
		phase  string
		cursor string
		label  string
	}{
		{phase: "design", cursor: "S2", label: "planning.design"},
		{phase: "contracts", cursor: "S3", label: "planning.contracts"},
		{phase: "tasks", cursor: "S4", label: "planning.tasks"},
	} {
		cursor, label := StageFor("planning", want.phase, root)
		if cursor != want.cursor || label != want.label {
			t.Errorf("phase %s projected as %s %s, want %s %s", want.phase, cursor, label, want.cursor, want.label)
		}
	}
}

func TestLegacyPlanningPhaseForArtifactsReconcilesThreeStates(t *testing.T) {
	cases := []struct {
		name            string
		contractsExists bool
		tasksExists     bool
		want            string
	}{
		{name: "design artifacts only", want: "design"},
		{name: "contracts artifacts", contractsExists: true, want: "contracts"},
		{name: "tasks artifacts", contractsExists: true, tasksExists: true, want: "tasks"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.contractsExists {
				if err := os.MkdirAll(filepath.Join(root, "docs", "contracts"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "docs", "contracts", "CONTRACTS-legacy.md"), []byte("legacy"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.tasksExists {
				if err := os.MkdirAll(filepath.Join(root, "docs", "tasks"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "docs", "tasks", "TASK-legacy.md"), []byte("legacy"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			phase, err := ReconcileLegacyPlanningPhase(root)
			if err != nil {
				t.Fatal(err)
			}
			if phase != tc.want {
				t.Fatalf("legacy phase = %s, want %s", phase, tc.want)
			}
		})
	}
}
