package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestMainIntegrationPreservesUntrackedPrivateFile(t *testing.T) {
	fixture := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
	privatePath := filepath.Join(fixture.root, "personal-notes.txt")
	if err := os.WriteFile(privatePath, []byte("private Main material\n"), 0600); err != nil {
		t.Fatal(err)
	}
	exclude := filepath.Join(fixture.root, ".git", "info", "exclude")
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n/ignored-personal.txt\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ignoredPath := filepath.Join(fixture.root, "ignored-personal.txt")
	if err := os.WriteFile(ignoredPath, []byte("ignored private material\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inspection := fixture.inspect(t, "")
	if !inspection.Ready {
		t.Fatalf("actual responsibility could not be inspected: %+v", inspection)
	}
	result, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || result.Checkpoint.State != integration.StateVerified {
		t.Fatalf("unrelated private file blocked actual Main integration: %+v %v", result.Checkpoint, err)
	}
	if data, err := os.ReadFile(privatePath); err != nil || string(data) != "private Main material\n" {
		t.Fatalf("private Main file changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(ignoredPath); err != nil || string(data) != "ignored private material\n" {
		t.Fatalf("ignored Main file changed: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(fixture.root, canonicalSourceFile)); err != nil || string(data) != "main-check-marker\n" {
		t.Fatalf("actual Worker branch was not merged: %q %v", data, err)
	}
}

func TestMainIntegrationPreservesPrivateFileAtRealGitCollision(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		name := "untracked"
		if ignored {
			name = "ignored"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
			inspection := fixture.inspect(t, "")
			if !inspection.Ready {
				t.Fatalf("actual responsibility could not be inspected: %+v", inspection)
			}
			if ignored {
				exclude := filepath.Join(fixture.root, ".git", "info", "exclude")
				f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteString("\n/" + canonicalSourceFile + "\n"); err != nil {
					f.Close()
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			privatePath := filepath.Join(fixture.root, canonicalSourceFile)
			if err := os.MkdirAll(filepath.Dir(privatePath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(privatePath, []byte("private collision\n"), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
			if !errors.Is(err, integration.ErrMergeConflict) || result.Checkpoint.State != integration.StatePreserved {
				t.Fatalf("real Git collision was not preserved and reported: %+v %v", result.Checkpoint, err)
			}
			if data, err := os.ReadFile(privatePath); err != nil || string(data) != "private collision\n" {
				t.Fatalf("private collision was overwritten: %q %v", data, err)
			}
			if count := fileSize(t, fixture.counterPath); count != 0 {
				t.Fatalf("checks ran despite failed merge: %d", count)
			}
			if ignored {
				// Main resolves the real collision; the framework never moves it.
				moved := filepath.Join(fixture.root, "preserved-personal.txt")
				if err := os.Rename(privatePath, moved); err != nil {
					t.Fatal(err)
				}
				retry, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection, RetryPreserved: true}, fixture.integrateConfig())
				if err != nil || retry.Checkpoint.State != integration.StateVerified {
					t.Fatalf("resolved collision could not resume: %+v %v", retry.Checkpoint, err)
				}
				if data, err := os.ReadFile(moved); err != nil || string(data) != "private collision\n" {
					t.Fatalf("resolved private file changed: %q %v", data, err)
				}
			}
		})
	}
}

func TestMainIntegrationPreservesTrackedChanges(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "unstaged"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newCanonicalMainFixtureWithClosing(t, canonicalSourceFile, true, false)
			inspection := fixture.inspect(t, "")
			if !inspection.Ready {
				t.Fatalf("actual responsibility could not be inspected: %+v", inspection)
			}
			path := filepath.Join(fixture.root, canonicalTask)
			if err := os.WriteFile(path, []byte("local tracked change\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if staged {
				gitFixture(t, fixture.root, "add", canonicalTask)
			}
			if _, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); !errors.Is(err, integration.ErrDirtyWorktree) {
				t.Fatalf("tracked Main change was not preserved: %v", err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "local tracked change\n" {
				t.Fatalf("tracked Main file changed: %q %v", data, err)
			}
		})
	}
}
