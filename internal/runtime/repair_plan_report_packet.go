package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const repairPlanReportPacketTemplateID = "s9-plan-report-v1"

// Builders may be dispatched while a sibling is already fixing; the original
// issuer, current authority and registration/issue proofs share this scope.
func repairPlanReportDispatchPhase(phase any) bool {
	return phase == "planning" || phase == "reproducing" || phase == "fixing"
}

// RepairPlanReportPackets reads the existing packet registry. It does not
// grant permission to answer or derive a repair plan report from packet inputs.
func RepairPlanReportPackets(state map[string]any) ([]workpacket.RepairPlanReportPacket, error) {
	return RepairPlanReportPacketsContext(context.Background(), state)
}

func RepairPlanReportPacketsContext(ctx context.Context, state map[string]any) ([]workpacket.RepairPlanReportPacket, error) {
	return repairPlanReportPacketsMatchingContext(ctx, state, nil)
}

func FindRepairPlanReportPacket(state map[string]any, id string) (workpacket.RepairPlanReportPacket, bool, error) {
	return FindRepairPlanReportPacketContext(context.Background(), state, id)
}

func FindRepairPlanReportPacketContext(ctx context.Context, state map[string]any, id string) (workpacket.RepairPlanReportPacket, bool, error) {
	if id == "" {
		return workpacket.RepairPlanReportPacket{}, false, fmt.Errorf("S9 packet ID is required")
	}
	rows, err := repairPacketRowsContext(ctx, state, repairPlanReportPacketTemplateID)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, false, err
	}
	for _, row := range rows {
		if row["packet_id"] == id {
			packet, err := readRepairPlanReportRowContext(ctx, row)
			return packet, err == nil, err
		}
	}
	return workpacket.RepairPlanReportPacket{}, false, nil
}

// CurrentRepairPlanReportPackets selects current responsibilities, not write grants.
func CurrentRepairPlanReportPackets(root string, state map[string]any, owner string) ([]workpacket.RepairPlanReportPacket, error) {
	return CurrentRepairPlanReportPacketsContext(context.Background(), root, state, owner)
}

func CurrentRepairPlanReportPacketsContext(ctx context.Context, root string, state map[string]any, owner string) ([]workpacket.RepairPlanReportPacket, error) {
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	gen, _ := integerField(baseline, "generation")
	owners, _ := pointer["assignment_owners"].(map[string]any)
	selected, err := repairPlanReportPacketsMatchingContext(ctx, state, func(b workpacket.RepairPlanReportBinding) bool {
		return (owner == "" || b.OwnerAgentID == owner) && b.RuntimeID == state["runtime_id"] && b.BaselineGeneration == gen && pointer["session_id"] == b.SessionID && pointer["plan_ref"] == b.PlanRef && owners[b.RepairAssignmentID] == b.OwnerAgentID
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range selected {
		if seen[p.Binding.RepairAssignmentID] {
			return nil, fmt.Errorf("S9 current Assignment has multiple issued packets")
		}
		seen[p.Binding.RepairAssignmentID] = true
	}
	return selected, nil
}

func VerifyRepairPlanReportPacketIssue(root string, state map[string]any, p workpacket.RepairPlanReportPacket) error {
	return VerifyRepairPlanReportPacketIssueContext(context.Background(), root, state, p)
}
func VerifyRepairPlanReportPacketIssueContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket) error {
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return err
	}
	return verifyRepairPlanReportPacketIssue(root, state, p, j, ctx)
}
func verifyRepairPlanReportPacketIssue(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection, contexts ...context.Context) error {
	stored, found, err := FindRepairPlanReportPacketContext(repairPacketContext(root, contexts...), state, p.ID)
	if err != nil {
		return fmt.Errorf("S9 packet registered row: %w", err)
	}
	if !found || !reflect.DeepEqual(stored, p) {
		return fmt.Errorf("S9 packet is not exact registered row")
	}
	return verifyRegisteredRepairPlanReportPacketIssue(root, state, p, j, contexts...)
}

