package buildsurface

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	loopruntime "github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/mainreceipt"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

type readCacheKey struct{}
type immutableReads struct {
	mu       sync.Mutex
	data     map[string][]byte
	inflight map[string]*immutableRead
}

type immutableRead struct {
	done chan struct{}
	data []byte
	err  error
}

// WithReadCache scopes reuse to one request and its derived contexts. Only
// successful reads of pinned Git objects enter it. Disk/evidence reads, current
// Runtime facts and source authorization remain live checks.
func WithReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = mainreceipt.WithReadCache(fileview.WithReadCache(loopruntime.WithJournalReadCache(ctx)))
	if _, present := ctx.Value(readCacheKey{}).(*immutableReads); present {
		return ctx
	}
	return context.WithValue(ctx, readCacheKey{}, &immutableReads{data: map[string][]byte{}, inflight: map[string]*immutableRead{}})
}

// fixedGitBytes is reserved for exact object operations in treeBlobContext and
// changedPathsContext. HEAD, status, branch resolution and disk reads bypass it.
func fixedGitBytes(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache, _ := ctx.Value(readCacheKey{}).(*immutableReads)
	if cache == nil {
		return gitBytesContext(ctx, root, limit, args...)
	}
	repository, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	repository, err = filepath.Abs(repository)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprint(limit) + "\x00" + repository + "\x00" + strings.Join(args, "\x00")
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cache.mu.Lock()
		if data, found := cache.data[key]; found {
			cache.mu.Unlock()
			defer metrics.Span(ctx, "source/git_cache_hit")()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return append([]byte(nil), data...), nil
		}
		if pending := cache.inflight[key]; pending != nil {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending.done:
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// A sibling may have a shorter deadline. Its cancellation must not
			// cancel this caller or become a permanently cached failure.
			if errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
				continue
			}
			if pending.err != nil {
				return nil, pending.err
			}
			defer metrics.Span(ctx, "source/git_cache_hit")()
			return append([]byte(nil), pending.data...), nil
		}
		pending := &immutableRead{done: make(chan struct{})}
		cache.inflight[key] = pending
		cache.mu.Unlock()
		data, err := gitBytesContext(ctx, root, limit, args...)
		if err == nil {
			err = ctx.Err()
		}
		cache.mu.Lock()
		pending.err = err
		if err == nil {
			pending.data = append([]byte(nil), data...)
			cache.data[key] = pending.data
		}
		delete(cache.inflight, key)
		close(pending.done)
		cache.mu.Unlock()
		return data, err
	}
}

func fixedGitText(ctx context.Context, root string, args ...string) (string, error) {
	data, err := fixedGitBytes(ctx, root, 2<<20, args...)
	return strings.TrimSpace(string(data)), err
}
