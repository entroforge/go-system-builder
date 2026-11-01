package runtime

import (
	"context"
	"errors"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const reviewResultArtifactLimit int64 = 20 << 20

// VerifyReviewResultPacketIssue reads the original Runtime packet and its
// responsibility. It does not authorize a save or check completion.
func VerifyReviewResultPacketIssue(root string, state map[string]any, p workpacket.ReviewResultPacket) error {
	return VerifyReviewResultPacketIssueContext(context.Background(), root, state, p)
}

func VerifyReviewResultPacketIssueContext(ctx context.Context, root string, state map[string]any, p workpacket.ReviewResultPacket) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = WithPacketBodyRoot(ctx, root)
	stored, found, err := FindReviewResultPacketContext(ctx, state, p.ID)
	if err != nil {
		return err
	}
	if !found || stored.Binding.RuntimeID != p.Binding.RuntimeID || stored.Binding.ReviewPlanID != p.Binding.ReviewPlanID || stored.Binding.ReviewPlanRevision != p.Binding.ReviewPlanRevision || stored.Binding.AssignmentID != p.Binding.AssignmentID || stored.Binding.OwnerAgentID != p.Binding.OwnerAgentID {
		return errors.New("S7 original packet responsibility is unavailable")
	}
	return nil
}

func validateReviewResultCheckpointIndices(root string, state map[string]any, journalBytes []byte) (map[string]any, error) {
	return validateReviewResultCheckpointIndicesContext(context.Background(), root, state, journalBytes)
}

func validateReviewResultCheckpointIndicesContext(ctx context.Context, root string, state map[string]any, journalBytes []byte) (map[string]any, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	// Other checkpoint logic handles the shared transaction. Old S7 material
	// stays at its original path; do not re-prove every historical receipt.
	return withoutReviewResultPackets(state), nil
}

func withoutReviewResultPackets(state map[string]any) map[string]any {
	copyState := make(map[string]any, len(state))
	for key, value := range state {
		copyState[key] = value
	}
	entities, _ := state["entities"].(map[string]any)
	copyEntities := make(map[string]any, len(entities))
	for key, value := range entities {
		copyEntities[key] = value
	}
	rows, _ := entities["work_packets"].([]any)
	kept := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["template_id"] == reviewResultPacketTemplateID {
			continue
		}
		kept = append(kept, raw)
	}
	copyEntities["work_packets"] = kept
	copyState["entities"] = copyEntities
	return copyState
}
