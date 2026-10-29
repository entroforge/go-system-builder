package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairAgentDefinitionUsesSelectedAssetLayout(t *testing.T) {
	t.Run("legacy factory layout", func(t *testing.T) {
		root := t.TempDir()
		writeAgentDefinitionFixture(t, root, "agents/backend-builder.md", "legacy role")
		path, data, err := repairAgentDefinition(root, "backend-builder", "", false)
		if err != nil || path != "agents/backend-builder.md" || string(data) != "legacy role" {
			t.Fatalf("legacy resolver = %q %q %v", path, data, err)
		}
	})

	t.Run("installed layout", func(t *testing.T) {
		root := t.TempDir()
		writeAgentDefinitionFixture(t, root, ".claude/agents/backend-builder.md", "installed role")
		if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		path, data, err := repairAgentDefinition(root, "backend-builder", "", false)
		if err != nil || path != ".claude/agents/backend-builder.md" || string(data) != "installed role" {
			t.Fatalf("installed resolver = %q %q %v", path, data, err)
		}
	})

	t.Run("installed missing asset does not fall back", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".claude/agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := repairAgentDefinition(root, "backend-builder", "", false); err == nil {
			t.Fatal("installed asset omission silently selected a factory definition")
		}
	})

	t.Run("mixed layouts are rejected instead of fallback", func(t *testing.T) {
		root := t.TempDir()
		writeAgentDefinitionFixture(t, root, "agents/backend-builder.md", "unapproved fallback")
		if err := os.MkdirAll(filepath.Join(root, ".claude/agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := repairAgentDefinition(root, "backend-builder", "", false); err == nil || !strings.Contains(err.Error(), "mixed asset layout") {
			t.Fatalf("mixed asset sources were accepted: %v", err)
		}
	})

	t.Run("explicit path remains explicit", func(t *testing.T) {
		root := t.TempDir()
		writeAgentDefinitionFixture(t, root, "agents/backend-builder.md", "explicit role")
		if err := os.MkdirAll(filepath.Join(root, ".claude/agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		path, data, err := repairAgentDefinition(root, "backend-builder", "agents/backend-builder.md", true)
		if err != nil || path != "agents/backend-builder.md" || string(data) != "explicit role" {
			t.Fatalf("explicit resolver = %q %q %v", path, data, err)
		}
	})
}

func writeAgentDefinitionFixture(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
