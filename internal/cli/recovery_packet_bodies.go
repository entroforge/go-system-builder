package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The existing approved plan retains the original packet bodies. Allocate
// actual paths before pending commits, without claiming an occupied location.
func restoreRecoveryCandidatePacketBodies(ctx context.Context, root string, document recoveryPlanDocument, stateBytes []byte) ([]byte, error) {
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, err
	}
	bodies := make(map[string][]byte, len(document.CandidatePacketBodies))
	for original, saved := range document.CandidatePacketBodies {
		data, err := workpacket.ReadFile(root, saved, packetbody.Limit)
		if err != nil {
			return nil, fmt.Errorf("read original recovery body: %w", err)
		}
		bodies[original] = data
	}
	if err := runtime.RestoreCheckpointPacketBodies(ctx, root, runtime.CheckpointPair{State: state, PacketBodies: bodies}); err != nil {
		return nil, err
	}
	return json.Marshal(state)
}
