package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapResumesAndPreservesChangedAssets(t *testing.T) {
	b, e, _, _ := workerFixture(t)
	for _, path := range []string{"agents/builder.md", "skills/build/SKILL.md"} {
		path = filepath.Join(b.MainRoot, ".claude", path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("trusted asset"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(t.TempDir(), "native")
	if err := os.WriteFile(executable, []byte("native test fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.MainRoot, ".claude/credentials.json"), []byte("do not copy"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := b.Bootstrap(e, executable); err != nil {
			t.Fatal(err)
		}
		if err := b.ValidateBootstrap(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"credentials.json", "loop-state.json", "loop-events.jsonl"} {
		if _, err := os.Stat(filepath.Join(e.Path, ".claude", name)); !os.IsNotExist(err) {
			t.Fatalf("copied control file %s", name)
		}
	}
	asset := filepath.Join(e.Path, ".claude/agents/builder.md")
	if err := os.WriteFile(asset, []byte("local change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.ValidateBootstrap(e); err == nil {
		t.Fatal("accepted asset drift")
	}
	if err := b.Bootstrap(e, executable); err == nil {
		t.Fatal("overwrote changed asset")
	}
	data, _ := os.ReadFile(asset)
	if string(data) != "local change" {
		t.Fatal("lost Worker data")
	}
}

func TestFrozenInputChangesBlockPreparation(t *testing.T) {
	b, ctx := repository(t)
	state := map[string]any{"runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}}
	e, err := b.Plan(ctx, state, "assignment", "builder", []string{"input.md"}, nil, []string{"input.md"})
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err := os.WriteFile(filepath.Join(b.MainRoot, "input.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Materialize(ctx, e); err == nil {
		t.Fatal("materialized changed frozen inputs")
	}
	if _, err := os.Stat(e.Path); !os.IsNotExist(err) {
		t.Fatal("created Worker before input validation")
	}
}
