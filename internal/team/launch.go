package team

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/entroforge/go-system-builder/internal/catalog"
)

type DocumentReference struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	ReadOrder int    `json:"read_order"`
}

type SkillReference struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Source   string `json:"source"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
}

type MessageScope struct {
	Responsibility        string   `json:"responsibility"`
	ReadPaths             []string `json:"read_paths"`
	ProspectiveWritePaths []string `json:"prospective_write_paths"`
	ForbiddenPaths        []string `json:"forbidden_paths"`
	ForbiddenActions      []string `json:"forbidden_actions"`
	OutputPaths           []string `json:"output_paths"`
}

type ReadbackRequest struct {
	SchemaVersion           string              `json:"schema_version"`
	MessageType             string              `json:"message_type"`
	MessageID               string              `json:"message_id"`
	CorrelationID           string              `json:"correlation_id"`
	RuntimeID               string              `json:"runtime_id"`
	ExpectedRuntimeRevision int                 `json:"expected_runtime_revision"`
	AgentID                 string              `json:"agent_id"`
	AgentDefinitionRef      string              `json:"agent_definition_ref"`
	TaskID                  string              `json:"task_id"`
	BugID                   *string             `json:"bug_id"`
	TeamID                  *string             `json:"team_id"`
	OccurredAt              string              `json:"occurred_at"`
	RoleFamily              string              `json:"role_family"`
	Documents               []DocumentReference `json:"documents"`
	Skills                  []SkillReference    `json:"skills"`
	Scope                   MessageScope        `json:"scope"`
	ClosingContractRef      string              `json:"closing_contract_ref"`
	ReadbackFields          []string            `json:"readback_fields"`
}

type LaunchOptions struct {
	TaskID                  string
	BugID                   *string
	ExpectedRuntimeRevision int
	Documents               []DocumentReference
	OccurredAt              time.Time
}

type launchManifest struct {
	RuntimeID      string       `json:"runtime_id"`
	PlatformTeamID string       `json:"platform_team_id"`
	WorkgroupID    string       `json:"workgroup_id"`
	Assignments    []assignment `json:"assignments"`
}

// roleDefaultSkills mirrors each Agent Definition's skills: frontmatter.
// Claude Code does not preload that frontmatter for Agent Team teammates, so
// launch packages carry the same profile for explicit Skill-tool loading.
var roleDefaultSkills = map[string][]string{
	"frontend-builder": {
		"frontend-engineering", "typescript-type-safety", "vue-router", "pinia",
		"vitest", "eslint", "prettier", "vue-devtools", "ui-prototyping",
		"user-story-design", "user-flow-design", "api-contracts",
		"integration-verification", "testing-strategy", "code-quality",
	},
	"backend-builder": {
		"backend-engineering", "domain-driven-design", "http-api-design", "gorm",
		"openapi-swagger", "gin", "casbin-authorization", "structured-logging",
		"s3-object-storage", "jwt-authentication", "dag-design", "api-contracts",
		"database-change", "state-machine-design", "security-review", "reliability-review",
		"performance-review", "integration-verification", "testing-strategy", "code-quality",
	},
	"test-builder": {
		"testing-strategy", "vitest", "integration-verification", "api-contracts",
		"database-change", "state-machine-design", "reliability-review", "code-quality",
	},
	"document-verifier": {
		"document-verification", "specification-planning", "ui-prototyping",
		"user-story-design", "user-flow-design", "api-contracts", "state-machine-design",
		"database-change",
	},
	"delivery-verifier": {
		"integration-verification", "frontend-engineering", "backend-engineering",
		"api-contracts", "testing-strategy", "user-flow-design", "state-machine-design",
		"database-change", "code-quality",
	},
	"qa": {
		"testing-strategy", "code-quality", "frontend-engineering", "backend-engineering",
		"api-contracts", "integration-verification", "security-review", "performance-review",
		"reliability-review", "database-change", "state-machine-design", "vitest",
	},
	"e2e-tester": {
		"e2e-browser-testing", "playwright-e2e", "testing-strategy", "user-flow-design",
		"ui-prototyping", "frontend-engineering", "vue-router", "pinia", "api-contracts",
		"integration-verification",
	},
}

func GenerateReadbackRequests(root string, data []byte, options LaunchOptions) ([]ReadbackRequest, error) {
	if err := ValidateBytes(data); err != nil {
		return nil, err
	}
	if err := validateDocumentOrder(options.Documents, options.BugID != nil); err != nil {
		return nil, err
	}
	var value launchManifest
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode launch manifest: %w", err)
	}
	occurredAt := options.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	teamID := value.PlatformTeamID
	requests := make([]ReadbackRequest, 0, len(value.Assignments))
	for _, item := range value.Assignments {
		skillNames := append([]string{"two-phase-activation"}, roleDefaultSkills[item.RoleFamily]...)
		skillNames = append(skillNames, item.SkillRefs...)
		skills, err := resolveSkills(root, skillNames)
		if err != nil {
			return nil, fmt.Errorf("assignment %s: %w", item.AssignmentID, err)
		}
		requests = append(requests, ReadbackRequest{
			SchemaVersion:           "1.0.0",
			MessageType:             "readback_request",
			MessageID:               "msg-readback-" + item.AssignmentID,
			CorrelationID:           "corr-" + value.WorkgroupID + "-" + item.AssignmentID,
			RuntimeID:               value.RuntimeID,
			ExpectedRuntimeRevision: options.ExpectedRuntimeRevision,
			AgentID:                 item.AgentID,
			AgentDefinitionRef:      item.AgentDefinitionRef,
			TaskID:                  options.TaskID,
			BugID:                   options.BugID,
			TeamID:                  &teamID,
			OccurredAt:              occurredAt.UTC().Format(time.RFC3339Nano),
			RoleFamily:              item.RoleFamily,
			Documents:               options.Documents,
			Skills:                  skills,
			Scope: MessageScope{
				Responsibility:        item.Responsibility + ": " + item.GroupingRationale,
				ReadPaths:             item.ReadPaths,
				ProspectiveWritePaths: item.WritePaths,
				ForbiddenPaths:        []string{".claude/loop-state.json", "docs/requirements/", "docs/contracts/"},
				ForbiddenActions:      []string{"self activation", "scope expansion", "squash merge", "formal release"},
				OutputPaths:           item.OutputPaths,
			},
			ClosingContractRef: options.TaskID + "#closing-contract",
			ReadbackFields: []string{
				"objective", "user_value", "responsibility", "covered_clauses",
				"planned_surfaces", "allowed_actions", "forbidden_actions",
				"dependencies", "integration_boundaries", "expected_outputs",
				"evidence_plan", "closing_criteria", "skill_applications",
				"assumptions", "risks", "unresolved_questions", "documents_read",
			},
		})
	}
	return requests, nil
}

func validateDocumentOrder(documents []DocumentReference, repair bool) error {
	if len(documents) < 3 {
		return fmt.Errorf("launch requires at least TASK, contract, and REQ documents")
	}
	expected := []string{"task", "contract", "req"}
	if repair {
		expected = []string{"bug", "task", "contract", "req"}
	}
	if len(documents) < len(expected) {
		return fmt.Errorf("launch document chain is incomplete")
	}
	for index, kind := range expected {
		if documents[index].Kind != kind || documents[index].ReadOrder != index+1 {
			return fmt.Errorf("documents must use bottom-up order %v", expected)
		}
	}
	for index := 1; index < len(documents); index++ {
		if documents[index].ReadOrder <= documents[index-1].ReadOrder {
			return fmt.Errorf("document read_order must be strictly increasing")
		}
	}
	return nil
}

func resolveSkills(root string, names []string) ([]SkillReference, error) {
	specs := make(map[string]catalog.SkillSpec, len(catalog.Skills))
	for _, spec := range catalog.Skills {
		specs[spec.Name] = spec
	}
	unique := make(map[string]SkillReference)
	for _, name := range names {
		if _, ok := unique[name]; ok {
			continue
		}
		spec, ok := specs[name]
		if !ok {
			return nil, fmt.Errorf("unknown Skill %s", name)
		}
		source := filepath.ToSlash(filepath.Join(".claude", "skills", name, "SKILL.md"))
		// The Skill file lives under .claude/skills/ in a running project and
		// under skills/ in the template factory. Try both so the same binary
		// works in either layout.
		data, err := os.ReadFile(filepath.Join(root, ".claude", "skills", name, "SKILL.md"))
		if err != nil {
			data, err = os.ReadFile(filepath.Join(root, "skills", name, "SKILL.md"))
			if err != nil {
				return nil, fmt.Errorf("read Skill %s: %w", name, err)
			}
		}
		unique[name] = SkillReference{
			Name:     name,
			Category: spec.Category,
			Source:   source,
			Version:  "1.0.0",
			SHA256:   fmt.Sprintf("%x", sha256.Sum256(data)),
		}
	}
	result := make([]SkillReference, 0, len(unique))
	for _, item := range unique {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
