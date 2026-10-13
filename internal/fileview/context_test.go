package fileview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileViewStopsReadingAndVerifyingAfterCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evidence.json"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	v, err := NewContext(ctx, root, "HEAD", []Rule{{".", "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := v.ReadFile("evidence.json"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := v.ReadDir("."); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := v.Verify(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ResolveContext(ctx, root, "HEAD"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
