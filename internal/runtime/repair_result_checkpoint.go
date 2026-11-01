package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func repairResultJournal(root string, state map[string]any, p workpacket.RepairResultPacket, contexts ...context.Context) (controlproof.JournalInspection, error) {
	ctx := controlproof.WithJournalReadCache(repairPacketContext(root, contexts...))
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return controlproof.JournalInspection{}, err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return j, err
	}
	return j, verifyRepairResultPacketIssue(root, state, p, j, ctx)
}

// VerifiedRepairResultCandidateReceipt proves the original candidate reception.
// The S9 consumer must separately establish present source/own-check completion.
func VerifiedRepairResultCandidateReceipt(root string, state map[string]any, packetID, owner string) (RepairResultCandidateReceipt, bool, error) {
	p, ok, err := FindRepairResultPacketContext(repairPacketContext(root), state, packetID)
	if err != nil || !ok {
		return RepairResultCandidateReceipt{}, false, err
	}
	if owner == "" || owner != p.Binding.Sources.OwnerAgentID {
		return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 candidate receipt owner differs")
	}
	j, err := repairResultJournal(root, state, p)
	if err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	return findRepairResultCandidateReceipt(root, state, p, j)
}

func findRepairResultCandidateReceipt(root string, state map[string]any, p workpacket.RepairResultPacket, j controlproof.JournalInspection, contexts ...context.Context) (RepairResultCandidateReceipt, bool, error) {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}

	eventID := "evt-s9-issued-repair-candidate-" + p.ID
	index, exists := j.EventIndex[eventID]
	if !exists {
		return RepairResultCandidateReceipt{}, false, nil
	}
	event := j.Events[index]
	refs, refsOK := checkpointStringSlice(event["evidence_ids"])
	if !refsOK {
		return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 candidate event refs are not strings")
	}
	// The original Writer records its actual Candidate path first, followed by
	// the packet and assignment identities. Its independently selected path
	// can use an alternate directory when an unknown path was already occupied.
	if len(refs) == 0 || refs[0] == "" {
		return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 accepted candidate reference missing")
	}
	path := refs[0]
	if err := ctx.Err(); err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	doc, err := readRepairPacketSource(root, path, "repair-workspace-candidate.schema.json")
	if err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	raw, has := doc["provenance"]
	if !has {
		return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 original accepted candidate has no typed provenance")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	var provenance RepairResultCandidateProvenance
	if err := json.Unmarshal(data, &provenance); err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	if err := verifyRepairResultCandidateDocument(root, p, doc, provenance, ctx); err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	before, be := integerField(event, "before_revision")
	after, ae := integerField(event, "after_revision")
	seq, se := integerField(event, "sequence")
	if be != nil || ae != nil || se != nil || after != before+1 {
		return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 original candidate event revision/sequence invalid")
	}
	receipt := RepairResultCandidateReceipt{Provenance: provenance, CandidateRef: path, AcceptedEventID: eventID, AcceptedIdempotencyKey: "runtime:s9:issued-repair-candidate:" + p.ID, AcceptedAfterRuntimeRevision: after, AcceptedSequence: seq}
	if err := verifyRepairResultAcceptanceEvent(state, p, j, eventID, receipt.AcceptedIdempotencyKey, "WORKSPACE", path, after, seq); err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	// A current delivery pointer can never disagree with its own original event.
	workspace, _ := state["workspace"].(map[string]any)
	executions, _ := workspace["executions"].(map[string]any)
	for _, value := range executions {
		e, _ := value.(map[string]any)
		if e["runtime_id"] != provenance.RuntimeID || e["agent_id"] != provenance.OwnerAgentID || e["assignment_id"] != provenance.ExecutionAssignmentID || !hypothesisJSONEqual(e["baseline_generation"], provenance.BaselineGeneration) || !hypothesisJSONEqual(e["execution_generation"], provenance.ExecutionGeneration) || stringValue(e["rework_ref"]) != provenance.ReworkRef {
			continue
		}
		if ref := stringValue(e["delivery_ref"]); ref != path {
			return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 current attempt lacks its exact original typed delivery pointer")
		}
	}
	return receipt, true, nil
}

// ValidateRepairResultProvenanceBinding checks the original automatic source
// tuple only. It grants no approval, reception or completion. The original
// consumer still validates the schema, professional answer, checks and receipt.
func ValidateRepairResultProvenanceBinding(p workpacket.RepairResultPacket, r RepairResultCandidateProvenance) error {
	b := p.Binding.Sources
	if r.PacketID != p.ID || r.OwnerAgentID != b.OwnerAgentID || r.RuntimeID != b.RuntimeID {
		return fmt.Errorf("S9 candidate responsibility differs")
	}
	return nil
}

