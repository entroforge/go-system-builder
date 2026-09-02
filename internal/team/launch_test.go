package team_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
)

func TestGenerateReadbackRequestsCreatesOneSchemaValidPackagePerAssignment(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestData, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	documents := []team.DocumentReference{
		{ID: "TASK-001", Kind: "task", Path: "docs/tasks/TASK-001.md", Version: "v1", SHA256: hash('1'), ReadOrder: 1},
		{ID: "CONTRACTS-001", Kind: "contract", Path: "docs/contracts/CONTRACTS-001.md", Version: "v1", SHA256: hash('2'), ReadOrder: 2},
		{ID: "REQ-002", Kind: "req", Path: "docs/requirements/REQ-002.md", Version: "v1", SHA256: hash('3'), ReadOrder: 3},
	}

	requests, err := team.GenerateReadbackRequests(root, manifestData, team.LaunchOptions{
		TaskID:                  "TASK-001",
		ExpectedRuntimeRevision: 4,
		Documents:               documents,
		OccurredAt:              time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 6 {
		t.Fatalf("expected 6 launch packages, got %d", len(requests))
	}

	validator := schema.NewValidator(root)
	for index, request := range requests {
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := validator.ValidateBytes("agent-message.schema.json", data); err != nil {
			t.Fatalf("request %d: %v", index, err)
		}
		if request.MessageType != "readback_request" {
			t.Fatalf("request %d is not phase one", index)
		}
		if request.Documents[0].Kind != "task" || request.Documents[1].Kind != "contract" || request.Documents[2].Kind != "req" {
			t.Fatalf("request %d has wrong read order: %#v", index, request.Documents)
		}
		if len(request.Skills) == 0 || request.Skills[0].SHA256 == "" {
			t.Fatalf("request %d lacks fingerprinted Skills", index)
		}
		if request.RoleFamily == "qa" {
			for _, name := range []string{
				"agent-dispatch", "testing-strategy", "code-quality", "security-review",
				"performance-review", "reliability-review", "database-change", "state-machine-design",
			} {
				if !hasSkill(request, name) {
					t.Fatalf("request %d lacks QA default skill %s", index, name)
				}
			}
		}
	}
}

func TestGenerateReadbackRequestsRejectsNonBottomUpDocuments(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestData, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = team.GenerateReadbackRequests(root, manifestData, team.LaunchOptions{
		TaskID: "TASK-001",
		Documents: []team.DocumentReference{
			{ID: "REQ-002", Kind: "req", Path: "req", Version: "v1", SHA256: hash('1'), ReadOrder: 1},
			{ID: "TASK-001", Kind: "task", Path: "task", Version: "v1", SHA256: hash('2'), ReadOrder: 2},
			{ID: "CONTRACTS-001", Kind: "contract", Path: "contract", Version: "v1", SHA256: hash('3'), ReadOrder: 3},
		},
	})
	if err == nil {
		t.Fatal("expected document-order rejection")
	}
}

func TestGenerateReadbackRequestsOmitsRuntimeRevisionByDefault(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestData, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	requests, err := team.GenerateReadbackRequests(root, manifestData, team.LaunchOptions{
		TaskID: "TASK-001",
		Documents: []team.DocumentReference{
			{ID: "TASK-001", Kind: "task", Path: "task", Version: "v1", SHA256: hash('1'), ReadOrder: 1},
			{ID: "CONTRACTS-001", Kind: "contract", Path: "contract", Version: "v1", SHA256: hash('2'), ReadOrder: 2},
			{ID: "REQ-002", Kind: "req", Path: "req", Version: "v1", SHA256: hash('3'), ReadOrder: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "expected_runtime_revision") {
		t.Fatalf("normal readback request must not expose Runtime revision: %s", data)
	}
}

func TestGenerateReadbackRequestsRejectsAuthoringPlaceholderAgentID(t *testing.T) {
	root := filepath.Join("..", "..")
	manifestData, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) == 0 {
		t.Fatal("manifest has no assignments")
	}
	first, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatal("manifest assignment has unexpected shape")
	}
	first["agent_id"] = "TODO(planner):agent-id-for-qa"
	manifestData, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	_, err = team.GenerateReadbackRequests(root, manifestData, team.LaunchOptions{
		TaskID: "TASK-001",
		Documents: []team.DocumentReference{
			{ID: "TASK-001", Kind: "task", Path: "task", Version: "v1", SHA256: hash('1'), ReadOrder: 1},
			{ID: "CONTRACTS-001", Kind: "contract", Path: "contract", Version: "v1", SHA256: hash('2'), ReadOrder: 2},
			{ID: "REQ-002", Kind: "req", Path: "req", Version: "v1", SHA256: hash('3'), ReadOrder: 3},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "authoring placeholder") {
		t.Fatalf("expected authoring-placeholder rejection, got %v", err)
	}
}

func hash(value byte) string {
	result := make([]byte, 64)
	for index := range result {
		result[index] = value
	}
	return string(result)
}

func hasSkill(request team.ReadbackRequest, name string) bool {
	for _, skill := range request.Skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}
