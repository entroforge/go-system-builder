//go:build !windows

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHookDescendantsCannotOutliveCancellation(t *testing.T) {
	b, _ := repository(t)
	marker := filepath.Join(t.TempDir(), "late-write")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }
	hook := "#!/bin/sh\n(sleep 0.5; echo late > " + quote(marker) + ") &\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(b.CommonDir, "hooks/pre-commit"), []byte(hook), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := Git(ctx, b.MainRoot, "commit", "--allow-empty", "-m", "cancelled"); err == nil {
		t.Fatal("Git hook ignored deadline")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("Git cancellation waited on a surviving hook")
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("Git hook descendant wrote after cancellation")
	}
}
