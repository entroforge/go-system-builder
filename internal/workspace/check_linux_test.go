package workspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsolatedCheckCannotWriteMainOrSourceWorker(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("Bubblewrap not installed")
	}
	b, e, _, ctx := workerFixture(t)
	protected := filepath.Join(b.MainRoot, "protected.txt")
	if err := os.WriteFile(protected, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// /tmp is private in the sandbox; use a path through the original
	// read-only root which is visible on normal (non-/tmp) checkouts too.
	e.Checks = []string{"echo generated > generated.txt; test -f generated.txt; if echo broken > '" + protected + "'; then exit 42; fi; echo isolated-pass"}
	var out bytes.Buffer
	path, err := b.RunCheck(ctx, e, 0, &out)
	if err != nil {
		t.Fatalf("isolated check: %v; output=%s", err, &out)
	}
	if !strings.Contains(out.String(), "isolated-pass") || path == "" {
		t.Fatalf("missing receipt/output: %s %s", path, &out)
	}
	data, _ := os.ReadFile(protected)
	if string(data) != "original" {
		t.Fatal("check modified Main")
	}
	if _, err := os.Stat(filepath.Join(e.Path, "generated.txt")); !os.IsNotExist(err) {
		t.Fatal("check changed source Worker")
	}
	if _, err := exec.LookPath("python3"); err == nil {
		e.Checks = []string{"python3 -c 'import socket\nfor family in (socket.AF_INET, socket.AF_UNIX):\n try: socket.socket(family)\n except PermissionError: pass\n else: raise AssertionError(\"socket syscall was allowed\")'"}
		if _, err := b.RunCheck(ctx, e, 0, &out); err != nil {
			t.Fatalf("socket isolation test failed: %v; output=%s", err, &out)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	e.Checks = []string{"sleep 30"}
	started := time.Now()
	if _, err = b.RunCheck(ctx, e, 0, &out); err == nil {
		t.Fatal("timeout accepted")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("timed out process kept running")
	}
}
