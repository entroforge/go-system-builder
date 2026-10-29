package qualitygate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestPlannedProgressRequiresCurrentReportAndAssignment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "manifest"), []byte(`{"schema_version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "init", "--initial-branch=develop")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "add", "base.txt")
	qualityGit(t, root, "commit", "-m", "base")
	mergeCommit := strings.TrimSpace(qualityGit(t, root, "rev-parse", "HEAD"))
	put := func(p string, v any) []byte {
		t.Helper()
		b, _ := json.Marshal(v)
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755)
		if e := os.WriteFile(filepath.Join(root, p), b, 0644); e != nil {
			t.Fatal(e)
		}
		return b
	}
	report := ".claude/result.json"
	checkpoint := ".claude/evidence/run/g1/worktree/a1/checkpoint.json"
	env := map[string]any{"evidence_id": "E1", "kind": "completion_report", "task_id": "TASK-042-01", "runtime_id": "run", "baseline_generation": 1, "producer_agent_id": "builder", "conclusion": "completed", "checks": []any{map[string]any{"name": "test", "result": "pass"}}, "created_at": "2026-09-19T10:00:00Z"}
	b := put(report, env)
	cp := map[string]any{"task_id": "TASK-042-01", "assignment_id": "a1", "baseline_generation": 1, "state": "verified", "verified_at": "2026-09-19T11:00:00Z"}
	cp["completion_report_path"] = report
	cp["completion_report_sha256"] = sha256Hex(b)
	cp["target_branch"] = "develop"
	cp["merge_commit"] = mergeCommit
	put(checkpoint, cp)
	task := map[string]any{"id": "TASK-042-01", "state": "review", "owner_agent_ids": []any{"builder"}, "completion_report_ref": report}
	index := map[string]any{"id": "E1", "path": report, "kind": "completion_report", "status": "valid", "baseline_generation": 1, "sha256": sha256Hex(b)}
	agent := map[string]any{"id": "builder", "role": "backend-builder", "state": "reported", "task_ids": []any{"TASK-042-01"}, "team_id": "team-builder", "definition_ref": "agents/backend-builder.md", "prompt_ref": "manifest#a1"}
	team := map[string]any{"id": "team-builder", "kind": "builder", "status": "planned", "manifest_ref": "manifest", "agent_ids": []any{"builder"}}
	state := map[string]any{"runtime_id": "run", "lifecycle": map[string]any{"state": "building"}, "baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"workspace": map[string]any{"dev_branch": "develop"}}, "entities": map[string]any{"tasks": []any{task}, "agents": []any{agent}, "teams": []any{team}}, "evidence": []any{index}}
	input := Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: fileview.Disk{Root: root}}
	check := func(want workstate.Status, integrated, deliveryRecorded bool) {
		t.Helper()
		got := PlannedBuilderProgress(input)["TASK-042-01"]
		if got.State != want || got.Integrated != integrated || got.DeliveryRecorded != deliveryRecorded {
			t.Fatalf("want state=%s integrated=%t delivery_recorded=%t, got %+v", want, integrated, deliveryRecorded, got)
		}
	}
	check(workstate.Done, true, true)
	delete(cp, "completion_report_sha256")
	put(checkpoint, cp)
	check(workstate.Active, false, true) // Old checkpoints need fresh verification for planned dispatch.
	cp["completion_report_sha256"] = sha256Hex(b)
	cp["completion_report_path"] = ".claude/other-result.json"
	put(checkpoint, cp)
	check(workstate.Active, false, true)
	cp["completion_report_path"] = report
	put(checkpoint, cp)
	check(workstate.Done, true, true)
	agent["prompt_ref"] = "manifest#a2"
	check(workstate.Active, false, true)
	agent["prompt_ref"] = "manifest#a1"
	index["invalidated_by"] = "superseded"
	check(workstate.Active, false, true)
	delete(index, "invalidated_by")
	env["created_at"] = "2026-09-19T12:00:00Z"
	b = put(report, env)
	index["sha256"] = sha256Hex(b)
	check(workstate.Active, false, true)
	env["created_at"] = "2026-09-19T10:00:00Z"
	env["checks"] = []any{map[string]any{"result": "fail"}}
	b = put(report, env)
	index["sha256"] = sha256Hex(b)
	check(workstate.Active, false, true)
	task["state"] = "blocked"
	check(workstate.Blocked, false, false)
	task["state"] = "cancelled"
	check(workstate.Cancelled, false, false)
}

func TestPlannedProgressRejectsUnreachableMergeReceipt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "manifest"), []byte(`{"schema_version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "init", "--initial-branch=develop")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "add", "base.txt")
	qualityGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(qualityGit(t, root, "rev-parse", "HEAD"))
	qualityGit(t, root, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "add", "feature.txt")
	qualityGit(t, root, "commit", "-m", "feature")
	qualityGit(t, root, "checkout", "develop")
	qualityGit(t, root, "merge", "--no-ff", "feature", "-m", "receive feature")
	mergeCommit := strings.TrimSpace(qualityGit(t, root, "rev-parse", "HEAD"))
	qualityGit(t, root, "reset", "--hard", base)

	put := func(p string, v any) []byte {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), b, 0o644); err != nil {
			t.Fatal(err)
		}
		return b
	}
	report := ".claude/result.json"
	env := map[string]any{
		"evidence_id": "E-receipt", "kind": "completion_report", "task_id": "TASK-RECEIPT",
		"runtime_id": "run", "baseline_generation": 1, "producer_agent_id": "builder",
		"conclusion": "completed", "checks": []any{map[string]any{"name": "test", "result": "pass"}},
		"created_at": "2026-09-19T10:00:00Z",
	}
	reportBytes := put(report, env)
	checkpoint := ".claude/evidence/run/g1/worktree/a-receipt/checkpoint.json"
	put(checkpoint, map[string]any{
		"task_id": "TASK-RECEIPT", "assignment_id": "a-receipt", "baseline_generation": 1,
		"state": "verified", "verified_at": "2026-09-19T11:00:00Z",
		"completion_report_path": report, "completion_report_sha256": sha256Hex(reportBytes),
		"target_branch": "develop", "merge_commit": mergeCommit,
	})
	state := map[string]any{
		"runtime_id": "run", "baseline": map[string]any{"generation": 1},
		"lifecycle": map[string]any{"state": "building"},
		"bound_req": map[string]any{"workspace": map[string]any{"dev_branch": "develop"}},
		"entities": map[string]any{
			"tasks":  []any{map[string]any{"id": "TASK-RECEIPT", "state": "review", "owner_agent_ids": []any{"builder"}, "completion_report_ref": report}},
			"agents": []any{map[string]any{"id": "builder", "role": "backend-builder", "state": "reported", "task_ids": []any{"TASK-RECEIPT"}, "team_id": "team-builder", "definition_ref": "agents/backend-builder.md", "prompt_ref": "manifest#a-receipt"}},
			"teams":  []any{map[string]any{"id": "team-builder", "kind": "builder", "status": "planned", "manifest_ref": "manifest", "agent_ids": []any{"builder"}}},
		},
		"evidence": []any{map[string]any{"id": "E-receipt", "path": report, "kind": "completion_report", "status": "valid", "baseline_generation": 1, "sha256": sha256Hex(reportBytes)}},
	}
	input := Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: fileview.Disk{Root: root}}
	if got := PlannedBuilderProgress(input)["TASK-RECEIPT"]; got.State != workstate.Active || got.Integrated || !got.DeliveryRecorded {
		t.Fatalf("unreachable merge receipt released dispatch: %+v", got)
	}

	// A normal post-delivery commit keeps the receipt valid; the guard must
	// check ancestry rather than requiring target HEAD to equal the receipt.
	qualityGit(t, root, "reset", "--hard", mergeCommit)
	if err := os.WriteFile(filepath.Join(root, "post-delivery.txt"), []byte("follow-up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "add", "post-delivery.txt")
	qualityGit(t, root, "commit", "-m", "post-delivery maintenance")
	if got := PlannedBuilderProgress(input)["TASK-RECEIPT"]; got.State != workstate.Done || !got.Integrated || !got.DeliveryRecorded {
		t.Fatalf("descendant target incorrectly invalidated receipt: %+v", got)
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
	reviewerManifest, err := os.ReadFile(reviewerManifestPath)
	if err != nil {
		t.Fatal(err)
	}
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
	if progress, ok := got["TASK-BUILD"]; !ok || progress.State != workstate.Active || progress.Reason != "registered Builder owner" {
		t.Fatalf("registered Builder task missing from S6 capacity: %+v (present=%t)", progress, ok)
	}
	if progress, ok := got["TASK-UNKNOWN"]; !ok || progress.State != workstate.Active || progress.Reason != "Builder owner identity unresolved" {
		t.Fatalf("unresolved identity must retain S6 capacity: %+v (present=%t)", progress, ok)
	}
	if progress, ok := got["TASK-CANCELLED"]; !ok || progress.State != workstate.Cancelled {
		t.Fatalf("cancellation should remain visible outside owner qualification: %+v (present=%t)", progress, ok)
	}

	// The actual S5→S6 path leaves completed reviewers on TASK-001 after their
	// original Team is retired. Their exact manifest assignment proves they
	// are review responsibility, not unresolved Builder ownership.
	if err := os.WriteFile(reviewerManifestPath, reviewerManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	reviewerAgent["state"] = "cancelled"
	reviewerTeam["status"] = "retired"
	if progress, ok := PlannedBuilderProgress(input)["TASK-REVIEW"]; ok {
		t.Fatalf("historical S5 reviewer with an exact retired manifest occupied S6 Builder capacity: %+v", progress)
	}

	// Original register-workgroup appends the newly registered Builder to the
	// existing TASK owner list. The old S5 owners must not make that first S6
	// projection unresolved.
	reviewerTask["owner_agent_ids"] = []any{"reviewer-s5", "builder-s6"}
	builderAgent["task_ids"] = []any{"TASK-BUILD", "TASK-REVIEW"}
	if progress, ok := PlannedBuilderProgress(input)["TASK-REVIEW"]; !ok || progress.State != workstate.Active || progress.Reason != "registered Builder owner" {
		t.Fatalf("registered S6 Builder could not take ownership alongside historical S5 reviewers: %+v (present=%t)", progress, ok)
	}

	// Retired Builder ownership is still unresolved and must not disappear from
	// the S6 projection just because its Team is no longer live.
	builderTeam["status"] = "retired"
	if progress, ok := PlannedBuilderProgress(input)["TASK-BUILD"]; !ok || progress.State != workstate.Active || progress.Reason != "Builder owner identity unresolved" {
		t.Fatalf("retired Builder owner must remain visible as unresolved: %+v (present=%t)", progress, ok)
	}
	builderTeam["status"] = "planned"

	state["lifecycle"] = map[string]any{"state": "document_verification"}
	if got := PlannedBuilderProgress(input); len(got) != 1 || got["TASK-CANCELLED"].State != workstate.Cancelled {
		t.Fatalf("non-S6 work should not create Builder capacity, while cancellation remains visible: %+v", got)
	}

	// A contradictory Team/Agent role pair is unknown ownership, never a reason
	// to omit capacity or to classify the owner as a reviewer.
	state["lifecycle"] = map[string]any{"state": "building"}
	reviewerAgent["role"] = "backend-builder"
	if progress := PlannedBuilderProgress(input)["TASK-REVIEW"]; progress.State != workstate.Active || progress.Reason != "Builder owner identity unresolved" {
		t.Fatalf("contradictory owner identity should retain unresolved capacity: %+v", progress)
	}
}

func TestVerifiedIntegrationRejectsReportBoundCheckpointWithoutMergeReceipt(t *testing.T) {
	root := t.TempDir()
	checkpointPath := filepath.Join(root, ".claude", "evidence", "run", "g1", "worktree", "assignment-verified", "checkpoint.json")
	if err := os.MkdirAll(filepath.Dir(checkpointPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"task_id":                  "TASK-STALE",
		"assignment_id":            "assignment-verified",
		"baseline_generation":      1,
		"state":                    "verified",
		"completion_report_path":   ".claude/result.json",
		"completion_report_sha256": "report-sha",
		"target_branch":            "develop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpointPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	input := Input{
		Root: root,
		Snapshot: runtime.Snapshot{State: map[string]any{
			"runtime_id": "run",
			"baseline":   map[string]any{"generation": 1},
			"bound_req":  map[string]any{"workspace": map[string]any{"dev_branch": "develop"}},
		}},
		Files: fileview.Disk{Root: root},
	}
	if got := verifiedIntegrationTaskIDs(input)["TASK-STALE"]; got {
		t.Fatal("report-bound checkpoint without merge receipt released a successor")
	}
}

func qualityGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", root, "-c", "user.name=qualitygate-test", "-c", "user.email=qualitygate-test@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
