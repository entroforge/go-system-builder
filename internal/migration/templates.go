package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fileContract struct {
	path      string
	required  []string
	forbidden []string
	// optional files are validated only when present. Template source files
	// that are renamed in a target project (AGENTS-template.md -> AGENTS.md,
	// project-map-template.md -> project-map.md) are optional so the same
	// validator works in both layouts.
	optional bool
}

// settingsContractPath locates the Hook registration file. It lives at
// settings.json in the template factory and at .claude/settings.json in a
// running project. Both layouts carry the same migrated content.
func settingsContractPath(root string) (string, string) {
	target := filepath.Join(root, ".claude", "settings.json")
	if _, err := os.Stat(target); err == nil {
		return target, ".claude/settings.json"
	}
	return filepath.Join(root, "settings.json"), "settings.json"
}

func ValidateTemplates(root string) error {
	contracts := []fileContract{
		{
			path:     "AGENTS-template.md",
			optional: true,
			required: []string{
				"loop-definition.json", ".claude/loop-state.json", ".claude/skills/",
				".claude/agents/", "hook-policy.json", "awaiting_human_release",
				"docs/agent-protocol.md", "DRIVE()", "S0", "S11",
			},
			forbidden: []string{"Delivery Verifier Team：", "QA Team："},
		},
		{
			path: "prelude.md",
			required: []string{
				"loop-orchestration", "team-planning", "agent-dispatch",
				"bug-resolution", "clean-round-evaluation",
			},
			forbidden: []string{"## 3. Core Gates"},
		},
		{
			path: "docs/project.yaml",
			required: []string{
				"loop_definition:", "loop_runtime:", "hook_policy:", "skills:", "agents:",
			},
			forbidden: []string{"loop_mode:", "active_loop_req:", "goal_mode:", "lifecycle_phase:"},
		},
		{
			path:     "docs/project-map-template.md",
			optional: true,
			required: []string{
				"human-facing summary", ".claude/loop-state.json", "runtime ID and revision",
			},
			forbidden: []string{"Goal 模式", "Loop 模式", "Active loop REQ"},
		},
		{
			path: "docs/tasks/TASK-template.md",
			required: []string{
				"Team manifest:", "Assignment ID:", "Document Manifest",
				"Delivered Clauses", "Module Impact",
				"Selected Skills", "Lifecycle Evidence", "Closing Contract",
			},
			forbidden: []string{
				"TaskUpdate", "SendMessage", "30 个文件", "Agent Team 分工",
				"第一轮：阅读与复述", "BUG 修复循环",
			},
		},
		{
			path: "docs/reports/review/REV-template.md",
			required: []string{
				// v4.2 findings-only design: the §0 envelope skeleton is the
				// mandatory artifact; the markdown report is findings-only.
				"document_review", "subject_refs", "conclusion",
				"requested_event", "Findings",
			},
		},
		{
			path:     "docs/reports/qa/QA-template.md",
			required: []string{"Review round:", "Workgroup manifest:", "Responsibility:", "Best Practice:"},
			forbidden: []string{
				"每个 QA Agent 只能承担一个单一职责维度",
			},
		},
		{
			path: "docs/reports/e2e/E2E-template.md",
			required: []string{
				// The E2E projection must keep the seven-field negative-CASE
				// accounting and the cold-start digest binding discoverable.
				"Real-Browser Flow Execution", "`persisted_effects`", "`recovery`",
				"s7 workspace-digest", "capture_gaps",
			},
		},
		{
			path: "docs/reports/bugs/BUG-template.md",
			required: []string{
				"Canonical BUG", "InvestigationCase", "RepairContract",
				"## 2. Root Cause and Causal Model", "## 3. Approved Repair Contract Projection",
				"Targeted source-Finding verification", "complete S7 round",
			},
		},
		{
			path:     "docs/reports/acceptance/ACC-template.md",
			required: []string{"Clean review round:", "Clean-round evidence:", "human release approval"},
		},
		{
			path:      "docs/release_audits/TEMPLATE.md",
			required:  []string{"Clean-round evidence", "Release architecture audit", "Human release approval"},
			forbidden: []string{"Human audit |"},
		},
	}
	for _, contract := range contracts {
		data, err := os.ReadFile(filepath.Join(root, contract.path))
		if err != nil {
			if contract.optional {
				continue
			}
			return fmt.Errorf("%s: %w", contract.path, err)
		}
		content := string(data)
		for _, value := range contract.required {
			if !strings.Contains(content, value) {
				return fmt.Errorf("%s: missing migrated field or route %q", contract.path, value)
			}
		}
		for _, value := range contract.forbidden {
			if strings.Contains(content, value) {
				return fmt.Errorf("%s: contains migrated responsibility %q", contract.path, value)
			}
		}
	}

	// settings.json lives at different paths in the template factory (root) and
	// in a running project (.claude/). Validate whichever exists.
	settingsPath, settingsLabel := settingsContractPath(root)
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("%s: %w", settingsLabel, err)
	}
	for _, value := range []string{
		`"PreToolUse"`, `"SubagentStart"`, `"SubagentStop"`,
		`"TeammateIdle"`, `"Stop"`, `"SessionStart"`, `"PreCompact"`, `"PostToolUse"`,
		`"PostToolUseFailure"`, `"ConfigChange"`,
		`.claude/bin/loop-harness hook --event`,
	} {
		if !strings.Contains(string(settingsData), value) {
			return fmt.Errorf("%s: missing migrated field or route %q", settingsLabel, value)
		}
	}
	// PostToolUse and PostToolUseFailure are live observation events. ConfigChange
	// is also an audit-only observer; it cannot veto policy_settings changes.
	// PermissionRequest and TaskCompleted stay retired until their separate
	// human-gateway / stop-channel designs are approved.
	for _, removed := range []string{`"PermissionRequest"`, `"TaskCompleted"`} {
		if strings.Contains(string(settingsData), removed) {
			return fmt.Errorf("%s: obsolete Hook event %s", settingsLabel, removed)
		}
	}

	for _, path := range []string{
		"docs/project-map-template.md",
		"docs/rules/README.md",
		"docs/rules/communication.md",
		"docs/tasks/TASK-template.md",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			// These files are renamed or absent in a target project; the check
			// only applies when the file exists.
			continue
		}
		if strings.Contains(string(data), "docs/agent-protocol.md") {
			return fmt.Errorf("%s: still routes daily behavior through agent-protocol", path)
		}
	}
	return nil
}
