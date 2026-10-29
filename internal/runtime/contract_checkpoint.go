package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Each subtype proves its original records. The S5 validator receives a
// temporary projection only; the persisted full pair and journal are untouched.
func validateContractCheckpointIndices(root string, state map[string]any, journalBytes []byte) (map[string]any, error) {
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	design := map[string]workpacket.ContractPacket{}
	keepPackets := []any{}
	allIDs := map[string]bool{}
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		id := stringValue(row["packet_id"])
		if allIDs[id] {
			return nil, fmt.Errorf("duplicate checkpoint packet identity")
		}
		allIDs[id] = true
		if row["template_id"] != "s3-contract-v1" {
			keepPackets = append(keepPackets, raw)
			continue
		}
		p, found, err := FindContractPacket(state, id)
		if err != nil || !found {
			return nil, fmt.Errorf("S3 checkpoint decode failed")
		}
		expected, err := workpacket.BuildContracts(p.Binding, p.Inputs)
		if err != nil || !reflect.DeepEqual(expected, p) || p.Binding.RuntimeID != state["runtime_id"] || p.Binding.Generation > baselineGeneration(state) {
			return nil, fmt.Errorf("S3 checkpoint frozen identity differs")
		}
		for _, old := range design {
			if old.Binding.RuntimeID == p.Binding.RuntimeID && old.Binding.Generation == p.Binding.Generation && old.Binding.StageEntry == p.Binding.StageEntry {
				return nil, fmt.Errorf("duplicate S3 stage responsibility")
			}
		}
		design[id] = p
	}
	if len(design) == 0 {
		return state, nil
	}
	journal, err := inspectJournalData(journalBytes)
	if err != nil {
		return nil, err
	}
	if len(journal.Events) > 0 {
		after, err := integerField(journal.Events[len(journal.Events)-1], "after_revision")
		revision, revErr := integerField(state, "revision")
		if err != nil || revErr != nil || after != revision {
			return nil, fmt.Errorf("S3 checkpoint is not linked to journal tail")
		}
	}
	for _, p := range design {
		found := false
		for _, event := range journal.Events {
			from, _ := event["from"].(map[string]any)
			to, _ := event["to"].(map[string]any)
			if event["idempotency_key"] == "packet:"+p.ID && event["transition_id"] == "PACKET-ISSUE" && event["event_id"] == "evt-contract-packet-"+p.ID && event["runtime_id"] == p.Binding.RuntimeID && contractJournalActor(event) == "orchestrator" && from["state"] == "planning" && from["phase"] == "contracts" && to["state"] == "planning" && to["phase"] == "contracts" {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("S3 checkpoint has no original issuance")
		}
	}
	evidence := map[string]map[string]any{}
	evs, _ := state["evidence"].([]any)
	for _, raw := range evs {
		row, _ := raw.(map[string]any)
		id := stringValue(row["id"])
		if evidence[id] != nil {
			return nil, fmt.Errorf("duplicate checkpoint evidence")
		}
		evidence[id] = row
	}
	rows, _ := entities["submissions"].([]any)
	keepSubmissions := []any{}
	keys := map[string]bool{}
	received := map[string]bool{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		key := stringValue(row["submission_key"])
		if keys[key] {
			return nil, fmt.Errorf("duplicate checkpoint submission key")
		}
		keys[key] = true
		p, ours := design[stringValue(row["packet_id"])]
		if !ours {
			keepSubmissions = append(keepSubmissions, raw)
			continue
		}
		r, found, err := FindSubmission(state, p.ID)
		if err != nil || !found || received[p.ID] {
			return nil, fmt.Errorf("duplicate or unreadable S3 reception")
		}
		received[p.ID] = true
		expectedKey := workpacket.Digest([]byte("submission:" + p.ID))
		if r.Key != expectedKey || r.FollowupID != workpacket.Digest([]byte(expectedKey+":after-domain")) || r.Owner != "Main" || r.OriginState != "planning" || r.OriginPhase != "contracts" || r.OriginEntry != p.Binding.StageEntry || r.RevisionPacketID != "" || r.SupersededBy != "" {
			return nil, fmt.Errorf("S3 checkpoint reception identity differs")
		}
		e := evidence[r.EvidenceID]
		generation, genErr := integerField(e, "baseline_generation")
		producers, _ := e["produced_by"].([]any)
		if e == nil || e["kind"] != "planning_contract" || e["responsibility_id"] != "Orchestrator" || genErr != nil || generation != p.Binding.Generation || len(producers) != 1 || producers[0] != "Main" {
			return nil, fmt.Errorf("S3 checkpoint has no original domain evidence")
		}
		data, err := workpacket.ReadFile(root, stringValue(e["path"]), workpacket.ContractEvidenceReadLimit(p))
		if err != nil || workpacket.Digest(data) != e["sha256"] {
			return nil, fmt.Errorf("S3 checkpoint original evidence unavailable")
		}
		var envelope map[string]json.RawMessage
		json.Unmarshal(data, &envelope)
		var answer workpacket.ContractAnswer
		if err := json.Unmarshal(envelope["contract_answer"], &answer); err != nil {
			return nil, fmt.Errorf("S3 checkpoint professional answer malformed")
		}
		if err := ValidateContractEnvelope(p, r.EvidenceID, answer, data); err != nil {
			return nil, err
		}
		digest, err := workpacket.ContractAnswerDigest(answer)
		if err != nil || digest != r.ContentHash {
			return nil, fmt.Errorf("S3 checkpoint professional digest differs")
		}
		receptionFound := false
		for _, event := range journal.Events {
			if event["transition_id"] != "EVIDENCE-RECORD" {
				continue
			}
			before, err := integerField(event, "before_revision")
			after, afterErr := integerField(event, "after_revision")
			if err != nil || afterErr != nil || after != before+1 || event["event_id"] != fmt.Sprintf("evt-evidence-%s-r%d", r.EvidenceID, after) || event["idempotency_key"] != fmt.Sprintf("runtime:evidence:%s:%d", r.EvidenceID, before) || contractJournalActor(event) != "orchestrator" || event["runtime_id"] != p.Binding.RuntimeID {
				continue
			}
			ids, _ := event["evidence_ids"].([]any)
			for _, id := range ids {
				if id == r.EvidenceID {
					receptionFound = true
				}
			}
		}
		if !receptionFound {
			return nil, fmt.Errorf("S3 reception lacks original journal commit")
		}
		switch r.ControlStatus {
		case "pending":
			if r.TransitionEventID != "" {
				return nil, fmt.Errorf("unadvanced S3 has a transition event")
			}
		case "no_transition", "replaced":
			index, found := journal.EventIndex["evt-submission-control-"+r.FollowupID]
			if r.TransitionEventID != "" || !found || journal.Events[index]["transition_id"] != "SUBMISSION-EVALUATE" || journal.Events[index]["idempotency_key"] != "submission-control:"+r.FollowupID || contractJournalActor(journal.Events[index]) != "hook_controller" || journal.Events[index]["runtime_id"] != p.Binding.RuntimeID {
				return nil, fmt.Errorf("S3 evaluation has no original control event")
			}
		case "advanced":
			index, found := journal.EventIndex[r.TransitionEventID]
			if !found {
				return nil, fmt.Errorf("S3 advancement has no original transition")
			}
			event := journal.Events[index]
			from, _ := event["from"].(map[string]any)
			to, _ := event["to"].(map[string]any)
			if event["runtime_id"] != p.Binding.RuntimeID || contractJournalActor(event) != "hook_controller" || event["transition_id"] != "PTR-PLAN-02" || from["state"] != "planning" || from["phase"] != "contracts" || to["state"] != "planning" || to["phase"] != "tasks" {
				return nil, fmt.Errorf("S3 checkpoint transition differs from original route")
			}
		default:
			return nil, fmt.Errorf("unknown S3 control status")
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

func contractJournalActor(event map[string]any) string {
	actor, _ := event["actor"].(map[string]any)
	return stringValue(actor["id"])
}
