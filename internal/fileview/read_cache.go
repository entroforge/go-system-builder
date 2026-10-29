package fileview

import (
	"context"
	"errors"
	"strings"
	"sync"

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
// Runtime facts, source-rule resolution and Verify remain live checks.
func WithReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, present := ctx.Value(readCacheKey{}).(*immutableReads); present {
		return ctx
	}
	return context.WithValue(ctx, readCacheKey{}, &immutableReads{data: map[string][]byte{}, inflight: map[string]*immutableRead{}})
}

// Only View's resolved commit/tree and blob reads call this function; resolving
// a mutable branch reference must always use gitContext directly.
func immutableGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache, _ := ctx.Value(readCacheKey{}).(*immutableReads)
	if cache == nil {
		return gitContext(ctx, root, args...)
	}
	key := root + "\x00" + strings.Join(args, "\x00")
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
		data, err := gitContext(ctx, root, args...)
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
