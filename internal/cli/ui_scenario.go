package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var uiModuleReferencePattern = regexp.MustCompile(`docs/design/prototypes/([a-z0-9]+(?:-[a-z0-9]+)*)/`)

// inspectUIDesignPackageFilesForREQ is a passive inventory, not professional
// validation. Ordinary guidance must not parse/generate/validate whole scenario
// packages; authors run the scenario checks as part of their design work.
func inspectUIDesignPackageFilesForREQ(root, reqPath string) error {
	modules, err := modulesBoundToREQ(root, reqPath)
	if err != nil {
		return err
	}
	for _, module := range modules {
		directory, prototypesPath, exists, err := secureUIDesignModuleDirectory(root, module)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("missing module directory docs/design/prototypes/%s", module)
		}
		for _, name := range []string{"index.html", "stories.md", "flows.md", "scenario-model.json", "fixture-contract.json", "cross-matrix.json", "cases.json", "scenario-coverage.json"} {
			exists, err := validateUIDesignFile(filepath.Join(directory, name), prototypesPath)
			if err != nil {
				return fmt.Errorf("module %s: %s: %w", module, name, err)
			}
			if !exists {
				return fmt.Errorf("missing docs/design/prototypes/%s/%s", module, name)
			}
		}
	}
	return nil
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
