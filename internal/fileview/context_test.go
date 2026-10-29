package fileview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestViewContextStopsDeclaredSourceReadWithoutChangingOriginalView(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := New(root, "", []Rule{{Path: ".", Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.WithContext(ctx).ReadFile("source.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("read ignored cancellation: %v", err)
	}
	if data, err := v.ReadFile("source.txt"); err != nil || string(data) != "original" {
		t.Fatalf("original view changed: %s %v", data, err)
	}
	if _, err := ResolveContext(ctx, root, "HEAD"); err == nil {
		t.Fatal("Git resolution ignored cancellation")
	}
}
