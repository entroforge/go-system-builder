package runtime

import (
	"context"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// ReviewResultReception is mutable Runtime reception metadata beside the
// immutable original issuance. It contains no professional answer text.
func ReviewResultReception(state map[string]any, packetID string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] == reviewResultPacketTemplateID && row["packet_id"] == packetID {
			reception, _ := row["review_result_reception"].(map[string]any)
			return reception
		}
	}
	return nil
}

func SetReviewResultReception(state map[string]any, packetID string, reception map[string]any) error {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] == reviewResultPacketTemplateID && row["packet_id"] == packetID {
			row["review_result_reception"] = reception
			return nil
		}
	}
	return fmt.Errorf("S7 original issuance %s is missing while saving reception", packetID)
}

// ReviewResultPacketCurrent compares the original issuance, never a bare reused
// AssignmentID. Current projection consumers retain their original meaning.
func ReviewResultPacketCurrent(state map[string]any, packet workpacket.ReviewResultPacket) bool {
	review, _ := state["review"].(map[string]any)
	ptr, _ := review["plan"].(map[string]any)
	binding := packet.Binding
	return ptr["plan_id"] == binding.ReviewPlanID && ptr["path"] == binding.ReviewPlanRef &&
		intValue(ptr["revision"]) == binding.ReviewPlanRevision
}

// ProjectIssuedReviewResultPacketIndex selects exact old or current issuance
// coordinates without opening historical bodies or professional Results.
func ProjectIssuedReviewResultPacketIndex(state map[string]any, owner string) ([]ReviewResultPacketIndex, error) {
	rows, err := reviewResultRowsContext(context.Background(), state)
	if err != nil {
		return nil, err
	}
	result := make([]ReviewResultPacketIndex, 0, len(rows))
	for _, row := range rows {
		p := row.header
		if owner != "" && p.Binding.OwnerAgentID != owner {
			continue
		}
		reception, _ := row.row["review_result_reception"].(map[string]any)
		result = append(result, ReviewResultPacketIndex{ID: p.ID, Binding: p.Binding, ViewPath: p.ViewPath, AnswerPath: p.AnswerPath, Reception: reception})
	}
	return result, nil
}

// Preserve pre-upgrade receipts before a plan replacement can discard its
// compatibility projection. Only an explicit packet_id relation is migrated;
// neither filenames nor the current owner establish historical acceptance.
func preserveReviewResultReceptions(previous, candidate map[string]any) bool {
	review, _ := previous["review"].(map[string]any)
	assignments, _ := review["assignments"].(map[string]any)
	changed := false
	for _, raw := range assignments {
		assignment, _ := raw.(map[string]any)
		receipt, _ := assignment["result_receipt"].(map[string]any)
		packetID, _ := receipt["packet_id"].(string)
		if packetID == "" || ReviewResultReception(candidate, packetID)["receipt"] != nil {
			continue
		}
		activity := workstate.ReviewAssignmentActivity(assignment)
		reception := map[string]any{"receipt": receipt, "work_complete": activity == "consumed" || activity == "result_submitted", "execution_root": receipt["execution_root"]}
		if err := SetReviewResultReception(candidate, packetID, reception); err == nil {
			changed = true
		}
	}
	return changed
}
