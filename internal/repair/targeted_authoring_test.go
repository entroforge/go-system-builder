package repair

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/targetedauthoring"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func targetedAuthoringFixture(t *testing.T) (string, map[string]any, map[string]any, map[string]any, map[string]any) {
	t.Helper()
	root := t.TempDir()
	original := "repair-assignment-unit"
	performing := VerificationAssignmentID(original)
	taskID := targetedauthoring.TaskID("session-1", performing)
	p := map[string]any{"case_id": "case-1", "contract_id": "contract-1", "session_id": "session-1", "plan_ref": ".claude/plan.json", "plan_sha256": strings.Repeat("a", 64), "contract_ref": ".claude/contract.json", "contract_sha256": strings.Repeat("b", 64), "assignment_owners": map[string]any{original: "builder", performing: "verifier"}}
	taskRow := map[string]any{"id": taskID, "path": ".claude/task.json", "owner_agent_ids": []any{"verifier"}}
	team := map[string]any{"id": "team-verify", "manifest_ref": ".claude/manifest.json"}
	agent := map[string]any{"id": "verifier", "team_id": "team-verify", "task_ids": []any{taskID}, "prompt_ref": ".claude/manifest.json#" + ManifestVerificationAssignmentID(original)}
	state := map[string]any{"runtime_id": "loop-1", "baseline": map[string]any{"generation": 1}, "review": map[string]any{"repair": p}, "entities": map[string]any{"tasks": []any{taskRow}, "teams": []any{team}, "agents": []any{agent}}}
	task := map[string]any{"task_id": taskID, "session_id": "session-1", "assignment_id": performing, "targeted_authoring": NewTargetedAuthoring(state, original, performing, "verifier")}
	manifest := map[string]any{"runtime_id": "loop-1", "baseline_generation": 1, "workgroup_id": "team-verify", "assignments": []any{map[string]any{"assignment_id": ManifestVerificationAssignmentID(original), "agent_id": "verifier", "write_paths": []any{}}}}
	writeTargetedSource(t, root, taskRow, "path", task)
	writeTargetedSource(t, root, team, "manifest_ref", manifest)
	return root, state, taskRow, team, task
}

func writeTargetedSource(t *testing.T, root string, row map[string]any, pathKey string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, row[pathKey].(string))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	digestKey := "sha256"
	if pathKey == "manifest_ref" {
		digestKey = "manifest_sha256"
	}
	row[digestKey] = workpacket.Digest(data)
}

func TestTargetedAuthoringUsesOriginalRegisteredAuthorities(t *testing.T) {
	root, state, _, _, _ := targetedAuthoringFixture(t)
	normal, err := UsesSharedTargetedAuthoring(root, state, "repair-assignment-unit", VerificationAssignmentID("repair-assignment-unit"), "verifier")
	if err != nil || !normal {
		t.Fatalf("normal=%v err=%v", normal, err)
	}
}

func TestTargetedAuthoringRejectsChangedOrMismatchedSources(t *testing.T) {
	for _, name := range []string{"removed_declaration_without_digest", "manifest_changed", "unknown_protocol", "wrong_owner", "wrong_session", "product_write", "duplicate_agent", "wrong_task_owner", "missing_agent", "empty_task_ids", "duplicate_original_task"} {
		t.Run(name, func(t *testing.T) {
			root, state, taskRow, team, task := targetedAuthoringFixture(t)
			switch name {
			case "removed_declaration_without_digest":
				delete(task, "targeted_authoring")
				data, _ := json.Marshal(task)
				os.WriteFile(filepath.Join(root, taskRow["path"].(string)), data, 0644)
			case "manifest_changed":
				os.WriteFile(filepath.Join(root, team["manifest_ref"].(string)), []byte(`{}`), 0644)
			case "unknown_protocol":
				c := task["targeted_authoring"].(TargetedAuthoring)
				c.Protocol = "future"
				task["targeted_authoring"] = c
				writeTargetedSource(t, root, taskRow, "path", task)
			case "missing_agent":
				state["entities"].(map[string]any)["agents"] = []any{}
			case "empty_task_ids":
				state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)["task_ids"] = []any{}
			case "duplicate_original_task":
				a := state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
				a["task_ids"] = append(a["task_ids"].([]any), a["task_ids"].([]any)[0])
			case "wrong_task_owner":
				taskRow["owner_agent_ids"] = []any{"builder"}
			case "wrong_owner":
				c := task["targeted_authoring"].(TargetedAuthoring)
				c.Owner = "builder"
				task["targeted_authoring"] = c
				writeTargetedSource(t, root, taskRow, "path", task)
			case "wrong_session":
				task["session_id"] = "old-session"
				writeTargetedSource(t, root, taskRow, "path", task)
			case "product_write":
				data, _ := os.ReadFile(filepath.Join(root, team["manifest_ref"].(string)))
				var m map[string]any
				json.Unmarshal(data, &m)
				m["assignments"].([]any)[0].(map[string]any)["write_paths"] = []any{"src"}
				writeTargetedSource(t, root, team, "manifest_ref", m)
			case "duplicate_agent":
				e := state["entities"].(map[string]any)
				e["agents"] = append(e["agents"].([]any), e["agents"].([]any)[0])
			}
			normal, err := UsesSharedTargetedAuthoring(root, state, "repair-assignment-unit", VerificationAssignmentID("repair-assignment-unit"), "verifier")
			if err == nil || normal {
				t.Fatalf("changed source accepted normal=%v err=%v", normal, err)
			}
		})
	}
}

