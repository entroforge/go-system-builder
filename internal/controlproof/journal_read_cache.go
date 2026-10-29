package controlproof

import (
	"context"
	"crypto/sha256"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

type journalReadCacheKey struct{}
type journalReadCache struct {
	gate       chan struct{}
	digest     [32]byte
	first      int
	valid      bool
	inspection JournalInspection
}

// WithJournalReadCache scopes validated journal decoding to one request. It
// caches only the latest exact byte sequence and starting cursor, never files,
// state/journal pairing, source verification or authority decisions.
func WithJournalReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(journalReadCacheKey{}).(*journalReadCache); ok {
		return ctx
	}
	return context.WithValue(ctx, journalReadCacheKey{}, &journalReadCache{gate: make(chan struct{}, 1)})
}

func InspectJournalSegmentDataFromContext(ctx context.Context, data []byte, firstSequence int) (JournalInspection, error) {
	if err := ctx.Err(); err != nil {
		return JournalInspection{}, err
	}
	cache, _ := ctx.Value(journalReadCacheKey{}).(*journalReadCache)
	if cache == nil {
		return inspectJournalSegmentDataFromContext(ctx, data, firstSequence)
	}
	digest := sha256.Sum256(data)
	select {
	case cache.gate <- struct{}{}:
	case <-ctx.Done():
		return JournalInspection{}, ctx.Err()
	}
	defer func() { <-cache.gate }()
	if err := ctx.Err(); err != nil {
		return JournalInspection{}, err
	}
	if cache.valid && cache.digest == digest && cache.first == firstSequence {
		defer metrics.Span(ctx, "runtime/journal_decode_hit")()
		return cloneJournalInspection(ctx, cache.inspection)
	}
	defer metrics.Span(ctx, "runtime/journal_decode_build")()
	inspection, err := inspectJournalSegmentDataFromContext(ctx, data, firstSequence)
	if err != nil {
		return JournalInspection{}, err
	}
	if err := ctx.Err(); err != nil {
		return JournalInspection{}, err
	}
	cache.digest, cache.first, cache.valid, cache.inspection = digest, firstSequence, true, inspection
	return cloneJournalInspection(ctx, inspection)
}

func cloneJournalInspection(ctx context.Context, in JournalInspection) (JournalInspection, error) {
	out := in
	out.EventIndex = make(map[string]int, len(in.EventIndex))
	for k, v := range in.EventIndex {
		out.EventIndex[k] = v
	}
	out.Events = make([]map[string]any, len(in.Events))
	for i, event := range in.Events {
		if err := ctx.Err(); err != nil {
			return JournalInspection{}, err
		}
		out.Events[i] = cloneJournalJSON(event).(map[string]any)
	}
	return out, ctx.Err()
}

func cloneJournalJSON(value any) any {
	switch value := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(value))
		for k, v := range value {
			copy[k] = cloneJournalJSON(v)
		}
		return copy
	case []any:
		copy := make([]any, len(value))
		for i, v := range value {
			copy[i] = cloneJournalJSON(v)
		}
		return copy
	default:
		return value
	}
}
