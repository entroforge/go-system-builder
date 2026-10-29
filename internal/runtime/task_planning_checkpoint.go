package runtime

import "fmt"

// The S5 validator receives a temporary projection without S4's typed rows.
// Their original consumers already recorded their facts; reading this view does
// not replay issuance, evidence or journal qualification. Persisted state stays
// untouched and the original storage/Writer validation remains responsible for
// the full pair.
func validateTaskPlanningCheckpointIndices(_ string, state map[string]any, _ []byte) (map[string]any, error) {
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	keepPackets := make([]any, 0, len(packets))
	s4 := map[string]bool{}
	for _, raw := range packets {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("checkpoint packet row is unreadable")
		}
		if row["template_id"] == "s4-task-planning-v1" {
			id := stringValue(row["packet_id"])
			if id == "" {
				return nil, fmt.Errorf("S4 checkpoint packet has no readable id")
			}
			s4[id] = true
			continue
		}
		keepPackets = append(keepPackets, raw)
	}
	if len(s4) == 0 {
		return state, nil
	}
	rows, _ := entities["submissions"].([]any)
	keepSubmissions := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("checkpoint submission row is unreadable")
		}
		if !s4[stringValue(row["packet_id"])] {
			keepSubmissions = append(keepSubmissions, raw)
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
