//go:build !windows

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTimeoutKillsDescendants(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := CommandCheckRunner(ctx, root, "(sleep 0.4; echo leaked > marker) & wait")
	if err == nil {
		t.Fatal("timeout passed")
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatal("descendant survived cancellation")
	}
}
func TestSuccessfulShellCannotLeaveBackgroundWriter(t *testing.T) {
	root := t.TempDir()
	if err := CommandCheckRunner(context.Background(), root, "(sleep 0.3; echo leaked > marker) &"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
		t.Fatal("background descendant survived successful shell")
	}
}
