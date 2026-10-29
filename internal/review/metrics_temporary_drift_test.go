package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const metricsTemporaryFixture = ".claude/.loop-metrics-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp"

func TestMetricsTemporaryDriftRequiresExactUntrackedRegularPath(t *testing.T) {
	newRoot := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		reviewGitBaseline(t, root)
		if err := os.Mkdir(filepath.Join(root, ".claude"), 0700); err != nil {
			t.Fatal(err)
		}
		return root
	}
	write := func(t *testing.T, root, path string) {
		t.Helper()
		name := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("partial bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scan := func(t *testing.T, root string) []string {
		t.Helper()
		drift, err := detectUndeclaredProductDriftWithState(root, &Plan{}, nil, root)
		if err != nil {
			t.Fatal(err)
		}
		return drift
	}
	t.Run("current and legacy", func(t *testing.T) {
		root := newRoot(t)
		for _, path := range []string{metricsTemporaryFixture, ".claude/.loop-metrics-42.tmp"} {
			write(t, root, path)
		}
		if drift := scan(t, root); len(drift) != 0 {
			t.Fatalf("temporary storage classified as drift: %v", drift)
		}
		neighbor := ".claude/.loop-metrics-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp.go"
		write(t, root, neighbor)
		if drift := scan(t, root); len(drift) != 1 || drift[0] != neighbor {
			t.Fatalf("neighbor not preserved as product drift: %v", drift)
		}
		if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(metricsTemporaryFixture))); err != nil || string(data) != "partial bytes" {
			t.Fatalf("scan altered staging: %q %v", data, err)
		}
	})
	for _, kind := range []string{"staged", "tracked", "staged-delete-and-recreated", "leaf-symlink", "directory", "backslash"} {
		t.Run(kind, func(t *testing.T) {
			root := newRoot(t)
			path := metricsTemporaryFixture
			switch kind {
			case "leaf-symlink":
				if err := os.Symlink("missing", filepath.Join(root, filepath.FromSlash(path))); err != nil {
					t.Skip(err)
				}
			case "directory":
				path += "/product.go"
				write(t, root, path)
			case "backslash":
				if runtime.GOOS == "windows" {
					t.Skip("backslash is a path separator on Windows")
				}
				path = strings.Replace(metricsTemporaryFixture, "/", "\\", 1)
				write(t, root, path)
			default:
				write(t, root, path)
				reviewFixtureGit(t, root, "add", "--", path)
				if kind != "staged" {
					reviewFixtureGit(t, root, "commit", "-qm", "tracked temporary name")
					if kind == "tracked" {
						write(t, root, path)
						if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("changed"), 0600); err != nil {
							t.Fatal(err)
						}
					} else {
						reviewFixtureGit(t, root, "rm", "--", path)
						write(t, root, path)
					}
				}
			}
			drift := scan(t, root)
			if len(drift) == 0 {
				t.Fatalf("%s acquired metrics exception", kind)
			}
		})
	}
	t.Run("parent symlink", func(t *testing.T) {
		root := t.TempDir()
		reviewGitBaseline(t, root)
		if err := os.Symlink(t.TempDir(), filepath.Join(root, ".claude")); err != nil {
			t.Skip(err)
		}
		if drift := scan(t, root); len(drift) == 0 {
			t.Fatal("symlinked control parent was hidden")
		}
	})
	t.Run("frozen", func(t *testing.T) {
		root := newRoot(t)
		write(t, root, metricsTemporaryFixture)
		plan := &Plan{FrozenSubjects: []FrozenSubject{{Path: metricsTemporaryFixture, SHA256: sha256Of([]byte("original"))}}}
		if err := verifyFrozenSubjects(root, plan, nil); err == nil || !strings.Contains(err.Error(), "drifted") {
			t.Fatalf("frozen changed bytes excused: %v", err)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := detectUndeclaredProductDriftWithState(t.TempDir(), &Plan{}, nil, "", ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan = %v", err)
	}
}
