package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

func repositoryContainedPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be repository-relative")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(rel))
	relToRoot, err := filepath.Rel(rootAbs, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("path %q escapes repository", rel)
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(rootAbs)
	if rootErr != nil {
		return "", fmt.Errorf("resolve repository root symlinks: %w", rootErr)
	}
	// EvalSymlinks(candidate) fails when the leaf is new. Walk upward until an
	// existing ancestor is found so a symlinked parent cannot smuggle a future
	// artifact outside the repository.
	for current := abs; ; current = filepath.Dir(current) {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			resolvedRel, relErr := filepath.Rel(resolvedRoot, resolved)
			if relErr != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
				return "", fmt.Errorf("path %q follows a symlink outside repository", rel)
			}
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return abs, nil
}
