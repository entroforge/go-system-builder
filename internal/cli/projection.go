package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/change"
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
// S7 budget gateway, and docs/agent-protocol.md §S7 must all name the same
// Methodology Skill; controller.go projects this value verbatim into
// guidance and the recovery read order (.claude/skills/<skill>/SKILL.md).
// Focus-specific DV/QA/E2E Skills are per-Assignment dispatch facts, not the
// round's primary_skill.
const PrimarySkillS7 = "loop-orchestration"

var projectionContracts = map[string]stageContract{
	"S0":                 {"produce one human-locked requirement (draft via requirement-funnel; binding is the S1 action)", []string{"docs/requirements/"}, []string{"human_locked_req"}, []string{"a locked REQ exists in docs/requirements/ — `req bind` (S1) initializes the runtime and fingerprints it"}},
	"S2":                 {"complete architecture and any required UI design package", []string{"bound REQ", "docs/design/", "docs/rules/"}, []string{"architecture_record"}, []string{"architecture decisions cover the contract boundary", "any UI-impacting module has a complete target design package"}},
	"S3":                 {"complete the development contract set", []string{"bound REQ", "docs/design/", "docs/contracts/"}, []string{"locked_contract_set"}, []string{"at least one contract set is locked and traces to the REQ"}},
	"S4":                 {"complete an executable TASK batch", []string{"bound REQ", "docs/contracts/", "docs/tasks/"}, []string{"complete_task_batch"}, []string{"at least one TASK is complete and every contract clause has TASK coverage"}},
	"S5":                 {"independently verify and atomically lock the specification chain", []string{"bound REQ", "docs/design/", "docs/contracts/", "docs/tasks/"}, []string{"joint_document_pass"}, []string{"document-verification responsibilities pass with current fingerprints"}},
	"S6":                 {"implement the locked TASK batch", []string{"bound REQ", "locked contracts", "locked TASKs", "docs/agent-protocol.md#s6"}, []string{"builder_completion_reports", "verified_integration_checkpoints"}, []string{"every TASK in the TR-003 batch has a Builder Result with passing checks, no unapproved scope deviations, and a verified integration checkpoint (register results via `runtime task-complete`; no team manifest is required)"}},
	"S7":                 {"complete one current full verification round", []string{"bound REQ", "locked specification chain", "Builder evidence", "docs/agent-protocol.md#s7"}, []string{"review_plan"}, []string{"every required Claim of the registered ReviewPlan has a consumed pass Result; findings seal into the ObservationBatch (TR-008), otherwise the machine CleanRound closes the round (TR-009)"}},
	"S8":                 {"turn the sealed ObservationBatch into evidence-backed InvestigationCase dispositions", []string{"sealed ObservationBatch", "locked specification chain", "implementation"}, []string{"investigation_case", "causal_model_or_route"}, []string{"every Finding is covered by a Case route and every s9_repair Case has an approved RepairContract"}},
	"S9":                 {"execute approved RepairContracts and target-reverify them", []string{"approved RepairContract", "locked specification chain", "implementation"}, []string{"targeted_reverification"}, []string{"repair evidence is current and the Contract assertions pass"}},
	"S10":                {"complete acceptance and release audit", []string{"bound REQ", "current clean round", "ACC-template.md", "release_audits/TEMPLATE.md", "release-architecture-audit.md", "acceptance-and-handoff/SKILL.md"}, []string{"coverage_inventory", "counterevidence_ledger", "acceptance_record", "release_audit"}, []string{"coverage inventory is frozen and 100% dispositioned", "counterevidence is recorded for every coverage item", "UNKNOWN, unsupported PASS, unowned risk, untracked debt, and blocking finding are all zero", "S9 changes have returned through a fresh S7 clean round; no S9→S10 shortcut"}},
	"S11":                {"present the release-ready package to the human and record one explicit decision", []string{"acceptance record", "release audit", "release-ready package"}, []string{"human_decision"}, []string{"one explicit S11 decision is recorded or the Gateway remains awaiting a decision"}},
	"release_authorized": {"S11 human-authorized terminal", []string{"human decision record"}, []string{}, []string{"human authorization is recorded; Harness performs no merge, publication, deployment, or formal release"}},
	"aborted":            {"aborted terminal Runtime", []string{"human decision record"}, []string{}, []string{"automation remains stopped and only an eligible human-authorized rollover may start a new Runtime"}},
	"paused":             {"resolve the pause via one of the three human-gated exits: `runtime resume` (baselines unchanged) / `req amend` (drifted baseline, new REQ version) / `runtime human-decision --disposition abort` (abandon)", []string{"runtime pause checkpoint", "recorded blockers"}, []string{"pause_resolution"}, []string{"the blocking condition is resolved or a human chooses the next route"}},
	"cross-stage":        {"recover a valid runtime cursor", []string{".claude/loop-state.json", "docs/loop-definition.json"}, []string{"valid_runtime_cursor"}, []string{"runtime lifecycle and phase map to one declared stage"}},
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
	Stage         string   `json:"stage"`
	ProtocolRef   string   `json:"protocol_ref"`
	Objective     string   `json:"objective"`
	Action        string   `json:"action"`
	Read          []string `json:"read"`
	PrimarySkill  string   `json:"primary_skill"`
	Missing       []string `json:"missing"`
	DoneWhen      []string `json:"done_when"`
	Then          string   `json:"then"`
	HumanRequired bool     `json:"human_required"`
	ChangeID      string   `json:"change_id,omitempty"`
	WorkItemID    string   `json:"work_item_id,omitempty"`
	CheckIDs      []string `json:"check_ids,omitempty"`
}

