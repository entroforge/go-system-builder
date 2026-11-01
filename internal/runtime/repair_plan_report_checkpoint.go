package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"strings"
)

// RepairPlanReportReceipt is stored only in the original immutable report.
// Report SHA comes from the original pointer; this pre-execution milestone
// never marks the Builder's complete responsibility as finished.
type RepairPlanReportReceipt struct {
	Contract string `json:"contract"`
	PacketID string `json:"packet_id"`

	SubmittedAnswer    json.RawMessage `json:"submitted_answer"`
	OwnerAgentID       string          `json:"owner_agent_id"`
	RuntimeID          string          `json:"runtime_id"`
	BaselineGeneration int             `json:"baseline_generation"`

	CaseID  string `json:"case_id"`
	CaseRef string `json:"case_ref"`

	ContractID  string `json:"contract_id"`
	ContractRef string `json:"contract_ref"`

	SessionID  string `json:"session_id"`
	SessionRef string `json:"session_ref"`

	PlanID  string `json:"plan_id"`
	PlanRef string `json:"plan_ref"`

	RepairAssignmentID string `json:"repair_assignment_id"`

	ManifestAssignmentID string `json:"manifest_assignment_id"`

	TeamID  string `json:"team_id"`
	TaskID  string `json:"task_id"`
	TaskRef string `json:"task_ref"`

	ManifestRef string `json:"manifest_ref"`

	ExecutionRoot                string `json:"execution_root"`
	ReportID                     string `json:"report_id"`
	ReportRef                    string `json:"report_ref"`
	WorkComplete                 bool   `json:"work_complete"`
	IssuanceEventID              string `json:"issuance_event_id"`
	AcceptedEventID              string `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int    `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int    `json:"accepted_sequence"`
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
	ctx := repairPacketContext(root, contexts...)
	p, found, err := FindRepairPlanReportPacketContext(ctx, state, packetID)
	if err != nil || !found {
		return RepairPlanReportReceipt{}, false, err
	}
	if owner == "" || owner != p.Binding.OwnerAgentID {
		return RepairPlanReportReceipt{}, false, fmt.Errorf("S9 receipt owner differs")
	}
	return findRepairPlanReportReceipt(root, state, p, controlproof.JournalInspection{})
}

func repairPlanReportIndexedRefs(state map[string]any) ([]map[string]any, error) {
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	refs := []map[string]any{}
	seen := map[string]bool{}
	add := func(row map[string]any) error {
		path := stringValue(row["path"])
		if path == "" {
			return fmt.Errorf("S9 original PlanReport index missing path")
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
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
		if err := add(map[string]any{"path": pointer["plan_report_ref"]}); err != nil {
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
		doc, err := readRepairPacketSource(root, stringValue(ref["path"]), "repair-plan-report.schema.json")
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
func verifyRepairPlanReportReceipt(_ string, _ map[string]any, p workpacket.RepairPlanReportPacket, _ controlproof.JournalInspection, doc map[string]any, path string, r RepairPlanReportReceipt) error {
	if r.PacketID != p.ID || r.OwnerAgentID != p.Binding.OwnerAgentID || r.RuntimeID != p.Binding.RuntimeID || r.ReportRef != path || r.RepairAssignmentID != p.Binding.RepairAssignmentID || r.WorkComplete {
		return fmt.Errorf("S9 receipt responsibility differs")
	}
	var answer map[string]any
	if json.Unmarshal(r.SubmittedAnswer, &answer) != nil {
		return fmt.Errorf("S9 original PlanReport answer unavailable")
	}
	if !hypothesisJSONEqual(doc["plan"], answer["plan"]) || !hypothesisJSONEqual(doc["red_checks"], answer["red_checks"]) || !hypothesisJSONEqual(doc["proposed_paths"], answer["proposed_paths"]) {
		return fmt.Errorf("S9 original PlanReport content differs")
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
		doc, err := readRepairPacketSource(root, stringValue(ref["path"]), "repair-plan-report.schema.json")
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
