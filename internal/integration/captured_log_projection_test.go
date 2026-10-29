package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestCapturedLogProjectionPreservesTrackedAndNeighborChecks(t *testing.T) {
	for _, mode := range []string{"untracked-output", "tracked-edit", "changed-bytes", "unknown-neighbor"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			git := func(args ...string) {
				t.Helper()
				if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
			}
			git("init")
			git("config", "user.name", "fixture")
			git("config", "user.email", "fixture@example.com")
			data := []byte("original retained check output\n")
			path := "logs/check.txt"
			writeProjectionObject(t, root, path, data)
			if mode == "tracked-edit" {
				git("add", path)
				git("commit", "-qm", "tracked product fixture")
				if err := os.WriteFile(filepath.Join(root, path), []byte("modified tracked output"), 0600); err != nil {
					t.Fatal(err)
				}
				data = []byte("modified tracked output")
			}
			evidence := map[string]string{path: workpacket.Digest(data)}
			if mode == "changed-bytes" {
				writeProjectionObject(t, root, path, []byte("changed after capture"))
			}
			if mode == "unknown-neighbor" {
				writeProjectionObject(t, root, "logs/product.go", []byte("package product\n"))
			}
			clean, err := worktreeCleanWithProjection(t.Context(), root, nil, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if clean != (mode == "untracked-output") {
				t.Fatalf("%s clean=%v", mode, clean)
			}
		})
	}
}
