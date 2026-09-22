package sharedmodel

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/pathscope"
)

// Editing-time disk reads obey the same root/worktree boundary. Git views keep
// their own snapshot semantics; a dirty symlink must not alter a pinned tree.
func bounded(root string, files fileview.Reader) fileview.Reader {
	if _, ok := files.(fileview.Disk); !ok {
		return files
	}
	return diskView{root: root, scope: pathscope.New(root)}
}

type diskView struct {
	root  string
	scope pathscope.Worktrees
}

func (d diskView) path(p string) (string, error) {
	if !filepath.IsAbs(p) {
		p = filepath.Join(d.root, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if !pathscope.Within(d.root, abs) || d.scope.Excludes(abs) {
		return "", fmt.Errorf("shared input outside authority scope: %s", p)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(d.root)
	if err != nil {
		return "", err
	}
	if !pathscope.Within(root, real) {
		return "", fmt.Errorf("shared input symlink escapes authority root: %s", p)
	}
	return abs, nil
}
func (d diskView) ReadFile(p string) ([]byte, error) {
	p, e := d.path(p)
	if e != nil {
		return nil, e
	}
	return os.ReadFile(p)
}
func (d diskView) ReadDir(p string) ([]os.DirEntry, error) {
	p, e := d.path(p)
	if e != nil {
		return nil, e
	}
	return os.ReadDir(p)
}
