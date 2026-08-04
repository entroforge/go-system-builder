package semantic_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestValidateRepositoryAcceptsCurrentDesign(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := semantic.ValidateRepository(root); err != nil {
		t.Fatalf("repository validation failed: %v", err)
	}
}

func TestValidateRuntimeRejectsUnknownLifecycleState(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"].(map[string]any)["state"] = "unknown"
	invalid, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	if err := semantic.ValidateRuntimeBytes(root, invalid); err == nil {
		t.Fatal("expected unknown lifecycle state to fail")
	}
}

func TestValidateRuntimeFileAcceptsCommittedInactiveState(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := semantic.ValidateRuntimeFile(root, ".claude/loop-state.json"); err != nil {
		t.Fatalf("committed runtime validation failed: %v", err)
	}
}

func TestValidateReviewManifestsChecksCommittedWorkgroups(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := semantic.ValidateReviewManifests(root); err != nil {
		t.Fatalf("review manifest validation failed: %v", err)
	}
}

func TestValidateAgentMessagesChecksCommittedEvidence(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := semantic.ValidateAgentMessages(root); err != nil {
		t.Fatalf("Agent message validation failed: %v", err)
	}
}

func TestValidateRuntimeReachabilityAcceptsFreshRuntime(t *testing.T) {
	root := filepath.Join("..", "..")
	if err := semantic.ValidateRuntimeReachability(root); err != nil {
		t.Fatalf("runtime reachability validation failed: %v", err)
	}
}

// TestValidateReadbackTemplatesAcceptsWellFormedTemplate writes a well-formed
// template (documents in the canonical task, contract, req order) under a temp
// tree and asserts ValidateReadbackTemplates passes — BUG-002 B1.
func TestValidateReadbackTemplatesAcceptsWellFormedTemplate(t *testing.T) {
	root := t.TempDir()
	teamDir := filepath.Join(root, "docs", "teams", "team-a")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	template := map[string]any{
		"schema_version": "1.0.0",
		"message_type":   "readback_request",
		"message_id":     "msg-1",
		"agent_id":       "agent-1",
		"task_id":        "TASK-1",
		"team_id":        "team-a",
		"documents": []any{
			map[string]any{"id": "T", "kind": "task", "path": "docs/x.md", "version": "v1", "sha256": "1111111111111111111111111111111111111111111111111111111111111111", "read_order": 1},
			map[string]any{"id": "C", "kind": "contract", "path": "docs/x.md", "version": "v1", "sha256": "2222222222222222222222222222222222222222222222222222222222222222", "read_order": 2},
			map[string]any{"id": "R", "kind": "req", "path": "docs/x.md", "version": "v1", "sha256": "3333333333333333333333333333333333333333333333333333333333333333", "read_order": 3},
		},
		"skills":               []any{map[string]any{"name": "x"}},
		"scope":                map[string]any{"responsibility": "x"},
		"closing_contract_ref": "TASK-1#closing",
		"readback_fields":      []string{"objective"},
	}
	data, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamDir, "readback-request.template.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := semantic.ValidateReadbackTemplates(root); err != nil {
		t.Fatalf("expected well-formed readback template to pass, got: %v", err)
	}
}

// TestValidateReadbackTemplatesRejectsReqFirstOrder writes a template with
// documents in [req, task, contract] order and asserts ValidateReadbackTemplates
// rejects it — the prefixItems rule requires task, contract, req at read_order
// 1, 2, 3 when bug_id is unset. BUG-002 B1.
func TestValidateReadbackTemplatesRejectsReqFirstOrder(t *testing.T) {
	root := t.TempDir()
	teamDir := filepath.Join(root, "docs", "teams", "team-b")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	template := map[string]any{
		"schema_version": "1.0.0",
		"message_type":   "readback_request",
		"message_id":     "msg-2",
		"agent_id":       "agent-2",
		"task_id":        "TASK-2",
		"team_id":        "team-b",
		"documents": []any{
			map[string]any{"id": "R", "kind": "req", "path": "docs/x.md", "version": "v1", "sha256": "1111111111111111111111111111111111111111111111111111111111111111", "read_order": 1},
			map[string]any{"id": "T", "kind": "task", "path": "docs/x.md", "version": "v1", "sha256": "2222222222222222222222222222222222222222222222222222222222222222", "read_order": 2},
			map[string]any{"id": "C", "kind": "contract", "path": "docs/x.md", "version": "v1", "sha256": "3333333333333333333333333333333333333333333333333333333333333333", "read_order": 3},
		},
		"skills":               []any{map[string]any{"name": "x"}},
		"scope":                map[string]any{"responsibility": "x"},
		"closing_contract_ref": "TASK-2#closing",
		"readback_fields":      []string{"objective"},
	}
	data, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamDir, "readback-request.template.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = semantic.ValidateReadbackTemplates(root)
	if err == nil {
		t.Fatal("expected req-first-order template to fail validation")
	}
	if !strings.Contains(err.Error(), "readback-request.template.json") {
		t.Fatalf("error should mention the offending template file, got: %v", err)
	}
}

