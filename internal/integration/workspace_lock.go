package integration

import (
	"context"
	"errors"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"path/filepath"
	"time"
)

var ErrIntegrationBusy = errors.New("integration workspace busy; retry the same assignment after its current integration completes")

type lockContextKey struct{}

// LockWorkspace covers inspection as well as merge/check/cleanup. Calls from
// the same controller operation share ownership through context, never globally.
func LockWorkspace(ctx context.Context, root string) (context.Context, func(), error) {
	path, err := filepath.Abs(root)
	if err != nil {
		return ctx, nil, err
	}
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	if held, _ := ctx.Value(lockContextKey{}).(string); held == path {
		return ctx, func() {}, nil
	}
	budget, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	release, err := filelock.Acquire(budget, filepath.Join(path, ".claude", "integration.lock"))
	if err != nil {
		return ctx, nil, errors.Join(ErrIntegrationBusy, err)
	}
	return context.WithValue(ctx, lockContextKey{}, path), release, nil
}
