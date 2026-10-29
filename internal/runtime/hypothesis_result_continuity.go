package runtime

import (
	"fmt"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// VerifyHypothesisResultContinuity proves that an unanswered packet's own
// sources stayed unchanged across sibling Case revisions. Restoring a current
// field cannot erase an earlier revision or route-consumption transaction.
// Accepted receipt queries deliberately do not call this fresh-work guard.
func VerifyHypothesisResultContinuity(root string, state map[string]any, p workpacket.HypothesisResultPacket) error {
	stored, found, err := FindHypothesisResultPacket(state, p.ID)
	if err != nil || !found || !hypothesisJSONEqual(stored, p) {
		return fmt.Errorf("S8 continuity requires the exact original issued packet: %v", err)
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil || pointer["case_id"] != p.Binding.CaseID || !hypothesisCaseRouteEqual(pointer, p.Inputs.CaseRoute) {
		return fmt.Errorf("S8 current Runtime Case route differs from the unanswered issuance")
	}
	current, err := readCheckpointHypothesisCase(root, stringValue(pointer["path"]), stringValue(pointer["sha256"]))
	if err != nil {
		return err
	}
	revision, err := integerField(current, "revision")
	pointerRevision, ptrErr := integerField(pointer, "revision")
	if err != nil || ptrErr != nil || revision != pointerRevision || revision < p.Binding.CaseRevision {
		return fmt.Errorf("S8 current Case revision has no continuous issuance lineage")
	}
	if err := verifyHypothesisContinuityProjection(current, p); err != nil {
		return err
	}
	if revision == p.Binding.CaseRevision {
		if pointer["path"] != p.Binding.CaseRef || pointer["sha256"] != p.Binding.CaseSHA256 {
			return fmt.Errorf("S8 issuance Case identity differs at the same revision")
		}
	} else {
		byRevision := map[int]map[string]any{}
		for _, row := range objectRows(current["revision_history"]) {
			r, err := integerField(row, "revision")
			if err != nil {
				return err
			}
			if r < p.Binding.CaseRevision || r >= revision {
				continue
			}
			if byRevision[r] != nil {
				return fmt.Errorf("S8 original Case history has duplicate source revisions")
			}
			byRevision[r] = row
		}
		for r := p.Binding.CaseRevision; r < revision; r++ {
			row := byRevision[r]
			if row == nil {
				return fmt.Errorf("S8 original Case history is missing revision %d", r)
			}
			ref := fmt.Sprintf(".claude/review/investigation/cases/%s-r%d.json", p.Binding.CaseID, r)
			if row["path"] != ref || r == p.Binding.CaseRevision && (ref != p.Binding.CaseRef || row["sha256"] != p.Binding.CaseSHA256) {
				return fmt.Errorf("S8 original Case history differs from issuance identity")
			}
			document, err := readCheckpointHypothesisCase(root, ref, stringValue(row["sha256"]))
			if err != nil {
				return err
			}
			actualRevision, revErr := integerField(document, "revision")
			if revErr != nil || actualRevision != r {
				return fmt.Errorf("S8 historical Case revision differs from its original source")
			}
			if err := verifyHypothesisContinuityProjection(document, p); err != nil {
				return err
			}
		}
	}
	data, err := controlproof.ReadJournalForState(root, state)
	if err != nil {
		return err
	}
	journal, err := controlproof.InspectJournalData(data)
	if err != nil {
		return err
	}
	if err := controlproof.ValidateStateJournalPair(state, journal); err != nil {
		return err
	}
	issue, found := journal.EventIndex[p.IssuanceEventID]
	if !found {
		return fmt.Errorf("S8 continuity has no original issuance transaction")
	}
	for _, event := range journal.Events[issue+1:] {
		if event["transition_id"] != "S8-ROUTE-CONSUME" || event["runtime_id"] != p.Binding.RuntimeID {
			continue
		}
		after, err := integerField(event, "after_revision")
		if err != nil {
			return err
		}
		if event["event_id"] == fmt.Sprintf("evt-s8-route-consume-%s-r%d", p.Binding.CaseID, after) ||
			event["idempotency_key"] == fmt.Sprintf("runtime:s8:route-consume:%s:%d", p.Binding.CaseID, after-1) {
			return fmt.Errorf("S8 original Case route was consumed after issuance; Main must register and dispatch a new question")
		}
	}
	return nil
}

func verifyHypothesisContinuityProjection(document map[string]any, p workpacket.HypothesisResultPacket) error {
	b := p.Binding
	if document["case_id"] != b.CaseID || stringValue(document["observation_batch_id"]) != b.ObservationBatchID ||
		stringValue(document["observation_batch_ref"]) != b.ObservationBatchRef || stringValue(document["observation_batch_sha256"]) != b.ObservationBatchSHA256 ||
		stringValue(document["baseline_digest"]) != b.BaselineDigest || !hypothesisJSONEqual(document["baseline_generation"], b.BaselineGeneration) ||
		!hypothesisExactStringSet(document["source_finding_ids"], b.SourceFindingIDs) || !hypothesisCaseRouteEqual(document, p.Inputs.CaseRoute) {
		return fmt.Errorf("S8 own Case source or route changed after issuance")
	}
	matched := 0
	for _, row := range objectRows(document["hypotheses"]) {
		if row["hypothesis_id"] == b.HypothesisID {
			if !hypothesisJSONEqual(row, p.Inputs.Hypothesis) {
				return fmt.Errorf("S8 own hypothesis changed after issuance")
			}
			matched++
		}
	}
	if matched != 1 {
		return fmt.Errorf("S8 original question has no unique continuous Case lineage")
	}
	return nil
}
