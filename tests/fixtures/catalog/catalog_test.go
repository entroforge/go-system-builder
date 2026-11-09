package catalog

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func testCatalog(t *testing.T) (*Catalog, string) {
	t.Helper()
	root := t.TempDir()
	c, err := Create(root)
	if err != nil {
		t.Fatal(err)
	}
	return c, root
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestCopiesAreIsolatedIncludingGit(t *testing.T) {
	c, root := testCatalog(t)
	err := c.Prepare("independent-git", func(seed string) error {
		git(t, seed, "init", "-q")
		git(t, seed, "config", "user.name", "Fixture Test")
		git(t, seed, "config", "user.email", "fixture@example.invalid")
		write(t, filepath.Join(seed, "tracked"), "seed", 0o640)
		git(t, seed, "add", "tracked")
		git(t, seed, "commit", "-qm", "seed")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Prepare("another", func(string) error { return nil }); err == nil {
		t.Fatal("injected catalog allowed a consumer to publish a seed")
	}
	a, b := t.TempDir(), t.TempDir()
	workers := []string{filepath.Join(t.TempDir(), "worker"), filepath.Join(t.TempDir(), "worker")}
	if err := reader.Materialize("independent-git", a); err != nil {
		t.Fatal(err)
	}
	if err := reader.Materialize("independent-git", b); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(root, "seeds", "independent-git", "root")
	seedInfo, _ := os.Stat(filepath.Join(seed, "tracked"))
	copyInfo, _ := os.Stat(filepath.Join(a, "tracked"))
	if os.SameFile(seedInfo, copyInfo) {
		t.Fatal("instance hard-links a mutable seed file")
	}
	if runtime.GOOS != "windows" && copyInfo.Mode().Perm() != 0o640 {
		t.Fatalf("file mode = %o", copyInfo.Mode().Perm())
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, dir := range []string{a, b} {
		wg.Add(1)
		go func(i int, dir string) {
			defer wg.Done()
			if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte(fmt.Sprintf("instance-%d", i)), 0o640); err != nil {
				errs <- err
				return
			}
			for _, args := range [][]string{{"add", "tracked"}, {"commit", "-qm", fmt.Sprintf("instance-%d", i)}, {"worktree", "add", "-q", "-b", fmt.Sprintf("worker-%d", i), workers[i]}} {
				if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
					errs <- fmt.Errorf("git %v: %v: %s", args, err, out)
					return
				}
			}
		}(i, dir)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if git(t, a, "rev-parse", "HEAD") == git(t, b, "rev-parse", "HEAD") {
		t.Fatal("independent commits unexpectedly have the same HEAD")
	}
	if got, err := os.ReadFile(filepath.Join(seed, "tracked")); err != nil || string(got) != "seed" {
		t.Fatalf("seed changed: %q, %v", got, err)
	}
	if got := git(t, seed, "status", "--porcelain"); got != "" {
		t.Fatalf("seed Git state changed: %s", got)
	}
}

func TestUnfinishedSeedCannotBeConsumed(t *testing.T) {
	c, root := testCatalog(t)
	started := make(chan struct{})
	finish := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- c.Prepare("delayed", func(seed string) error {
			if err := os.WriteFile(filepath.Join(seed, "value"), []byte("done"), 0o600); err != nil {
				return err
			}
			close(started)
			<-finish
			return nil
		})
	}()
	<-started
	reader, err := Load(root)
	if err != nil {
		close(finish)
		t.Fatal(err)
	}
	if err := reader.Materialize("delayed", t.TempDir()); err == nil {
		close(finish)
		t.Fatal("seed was readable before publication")
	}
	close(finish)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := reader.Materialize("delayed", t.TempDir()); err != nil {
		t.Fatalf("published seed unavailable: %v", err)
	}
}

func TestSeedFailuresAreIndependentAndNeverPublishedPartially(t *testing.T) {
	c, root := testCatalog(t)
	if err := c.Prepare("broken", func(seed string) error {
		write(t, filepath.Join(seed, "partial"), "not a published seed", 0o600)
		return errors.New("original builder error")
	}); err == nil || !strings.Contains(err.Error(), "original builder error") {
		t.Fatalf("missing builder error: %v", err)
	}
	if err := c.Prepare("ready", func(seed string) error {
		return os.WriteFile(filepath.Join(seed, "data"), []byte("ready"), 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	reader, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Materialize("broken", t.TempDir()); err == nil || !strings.Contains(err.Error(), "original builder error") {
		t.Fatalf("failed seed was not diagnosed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "seeds", "broken", "root")); !os.IsNotExist(err) {
		t.Fatalf("failed seed retained consumable data: %v", err)
	}
	if err := reader.Materialize("ready", t.TempDir()); err != nil {
		t.Fatalf("unrelated seed failed: %v", err)
	}
	if err := reader.Materialize("missing", t.TempDir()); err == nil {
		t.Fatal("missing seed did not fail")
	}
	if err := c.Prepare("ready", func(string) error { return nil }); err == nil {
		t.Fatal("published seed was overwritten")
	}
}

func TestSymlinkCopyAndEscapeRejection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating Windows symlinks requires privileges unavailable to many CI runners")
	}
	c, root := testCatalog(t)
	if err := c.Prepare("safe", func(seed string) error {
		write(t, filepath.Join(seed, "file"), "ok", 0o600)
		return os.Symlink("file", filepath.Join(seed, "link"))
	}); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := c.Materialize("safe", dest); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(dest, "link")); err != nil || target != "file" {
		t.Fatalf("link lost: %q %v", target, err)
	}
	if err := c.Prepare("escape", func(seed string) error {
		return os.Symlink(root, filepath.Join(seed, "outside"))
	}); err == nil || !strings.Contains(err.Error(), "absolute fixture symlink") {
		t.Fatalf("external link accepted: %v", err)
	}
	if err := c.Prepare("missing-target", func(seed string) error {
		return os.Symlink("missing", filepath.Join(seed, "link"))
	}); err == nil || !strings.Contains(err.Error(), "resolve fixture symlink") {
		t.Fatalf("dangling link accepted: %v", err)
	}
}

func TestLoadRejectsMissingAndInvalidCatalog(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("empty injected path accepted")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("missing marker accepted")
	}
	c, root := testCatalog(t)
	_ = c
	write(t, filepath.Join(root, ".catalog", "format"), "bad", 0o600)
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "invalid format marker") {
		t.Fatalf("invalid marker accepted: %v", err)
	}
}

func TestGitExternalReferencesAreRejected(t *testing.T) {
	c, _ := testCatalog(t)
	err := c.Prepare("linked", func(seed string) error {
		git(t, seed, "init", "-q")
		return os.MkdirAll(filepath.Join(seed, ".git", "worktrees", "other"), 0o700)
	})
	if err == nil || !strings.Contains(err.Error(), "linked worktrees") {
		t.Fatalf("linked worktree accepted: %v", err)
	}
	err = c.Prepare("locked", func(seed string) error {
		git(t, seed, "init", "-q")
		return os.WriteFile(filepath.Join(seed, ".git", "index.lock"), nil, 0o600)
	})
	if err == nil || !strings.Contains(err.Error(), "Git lock") {
		t.Fatalf("Git lock accepted: %v", err)
	}
}
