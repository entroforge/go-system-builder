package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Approved checkpoint plans freeze their own bodies, independently of the
// rolling two-checkpoint retention window. Publish bodies before references
// can enter the original recovery transaction; pending retries then use that
// original pair and the retained immutable bodies.
func restoreRecoveryCandidatePacketBodies(ctx context.Context, root string, document recoveryPlanDocument, stateBytes []byte) error {
	if sha256Text(stateBytes) != document.CandidateStateSHA256 {
		return fmt.Errorf("checkpoint candidate state differs from approved plan")
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return err
	}
	bodies := map[string][]byte{}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return err
		}
		if !external {
			continue
		}
		path := ".claude/recovery/" + document.PlanID + "/packet-bodies/" + ref.SHA256 + ".json"
		if document.CandidatePacketBodies[ref.Path] != path || filepath.Base(document.PlanID) != document.PlanID {
			return fmt.Errorf("checkpoint plan lacks the exact sealed packet body %s", ref.Path)
		}
		data, err := workpacket.ReadFile(root, path, packetbody.Limit)
		if err != nil {
			return err
		}
		if err := packetbody.Verify(ref, data); err != nil {
			return err
		}
		bodies[ref.Path] = data
	}
	if len(bodies) != len(document.CandidatePacketBodies) {
		return fmt.Errorf("checkpoint plan has unrelated packet bodies")
	}
	return runtime.RestoreCheckpointPacketBodies(ctx, root, runtime.CheckpointPair{State: state, PacketBodies: bodies})
}