func buildStatusProjection(state map[string]any, stage, lifecycle string, phase any, root string) statusProjection {
	contract := contractFor(stage, state, root)
	projection := statusProjection{
		RuntimeID: state["runtime_id"], Revision: state["revision"], BoundREQ: state["bound_req"],
		Stage: stage, Lifecycle: lifecycle, Phase: phase, Objective: contract.Objective,
		Completed: completedStages(stage), OpenItems: contract.Missing,
		ActiveWork: activeRuntimeWork(state), HumanGateway: projectedGateway(state, stage),
	}
	if record, ok := projectedChange(state); ok {
		summary := change.Summarize(record)
		projection.Change = &summary
		projection.OpenItems = changeOpenItems(record)
	}
	return projection
}

func buildNextProjection(state map[string]any, stage, skill, action, root string) nextProjection {
	contract := contractFor(stage, state, root)
	projection := nextProjection{
		Stage: stage, ProtocolRef: protocolReference(stage), Objective: contract.Objective,
		Action: action, Read: resolveBoundREQRead(contract.Read, state), PrimarySkill: skill,
		Missing: contract.Missing, DoneWhen: contract.DoneWhen, Then: "recompute",
		HumanRequired: stage == "S11" || stage == "paused" || stage == "aborted",
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
	applyS7BudgetGateway(&projection, state)
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
		if work.Status == "open" || work.Status == "active" || work.Status == "blocked" {
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
		if hasMarkdownArtifact(root, "docs/design/architecture", "ARCHITECTURE-*.md", "") {
			contract.Missing = []string{"contract_set"}
			if boundREQHasUIImpact(state) {
				complete, _ := hasCompleteUIDesignPackageForREQ(root, boundREQPathFromState(state))
				if !complete {
					contract.Missing = []string{"ui_design_package"}
				}
			}
		}
	case "S3":
		if hasMarkdownArtifact(root, "docs/contracts", "CONTRACTS-*.md", "locked") {
			contract.Missing = []string{"task_batch"}
		}
	case "S4":
		switch {
		case !hasMarkdownArtifact(root, "docs/contracts", "CONTRACTS-*.md", "locked"):
			contract.Missing = []string{"locked_contract_set"}
		case !hasMarkdownArtifact(root, "docs/tasks", "TASK-*.md", "complete"):
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
			contract.Missing = []string{"coverage_inventory", "counterevidence_ledger", "acceptance_manifest"}
		case "release_audit":
			contract.Missing = []string{"audit_areas:8", "counterevidence_ledger", "release_audit_manifest", "s11_handoff"}
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
	result := append([]string(nil), read...)
	for i := range result {
		if result[i] == "bound REQ" {
			result[i] = reqPath
		}
	}
	return result
}

func protocolReference(stage string) string {
	if len(stage) > 1 && stage[0] == 'S' {
		return "docs/agent-protocol.md#" + strings.ToLower(stage)
	}
	return "docs/agent-protocol.md#cursor-mapping"
}

func completedStages(stage string) []string {
	known := map[string][]string{
		"S0":      {},
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

func projectedGateway(state map[string]any, stage string) any {
	if s7BudgetGateRequired(state) {
		return map[string]any{
			"type":             "s7_budget_gateway",
			"human_required":   true,
			"decision_command": "loop-harness runtime s7-budget-decision --file <decision.json> --expected-revision <N> --actor <user>",
			"decisions":        []string{"increase_budget", "return_to_governance"},
			"guidance":         "the current S7 round may finish, but no new full review round opens until the human decision is recorded",
		}
	}
	switch lifecycleState(state) {
	case "awaiting_human_release":
		return map[string]any{
			"type":             "human_release_gateway",
			"human_required":   true,
			"decision_command": "loop-harness runtime human-decision --disposition <approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort> --expected-revision <N> --actor <user|orchestrator> --decision-evidence <ref>",
			"dispositions":     []string{"approve", "defer", "reject_defect", "reject_acceptance", "reject_release_audit", "abort"},
			"finding_evidence": "--finding-evidence <ref> is required for reject_defect",
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
