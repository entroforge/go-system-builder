package runtime

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// RepairResultCompletionReport identifies the original accepted, non-S6 S9
// completion envelope. It is evidence, never a candidate or work-complete grant.
type RepairResultCompletionReport struct {
	Ref     string
	SHA256  string
	EventID string
}

// CurrentRepairResultCompletionReport reads the original completion registry
// and journal. It does not advance the Agent, recover pending writes or register
// evidence. The delivery consumer must also validate reported PLAN authority.
func CurrentRepairResultCompletionReport(root string, state map[string]any, p workpacket.RepairResultPacket) (RepairResultCompletionReport, error) {
	j, err := repairResultJournal(root, state, p)
	if err != nil {
		return RepairResultCompletionReport{}, err
	}
	b := p.Binding.Sources
	entities, _ := state["entities"].(map[string]any)
	ref := ""
	for _, a := range objectRows(entities["agents"]) {
		if a["id"] != b.OwnerAgentID {
			continue
		}
		if ref != "" || workstate.AgentActivity(a) != "reported" {
			return RepairResultCompletionReport{}, fmt.Errorf("S9 delivery requires one original reported Agent")
		}
		ref = stringValue(a["completion_reported_ref"])
		ids, _ := checkpointStringSlice(a["task_ids"])
		if ref == "" || !hypothesisStringContains(ids, b.TaskID) {
			return RepairResultCompletionReport{}, fmt.Errorf("S9 original reported Agent lacks its completion report/TASK")
		}
	}
	if ref == "" {
		return RepairResultCompletionReport{}, fmt.Errorf("S9 original completion report is missing")
	}
	taskMatches := 0
	for _, task := range objectRows(entities["tasks"]) {
		if task["id"] != b.TaskID {
			continue
		}
		taskMatches++
		activity := workstate.TaskActivity(task)
		if taskMatches > 1 || task["completion_report_ref"] != ref || (activity != "review" && activity != "done") {
			return RepairResultCompletionReport{}, fmt.Errorf("S9 TASK completion report differs from its owner or original lifecycle")
		}
	}
	proof := RepairResultCompletionReport{Ref: ref}
	for _, row := range objectRows(state["evidence"]) {
		if row["path"] != ref {
			continue
		}
		if proof.SHA256 != "" || row["status"] != "valid" || row["invalidated_by"] != nil {
			return RepairResultCompletionReport{}, fmt.Errorf("S9 current completion evidence is ambiguous or invalid")
		}
		proof.SHA256 = stringValue(row["sha256"])
	}
	if proof.SHA256 == "" {
		return RepairResultCompletionReport{}, fmt.Errorf("S9 original completion evidence index is missing")
	}
	return verifyRepairResultCompletionReport(root, state, p, j, proof, -1)
}

