package dispatch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestFinalAuditDispatchViewUsesCatalogSources verifies that dispatch and the
// Controller's catalog view consume identical bytes for both the default
// root rule and a nested override. The nested override is injected into a
// complete Loop Definition copy so this exercises the production catalog
// loading path rather than a hand-built comparator only.
func TestFinalAuditDispatchViewUsesCatalogSources(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Final audit")
	t.Setenv("GIT_AUTHOR_EMAIL", "final-audit@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Final audit")
	t.Setenv("GIT_COMMITTER_EMAIL", "final-audit@example.invalid")

	root := t.TempDir()
	definition, err := os.ReadFile("../../docs/control/loop-definition.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalogJSON map[string]any
	if err := json.Unmarshal(definition, &catalogJSON); err != nil {
		t.Fatal(err)
	}
	sources, ok := catalogJSON["file_sources"].([]any)
	if !ok {
		t.Fatal("current Loop Definition has no file_sources array")
	}
	catalogJSON["file_sources"] = append(sources, map[string]any{"path": "custom", "source": "disk"})
	definition, err = json.MarshalIndent(catalogJSON, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}
	gitFinalAudit(t, root, "init", "-b", "development")
	gitFinalAudit(t, root, "config", "user.name", "Final audit")
	gitFinalAudit(t, root, "config", "user.email", "final-audit@example.invalid")

	path := filepath.Join(root, "custom", "runtime-input.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(root, "docs", "default-input.txt")
	if err := os.WriteFile(defaultPath, []byte("default-committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitFinalAudit(t, root, "add", "docs/control/loop-definition.json", "custom/runtime-input.json", "docs/default-input.txt")
	gitFinalAudit(t, root, "commit", "-qm", "committed custom input")
	if err := os.WriteFile(path, []byte("disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultPath, []byte("default-disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claudeEvidence := filepath.Join(root, ".claude", "evidence", "mutable.json")
	if err := os.MkdirAll(filepath.Dir(claudeEvidence), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeEvidence, []byte("disk-evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := map[string]any{
		"bound_req": map[string]any{
			"workspace": map[string]any{"dev_branch": "development"},
		},
	}
	hardcoded, err := View(root, state)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := fileview.New(root, "refs/heads/development", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		path       string
		wantSource string
		wantBytes  string
	}{
		{name: "nested override", path: "custom/runtime-input.json", wantSource: "disk", wantBytes: "disk\n"},
		{name: "default root", path: "docs/default-input.txt", wantSource: "git_tree", wantBytes: "default-committed\n"},
		{name: "catalog disk rule", path: ".claude/evidence/mutable.json", wantSource: "disk", wantBytes: "disk-evidence\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, view := range map[string]*fileview.View{"dispatch": hardcoded, "catalog": configured} {
				source, err := view.Source(tc.path)
				if err != nil || source != tc.wantSource {
					t.Fatalf("%s source = %q, %v; want %s", name, source, err, tc.wantSource)
				}
				data, err := view.ReadFile(tc.path)
				if err != nil {
					t.Fatalf("%s read: %v", name, err)
				}
				if string(data) != tc.wantBytes {
					t.Fatalf("%s read %q, want %q", name, data, tc.wantBytes)
				}
			}
		})
	}
}

func TestFinalAuditDispatchViewRejectsMissingOrMalformedCatalog(t *testing.T) {
	state := map[string]any{"bound_req": map[string]any{
		"workspace": map[string]any{"dev_branch": "development"},
	}}
	t.Run("missing", func(t *testing.T) {
		root := t.TempDir()
		if _, err := View(root, state); err == nil || !strings.Contains(err.Error(), "read Loop Definition") {
			t.Fatalf("missing catalog must fail closed, err=%v", err)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "docs", "control", "loop-definition.json"), []byte("{\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := View(root, state); err == nil || !strings.Contains(err.Error(), "decode Loop Definition") {
			t.Fatalf("malformed catalog must fail closed, err=%v", err)
		}
	})
}

func gitFinalAudit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Final audit",
		"GIT_AUTHOR_EMAIL=final-audit@example.invalid",
		"GIT_COMMITTER_NAME=Final audit",
		"GIT_COMMITTER_EMAIL=final-audit@example.invalid",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