func verifyRepairResultProvenance(root string, p workpacket.RepairResultPacket, r RepairResultCandidateProvenance, contexts ...context.Context) (map[string]any, error) {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-candidate-provenance.schema.json", encoded); err != nil {
		return nil, err
	}
	if err := ValidateRepairResultProvenanceBinding(p, r); err != nil {
		return nil, err
	}
	draft := p.Draft
	draft.Answer = r.SubmittedAnswer
	draftData, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	answer, problems := workpacket.ValidateRepairResultAnswer(p, draftData)
	if len(problems) > 0 {
		return nil, fmt.Errorf("S9 candidate full answer differs: %v", problems)
	}
	var professional map[string]any
	if json.Unmarshal(answer, &professional) != nil || professional["verdict"] != "completed" {
		return nil, fmt.Errorf("S9 candidate is not a completed professional result")
	}
	return professional, nil
}

func verifyRepairResultCandidateDocument(root string, p workpacket.RepairResultPacket, doc map[string]any, r RepairResultCandidateProvenance, contexts ...context.Context) error {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return err
	}

	professional, err := verifyRepairResultProvenance(root, p, r, ctx)
	if err != nil {
		return err
	}
	b := p.Binding.Sources
	if doc["runtime_id"] != b.RuntimeID || doc["session_id"] != b.SessionID || doc["plan_id"] != b.PlanID {
		return fmt.Errorf("S9 candidate responsibility differs")
	}
	proposal, _ := doc["proposal"].(map[string]any)
	if proposal["assignment_id"] != b.RepairAssignmentID || proposal["producer_agent_id"] != b.OwnerAgentID {
		return fmt.Errorf("S9 candidate owner differs")
	}
	return verifyRepairResultProfessionalProjection(p, professional, proposal)
}

func verifyRepairResultProfessionalProjection(p workpacket.RepairResultPacket, answer, document map[string]any) error {
	for _, key := range []string{"verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs", "scope_deviations", "residual_risks", "migration_ref", "rollback_ref"} {
		// Legacy final artifact omits empty migration/rollback fields.
		value, exists := document[key]
		if !exists && (key == "migration_ref" || key == "rollback_ref") {
			value = ""
		}
		if !exists && (key == "detection_gap_artifacts" || key == "evidence_refs") {
			if refs, ok := checkpointStringSlice(answer[key]); ok && len(refs) == 0 {
				value = []string{}
			}
		}
		if key == "unit_results" {
			// Legacy optional projection fields may omit an explicitly empty
			// invariant list. The complete authored list remains in provenance.
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			var units []map[string]any
			if json.Unmarshal(encoded, &units) == nil {
				answered := objectRows(answer[key])
				for i, unit := range units {
					if i >= len(answered) {
						break
					}
					if _, has := unit["restored_invariant_refs"]; !has {
						if refs, ok := checkpointStringSlice(answered[i]["restored_invariant_refs"]); ok && len(refs) == 0 {
							unit["restored_invariant_refs"] = []string{}
						}
					}
				}
				value = units
			}
		}
		if !hypothesisJSONEqual(value, answer[key]) {
			return fmt.Errorf("S9 %s professional projection differs", key)
		}
	}
	var report map[string]any
	if json.Unmarshal(p.Inputs.PlanReport, &report) != nil || !hypothesisJSONEqual(document["before_fix_checks"], report["red_checks"]) {
		return fmt.Errorf("S9 before-fix checks were not taken from exact accepted PlanReport")
	}
	return nil
}

func verifyRepairResultOwnCheckFacts(root string, _ workpacket.RepairResultPacket, _ RepairResultCandidateProvenance, contexts ...context.Context) error {
	return repairPacketContext(root, contexts...).Err()
}

func verifyRepairResultAcceptanceEvent(state map[string]any, p workpacket.RepairResultPacket, j controlproof.JournalInspection, eventID, key, transition, ref string, after, seq int) error {
	index, ok := j.EventIndex[eventID]
	issue, issued := j.EventIndex[p.IssuanceEventID]
	if !ok || !issued || index <= issue {
		return fmt.Errorf("S9 original result reception missing or precedes issuance")
	}
	event := j.Events[index]
	before, be := integerField(event, "before_revision")
	actualAfter, ae := integerField(event, "after_revision")
	actualSeq, se := integerField(event, "sequence")
	gen, ge := integerField(event, "baseline_generation")
	revision, re := integerField(state, "revision")
	actor, _ := event["actor"].(map[string]any)
	from, _ := event["from"].(map[string]any)
	to, _ := event["to"].(map[string]any)
	refs, _ := checkpointStringSlice(event["evidence_ids"])
	expectedEvent := "transition_committed"
	if be != nil || ae != nil || se != nil || ge != nil || re != nil || actualAfter != before+1 || actualAfter != after || actualSeq != seq || actualAfter > revision || gen < 1 || event["event"] != expectedEvent || event["outcome"] != "committed" || event["transition_id"] != transition || event["idempotency_key"] != key || event["runtime_id"] != p.Binding.Sources.RuntimeID || actor["id"] != p.Binding.Sources.OwnerAgentID || actor["type"] != "agent" || !hypothesisStringContains(refs, ref) || !hypothesisJSONEqual(from, to) {
		return fmt.Errorf("S9 original result reception event tuple differs")
	}
	return nil
}

