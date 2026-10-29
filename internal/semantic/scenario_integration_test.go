package semantic_test

import (
	"encoding/json"
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

func TestContractsCurrentNumericCaseRanges(t *testing.T) {
	for _, tc := range []struct{ name, text, cases, want string }{
		{"known prefix group", "CASE-INPUT-*", `"CASE-INPUT-001","CASE-INPUT-002"`, ""},
		{"unknown prefix group", "CASE-UNKNOWN-*", `"CASE-INPUT-001"`, "CASE group CASE-UNKNOWN-* matches no known CASE"},
		{"base token is not group match", "CASE-INPUT CASE-INPUT-*", `"CASE-INPUT"`, "CASE group CASE-INPUT-* matches no known CASE"},
		{"other prefix remains uncovered", "CASE-INPUT-*", `"CASE-INPUT-001","CASE-OUTPUT-001"`, "reverse closure: CASE-OUTPUT-001"},
		{"fenced group cannot cover", "```text\nCASE-INPUT-*\n```", `"CASE-INPUT-001"`, "reverse closure: CASE-INPUT-001"},
		{"range includes middle", "CASE-008~CASE-010", `"CASE-008","CASE-009","CASE-010"`, ""},
		{"natural separator", "CASE-008至CASE-010", `"CASE-008","CASE-009","CASE-010"`, ""},
		{"en dash separator", "CASE-008–CASE-010", `"CASE-008","CASE-009","CASE-010"`, ""},
		{"unknown middle", "CASE-008~CASE-010", `"CASE-008","CASE-010"`, "token CASE-009 does not exist"},
		{"endpoints omit real middle", "CASE-008 CASE-010", `"CASE-008","CASE-009","CASE-010"`, "reverse closure: CASE-009"},
		{"fenced middle cannot cover", "CASE-008 CASE-010\n```markdown\nCASE-009\n```", `"CASE-008","CASE-009","CASE-010"`, "reverse closure: CASE-009"},
		{"tilde fenced unknown is example", "CASE-008\n~~~text\nCASE-999\n~~~", `"CASE-008"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(rel, text string) {
				t.Helper()
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var ids []string
			if err := json.Unmarshal([]byte("["+tc.cases+"]"), &ids); err != nil {
				t.Fatal(err)
			}
			cases := make([]map[string]string, 0, len(ids))
			for _, id := range ids {
				cases = append(cases, map[string]string{"id": id})
			}
			data, err := json.Marshal(map[string]any{"cases": cases})
			if err != nil {
				t.Fatal(err)
			}
			write("docs/design/prototypes/example/cases.json", string(data))
			write("docs/design/prototypes/example/scenario-model.json", `{"rules":[]}`)
			write("docs/dev/contracts/BE-001.md", "# Current contract\n\n## Professional behavior\n"+tc.text)
			result, err := semantic.ContractsCheck(root)
			if err != nil {
				t.Fatal(err)
			}
			problems := strings.Join(result.Problems, "\n")
			if tc.want != "" {
				if !strings.Contains(problems, tc.want) {
					t.Fatalf("want %s, got %s", tc.want, problems)
				}
			} else if len(result.Problems) > 0 {
				t.Fatal(problems)
			}
		})
	}
}

func TestContractsCanonicalCasesIgnoreEmbeddedCaseFragments(t *testing.T) {
	for _, tc := range []struct{ name, generated, contract, want string }{
		{"embedded references are not cases", `{"cases":[{"id":"CASE-CATEGORY-CASE-SENSITIVE","branch":"BR-CATEGORY-CASE-SENSITIVE","witness":{"FACT-CATEGORY-CASE-VARIANTS":"v"},"fixture_id":"FX-CASE-VARIANTS"}]}`, "CASE-CATEGORY-CASE-SENSITIVE FACT-CATEGORY-CASE-VARIANTS BR-CATEGORY-CASE-SENSITIVE FX-CASE-VARIANTS", ""},
		{"unknown shorthand remains rejected", `{"cases":[{"id":"CASE-CATEGORY-CASE-SENSITIVE"}]}`, "CASE-CATEGORY-CASE-SENSITIVE / -CASE-SENSITIVE", "token CASE-SENSITIVE does not exist"},
		{"embedded citation cannot cover", `{"cases":[{"id":"CASE-CATEGORY-CASE-SENSITIVE"}]}`, "BR-CASE-CATEGORY-CASE-SENSITIVE", "reverse closure: CASE-CATEGORY-CASE-SENSITIVE"},
		{"metadata cannot replace missing real case", `{"cases":[],"note":"CASE-CATEGORY-CASE-SENSITIVE"}`, "CASE-CATEGORY-CASE-SENSITIVE", "cases.json is missing CASE-CATEGORY-CASE-SENSITIVE"},
		{"unknown actual case remains rejected", `{"cases":[{"id":"CASE-CATEGORY-CASE-SENSITIVE"},{"id":"CASE-UNKNOWN"}]}`, "CASE-CATEGORY-CASE-SENSITIVE CASE-UNKNOWN", "cases.json carries CASE-UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(rel, text string) {
				t.Helper()
				p := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("docs/design/prototypes/example/cases.json", tc.generated)
			write("docs/design/prototypes/example/scenario-model.json", `{"rules":[{"branches":[{"case_id":"CASE-CATEGORY-CASE-SENSITIVE"}]}]}`)
			write("docs/design/prototypes/example/stories.md", "# S-001\nFACT-CATEGORY-CASE-VARIANTS")
			write("docs/design/prototypes/example/flows.md", "# F-001\nBR-CATEGORY-CASE-SENSITIVE")
			write("docs/dev/contracts/BE-001.md", "# Contract\n"+tc.contract)
			got, err := semantic.ContractsCheck(root)
			if err != nil {
				t.Fatal(err)
			}
			problems := strings.Join(got.Problems, "\n")
			if tc.want != "" {
				if !strings.Contains(problems, tc.want) {
					t.Fatalf("want %q, got %s", tc.want, problems)
				}
			} else if len(got.Problems) > 0 {
				t.Fatal(problems)
			}
		})
	}
}
