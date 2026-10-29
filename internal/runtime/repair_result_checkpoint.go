package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
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
	path := ""
	for _, ref := range refs {
		if strings.HasPrefix(ref, ".claude/review/repair/candidates/") {
			if path != "" {
				return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 candidate event has ambiguous artifact refs")
			}
			path = ref
		}
	}
	sha := strings.TrimSuffix(filepath.Base(path), ".json")
	if len(sha) != 64 || path != ".claude/review/repair/candidates/"+sha+".json" {
		return RepairResultCandidateReceipt{}, false, fmt.Errorf("S9 candidate event has no exact full-hash artifact")
	}
	if err := ctx.Err(); err != nil {
		return RepairResultCandidateReceipt{}, false, err
	}
	doc, err := readRepairPacketSource(root, path, sha, "repair-workspace-candidate.schema.json")
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
	receipt := RepairResultCandidateReceipt{Provenance: provenance, CandidateRef: path, CandidateSHA256: sha, AcceptedEventID: eventID, AcceptedIdempotencyKey: "runtime:s9:issued-repair-candidate:" + p.ID, AcceptedAfterRuntimeRevision: after, AcceptedSequence: seq}
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
		if ref := stringValue(e["delivery_ref"]); ref != path || e["delivery_sha256"] != sha {
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
	digest, err := workpacket.RepairResultPacketContentDigest(p)
	if err != nil {
		return err
	}
	var execution map[string]any
	if json.Unmarshal(p.Inputs.Execution, &execution) != nil {
		return fmt.Errorf("S9 frozen execution invalid")
	}
	if r.Contract != "s9-repair-result-candidate-v1" || r.PacketID != p.ID || r.PacketSHA256 != digest || r.IssuanceEventID != p.IssuanceEventID || r.OwnerAgentID != b.OwnerAgentID || r.RuntimeID != b.RuntimeID || r.BaselineGeneration != b.BaselineGeneration || r.ReviewRound != b.ReviewRound || r.ExecutionSHA256 != p.Binding.ExecutionSHA256 || r.ExecutionAssignmentID != execution["assignment_id"] || !hypothesisJSONEqual(execution["execution_generation"], r.ExecutionGeneration) || r.ExecutionRoot != execution["worktree_path"] || r.BaseCommit != execution["base_commit"] || r.PlanReportRef != p.Binding.PlanReportRef || r.PlanReportSHA256 != p.Binding.PlanReportSHA256 || r.ReworkRef != p.Binding.ReworkRef || r.ReworkSHA256 != p.Binding.ReworkSHA256 || r.SourceHead == "" {
		return fmt.Errorf("S9 candidate provenance identity differs")
	}
	if len(r.PrivateSubmissions) != 0 {
		var report struct {
			Receipt *struct {
				PacketID string `json:"packet_id"`
			} `json:"receipt"`
		}
		if err := json.Unmarshal(p.Inputs.PlanReport, &report); err != nil || report.Receipt == nil || len(r.PrivateSubmissions) != 2 {
			return fmt.Errorf("S9 private provenance original Plan identity unavailable")
		}
		dir := ".loop-submissions/native-s9/" + report.Receipt.PacketID + "/"
		for i, kind := range []string{"plan", "result"} {
			item := r.PrivateSubmissions[i]
			if item.Kind != kind || item.Path != dir+kind+".json" || len(item.SHA256) != 64 {
				return fmt.Errorf("S9 private provenance differs from original responsibility")
			}
		}
	}
	resumeSHA, err := workpacket.RepairResultResumeSHA256(p.Binding.Resume)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r.Resume, p.Binding.Resume) || r.ResumeSHA256 != resumeSHA {
		return fmt.Errorf("S9 candidate provenance Resume tuple/digest differs from its issued packet")
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
	if len(problems) > 0 || workpacket.Digest(answer) != r.AnswerSHA256 || string(answer) != string(r.SubmittedAnswer) {
		return nil, fmt.Errorf("S9 candidate full answer differs: %v", problems)
	}
	var professional map[string]any
	if json.Unmarshal(answer, &professional) != nil || professional["verdict"] != "completed" {
		return nil, fmt.Errorf("S9 candidate is not a completed professional result")
	}
	if err := verifyRepairResultOwnCheckFacts(root, p, r, ctx); err != nil {
		return nil, err
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
	if doc["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(doc["baseline_generation"], b.BaselineGeneration) || doc["assignment_id"] != r.ExecutionAssignmentID || !hypothesisJSONEqual(doc["execution_generation"], r.ExecutionGeneration) || doc["source_head"] != r.SourceHead || doc["base_commit"] != r.BaseCommit || doc["session_id"] != b.SessionID || doc["plan_id"] != b.PlanID || doc["contract_sha256"] != b.ContractSHA256 {
		return fmt.Errorf("S9 candidate original source tuple differs")
	}
	proposal, _ := doc["proposal"].(map[string]any)
	if proposal["result_id"] != p.ResultID || proposal["assignment_id"] != b.RepairAssignmentID || proposal["producer_agent_id"] != b.OwnerAgentID || proposal["result"] != "pass" {
		return fmt.Errorf("S9 candidate machine proposal identity differs")
	}
	for _, pair := range []struct{ key, ref, sha string }{{"contract", b.ContractRef, b.ContractSHA256}, {"session", b.SessionRef, b.SessionSHA256}, {"plan", b.PlanRef, b.PlanSHA256}, {"plan_report", p.Binding.PlanReportRef, p.Binding.PlanReportSHA256}} {
		source, _ := proposal[pair.key].(map[string]any)
		if source["path"] != pair.ref || source["sha256"] != pair.sha {
			return fmt.Errorf("S9 candidate proposal %s source differs", pair.key)
		}
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

func verifyRepairResultOwnCheckFacts(root string, p workpacket.RepairResultPacket, r RepairResultCandidateProvenance, contexts ...context.Context) error {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return err
	}

	var definition struct {
		Location string              `json:"check_location"`
		Checks   []string            `json:"required_checks"`
		Profiles []checkexec.Profile `json:"check_profiles"`
		Owned    []struct {
			Index   int               `json:"index"`
			Command string            `json:"command"`
			Profile checkexec.Profile `json:"profile"`
		} `json:"worker_checks"`
	}
	if err := json.Unmarshal(p.Inputs.Execution, &definition); err != nil {
		return err
	}
	binding := checkexec.Binding{RuntimeID: r.RuntimeID, Generation: r.BaselineGeneration, AssignmentID: r.ExecutionAssignmentID, Owner: r.OwnerAgentID, ExecutionGeneration: r.ExecutionGeneration, Responsibility: "owned_check"}
	verify := func(ref checkexec.ToolReceipt, profile checkexec.Profile) error {
		if ref.Binding != binding || ref.Checkout != r.ExecutionRoot || ref.Command != profile.Command || ref.ProfileDigest != profile.Digest() {
			return fmt.Errorf("S9 own check tuple/profile differs")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		verified, err := checkexec.VerifyToolReceipt(filepath.Join(root, ".claude/evidence/workspace-checks"), ref)
		if err != nil {
			return err
		}
		if !verified.Passed {
			return fmt.Errorf("S9 own check did not pass")
		}
		return nil
	}
	if definition.Location == "main" {
		if len(r.WorkerCheckReceipts) != 0 {
			return fmt.Errorf("S9 Main-owned checks cannot be Worker check receipts")
		}
	} else {
		expected := 0
		for index, command := range definition.Checks {
			if strings.HasPrefix(strings.TrimSpace(command), "locked:") {
				continue
			}
			expected++
			var selected *checkexec.Profile
			for i := range definition.Profiles {
				if definition.Profiles[i].CheckIndex == index {
					if selected != nil {
						return fmt.Errorf("S9 duplicate own profile")
					}
					selected = &definition.Profiles[i]
				}
			}
			if selected == nil || selected.Command != command {
				return fmt.Errorf("S9 own required check lacks frozen profile")
			}
			matches := 0
			for _, ref := range r.WorkerCheckReceipts {
				if ref.Command == command && ref.ProfileDigest == selected.Digest() {
					matches++
					if err := verify(ref, *selected); err != nil {
						return err
					}
				}
			}
			if matches != 1 {
				return fmt.Errorf("S9 own required check receipt coverage differs")
			}
		}
		if len(r.WorkerCheckReceipts) != expected {
			return fmt.Errorf("S9 own check receipt set contains extra facts")
		}
	}
	if len(definition.Owned) != len(r.OwnedCheckReceipts) {
		return fmt.Errorf("S9 independently owned check receipt coverage differs")
	}
	for _, check := range definition.Owned {
		matches := 0
		for _, row := range r.OwnedCheckReceipts {
			if row.WorkerCheckIndex == check.Index {
				matches++
				if err := verify(row.Receipt, check.Profile); err != nil {
					return err
				}
			}
		}
		if matches != 1 {
			return fmt.Errorf("S9 frozen WorkerCheck lacks unique received proof")
		}
	}
	return nil
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
	if be != nil || ae != nil || se != nil || ge != nil || re != nil || actualAfter != before+1 || actualAfter != after || actualSeq != seq || actualAfter > revision || gen != p.Binding.Sources.BaselineGeneration || event["event"] != expectedEvent || event["outcome"] != "committed" || event["transition_id"] != transition || event["idempotency_key"] != key || event["runtime_id"] != p.Binding.Sources.RuntimeID || actor["id"] != p.Binding.Sources.OwnerAgentID || actor["type"] != "agent" || !hypothesisStringContains(refs, ref) || from["state"] != "bug_resolution" || from["phase"] != "fixing" || !hypothesisJSONEqual(from, to) {
		return fmt.Errorf("S9 original result reception event tuple differs")
	}
	return nil
}

func repairResultIndexedRefs(state map[string]any) ([]map[string]any, error) {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	refs := []map[string]any{}
	seen := map[string]string{}
	add := func(row map[string]any) error {
		path, sha := stringValue(row["path"]), stringValue(row["sha256"])
		if path == "" || sha == "" {
			return fmt.Errorf("S9 original Result index lacks path/SHA")
		}
		if old, exists := seen[path]; exists {
			if old != sha {
				return fmt.Errorf("S9 Result index has conflicting SHA")
			}
			return nil
		}
		seen[path] = sha
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
		if err := add(map[string]any{"path": path, "sha256": pointer["result_sha256"]}); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func VerifiedRepairResultReceipt(root string, state map[string]any, packetID, owner string) (RepairResultReceipt, bool, error) {
	return VerifiedRepairResultReceiptContext(context.Background(), root, state, packetID, owner)
}
func VerifiedRepairResultReceiptContext(ctx context.Context, root string, state map[string]any, packetID, owner string) (RepairResultReceipt, bool, error) {
	ctx = controlproof.WithJournalReadCache(repairPacketContext(root, ctx))
	p, ok, err := FindRepairResultPacketContext(ctx, state, packetID)
	if err != nil || !ok {
		return RepairResultReceipt{}, false, err
	}
	if owner == "" || owner != p.Binding.Sources.OwnerAgentID {
		return RepairResultReceipt{}, false, fmt.Errorf("S9 Result receipt owner differs")
	}
	j, err := repairResultJournal(root, state, p, ctx)
	if err != nil {
		return RepairResultReceipt{}, false, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	if pointer["session_id"] != p.Binding.Sources.SessionID {
		return RepairResultReceipt{}, false, fmt.Errorf("S9 historical Result has no retained original Session authority")
	}
	return findRepairResultReceipt(root, state, p, j, ctx)
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
		doc, err := readRepairPacketSource(root, path, stringValue(ref["sha256"]), "repair-result.schema.json")
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
		if err := json.Unmarshal(data, &receipt); err != nil {
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

func verifyRepairResultReceipt(root string, state map[string]any, p workpacket.RepairResultPacket, j controlproof.JournalInspection, document map[string]any, path string, r RepairResultReceipt, contexts ...context.Context) error {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-result-receipt.schema.json", data); err != nil {
		return err
	}
	if r.Contract != "s9-repair-result-receipt-v1" || !r.WorkComplete || r.ResultID != p.ResultID || r.ResultRef != path || path != ".claude/review/repair/results/"+p.ResultID+".json" || r.AcceptedEventID != "evt-s9-issued-repair-result-"+p.ID || r.AcceptedIdempotencyKey != "runtime:s9:issued-repair-result:"+p.ID {
		return fmt.Errorf("S9 final Result receipt identity differs")
	}
	answer, err := verifyRepairResultProvenance(root, p, r.Provenance, ctx)
	if err != nil {
		return err
	}
	candidate, ok, err := findRepairResultCandidateReceipt(root, state, p, j, ctx)
	if err != nil || !ok || candidate.CandidateRef != r.CandidateRef || candidate.CandidateSHA256 != r.CandidateSHA256 || !hypothesisJSONEqual(candidate.Provenance, r.Provenance) {
		return fmt.Errorf("S9 final Result has no exact original candidate proof: %v", err)
	}
	b := p.Binding.Sources
	if document["result_id"] != p.ResultID || document["session_id"] != b.SessionID || document["plan_id"] != b.PlanID || document["contract_id"] != b.ContractID || !hypothesisJSONEqual(document["baseline_generation"], b.BaselineGeneration) || document["assignment_id"] != b.RepairAssignmentID || document["producer_agent_id"] != b.OwnerAgentID || document["result"] != "pass" {
		return fmt.Errorf("S9 original Result identity differs")
	}
	report, _ := document["plan_report_ref"].(map[string]any)
	if report["path"] != p.Binding.PlanReportRef || report["sha256"] != p.Binding.PlanReportSHA256 {
		return fmt.Errorf("S9 final Result PlanReport binding differs")
	}
	if err := verifyRepairResultProfessionalProjection(p, answer, document); err != nil {
		return err
	}
	if err := verifyRepairResultAcceptanceEvent(state, p, j, r.AcceptedEventID, r.AcceptedIdempotencyKey, "S9-RESULT-SUBMIT", path, r.AcceptedAfterRuntimeRevision, r.AcceptedSequence); err != nil {
		return err
	}
	ci := j.EventIndex[candidate.AcceptedEventID]
	ri := j.EventIndex[r.AcceptedEventID]
	if ri <= ci {
		return fmt.Errorf("S9 final Result did not follow candidate acceptance")
	}
	if err := verifyRepairResultMainProof(root, p, r, document, ctx); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, stringValue(document["submitted_at"])); err != nil {
		return fmt.Errorf("S9 original Result submitted_at invalid")
	}
	return nil
}

// VerifyRepairResultMainProof rereads the original checkpoint and exact captured
// check bytes. It grants no integration capability: the original S9 consumer
// must hold the workspace lock, rederive the proof and check current Main HEAD.
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

func verifyRepairResultMainProof(root string, p workpacket.RepairResultPacket, r RepairResultReceipt, document map[string]any, contexts ...context.Context) error {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return err
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	canonicalRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return err
	}
	root = canonicalRoot
	proof := r.MainProof
	var executionSlot struct {
		AssignmentID string                            `json:"assignment_id"`
		Record       *assignmentrecord.RecordReference `json:"assignment_record"`
	}
	if err := json.Unmarshal(p.Inputs.Execution, &executionSlot); err != nil {
		return err
	}
	expectedPath := filepath.Join(root, ".claude/evidence", p.Binding.Sources.RuntimeID, fmt.Sprintf("g%d", p.Binding.Sources.BaselineGeneration), "worktree", executionSlot.AssignmentID, "checkpoint.json")
	if executionSlot.Record != nil {
		expectedPath = assignmentrecord.IntegrationCheckpointPath(root, *executionSlot.Record)
	}
	relativeSlot, slotErr := filepath.Rel(root, expectedPath)
	if slotErr != nil || !filepath.IsLocal(relativeSlot) || proof.CheckpointRef != filepath.ToSlash(relativeSlot) {
		return fmt.Errorf("S9 Main proof does not use the original registered checkpoint slot")
	}
	facts, err := canonicalRuntimeJSON(proof.VerifiedFacts)
	if err != nil || workpacket.Digest(facts) != proof.VerifiedFactsSHA256 {
		return fmt.Errorf("S9 Main proof digest differs")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cpData, err := workpacket.ReadFile(root, proof.CheckpointRef, 20<<20)
	if err != nil {
		return fmt.Errorf("S9 retained original Main checkpoint unavailable: %w", err)
	}
	actual, err := ProjectRepairResultMainFacts(cpData)
	if err != nil || string(actual) != string(facts) {
		return fmt.Errorf("S9 original Main verified facts changed: %v", err)
	}
	var checkpoint map[string]any
	if json.Unmarshal(cpData, &checkpoint) != nil {
		return fmt.Errorf("S9 Main checkpoint invalid")
	}
	switch checkpoint["state"] {
	case "verified", "acknowledged", "cleanup_pending", "complete":
	default:
		return fmt.Errorf("S9 retained checkpoint is not a verified Main lifecycle")
	}
	var definition struct {
		AssignmentID string `json:"assignment_id"`
		Branch       string `json:"branch"`
		Target       string `json:"target_branch"`
		Path         string `json:"worktree_path"`
	}
	if err := json.Unmarshal(p.Inputs.Execution, &definition); err != nil {
		return err
	}
	if checkpoint["assignment_id"] != definition.AssignmentID || checkpoint["worktree_path"] != definition.Path || checkpoint["source_branch"] != definition.Branch || checkpoint["target_branch"] != definition.Target || checkpoint["source_head"] != r.Provenance.SourceHead || !hypothesisJSONEqual(checkpoint["baseline_generation"], r.Provenance.BaselineGeneration) {
		return fmt.Errorf("S9 Main checkpoint differs from exact original candidate/execution")
	}
	paths, pathsOK := checkpointStringSlice(checkpoint["check_receipts"])
	if !pathsOK || len(paths) == 0 || stringValue(checkpoint["tested_head"]) == "" {
		return fmt.Errorf("S9 Main checks remain pending: actual executed checks and tested HEAD are required")
	}
	evidenceRoot := filepath.Join(root, filepath.Dir(filepath.FromSlash(proof.CheckpointRef)), "checks")
	actualChecks := map[string]string{}
	seenPaths := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if seenPaths[path] {
			return fmt.Errorf("S9 Main checkpoint repeats a check record")
		}
		seenPaths[path] = true
		relative, err := filepath.Rel(evidenceRoot, path)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("S9 Main check record escaped assignment evidence")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := workpacket.ReadFile(evidenceRoot, filepath.ToSlash(relative), 4<<20)
		if err != nil {
			return err
		}
		var executed map[string]any
		if json.Unmarshal(data, &executed) != nil {
			return fmt.Errorf("S9 Main check record invalid")
		}
		// Failed attempts remain history. A successful retry must have its
		// own real execution at the checkpoint's tested HEAD.
		if executed["status"] != "pass" || !hypothesisJSONEqual(executed["exit_code"], 0) || executed["head_before"] != checkpoint["tested_head"] || executed["head_after"] != checkpoint["tested_head"] {
			continue
		}
		command := stringValue(executed["command"])
		cwd := stringValue(executed["cwd"])
		if cwd != "" && !filepath.IsAbs(cwd) {
			cwd = filepath.Join(root, cwd)
		}
		if strings.TrimSpace(command) == "" || cwd == "" || filepath.Clean(cwd) != root {
			return fmt.Errorf("S9 Main check lacks its actual command or Main checkout")
		}
		relative, err = filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("S9 Main check record escaped Main root")
		}
		actualChecks[command] = "path:" + filepath.ToSlash(relative) + "#sha256=" + workpacket.Digest(data)
	}
	checks := objectRows(document["checks"])
	if len(actualChecks) == 0 || len(checks) != len(actualChecks) {
		return fmt.Errorf("S9 final Result needs the actual successful Main checks")
	}
	for _, check := range checks {
		command := stringValue(check["command"])
		evidence, exists := actualChecks[command]
		if !exists || check["result"] != "pass" || !hypothesisExactStringSet(check["evidence_refs"], []string{evidence}) {
			return fmt.Errorf("S9 final Result checks differ from actual Main execution")
		}
		delete(actualChecks, command)
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
		doc, err := readRepairPacketSource(root, stringValue(ref["path"]), stringValue(ref["sha256"]), "repair-result.schema.json")
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
