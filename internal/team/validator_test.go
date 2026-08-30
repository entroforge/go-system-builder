package team_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
)

func TestApprovedExampleIsSemanticallyValid(t *testing.T) {
	data, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := team.ValidateBytes(data); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRejectsMissingMandatoryResponsibility(t *testing.T) {
	manifest := loadExample(t)
	dispositions := manifest["responsibility_dispositions"].([]any)
	manifest["responsibility_dispositions"] = dispositions[1:]
	assertInvalid(t, manifest, "missing mandatory responsibility QA-MODULE-CODE")
}

func TestManifestRejectsRiskSkillMismatch(t *testing.T) {
	manifest := loadExample(t)
	assignments := manifest["assignments"].([]any)
	for _, raw := range assignments {
		assignment := raw.(map[string]any)
		if assignment["responsibility_id"] == "QA-RELIABILITY" {
			assignment["skill_refs"] = []any{"code-quality"}
		}
	}
	assertInvalid(t, manifest, "requires skill reliability-review")
}

func TestManifestRejectsSeparatedAssignmentsSharingAgent(t *testing.T) {
	manifest := loadExample(t)
	assignments := manifest["assignments"].([]any)
	assignments[1].(map[string]any)["agent_id"] = assignments[0].(map[string]any)["agent_id"]
	manifest["planned_agent_count"] = float64(5)
	assertInvalid(t, manifest, "separation edge assignments share agent")
}

func TestManifestRejectsDependencyCycle(t *testing.T) {
	manifest := loadExample(t)
	assignments := manifest["assignments"].([]any)
	left := assignments[0].(map[string]any)
	right := assignments[1].(map[string]any)
	left["depends_on"] = []any{right["assignment_id"]}
	right["depends_on"] = []any{left["assignment_id"]}
	assertInvalid(t, manifest, "assignment dependency cycle")
}

// TestManifestRejectsMissingDependency covers BUG-001: depends_on references
// to assignments outside the current workgroup manifest must be rejected with
// a distinct error class (not reported as a cycle).
func TestManifestRejectsMissingDependency(t *testing.T) {
	manifest := loadExample(t)
	assignments := manifest["assignments"].([]any)
	assignments[0].(map[string]any)["depends_on"] = []any{"assignment-builder-outside-workgroup"}
	assertInvalid(t, manifest, "not in this manifest")
	if strings.Contains(lastErr.Error(), "cycle") {
		t.Fatalf("missing-dependency error must not be reported as a cycle; got %v", lastErr)
	}
}

func TestManifestRejectsIncorrectAgentCount(t *testing.T) {
	manifest := loadExample(t)
	manifest["planned_agent_count"] = float64(5)
	assertInvalid(t, manifest, "planned_agent_count")
}

func TestE2EBrowserManifestRequiresBrowserResponsibilities(t *testing.T) {
	manifest := loadExample(t)
	manifest["manifest_id"] = "team-manifest-REQ-001-e2e-round-1"
	manifest["workgroup_id"] = "workgroup-e2e-round-1"
	manifest["workgroup_kind"] = "e2e_browser"
	manifest["planned_agent_count"] = float64(2)
	manifest["max_parallel_agents"] = float64(2)
	manifest["responsibility_dispositions"] = []any{
		disposition("E2E-USER-FLOW", "assignment-e2e-user-flow"),
		disposition("E2E-CONSOLE-NETWORK", "assignment-e2e-console-network"),
	}
	manifest["assignments"] = []any{
		assignment("assignment-e2e-user-flow", "E2E-USER-FLOW", "agent-e2e-user-flow", "e2e-browser-testing"),
		assignment("assignment-e2e-console-network", "E2E-CONSOLE-NETWORK", "agent-e2e-console-network", "e2e-browser-testing"),
	}
	manifest["separation_edges"] = []any{}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := team.ValidateBytes(data); err != nil {
		t.Fatal(err)
	}

	manifest["assignments"].([]any)[0].(map[string]any)["skill_refs"] = []any{"e2e-browser-testing"}
	assertInvalid(t, manifest, "responsibility E2E-USER-FLOW requires skill playwright-e2e")

	manifest = loadExample(t)
	manifest["manifest_id"] = "team-manifest-REQ-001-e2e-round-1"
	manifest["workgroup_id"] = "workgroup-e2e-round-1"
	manifest["workgroup_kind"] = "e2e_browser"
	manifest["planned_agent_count"] = float64(2)
	manifest["max_parallel_agents"] = float64(2)

	manifest["responsibility_dispositions"] = []any{
		disposition("E2E-USER-FLOW", "assignment-e2e-user-flow"),
	}
	assertInvalid(t, manifest, "missing mandatory responsibility E2E-CONSOLE-NETWORK")
}

func TestInvestigatorManifestIsSemanticallyValid(t *testing.T) {
	manifest := loadExample(t)
	manifest["manifest_id"] = "team-manifest-S8-case-1-hyp-1"
	manifest["workgroup_id"] = "workgroup-S8-case-1-hyp-1"
	manifest["workgroup_kind"] = "investigator"
	manifest["planned_agent_count"] = float64(1)
	manifest["max_parallel_agents"] = float64(1)
	manifest["responsibility_dispositions"] = []any{
		disposition("S8-CAUSAL-INVESTIGATION", "assignment-s8-case-1-hyp-1"),
	}
	manifest["assignments"] = []any{
		assignment("assignment-s8-case-1-hyp-1", "S8-CAUSAL-INVESTIGATION", "agent-investigator-1", "bug-resolution"),
	}
	manifest["separation_edges"] = []any{}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := team.ValidateBytes(data); err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", data); err != nil {
		t.Fatal(err)
	}
}

func loadExample(t *testing.T) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

var lastErr error

func disposition(id, assignmentID string) map[string]any {
	return map[string]any{
		"responsibility_id": id,
		"disposition":       "assigned",
		"trigger":           "mandatory e2e browser baseline",
		"assignment_ids":    []any{assignmentID},
		"na_rationale":      nil,
		"evidence_ref":      nil,
	}
}

func assignment(id, responsibilityID, agentID, skill string) map[string]any {
	return map[string]any{
		"assignment_id":        id,
		"responsibility_id":    responsibilityID,
		"role_family":          "e2e-tester",
		"scope":                []any{"browser e2e"},
		"agent_id":             agentID,
		"agent_definition_ref": ".claude/agents/e2e-tester.md",
		"skill_refs":           []any{skill, "playwright-e2e"},
		"read_paths":           []any{"docs/", "tests/", "src/"},
		"write_paths":          []any{"docs/reports/e2e/" + id + ".md"},
		"output_paths":         []any{"docs/reports/e2e/" + id + ".md"},
		"depends_on":           []any{},
		"reuse_decision":       "create",
		"grouping_rationale":   "E2E browser responsibility has an independent conclusion.",
		"status":               "planned",
	}
}

func assertInvalid(t *testing.T, manifest map[string]any, want string) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	lastErr = team.ValidateBytes(data)
	if lastErr == nil || !strings.Contains(lastErr.Error(), want) {
		t.Fatalf("expected %q, got %v", want, lastErr)
	}
}
