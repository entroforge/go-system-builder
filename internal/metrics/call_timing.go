package metrics

import (
	"context"
	"sync"
	"time"
)

type callTimingKey struct{}

// CallTimings is request-local. Only completed spans are observable; nested
// paths overlap, so durations must never be summed as total wall time.
type CallTimings struct {
	mu    sync.Mutex
	spans map[string]DurationStats
}

func WithCallTimings(ctx context.Context) (context.Context, *CallTimings) {
	if ctx == nil {
		ctx = context.Background()
	}
	c := &CallTimings{spans: map[string]DurationStats{}}
	return context.WithValue(ctx, callTimingKey{}, c), c
}

func Span(ctx context.Context, path string) func() {
	if ctx == nil {
		return func() {}
	}
	c, _ := ctx.Value(callTimingKey{}).(*CallTimings)
	if c == nil {
		return func() {}
	}
	started := time.Now()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			s := c.spans[path]
			s.Count++
			s.SumMS += time.Since(started).Milliseconds()
			c.spans[path] = s
		})
	}
}

func (c *CallTimings) Snapshot() map[string]DurationStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]DurationStats, len(c.spans))
	for k, v := range c.spans {
		out[k] = v
	}
	return out
}

// CompletedSpans reads the current request without writing a metrics file.
func CompletedSpans(ctx context.Context) map[string]DurationStats {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(callTimingKey{}).(*CallTimings)
	if c == nil {
		return nil
	}
	return c.Snapshot()
}
