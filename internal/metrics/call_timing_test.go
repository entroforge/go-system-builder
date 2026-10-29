package metrics

import (
	"context"
	"sync"
	"testing"
)

func TestCallTimingsPublishOnlyCompletedSpansAndKeepCallsIsolated(t *testing.T) {
	ctx, spans := WithCallTimings(context.Background())
	end := Span(ctx, "runtime/lock_hold")
	if len(spans.Snapshot()) != 0 {
		t.Fatal("unfinished span was published")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); Span(ctx, "runtime/lock_hold/validate")() }()
	}
	wg.Wait()
	end()
	end()
	got := spans.Snapshot()
	if got["runtime/lock_hold"].Count != 1 || got["runtime/lock_hold/validate"].Count != 8 {
		t.Fatalf("bad nested counts: %+v", got)
	}
	_, other := WithCallTimings(ctx)
	if len(other.Snapshot()) != 0 {
		t.Fatal("requests shared timings")
	}
}
