package repair

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// baselineBoundary is captured once per scan. A directory name alone never
// proves that its contents are disposable. Registered worktrees and ignored,
// marked Python environments have separate ownership; tracked files win.
type baselineBoundary struct {
	root      string
	tracked   map[string]bool
	worktrees []string
	gitOK     bool
}

func newBaselineBoundary(root string) baselineBoundary {
	b := baselineBoundary{root: root, tracked: map[string]bool{}}
	data, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		return b
	}
	b.gitOK = true
	for _, p := range strings.Split(string(data), "\x00") {
		if p != "" {
			b.tracked[p] = true
		}
	}
	data, err = exec.Command("git", "-C", root, "worktree", "list", "--porcelain", "-z").Output()
	separator := "\x00"
	if err != nil {
		// Git before worktree-list -z: porcelain quotes special paths.
		data, err = exec.Command("git", "-C", root, "-c", "core.quotePath=true", "worktree", "list", "--porcelain").Output()
		if err != nil {
			return b
		}
		separator = "\n"
	}
	for _, field := range strings.Split(string(data), separator) {
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		name := strings.TrimPrefix(field, "worktree ")
		if separator == "\n" && strings.HasPrefix(name, "\"") {
			decoded, err := strconv.Unquote(name)
			if err != nil {
				continue
			}
			name = decoded
		}
		rel, err := filepath.Rel(root, name)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rel = filepath.ToSlash(rel)
		if !b.hasTracked(rel) {
			b.worktrees = append(b.worktrees, rel)
		}
	}
	return b
}
func (b baselineBoundary) hasTracked(dir string) bool {
	for p := range b.tracked {
		if p == dir || strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}
func (b baselineBoundary) excludes(rel string) bool {
	if b.tracked[rel] {
		return false
	}
	for _, p := range b.worktrees {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}
func (b *baselineBoundary) discoverEnvironment(rel string) bool {
	if !b.gitOK {
		return false
	}
	// Require both the standard marker and Git's ignore verdict. Unmarked
	// directories and vendored/tracked environments remain protected.
	marker := rel + "/pyvenv.cfg"
	info, err := os.Lstat(filepath.Join(b.root, filepath.FromSlash(marker)))
	if err != nil || !info.Mode().IsRegular() || b.hasTracked(rel) || !gitIgnoredPaths(b.root, []string{marker})[marker] {
		return false
	}
	b.worktrees = append(b.worktrees, rel)
	return true
}
func excludedBaselinePaths(root string, paths []string) map[string]bool {
	b := newBaselineBoundary(root)
	seen := map[string]bool{}
	for _, p := range paths {
		if b.excludes(p) {
			continue
		}
		parts := strings.Split(p, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			if !seen[dir] {
				seen[dir] = true
				b.discoverEnvironment(dir)
			}
		}
	}
	excluded := runtimeLogPaths(root, paths)
	for _, p := range paths {
		if ignoreBaselinePath(p) || b.excludes(p) {
			excluded[p] = true
		}
	}
	return excluded
}
