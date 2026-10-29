package runtime

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairResultResumePending reports a recorded resolution awaiting its current
// successor questionnaire through the original verified current selector.
func RepairResultResumePending(root string, state map[string]any, owner, assignment string) (bool, error) {
	if !PacketEnabled(state) || owner == "" || assignment == "" {
		return false, nil
	}
	packets, err := repairResultPacketsMatchingContext(repairPacketContext(root), state, func(b workpacket.RepairResultBinding) bool {
		return b.Sources.OwnerAgentID == owner && b.Sources.RepairAssignmentID == assignment
	})
	if err != nil {
		return false, err
	}
	current, err := currentRepairResultPackets(root, state, owner, packets)
	if err != nil {
		return false, err
	}
	for _, packet := range current {
		if packet.Binding.Sources.RepairAssignmentID == assignment {
			return false, nil
		}
	}
	for _, packet := range packets {
		b := packet.Binding.Sources
		if b.OwnerAgentID != owner || b.RepairAssignmentID != assignment {
			continue
		}
		resume, err := RepairResultResumeBinding(root, state, packet.Binding)
		if err != nil {
			return false, err
		}
		if resume != nil {
			return true, nil
		}
	}
	return false, nil
}

func repairResultResumeJournal(root string, state map[string]any, contexts ...context.Context) (controlproof.JournalInspection, error) {
	ctx := controlproof.WithJournalReadCache(repairPacketContext(root, contexts...))
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return controlproof.JournalInspection{}, err
	}
	journal, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return controlproof.JournalInspection{}, err
	}
	if err := controlproof.ValidateStateJournalPair(state, journal); err != nil {
		return controlproof.JournalInspection{}, err
	}
	return journal, nil
}