func repairResultIndexedRefs(state map[string]any) ([]map[string]any, error) {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	refs := []map[string]any{}
	seen := map[string]bool{}
	add := func(row map[string]any) error {
		path := stringValue(row["path"])
		if path == "" {
			return fmt.Errorf("S9 original Result index lacks path")
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
		refs = append(refs, row)
		return nil
	}
	if value, exists := pointer["result_refs"]; exists && value != nil {
		rows, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("S9 Result refs is not an array")
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("S9 Result index row not an object")
			}
			if err := add(row); err != nil {
				return nil, err
			}
		}
	}
	if path := stringValue(pointer["result_ref"]); path != "" {
		if err := add(map[string]any{"path": path}); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func VerifiedRepairResultReceipt(root string, state map[string]any, packetID, owner string) (RepairResultReceipt, bool, error) {
	return VerifiedRepairResultReceiptContext(context.Background(), root, state, packetID, owner)
}
func VerifiedRepairResultReceiptContext(ctx context.Context, root string, state map[string]any, packetID, owner string) (RepairResultReceipt, bool, error) {
	ctx = repairPacketContext(root, ctx)
	p, ok, err := FindRepairResultPacketContext(ctx, state, packetID)
	if err != nil || !ok {
		return RepairResultReceipt{}, false, err
	}
	if owner == "" || owner != p.Binding.Sources.OwnerAgentID {
		return RepairResultReceipt{}, false, fmt.Errorf("S9 Result receipt owner differs")
	}
	return findRepairResultReceipt(root, state, p, controlproof.JournalInspection{}, ctx)
}

func findRepairResultReceipt(root string, state map[string]any, p workpacket.RepairResultPacket, j controlproof.JournalInspection, contexts ...context.Context) (RepairResultReceipt, bool, error) {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return RepairResultReceipt{}, false, err
	}

	refs, err := repairResultIndexedRefs(state)
	if err != nil {
		return RepairResultReceipt{}, false, err
	}
	var found *RepairResultReceipt
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return RepairResultReceipt{}, false, err
		}
		path := stringValue(ref["path"])
		doc, err := readRepairPacketSource(root, path, "repair-result.schema.json")
		if err != nil {
			return RepairResultReceipt{}, false, err
		}
		raw, exists := doc["receipt"]
		if !exists {
			continue
		}
		receiptMap, ok := raw.(map[string]any)
		if !ok {
			return RepairResultReceipt{}, false, fmt.Errorf("S9 final receipt is not object")
		}
		provenance, _ := receiptMap["provenance"].(map[string]any)
		if provenance["packet_id"] != p.ID {
			continue
		}
		if found != nil {
			return RepairResultReceipt{}, false, fmt.Errorf("S9 duplicate final receipt")
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return RepairResultReceipt{}, false, err
		}
		var receipt RepairResultReceipt
		if err := decodeOriginalRepairResultReceipt(data, &receipt); err != nil {
			return RepairResultReceipt{}, false, err
		}
		if err := verifyRepairResultReceipt(root, state, p, j, doc, path, receipt, ctx); err != nil {
			return RepairResultReceipt{}, false, err
		}
		found = &receipt
	}
	if found == nil {
		if _, exists := j.EventIndex["evt-s9-issued-repair-result-"+p.ID]; exists {
			return RepairResultReceipt{}, false, fmt.Errorf("S9 accepted Result event lacks retained original artifact")
		}
		return RepairResultReceipt{}, false, nil
	}
	return *found, true, nil
}

func verifyRepairResultReceipt(root string, _ map[string]any, p workpacket.RepairResultPacket, _ controlproof.JournalInspection, document map[string]any, path string, r RepairResultReceipt, contexts ...context.Context) error {
	if err := repairPacketContext(root, contexts...).Err(); err != nil {
		return err
	}
	if r.Provenance.PacketID != p.ID || r.Provenance.OwnerAgentID != p.Binding.Sources.OwnerAgentID || r.ResultRef != path {
		return fmt.Errorf("S9 Result receipt responsibility differs")
	}
	var answer map[string]any
	if json.Unmarshal(r.Provenance.SubmittedAnswer, &answer) != nil {
		return fmt.Errorf("S9 original Result answer unavailable")
	}
	for _, key := range []string{"verdict", "before_fix_results", "unit_results", "detection_gap_artifacts", "evidence_refs", "scope_deviations", "residual_risks", "migration_ref", "rollback_ref"} {
		if value, ok := document[key]; ok && !hypothesisJSONEqual(value, answer[key]) {
			return fmt.Errorf("S9 Result professional %s differs", key)
		}
	}
	return nil
}

