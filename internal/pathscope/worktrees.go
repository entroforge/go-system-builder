// Package pathscope defines the project scan boundary independently of cwd.
package pathscope

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Worktrees is an immutable inventory for one scan; it never reanchors root
// to Git's common checkout (the authority may itself be a linked worktree).
type Worktrees struct {
	root     string
	excluded []string
}

func New(root string) Worktrees {
	root, _ = filepath.Abs(root)
	root = canonical(root)
	s := Worktrees{root: root, excluded: []string{filepath.Join(root, ".worktrees"), filepath.Join(root, ".claude", "worktrees")}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", root, "worktree", "list", "--porcelain", "-z").Output()
	if err == nil {
		for _, field := range strings.Split(string(out), "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				p := canonical(strings.TrimPrefix(field, "worktree "))
				if p != root && !Within(p, root) {
					s.excluded = append(s.excluded, p)
				}
			}
		}
	}
	return s
}
func (s Worktrees) Excludes(path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.root, path)
	}
	path = canonical(path)
	for _, p := range s.excluded {
		if Within(p, path) {
			return true
		}
	}
	return false
}
func Within(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// Resolve existing ancestors as well as missing output files.
func canonical(path string) string {
	path = filepath.Clean(path)
	if p, e := filepath.EvalSymlinks(path); e == nil {
		return p
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	if _, e := os.Lstat(path); os.IsNotExist(e) {
		return filepath.Join(canonical(parent), filepath.Base(path))
	}
	return path
}

// Walk and WalkDir apply the project boundary to bounded subtree scans too:
// a registered custom checkout can live under docs/, not only .worktrees/.
func Walk(root, start string, visit filepath.WalkFunc) error {
	scope := New(root)
	return filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
		abs, _ := filepath.Abs(path)
		if scope.Excludes(abs) {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(path, info, err)
	})
}
func WalkDir(root, start string, visit fs.WalkDirFunc) error {
	scope := New(root)
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		abs, _ := filepath.Abs(path)
		if scope.Excludes(abs) {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(path, entry, err)
	})
}