// verifyRegisteredRepairPlanReportPacketIssue is used only after this call's
// immutable state row has been decoded and canonically validated by Find.
// It avoids rebuilding that same registry twice, while retaining all original
// journal, approval and on-disk source checks at the issuance revision.
func verifyRegisteredRepairPlanReportPacketIssue(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection, contexts ...context.Context) error {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := controlproof.ValidateStateJournalPair(state, j); err != nil {
		return err
	}
	i, ok := j.EventIndex[p.IssuanceEventID]
	if !ok {
		return fmt.Errorf("S9 original issuance event missing")
	}
	e := j.Events[i]
	actor, _ := e["actor"].(map[string]any)
	from, _ := e["from"].(map[string]any)
	to, _ := e["to"].(map[string]any)
	gen, ge := integerField(e, "baseline_generation")
	before, be := integerField(e, "before_revision")
	after, ae := integerField(e, "after_revision")
	rev, re := integerField(state, "revision")
	if e["event"] != "transition_committed" || e["outcome"] != "committed" || e["transition_id"] != "PACKET-ISSUE" || e["idempotency_key"] != "packet:"+p.ID || e["runtime_id"] != p.Binding.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || ge != nil || gen != p.Binding.BaselineGeneration || be != nil || ae != nil || after != before+1 || re != nil || after > rev || from["state"] != "bug_resolution" || !repairPlanReportDispatchPhase(from["phase"]) || !reflect.DeepEqual(from, to) {
		return fmt.Errorf("S9 original issuance tuple differs")
	}
	return verifyRepairPlanReportSources(root, state, p, j, after, ctx)
}

// VerifyRepairPlanReportSources verifies the original immutable registered sources.
// issuedAfter is the candidate issuance revision when called before publication.
func VerifyRepairPlanReportSources(root string, state map[string]any, p workpacket.RepairPlanReportPacket, issuedAfter int, contexts ...context.Context) error {
	ctx := controlproof.WithJournalReadCache(repairPacketContext(root, contexts...))
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return err
	}
	if err := controlproof.ValidateStateJournalPair(state, j); err != nil {
		return err
	}
	return verifyRepairPlanReportSources(root, state, p, j, issuedAfter, ctx)
}

