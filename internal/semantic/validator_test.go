package semantic_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestValidateRepositoryAcceptsCurrentDesign(t *testing.T) {
	root := isolatedSemanticProject(t)
	if err := semantic.ValidateRepository(root); err != nil {
		t.Fatalf("repository validation failed: %v", err)
	}
}

func TestValidateRuntimeRejectsUnknownLifecycleState(t *testing.T) {
	root := isolatedSemanticProject(t)
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

func TestValidateRuntimeRequiresMatchingUnifiedS7AssignmentEvent(t *testing.T) {
	root := isolatedSemanticProject(t)
	runtimePath := filepath.Join(root, ".claude", "loop-state.json")
	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state["schema_version"] != "1.3.0" {
		t.Fatalf("fresh fixture format = %v, want 1.3.0", state["schema_version"])
	}
	review := state["review"].(map[string]any)
	review["assignments"] = map[string]any{
		"assignment-qa-1": map[string]any{
			"lens":                 "qa",
			"claim_ids":            []any{},
			"status":               "pending",
			"last_lifecycle_event": "review_plan_registered",
		},
	}
	valid, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeBytes(root, valid); err != nil {
		t.Fatalf("matching S7 Assignment event rejected: %v", err)
	}

	row := review["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	row["status"] = "active"
	invalid, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeBytes(root, invalid); err == nil {
		t.Fatal("mismatched S7 Assignment status/event was accepted")
	}
}

func TestValidateRuntimeFileAcceptsFreshInactiveState(t *testing.T) {
	root := isolatedSemanticProject(t)
	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	definition := state["definition"].(map[string]any)
	policyRef := state["hook_control"].(map[string]any)["policy_ref"].(map[string]any)
	if _, exists := definition["sha256"]; exists {
		t.Fatal("fresh Runtime unexpectedly registered a Definition digest")
	}
	if _, exists := policyRef["sha256"]; exists {
		t.Fatal("fresh Runtime unexpectedly registered a Hook policy digest")
	}
	if err := semantic.ValidateRuntimeFile(root, ".claude/loop-state.json"); err != nil {
		t.Fatalf("fresh runtime validation failed: %v", err)
	}
	// Legacy fields remain readable but are not compared to the current files.
	definition["sha256"] = strings.Repeat("0", 64)
	policyRef["sha256"] = strings.Repeat("0", 64)
	legacy, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeFile(root, ".claude/loop-state.json"); err != nil {
		t.Fatalf("historical Runtime digests must not be compared: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "docs", "control", "hook-policy.json")); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateRuntimeFile(root, ".claude/loop-state.json"); err == nil {
		t.Fatal("missing referenced Hook policy must be reported")
	}
}

func TestValidateReviewManifestsAcceptsFreshProject(t *testing.T) {
	root := isolatedSemanticProject(t)
	if err := semantic.ValidateReviewManifests(root); err != nil {
		t.Fatalf("review manifest validation failed: %v", err)
	}
}

func TestValidateAgentMessagesAcceptsFreshProject(t *testing.T) {
	root := isolatedSemanticProject(t)
	if err := semantic.ValidateAgentMessages(root); err != nil {
		t.Fatalf("Agent message validation failed: %v", err)
	}
}

func TestValidateRuntimeReachabilityAcceptsFreshRuntime(t *testing.T) {
	root := isolatedSemanticProject(t)
	if err := semantic.ValidateRuntimeReachability(root); err != nil {
		t.Fatalf("runtime reachability validation failed: %v", err)
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

// Historical digest text does not block a reachable original document.
func TestValidateReviewManifestReferencesIgnoresHistoricalDigest(t *testing.T) {
	root := t.TempDir()
	// Minimal doc on disk.
	docPath := filepath.Join(root, "docs", "dev", "tasks", "TASK-1.md")
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
				"path":   "docs/dev/tasks/TASK-1.md",
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
	if err != nil {
		t.Fatalf("historical digest must not block the source: %v", err)
	}
	if err := os.Remove(docPath); err != nil {
		t.Fatal(err)
	}
	if err := semantic.ValidateReviewManifestReferences(root, "docs/teams/team-a/manifest.json", "loop-test"); err == nil {
		t.Fatal("missing actual source must still be reported")
	}
}

// Each repository-level check owns an inactive Runtime and never reads the
// developer's ignored state or journal from the source checkout.
func freshRepositoryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("..", "..")
	for _, dir := range []string{"docs", "agents", "skills"} {
		if err := os.CopyFS(filepath.Join(root, dir), os.DirFS(filepath.Join(source, dir))); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "settings.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, entry.Name()), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("init fixture: %s %s", stdout.String(), stderr.String())
	}
	return root
}
