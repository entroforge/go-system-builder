// Package fileview executes upstream source declarations. It never infers a
// source from a suffix, gitignore, or the presence of a working file.
package fileview

import (
	"context"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/metrics"
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
	ctx               context.Context
	Root, Ref, Commit string
	rules             []Rule
	scope             pathscope.Worktrees
}

func Resolve(root, ref string) (string, error) {
	return ResolveContext(context.Background(), root, ref)
}
func ResolveContext(ctx context.Context, root, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("explicit Git reference is required")
	}
	out, e := gitContext(ctx, root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if e != nil {
		return "", e
	}
	return strings.TrimSpace(string(out)), nil
}
func git(root string, args ...string) ([]byte, error) {
	return gitContext(context.Background(), root, args...)
}
func gitContext(parent context.Context, root string, args ...string) ([]byte, error) {
	if parent == nil {
		parent = context.Background()
	}
	defer metrics.Span(parent, "source/git")()
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if e != nil {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), root, err)
		}
		detail := strings.TrimSpace(string(out))
		if len(detail) > 1024 {
			detail = detail[:1024]
		}
		return nil, fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), root, e, detail)
	}
	return out, nil
}
func New(root, ref string, rules []Rule) (*View, error) {
	return NewContext(context.Background(), root, ref, rules)
}
func NewContext(ctx context.Context, root, ref string, rules []Rule) (*View, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, e := declaredView(root, rules)
	if e != nil {
		return nil, e
	}
	v.ctx, v.Ref = ctx, ref
	for _, rule := range v.rules {
		if rule.Source == "git_tree" {
			v.Commit, e = ResolveContext(ctx, v.Root, ref)
			if e != nil {
				return nil, e
			}
			break
		}
	}
	return v, nil
}

// declaredView validates and orders the source declarations without resolving
// their Git reference or reading their contents. Only NewContext returns a
// usable source reader; guidance queries the declarations via SourceForRules.
func declaredView(root string, rules []Rule) (*View, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("missing upstream file source contract")
	}
	v := &View{Root: root, rules: append([]Rule(nil), rules...), scope: pathscope.New(root)}
	seen := map[string]bool{}
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
	}
	sort.SliceStable(v.rules, func(i, j int) bool { return len(v.rules[i].Path) > len(v.rules[j].Path) })
	return v, nil
}

// SourceForRules projects a declared source without opening a Git tree. It
// shares normalization, containment and longest-prefix resolution with View.
// A declaration is a delivery hint, not proof that the file exists or matches
// its formal source; those checks remain with the original consumer.
func SourceForRules(root, path string, rules []Rule) (string, error) {
	v, err := declaredView(root, rules)
	if err != nil {
		return "", err
	}
	return v.Source(path)
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
	if err := v.context().Err(); err != nil {
		return nil, err
	}
	defer metrics.Span(v.context(), "source/read")()
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
	out, e := immutableGit(v.context(), v.Root, "ls-tree", "-z", v.Commit, "--", p)
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
	return immutableGit(v.context(), v.Root, "cat-file", "blob", fields[2])
}
func (v *View) ReadDir(path string) ([]os.DirEntry, error) {
	if err := v.context().Err(); err != nil {
		return nil, err
	}
	defer metrics.Span(v.context(), "source/read_dir")()
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
		out, err := immutableGit(v.context(), v.Root, "ls-tree", "-z", v.Commit, "--", p)
		if err != nil {
			return nil, err
		}
		if len(out) == 0 {
			return nil, &os.PathError{Op: "read Git directory", Path: p, Err: os.ErrNotExist}
		}
		spec += p
	}
	out, e := immutableGit(v.context(), v.Root, "ls-tree", "-z", spec)
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
	if err := v.context().Err(); err != nil {
		return err
	}
	defer metrics.Span(v.context(), "source/verify")()
	if v.Commit == "" {
		return nil
	}
	current, e := ResolveContext(v.context(), v.Root, v.Ref)
	if e != nil {
		return e
	}
	if current != v.Commit {
		return fmt.Errorf("file source moved: %s was %s, now %s; reevaluate", v.Ref, v.Commit, current)
	}
	return nil
}

// Source returns the declared source for path using the same containment and
// source-rule resolution as ReadFile and ReadDir.
func (v *View) Source(path string) (string, error) {
	_, source, err := v.source(path)
	return source, err
}

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

func (v *View) context() context.Context {
	if v.ctx != nil {
		return v.ctx
	}
	return context.Background()
}
func (v *View) WithContext(ctx context.Context) *View { copy := *v; copy.ctx = ctx; return &copy }