// TestValidateReadbackTemplatesNoTemplatesReturnsNil verifies that on a tree
// with no docs/teams directory the function returns nil. BUG-002 B1.
func TestValidateReadbackTemplatesNoTemplatesReturnsNil(t *testing.T) {
	root := t.TempDir()
	if err := semantic.ValidateReadbackTemplates(root); err != nil {
		t.Fatalf("expected nil error on empty tree, got: %v", err)
	}
}

// writeTeamManifestFixture writes a minimal team manifest with the given
// assignments to the given path under root.
func writeTeamManifestFixture(t *testing.T, path string, workgroupKind string, assignments []map[string]any) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version":      "1.0.0",
		"manifest_id":         "manifest-" + workgroupKind,
		"version":             "v1.0.0",
		"runtime_id":          "loop-test",
		"req_id":              "REQ-TEST",
		"baseline_generation": 1,
		"review_round":        1,
		"platform_team_id":    "team-test",
		"workgroup_id":        "workgroup-" + workgroupKind,
		"workgroup_kind":      workgroupKind,
		"status":              "planned",
		"documents":           []any{},
		"assignments":         assignments,
		"separation_edges":    []any{},
		"planned_agent_count": len(assignments),
		"max_parallel_agents": 1,
		"quantity_rationale":  "test",
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestValidateCrossManifestDependenciesRejectsPhantomEdge writes two team
// manifests under <root>/docs/teams/ where one manifest's assignment depends_on
// an assignment ID that lives in the other manifest, and asserts the scan
// rejects it with the "different manifest" hint. BUG-001 §8.3 A3.
func TestValidateCrossManifestDependenciesRejectsPhantomEdge(t *testing.T) {
	root := t.TempDir()
	manifestA := filepath.Join(root, "docs", "teams", "team-a", "manifest-a.json")
	manifestB := filepath.Join(root, "docs", "teams", "team-b", "manifest-b.json")

	writeTeamManifestFixture(t, manifestA, "qa", []map[string]any{
		{
			"assignment_id":        "assignment-qa-a",
			"responsibility_id":    "QA-A",
			"role_family":          "qa",
			"agent_id":             "agent-a",
			"agent_definition_ref": ".claude/agents/qa.md",
			"depends_on":           []string{"assignment-dv-b"}, // phantom edge into manifest B
			"reuse_decision":       "create",
			"status":               "planned",
		},
	})
	writeTeamManifestFixture(t, manifestB, "dv", []map[string]any{
		{
			"assignment_id":        "assignment-dv-b",
			"responsibility_id":    "DV-B",
			"role_family":          "dv",
			"agent_id":             "agent-b",
			"agent_definition_ref": ".claude/agents/dv.md",
			"depends_on":           []string{},
			"reuse_decision":       "create",
			"status":               "planned",
		},
	})

	refs := []string{
		filepath.ToSlash(filepath.Join("docs", "teams", "team-a", "manifest-a.json")),
		filepath.ToSlash(filepath.Join("docs", "teams", "team-b", "manifest-b.json")),
	}
	err := semantic.ValidateCrossManifestDependencies(root, refs)
	if err == nil {
		t.Fatal("expected cross-manifest phantom-edge dependency to fail")
	}
	if !strings.Contains(err.Error(), "different manifest") {
		t.Fatalf("error should mention 'different manifest', got: %v", err)
	}
}

