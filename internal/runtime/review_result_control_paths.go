package runtime

import (
	"context"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// VerifyReviewResultPacketControlPathsContext proves only the exact S7 control
// paths needed by a worktree drift scan. Global registry shape and identity
// uniqueness remain mandatory; unrelated packet bodies and historical sources
// belong to the independent full-history validator, not this classification.
func VerifyReviewResultPacketControlPathsContext(ctx context.Context, root string, state map[string]any, paths []string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		wanted[path] = true
	}
	ctx = WithPacketBodyRoot(ctx, root)
	indexed, err := reviewResultRowsContext(ctx, state)
	if err != nil {
		return nil, err
	}
	var selected []map[string]any
	bodyPaths := map[string]string{}
	for _, row := range indexed {
		ref, external, err := packetbody.ReferenceFromIndex(row.row)
		if err != nil {
			return nil, err
		}
		if external {
			bodyPaths[row.header.ID] = ref.Path
		}
		if wanted[row.header.ViewPath] || wanted[row.header.AnswerPath] || (external && wanted[ref.Path]) {
			selected = append(selected, row.row)
		}
	}
	packets := make([]workpacket.ReviewResultPacket, 0, len(selected))
	for _, row := range selected {
		p, err := canonicalReviewResultRowContext(ctx, row)
		if err != nil {
			return nil, err
		}
		packets = append(packets, p)
	}
	if len(packets) == 0 {
		return nil, ctx.Err()
	}
	journal, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return nil, fmt.Errorf("read original Runtime journal for S7 packet: %w", err)
	}
	inspection, err := controlproof.InspectJournalSegmentDataFromContext(ctx, journal, 1)
	if err != nil {
		return nil, fmt.Errorf("inspect original Runtime journal for S7 packet: %w", err)
	}
	var verified []string
	for _, p := range packets {
		if err := verifyCanonicalReviewResultPacketSources(ctx, root, state, p); err != nil {
			return nil, err
		}
		if err := verifyReviewResultPacketIssueEvent(state, p, inspection); err != nil {
			return nil, err
		}
		for _, path := range []string{p.ViewPath, p.AnswerPath, bodyPaths[p.ID]} {
			if wanted[path] {
				verified = append(verified, path)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return verified, nil
}
