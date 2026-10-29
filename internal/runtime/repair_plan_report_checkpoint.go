package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"strings"
	"time"
)

// RepairPlanReportReceipt is stored only in the original immutable report.
// Report SHA comes from the original pointer; this pre-execution milestone
// never marks the Builder's complete responsibility as finished.
type RepairPlanReportReceipt struct {
	Contract                     string          `json:"contract"`
	PacketID                     string          `json:"packet_id"`
	PacketSHA256                 string          `json:"packet_sha256"`
	AnswerSHA256                 string          `json:"answer_sha256"`
	SubmittedAnswer              json.RawMessage `json:"submitted_answer"`
	OwnerAgentID                 string          `json:"owner_agent_id"`
	RuntimeID                    string          `json:"runtime_id"`
	BaselineGeneration           int             `json:"baseline_generation"`
	ReviewRound                  int             `json:"review_round"`
	CaseID                       string          `json:"case_id"`
	CaseRef                      string          `json:"case_ref"`
	CaseSHA256                   string          `json:"case_sha256"`
	ContractID                   string          `json:"contract_id"`
	ContractRef                  string          `json:"contract_ref"`
	ContractSHA256               string          `json:"contract_sha256"`
	SessionID                    string          `json:"session_id"`
	SessionRef                   string          `json:"session_ref"`
	SessionSHA256                string          `json:"session_sha256"`
	PlanID                       string          `json:"plan_id"`
	PlanRef                      string          `json:"plan_ref"`
	PlanSHA256                   string          `json:"plan_sha256"`
	RepairAssignmentID           string          `json:"repair_assignment_id"`
	RepairAssignmentSHA256       string          `json:"repair_assignment_sha256"`
	ManifestAssignmentID         string          `json:"manifest_assignment_id"`
	ManifestAssignmentSHA256     string          `json:"manifest_assignment_sha256"`
	TeamID                       string          `json:"team_id"`
	TaskID                       string          `json:"task_id"`
	TaskRef                      string          `json:"task_ref"`
	TaskSHA256                   string          `json:"task_sha256"`
	ManifestRef                  string          `json:"manifest_ref"`
	ManifestSHA256               string          `json:"manifest_sha256"`
	ExecutionRoot                string          `json:"execution_root"`
	ReportID                     string          `json:"report_id"`
	ReportRef                    string          `json:"report_ref"`
	WorkComplete                 bool            `json:"work_complete"`
	IssuanceEventID              string          `json:"issuance_event_id"`
	AcceptedEventID              string          `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string          `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int             `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int             `json:"accepted_sequence"`
}

// UnmarshalJSON keeps the full professional answer canonical even when the
// original immutable report serializer indents its embedded JSON value.
func (receipt *RepairPlanReportReceipt) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairPlanReportReceipt
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	answer, err := canonicalRuntimeJSON(decoded.SubmittedAnswer)
	if err != nil {
		return fmt.Errorf("S9 receipt submitted_answer: %w", err)
	}
	decoded.SubmittedAnswer = answer
	*receipt = RepairPlanReportReceipt(decoded)
	return nil
}

func VerifiedRepairPlanReportReceipt(root string, state map[string]any, packetID, owner string, contexts ...context.Context) (RepairPlanReportReceipt, bool, error) {
	ctx := controlproof.WithJournalReadCache(repairPacketContext(root, contexts...))
	p, found, err := FindRepairPlanReportPacketContext(ctx, state, packetID)
	if err != nil || !found {
		return RepairPlanReportReceipt{}, false, err
	}
	if owner == "" || owner != p.Binding.OwnerAgentID {
		return RepairPlanReportReceipt{}, false, fmt.Errorf("S9 receipt owner differs")
	}
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return RepairPlanReportReceipt{}, false, err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return RepairPlanReportReceipt{}, false, err
	}
	if err := verifyRepairPlanReportPacketIssue(root, state, p, j, ctx); err != nil {
		return RepairPlanReportReceipt{}, false, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	if pointer["session_id"] != p.Binding.SessionID {
		return RepairPlanReportReceipt{}, false, fmt.Errorf("S9 historical receipt has no original retained Session authority")
	}
	return findRepairPlanReportReceipt(root, state, p, j)
}
func repairPlanReportIndexedRefs(state map[string]any) ([]map[string]any, error) {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	refs := []map[string]any{}
	seen := map[string]string{}
	add := func(row map[string]any) error {
		path, sha := stringValue(row["path"]), stringValue(row["sha256"])
		if path == "" || sha == "" {
			return fmt.Errorf("S9 original PlanReport index missing path/SHA")
		}
		if old, ok := seen[path]; ok {
			if old != sha {
				return fmt.Errorf("S9 PlanReport index conflicting SHA")
			}
			return nil
		}
		seen[path] = sha
		refs = append(refs, row)
		return nil
	}
	if raw, ok := pointer["plan_report_refs"]; ok && raw != nil {
		rows, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("S9 original report refs not array")
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("S9 report ref not object")
			}
			if err := add(row); err != nil {
				return nil, err
			}
		}
	}
	if pointer["plan_report_ref"] != nil && pointer["plan_report_ref"] != "" {
		if err := add(map[string]any{"path": pointer["plan_report_ref"], "sha256": pointer["plan_report_sha256"]}); err != nil {
			return nil, err
		}
	}
	return refs, nil
}
func findRepairPlanReportReceipt(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection) (RepairPlanReportReceipt, bool, error) {
	refs, err := repairPlanReportIndexedRefs(state)
	if err != nil {
		return RepairPlanReportReceipt{}, false, err
	}
	var found *RepairPlanReportReceipt
	for _, ref := range refs {
		doc, err := readRepairPacketSource(root, stringValue(ref["path"]), stringValue(ref["sha256"]), "repair-plan-report.schema.json")
		if err != nil {
			return RepairPlanReportReceipt{}, false, err
		}
		raw, has := doc["receipt"]
		if !has {
			continue
		}
		rmap, ok := raw.(map[string]any)
		if !ok {
			return RepairPlanReportReceipt{}, false, fmt.Errorf("S9 report receipt not object")
		}
		if rmap["packet_id"] != p.ID {
			continue
		}
		if found != nil {
			return RepairPlanReportReceipt{}, false, fmt.Errorf("S9 duplicate report receipt")
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return RepairPlanReportReceipt{}, false, err
		}
		var r RepairPlanReportReceipt
		if err := json.Unmarshal(data, &r); err != nil {
			return RepairPlanReportReceipt{}, false, err
		}
		if err := verifyRepairPlanReportReceipt(root, state, p, j, doc, stringValue(ref["path"]), r); err != nil {
			return RepairPlanReportReceipt{}, false, err
		}
		found = &r
	}
	if found == nil {
		if _, exists := j.EventIndex["evt-s9-issued-plan-report-"+p.ID]; exists {
			return RepairPlanReportReceipt{}, false, fmt.Errorf("S9 accepted event lacks retained original report receipt")
		}
		return RepairPlanReportReceipt{}, false, nil
	}
	return *found, true, nil
}
func verifyRepairPlanReportReceipt(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection, doc map[string]any, path string, r RepairPlanReportReceipt) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("repair-plan-report-receipt.schema.json", data); err != nil {
		return err
	}
	b := p.Binding
	digest, err := workpacket.RepairPlanReportPacketDigest(p)
	if err != nil {
		return err
	}
	if r.Contract != "s9-plan-report-receipt-v1" || r.WorkComplete || r.PacketID != p.ID || r.PacketSHA256 != digest || r.OwnerAgentID != b.OwnerAgentID || r.RuntimeID != b.RuntimeID || r.BaselineGeneration != b.BaselineGeneration || r.ReviewRound != b.ReviewRound || r.ReportID != p.ReportID || r.ReportRef != path || r.ReportRef != ".claude/review/repair/plan-reports/"+p.ReportID+".json" || r.IssuanceEventID != p.IssuanceEventID || r.AcceptedEventID != "evt-s9-issued-plan-report-"+p.ID || r.AcceptedIdempotencyKey != "runtime:s9:issued-plan-report:"+p.ID {
		return fmt.Errorf("S9 receipt identity differs")
	}
	if r.CaseID != b.CaseID {
		return fmt.Errorf("S9 receipt case_id differs")
	}
	if r.CaseRef != b.CaseRef {
		return fmt.Errorf("S9 receipt case_ref differs")
	}
	if r.CaseSHA256 != b.CaseSHA256 {
		return fmt.Errorf("S9 receipt case_sha256 differs")
	}
	if r.ContractID != b.ContractID {
		return fmt.Errorf("S9 receipt contract_id differs")
	}
	if r.ContractRef != b.ContractRef {
		return fmt.Errorf("S9 receipt contract_ref differs")
	}
	if r.ContractSHA256 != b.ContractSHA256 {
		return fmt.Errorf("S9 receipt contract_sha256 differs")
	}
	if r.SessionID != b.SessionID {
		return fmt.Errorf("S9 receipt session_id differs")
	}
	if r.SessionRef != b.SessionRef {
		return fmt.Errorf("S9 receipt session_ref differs")
	}
	if r.SessionSHA256 != b.SessionSHA256 {
		return fmt.Errorf("S9 receipt session_sha256 differs")
	}
	if r.PlanID != b.PlanID {
		return fmt.Errorf("S9 receipt plan_id differs")
	}
	if r.PlanRef != b.PlanRef {
		return fmt.Errorf("S9 receipt plan_ref differs")
	}
	if r.PlanSHA256 != b.PlanSHA256 {
		return fmt.Errorf("S9 receipt plan_sha256 differs")
	}
	if r.RepairAssignmentID != b.RepairAssignmentID {
		return fmt.Errorf("S9 receipt repair_assignment_id differs")
	}
	if r.RepairAssignmentSHA256 != b.RepairAssignmentSHA256 {
		return fmt.Errorf("S9 receipt repair_assignment_sha256 differs")
	}
	if r.ManifestAssignmentID != b.ManifestAssignmentID {
		return fmt.Errorf("S9 receipt manifest_assignment_id differs")
	}
	if r.ManifestAssignmentSHA256 != b.ManifestAssignmentSHA256 {
		return fmt.Errorf("S9 receipt manifest_assignment_sha256 differs")
	}
	if r.TeamID != b.TeamID {
		return fmt.Errorf("S9 receipt team_id differs")
	}
	if r.TaskID != b.TaskID {
		return fmt.Errorf("S9 receipt task_id differs")
	}
	if r.TaskRef != b.TaskRef {
		return fmt.Errorf("S9 receipt task_ref differs")
	}
	if r.TaskSHA256 != b.TaskSHA256 {
		return fmt.Errorf("S9 receipt task_sha256 differs")
	}
	if r.ManifestRef != b.ManifestRef {
		return fmt.Errorf("S9 receipt manifest_ref differs")
	}
	if r.ManifestSHA256 != b.ManifestSHA256 {
		return fmt.Errorf("S9 receipt manifest_sha256 differs")
	}

	draft := p.Draft
	draft.Answer = r.SubmittedAnswer
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	acceptedAnswer, problems := workpacket.ValidateRepairPlanReportAnswer(p, draftBytes)
	if len(problems) > 0 || workpacket.Digest(acceptedAnswer) != r.AnswerSHA256 {
		return fmt.Errorf("S9 receipt professional answer invalid: %v", problems)
	}
	var answer map[string]any
	if err := workpacket.ValidateUniqueJSON(r.SubmittedAnswer); err != nil {
		return err
	}
	if err := json.Unmarshal(r.SubmittedAnswer, &answer); err != nil {
		return err
	}
	canonical, err := json.Marshal(answer)
	if err != nil || workpacket.Digest(canonical) != r.AnswerSHA256 {
		return fmt.Errorf("S9 receipt full answer SHA differs")
	}
	if len(answer) != 3 || !hypothesisJSONEqual(doc["plan"], answer["plan"]) || !hypothesisJSONEqual(doc["red_checks"], answer["red_checks"]) || !hypothesisJSONEqual(doc["proposed_paths"], answer["proposed_paths"]) || doc["report_id"] != p.ReportID || doc["session_id"] != b.SessionID || doc["plan_id"] != b.PlanID || doc["assignment_id"] != b.RepairAssignmentID || doc["agent_id"] != b.OwnerAgentID || doc["status"] != "reported" {
		return fmt.Errorf("S9 report professional projection differs")
	}
	var assignment map[string]any
	if json.Unmarshal(p.Inputs.RepairAssignment, &assignment) != nil || !hypothesisExactStringSet(doc["assertion_ids"], stringValuesRepair(assignment["assertion_ids"])) {
		return fmt.Errorf("S9 report assertion exact set differs")
	}
	ai, ok := j.EventIndex[r.AcceptedEventID]
	ii, iok := j.EventIndex[p.IssuanceEventID]
	if !ok || !iok || ai <= ii {
		return fmt.Errorf("S9 receipt original acceptance missing")
	}
	e := j.Events[ai]
	before, be := integerField(e, "before_revision")
	after, ae := integerField(e, "after_revision")
	seq, se := integerField(e, "sequence")
	gen, ge := integerField(e, "baseline_generation")
	actor, _ := e["actor"].(map[string]any)
	from, _ := e["from"].(map[string]any)
	to, _ := e["to"].(map[string]any)
	refs, _ := checkpointStringSlice(e["evidence_ids"])
	if be != nil || ae != nil || after != before+1 || after != r.AcceptedAfterRuntimeRevision || se != nil || seq != r.AcceptedSequence || ge != nil || gen != b.BaselineGeneration || e["event"] != "transition_committed" || e["outcome"] != "committed" || e["transition_id"] != "PTR-BUG-10" || e["idempotency_key"] != r.AcceptedIdempotencyKey || e["runtime_id"] != b.RuntimeID || actor["id"] != b.OwnerAgentID || actor["type"] != "agent" || len(refs) != 1 || refs[0] != path || from["state"] != "bug_resolution" || (from["phase"] != "planning" && from["phase"] != "reproducing" && from["phase"] != "fixing") || to["state"] != "bug_resolution" || (to["phase"] != "reproducing" && to["phase"] != "fixing") {
		return fmt.Errorf("S9 original acceptance event tuple differs")
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, stringValue(j.Events[ii]["occurred_at"]))
	if err != nil || doc["reported_at"] != issuedAt.UTC().Format(time.RFC3339Nano) {
		return fmt.Errorf("S9 deterministic original report time differs")
	}
	return nil
}
func stringValuesRepair(v any) []string { values, _ := checkpointStringSlice(v); return values }

func validateRepairPlanReportCheckpointIndices(root string, state map[string]any, data []byte, contexts ...context.Context) (map[string]any, error) {
	ctx := repairPacketContext(root, contexts...)
	packets, err := RepairPlanReportPacketsContext(ctx, state)
	if err != nil {
		return nil, err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return nil, err
	}
	byID := map[string]workpacket.RepairPlanReportPacket{}
	for _, p := range packets {
		if err := verifyRegisteredRepairPlanReportPacketIssue(root, state, p, j, ctx); err != nil {
			return nil, err
		}
		byID[p.ID] = p
	}
	for _, e := range j.Events {
		id := stringValue(e["event_id"])
		if strings.HasPrefix(id, "evt-s9-issued-plan-report-") {
			if _, ok := byID[strings.TrimPrefix(id, "evt-s9-issued-plan-report-")]; !ok {
				return nil, fmt.Errorf("S9 orphan original acceptance event")
			}
		}
		prefix := "evt-repair-plan-report-packet-"
		if strings.HasPrefix(id, prefix) {
			p, ok := byID[strings.TrimPrefix(id, prefix)]
			if !ok || p.IssuanceEventID != id {
				return nil, fmt.Errorf("S9 orphan original issuance event")
			}
		}
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	for _, p := range packets {
		if pointer["session_id"] != p.Binding.SessionID {
			continue
		}
		if _, _, err := findRepairPlanReportReceipt(root, state, p, j); err != nil {
			return nil, err
		}
	}
	refs, err := repairPlanReportIndexedRefs(state)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		doc, err := readRepairPacketSource(root, stringValue(ref["path"]), stringValue(ref["sha256"]), "repair-plan-report.schema.json")
		if err != nil {
			return nil, err
		}
		if raw, has := doc["receipt"]; has {
			row, _ := raw.(map[string]any)
			p, ok := byID[stringValue(row["packet_id"])]
			if !ok || pointer["session_id"] != p.Binding.SessionID {
				return nil, fmt.Errorf("S9 original report has no exact issued Session packet")
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
		if row["template_id"] != repairPlanReportPacketTemplateID {
			filtered = append(filtered, raw)
		}
	}
	copyState := make(map[string]any, len(state))
	for k, v := range state {
		copyState[k] = v
	}
	copyEntities := make(map[string]any, len(entities))
	for k, v := range entities {
		copyEntities[k] = v
	}
	copyEntities["work_packets"] = filtered
	copyState["entities"] = copyEntities
	return copyState, nil
}
