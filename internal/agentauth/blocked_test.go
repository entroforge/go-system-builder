package agentauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestValidateBlockedPlanAuthorizationRequiresOriginalPlanActivation(t *testing.T) {
	t.Run("valid blocked owner retains original authorization", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err != nil {
			t.Fatalf("valid blocked PLAN/activation rejected: %v", err)
		}
	})

	t.Run("wrong state rejected", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		fixture.agent["state"] = "working"
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err == nil {
			t.Fatal("working Agent was accepted as blocked")
		}
	})

	t.Run("missing PLAN rejected", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(fixture.planRef))); err != nil {
			t.Fatal(err)
		}
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err == nil {
			t.Fatal("missing original PLAN was accepted")
		}
	})

	t.Run("changed PLAN bytes rejected", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		path := filepath.Join(fixture.root, filepath.FromSlash(fixture.planRef))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err == nil {
			t.Fatal("PLAN whose bytes differ from the approved activation was accepted")
		}
	})

	t.Run("missing activation rejected", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(fixture.activationRef))); err != nil {
			t.Fatal(err)
		}
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err == nil {
			t.Fatal("missing original activation was accepted")
		}
	})

	t.Run("changed activation rejected", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		path := filepath.Join(fixture.root, filepath.FromSlash(fixture.activationRef))
		var activation map[string]any
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &activation); err != nil {
			t.Fatal(err)
		}
		activation["approved_readback_sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
		if err := os.WriteFile(path, mustBlockedJSON(t, activation), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err == nil {
			t.Fatal("activation with a changed PLAN digest was accepted")
		}
	})

	t.Run("one shot is not activated plan authorization", func(t *testing.T) {
		fixture := newBlockedPlanFixture(t)
		fixture.agent["dispatch_mode"] = "one_shot"
		if err := validateBlockedReadOnly(t, fixture, "agent-s9"); err == nil {
			t.Fatal("one_shot dispatch was accepted as an activated PLAN chain")
		}
	})
}

type blockedPlanFixture struct {
	root          string
	state         map[string]any
	agent         map[string]any
	planRef       string
	activationRef string
}

func newBlockedPlanFixture(t *testing.T) *blockedPlanFixture {
	t.Helper()
	const (
		runtimeID     = "loop-REQ-123"
		teamID        = "workgroup-s9"
		agentID       = "agent-s9"
		taskID        = "TASK-123"
		assignID      = "assignment-s9-unit"
		planRef       = "plans/plan.json"
		activationRef = "plans/activation.json"
	)
	root := t.TempDir()

	manifest := decodeBlockedAsset(t, "team-manifest.example.json")
	manifest["manifest_id"] = "team-manifest-s9"
	manifest["runtime_id"] = runtimeID
	manifest["req_id"] = "REQ-123"
	manifest["workgroup_id"] = teamID
	manifest["workgroup_kind"] = "builder"
	manifest["planned_agent_count"] = 1
	manifest["max_parallel_agents"] = 1
	manifest["separation_edges"] = []any{}
	assignments, ok := manifest["assignments"].([]any)
	if !ok || len(assignments) == 0 {
		t.Fatal("embedded team manifest has no assignments")
	}
	assignment, ok := assignments[0].(map[string]any)
	if !ok {
		t.Fatal("embedded team manifest assignment is not an object")
	}
	assignment["assignment_id"] = assignID
	assignment["role_family"] = "backend-builder"
	assignment["agent_id"] = agentID
	manifest["assignments"] = []any{assignment}
	writeBlockedJSON(t, root, "manifest.json", manifest)

	plan := decodeBlockedMessage(t, "plan_report")
	plan["runtime_id"] = runtimeID
	plan["agent_id"] = agentID
	plan["task_id"] = taskID
	plan["team_id"] = teamID
	plan["assignment_id"] = assignID
	plan["assignment_revision"] = 1
	plan["expected_runtime_revision"] = 7
	planBytes := mustBlockedJSON(t, plan)
	writeBlockedBytes(t, root, planRef, planBytes)

	activation := decodeBlockedMessage(t, "activation")
	activation["runtime_id"] = runtimeID
	activation["agent_id"] = agentID
	activation["task_id"] = taskID
	activation["team_id"] = teamID
	activation["approved_readback_message_id"] = plan["message_id"]
	activation["approved_readback_sha256"] = blockedSHA256(planBytes)
	writeBlockedJSON(t, root, activationRef, activation)

	agent := map[string]any{
		"id": agentID, "state": "blocked", "dispatch_mode": "plan_checkpoint",
		"team_id": teamID, "task_ids": []any{taskID},
		"readback_ref": planRef, "activation_ref": activationRef,
	}
	state := map[string]any{
		"runtime_id": runtimeID,
		"review":     map[string]any{},
		"entities": map[string]any{
			"teams":  []any{map[string]any{"id": teamID, "manifest_ref": "manifest.json", "agent_ids": []any{agentID}}},
			"agents": []any{agent},
		},
	}
	return &blockedPlanFixture{root: root, state: state, agent: agent, planRef: planRef, activationRef: activationRef}
}

func validateBlockedReadOnly(t *testing.T, fixture *blockedPlanFixture, owner string) error {
	t.Helper()
	before, err := json.Marshal(fixture.state)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateBlockedPlanAuthorization(fixture.root, fixture.state, owner)
	after, marshalErr := json.Marshal(fixture.state)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("blocked authorization validation mutated Runtime state")
	}
	return err
}

func decodeBlockedAsset(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset(name)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func decodeBlockedMessage(t *testing.T, messageType string) map[string]any {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message["message_type"] == messageType {
			return message
		}
	}
	t.Fatalf("embedded message examples have no %s", messageType)
	return nil
}

func writeBlockedJSON(t *testing.T, root, ref string, value any) {
	t.Helper()
	writeBlockedBytes(t, root, ref, mustBlockedJSON(t, value))
}

func writeBlockedBytes(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustBlockedJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func blockedSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