func readRepairPacketSource(root, ref, sha, schemaName string) (map[string]any, error) {
	data, err := workpacket.ReadFile(root, ref, 20<<20)
	if err != nil || workpacket.Digest(data) != sha {
		return nil, fmt.Errorf("S9 source %s bytes/SHA differ: %v", ref, err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	if schemaName != "" {
		if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
			return nil, err
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, fmt.Errorf("S9 source %s must be object", ref)
	}
	return doc, nil
}
func verifyRepairPlanReportSources(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection, issuedAfter int, contexts ...context.Context) error {
	ctx := repairPacketContext(root, contexts...)
	if err := ctx.Err(); err != nil {
		return err
	}
	b := p.Binding
	sources := []struct {
		ref, sha, schema string
		frozen           json.RawMessage
	}{
		{b.CaseRef, b.CaseSHA256, "review-investigation-case.schema.json", p.Inputs.Case},
		{b.ContractRef, b.ContractSHA256, "repair-contract.schema.json", p.Inputs.Contract},
		{b.SessionRef, b.SessionSHA256, "repair-session.schema.json", p.Inputs.Session},
		{b.PlanRef, b.PlanSHA256, "repair-plan.schema.json", p.Inputs.Plan},
		{b.TaskRef, b.TaskSHA256, "", p.Inputs.Task},
	}
	docs := make([]map[string]any, 0, len(sources))
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		d, err := readRepairPacketSource(root, s.ref, s.sha, s.schema)
		if err != nil {
			return err
		}
		var frozen map[string]any
		if json.Unmarshal(s.frozen, &frozen) != nil || !hypothesisJSONEqual(d, frozen) {
			return fmt.Errorf("S9 frozen document %s differs", s.ref)
		}
		docs = append(docs, d)
	}
	c, contract, session, plan, task := docs[0], docs[1], docs[2], docs[3], docs[4]
	if !hypothesisJSONEqual(c["baseline_generation"], b.BaselineGeneration) || !hypothesisExactStringSet(c["source_finding_ids"], stringValuesRepair(contract["source_finding_ids"])) {
		return fmt.Errorf("S9 original approved Finding/generation closure differs")
	}
	if c["case_id"] != b.CaseID || c["status"] != "contract_approved" || c["repair_contract_ref"] != b.ContractRef || c["repair_contract_sha256"] != b.ContractSHA256 || contract["repair_contract_id"] != b.ContractID || contract["case_id"] != b.CaseID || contract["status"] != "approved" || session["session_id"] != b.SessionID || session["contract_ref"] != b.ContractRef || session["contract_sha256"] != b.ContractSHA256 || session["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(session["baseline_generation"], b.BaselineGeneration) || plan["plan_id"] != b.PlanID || plan["session_id"] != b.SessionID || plan["contract_ref"] != b.ContractRef || plan["contract_sha256"] != b.ContractSHA256 || plan["contract_id"] != b.ContractID || task["task_id"] != b.TaskID || task["session_id"] != b.SessionID || task["plan_id"] != b.PlanID || task["assignment_id"] != b.RepairAssignmentID {
		return fmt.Errorf("S9 source identity tuple differs")
	}
	var assignment map[string]any
	count := 0
	for _, row := range objectRows(plan["assignments"]) {
		if row["assignment_id"] == b.RepairAssignmentID {
			assignment = row
			count++
		}
	}
	ab, _ := json.Marshal(assignment)
	var frozenAssignment map[string]any
	_ = json.Unmarshal(p.Inputs.RepairAssignment, &frozenAssignment)
	if count != 1 || workpacket.Digest(ab) != b.RepairAssignmentSHA256 || !hypothesisJSONEqual(assignment, frozenAssignment) || !hypothesisJSONEqual(plan["prospective_scope"], p.Inputs.ProspectiveScope) || !hypothesisJSONEqual(plan["forbidden_scope"], p.Inputs.ForbiddenScope) {
		return fmt.Errorf("S9 immutable RepairAssignment/scope differs")
	}
	for _, key := range []string{"unit_ids", "scope", "depends_on", "resource_locks"} {
		if !hypothesisJSONEqual(task[key], assignment[key]) {
			return fmt.Errorf("S9 TASK %s differs from RepairAssignment", key)
		}
	}
	manifest, err := readRepairPacketSource(root, b.ManifestRef, b.ManifestSHA256, "team-manifest.schema.json")
	if err != nil {
		return err
	}
	if manifest["runtime_id"] != b.RuntimeID || manifest["workgroup_id"] != b.TeamID || manifest["workgroup_kind"] != "builder" || !hypothesisJSONEqual(manifest["baseline_generation"], b.BaselineGeneration) || !hypothesisJSONEqual(manifest["review_round"], b.ReviewRound) {
		return fmt.Errorf("S9 original manifest identity differs")
	}
	for _, src := range []struct{ id, ref, sha string }{{"repair-contract", b.ContractRef, b.ContractSHA256}, {"repair-session", b.SessionRef, b.SessionSHA256}, {"repair-plan", b.PlanRef, b.PlanSHA256}} {
		n := 0
		for _, row := range objectRows(manifest["documents"]) {
			if row["id"] == src.id {
				n++
				if row["path"] != src.ref || row["sha256"] != src.sha {
					return fmt.Errorf("S9 manifest source %s differs", src.id)
				}
			}
		}
		if n != 1 {
			return fmt.Errorf("S9 manifest missing unique source %s", src.id)
		}
	}
	var ma map[string]any
	n := 0
	for _, row := range objectRows(manifest["assignments"]) {
		if row["assignment_id"] == b.ManifestAssignmentID {
			ma = row
			n++
		}
	}
	mb, _ := json.Marshal(ma)
	var frozenMA map[string]any
	_ = json.Unmarshal(p.Inputs.ManifestAssignment, &frozenMA)
	if n != 1 || workpacket.Digest(mb) != b.ManifestAssignmentSHA256 || !hypothesisJSONEqual(ma, frozenMA) || ma["agent_id"] != b.OwnerAgentID || (ma["role_family"] != "backend-builder" && ma["role_family"] != "frontend-builder" && ma["role_family"] != "test-builder") || !hypothesisJSONEqual(ma["scope"], assignment["scope"]) {
		return fmt.Errorf("S9 registered manifest Assignment differs")
	}
	entities, _ := state["entities"].(map[string]any)
	n = 0
	for _, row := range objectRows(entities["teams"]) {
		if row["id"] == b.TeamID {
			n++
			if row["kind"] != "builder" || row["manifest_ref"] != b.ManifestRef || row["manifest_sha256"] != b.ManifestSHA256 {
				return fmt.Errorf("S9 original Team manifest authority differs")
			}
		}
	}
	if n != 1 {
		return fmt.Errorf("S9 unique registered Team missing")
	}
	n = 0
	for _, row := range objectRows(entities["tasks"]) {
		if row["id"] == b.TaskID {
			n++
			if row["path"] != b.TaskRef || row["sha256"] != b.TaskSHA256 {
				return fmt.Errorf("S9 original TASK registry differs")
			}
		}
	}
	if n != 1 {
		return fmt.Errorf("S9 unique registered TASK missing")
	}
	n = 0
	for _, e := range j.Events {
		if e["transition_id"] != "ENTITY-REGISTER" {
			continue
		}
		after, err := integerField(e, "after_revision")
		if err != nil || after >= issuedAfter {
			continue
		}
		refs, _ := checkpointStringSlice(e["evidence_ids"])
		if !hypothesisStringContains(refs, b.ManifestRef) || !hypothesisStringContains(refs, b.TaskRef) {
			continue
		}
		before, be := integerField(e, "before_revision")
		gen, ge := integerField(e, "baseline_generation")
		actor, _ := e["actor"].(map[string]any)
		from, _ := e["from"].(map[string]any)
		to, _ := e["to"].(map[string]any)
		if be != nil || after != before+1 || ge != nil || gen != b.BaselineGeneration || e["runtime_id"] != b.RuntimeID || actor["type"] != "orchestrator" || actor["id"] != "orchestrator" || e["event_id"] != fmt.Sprintf("evt-register-%s-r%d", b.TeamID, after) || e["idempotency_key"] != fmt.Sprintf("runtime:register:%s:%d", stringValue(manifest["manifest_id"]), before) || from["state"] != "bug_resolution" || !repairPlanReportDispatchPhase(from["phase"]) || !hypothesisJSONEqual(from, to) {
			return fmt.Errorf("S9 original registration event tuple differs")
		}
		n++
	}
	if n != 1 {
		return fmt.Errorf("S9 original manifest/TASK registration missing or ambiguous")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyRepairPlanReportAuthorityChain(root, state, p, j, issuedAfter, contract); err != nil {
		return err
	}
	return ctx.Err()
}

// VerifyRepairPlanReportCurrent checks fresh source and dispatch authority only.
// The original consumer separately repeats trusted host/session and PLAN checks.
func VerifyRepairPlanReportCurrent(root string, state map[string]any, p workpacket.RepairPlanReportPacket, contexts ...context.Context) error {
	ctx := controlproof.WithJournalReadCache(repairPacketContext(root, contexts...))
	if err := ctx.Err(); err != nil {
		return err
	}
	b := p.Binding
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	cp, _ := review["investigation"].(map[string]any)
	base, _ := state["baseline"].(map[string]any)
	life, _ := state["lifecycle"].(map[string]any)
	if !PacketEnabled(state) || state["runtime_id"] != b.RuntimeID || !hypothesisJSONEqual(base["generation"], b.BaselineGeneration) || !hypothesisJSONEqual(review["round"], b.ReviewRound) || life["state"] != "bug_resolution" || !repairPlanReportDispatchPhase(life["phase"]) || (pointer["status"] != "planning" && pointer["status"] != "reproducing" && pointer["status"] != "repairing") || pointer["session_id"] != b.SessionID || pointer["path"] != b.SessionRef || pointer["sha256"] != b.SessionSHA256 || pointer["plan_ref"] != b.PlanRef || pointer["plan_sha256"] != b.PlanSHA256 || pointer["contract_ref"] != b.ContractRef || pointer["contract_sha256"] != b.ContractSHA256 || pointer["case_id"] != b.CaseID || cp["case_id"] != b.CaseID || cp["path"] != b.CaseRef || cp["sha256"] != b.CaseSHA256 || cp["status"] != "contract_approved" {
		return fmt.Errorf("S9 fresh source/phase authority changed; recover by named dispatch")
	}
	owners, _ := pointer["assignment_owners"].(map[string]any)
	if owners[b.RepairAssignmentID] != b.OwnerAgentID {
		return fmt.Errorf("S9 original RepairAssignment owner differs")
	}
	entities, _ := state["entities"].(map[string]any)
	var manifestAssignment map[string]any
	if json.Unmarshal(p.Inputs.ManifestAssignment, &manifestAssignment) != nil {
		return fmt.Errorf("S9 issued manifest Assignment invalid")
	}
	n := 0
	for _, row := range objectRows(entities["agents"]) {
		if row["id"] != b.OwnerAgentID {
			continue
		}
		tasks, _ := checkpointStringSlice(row["task_ids"])
		if row["role"] == manifestAssignment["role_family"] && row["team_id"] == b.TeamID && row["prompt_ref"] == b.ManifestRef+"#"+b.ManifestAssignmentID && hypothesisStringContains(tasks, b.TaskID) {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("S9 original Agent registration differs")
	}
	data, err := controlproof.ReadJournalForStateContext(ctx, root, state)
	if err != nil {
		return err
	}
	j, err := controlproof.InspectJournalSegmentDataFromContext(ctx, data, 1)
	if err != nil {
		return err
	}
	if i, ok := j.EventIndex[p.IssuanceEventID]; ok {
		for _, e := range j.Events[i+1:] {
			if e["transition_id"] == "S9-SESSION-OPEN" || e["transition_id"] == "PTR-BUG-09" {
				return fmt.Errorf("S9 original Session/Plan changed after issuance; named recovery required")
			}
		}
	}
	return ctx.Err()
}

// verifyRepairPlanReportAuthorityChain consumes the original approval/session/plan
// facts. It verifies their retained proof, never re-runs or creates an approval.
func verifyRepairPlanReportAuthorityChain(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection, issuedAfter int, contract map[string]any) error {
	b := p.Binding
	approvalID := stringValue(contract["approval_evidence_id"])
	if approvalID == "" {
		return fmt.Errorf("S9 original approved Contract lacks approval evidence")
	}
	authority, _ := contract["delegated_authority"].(map[string]any)
	delegationID := stringValue(authority["delegation_evidence_id"])
	approvalAfter, sessionAfter, planAfter, registrationAfter := 0, 0, 0, 0
	for _, e := range j.Events {
		after, err := integerField(e, "after_revision")
		if err != nil || after >= issuedAfter {
			continue
		}
		refs, _ := checkpointStringSlice(e["evidence_ids"])
		gen, ge := integerField(e, "baseline_generation")
		if e["runtime_id"] != b.RuntimeID || ge != nil || gen != b.BaselineGeneration {
			continue
		}
		if e["event"] != "transition_committed" || e["outcome"] != "committed" {
			continue
		}
		switch e["transition_id"] {
		case "ENTITY-REGISTER":
			if hypothesisStringContains(refs, b.ManifestRef) && hypothesisStringContains(refs, b.TaskRef) {
				registrationAfter = after
			}
		case "S8-REPAIR-CONTRACT-APPROVAL":
			if !hypothesisStringContains(refs, b.ContractID) || !hypothesisStringContains(refs, b.CaseID) {
				continue
			}
			before, be := integerField(e, "before_revision")
			actor, _ := e["actor"].(map[string]any)
			to, _ := e["to"].(map[string]any)
			from, _ := e["from"].(map[string]any)
			if from["state"] != "bug_resolution" || approvalAfter != 0 || be != nil || after != before+1 || e["event_id"] != fmt.Sprintf("evt-repair-contract-approved-%s-r%d", b.ContractID, after) || e["idempotency_key"] != fmt.Sprintf("runtime:investigation-contract-approve:%s:%d", b.ContractID, before) || actor["id"] != "orchestrator" || actor["type"] != "orchestrator" || to["state"] != "bug_resolution" || to["phase"] != "repair_readback" || !hypothesisStringContains(refs, approvalID) || (delegationID != "" && !hypothesisStringContains(refs, delegationID)) {
				return fmt.Errorf("S9 original approval event differs")
			}
			approvalAfter = after
		case "S9-SESSION-OPEN":
			if !hypothesisStringContains(refs, b.SessionID) {
				continue
			}
			before, be := integerField(e, "before_revision")
			from, _ := e["from"].(map[string]any)
			to, _ := e["to"].(map[string]any)
			actor, _ := e["actor"].(map[string]any)
			if from["state"] != "bug_resolution" || !hypothesisJSONEqual(from, to) || actor["type"] != actorType(stringValue(actor["id"])) || sessionAfter != 0 || be != nil || after != before+1 || e["event_id"] != fmt.Sprintf("evt-s9-session-open-%s-r%d", b.SessionID, after) || e["idempotency_key"] != fmt.Sprintf("runtime:s9:session-open:%s:%d", b.SessionID, before) || !hypothesisStringContains(refs, b.ContractID) {
				return fmt.Errorf("S9 original Session open event differs")
			}
			sessionAfter = after
		case "PTR-BUG-09":
			if !hypothesisStringContains(refs, b.PlanID) {
				continue
			}
			before, be := integerField(e, "before_revision")
			from, _ := e["from"].(map[string]any)
			to, _ := e["to"].(map[string]any)
			actor, _ := e["actor"].(map[string]any)
			if from["state"] != "bug_resolution" || to["state"] != "bug_resolution" || to["phase"] != "planning" || actor["type"] != actorType(stringValue(actor["id"])) || planAfter != 0 || be != nil || after != before+1 || e["event_id"] != fmt.Sprintf("evt-s9-plan-%s-r%d", b.PlanID, after) || e["idempotency_key"] != fmt.Sprintf("runtime:s9:plan:%s:%d", b.PlanID, before) {
				return fmt.Errorf("S9 original Plan compile event differs")
			}
			planAfter = after
		}
	}
	if approvalAfter == 0 || sessionAfter <= approvalAfter || planAfter <= sessionAfter || registrationAfter <= planAfter {
		return fmt.Errorf("S9 original approval→Session→Plan chain missing")
	}
	evidence := func(id, kind, sha string) (map[string]any, []byte, error) {
		var row map[string]any
		for _, r := range objectRows(state["evidence"]) {
			if r["id"] == id {
				if row != nil {
					return nil, nil, fmt.Errorf("S9 duplicate approval evidence")
				}
				row = r
			}
		}
		if row == nil || row["kind"] != kind || (sha != "" && row["sha256"] != sha) {
			return nil, nil, fmt.Errorf("S9 original approval evidence tuple differs")
		}
		data, err := workpacket.ReadFile(root, stringValue(row["path"]), 20<<20)
		if err != nil || workpacket.Digest(data) != row["sha256"] {
			return nil, nil, fmt.Errorf("S9 original approval evidence bytes/SHA differ")
		}
		return row, data, nil
	}
	if delegationID == "" {
		row, data, err := evidence(approvalID, "human_decision", "")
		if err != nil {
			return err
		}
		if row["consumed_by"] != "S8-REPAIR-CONTRACT-APPROVAL" || row["consumed_at"] != contract["approved_at"] {
			return fmt.Errorf("S9 original human approval consumption missing")
		}
		scopes, _ := checkpointStringSlice(row["scope_refs"])
		semanticScope := "s8_contract_approval:" + b.RuntimeID
		if !hypothesisStringContains(scopes, semanticScope) && !hypothesisStringContains(scopes, fmt.Sprintf("%s@%d", semanticScope, approvalAfter-1)) {
			return fmt.Errorf("S9 original human approval scope differs")
		}
		producers, _ := checkpointStringSlice(row["produced_by"])
		if !hypothesisStringContains(producers, stringValue(contract["approved_by"])) {
			return fmt.Errorf("S9 original human approval producer differs")
		}
		if strings.HasSuffix(stringValue(row["path"]), ".json") {
			if err := workpacket.ValidateUniqueJSON(data); err != nil {
				return err
			}
			var a map[string]any
			if json.Unmarshal(data, &a) != nil || a["decision"] != "approve_contract" || a["decision_id"] != approvalID || a["runtime_id"] != b.RuntimeID || a["case_id"] != b.CaseID || a["contract_id"] != b.ContractID || a["approval_hash"] != contract["approval_hash"] {
				return fmt.Errorf("S9 original scoped human decision differs")
			}
		}
		return nil
	}
	config, _ := state["configuration"].(map[string]any)
	repair, _ := config["repair"].(map[string]any)
	uses, _ := repair["delegation_uses"].(map[string]any)
	count, err := integerField(uses, delegationID)
	if err != nil || count < 1 {
		return fmt.Errorf("S9 original delegation use was not consumed")
	}
	_, data, err := evidence(approvalID, "repair_contract_review", stringValue(authority["review_sha256"]))
	if err != nil {
		return err
	}
	if authority["review_evidence_id"] != approvalID {
		return fmt.Errorf("S9 original delegated review differs")
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var a map[string]any
	if json.Unmarshal(data, &a) != nil || a["decision"] != "approve_bounded_repair" || a["runtime_id"] != b.RuntimeID || a["case_id"] != b.CaseID || a["contract_id"] != b.ContractID || a["approval_hash"] != contract["approval_hash"] || a["delegation_evidence_id"] != delegationID {
		return fmt.Errorf("S9 original delegated review binding differs")
	}
	if strings.HasPrefix(delegationID, "binding-policy:") {
		if delegationID != "binding-policy:"+stringValue(authority["delegation_sha256"]) {
			return fmt.Errorf("S9 original approved binding policy digest differs")
		}
	} else {
		if _, _, err := evidence(delegationID, "human_decision", stringValue(authority["delegation_sha256"])); err != nil {
			return err
		}
	}
	return nil
}
