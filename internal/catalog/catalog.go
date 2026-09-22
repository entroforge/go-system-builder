package catalog

import (
	"bufio"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"os"
	"path/filepath"
	"strings"
)

type SkillSpec struct {
	Name     string
	Category string
}

var Skills = []SkillSpec{
	{"loop-orchestration", "methodology"},
	{"requirement-funnel", "methodology"},
	{"design-foundation", "methodology"},
	{"specification-planning", "methodology"},
	{"document-verification", "methodology"},
	{"agent-dispatch", "methodology"},
	{"team-planning", "methodology"},
	{"bug-resolution", "methodology"},
	{"impact-analysis", "methodology"},
	{"clean-round-evaluation", "methodology"},
	{"acceptance-and-handoff", "methodology"},
	{"frontend-engineering", "best-practice"},
	{"typescript-type-safety", "best-practice"},
	{"vue-router", "best-practice"},
	{"pinia", "best-practice"},
	{"vitest", "best-practice"},
	{"playwright-e2e", "best-practice"},
	{"eslint", "best-practice"},
	{"prettier", "best-practice"},
	{"vue-devtools", "best-practice"},
	{"backend-engineering", "best-practice"},
	{"domain-driven-design", "best-practice"},
	{"http-api-design", "best-practice"},
	{"gorm", "best-practice"},
	{"openapi-swagger", "best-practice"},
	{"gin", "best-practice"},
	{"casbin-authorization", "best-practice"},
	{"structured-logging", "best-practice"},
	{"s3-object-storage", "best-practice"},
	{"jwt-authentication", "best-practice"},
	{"dag-design", "best-practice"},
	{"api-contracts", "best-practice"},
	{"ui-prototyping", "best-practice"},
	{"design-critic", "best-practice"},
	{"scenario-model-design", "best-practice"},
	{"user-story-design", "best-practice"},
	{"user-flow-design", "best-practice"},
	{"testing-strategy", "best-practice"},
	{"integration-verification", "best-practice"},
	{"security-review", "best-practice"},
	{"performance-review", "best-practice"},
	{"reliability-review", "best-practice"},
	{"database-change", "best-practice"},
	{"state-machine-design", "best-practice"},
	{"code-quality", "best-practice"},
}

var agentRoles = []string{
	"frontend-builder",
	"backend-builder",
	"test-builder",
	"document-verifier",
	"delivery-verifier",
	"qa",
	"e2e-tester",
}

// installedAssetLayout selects one authority for the complete asset set.
// A deployed marker or either deployed asset directory selects installed mode;
// individual missing files must never fall back to the factory source tree.
func installedAssetLayout(root string) bool {
	for _, rel := range []string{"AGENTS.md", ".claude/settings.json", ".claude/skills", ".claude/agents"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			return true
		}
	}
	return false
}

func validateAssetAuthority(root string) error {
	if !installedAssetLayout(root) {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		return fmt.Errorf("mixed asset layout: installed skills must use .claude/skills, not root skills")
	}
	for _, role := range agentRoles {
		if _, err := os.Lstat(filepath.Join(root, "agents", role+".md")); err == nil {
			return fmt.Errorf("mixed asset layout: installed agents must use .claude/agents, not root agents")
		}
	}
	return nil
}

func resolveAssetPath(root, kind, name, leaf string) string {
	base := root
	if installedAssetLayout(root) {
		base = filepath.Join(root, ".claude")
	}
	segments := []string{base, kind, name}
	if leaf != "" {
		segments = append(segments, leaf)
	}
	return filepath.Join(segments...)
}

func ValidateSkills(root string) error {
	for _, spec := range Skills {
		if err := ValidateSkill(root, spec); err != nil {
			return err
		}
	}
	return nil
}

func ValidateSkill(root string, spec SkillSpec) error {
	if err := validateAssetAuthority(root); err != nil {
		return err
	}
	path := resolveAssetPath(root, "skills", spec.Name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("skill %s: %w", spec.Name, err)
	}
	frontmatter, body, err := splitFrontmatter(string(data))
	if err != nil {
		return fmt.Errorf("skill %s: %w", spec.Name, err)
	}
	if frontmatter["name"] != spec.Name {
		return fmt.Errorf("skill %s: frontmatter name mismatch", spec.Name)
	}
	if category := frontmatter["category"]; category != "" && category != spec.Category {
		return fmt.Errorf("skill %s: category must be %s", spec.Name, spec.Category)
	}
	description := frontmatter["description"]
	if !strings.HasPrefix(description, "Use when ") {
		return fmt.Errorf("skill %s: description must start with Use when", spec.Name)
	}
	for _, workflowWord := range []string{"; reads ", "; selects ", "; commits ", "; creates ", "; validates "} {
		if strings.Contains(strings.ToLower(description), workflowWord) {
			return fmt.Errorf("skill %s: description must contain triggers only", spec.Name)
		}
	}
	required := []string{"## Authority", "## Required Inputs", "## Outputs", "## Stop Conditions", "## Non-Goals"}
	if spec.Category == "methodology" {
		required = append(required, "## Entry Conditions", "## Procedure", "## Exit Conditions")
	} else {
		required = append(required, "## Applicability", "## Quality Criteria", "## N/A Criteria")
	}
	for _, heading := range required {
		if !strings.Contains(body, heading) {
			return fmt.Errorf("skill %s: missing %s", spec.Name, heading)
		}
	}
	// A Skill must cite at least one authoritative source. Shipped runtime
	// authorities include the Loop Definition, Hook Policy, Main Spine, and
	// reusable rules. Factory design documents cannot substitute for an
	// installed execution authority.
	if !strings.Contains(body, projectlayout.Definition) &&
		!strings.Contains(body, projectlayout.Protocol) &&
		!strings.Contains(body, projectlayout.Policy) &&
		!strings.Contains(body, "docs/rules/") {
		return fmt.Errorf("skill %s: missing authoritative source reference", spec.Name)
	}
	return nil
}

func ValidateAgents(root string) error {
	if err := validateAssetAuthority(root); err != nil {
		return err
	}
	for _, role := range agentRoles {
		path := resolveAssetPath(root, "agents", role+".md", "")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("agent %s: %w", role, err)
		}
		frontmatter, body, err := splitFrontmatter(string(data))
		if err != nil {
			return fmt.Errorf("agent %s: %w", role, err)
		}
		if frontmatter["name"] != role {
			return fmt.Errorf("agent %s: frontmatter name mismatch", role)
		}
		for _, field := range []string{"description", "tools", "disallowedTools", "permissionMode"} {
			if frontmatter[field] == "" {
				return fmt.Errorf("agent %s: missing frontmatter %s", role, field)
			}
		}
		for _, phrase := range []string{
			// L4 dispatch vocabulary: every definition states the plan
			// checkpoint flow and the activation exception.
			"PLAN_REPORT", "plan_checkpoint", "plan_approval_required", ".claude/loop-state.json",
			"squash merge", "## Mission", "## Allowed Artifacts", "## Output Contract", "## Stop Conditions",
		} {
			if !strings.Contains(body, phrase) {
				return fmt.Errorf("agent %s: missing contract phrase %q", role, phrase)
			}
		}
	}
	return nil
}

func splitFrontmatter(content string) (map[string]string, string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return nil, "", fmt.Errorf("missing YAML frontmatter")
	}
	parts := strings.SplitN(content[4:], "\n---\n", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("unterminated YAML frontmatter")
	}
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(parts[0]))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return values, parts[1], scanner.Err()
}
