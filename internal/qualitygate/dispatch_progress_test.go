package qualitygate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestPlannedProgressDoesNotInferCompletionWithoutCurrentAssignment(t *testing.T) {
	state := map[string]any{
		"lifecycle": map[string]any{"state": "building"},
		"entities":  map[string]any{"tasks": []any{map[string]any{"id": "TASK-042-01", "state": "active", "owner_agent_ids": []any{"builder"}}}},
	}
	got := PlannedBuilderProgress(Input{Snapshot: runtime.Snapshot{State: state}})["TASK-042-01"]
	if !got.Unknown || got.State != "" || got.DeliveryRecorded || got.Integrated {
		t.Fatalf("unknown assignment became completion: %+v", got)
	}
}

func TestPlannedBuilderProgressUsesRegisteredBuilderOwnership(t *testing.T) {
	makeOwner := func(id, taskID, teamID, role, kind string, status string) (map[string]any, map[string]any) {
		manifest := "manifests/" + id + ".json"
		agent := map[string]any{
			"id": id, "role": role, "state": "working", "task_ids": []any{taskID}, "team_id": teamID,
			"definition_ref": "agents/" + role + ".md", "prompt_ref": manifest + "#assignment-" + id,
		}
		team := map[string]any{"id": teamID, "kind": kind, "status": status, "manifest_ref": manifest, "agent_ids": []any{id}}
		return agent, team
	}

	reviewerAgent, reviewerTeam := makeOwner("reviewer-s5", "TASK-REVIEW", "team-review", "document-verifier", "document_verifier", "active")
	builderAgent, builderTeam := makeOwner("builder-s6", "TASK-BUILD", "team-builder", "backend-builder", "builder", "planned")
	unresolvedAgent, unresolvedTeam := makeOwner("agent-unknown", "TASK-UNKNOWN", "team-unknown", "unknown-role", "unknown-kind", "active")
	reviewerTask := map[string]any{"id": "TASK-REVIEW", "state": "in_progress", "owner_agent_ids": []any{"reviewer-s5"}}
	builderTask := map[string]any{"id": "TASK-BUILD", "state": "in_progress", "owner_agent_ids": []any{"builder-s6"}}
	unknownTask := map[string]any{"id": "TASK-UNKNOWN", "state": "in_progress", "owner_agent_ids": []any{"agent-unknown"}}
	cancelledTask := map[string]any{"id": "TASK-CANCELLED", "state": "cancelled"}
	state := map[string]any{
		"lifecycle": map[string]any{"state": "building"},
		"entities": map[string]any{
			"tasks":  []any{reviewerTask, builderTask, unknownTask, cancelledTask},
			"agents": []any{reviewerAgent, builderAgent, unresolvedAgent},
			"teams":  []any{reviewerTeam, builderTeam, unresolvedTeam},
		},
	}
	root := t.TempDir()
	for _, source := range []struct {
		path, id, role, kind string
	}{
		{"manifests/reviewer-s5.json", "reviewer-s5", "document-verifier", "document_verifier"},
		{"manifests/builder-s6.json", "builder-s6", "backend-builder", "builder"},
		{"manifests/agent-unknown.json", "agent-unknown", "unknown-role", "unknown-kind"},
	} {
		data := fmt.Sprintf(`{"schema_version":"1.0.0","workgroup_kind":%q,"assignments":[{"assignment_id":%q,"agent_id":%q,"role_family":%q}]}`,
			source.kind, "assignment-"+source.id, source.id, source.role)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, source.path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, source.path), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reviewerManifestPath := filepath.Join(root, "manifests/reviewer-s5.json")
	input := Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: fileview.Disk{Root: root}}
	// Active legacy reviewers keep the original role/team classification; the
	// extra source-assignment check is only needed to recognize retired history.
	if err := os.WriteFile(reviewerManifestPath, []byte(`{"workgroup_kind":"document_verifier","assignments":[{"assignment_id":"assignment-reviewer-s5","agent_id":"reviewer-s5","role_family":"different-role"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := PlannedBuilderProgress(input)
	if _, ok := got["TASK-REVIEW"]; ok {
		t.Fatalf("known S5 reviewer-only task occupied S6 Builder capacity: %+v", got["TASK-REVIEW"])
	}
	if progress, ok := got["TASK-BUILD"]; !ok || progress.State != workstate.Active || progress.Reason != "current Builder responsibility is not yet available" {
		t.Fatalf("registered Builder task missing from S6 capacity: %+v (present=%t)", progress, ok)
	}
	if progress, ok := got["TASK-UNKNOWN"]; !ok || !progress.Unknown || progress.State != "" {
		t.Fatalf("unresolved identity must remain visible without taking Builder capacity: %+v (present=%t)", progress, ok)
	}
	if progress, ok := got["TASK-CANCELLED"]; !ok || progress.State != workstate.Cancelled {
		t.Fatalf("cancellation should remain visible outside owner qualification: %+v (present=%t)", progress, ok)
	}
	reviewerTeam["status"] = "retired"
	if _, ok := PlannedBuilderProgress(input)["TASK-REVIEW"]; ok {
		t.Fatal("retired S5 reviewer occupied Builder capacity")
	}
	// A known historical reviewer must not hide a real current Builder.
	refOwners := reviewerTask["owner_agent_ids"]
	refTasks := builderAgent["task_ids"]
	reviewerTask["owner_agent_ids"] = []any{"reviewer-s5", "builder-s6"}
	builderAgent["task_ids"] = []any{"TASK-BUILD", "TASK-REVIEW"}
	if progress := PlannedBuilderProgress(input)["TASK-REVIEW"]; progress.State != workstate.Active {
		t.Fatalf("historical reviewer hid current Builder: %+v", progress)
	}
	reviewerTask["owner_agent_ids"], builderAgent["task_ids"] = refOwners, refTasks
	reviewerTeam["status"] = "active"

	state["lifecycle"] = map[string]any{"state": "document_verification"}
	if got := PlannedBuilderProgress(input); len(got) != 1 || got["TASK-CANCELLED"].State != workstate.Cancelled {
		t.Fatalf("non-S6 work should not create Builder capacity, while cancellation remains visible: %+v", got)
	}

	// A contradictory Team/Agent role pair is unknown ownership, never a reason
	// to omit capacity or to classify the owner as a reviewer.
	state["lifecycle"] = map[string]any{"state": "building"}
	reviewerAgent["role"] = "backend-builder"
	if progress := PlannedBuilderProgress(input)["TASK-REVIEW"]; !progress.Unknown || progress.State != "" {
		t.Fatalf("contradictory owner identity should remain unknown: %+v", progress)
	}
}
