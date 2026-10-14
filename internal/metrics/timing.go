package metrics

import (
	"context"
	"sync"
	"time"
)

// PhaseTiming is diagnostic only. Absent phases were not observed; a measured
// zero is distinct from absence. Nested phases overlap and must not be summed
// to estimate wall time or user waiting time.
type PhaseTiming struct {
	Calls      int64 `json:"calls"`
	DurationNS int64 `json:"duration_ns"`
}

type Timing map[string]PhaseTiming
type timingKey struct{}
type timingCollector struct {
	mu     sync.Mutex
	phases Timing
}

// WithTiming shares a collector with nested operations, without global state
// or a metrics write/lock on the measured path.
func WithTiming(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(timingKey{}).(*timingCollector); ok {
		return ctx
	}
	return context.WithValue(ctx, timingKey{}, &timingCollector{phases: Timing{}})
}

func StartPhase(ctx context.Context, phase string) func() {
	if ctx == nil {
		return func() {}
	}
	c, ok := ctx.Value(timingKey{}).(*timingCollector)
	if !ok {
		return func() {}
	}
	started := time.Now()
	var once sync.Once
	return func() {
		once.Do(func() {
			elapsed := time.Since(started).Nanoseconds()
			c.mu.Lock()
			defer c.mu.Unlock()
			p := c.phases[phase]
			p.Calls++
			p.DurationNS += elapsed
			c.phases[phase] = p
		})
	}
}

// ReadTiming returns a detached snapshot. In-flight spans remain absent until
// they finish; cancellation does not invent a duration for unfinished work.
func ReadTiming(ctx context.Context) Timing {
	if ctx == nil {
		return nil
	}
	c, ok := ctx.Value(timingKey{}).(*timingCollector)
	if !ok {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := Timing{}
	for k, v := range c.phases {
		result[k] = v
	}
	return result
}
