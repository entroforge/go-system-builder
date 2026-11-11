package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The original reader needs no digest-derived cache identity.
func WithReviewResultReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func reviewResultPacketsContext(ctx context.Context, state map[string]any) ([]workpacket.ReviewResultPacket, error) {
	return decodeReviewResultPackets(ctx, state)
}

// Only private readers see cached structs. Public results own all their nested
// slices, pointers and maps so a caller cannot corrupt another lookup.
func cloneReviewResultPacket(p workpacket.ReviewResultPacket) workpacket.ReviewResultPacket {
	p.Binding.ClaimIDs = slices.Clone(p.Binding.ClaimIDs)
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
	id := stringValue(row["packet_id"])
	packet, err := decodeReviewResultPacket(row)
	if err != nil {
		return workpacket.ReviewResultPacket{}, fmt.Errorf("decode S7 ReviewResult packet %s: %w", id, err)
	}
	if packet.ID != id {
		return workpacket.ReviewResultPacket{}, fmt.Errorf("S7 ReviewResult packet key differs from its row ID")
	}
	return packet, ctx.Err()
}
