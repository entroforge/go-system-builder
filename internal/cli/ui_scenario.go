package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/scenario"
)

var uiModuleReferencePattern = regexp.MustCompile(`docs/design/prototypes/([a-z0-9]+(?:-[a-z0-9]+)*)/`)

// hasCompleteUIDesignPackageForREQ binds the UI gate to the current REQ's
// explicit module references. A package in an unrelated module never satisfies
// this check, and every referenced module must pass both shape and scenario
// semantics checks.
func hasCompleteUIDesignPackageForREQ(root, reqPath string) (bool, error) {
	modules, err := modulesBoundToREQ(root, reqPath)
	if err != nil {
		return false, err
	}
	for _, module := range modules {
		complete, err := hasCompleteUIDesignPackageForModule(root, module)
		if err != nil {
			return false, err
		}
		if !complete {
			return false, nil
		}
	}
	return true, nil
}

func modulesBoundToREQ(root, reqPath string) ([]string, error) {
	if filepath.IsAbs(reqPath) {
		return nil, fmt.Errorf("UI-impact REQ must use a repository-relative path")
	}
	clean := filepath.ToSlash(filepath.Clean(reqPath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("UI-impact REQ path escapes the repository")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return nil, fmt.Errorf("read bound UI-impact REQ %s: %w", clean, err)
	}
	seen := map[string]struct{}{}
	for _, match := range uiModuleReferencePattern.FindAllStringSubmatch(string(data), -1) {
		if len(match) == 2 {
			seen[match[1]] = struct{}{}
		}
	}
	modules := make([]string, 0, len(seen))
	for module := range seen {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	if len(modules) == 0 {
		return nil, fmt.Errorf("UI-impact REQ must contain at least one explicit module path under docs/design/prototypes/<module>/")
	}
	return modules, nil
}

func hasCompleteUIDesignPackageForModule(root, module string) (bool, error) {
	directory, prototypesRealPath, exists, err := secureUIDesignModuleDirectory(root, module)
	if err != nil || !exists {
		return false, err
	}
	for _, name := range []string{
		"index.html", "stories.md", "flows.md", "scenario-model.json",
		"fixture-contract.json", "cross-matrix.json", "cases.json",
		"scenario-coverage.json",
	} {
		exists, err := validateUIDesignFile(filepath.Join(directory, name), prototypesRealPath)
		if err != nil {
			return false, fmt.Errorf("module %s: required file %s: %w", module, name, err)
		}
		if !exists {
			return false, nil
		}
	}
	pagePaths := make([]string, 0)
	if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("path %s is a symlink", path)
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".html") && entry.Name() != "index.html" {
			if _, err := validateUIDesignFile(path, prototypesRealPath); err != nil {
				return err
			}
			pagePaths = append(pagePaths, path)
		}
		return nil
	}); err != nil {
		return false, fmt.Errorf("module %s: inspect pages: %w", module, err)
	}
	if !hasProtoMetaHeader(filepath.Join(directory, "index.html")) {
		return missingProtoHeaderError(directory, "index.html")
	}
	if len(pagePaths) == 0 {
		return false, fmt.Errorf("module %s: no page HTML beyond index.html — the package needs at least one page", module)
	}
	for _, pagePath := range pagePaths {
		if !hasProtoMetaHeader(pagePath) {
			return missingProtoHeaderError(directory, pagePath)
		}
	}
	if !hasStoryIDWithReqID(filepath.Join(directory, "stories.md")) ||
		!hasFlowIDWithReqID(filepath.Join(directory, "flows.md")) {
		return false, nil
	}
	if _, err := scenario.ValidateModule(root, module, scenario.ValidateOptions{}); err != nil {
		return false, fmt.Errorf("module %s: scenario package validation: %w", module, err)
	}
	return true, nil
}

func secureUIDesignModuleDirectory(root, module string) (string, string, bool, error) {
	prototypesPath := filepath.Join(root, "docs", "design", "prototypes")
	prototypesInfo, err := os.Lstat(prototypesPath)
	if os.IsNotExist(err) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("inspect prototypes root: %w", err)
	}
	if prototypesInfo.Mode()&os.ModeSymlink != 0 {
		return "", "", false, fmt.Errorf("prototypes root %s is a symlink", prototypesPath)
	}
	if !prototypesInfo.IsDir() {
		return "", "", false, fmt.Errorf("prototypes root %s is not a directory", prototypesPath)
	}
	prototypesRealPath, err := filepath.EvalSymlinks(prototypesPath)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve prototypes root: %w", err)
	}

	directory := filepath.Join(prototypesPath, module)
	moduleInfo, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("inspect module directory %s: %w", module, err)
	}
	if moduleInfo.Mode()&os.ModeSymlink != 0 {
		return "", "", false, fmt.Errorf("module directory %s is a symlink", module)
	}
	if !moduleInfo.IsDir() {
		return "", "", false, fmt.Errorf("module path %s is not a directory", module)
	}
	moduleRealPath, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve module directory %s: %w", module, err)
	}
	if !pathWithin(prototypesRealPath, moduleRealPath) {
		return "", "", false, fmt.Errorf("module directory %s resolves outside prototypes root", module)
	}
	return directory, prototypesRealPath, true, nil
}

func validateUIDesignFile(path, prototypesRealPath string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("path %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("path %s is not a regular file", path)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", path, err)
	}
	if !pathWithin(prototypesRealPath, realPath) {
		return false, fmt.Errorf("path %s resolves outside prototypes root", path)
	}
	return true, nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func boundREQPathFromState(state map[string]any) string {
	bound, _ := state["bound_req"].(map[string]any)
	path, _ := bound["path"].(string)
	return path
}

// missingProtoHeaderError names the page and the missing header tokens so a
// failed UI gate is self-repairing (the 4-field header is taught in
// skills/ui-prototyping; BUG round-3 NEW-3: the gate used to fail silently).
func missingProtoHeaderError(directory, rel string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(directory, rel))
	if err != nil {
		return false, fmt.Errorf("page %s: unreadable: %w", rel, err)
	}
	var missing []string
	for _, marker := range []string{"设计代数", "更新", "路由", "index.html"} {
		if !strings.Contains(string(data), marker) {
			missing = append(missing, marker)
		}
	}
	return false, fmt.Errorf("page %s: missing 4-field proto-meta header token(s) %v — every page HTML carries 设计代数 / 更新 / 路由 / index 链接 (see skills/ui-prototyping)", rel, missing)
}
