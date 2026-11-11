package semantic_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
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

func TestDoctorIgnoresHistoricalManifestDigests(t *testing.T) {
	fixture := writeSemanticReferenceFixture(t)
	manifestPath := filepath.Join(fixture.root, fixture.manifestPath)
	runtimePath := filepath.Join(fixture.root, ".claude", "loop-state.json")
	runtimeData, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeState map[string]any
	if err := json.Unmarshal(runtimeData, &runtimeState); err != nil {
		t.Fatal(err)
	}
	example, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var exampleState map[string]any
	if err := json.Unmarshal(example, &exampleState); err != nil {
		t.Fatal(err)
	}
	bound := exampleState["bound_req"].(map[string]any)
	bound["path"] = "docs/requirements/REQ-002-reference-test.md"
	runtimeState["bound_req"] = bound
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		value   any
		omitted bool
	}{{"omitted", nil, true}, {"null", nil, false}, {"nonstandard", "old-nonstandard", false}, {"number", 42, false}, {"object", map[string]any{"old": true}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			machineRefs := []map[string]any{runtimeState["definition"].(map[string]any), runtimeState["hook_control"].(map[string]any)["policy_ref"].(map[string]any), bound}
			for _, ref := range machineRefs {
				if tc.omitted {
					delete(ref, "sha256")
				} else {
					ref["sha256"] = tc.value
				}
			}
			writeSemanticJSON(t, fixture.root, ".claude/loop-state.json", runtimeState)
			for _, value := range manifest["documents"].([]any) {
				doc := value.(map[string]any)
				if tc.omitted {
					delete(doc, "sha256")
				} else {
					doc["sha256"] = tc.value
				}
			}
			writeSemanticJSON(t, fixture.root, fixture.manifestPath, manifest)
			before, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			runtimeBefore, err := os.ReadFile(runtimePath)
			if err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := cli.Run([]string{"doctor", "--root", fixture.root}, nil, &out, &stderr); code != 0 {
				t.Fatalf("doctor rejected historical manifest digest: code=%d stderr=%s", code, stderr.String())
			}
			after, err := os.ReadFile(manifestPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("doctor rewrote original manifest: %v", err)
			}
			runtimeAfter, err := os.ReadFile(runtimePath)
			if err != nil || !bytes.Equal(runtimeBefore, runtimeAfter) {
				t.Fatalf("doctor rewrote original Runtime: %v", err)
			}
		})
	}
}

func TestValidateAgentMessagesDistinguishesStagedEnvelopeFromActivation(t *testing.T) {
	fixture := writeSemanticReferenceFixture(t)
	runtimePath := filepath.Join(fixture.root, ".claude", "loop-state.json")
	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	agent := state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	activationRef := "docs/teams/reference/activation-envelope.json"
	writeSemanticJSON(t, fixture.root, activationRef, map[string]any{
		"agent_id": "agent-reference-test", "allowed_tools": []string{"Read"},
		"allowed_write_paths": []string{}, "allowed_command_classes": []string{},
	})
	// In Runtime 1.3, registration projects an Agent as active while its
	// lifecycle event is still workgroup_registered. That is not an activation
	// message: activation_revision remains nil until activation_sent commits.
	agent["state"], agent["last_lifecycle_event"], agent["activation_ref"], agent["activation_revision"] = "active", "workgroup_registered", activationRef, nil
	writeSemanticJSON(t, fixture.root, ".claude/loop-state.json", state)
	if err := semantic.ValidateAgentMessages(fixture.root); err != nil {
		t.Fatalf("1.3 registered capability envelope was treated as an Agent message: %v", err)
	}
	if err := semantic.ValidateRuntimeReachability(fixture.root); err != nil {
		t.Fatalf("1.3 registered capability envelope was not reachable: %v", err)
	}

	agent["last_lifecycle_event"], agent["activation_revision"] = "activation_sent", float64(8)
	writeSemanticJSON(t, fixture.root, ".claude/loop-state.json", state)
	if err := semantic.ValidateAgentMessages(fixture.root); err == nil {
		t.Fatal("malformed activation message was skipped after a real activation revision")
	}
}

func TestValidateAgentMessagesCancellationDoesNotInventAnActivation(t *testing.T) {
	for version, activity := range map[string]string{"1.2.0": "stopped", "1.3.0": "cancelled"} {
		t.Run(version, func(t *testing.T) {
			fixture := writeSemanticReferenceFixture(t)
			data, err := os.ReadFile(filepath.Join(fixture.root, ".claude/loop-state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			state["schema_version"] = version
			agent := state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
			ref := "docs/teams/reference/activation-envelope.json"
			writeSemanticJSON(t, fixture.root, ref, map[string]any{
				"agent_id": "agent-reference-test", "allowed_tools": []string{"Read"},
				"allowed_write_paths": []string{}, "allowed_command_classes": []string{},
			})
			agent["state"], agent["last_lifecycle_event"] = activity, "shutdown_approved"
			agent["activation_ref"], agent["activation_revision"] = ref, nil
			writeSemanticJSON(t, fixture.root, ".claude/loop-state.json", state)
			if err := semantic.ValidateAgentMessages(fixture.root); err != nil {
				t.Fatalf("cancellation invented a prior activation message: %v", err)
			}
			if err := semantic.ValidateRuntimeReachability(fixture.root); err != nil {
				t.Fatalf("original staged source was not reachable: %v", err)
			}
			agent["activation_revision"] = float64(8)
			writeSemanticJSON(t, fixture.root, ".claude/loop-state.json", state)
			if err := semantic.ValidateAgentMessages(fixture.root); err == nil {
				t.Fatal("cancellation skipped the actual activation source schema")
			}
		})
	}
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
	var message map[string]any
	for _, candidate := range messages {
		if candidate["message_type"] == "readback_response" {
			message = candidate
			break
		}
	}
	if message == nil {
		t.Fatal("agent-message example lacks readback_response")
	}
	message["message_id"] = "msg-reference-test"
	message["correlation_id"] = "corr-reference-test"
	message["runtime_id"] = runtimeID
	message["agent_id"] = "agent-reference-test"
	message["agent_definition_ref"] = "agents/backend-builder.md"
	message["task_id"] = "TASK-001"
	message["team_id"] = "team-reference-test"
	messagePath := "docs/teams/reference/readback.json"
	writeSemanticJSON(t, root, messagePath, message)

	entities, ok := state["entities"].(map[string]any)
	if !ok {
		t.Fatalf("initialized runtime entities have unexpected shape: %#v", state["entities"])
	}
	entities["agents"] = []any{map[string]any{
		"id":                   "agent-reference-test",
		"role":                 "backend-builder",
		"state":                "active",
		"last_lifecycle_event": "workgroup_registered",
		"task_ids":             []any{"TASK-001"},
		"team_id":              "team-reference-test",
		"definition_ref":       "agents/backend-builder.md",
		"prompt_ref":           "agents/backend-builder.md",
		"readback_ref":         messagePath,
		"activation_ref":       nil,
		"activation_revision":  nil,
		"updated_at":           "2026-09-21T00:00:00Z",
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
	}
}

func semanticManifestDocuments(documents []semanticReferenceDocument) []any {
	items := make([]any, 0, len(documents))
	for _, document := range documents {
		items = append(items, map[string]any{
			"id":      document.ID,
			"path":    document.Path,
			"version": document.Version,
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
