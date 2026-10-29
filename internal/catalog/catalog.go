package catalog

import (
	"bufio"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"os"
	"path/filepath"
	"regexp"
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
	{"e2e-browser-testing", "best-practice"},
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

// ReadSkillAsset chooses one asset authority for the entire project. Missing
// installed files never fall back to a factory file with the same name.
func ReadSkillAsset(root, name string) ([]byte, string, error) {
	known := false
	for _, spec := range Skills {
		if spec.Name == name {
			known = true
			break
		}
	}
	if !known {
		return nil, "", fmt.Errorf("unknown Skill %s", name)
	}
	if err := validateAssetAuthority(root); err != nil {
		return nil, "", err
	}
	path := resolveAssetPath(root, "skills", name, "SKILL.md")
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, "", err
	}
	rel = filepath.ToSlash(rel)
	data, err := workpacket.ReadFile(root, rel, 1<<20)
	if err != nil {
		return nil, rel, fmt.Errorf("read Skill %s: %w", name, err)
	}
	return data, rel, nil
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
		// Native roles inherit platform tools unless the user configures them.
		// Engineering responsibilities do not require a tool allow/deny list.
		for _, field := range []string{"description", "permissionMode"} {
			if frontmatter[field] == "" {
				return fmt.Errorf("agent %s: missing frontmatter %s", role, field)
			}
		}
		// This is a definition/format check, not certification of professional
		// work. Native return and frozen sources are shared responsibilities;
		// Builder plans are not duties of independent Reviewers.
		required := []string{
			"## Mission", "## Allowed Artifacts", "## Output Contract", "## Stop Conditions",
			"SubagentStop", "frozen",
		}
		if strings.HasSuffix(role, "-builder") {
			required = append(required, "task-brief", "PlanReport", "RepairResult")
		} else {
			required = append(required, "questionnaire", "independent")
		}
		contractText := strings.ToLower(agentContractVisibleText(body))
		for _, phrase := range required {
			if !strings.Contains(contractText, strings.ToLower(phrase)) {
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

// ReadAgentAsset uses the same single asset authority as Skill delivery.
func ReadAgentAsset(root, role string) ([]byte, string, error) {
	known := false
	for _, name := range agentRoles {
		if name == role {
			known = true
		}
	}
	if !known {
		return nil, "", fmt.Errorf("unknown Agent role %s", role)
	}
	if err := validateAssetAuthority(root); err != nil {
		return nil, "", err
	}
	path := resolveAssetPath(root, "agents", role+".md", "")
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, "", err
	}
	rel = filepath.ToSlash(rel)
	data, err := workpacket.ReadFile(root, rel, 1<<20)
	if err != nil {
		return nil, rel, fmt.Errorf("read Agent role %s: %w", role, err)
	}
	return data, rel, nil
}

var agentLinkDefinition = regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]\n]+\]:[^\n]*(?:\n|$)`)

// Check the instructions an author reads, not link targets. Preserve link
// labels, including labels whose destinations contain balanced parentheses.
func agentContractVisibleText(body string) string {
	body = agentLinkDefinition.ReplaceAllString(body, "\n")
	var visible strings.Builder
	for i := 0; i < len(body); {
		if body[i] == '<' {
			if end := strings.IndexByte(body[i:], '>'); end >= 0 {
				target := body[i+1 : i+end]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					i += end + 1
					continue
				}
			}
		}
		if body[i] == '[' {
			end, depth := i+1, 1
			for ; end < len(body) && depth > 0; end++ {
				if body[end] == '\\' {
					end++
					continue
				}
				if body[end] == '[' {
					depth++
				}
				if body[end] == ']' {
					depth--
				}
			}
			if depth == 0 && end < len(body) && (body[end] == '(' || body[end] == '[') {
				open, close := body[end], byte(')')
				if open == '[' {
					close = ']'
				}
				targetEnd, targetDepth := end+1, 1
				for ; targetEnd < len(body) && targetDepth > 0; targetEnd++ {
					if body[targetEnd] == '\\' {
						targetEnd++
						continue
					}
					if body[targetEnd] == open {
						targetDepth++
					}
					if body[targetEnd] == close {
						targetDepth--
					}
				}
				if targetDepth == 0 {
					visible.WriteString(agentContractVisibleText(body[i+1 : end-1]))
					i = targetEnd
					continue
				}
			}
		}
		visible.WriteByte(body[i])
		i++
	}
	return visible.String()
}