// beforeIndex is the candidate acceptance event for historical proof, or -1
// for the current pre-delivery proof. Historical reads retain the original
// report even if the Agent/TASK has since advanced or reported again.
func verifyRepairResultCompletionReport(root string, state map[string]any, p workpacket.RepairResultPacket, j controlproof.JournalInspection, proof RepairResultCompletionReport, beforeIndex int) (RepairResultCompletionReport, error) {
	b := p.Binding.Sources
	prefix := fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", b.RuntimeID, b.BaselineGeneration, b.OwnerAgentID)
	if !strings.HasPrefix(proof.Ref, prefix) || !strings.HasSuffix(proof.Ref, ".json") {
		return proof, fmt.Errorf("S9 completion report is outside its original owner slot")
	}
	doc, err := readRepairPacketSource(root, proof.Ref, proof.SHA256, "")
	if err != nil {
		return proof, err
	}
	id := stringValue(doc["evidence_id"])
	baseID := fmt.Sprintf("ev-completion-%s-g%d", b.TaskID, b.BaselineGeneration)
	validID := id == baseID
	if suffix, found := strings.CutPrefix(id, baseID+"-r"); found {
		n, err := strconv.Atoi(suffix)
		validID = err == nil && n >= 2 && strconv.Itoa(n) == suffix
	}
	if !validID || proof.Ref != prefix+id+".json" || doc["schema_version"] != "1.0.0" || doc["kind"] != "completion_report" || doc["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(doc["baseline_generation"], b.BaselineGeneration) || doc["producer_agent_id"] != b.OwnerAgentID || doc["producer_responsibility"] != "BUILD-WORK-PACKAGE" || doc["task_id"] != b.TaskID || doc["conclusion"] != "completed" {
		return proof, fmt.Errorf("S9 original completion envelope identity differs")
	}
	matched := 0
	for _, row := range objectRows(state["evidence"]) {
		if row["id"] != id && row["path"] != proof.Ref {
			continue
		}
		owners, ok := checkpointStringSlice(row["produced_by"])
		if row["id"] != id || row["path"] != proof.Ref || row["sha256"] != proof.SHA256 || row["kind"] != "completion_report" || row["responsibility_id"] != "BUILD-WORK-PACKAGE" || !hypothesisJSONEqual(row["baseline_generation"], b.BaselineGeneration) || !ok || len(owners) != 1 || owners[0] != b.OwnerAgentID {
			return proof, fmt.Errorf("S9 original completion evidence tuple differs")
		}
		matched++
	}
	if matched != 1 {
		return proof, fmt.Errorf("S9 original completion evidence is not uniquely registered")
	}
	issue, issued := j.EventIndex[p.IssuanceEventID]
	if !issued {
		return proof, fmt.Errorf("S9 completion has no original packet issuance")
	}
	attemptStart := -1
	for i, event := range j.Events {
		if i >= issue {
			break
		}
		if event["transition_id"] == "PTR-BUG-11" {
			attemptStart = i
		}
		refs, _ := checkpointStringSlice(event["evidence_ids"])
		if p.Binding.ReworkRef != "" && event["transition_id"] == "WORKSPACE-REWORK" && hypothesisStringContains(refs, p.Binding.ReworkRef) {
			attemptStart = i
		}
	}
	if attemptStart < 0 {
		return proof, fmt.Errorf("S9 completion lacks its original execution/attempt start")
	}
	if p.Binding.Resume != nil {
		// The caller supplies the journal returned by repairResultJournal, which
		// has already verified this packet's complete Resume chain.
		resolution, exists := j.EventIndex[p.Binding.Resume.ResolutionEventID]
		if !exists || resolution >= issue {
			return proof, fmt.Errorf("S9 resumed completion packet was not issued after its accepted resolution")
		}
		// Keep the original Begin/Rework floor and raise it past the verified
		// resolution and successor issue. Completion must belong to this new
		// attempt, not merely to the accepted predecessor.
		if resolution > attemptStart {
			attemptStart = resolution
		}
		if issue > attemptStart {
			attemptStart = issue
		}
	}
	matched = 0
	for i, event := range j.Events {
		refs, _ := checkpointStringSlice(event["evidence_ids"])
		if !hypothesisStringContains(refs, id) || event["transition_id"] != "BUILDER-RESULT" {
			continue
		}
		if len(refs) != 1 {
			return proof, fmt.Errorf("S9 original completion event evidence differs")
		}
		actor, _ := event["actor"].(map[string]any)
		from, _ := event["from"].(map[string]any)
		to, _ := event["to"].(map[string]any)
		before, be := integerField(event, "before_revision")
		after, ae := integerField(event, "after_revision")
		gen, ge := integerField(event, "baseline_generation")
		revision, re := integerField(state, "revision")
		if be != nil || ae != nil || ge != nil || re != nil || after != before+1 || after > revision || gen != b.BaselineGeneration || event["transition_id"] != "BUILDER-RESULT" || event["event"] != "transition_committed" || event["outcome"] != "committed" || event["runtime_id"] != b.RuntimeID || actor["id"] != "orchestrator" || actor["type"] != "orchestrator" || from["state"] != "bug_resolution" || from["phase"] != "fixing" || !hypothesisJSONEqual(from, to) || i <= attemptStart || (beforeIndex >= 0 && i >= beforeIndex) {
			return proof, fmt.Errorf("S9 original completion reception event differs or is out of order")
		}
		wantID := fmt.Sprintf("evt-complete-%s-%s-r%d", b.OwnerAgentID, b.TaskID, after)
		wantKey := fmt.Sprintf("runtime:complete:%s:%s:%d", b.OwnerAgentID, b.TaskID, before)
		if event["event_id"] != wantID || event["idempotency_key"] != wantKey || (proof.EventID != "" && proof.EventID != wantID) {
			return proof, fmt.Errorf("S9 original completion reception identity differs")
		}
		proof.EventID = wantID
		matched++
	}
	if matched != 1 {
		return proof, fmt.Errorf("S9 original completion reception is not unique")
	}
	return proof, nil
}