// TestValidateCrossManifestDependenciesPassesOnCleanManifests writes two
// manifests whose depends_on entries only reference assignments in the same
// manifest, and asserts the scan passes. BUG-001 §8.3 A3.
func TestValidateCrossManifestDependenciesPassesOnCleanManifests(t *testing.T) {
	root := t.TempDir()
	manifestA := filepath.Join(root, "docs", "teams", "team-a", "manifest-a.json")
	manifestB := filepath.Join(root, "docs", "teams", "team-b", "manifest-b.json")

	writeTeamManifestFixture(t, manifestA, "qa", []map[string]any{
		{
			"assignment_id":        "assignment-qa-a1",
			"responsibility_id":    "QA-A1",
			"role_family":          "qa",
			"agent_id":             "agent-a1",
			"agent_definition_ref": ".claude/agents/qa.md",
			"depends_on":           []string{"assignment-qa-a2"}, // internal edge, fine
			"reuse_decision":       "create",
			"status":               "planned",
		},
		{
			"assignment_id":        "assignment-qa-a2",
			"responsibility_id":    "QA-A2",
			"role_family":          "qa",
			"agent_id":             "agent-a2",
			"agent_definition_ref": ".claude/agents/qa.md",
			"depends_on":           []string{},
			"reuse_decision":       "create",
			"status":               "planned",
		},
	})
	writeTeamManifestFixture(t, manifestB, "dv", []map[string]any{
		{
			"assignment_id":        "assignment-dv-b1",
			"responsibility_id":    "DV-B1",
			"role_family":          "dv",
			"agent_id":             "agent-b1",
			"agent_definition_ref": ".claude/agents/dv.md",
			"depends_on":           []string{}, // no cross-manifest edge
			"reuse_decision":       "create",
			"status":               "planned",
		},
	})

	refs := []string{
		filepath.ToSlash(filepath.Join("docs", "teams", "team-a", "manifest-a.json")),
		filepath.ToSlash(filepath.Join("docs", "teams", "team-b", "manifest-b.json")),
	}
	if err := semantic.ValidateCrossManifestDependencies(root, refs); err != nil {
		t.Fatalf("expected clean manifests to pass, got: %v", err)
	}
}

// TestValidateCrossManifestDependenciesEmptyRefsReturnsNil verifies the
// function returns nil when refs is empty (no active runtime / no manifests).
func TestValidateCrossManifestDependenciesEmptyRefsReturnsNil(t *testing.T) {
	root := t.TempDir()
	if err := semantic.ValidateCrossManifestDependencies(root, nil); err != nil {
		t.Fatalf("expected nil error on empty refs, got: %v", err)
	}
	if err := semantic.ValidateCrossManifestDependencies(root, []string{}); err != nil {
		t.Fatalf("expected nil error on empty refs, got: %v", err)
	}
}

// TestValidateReviewManifestReferencesMismatchErrorIncludesHint constructs a
// minimal tree where the manifest's document fingerprint does not match the
// on-disk hash, and asserts the error message contains the BUG-004 D5 recovery
// hint. BUG-004 §8 D5.
func TestValidateReviewManifestReferencesMismatchErrorIncludesHint(t *testing.T) {
	root := t.TempDir()
	// Minimal doc on disk.
	docPath := filepath.Join(root, "docs", "tasks", "TASK-1.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("# TASK-1\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Manifest with a deliberately wrong sha256.
	manifest := map[string]any{
		"runtime_id": "loop-test",
		"documents": []any{
			map[string]any{
				"id":     "TASK-1",
				"path":   "docs/tasks/TASK-1.md",
				"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
			},
		},
	}
	manifestPath := filepath.Join(root, "docs", "teams", "team-a", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = semantic.ValidateReviewManifestReferences(root, "docs/teams/team-a/manifest.json", "loop-test")
	if err == nil {
		t.Fatal("expected fingerprint mismatch to fail")
	}
	if !strings.Contains(err.Error(), "loop-harness runtime fingerprint") {
		t.Fatalf("error should include BUG-004 D5 recovery hint, got: %v", err)
	}
	if !strings.Contains(err.Error(), "BUG-004") {
		t.Fatalf("error should mention BUG-004, got: %v", err)
	}
}
