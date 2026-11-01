package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const hypothesisResultPacketTemplateID = "s8-hypothesis-result-v1"

// HypothesisResultPackets reads the existing packet registry. It does not
// grant permission to answer or derive an investigation from packet inputs.
func HypothesisResultPackets(state map[string]any) ([]workpacket.HypothesisResultPacket, error) {
	entities, _ := state["entities"].(map[string]any)
	rawRows, exists := entities["work_packets"]
	if !exists || rawRows == nil {
		return []workpacket.HypothesisResultPacket{}, nil
	}
	rows, ok := rawRows.([]any)
	if !ok {
		return nil, fmt.Errorf("Runtime work_packets is not an array")
	}
	seen := map[string]bool{}
	packets := make([]workpacket.HypothesisResultPacket, 0)
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		id := stringValue(row["packet_id"])
		if !ok || id == "" || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate Runtime work_packets[%d] identity", i)
		}
		seen[id] = true
		if row["template_id"] != hypothesisResultPacketTemplateID {
			continue
		}
		data, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("hypothesis-result-work-packet.schema.json", data); err != nil {
			return nil, fmt.Errorf("S8 packet %s schema: %w", id, err)
		}
		var p workpacket.HypothesisResultPacket
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		if p.ID != id || p.Binding.OwnerAgentID == "" || p.Binding.AssignmentID == "" {
			return nil, fmt.Errorf("S8 packet identity is incomplete")
		}
		packets = append(packets, p)
	}
	if len(packets) > 0 && !PacketEnabled(state) {
		return nil, fmt.Errorf("S8 packets require the explicit v1.2 packet contract")
	}
	sort.Slice(packets, func(i, j int) bool { return packets[i].ID < packets[j].ID })
	return packets, nil
}

func FindHypothesisResultPacket(state map[string]any, id string) (workpacket.HypothesisResultPacket, bool, error) {
	if id == "" {
		return workpacket.HypothesisResultPacket{}, false, fmt.Errorf("S8 packet ID is required")
	}
	packets, err := HypothesisResultPackets(state)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, false, err
	}
	for _, p := range packets {
		if p.ID == id {
			return p, true, nil
		}
	}
	return workpacket.HypothesisResultPacket{}, false, nil
}

// CurrentHypothesisResultPackets selects current issued responsibilities.
// Accepted rows remain selectable for exact save replay; a new write grant
// still requires an open hypothesis, unchanged route and original Case
// Assignment continuity. Generic PLAN_REPORT is not a Case answer prerequisite.
func CurrentHypothesisResultPackets(root string, state map[string]any, owner string) ([]workpacket.HypothesisResultPacket, error) {
	all, err := HypothesisResultPackets(state)
	if err != nil || len(all) == 0 {
		return all, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	life, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation, _ := integerField(baseline, "generation")
	entities, _ := state["entities"].(map[string]any)
	selected := make([]workpacket.HypothesisResultPacket, 0)
	seenSlots := map[string]bool{}
	for _, p := range all {
		b := p.Binding
		if owner != "" && b.OwnerAgentID != owner || b.RuntimeID != state["runtime_id"] {
			continue
		}
		// An issued responsibility remains receivable when a later Case is current.
		// The packet itself is the original assignment and exact author path.
		if pointer == nil || b.CaseID != pointer["case_id"] || life["state"] != "bug_resolution" || life["phase"] != "investigation" {
			slot := b.CaseID + "/" + b.AssignmentID
			if seenSlots[slot] {
				return nil, fmt.Errorf("S8 original responsibility has multiple issuances")
			}
			seenSlots[slot] = true
			selected = append(selected, p)
			continue
		}
		if b.BaselineGeneration != generation {
			continue
		}
		registered := false
		for _, agent := range objectRows(entities["agents"]) {
			if agent["id"] != b.OwnerAgentID {
				continue
			}
			if agent["role"] != "investigator" || agent["team_id"] != b.TeamID || agent["prompt_ref"] != b.ManifestRef+"#"+b.AssignmentID {
				continue
			}
			tasks, _ := checkpointStringSlice(agent["task_ids"])
			if !hypothesisStringContains(tasks, b.TaskID) {
				continue
			}
			for _, team := range objectRows(entities["teams"]) {
				if team["id"] == b.TeamID && team["kind"] == "investigator" && team["manifest_ref"] == b.ManifestRef {
					registered = true
				}
			}
		}
		if !registered {
			continue
		}
		if seenSlots[b.CaseID+"/"+b.AssignmentID] {
			return nil, fmt.Errorf("current S8 responsibility has multiple compatible issuances")
		}
		seenSlots[b.CaseID+"/"+b.AssignmentID] = true
		selected = append(selected, p)
	}
	return selected, nil
}

// VerifyHypothesisResultPacketIssue checks the original committed issue and
// frozen sources. Current owner/PLAN, route compatibility and professional
// evidence remain checks of the original investigation consumer.
func VerifyHypothesisResultPacketIssue(root string, state map[string]any, p workpacket.HypothesisResultPacket) error {
	stored, found, err := FindHypothesisResultPacket(state, p.ID)
	if err != nil {
		return err
	}
	if !found || stored.Binding.OwnerAgentID != p.Binding.OwnerAgentID || stored.Binding.AssignmentID != p.Binding.AssignmentID {
		return fmt.Errorf("S8 packet has no original registered responsibility")
	}
	return nil
}

func hypothesisJSONEqual(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	var nx, ny any
	if json.Unmarshal(x, &nx) != nil || json.Unmarshal(y, &ny) != nil {
		return false
	}
	return reflect.DeepEqual(nx, ny)
}

func objectRows(value any) []map[string]any {
	rows, _ := value.([]any)
	objects := make([]map[string]any, 0, len(rows))
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok {
			objects = append(objects, row)
		}
	}
	return objects
}

func hypothesisStringContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func hypothesisExactStringSet(value any, expected []string) bool {
	actual, ok := checkpointStringSlice(value)
	if !ok || len(actual) != len(expected) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range actual {
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	for _, id := range expected {
		if !seen[id] {
			return false
		}
	}
	return true
}

func hypothesisCaseRouteEqual(document map[string]any, expected workpacket.HypothesisResultCaseRoute) bool {
	route := map[string]any{}
	for _, key := range []string{"status", "route", "route_reason", "route_consumed_at", "route_consumer"} {
		route[key] = document[key]
	}
	return hypothesisJSONEqual(route, expected)
}