func VerifyRepairResultMainProof(root string, p workpacket.RepairResultPacket, provenance RepairResultCandidateProvenance, proof RepairResultMainProof, checksDocument map[string]any) error {
	encoded, err := json.Marshal(checksDocument)
	if err != nil {
		return err
	}
	if err := workpacket.ValidateUniqueJSON(encoded); err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	return verifyRepairResultMainProof(root, p, RepairResultReceipt{Provenance: provenance, MainProof: proof}, document)
}

func verifyRepairResultMainProof(root string, p workpacket.RepairResultPacket, r RepairResultReceipt, _ map[string]any, contexts ...context.Context) error {
	if err := repairPacketContext(root, contexts...).Err(); err != nil {
		return err
	}
	if r.MainProof.CheckpointRef == "" {
		return fmt.Errorf("S9 Main checkpoint unavailable")
	}
	doc, err := readRepairPacketSource(root, r.MainProof.CheckpointRef, "")
	if err != nil {
		return err
	}
	switch doc["state"] {
	case "verified", "acknowledged", "cleanup_pending", "complete":
	default:
		return fmt.Errorf("S9 Main checkpoint not yet verified")
	}
	if id := stringValue(doc["assignment_id"]); id != p.Binding.Sources.RepairAssignmentID && id != p.Binding.Sources.ManifestAssignmentID {
		return fmt.Errorf("S9 Main checkpoint belongs to another responsibility")
	}
	return nil
}

func validateRepairResultCheckpointIndices(root string, state map[string]any, journalBytes []byte, contexts ...context.Context) (map[string]any, error) {
	ctx := repairPacketContext(root, contexts...)
	packets, err := RepairResultPacketsContext(ctx, state)
	if err != nil {
		return nil, err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, journalBytes, 1)
	if err != nil {
		return nil, err
	}
	verifier, err := newRepairResultResumeVerifier(root, state, j, packets)
	if err != nil {
		return nil, err
	}
	byID := map[string]workpacket.RepairResultPacket{}
	for _, p := range packets {
		if err := verifyRepairResultPacketIssueWithVerifier(root, state, p, verifier); err != nil {
			return nil, err
		}
		byID[p.ID] = p
	}
	for _, event := range j.Events {
		id := stringValue(event["event_id"])
		for _, prefix := range []string{"evt-repair-result-packet-", "evt-s9-issued-repair-candidate-", "evt-s9-issued-repair-result-"} {
			if strings.HasPrefix(id, prefix) {
				p, ok := byID[strings.TrimPrefix(id, prefix)]
				if !ok || (prefix == "evt-repair-result-packet-" && p.IssuanceEventID != id) {
					return nil, fmt.Errorf("S9 orphan typed Result issuance/reception event")
				}
			}
		}
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	for _, p := range packets {
		if _, _, err := findRepairResultCandidateReceipt(root, state, p, j, ctx); err != nil {
			return nil, err
		}
		if pointer["session_id"] == p.Binding.Sources.SessionID {
			if _, _, err := findRepairResultReceipt(root, state, p, j, ctx); err != nil {
				return nil, err
			}
		}
	}
	refs, err := repairResultIndexedRefs(state)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		doc, err := readRepairPacketSource(root, stringValue(ref["path"]), "repair-result.schema.json")
		if err != nil {
			return nil, err
		}
		if raw, exists := doc["receipt"]; exists {
			rmap, _ := raw.(map[string]any)
			provenance, _ := rmap["provenance"].(map[string]any)
			p, found := byID[stringValue(provenance["packet_id"])]
			if !found || pointer["session_id"] != p.Binding.Sources.SessionID {
				return nil, fmt.Errorf("S9 original Result has no exact issued Session packet")
			}
		}
	}
	if len(packets) == 0 {
		return state, nil
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	filtered := make([]any, 0, len(rows)-len(packets))
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != repairResultPacketTemplateID {
			filtered = append(filtered, raw)
		}
	}
	clone := make(map[string]any, len(state))
	for key, value := range state {
		clone[key] = value
	}
	cloneEntities := make(map[string]any, len(entities))
	for key, value := range entities {
		cloneEntities[key] = value
	}
	cloneEntities["work_packets"] = filtered
	clone["entities"] = cloneEntities
	return clone, nil
}
