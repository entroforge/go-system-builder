package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/schema"
)

type manifest struct {
	WorkgroupKind              string        `json:"workgroup_kind"`
	RiskTags                   []riskTag     `json:"risk_tags"`
	ResponsibilityDispositions []disposition `json:"responsibility_dispositions"`
	Assignments                []assignment  `json:"assignments"`
	SeparationEdges            []edge        `json:"separation_edges"`
	PlannedAgentCount          int           `json:"planned_agent_count"`
	MaxParallelAgents          int           `json:"max_parallel_agents"`
}

type riskTag struct {
	Tag string `json:"tag"`
}

type disposition struct {
	ResponsibilityID string   `json:"responsibility_id"`
	Disposition      string   `json:"disposition"`
	AssignmentIDs    []string `json:"assignment_ids"`
}

type assignment struct {
	AssignmentID       string   `json:"assignment_id"`
	Responsibility     string   `json:"responsibility_id"`
	RoleFamily         string   `json:"role_family"`
	AgentID            string   `json:"agent_id"`
	AgentDefinitionRef string   `json:"agent_definition_ref"`
	SkillRefs          []string `json:"skill_refs"`
	ReadPaths          []string `json:"read_paths"`
	WritePaths         []string `json:"write_paths"`
	OutputPaths        []string `json:"output_paths"`
	DependsOn          []string `json:"depends_on"`
	ReuseDecision      string   `json:"reuse_decision"`
	GroupingRationale  string   `json:"grouping_rationale"`
}

type edge struct {
	Left  string `json:"left_assignment_id"`
	Right string `json:"right_assignment_id"`
}

var mandatoryByWorkgroup = map[string][]string{
	"document_verifier": {"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"},
	"delivery_verifier": {"VER-REQ-GAP", "VER-SPEC-GAP", "VER-MODULE-COMPLETE"},
	"qa": {
		"QA-MODULE-CODE",
		"QA-REUSE-ABSTRACTION",
		"QA-UNIT-TEST",
		"QA-INTEGRATION-TEST",
	},
	"e2e_browser": {"E2E-USER-FLOW", "E2E-CONSOLE-NETWORK"},
}

var responsibilitySkills = map[string][]string{
	"QA-UNIT-TEST":        {"testing-strategy"},
	"QA-INTEGRATION-TEST": {"testing-strategy"},
	"QA-SECURITY":         {"security-review"},
	"QA-PERFORMANCE":      {"performance-review"},
	"QA-RELIABILITY":      {"reliability-review"},
	"QA-MIGRATION":        {"database-change"},
	"VER-INTEGRATION":     {"integration-verification"},
	"E2E-USER-FLOW":       {"e2e-browser-testing", "playwright-e2e"},
	"E2E-CONSOLE-NETWORK": {"e2e-browser-testing", "playwright-e2e"},
}

var riskResponsibilityByWorkgroup = map[string]map[string]string{
	"delivery_verifier": {
		"cross-component": "VER-INTEGRATION",
		"regression":      "VER-REGRESSION",
	},
	"qa": {
		"architecture": "QA-ARCHITECTURE",
		"security":     "QA-SECURITY",
		"performance":  "QA-PERFORMANCE",
		"reliability":  "QA-RELIABILITY",
		"concurrency":  "QA-RELIABILITY",
		"database":     "QA-MIGRATION",
		"migration":    "QA-MIGRATION",
	},
	"e2e_browser": {
		"frontend":   "E2E-USER-FLOW",
		"ui":         "E2E-USER-FLOW",
		"regression": "E2E-USER-FLOW",
	},
}

func ValidateFile(root, path string) error {
	if err := schema.NewValidator(root).ValidateFile(
		"team-manifest.schema.json",
		path,
	); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return err
	}
	return ValidateBytes(data)
}

func ValidateBytes(data []byte) error {
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode team manifest: %w", err)
	}

	dispositions := make(map[string]disposition, len(value.ResponsibilityDispositions))
	for _, item := range value.ResponsibilityDispositions {
		dispositions[item.ResponsibilityID] = item
	}
	for _, responsibility := range mandatoryByWorkgroup[value.WorkgroupKind] {
		if _, ok := dispositions[responsibility]; !ok {
			return fmt.Errorf("missing mandatory responsibility %s", responsibility)
		}
	}
	for _, tag := range value.RiskTags {
		required := riskResponsibilityByWorkgroup[value.WorkgroupKind][tag.Tag]
		if required == "" {
			continue
		}
		item, ok := dispositions[required]
		if !ok || item.Disposition != "assigned" {
			return fmt.Errorf("risk %s requires assigned responsibility %s", tag.Tag, required)
		}
	}

	assignments := make(map[string]assignment, len(value.Assignments))
	agents := make(map[string]struct{})
	for _, item := range value.Assignments {
		assignments[item.AssignmentID] = item
		agents[item.AgentID] = struct{}{}
		for _, requiredSkill := range responsibilitySkills[item.Responsibility] {
			if !contains(item.SkillRefs, requiredSkill) {
				return fmt.Errorf("responsibility %s requires skill %s", item.Responsibility, requiredSkill)
			}
		}
	}
	if len(agents) != value.PlannedAgentCount {
		return fmt.Errorf("planned_agent_count=%d does not match unique agents=%d", value.PlannedAgentCount, len(agents))
	}
	if value.MaxParallelAgents > value.PlannedAgentCount {
		return fmt.Errorf("max_parallel_agents exceeds planned_agent_count")
	}
	for _, item := range value.SeparationEdges {
		left, leftOK := assignments[item.Left]
		right, rightOK := assignments[item.Right]
		if !leftOK || !rightOK {
			return fmt.Errorf("separation edge references unknown assignment")
		}
		if left.AgentID == right.AgentID {
			return fmt.Errorf("separation edge assignments share agent %s", left.AgentID)
		}
	}
	if first := firstMissingDependency(assignments); first != "" {
		return fmt.Errorf("depends_on references assignment %q not in this manifest (depends_on is workgroup-internal; encode cross-workgroup waits via separation edges or runtime scheduling)", first)
	}
	if hasCycle(assignments) {
		return fmt.Errorf("assignment dependency cycle")
	}
	return nil
}

// firstMissingDependency returns the alphabetically-first depends_on target
// that is not present in the assignment map, or "" if all deps resolve locally.
func firstMissingDependency(assignments map[string]assignment) string {
	missing := make(map[string]struct{})
	for _, item := range assignments {
		for _, dependency := range item.DependsOn {
			if _, ok := assignments[dependency]; !ok {
				missing[dependency] = struct{}{}
			}
		}
	}
	if len(missing) == 0 {
		return ""
	}
	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys[0]
}

func hasCycle(assignments map[string]assignment) bool {
	const (
		unseen = iota
		visiting
		done
	)
	states := make(map[string]int, len(assignments))
	var visit func(string) bool
	visit = func(id string) bool {
		if states[id] == visiting {
			return true
		}
		if states[id] == done {
			return false
		}
		states[id] = visiting
		for _, dependency := range assignments[id].DependsOn {
			if _, ok := assignments[dependency]; !ok {
				continue
			}
			if visit(dependency) {
				return true
			}
		}
		states[id] = done
		return false
	}
	for id := range assignments {
		if visit(id) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
