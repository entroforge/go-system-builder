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

var projectionContracts = map[string]stageContract{
	"S0":          {"bind one human-locked requirement", []string{"docs/requirements/"}, []string{"locked_req_binding"}, []string{"a locked REQ is fingerprinted and bound to the runtime"}},
	"S2":          {"complete architecture and any required UI design package", []string{"bound REQ", "docs/design/", "docs/rules/"}, []string{"architecture_record"}, []string{"architecture decisions cover the contract boundary", "any UI-impacting module has a complete target design package"}},
	"S3":          {"complete the development contract set", []string{"bound REQ", "docs/design/", "docs/contracts/"}, []string{"locked_contract_set"}, []string{"at least one contract set is locked and traces to the REQ"}},
	"S4":          {"complete an executable TASK batch", []string{"bound REQ", "docs/contracts/", "docs/tasks/"}, []string{"complete_task_batch"}, []string{"at least one TASK is complete and every contract clause has TASK coverage"}},
	"S5":          {"independently verify and atomically lock the specification chain", []string{"bound REQ", "docs/design/", "docs/contracts/", "docs/tasks/"}, []string{"joint_document_pass"}, []string{"document-verification responsibilities pass with current fingerprints"}},
	"S6":          {"implement the locked TASK batch", []string{"bound REQ", "locked contracts", "locked TASKs"}, []string{"builder_completion_reports"}, []string{"all Builder assignments report completion and owned checks pass"}},
	"S7":          {"complete one current full verification round", []string{"bound REQ", "locked specification chain", "Builder evidence"}, []string{"current_clean_round"}, []string{"all required verification dimensions pass in the same round"}},
	"S8":          {"turn blocking findings into evidence-backed dispositions", []string{"blocking findings", "locked specification chain", "implementation"}, []string{"finding_dispositions"}, []string{"every finding has a supported disposition and every accepted BUG has a Closing Contract"}},
	"S9":          {"repair accepted BUGs and target-reverify them", []string{"accepted BUGs", "locked specification chain", "implementation"}, []string{"targeted_reverification"}, []string{"repair evidence is current and targeted reverification passes"}},
	"S10":         {"complete acceptance and release audit", []string{"bound REQ", "current clean round", "valid evidence"}, []string{"acceptance_record", "release_audit"}, []string{"acceptance and release audit are complete with no open action"}},
	"S11":         {"present the release-ready package to the human", []string{"acceptance record", "release audit", "release-ready package"}, []string{}, []string{"the release Gateway is visible and automation has stopped"}},
	"paused":      {"resolve the recorded pause condition", []string{"runtime pause checkpoint", "recorded blockers"}, []string{"pause_resolution"}, []string{"the blocking condition is resolved or a human chooses the next route"}},
	"cross-stage": {"recover a valid runtime cursor", []string{".claude/loop-state.json", "docs/loop-definition.json"}, []string{"valid_runtime_cursor"}, []string{"runtime lifecycle and phase map to one declared stage"}},
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
		HumanRequired: stage == "S11" || stage == "paused",
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
				complete, _ := hasCompleteUIDesignPackage(filepath.Join(root, "docs/design/prototypes"))
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
		if lifecycleState(state) == "release_audit" {
			contract.Missing = []string{"release_audit"}
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

func verificationMissingItem(state map[string]any) string {
	switch lifecyclePhase(state) {
	case "delivery":
		return "delivery_round"
	case "qa":
		return "qa_round"
	case "e2e_browser":
		return "e2e_browser_round"
	case "clean_round_passed":
		return "acceptance_transition"
	default:
		return "current_clean_round"
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
		"S0":  {},
		"S2":  {"S0", "S1"},
		"S3":  {"S0", "S1", "S2"},
		"S4":  {"S0", "S1", "S2", "S3"},
		"S5":  {"S0", "S1", "S2", "S3", "S4"},
		"S6":  {"S0", "S1", "S2", "S3", "S4", "S5"},
		"S7":  {"S0", "S1", "S2", "S3", "S4", "S5", "S6"},
		"S8":  {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7"},
		"S9":  {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8"},
		"S10": {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7"},
		"S11": {"S0", "S1", "S2", "S3", "S4", "S5", "S6", "S7", "S10"},
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
	if stage == "S11" {
		return map[string]any{"type": "release_ready", "human_required": true}
	}
	if stage == "paused" {
		return map[string]any{"type": "pause_resolution", "human_required": true, "pause": state["pause"]}
	}
	return nil
}
