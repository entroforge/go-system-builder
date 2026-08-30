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

func TestUIDesignPackageRequiresEveryREQBoundModuleAndScenarioOutputs(t *testing.T) {
	root := t.TempDir()
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-001.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, []byte("# REQ-001\n\nAffected module: docs/design/prototypes/investor-workbench/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	complete, err := hasCompleteUIDesignPackageForREQ(root, "docs/requirements/REQ-001.md")
	if err == nil && complete {
		t.Fatal("missing current module scenario package must fail the UI design gate")
	}

	if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	complete, err = hasCompleteUIDesignPackageForREQ(root, "docs/requirements/REQ-001.md")
	if err != nil || !complete {
		t.Fatalf("complete current module package was rejected: complete=%v err=%v", complete, err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "design", "prototypes", "investor-workbench", "page.html"), []byte("page without metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	complete, _ = hasCompleteUIDesignPackageForREQ(root, "docs/requirements/REQ-001.md")
	if complete {
		t.Fatal("a page without the required prototype metadata header must fail the UI design gate")
	}

	if err := os.WriteFile(reqPath, []byte("# REQ-001\n\nAffected modules: docs/design/prototypes/investor-workbench/ and docs/design/prototypes/portfolio/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	complete, _ = hasCompleteUIDesignPackageForREQ(root, "docs/requirements/REQ-001.md")
	if complete {
		t.Fatal("a second unbound module must fail the UI design gate")
	}
}

func TestUIImpactREQWithoutExplicitModuleBindingFailsClosed(t *testing.T) {
	root := t.TempDir()
	reqPath := filepath.Join(root, "docs", "requirements", "REQ-002.md")
	if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, []byte("# REQ-002\n\nUI impact: changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := modulesBoundToREQ(root, "docs/requirements/REQ-002.md")
	if err == nil || !strings.Contains(err.Error(), "explicit module") {
		t.Fatalf("missing explicit module binding accepted: %v", err)
	}
}

func TestUIDesignPackageRejectsExternalModuleSymlink(t *testing.T) {
	root := t.TempDir()
	externalRoot := t.TempDir()
	if err := writeCompleteUIDesignPackageForTest(externalRoot, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	prototypesRoot := filepath.Join(root, "docs", "design", "prototypes")
	if err := os.MkdirAll(prototypesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	createSymlinkOrSkip(t,
		filepath.Join(externalRoot, "docs", "design", "prototypes", "investor-workbench"),
		filepath.Join(prototypesRoot, "investor-workbench"),
	)

	complete, err := hasCompleteUIDesignPackageForModule(root, "investor-workbench")
	if complete || err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("external module symlink must fail closed: complete=%v err=%v", complete, err)
	}
}

func TestUIDesignPackageRejectsSymlinkRequiredFiles(t *testing.T) {
	requiredFiles := []string{
		"index.html", "stories.md", "flows.md", "scenario-model.json",
		"fixture-contract.json", "cross-matrix.json", "cases.json",
		"scenario-coverage.json",
	}
	for _, name := range requiredFiles {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "docs", "design", "prototypes", "investor-workbench", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(external, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			createSymlinkOrSkip(t, external, path)

			complete, err := hasCompleteUIDesignPackageForModule(root, "investor-workbench")
			if complete || err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
				t.Fatalf("symlink required file %s must fail closed: complete=%v err=%v", name, complete, err)
			}
		})
	}
}

func TestUIDesignPackageRejectsExternalHTMLPageSymlink(t *testing.T) {
	root := t.TempDir()
	if err := writeCompleteUIDesignPackageForTest(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(root, "docs", "design", "prototypes", "investor-workbench", "page.html")
	if err := os.Remove(pagePath); err != nil {
		t.Fatal(err)
	}
	externalPage := filepath.Join(t.TempDir(), "external.html")
	if err := os.WriteFile(externalPage, []byte("设计代数 更新 路由 index.html"), 0o644); err != nil {
		t.Fatal(err)
	}
	createSymlinkOrSkip(t, externalPage, pagePath)

	complete, err := hasCompleteUIDesignPackageForModule(root, "investor-workbench")
	if complete || err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("external HTML page symlink must fail closed: complete=%v err=%v", complete, err)
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
	crossMatrix := map[string]any{"module": module, "entries": []any{map[string]any{
		"fact": "fact-investor", "req_ref": "REQ-001", "story": "S-001", "branch": "branch-allow",
	}}}
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
