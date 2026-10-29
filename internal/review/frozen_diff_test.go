package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// SIM-05: the pre-recovery comparison must separate pinned drift (restore)
// from unpinned current files (preserve, never blanket-delete).
func TestDiffFrozenSourcesSeparatesRestoreAndPreserve(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	pinned := filepath.Join(root, "internal", "example", "service.go")
	if err := os.MkdirAll(filepath.Dir(pinned), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinned, []byte("drifted"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unpinned current file (e.g. S8 process output) must be preserved.
	unpinned := filepath.Join(root, "internal", "example", "s8-notes.txt")
	if err := os.WriteFile(unpinned, []byte("investigation notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &Plan{FrozenSubjects: []FrozenSubject{
		{Path: "internal/example/service.go", SHA256: sha256Of([]byte("baseline")), Kind: "product_code"},
		{Path: "internal/example/removed.go", SHA256: sha256Of([]byte("gone")), Kind: "product_code"},
	}}
	diff, err := DiffFrozenSources(root, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Restore) != 1 || diff.Restore[0] != "internal/example/service.go" {
		t.Fatalf("drifted pinned subject must be listed for restore: %+v", diff)
	}
	if len(diff.Missing) != 1 || diff.Missing[0] != "internal/example/removed.go" {
		t.Fatalf("absent pinned subject must be listed as missing: %+v", diff)
	}
	found := false
	for _, path := range diff.Preserve {
		if path == "internal/example/s8-notes.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unpinned current file must be listed for preserve: %+v", diff)
	}
	report := FrozenDiffReport(diff)
	for _, want := range []string{"restore to exact pinned bytes", "preserve, never delete"} {
		if !strings.Contains(report, want) {
			t.Fatalf("recovery report omits %q: %s", want, report)
		}
	}
}
