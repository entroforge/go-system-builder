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
	root        string
	tracked     map[string]bool
	trackedDirs map[string]bool
	worktrees   []string
	gitOK       bool
	cache       map[string]bool
}

func newBaselineBoundary(root string) baselineBoundary {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return baselineBoundary{root: root, tracked: map[string]bool{}}
	}
	if resolved, resolveErr := filepath.EvalSymlinks(rootAbs); resolveErr == nil {
		rootAbs = resolved
	}
	b := baselineBoundary{root: rootAbs, tracked: map[string]bool{}}
	data, err := exec.Command("git", "-C", rootAbs, "ls-files", "-z").Output()
	if err != nil {
		return b
	}
	b.gitOK = true
	b.cache = map[string]bool{}
	// Include HEAD ownership so a staged deletion cannot acquire cache exemption.
	if head, e := exec.Command("git", "-C", rootAbs, "ls-tree", "-r", "--name-only", "-z", "HEAD").Output(); e == nil {
		data = append(data, head...)
	}
	for _, p := range strings.Split(string(data), "\x00") {
		if p != "" {
			b.addTracked(p)
		}
	}
	data, err = exec.Command("git", "-C", rootAbs, "worktree", "list", "--porcelain", "-z").Output()
	separator := "\x00"
	if err != nil {
		// Git before worktree-list -z: porcelain quotes special paths.
		data, err = exec.Command("git", "-C", rootAbs, "-c", "core.quotePath=true", "worktree", "list", "--porcelain").Output()
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
		rel, err := filepath.Rel(rootAbs, name)
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

// addTracked builds exact-file and ancestor indexes once per scan.
func (b *baselineBoundary) addTracked(path string) {
	if b.tracked == nil {
		b.tracked = map[string]bool{}
	}
	if b.trackedDirs == nil {
		b.trackedDirs = map[string]bool{}
	}
	b.tracked[path] = true
	for i := strings.LastIndexByte(path, '/'); i >= 0; i = strings.LastIndexByte(path, '/') {
		path = path[:i]
		b.trackedDirs[path] = true
	}
}
func (b baselineBoundary) hasTracked(path string) bool {
	return b.tracked[path] || b.trackedDirs[path]
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
	return b.cacheExcluded(rel)
}

// Cache names alone are never sufficient. A protected descendant prevents
// directory pruning, and unavailable Git never grants a cache exemption.
func (b baselineBoundary) cacheExcluded(rel string) bool {
	if !b.gitOK || b.hasTracked(rel) {
		return false
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		switch part {
		case "node_modules", "dist", "coverage", ".vite", ".turbo", ".nuxt", ".output", "test-results", "playwright-report", "blob-report", ".playwright", "tmp", "temp":
			candidate := strings.Join(parts[:i+1], "/")
			if b.hasTracked(candidate) {
				continue
			}
			ignored, ok := b.cache[candidate]
			if !ok {
				ignored = gitIgnoredPaths(b.root, []string{candidate})[candidate]
				b.cache[candidate] = ignored
			}
			if ignored {
				return true
			}
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
	if err != nil || !info.Mode().IsRegular() || b.hasTracked(rel) {
		return false
	}
	ignored := gitIgnoredPaths(b.root, []string{rel, marker})
	if !ignored[rel] || !ignored[marker] {
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
	// Stored baseline membership is historical authority. Current ignore rules
	// cannot reclassify its files, including legacy logs without provenance.
	excluded := map[string]bool{}
	for _, p := range paths {
		if b.hasTracked(p) && !isControlPlanePath(p, false) {
			delete(excluded, p)
			continue
		}
		// Stored membership is historical authority: a committed deletion must
		// not gain cache exemption merely because HEAD/index no longer own it.
		foreign := false
		for _, dir := range b.worktrees {
			if p == dir || strings.HasPrefix(p, dir+"/") {
				foreign = true
				break
			}
		}
		if ignoreBaselinePath(p) || foreign {
			excluded[p] = true
		}
	}
	return excluded
}
