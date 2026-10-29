package targetedauthoring

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func originalGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
func TestNormalOriginalPreservesProductAndUnknownNeighbors(t *testing.T) {
	root := t.TempDir()
	originalGit(t, root, "init", "-q")
	originalGit(t, root, "config", "user.email", "fixture@example.test")
	originalGit(t, root, "config", "user.name", "fixture")
	os.WriteFile(filepath.Join(root, "product"), []byte("product"), 0644)
	originalGit(t, root, "add", "product")
	originalGit(t, root, "commit", "-qm", "base")
	path := ".loop-submissions/s7/packet-fixture/reviewer/answer.json"
	if err := CheckNormalOriginal(context.Background(), root, path); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(root, filepath.FromSlash(path))
	os.MkdirAll(filepath.Dir(absolute), 0755)
	os.WriteFile(absolute, []byte("actual draft"), 0644)
	sibling := filepath.Join(filepath.Dir(absolute), "unknown-product")
	os.WriteFile(sibling, []byte("unknown"), 0644)
	if err := CheckNormalOriginal(context.Background(), root, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("qualification removed unknown work")
	}
	originalGit(t, root, "add", path)
	if err := CheckNormalOriginal(context.Background(), root, path); err == nil {
		t.Fatal("staged author path hidden as private")
	}
	originalGit(t, root, "reset", "-q", "HEAD", "--", path)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".loop-submissions/\n"), 0644)
	if err := CheckNormalOriginal(context.Background(), root, path); err == nil {
		t.Fatal("ignored original gained normal-source capability")
	}
	os.Remove(filepath.Join(root, ".gitignore"))
	os.Remove(absolute)
	os.Symlink(filepath.Join(root, "product"), absolute)
	if err := CheckNormalOriginal(context.Background(), root, path); err == nil {
		t.Fatal("symlink original accepted")
	}
}
