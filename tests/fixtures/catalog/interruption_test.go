package catalog

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInterruptedInitializerKeepsPublishedSeeds starts the real test binary
// as a catalog initializer. The child stops inside Prepare after publishing a
// different seed, and the parent kills it before the pending seed can be
// published. This tests the on-disk publication boundary across processes.
func TestInterruptedInitializerKeepsPublishedSeeds(t *testing.T) {
	const helperEnv = "GSB_CATALOG_INTERRUPTION_HELPER_ROOT"
	const started = "pending fixture builder started"
	if root := os.Getenv(helperEnv); root != "" {
		c, err := Create(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Prepare("ready", func(seed string) error {
			return os.WriteFile(filepath.Join(seed, "value"), []byte("published"), 0o600)
		}); err != nil {
			t.Fatal(err)
		}
		err = c.Prepare("interrupted", func(seed string) error {
			if err := os.WriteFile(filepath.Join(seed, "partial"), []byte("unfinished"), 0o600); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout, started); err != nil {
				return err
			}
			// The parent keeps stdin open until it has killed the process.
			var b [1]byte
			_, err := io.ReadFull(os.Stdin, b[:])
			return fmt.Errorf("initializer unexpectedly resumed: %w", err)
		})
		t.Fatalf("interrupted builder unexpectedly returned: %v", err)
	}

	sandbox := t.TempDir()
	root := filepath.Join(sandbox, "catalog")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(sandbox, "unrelated")
	if err := os.WriteFile(peer, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptedInitializerKeepsPublishedSeeds$")
	cmd.Env = append(os.Environ(), helperEnv+"="+root)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	type lineResult struct {
		line string
		err  error
	}
	line := make(chan lineResult, 1)
	go func() {
		value, err := bufio.NewReader(stdout).ReadString('\n')
		line <- lineResult{value, err}
	}()
	select {
	case result := <-line:
		if result.err != nil || strings.TrimSpace(result.line) != started {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			waited = true
			t.Fatalf("initializer did not reach pending builder: %q, %v; stderr: %s", result.line, result.err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		waited = true
		t.Fatalf("initializer did not reach pending builder within 30s; stderr: %s", stderr.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill initializer: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		waited = true
		t.Fatal("killed initializer exited successfully")
	}
	waited = true

	reader, err := Load(root)
	if err != nil {
		t.Fatalf("open catalog after initializer was killed: %v", err)
	}
	ready := t.TempDir()
	if err := reader.Materialize("ready", ready); err != nil {
		t.Fatalf("published seed unavailable after kill: %v", err)
	}
	if value, err := os.ReadFile(filepath.Join(ready, "value")); err != nil || string(value) != "published" {
		t.Fatalf("published seed changed: %q, %v", value, err)
	}
	if err := reader.Materialize("interrupted", t.TempDir()); err == nil {
		t.Fatal("killed initializer published its partial seed")
	}
	entries, err := os.ReadDir(filepath.Join(root, "seeds"))
	if err != nil {
		t.Fatal(err)
	}
	var preparing bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".preparing-") {
			preparing = true
			if err := reader.Materialize(entry.Name(), t.TempDir()); err == nil {
				t.Fatalf("unpublished temporary directory %q was consumable", entry.Name())
			}
		}
	}
	if !preparing {
		t.Fatal("kill did not leave the expected unpublished construction directory")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("clean private catalog: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("private catalog remains after cleanup: %v", err)
	}
	if value, err := os.ReadFile(peer); err != nil || string(value) != "preserve" {
		t.Fatalf("cleanup affected unrelated file: %q, %v", value, err)
	}
}
