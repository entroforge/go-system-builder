package semantic_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestValidateRepositoryRejectsInvalidModuleScenarioPackage(t *testing.T) {
	root := copyRepositoryForScenarioTest(t)
	moduleDir := filepath.Join(root, "docs", "design", "prototypes", "investor-workbench")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "scenario-model.json"), []byte(`{"module":"investor-workbench","coverage_profile":"ordinary","facts":[],"rules":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "fixture-contract.json"), []byte(`{"module":"investor-workbench","fixtures":[{"id":"fixture","persona":"operator","synthetic":true,"setup":["seed"],"cleanup":["cleanup"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "stories.md"), []byte("# S-001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "flows.md"), []byte("# F-001\n### PATH-INVESTOR\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := semantic.ValidateRepository(root)
	if err == nil || !strings.Contains(err.Error(), "scenario packages") || !strings.Contains(err.Error(), "scenario-model.json") {
		t.Fatalf("invalid module scenario package was not rejected by repository validation: %v", err)
	}
}

func copyRepositoryForScenarioTest(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..")
	destination := t.TempDir()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(target, data, mode)
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}
