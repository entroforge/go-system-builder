// Package fileview executes upstream source declarations. It never infers a
// source from a suffix, gitignore, or the presence of a working file.
package fileview

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/pathscope"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Rule struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}
type View struct {
	Root, Ref, Commit string
	rules             []Rule
	scope             pathscope.Worktrees
}

func Resolve(root, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("explicit Git reference is required")
	}
	out, e := git(root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if e != nil {
		return "", e
	}
	return strings.TrimSpace(string(out)), nil
}
func git(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	if e != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], e)
	}
	return out, nil
}
func New(root, ref string, rules []Rule) (*View, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("missing upstream file source contract")
	}
	v := &View{Root: root, Ref: ref, rules: append([]Rule(nil), rules...), scope: pathscope.New(root)}
	seen := map[string]bool{}
	needsGit := false
	for i, r := range rules {
		if r.Source != "git_tree" && r.Source != "disk" {
			return nil, fmt.Errorf("unknown file source %q", r.Source)
		}
		normalized, e := v.relative(r.Path)
		if e != nil {
			return nil, e
		}
		r.Path = normalized
		v.rules[i] = r
		if seen[r.Path] {
			return nil, fmt.Errorf("duplicate file source %q", r.Path)
		}
		seen[r.Path] = true
		needsGit = needsGit || r.Source == "git_tree"
	}
	if needsGit {
		v.Commit, e = Resolve(root, ref)
		if e != nil {
			return nil, e
		}
	}
	sort.SliceStable(v.rules, func(i, j int) bool { return len(v.rules[i].Path) > len(v.rules[j].Path) })
	return v, nil
}
func (v *View) relative(p string) (string, error) {
	if filepath.IsAbs(p) {
		r, e := filepath.Rel(v.Root, p)
		if e != nil {
			return "", e
		}
		p = r
	}
	p = filepath.Clean(p)
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file path escapes authority: %s", p)
	}
	return filepath.ToSlash(p), nil
}
func (v *View) source(p string) (string, string, error) {
	p, e := v.relative(p)
	if e != nil {
		return "", "", e
	}
	if v.scope.Excludes(p) {
		return "", "", fmt.Errorf("temporary worktree excluded: %s", p)
	}
	for _, r := range v.rules {
		prefix := strings.TrimSuffix(r.Path, "/")
		if prefix == "." || p == prefix || strings.HasPrefix(p, prefix+"/") {
			return p, r.Source, nil
		}
	}
	return "", "", fmt.Errorf("no upstream source declared for %s", p)
}
func (v *View) diskPath(p string) (string, error) {
	full := filepath.Join(v.Root, p)
	real, e := filepath.EvalSymlinks(full)
	if e != nil {
		return "", e
	}
	if !pathscope.Within(canonicalRoot(v.Root), real) || v.scope.Excludes(real) {
		return "", fmt.Errorf("disk path outside authority: %s", p)
	}
	return real, nil
}
func (v *View) ReadFile(path string) ([]byte, error) {
	p, source, e := v.source(path)
	if e != nil {
		return nil, e
	}
	if source == "disk" {
		full, e := v.diskPath(p)
		if e != nil {
			return nil, e
		}
		return os.ReadFile(full)
	}
	// Reject symlinks/submodules instead of treating their pointer bytes as content.
	out, e := git(v.Root, "ls-tree", "-z", v.Commit, "--", p)
	if e != nil {
		return nil, e
	}
	records := strings.Split(string(out), "\x00")
	if len(records) == 0 || records[0] == "" {
		return nil, &os.PathError{Op: "read Git blob", Path: p, Err: os.ErrNotExist}
	}
	fields := strings.Fields(strings.SplitN(records[0], "\t", 2)[0])
	if len(fields) != 3 || fields[1] != "blob" || fields[0] == "120000" {
		return nil, fmt.Errorf("not a regular Git blob: %s", p)
	}
	return git(v.Root, "cat-file", "blob", fields[2])
}
func (v *View) ReadDir(path string) ([]os.DirEntry, error) {
	p, source, e := v.source(path)
	if e != nil {
		return nil, e
	}
	if source == "disk" {
		full, e := v.diskPath(p)
		if e != nil {
			return nil, e
		}
		return os.ReadDir(full)
	}
	spec := v.Commit + ":"
	if p != "." {
		out, err := git(v.Root, "ls-tree", "-z", v.Commit, "--", p)
		if err != nil {
			return nil, err
		}
		if len(out) == 0 {
			return nil, &os.PathError{Op: "read Git directory", Path: p, Err: os.ErrNotExist}
		}
		spec += p
	}
	out, e := git(v.Root, "ls-tree", "-z", spec)
	if e != nil {
		return nil, e
	}
	entries := []os.DirEntry{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid Git tree")
		}
		fields := strings.Fields(parts[0])
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid Git tree entry")
		}
		mode := fs.FileMode(0644)
		if fields[1] == "tree" {
			mode = fs.ModeDir | 0755
		} else if fields[0] == "120000" {
			mode = fs.ModeSymlink
		} else if fields[1] != "blob" {
			continue
		}
		if !v.scope.Excludes(filepath.Join(p, parts[1])) {
			entries = append(entries, entry{name: parts[1], mode: mode})
		}
	}
	return entries, nil
}
func (v *View) Verify() error {
	if v.Commit == "" {
		return nil
	}
	current, e := Resolve(v.Root, v.Ref)
	if e != nil {
		return e
	}
	if current != v.Commit {
		return fmt.Errorf("file source moved: %s was %s, now %s; reevaluate", v.Ref, v.Commit, current)
	}
	return nil
}
func (v *View) Source(path string) (string, error) { _, s, e := v.source(path); return s, e }

type entry struct {
	name string
	mode fs.FileMode
}

func (e entry) Name() string               { return e.name }
func (e entry) IsDir() bool                { return e.mode.IsDir() }
func (e entry) Type() fs.FileMode          { return e.mode.Type() }
func (e entry) Info() (fs.FileInfo, error) { return e, nil }
func (e entry) Size() int64                { return 0 }
func (e entry) Mode() fs.FileMode          { return e.mode }
func (e entry) ModTime() time.Time         { return time.Time{} }
func (e entry) Sys() any                   { return nil }

func canonicalRoot(root string) string {
	if p, err := filepath.EvalSymlinks(root); err == nil {
		return p
	}
	return root
}
