package repair

import (
	"context"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// ReadRepairResultInput reads the original answer in its assigned workspace.
func ReadRepairResultInput(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket, e workspace.Execution) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return workpacket.ReadFile(e.Path, p.AnswerPath, repairResultAnswerLimit)
}
