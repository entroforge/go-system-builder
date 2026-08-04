// Package change contains the small deterministic Change Record overlay used
// by the Phase 1 workflow. It deliberately does not introduce a second state
// machine; work items and checks remain data attached to the current Runtime.
package change

import (
	"encoding/json"
	"fmt"
)

type Scope struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

type Acceptance struct {
	Ref    string `json:"ref"`
	Status string `json:"status,omitempty"`
}

type WorkItem struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Status     string   `json:"status"`
	DependsOn  []string `json:"depends_on"`
	Owner      string   `json:"owner"`
	WritePaths []string `json:"write_paths"`
}

type Check struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Reason         string   `json:"reason"`
	Command        string   `json:"command"`
	ScopeRefs      []string `json:"scope_refs"`
	Independence   string   `json:"independence"`
	AcceptanceRefs []string `json:"acceptance_refs"`
	Status         string   `json:"status"`
	EvidenceRef    *string  `json:"evidence_ref"`
}

type Finding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Status   string `json:"status"`
}

type Workspace struct {
	Branch            string `json:"branch"`
	BaseSHA           string `json:"base_sha"`
	IntegrationTarget string `json:"integration_target"`
}

type Record struct {
	ID             string       `json:"id"`
	REQRef         string       `json:"req_ref"`
	REQSHA         string       `json:"req_sha256"`
	Summary        string       `json:"summary"`
	Class          string       `json:"class"`
	Risk           string       `json:"risk"`
	Scope          Scope        `json:"scope"`
	Acceptance     []Acceptance `json:"acceptance"`
	Unknowns       []string     `json:"unknowns"`
	WorkItems      []WorkItem   `json:"work_items"`
	RequiredChecks []Check      `json:"required_checks"`
	Findings       []Finding    `json:"findings"`
	Workspace      Workspace    `json:"workspace"`
}

type Input struct {
	ID         string       `json:"id"`
	REQRef     string       `json:"req_ref"`
	REQSHA     string       `json:"req_sha256"`
	Summary    string       `json:"summary"`
	Class      string       `json:"class"`
	Risk       string       `json:"risk"`
	Scope      Scope        `json:"scope"`
	Acceptance []Acceptance `json:"acceptance"`
	Unknowns   []string     `json:"unknowns"`
	WorkItems  []WorkItem   `json:"work_items"`
	Workspace  Workspace    `json:"workspace"`
}

type Counts struct {
	Active int `json:"active"`
	Open   int `json:"open"`
	Done   int `json:"done"`
}

type CheckCounts struct {
	Passed int `json:"passed"`
	Open   int `json:"open"`
	Failed int `json:"failed"`
	NA     int `json:"n_a"`
}

type FindingCounts struct {
	Open     int `json:"open"`
	Blocking int `json:"blocking"`
}

type Summary struct {
	ID        string        `json:"id"`
	Summary   string        `json:"summary"`
	Class     string        `json:"class"`
	Risk      string        `json:"risk"`
	WorkItems Counts        `json:"work_items"`
	Checks    CheckCounts   `json:"checks"`
	Findings  FindingCounts `json:"findings"`
}

type Next struct {
	Action     string
	WorkItemID string
	CheckID    string
}

var validClasses = map[string]bool{
	"docs-only": true, "config": true, "behavior feature": true, "bugfix": true,
	"refactor": true, "deletion": true, "performance": true, "migration": true,
	"exploration": true,
}

var validRisks = map[string]bool{"low": true, "medium": true, "high": true}

