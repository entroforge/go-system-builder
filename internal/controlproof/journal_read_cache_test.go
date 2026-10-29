package controlproof

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestJournalReadCacheExactBytesCursorAndOwnedResults(t *testing.T) {
	data := controlProofJournal(t, controlProofEvent("loop-proof", "evt-first", 1, "TEST", 0, 1))
	ctx := WithJournalReadCache(context.Background())
	first, err := InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		t.Fatal(err)
	}
	first.Events[0]["actor"].(map[string]any)["id"] = "changed"
	delete(first.EventIndex, "evt-first")
	again, err := InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil || len(again.EventIndex) != 1 || again.Events[0]["actor"].(map[string]any)["id"] != "orchestrator" {
		t.Fatalf("cache result was mutable: %v %v", again, err)
	}
	if _, err := InspectJournalSegmentDataFromContext(ctx, data, 2); err == nil {
		t.Fatal("starting cursor not included in identity")
	}
	if _, err := InspectJournalSegmentDataFromContext(ctx, append(append([]byte(nil), data...), 'x'), 1); err == nil {
		t.Fatal("modified bytes reused old proof")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := InspectJournalSegmentDataFromContext(canceled, data, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cache hid cancellation: %v", err)
	}
}

func BenchmarkJournalRequestDecoding(b *testing.B) {
	for _, size := range []int{64, 1024} {
		events := make([]map[string]any, size)
		for i := range events {
			events[i] = controlProofEvent("loop-proof", fmt.Sprintf("evt-%d", i), i+1, "TEST", i, i+1)
		}
		data := controlProofJournal(b, events...)
		for _, cached := range []bool{false, true} {
			b.Run(fmt.Sprintf("events=%d/cache=%t/request4", size, cached), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					ctx := context.Background()
					if cached {
						ctx = WithJournalReadCache(ctx)
					}
					for range 4 {
						if _, err := InspectJournalSegmentDataFromContext(ctx, data, 1); err != nil {
							b.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func TestJournalReadCacheConcurrent(t *testing.T) {
	data := controlProofJournal(t, controlProofEvent("loop-proof", "evt-first", 1, "TEST", 0, 1))
	ctx := WithJournalReadCache(context.Background())
	var readers sync.WaitGroup
	for range 16 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			inspection, err := InspectJournalSegmentDataFromContext(ctx, data, 1)
			if err != nil {
				t.Error(err)
				return
			}
			if inspection.Events[0]["actor"].(map[string]any)["id"] != "orchestrator" {
				t.Error("another reader mutated shared results")
			}
			inspection.Events[0]["actor"].(map[string]any)["id"] = "caller-owned"
		}()
	}
	readers.Wait()
}

func TestJournalReadCacheCancelsWhileAnotherReaderBuilds(t *testing.T) {
	ctx := WithJournalReadCache(context.Background())
	cache := ctx.Value(journalReadCacheKey{}).(*journalReadCache)
	cache.gate <- struct{}{}
	defer func() { <-cache.gate }()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := InspectJournalSegmentDataFromContext(ctx, nil, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting inspection: %v", err)
	}
}