func TestTargetedAuthoringLegacySourceRetainsItsProtocol(t *testing.T) {
	root, state, row, _, task := targetedAuthoringFixture(t)
	delete(task, "targeted_authoring")
	writeTargetedSource(t, root, row, "path", task)
	normal, err := UsesSharedTargetedAuthoring(root, state, "repair-assignment-unit", VerificationAssignmentID("repair-assignment-unit"), "verifier")
	if err != nil || normal {
		t.Fatalf("legacy normal=%v err=%v", normal, err)
	}
}

func TestTargetedControlPathsRetainOriginalHistoryWithoutReadingUnrelatedTasks(t *testing.T) {
	root, state, _, _, _ := targetedAuthoringFixture(t)
	contract := NewTargetedAuthoring(state, "repair-assignment-unit", VerificationAssignmentID("repair-assignment-unit"), "verifier")
	path, err := contract.AnswerRelativePath()
	if err != nil {
		t.Fatal(err)
	}
	entities := state["entities"].(map[string]any)
	entities["tasks"] = append(entities["tasks"].([]any), map[string]any{"id": "TASK-unrelated", "path": "missing-unrelated-task.json", "sha256": "invalid"})
	// A later repair responsibility must not turn the original private file into
	// an unclassified product file or grant the old author a new responsibility.
	state["review"].(map[string]any)["repair"] = map[string]any{"session_id": "later-session", "assignment_owners": map[string]any{}}
	sibling := filepath.ToSlash(filepath.Join(filepath.Dir(path), "unknown-product.json"))
	paths, err := targetedauthoring.ControlPaths(root, state, []string{path, sibling})
	if err != nil || len(paths) != 1 || paths[0] != path {
		t.Fatalf("original history proof %v: %v", paths, err)
	}
	if normal, err := UsesSharedTargetedAuthoring(root, state, contract.OriginalAssignmentID, contract.PerformingAssignmentID, contract.Owner); err == nil || normal {
		t.Fatalf("historical control proof granted current reception: %v %v", normal, err)
	}
	paths, err = targetedauthoring.ControlPaths(root, state, []string{sibling})
	if err != nil || len(paths) != 0 {
		t.Fatalf("unknown sibling gained control proof %v: %v", paths, err)
	}
}

// The current register rejects reusing an Agent ID. Even if a historical or
// repaired projection contains other TASK references, they cannot select or
// block this exact original responsibility.
func TestTargetedAuthoringSelectsExactTaskBeforeOpeningOtherReferences(t *testing.T) {
	root, state, _, _, _ := targetedAuthoringFixture(t)
	entities := state["entities"].(map[string]any)
	agent := entities["agents"].([]any)[0].(map[string]any)
	agent["task_ids"] = append(agent["task_ids"].([]any), "TASK-900000000")
	entities["tasks"] = append(entities["tasks"].([]any), map[string]any{"id": "TASK-900000000", "path": "unrelated-unavailable-task.json", "sha256": "invalid", "owner_agent_ids": []any{"verifier"}})
	normal, err := UsesSharedTargetedAuthoring(root, state, "repair-assignment-unit", VerificationAssignmentID("repair-assignment-unit"), "verifier")
	if err != nil || !normal {
		t.Fatalf("unrelated TASK changed exact source qualification: %v %v", normal, err)
	}
}

func TestTargetedSourceCancellationIsNotLegacyFallback(t *testing.T) {
	root, state, _, _, _ := targetedAuthoringFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if normal, err := UsesSharedTargetedAuthoring(root, state, "repair-assignment-unit", VerificationAssignmentID("repair-assignment-unit"), "verifier", ctx); !errors.Is(err, context.Canceled) || normal {
		t.Fatalf("cancelled source %v: %v", normal, err)
	}
	if _, err := targetedauthoring.ControlPaths(root, state, nil, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled control proof: %v", err)
	}
}
