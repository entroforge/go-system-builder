package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repository(t *testing.T) (*ExecutionRegistry, context.Context) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
		if _, err := Git(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n.worktrees/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "input.md"), []byte("approved"), 0600)
	if _, err := Git(ctx, root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, root, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	head, err := Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	saveControlState(t, root, map[string]any{"bound_req": map[string]any{"workspace": Binding{root, "test2", "main", head}.Map()}})
	b, err := New(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	return b, ctx
}
func TestPinnedTest2AndDirtyInputs(t *testing.T) {
	b, ctx := repository(t)
	state := map[string]any{"bound_req": map[string]any{"workspace": Binding{b.MainRoot, b.Branch, "main", b.BoundHead}.Map()}, "runtime_id": "loop-test", "baseline": map[string]any{"generation": 1}}
	for _, args := range [][]string{{"branch", "test3"}, {"branch", "test4"}} {
		if _, err := Git(ctx, b.MainRoot, args...); err != nil {
			t.Fatal(err)
		}
	}
	e, err := b.Plan(ctx, state, "assignment", "builder", []string{"input.md"}, nil, []string{"input.md"})
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(ctx, e); err != nil {
		t.Fatal(err)
	}
	head, _ := Git(ctx, e.Path, "rev-parse", "HEAD")
	main, _ := Git(ctx, b.MainRoot, "branch", "--show-current")
	if head != b.BoundHead || main != "test2" || e.TargetBranch != "test2" {
		t.Fatalf("baseline drift: %+v %s %s", e, head, main)
	}
	for _, kind := range []string{"unstaged", "staged", "untracked"} {
		t.Run(kind, func(t *testing.T) {
			path := "input.md"
			if kind == "untracked" {
				path = "new.md"
			}
			os.WriteFile(filepath.Join(b.MainRoot, path), []byte("not committed"), 0600)
			if kind == "staged" {
				Git(ctx, b.MainRoot, "add", path)
			}
			if err := b.CleanInputs(ctx); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("missed %s: %v", kind, err)
			}
			if kind == "untracked" {
				os.Remove(filepath.Join(b.MainRoot, path))
			} else {
				Git(ctx, b.MainRoot, "restore", "--staged", "--worktree", path)
			}
		})
	}
	Git(ctx, b.MainRoot, "switch", "test3")
	if err = b.Validate(ctx, b.MainRoot); err == nil {
		t.Fatal("accepted main branch drift")
	}
	current, _ := Git(ctx, b.MainRoot, "branch", "--show-current")
	if current != "test3" {
		t.Fatal("validation switched user's branch")
	}
}
func TestInputSymlinkEscape(t *testing.T) {
	b, ctx := repository(t)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(b.MainRoot, "link")); err != nil {
		t.Skip(err)
	}
	Git(ctx, b.MainRoot, "add", "link")
	Git(ctx, b.MainRoot, "commit", "-m", "link")
	_, err := b.Plan(ctx, map[string]any{"bound_req": map[string]any{"workspace": Binding{b.MainRoot, b.Branch, "main", b.BoundHead}.Map()}, "runtime_id": "loop-test"}, "assignment", "builder", nil, nil, []string{"link"})
	if err == nil {
		t.Fatal("accepted outside input")
	}
}

func TestBindingUsesUsersCurrentCheckoutAndBranch(t *testing.T) {
	for _, branch := range []string{"main", "feature/customer-a", "release-2026.09"} {
		t.Run(branch, func(t *testing.T) {
			original, ctx := repository(t)
			userRoot := filepath.Join(t.TempDir(), "user-project")
			if _, err := Git(ctx, original.MainRoot, "worktree", "add", "-b", branch, userRoot); err != nil {
				t.Fatal(err)
			}
			saveControlState(t, userRoot, map[string]any{"bound_req": map[string]any{"workspace": Binding{userRoot, branch, "main", original.BoundHead}.Map()}})
			binding, err := New(ctx, userRoot)
			if err != nil {
				t.Fatal(err)
			}
			canonical, _ := Canonical(userRoot)
			if binding.MainRoot != canonical || binding.Branch != branch {
				t.Fatalf("did not bind user's checkout: %+v", binding)
			}
			if binding.CommonDir != original.CommonDir {
				t.Fatal("lost shared repository identity")
			}
			primaryBranch, _ := Git(ctx, original.MainRoot, "branch", "--show-current")
			if primaryBranch != "test2" {
				t.Fatal("binding modified another checkout")
			}
		})
	}
}

func TestExecutionRegistryCannotOverrideRequirementAuthority(t *testing.T) {
	b, ctx := repository(t)
	state := map[string]any{"bound_req": map[string]any{"workspace": Binding{b.MainRoot, b.Branch, "main", b.BoundHead}.Map()}}
	if err := b.ValidateAuthority(state); err != nil {
		t.Fatal(err)
	}
	b.Branch = "another-branch"
	if err := b.ValidateAuthority(state); err == nil {
		t.Fatal("execution registry overrode bound development branch")
	}
	saveControlState(t, b.MainRoot, map[string]any{})
	if _, err := New(ctx, b.MainRoot); err == nil {
		t.Fatal("created execution registry without bound requirement authority")
	}
}
