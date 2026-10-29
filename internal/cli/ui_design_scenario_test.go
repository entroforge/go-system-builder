package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/entroforge/go-system-builder/internal/scenario"
)

func uiInventoryREQ(t *testing.T, root, modules string) string {
	t.Helper()
	path := "docs/requirements/REQ-001.md"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte("# REQ-001\n"+modules), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUIDesignInventoryRequiresEveryREQBoundModuleAndOutput(t *testing.T) {
	root := t.TempDir()
	req := uiInventoryREQ(t, root, "docs/design/prototypes/investor-workbench/")
	if err := inspectUIDesignPackageFilesForREQ(root, req); err == nil || !strings.Contains(err.Error(), "investor-workbench") {
		t.Fatalf("missing module diagnostic: %v", err)
	}
	if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	if err := inspectUIDesignPackageFilesForREQ(root, req); err != nil {
		t.Fatal(err)
	}
	required := filepath.Join(root, "docs/design/prototypes/investor-workbench/cases.json")
	if err := os.Remove(required); err != nil {
		t.Fatal(err)
	}
	if err := inspectUIDesignPackageFilesForREQ(root, req); err == nil || !strings.Contains(err.Error(), "investor-workbench/cases.json") {
		t.Fatalf("missing file diagnostic: %v", err)
	}
	if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	uiInventoryREQ(t, root, "docs/design/prototypes/investor-workbench/ and docs/design/prototypes/portfolio/")
	if err := inspectUIDesignPackageFilesForREQ(root, req); err == nil || !strings.Contains(err.Error(), "portfolio") {
		t.Fatalf("second module diagnostic: %v", err)
	}
}

func TestUIImpactREQWithoutExplicitModuleBindingReportsGap(t *testing.T) {
	root := t.TempDir()
	req := uiInventoryREQ(t, root, "UI impact: changed")
	if err := inspectUIDesignPackageFilesForREQ(root, req); err == nil || !strings.Contains(err.Error(), "explicit module") {
		t.Fatalf("missing binding diagnostic: %v", err)
	}
}

func TestUIDesignInventoryRejectsExternalModuleSymlink(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	req := uiInventoryREQ(t, root, "docs/design/prototypes/investor-workbench/")
	if err := writeCompleteUIDesignPackageForTest(external, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	prototypes := filepath.Join(root, "docs/design/prototypes")
	if err := os.MkdirAll(prototypes, 0755); err != nil {
		t.Fatal(err)
	}
	createSymlinkOrSkip(t, filepath.Join(external, "docs/design/prototypes/investor-workbench"), filepath.Join(prototypes, "investor-workbench"))
	if err := inspectUIDesignPackageFilesForREQ(root, req); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("external module must not be traversed: %v", err)
	}
}

func TestUIDesignInventoryRejectsSymlinkRequiredFiles(t *testing.T) {
	for _, name := range []string{"index.html", "stories.md", "flows.md", "scenario-model.json", "fixture-contract.json", "cross-matrix.json", "cases.json", "scenario-coverage.json"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			req := uiInventoryREQ(t, root, "docs/design/prototypes/investor-workbench/")
			if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "docs/design/prototypes/investor-workbench", name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(external, []byte("external"), 0644); err != nil {
				t.Fatal(err)
			}
			createSymlinkOrSkip(t, external, path)
			if err := inspectUIDesignPackageFilesForREQ(root, req); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("required file must not be followed: %v", err)
			}
		})
	}
}

func createSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOTSUP) {
			t.Skipf("symlinks are unsupported in this environment: %v", err)
		}
		t.Fatalf("create symlink %s -> %s: %v", link, target, err)
	}
}

func writeCompleteUIDesignPackageForTest(root, module string) error {
	dir := filepath.Join(root, "docs", "design", "prototypes", module)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, content := range map[string]string{
		"index.html": "设计代数 更新 路由 index.html",
		"page.html":  "设计代数 更新 路由 index.html",
		"stories.md": "# S-001\nREQ-001\n",
		"flows.md":   "# F-001\n### PATH-INVESTOR\nREQ-001\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	model := map[string]any{
		"module": module, "coverage_profile": "ordinary",
		"facts": []any{map[string]any{"id": "fact-investor", "partitions": []any{
			map[string]any{"id": "institutional", "value": "institutional"}, map[string]any{"id": "individual", "value": "individual"},
		}}},
		"rules": []any{map[string]any{"id": "rule-investor", "source_refs": []any{"REQ-001"}, "risk": "ordinary", "branches": []any{
			map[string]any{"id": "branch-allow", "case_id": "CASE-ALLOW", "title": "allow", "polarity": "positive", "required": true, "witness": map[string]any{"fact-investor": "institutional"}, "oracle": map[string]any{"visible": []any{"form"}, "terminal_state": "submitted", "persisted_effects": []any{"created"}, "forbidden_side_effects": []any{"duplicate"}}, "fixture_id": "fixture", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true},
			map[string]any{"id": "branch-reject", "case_id": "CASE-REJECT", "title": "reject", "polarity": "negative", "required": true, "witness": map[string]any{"fact-investor": "individual"}, "oracle": map[string]any{"visible": []any{"validation-error"}, "terminal_state": "draft", "persisted_effects": []any{"draft-retained"}, "rejection": "not-allowed", "expected_state": "draft", "forbidden_side_effects": []any{"created"}, "recovery": "choose-institutional"}, "fixture_id": "fixture", "story_refs": []any{"S-001"}, "flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true},
		}}},
	}
	fixture := map[string]any{"module": module, "fixtures": []any{map[string]any{"id": "fixture", "persona": "operator", "synthetic": true, "setup": []any{"seed"}, "cleanup": []any{"cleanup"}}}}
	crossMatrix := map[string]any{"module": module, "entries": []any{
		map[string]any{"fact": "fact-investor", "req_ref": "REQ-001", "story": "S-001", "branch": "branch-allow"},
		map[string]any{"fact": "fact-investor", "req_ref": "REQ-001", "story": "S-001", "branch": "branch-reject"},
	}}
	for name, value := range map[string]any{"scenario-model.json": model, "fixture-contract.json": fixture, "cross-matrix.json": crossMatrix} {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	if _, err := scenario.GenerateModule(root, module); err != nil {
		return err
	}
	return nil
}
