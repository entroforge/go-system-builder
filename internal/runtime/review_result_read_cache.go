package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type reviewResultReadCacheKey struct{}
type reviewResultReadCache struct {
	gate     chan struct{}
	key      [32]byte
	packets  []workpacket.ReviewResultPacket
	valid    bool
	selected map[[32]byte]workpacket.ReviewResultPacket
}

// WithReviewResultReadCache reuses derived reads within one request. Registry
// reuse is keyed by its complete contents and packet-contract marker, never a
// mutable map address or a revision alone. Current assignments remain live.
// Journal files and pinned evidence are still read and verified on every call.
func WithReviewResultReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = controlproof.WithJournalReadCache(ctx)
	if _, ok := ctx.Value(reviewResultReadCacheKey{}).(*reviewResultReadCache); ok {
		return ctx
	}
	return context.WithValue(ctx, reviewResultReadCacheKey{}, &reviewResultReadCache{gate: make(chan struct{}, 1)})
}

func reviewResultPacketsContext(ctx context.Context, state map[string]any) ([]workpacket.ReviewResultPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache, _ := ctx.Value(reviewResultReadCacheKey{}).(*reviewResultReadCache)
	if cache == nil {
		return decodeReviewResultPackets(ctx, state)
	}
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return decodeReviewResultPackets(ctx, state)
	}
	// The decoder requires actual registry arrays and object rows, and reads
	// subtype/ID fields before JSON decoding. Do not let JSON-equivalent Go
	// values bypass those original structural checks after a cache hit.
	if raw := entities["work_packets"]; raw != nil {
		rows, ok := raw.([]any)
		if !ok {
			return decodeReviewResultPackets(ctx, state)
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				return decodeReviewResultPackets(ctx, state)
			}
			_, external, err := packetbody.ReferenceFromIndex(row)
			if err != nil {
				return nil, err
			}
			if err := validatePacketBodyIndexRow(row); err != nil {
				return nil, err
			}
			if external && row["template_id"] == reviewResultPacketTemplateID {
				if _, err := packetbody.Read(ctx, row); err != nil {
					return nil, err
				}
			}
			if _, ok := row["packet_id"].(string); !ok {
				return decodeReviewResultPackets(ctx, state)
			}
			if template, exists := row["template_id"]; exists && template != nil {
				if _, ok := template.(string); !ok {
					return decodeReviewResultPackets(ctx, state)
				}
			}
		}
	}
	// Keep only the latest exact snapshot. Unsupported values follow the
	// original decoder, retaining its original source-specific errors.
	data, err := json.Marshal(struct {
		Enabled bool
		Rows    any
	}{PacketEnabled(state), entities["work_packets"]})
	if err != nil {
		return decodeReviewResultPackets(ctx, state)
	}
	key := sha256.Sum256(data)
	select {
	case cache.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-cache.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cache.valid && cache.key == key {
		defer metrics.Span(ctx, "runtime/review_packet_index_hit")()
		return cache.packets, nil
	}
	defer metrics.Span(ctx, "runtime/review_packet_index_build")()
	packets, err := decodeReviewResultPackets(ctx, state)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache.key, cache.packets, cache.valid = key, packets, true
	return packets, nil
}

// Only private readers see cached structs. Public results own all their nested
// slices, pointers and maps so a caller cannot corrupt another lookup.
func cloneReviewResultPacket(p workpacket.ReviewResultPacket) workpacket.ReviewResultPacket {
	p.Binding.ClaimIDs = slices.Clone(p.Binding.ClaimIDs)
	if p.Binding.VerificationArtifactDigest != nil {
		v := *p.Binding.VerificationArtifactDigest
		p.Binding.VerificationArtifactDigest = &v
	}
	p.Inputs.Claims = slices.Clone(p.Inputs.Claims)
	p.Inputs.FrozenSubjects = slices.Clone(p.Inputs.FrozenSubjects)
	p.Inputs.Methods = slices.Clone(p.Inputs.Methods)
	if p.Inputs.MethodSource != nil {
		v := *p.Inputs.MethodSource
		p.Inputs.MethodSource = &v
	}
	p.Draft.Read = slices.Clone(p.Draft.Read)
	p.Draft.Answer = slices.Clone(p.Draft.Answer)
	if p.Draft.Questions != nil {
		questions := make(map[string]workpacket.Question, len(p.Draft.Questions))
		for key, q := range p.Draft.Questions {
			q.Choices = slices.Clone(q.Choices)
			if q.Example != nil {
				v := *q.Example
				q.Example = &v
			}
			questions[key] = q
		}
		p.Draft.Questions = questions
	}
	return p
}

// canonicalReviewResultRowContext caches the immutable rebuild of a selected
// row by its exact JSON bytes. It does not cache current source verification.
func canonicalReviewResultRowContext(ctx context.Context, row map[string]any) (workpacket.ReviewResultPacket, error) {
	if err := ctx.Err(); err != nil {
		return workpacket.ReviewResultPacket{}, err
	}
	row, err := packetbody.Read(ctx, row)
	if err != nil {
		return workpacket.ReviewResultPacket{}, err
	}
	data, err := json.Marshal(row)
	if err != nil {
		return workpacket.ReviewResultPacket{}, fmt.Errorf("decode S7 ReviewResult packet %s: %w", stringValue(row["packet_id"]), err)
	}
	key := sha256.Sum256(data)
	cache, _ := ctx.Value(reviewResultReadCacheKey{}).(*reviewResultReadCache)
	if cache != nil {
		select {
		case cache.gate <- struct{}{}:
		case <-ctx.Done():
			return workpacket.ReviewResultPacket{}, ctx.Err()
		}
		defer func() { <-cache.gate }()
		if err := ctx.Err(); err != nil {
			return workpacket.ReviewResultPacket{}, err
		}
		if packet, ok := cache.selected[key]; ok {
			defer metrics.Span(ctx, "runtime/review_packet_selected_hit")()
			return packet, nil
		}
	}
	defer metrics.Span(ctx, "runtime/review_packet_selected_build")()
	id := stringValue(row["packet_id"])
	packet, err := decodeReviewResultPacket(row)
	if err != nil {
		return workpacket.ReviewResultPacket{}, fmt.Errorf("decode S7 ReviewResult packet %s: %w", id, err)
	}
	if packet.ID != id {
		return workpacket.ReviewResultPacket{}, fmt.Errorf("S7 ReviewResult packet key differs from its row ID")
	}
	rebuilt, err := workpacket.RebuildReviewResult(packet)
	if err != nil || !reflect.DeepEqual(rebuilt, packet) {
		return workpacket.ReviewResultPacket{}, fmt.Errorf("S7 ReviewResult packet %s differs from its canonical rebuild", id)
	}
	if err := ctx.Err(); err != nil {
		return workpacket.ReviewResultPacket{}, err
	}
	if cache != nil {
		if cache.selected == nil {
			cache.selected = make(map[[32]byte]workpacket.ReviewResultPacket)
		}
		cache.selected[key] = packet
	}
	return packet, nil
}
