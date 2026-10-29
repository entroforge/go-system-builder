package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// S2 records are read as recorded facts. Other checkpoint subtypes receive
// a projection without S2 rows; this does not reconstruct issuance or evidence.
func validateDesignCheckpointIndices(root string, state map[string]any, journalBytes []byte) (map[string]any, error) {
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	design := map[string]workpacket.DesignPacket{}
	keepPackets := []any{}
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s2-design-v1" {
			keepPackets = append(keepPackets, raw)
			continue
		}
		data, err := json.Marshal(row)
		var p workpacket.DesignPacket
		if err != nil || json.Unmarshal(data, &p) != nil || p.ID == "" {
			return nil, fmt.Errorf("design checkpoint packet unreadable")
		}
		if _, exists := design[p.ID]; exists {
			return nil, fmt.Errorf("duplicate design packet identity")
		}
		design[p.ID] = p
	}
	if len(design) == 0 {
		return state, nil
	}
	rows, _ := entities["submissions"].([]any)
	keepSubmissions := []any{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		p, ours := design[stringValue(row["packet_id"])]
		if !ours {
			keepSubmissions = append(keepSubmissions, raw)
			continue
		}
		var r SubmissionRecord
		data, err := json.Marshal(row)
		if err != nil || json.Unmarshal(data, &r) != nil || r.OriginEntry != p.Binding.StageEntry {
			return nil, fmt.Errorf("design checkpoint reception responsibility differs")
		}
	}
	filtered := map[string]any{}
	for k, v := range state {
		filtered[k] = v
	}
	copyEntities := map[string]any{}
	for k, v := range entities {
		copyEntities[k] = v
	}
	copyEntities["work_packets"], copyEntities["submissions"] = keepPackets, keepSubmissions
	filtered["entities"] = copyEntities
	return filtered, nil
}
