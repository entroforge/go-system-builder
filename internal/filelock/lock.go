// Package filelock provides process-owned locks. Lock files must never be
// unlinked: doing so would create two independently locked inodes.
package filelock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func Acquire(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := tryLock(f); err == nil {
			var once sync.Once
			return func() { once.Do(func() { _ = unlock(f); _ = f.Close() }) }, nil
		} else if !contended(err) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("workspace busy: %s: %w", path, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
