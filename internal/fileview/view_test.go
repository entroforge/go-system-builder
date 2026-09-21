package fileview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractSelectsCommittedAndDiskInputs(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s %v", args, out, e)
		}
	}
	write := func(p, s string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755)
		if e := os.WriteFile(filepath.Join(root, p), []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	git("init", "-qb", "feature/req")
	write("docs/a.md", "committed")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "base")
	write("docs/a.md", "dirty")
	write("docs/staged.md", "staged")
	git("add", "docs/staged.md")
	write("docs/untracked.md", "new")
	write("runtime/evidence.json", "disk evidence")
	v, e := New(root, "refs/heads/feature/req", []Rule{{".", "git_tree"}, {"runtime", "disk"}})
	if e != nil {
		t.Fatal(e)
	}
	data, e := v.ReadFile("docs/a.md")
	if e != nil || string(data) != "committed" {
		t.Fatalf("dirty qualified: %s %v", data, e)
	}
	for _, p := range []string{"docs/staged.md", "docs/untracked.md", "../escape"} {
		if _, e := v.ReadFile(p); e == nil {
			t.Errorf("accepted %s", p)
		}
	}
	entries, e := v.ReadDir("docs")
	if e != nil || len(entries) != 1 || entries[0].Name() != "a.md" {
		t.Fatalf("mixed discovery: %v %v", entries, e)
	}
	data, e = v.ReadFile("runtime/evidence.json")
	if e != nil || string(data) != "disk evidence" {
		t.Fatalf("disk input: %s %v", data, e)
	}
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "advance")
	if v.Verify() == nil {
		t.Fatal("source movement not detected")
	}
	if _, e := v.ReadFile("docs/staged.md"); e == nil {
		t.Fatal("snapshot moved with branch")
	}
	if _, e := New(root, "HEAD", nil); e == nil {
		t.Fatal("implicit source accepted")
	}
	if _, e := New(root, "HEAD", []Rule{{".", "guess"}}); e == nil || !strings.Contains(e.Error(), "unknown") {
		t.Fatal(e)
	}
}
