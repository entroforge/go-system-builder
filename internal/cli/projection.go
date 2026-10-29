package cli

import (
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/change"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// stageContract is the executable projection contract consumed by status and
// next. It intentionally describes outcomes, not another state machine: the
// Loop Definition remains the authority for legal transitions.
type stageContract struct {
	Objective string
	Read      []string
	Missing   []string
	DoneWhen  []string
}

// PrimarySkill is the single source of truth for the S7 verification round's
// `primary_skill` projection value (RC-12 FL-4). `projectNext` (run.go), the
// S7 budget gateway, and docs/control/agent-protocol.md §S7 must all name the same
// Methodology Skill; controller.go projects this value verbatim into
// guidance and the recovery read order (.claude/skills/<skill>/SKILL.md).
// Focus-specific DV/QA/E2E Skills are per-Assignment dispatch facts, not the
// round's primary_skill.
const PrimarySkillS7 = "loop-orchestration"

var projectionContracts = map[string]stageContract{
	"S0":                 {"produce one human-locked requirement (draft via requirement-funnel; human explicitly locks, Main records locked status and the decision; save through an editor, script or copy tool; binding is the S1 action)", []string{"docs/requirements/"}, []string{"human_locked_req"}, []string{"the current requirement is complete and the human's actual lock of that version is recorded"}},
	"S1":                 {"bind the selected locked requirement using the actual startup decision and explicit project branch destinations", []string{"docs/requirements/", "docs/control/agent-protocol.md#s1"}, []string{"current_binding_choices"}, []string{"the original TR-001 records the selected committed requirement, actual startup decision and branch destinations, and supplies the current S2 responsibility"}},
	"S2":                 {"complete architecture and any required UI design package", []string{"bound REQ", projectlayout.Architecture + "/", "docs/design/", "docs/rules/"}, []string{"architecture_record"}, []string{"architecture decisions cover the contract boundary", "any UI-impacting module has a complete target design package"}},
	"S3":                 {"complete the development contract set", []string{"bound REQ", projectlayout.Architecture + "/", "docs/design/", "docs/dev/contracts/"}, []string{"locked_contract_set"}, []string{"the current bound REQ applicable locked contract batch and shared sources are accepted"}},
	"S4":                 {"complete an executable TASK batch", []string{"bound REQ", "docs/dev/contracts/", "docs/dev/tasks/"}, []string{"complete_task_batch"}, []string{"the complete current TASK batch is accepted, with clause coverage and executable dependencies"}},
	"S5":                 {"independently verify and atomically lock the specification chain", []string{"bound REQ", projectlayout.Architecture + "/", "docs/design/", "docs/dev/contracts/", "docs/dev/tasks/"}, []string{"joint_document_pass"}, []string{"document-verification responsibilities pass with current fingerprints"}},
	"S6":                 {"implement the locked TASK batch", []string{"bound REQ", "locked contracts", "locked TASKs", "docs/control/agent-protocol.md#s6"}, []string{"builder_completion_reports", "verified_integration_checkpoints"}, []string{"every TASK in the TR-003 batch has a Builder Result with passing checks and no unapproved scope deviations, saved at the exact Result path supplied by the listener for the original Result consumer; Main verifies the required integration checkpoints"}},
	"S7":                 {"complete one current full verification round", []string{"bound REQ", "locked specification chain", "Builder evidence", "docs/control/agent-protocol.md#s7"}, []string{"review_plan"}, []string{"every required Claim of the registered ReviewPlan has a consumed pass Result; findings seal into the ObservationBatch (TR-008), otherwise the machine CleanRound closes the round (TR-009)"}},
	"S8":                 {"investigate the current reported problem and actual observations to form an evidence-backed InvestigationCase disposition", []string{"actual current source: sealed S7 observations or the human-reported problem", "locked specification chain", "implementation"}, []string{"investigation_case", "causal_model_or_route"}, []string{"the current reported problem has a Case disposition; actual Findings, when present, are covered by the Case route; every s9_repair Case has an approved RepairContract"}},
	"S9":                 {"execute approved RepairContracts and target-reverify them", []string{"approved RepairContract", "locked specification chain", "implementation"}, []string{"targeted_reverification"}, []string{"repair evidence is current and the Contract assertions pass"}},
	"S10":                {"complete acceptance and release audit", []string{"bound REQ", "current clean round", "docs/reports/acceptance/ACC-template.md", projectlayout.ReleaseAudits + "/TEMPLATE.md", "docs/rules/release-architecture-audit.md", ".claude/skills/acceptance-and-handoff/SKILL.md"}, []string{"acceptance_record", "release_audit"}, []string{"the current Manifest explains actual requirement dispositions, applicable observations and counterevidence", "unknowns and blocking findings are resolved; remaining risks have actual owners and dispositions", "S9 changes have returned through a fresh S7 clean round; no S9→S10 shortcut"}},
	"S11":                {"present the release-ready package to the human and record one explicit decision", []string{"acceptance record", "release audit", "release-ready package"}, []string{"human_decision"}, []string{"one explicit S11 decision is recorded or the Gateway remains awaiting a decision"}},
	"release_authorized": {"S11 human-authorized terminal", []string{"human decision record"}, []string{}, []string{"human authorization is recorded; Harness performs no merge, publication, deployment, or formal release"}},
	"aborted":            {"aborted terminal Runtime", []string{"human decision record"}, []string{}, []string{"automation remains stopped and only an eligible human-authorized rollover may start a new Runtime"}},
	"paused":             {"resolve the pause via one of the three human-gated exits: `runtime resume` (baselines unchanged) / `req amend` (drifted baseline, new REQ version) / `runtime human-decision --disposition abort` (abandon)", []string{"runtime pause checkpoint", "recorded blockers"}, []string{"pause_resolution"}, []string{"the blocking condition is resolved or a human chooses the next route"}},
	"cross-stage":        {"recover a valid runtime cursor", []string{".claude/loop-state.json", projectlayout.Definition}, []string{"valid_runtime_cursor"}, []string{"runtime lifecycle and phase map to one declared stage"}},
}

type statusProjection struct {
	RuntimeID    any              `json:"runtime_id"`
	Revision     any              `json:"revision"`
	BoundREQ     any              `json:"bound_req"`
	Stage        string           `json:"stage"`
	Lifecycle    string           `json:"lifecycle"`
	Phase        any              `json:"phase"`
	Objective    string           `json:"objective"`
	Completed    []string         `json:"completed"`
	OpenItems    []string         `json:"open_items"`
	ActiveWork   []map[string]any `json:"active_work"`
	HumanGateway any              `json:"human_gateway"`
	Change       *change.Summary  `json:"change,omitempty"`
}

type nextProjection struct {
	Stage         string            `json:"stage"`
	ProtocolRef   string            `json:"protocol_ref"`
	Objective     string            `json:"objective"`
	Action        string            `json:"action"`
	Read          []string          `json:"read"`
	PrimarySkill  string            `json:"primary_skill"`
	Missing       []string          `json:"missing"`
	DoneWhen      []string          `json:"done_when"`
	Then          string            `json:"then"`
	HumanRequired bool              `json:"human_required"`
	ChangeID      string            `json:"change_id,omitempty"`
	WorkItemID    string            `json:"work_item_id,omitempty"`
	CheckIDs      []string          `json:"check_ids,omitempty"`
	mainPlanning  *mainPlanningNext `json:"-"`
}

func buildStatusProjection(state map[string]any, stage, lifecycle string, phase any, root string) statusProjection {
	contract := contractFor(stage, state, root)
	projection := statusProjection{
		RuntimeID: state["runtime_id"], Revision: state["revision"], BoundREQ: state["bound_req"],
		Stage: stage, Lifecycle: lifecycle, Phase: phase, Objective: contract.Objective,
		Completed: completedStages(stage), OpenItems: contract.Missing,
		ActiveWork: activeRuntimeWork(state), HumanGateway: projectedGateway(state, stage, root),
	}
	if record, ok := projectedChange(state); ok {
		summary := change.Summarize(record)
		projection.Change = &summary
		projection.OpenItems = changeOpenItems(record)
	}
	return projection
}

func buildNextProjection(state map[string]any, stage, skill, action, root string, mainViewer ...bool) nextProjection {
	contract := contractFor(stage, state, root)
	projection := nextProjection{
		Stage: stage, ProtocolRef: protocolReference(stage), Objective: contract.Objective,
		Action: action, Read: resolveBoundREQRead(contract.Read, state), PrimarySkill: skill,
		Missing: contract.Missing, DoneWhen: contract.DoneWhen, Then: "recompute",
		HumanRequired: stage == "S11" || stage == "paused" || stage == "aborted",
	}
	if stage == "S6" && len(nativeS6Records(state)) > 0 {
		projection.Action = nativeS6MainGuidance(root, state)
		projection.DoneWhen = []string{
			"each assigned Builder saves its own professional plan, implements its reviewed TASK, runs real self-checks and returns the Result through native SubagentStop",
			"Main explicitly integrates the original delivery, runs required Main checks, records ACK and completes cleanup; the full reviewed batch Gate remains required",
		}
	}
	showMainPlanning := len(mainViewer) == 0 || mainViewer[0]
	if showMainPlanning {
		if planning := mainPlanningNextForState(state, stage); planning != nil {
			projection.Action = planning.Action
			projection.mainPlanning = planning
		}
	}
	if record, ok := projectedChange(state); ok {
		next := change.NextStep(record)
		projection.ChangeID = record.ID
		projection.WorkItemID = next.WorkItemID
		projection.CheckIDs = nonEmptyIDs(record)
		projection.Action = next.Action
		projection.Missing = changeOpenItems(record)
		projection.DoneWhen = []string{"all Change Record work items are done", "all required checks are passed or evidence-backed N/A"}
	}
	applyS7BudgetGateway(&projection, state, root)
	return projection
}

func projectedChange(state map[string]any) (change.Record, bool) {
	raw, ok := state["change"].(map[string]any)
	if !ok || raw == nil {
		return change.Record{}, false
	}
	record, err := change.Decode(raw)
	if err != nil || record.ID == "" {
		return change.Record{}, false
	}
	return record, true
}

func changeOpenItems(record change.Record) []string {
	items := make([]string, 0)
	for _, work := range record.WorkItems {
		if work.Status == "pending" || work.Status == "open" || work.Status == "active" || work.Status == "blocked" {
			items = append(items, work.ID+": "+work.Text)
		}
	}
	for _, check := range record.RequiredChecks {
		if check.Status == "open" || check.Status == "invalid" || check.Status == "failed" {
			items = append(items, check.ID+": "+check.Kind)
		}
	}
	return items
}

func nonEmptyIDs(record change.Record) []string {
	ids := make([]string, 0, len(record.RequiredChecks))
	for _, check := range record.RequiredChecks {
		if check.Status == "open" || check.Status == "invalid" || check.Status == "failed" {
			ids = append(ids, check.ID)
		}
	}
	return ids
}

func contractFor(stage string, state map[string]any, root string) stageContract {
	contract, ok := projectionContracts[stage]
	if !ok {
		contract = projectionContracts["cross-stage"]
	}
	contract.Read = cloneStrings(contract.Read)
	contract.Missing = cloneStrings(contract.Missing)
	contract.DoneWhen = cloneStrings(contract.DoneWhen)
	switch stage {
	case "S2":
		// Keep the projection scoped to S2. An architecture artifact on disk
		// does not prove that the current design was accepted, and it cannot
		// imply the S3 contract set or S4 task batch.
		if currentDesignReceived(state) {
			// Reception is authoritative even when its original stage outlet
			// still needs work. The questionnaire feedback names that work.
			contract.Missing = nil
		}
		if boundREQHasUIImpact(state) {
			if err := inspectUIDesignPackageFilesForREQ(root, boundREQPathFromState(state)); err != nil {
				contract.Missing = appendUnique(contract.Missing, "ui_design_package: "+err.Error())
			}
		}
	case "S3":
		// Stay inside the current stage until its quality gate commits the
		// advance. A filename (even a locked one) is not current REQ evidence.
		contract.Missing = []string{"locked_contract_set", "planning_contract_record"}
		if row, err := registeredPlanningRow(state); err == nil && row != nil {
			if record, found, err := runtime.FindSubmission(state, stringValue(row["packet_id"])); err == nil && found && record.Owner == "Main" && record.OriginState == "planning" && record.OriginPhase == "contracts" && record.OriginEntry == tolerantInt(mapFieldCLI(state, "lifecycle")["phase_revision"]) {
				contract.Missing = nil
			}
		}
	case "S4":
		switch {
		case !hasMarkdownArtifact(root, projectlayout.Contracts, "CONTRACTS-*.md", "locked"):
			contract.Missing = []string{"locked_contract_set"}
		case !hasMarkdownArtifact(root, projectlayout.Tasks, "TASK-*.md", "complete"):
			contract.Missing = []string{"complete_task_batch"}
		default:
			contract.Missing = []string{"planning_ready_transition"}
		}
	case "S7":
		contract.Missing = []string{verificationMissingItem(state)}
	case "S8":
		contract.Missing = []string{investigationMissingItem(state)}
	case "S9":
		contract.Missing = []string{repairMissingItem(state)}
	case "S10":
		switch lifecycleState(state) {
		case "acceptance":
			contract.Missing = []string{"current_acceptance_manifest"}
		case "release_audit":
			contract.Missing = []string{"current_release_audit_manifest"}
		}
	case "S11":
		if lifecycleState(state) == "awaiting_human_release" {
			contract.Missing = []string{"human_decision: approve | defer | reject_defect | reject_acceptance | reject_release_audit | abort"}
		} else {
			contract.Missing = []string{}
		}
	}
	return contract
}

// currentDesignReceived reads only the current responsibility and reception
// facts. A document on disk or an older stage's accepted answer is insufficient.
func currentDesignReceived(state map[string]any) bool {
	if !runtime.PacketEnabled(state) {
		return false
	}
	baseline, _ := state["baseline"].(map[string]any)
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "planning" || lifecycle["phase"] != "design" {
		return false
	}
	binding := workpacket.PlanningBinding{
		RuntimeID:  stringValue(state["runtime_id"]),
		Generation: integerValue(baseline["generation"]),
		StageEntry: integerValue(lifecycle["phase_revision"]),
	}
	packet, issued, conflict := currentDesignPlanningPacket(state, workpacket.DesignPacket{Binding: binding})
	if !issued || conflict {
		return false
	}
	record, found, err := runtime.FindSubmission(state, packet.ID)
	return err == nil && found && record.PacketID == packet.ID && record.OriginState == "planning" && record.OriginPhase == "design" && record.OriginEntry == binding.StageEntry
}

func cloneStrings(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func boundREQHasUIImpact(state map[string]any) bool {
	bound, _ := state["bound_req"].(map[string]any)
	metadata, _ := bound["metadata"].(map[string]any)
	impact, _ := metadata["ui_impact"].(string)
	return impact == "changed"
}

func lifecycleState(state map[string]any) string {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	value, _ := lifecycle["state"].(string)
	return value
}

func lifecyclePhase(state map[string]any) string {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	value, _ := lifecycle["phase"].(string)
	return value
}

// verificationMissingItem names the single missing S7 fact for the
// projection. The ReviewPlan status drives the token: no plan -> register
// one; running/draining -> consume the pending Claim results; sealed ->
// TR-008; clean -> TR-009 (L3-S7 §11.1).
func verificationMissingItem(state map[string]any) string {
	reviewMap, _ := state["review"].(map[string]any)
	plan, _ := reviewMap["plan"].(map[string]any)
	if plan == nil {
		return "review_plan"
	}
	switch status, _ := plan["status"].(string); status {
	case "running", "cannot_clean", "discovery_draining":
		return "claim_results"
	case "observation_sealed":
		return "tr008_observation_handoff"
	case "clean":
		return "tr009_acceptance_transition"
	case "paused":
		return "pause_resolution"
	default:
		return "review_plan"
	}
}

func investigationMissingItem(state map[string]any) string {
	if lifecyclePhase(state) == "investigation" {
		return "root_cause_evidence"
	}
	return "finding_dispositions"
}

func repairMissingItem(state map[string]any) string {
	switch lifecyclePhase(state) {
	case "repair_readback":
		return "approved_repair_readback"
	case "fixing":
		return "repair_completion_report"
	case "targeted_reverification":
		return "targeted_reverification"
	case "ready_for_full_review":
		return "full_review_transition"
	default:
		return "repair_checkpoint"
	}
}

func hasMarkdownArtifact(root, dir, pattern, requiredStatus string) bool {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		matched, matchErr := filepath.Match(pattern, entry.Name())
		if entry.IsDir() || matchErr != nil || !matched {
			continue
		}
		if requiredStatus == "" {
			return true
		}
		data, readErr := os.ReadFile(filepath.Join(root, dir, entry.Name()))
		if readErr == nil && strings.EqualFold(readProjectionStatus(string(data)), requiredStatus) {
			return true
		}
	}
	return false
}

func readProjectionStatus(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, ">") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		for _, separator := range []string{"：", ":"} {
			parts := strings.SplitN(line, separator, 2)
			if len(parts) == 2 && (strings.EqualFold(strings.TrimSpace(parts[0]), "status") || strings.TrimSpace(parts[0]) == "状态") {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func resolveBoundREQRead(read []string, state map[string]any) []string {
	reqPath := "bound REQ"
	if req, ok := state["bound_req"].(map[string]any); ok {
		if path, ok := req["path"].(string); ok && path != "" {
			reqPath = path
		}
	}
	result := make([]string, 0, len(read))
	for _, ref := range read {
		if ref == "bound REQ" {
			result = append(result, reqPath)
			continue
		}
		kind := map[string]string{"locked contracts": "contract", "locked TASKs": "task"}[ref]
		if kind != "" {
			if paths := currentLockedReadPaths(state, kind); len(paths) > 0 {
				result = append(result, paths...)
				continue
			}
		}
		result = append(result, ref)
	}
	return result
}

// Read categories are diagnostic projections of original document rows. They
// grant no source authority and never infer registration from directory files.
func currentLockedReadPaths(state map[string]any, kind string) []string {
	baseline, _ := state["baseline"].(map[string]any)
	generation := tolerantInt(baseline["generation"])
	rows, _ := state["documents"].([]any)
	seen := map[string]bool{}
	var paths []string
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		path, _ := row["path"].(string)
		if row["kind"] != kind || row["status"] != "locked" || tolerantInt(row["generation"]) != generation || path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func protocolReference(stage string) string {
	if len(stage) > 1 && stage[0] == 'S' {
		return "docs/control/agent-protocol.md#" + strings.ToLower(stage)
	}
	return "docs/control/agent-protocol.md#cursor-mapping"
}

func completedStages(stage string) []string {
	known := map[string][]string{
		"S0":      {},
		"S1":      {"S0"},
		"S2":      {"S0", "S1"},
		"S3":      {"S0", "S1", "S2"},
		"S4":      {"S0", "S1", "S2", "S3"},
		"S5":      {"S0", "S1", "S2", "S3", "S4"},
		"S6":      {"S0", "S1", "S2", "S3", "S4", "S5"},
		"S7":      {"S0", "S1", "S2", "S3", "S4", "S5", "S6"},
		"S8":      {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7"},
		"S9":      {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8"},
		"S10":     {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7"},
		"S11":     {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7", "S10"},
		"aborted": {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7", "S10"},
	}
	items := known[stage]
	completed := make([]string, len(items))
	copy(completed, items)
	return completed
}

func activeRuntimeWork(state map[string]any) []map[string]any {
	entities, _ := state["entities"].(map[string]any)
	active := make([]map[string]any, 0)
	terminal := map[string]bool{"done": true, "completed": true, "stopped": true, "cancelled": true, "rejected": true, "duplicate": true, "closed": true}
	for _, kind := range []string{"agents", "tasks", "bugs", "teams"} {
		items, _ := entities[kind].([]any)
		for _, item := range items {
			entity, _ := item.(map[string]any)
			entityState, _ := entity["state"].(string)
			if entityState == "" {
				// Teams use `status` while agents/tasks/bugs use `state`.
				// Project both shapes through the status schema's single
				// active-work state field.
				entityState, _ = entity["status"].(string)
			}
			if entity == nil || terminal[entityState] {
				continue
			}
			active = append(active, map[string]any{"entity_type": strings.TrimSuffix(kind, "s"), "id": entity["id"], "state": entityState})
		}
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i]["entity_type"].(string)+stringValue(active[i]["id"]) < active[j]["entity_type"].(string)+stringValue(active[j]["id"])
	})
	return active
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func projectedGateway(state map[string]any, stage string, roots ...string) any {
	if s7BudgetGateRequired(state, roots...) {
		return map[string]any{
			"type":            "s7_budget_gateway",
			"human_required":  true,
			"decision_fields": []string{"decision", "new_max_full_review_rounds (only an actual increase)", "reason"},
			"decisions":       []string{"increase_budget", "return_to_governance"},
			"guidance":        "Use the exact prepared original budget application. Main records only the human's actual decision and reason, plus a genuinely higher maximum for increase_budget; save to invoke the original Writer and fixed successor. Do not copy Runtime metadata, author evidence or supply an actor command. An empty choice does not authorize another round.",
		}
	}
	switch lifecycleState(state) {
	case "awaiting_human_release":
		return map[string]any{
			"type":           "human_release_gateway",
			"human_required": true,
			"dispositions":   []string{"approve", "defer", "reject_defect", "reject_acceptance", "reject_release_audit", "abort"},
			"guidance":       "Read the current S10 materials and exact application path supplied by the original stage entry. After the human explicitly chooses, Main fills only disposition and optional reason and saves that application; the original Writer records and consumes the decision and follows its fixed exit. Empty disposition keeps waiting. System metadata and evidence registration are automatic; reject_defect hands the actual human problem to S8 without requiring a Finding. Approval records authorization and does not publish or deploy.",
		}
	case "release_authorized":
		return map[string]any{
			"type":           "release_authorized",
			"human_required": false,
			"terminal":       true,
			"guidance":       "S11 human-authorized terminal; Harness has no squash merge, publication, deployment, or formal release permission",
		}
	case "aborted":
		return map[string]any{
			"type":           "aborted",
			"human_required": false,
			"terminal":       true,
			"blocked":        true,
			"guidance":       "aborted terminal; stop automation and use only an eligible human-authorized rollover for a new Runtime",
		}
	}
	if stage == "S11" {
		return map[string]any{"type": "human_release_gateway", "human_required": true}
	}
	if stage == "paused" {
		return map[string]any{"type": "pause_resolution", "human_required": true, "pause": state["pause"]}
	}
	return nil
}
