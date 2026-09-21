package semantic_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

type semanticReferenceFixture struct {
	root         string
	manifestPath string
	messagePath  string
}

type semanticReferenceDocument struct {
	ID      string
	Kind    string
	Path    string
	Version string
	SHA256  string
}

// TestValidateRuntimeReferencesChecksNonEmptyFiles keeps the semantic
// validators on the path exercised by an initialized project with registered
// team and Agent artifacts. The fresh-project tests intentionally cover empty
// references; this fixture covers the corresponding non-empty branch.
func TestValidateRuntimeReferencesChecksNonEmptyFiles(t *testing.T) {
	t.Run("valid references pass", func(t *testing.T) {
		fixture := writeSemanticReferenceFixture(t)
		if err := semantic.ValidateReviewManifests(fixture.root); err != nil {
			t.Fatalf("valid team manifest reference rejected: %v", err)
		}
		if err := semantic.ValidateAgentMessages(fixture.root); err != nil {
			t.Fatalf("valid Agent message reference rejected: %v", err)
		}
		if err := semantic.ValidateRuntimeReachability(fixture.root); err != nil {
			t.Fatalf("valid Runtime references rejected: %v", err)
		}
	})

	t.Run("missing manifest is rejected", func(t *testing.T) {
		fixture := writeSemanticReferenceFixture(t)
		if err := os.Remove(filepath.Join(fixture.root, fixture.manifestPath)); err != nil {
			t.Fatal(err)
		}
		if err := semantic.ValidateReviewManifests(fixture.root); err == nil {
			t.Fatal("missing referenced team manifest was accepted")
		}
		if err := semantic.ValidateRuntimeReachability(fixture.root); err == nil {
			t.Fatal("missing referenced team manifest was reachable")
		}
	})

	t.Run("missing Agent message is rejected", func(t *testing.T) {
		fixture := writeSemanticReferenceFixture(t)
		if err := os.Remove(filepath.Join(fixture.root, fixture.messagePath)); err != nil {
			t.Fatal(err)
		}
		if err := semantic.ValidateAgentMessages(fixture.root); err == nil {
			t.Fatal("missing referenced Agent message was accepted")
		}
		if err := semantic.ValidateRuntimeReachability(fixture.root); err == nil {
			t.Fatal("missing referenced Agent message was reachable")
		}
	})
}

func writeSemanticReferenceFixture(t *testing.T) semanticReferenceFixture {
	t.Helper()
	root := isolatedSemanticProject(t)

	documents := []semanticReferenceDocument{
		writeSemanticReferenceDocument(t, root, "REQ-002", "req", "docs/requirements/REQ-002-reference-test.md", "# REQ-002\n\nReference requirement.\n"),
		writeSemanticReferenceDocument(t, root, "CONTRACTS-002", "contract", "docs/dev/contracts/CONTRACTS-002-reference-test.md", "# CONTRACTS-002\n\nReference contract.\n"),
		writeSemanticReferenceDocument(t, root, "TASK-001", "task", "docs/dev/tasks/TASK-001-reference-test.md", "# TASK-001\n\nReference task.\n"),
	}

	runtimePath := filepath.Join(root, ".claude", "loop-state.json")
	runtimeData, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(runtimeData, &state); err != nil {
		t.Fatal(err)
	}
	runtimeID, ok := state["runtime_id"].(string)
	if !ok || runtimeID == "" {
		t.Fatalf("initialized runtime has no runtime_id: %#v", state["runtime_id"])
	}

	manifestData, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = "team-manifest-reference-test"
	manifest["runtime_id"] = runtimeID
	manifest["platform_team_id"] = "platform-team-reference-test"
	manifest["workgroup_id"] = "workgroup-reference-test"
	manifest["documents"] = semanticManifestDocuments(documents)
	manifestPath := "docs/teams/reference/manifest.json"
	writeSemanticJSON(t, root, manifestPath, manifest)

	messageExamples, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(messageExamples, &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("agent-message examples are empty")
	}
	message := messages[0]
	message["message_id"] = "msg-reference-test"
	message["correlation_id"] = "corr-reference-test"
	message["runtime_id"] = runtimeID
	message["agent_id"] = "agent-reference-test"
	message["agent_definition_ref"] = "agents/backend-builder.md"
	message["task_id"] = "TASK-001"
	message["team_id"] = "team-reference-test"
	message["documents"] = semanticMessageDocuments(documents)
	messagePath := "docs/teams/reference/readback.json"
	writeSemanticJSON(t, root, messagePath, message)

	entities, ok := state["entities"].(map[string]any)
	if !ok {
		t.Fatalf("initialized runtime entities have unexpected shape: %#v", state["entities"])
	}
	entities["agents"] = []any{map[string]any{
		"id":                  "agent-reference-test",
		"role":                "backend-builder",
		"state":               "reading",
		"task_ids":            []any{"TASK-001"},
		"team_id":             "team-reference-test",
		"definition_ref":      "agents/backend-builder.md",
		"prompt_ref":          "agents/backend-builder.md",
		"readback_ref":        messagePath,
		"activation_ref":      nil,
		"activation_revision": nil,
		"updated_at":          "2026-09-21T00:00:00Z",
	}}
	entities["teams"] = []any{map[string]any{
		"id":                 "team-reference-test",
		"platform_team_id":   "platform-team-reference-test",
		"kind":               "qa",
		"status":             "planned",
		"manifest_ref":       manifestPath,
		"responsibility_ids": []any{"QA-MODULE-CODE", "QA-REUSE-ABSTRACTION", "QA-UNIT-TEST", "QA-INTEGRATION-TEST"},
		"agent_ids":          []any{"agent-reference-test"},
		"review_round":       1,
	}}

	updatedRuntime, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewValidator(root).ValidateBytes("loop-state.schema.json", updatedRuntime); err != nil {
		t.Fatalf("synthesized Runtime is not schema-valid: %v", err)
	}
	if err := os.WriteFile(runtimePath, append(updatedRuntime, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	return semanticReferenceFixture{
		root:         root,
		manifestPath: manifestPath,
		messagePath:  messagePath,
	}
}

func writeSemanticReferenceDocument(t *testing.T, root, id, kind, path, content string) semanticReferenceDocument {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return semanticReferenceDocument{
		ID:      id,
		Kind:    kind,
		Path:    path,
		Version: "v1.0.0",
		SHA256:  semanticSHA256([]byte(content)),
	}
}

func semanticManifestDocuments(documents []semanticReferenceDocument) []any {
	items := make([]any, 0, len(documents))
	for _, document := range documents {
		items = append(items, map[string]any{
			"id":      document.ID,
			"path":    document.Path,
			"version": document.Version,
			"sha256":  document.SHA256,
		})
	}
	return items
}

func semanticMessageDocuments(documents []semanticReferenceDocument) []any {
	items := make([]any, 0, len(documents))
	for index, document := range documents {
		items = append(items, map[string]any{
			"id":         document.ID,
			"kind":       document.Kind,
			"path":       document.Path,
			"version":    document.Version,
			"sha256":     document.SHA256,
			"read_order": index + 1,
		})
	}
	return items
}

func writeSemanticJSON(t *testing.T, root, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func semanticSHA256(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
