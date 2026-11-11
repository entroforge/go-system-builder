package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

var errCheckpointPublicationOccupied = errors.New("checkpoint publication path occupied")

func packetBodyReferences(state map[string]any) ([]packetbody.Reference, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	seen := map[string]bool{}
	var refs []packetbody.Reference
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return nil, err
		}
		if external && !seen[ref.Path] {
			refs = append(refs, ref)
			seen[ref.Path] = true
		}
	}
	return refs, nil
}

func checkpointBodyPath(dir string, ref packetbody.Reference) string {
	if strings.HasPrefix(ref.Path, packetbody.Directory) {
		return dir + "/packet-bodies/" + strings.TrimPrefix(ref.Path, packetbody.Directory)
	}
	return dir + "/packet-bodies/alternate-root/" + strings.TrimPrefix(ref.Path, ".claude/")
}

// CheckpointPacketBodyPath is the saved location of an already-issued body.
func CheckpointPacketBodyPath(dir string, ref packetbody.Reference) string {
	return checkpointBodyPath(dir, ref)
}

// Each retained checkpoint owns complete immutable copies of its referenced
// bodies. Global bodies are never pruned, including pending and archive refs.
func (s *Store) publishCheckpointPacketBodies(state map[string]any, dir string) error {
	refs, err := packetBodyReferences(state)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := s.Context().Err(); err != nil {
			return err
		}
		data, err := workpacket.ReadFile(s.root, ref.Path, packetbody.Limit)
		if err != nil {
			return fmt.Errorf("checkpoint original packet body %s: %w", ref.Path, err)
		}
		created, err := workpacket.PublishNewBounded(s.root, checkpointBodyPath(dir, ref), data, packetbody.Limit)
		if err != nil {
			return err
		}
		if !created {
			return errCheckpointPublicationOccupied
		}
	}
	return nil
}

func readCheckpointPacketBodies(root, dir string, state map[string]any) (map[string][]byte, error) {
	refs, err := packetBodyReferences(state)
	if err != nil {
		return nil, err
	}
	bodies := make(map[string][]byte, len(refs))
	for _, ref := range refs {
		data, err := workpacket.ReadFile(root, checkpointBodyPath(dir, ref), packetbody.Limit)
		if err != nil {
			return nil, fmt.Errorf("checkpoint packet body %s unavailable: %w", ref.Path, err)
		}
		bodies[ref.Path] = data
	}
	return bodies, nil
}

func withCheckpointPacketBodies(ctx context.Context, bodies map[string][]byte) context.Context {
	return packetbody.WithResolver(ctx, func(ctx context.Context, ref packetbody.Reference) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, ok := bodies[ref.Path]
		if !ok {
			return nil, fmt.Errorf("checkpoint has no original packet body %s", ref.Path)
		}
		return data, nil
	})
}

// RestoreCheckpointPacketBodies publishes the checkpoint's original bodies
// at newly owned paths and updates the candidate's references before review.
func RestoreCheckpointPacketBodies(ctx context.Context, root string, pair CheckpointPair) error {
	entities, _ := pair.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	published := map[string]packetbody.Reference{}
	for _, raw := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		row, _ := raw.(map[string]any)
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return err
		}
		if !external {
			continue
		}
		actual, ok := published[ref.Path]
		if !ok {
			data, exists := pair.PacketBodies[ref.Path]
			if !exists {
				return fmt.Errorf("checkpoint has no original packet body %s", ref.Path)
			}
			packetID, _ := row["packet_id"].(string)
			actual, err = packetbody.PublishOriginal(ctx, root, packetID, data)
			if err != nil {
				return err
			}
			published[ref.Path] = actual
		}
		row["body_ref"] = map[string]any{"format": actual.Format, "path": actual.Path}
	}
	return nil
}
