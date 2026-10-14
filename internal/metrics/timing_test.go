package metrics

import (
	"context"
	"sync"
	"testing"
)

func TestTimingIsConcurrentScopedAndDoesNotInventMissingSamples(t *testing.T) {
	ctx := WithTiming(context.Background())
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { finish := StartPhase(WithTiming(ctx), "work"); finish(); finish() })
	}
	wg.Wait()
	got := ReadTiming(ctx)
	if got["work"].Calls != 10 || len(got) != 1 {
		t.Fatalf("bad observations: %#v", got)
	}
	got["work"] = PhaseTiming{}
	if ReadTiming(ctx)["work"].Calls != 10 {
		t.Fatal("caller mutated collector")
	}
	if len(ReadTiming(WithTiming(context.Background()))) != 0 {
		t.Fatal("another operation inherited timings")
	}
}