func BuildRecord(input Input) (Record, error) {
	record := Record{
		ID: input.ID, REQRef: input.REQRef, REQSHA: input.REQSHA,
		Summary: input.Summary, Class: input.Class, Risk: input.Risk,
		Scope: input.Scope, Acceptance: input.Acceptance, Unknowns: input.Unknowns,
		WorkItems: input.WorkItems, Findings: []Finding{}, Workspace: input.Workspace,
	}
	if record.Scope.Include == nil {
		record.Scope.Include = []string{}
	}
	if record.Scope.Exclude == nil {
		record.Scope.Exclude = []string{}
	}
	if record.Acceptance == nil {
		record.Acceptance = []Acceptance{}
	}
	if record.Unknowns == nil {
		record.Unknowns = []string{}
	}
	for i := range record.WorkItems {
		if record.WorkItems[i].Status == "" {
			record.WorkItems[i].Status = "open"
		}
		if record.WorkItems[i].DependsOn == nil {
			record.WorkItems[i].DependsOn = []string{}
		}
		if record.WorkItems[i].WritePaths == nil {
			record.WorkItems[i].WritePaths = []string{}
		}
	}
	record.RequiredChecks = triggeredChecks(record.Class, record.Risk, record.Scope)
	if err := Validate(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func Validate(record Record) error {
	for name, value := range map[string]string{"id": record.ID, "req_ref": record.REQRef, "req_sha256": record.REQSHA, "summary": record.Summary, "class": record.Class, "risk": record.Risk} {
		if value == "" {
			return fmt.Errorf("change %s is required", name)
		}
	}
	if !validClasses[record.Class] {
		return fmt.Errorf("unsupported change class %q", record.Class)
	}
	if !validRisks[record.Risk] {
		return fmt.Errorf("unsupported change risk %q", record.Risk)
	}
	if len(record.WorkItems) == 0 {
		return fmt.Errorf("change requires at least one work item")
	}
	for i, item := range record.WorkItems {
		if item.ID == "" || item.Text == "" {
			return fmt.Errorf("work_items[%d] requires id and text", i)
		}
		if !validWorkStatus(item.Status) {
			return fmt.Errorf("work_items[%d] has unsupported status %q", i, item.Status)
		}
	}
	for i, check := range record.RequiredChecks {
		if check.ID == "" || check.Kind == "" {
			return fmt.Errorf("required_checks[%d] requires id and kind", i)
		}
		if !validCheckStatus(check.Status) {
			return fmt.Errorf("required_checks[%d] has unsupported status %q", i, check.Status)
		}
	}
	return nil
}

func Decode(value map[string]any) (Record, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return Record{}, fmt.Errorf("encode change record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("decode change record: %w", err)
	}
	return record, nil
}

func Encode(record Record) (map[string]any, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode change record: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode change record map: %w", err)
	}
	return value, nil
}

func Summarize(record Record) Summary {
	var summary Summary
	summary.ID, summary.Summary, summary.Class, summary.Risk = record.ID, record.Summary, record.Class, record.Risk
	for _, item := range record.WorkItems {
		switch item.Status {
		case "active":
			summary.WorkItems.Active++
		case "open", "blocked":
			summary.WorkItems.Open++
		case "done":
			summary.WorkItems.Done++
		}
	}
	for _, check := range record.RequiredChecks {
		switch check.Status {
		case "passed":
			summary.Checks.Passed++
		case "open", "invalid":
			summary.Checks.Open++
		case "failed":
			summary.Checks.Failed++
		case "n_a":
			summary.Checks.NA++
		}
	}
	for _, finding := range record.Findings {
		if finding.Status != "closed" && finding.Status != "rejected" {
			summary.Findings.Open++
		}
		if finding.Severity == "blocking" && finding.Status != "closed" && finding.Status != "rejected" {
			summary.Findings.Blocking++
		}
	}
	return summary
}

func NextStep(record Record) Next {
	var work *WorkItem
	for i := range record.WorkItems {
		if record.WorkItems[i].Status == "open" || record.WorkItems[i].Status == "active" {
			work = &record.WorkItems[i]
			break
		}
	}
	var check *Check
	for i := range record.RequiredChecks {
		if record.RequiredChecks[i].Status == "open" || record.RequiredChecks[i].Status == "invalid" {
			check = &record.RequiredChecks[i]
			break
		}
	}
	next := Next{}
	if work != nil {
		next.WorkItemID = work.ID
	}
	if check != nil {
		next.CheckID = check.ID
	}
	switch {
	case work != nil && check != nil:
		next.Action = fmt.Sprintf("implement %s and run %s", work.ID, check.ID)
	case work != nil:
		next.Action = "implement " + work.ID
	case check != nil:
		next.Action = "run " + check.ID
	default:
		next.Action = "recompute change readiness"
	}
	return next
}

// DefaultChecks returns the deterministic required_checks set derived from
// class+risk+scope. Runtime callers use it to assert that a Record's
// RequiredChecks has not been silently reduced below the governance default.
func DefaultChecks(class, risk string, scope Scope) []Check {
	return triggeredChecks(class, risk, scope)
}

func triggeredChecks(class, risk string, scope Scope) []Check {
	kinds := map[string][]string{
		"docs-only":        {"link_check"},
		"config":           {"config_parse", "affected_behavior"},
		"behavior feature": {"acceptance_test", "unit_integration"},
		"bugfix":           {"reproduction", "regression_test", "affected_tests"},
		"refactor":         {"characterization", "affected_regression"},
		"deletion":         {"reference_analysis", "affected_tests"},
		"performance":      {"benchmark"},
		"migration":        {"rehearsal", "data_validation", "rollback"},
		"exploration":      {"bounded_observation"},
	}
	checks := make([]Check, 0, len(kinds[class]))
	scopeRefs := copyStrings(scope.Include)
	for i, kind := range kinds[class] {
		checks = append(checks, Check{
			ID: fmt.Sprintf("CK-%d", i+1), Kind: kind, Reason: class,
			ScopeRefs: scopeRefs, AcceptanceRefs: []string{}, Independence: "self", Status: "open",
			EvidenceRef: nil,
		})
	}
	if risk == "high" {
		checks = append(checks, Check{ID: fmt.Sprintf("CK-%d", len(checks)+1), Kind: "independent_review", Reason: "high risk", ScopeRefs: scopeRefs, AcceptanceRefs: []string{}, Independence: "independent", Status: "open", EvidenceRef: nil})
	}
	return checks
}

func copyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}

func validWorkStatus(status string) bool {
	return map[string]bool{"open": true, "active": true, "done": true, "blocked": true, "cancelled": true}[status]
}

func validCheckStatus(status string) bool {
	return map[string]bool{"open": true, "passed": true, "failed": true, "n_a": true, "invalid": true}[status]
}
