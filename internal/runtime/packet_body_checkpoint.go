package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

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
	return dir + "/packet-bodies/" + ref.SHA256 + ".json"
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
		if err := packetbody.Verify(ref, data); err != nil {
			return err
		}
		copy, err := workpacket.PublishBounded(s.root, checkpointBodyPath(dir, ref), data, packetbody.Limit)
		if err != nil {
			return err
		}
		if !bytes.Equal(copy, data) {
			return errors.New("immutable checkpoint packet body conflict")
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
		if err := packetbody.Verify(ref, data); err != nil {
			return nil, err
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
		return data, packetbody.Verify(ref, data)
	})
}

// RestoreCheckpointPacketBodies republishes independently verified exact
// checkpoint bodies before the original recovery Writer installs references.
// It does not change Runtime, journal, issuance or professional reception.
func RestoreCheckpointPacketBodies(ctx context.Context, root string, pair CheckpointPair) error {
	refs, err := packetBodyReferences(pair.State)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return err
		}
		data := pair.PacketBodies[ref.Path]
		if err := packetbody.Verify(ref, data); err != nil {
			return err
		}
		existing, err := workpacket.PublishBounded(root, ref.Path, data, packetbody.Limit)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("original packet body restore conflict: %s", ref.Path)
		}
	}
	return nil
}
