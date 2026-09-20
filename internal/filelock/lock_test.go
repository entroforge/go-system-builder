package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedContentionAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.lock")
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = Acquire(ctx, path)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded/wrong contention: %v", err)
	}
	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	release, err = Acquire(ctx2, path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
