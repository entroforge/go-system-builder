package qualitygate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestPlannedProgressRequiresCurrentReportAndAssignment(t *testing.T) {
	root := t.TempDir()
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
	agent := map[string]any{"id": "builder", "state": "reported", "prompt_ref": "manifest#a1"}
	state := map[string]any{"runtime_id": "run", "baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"workspace": map[string]any{"dev_branch": "develop"}}, "entities": map[string]any{"tasks": []any{task}, "agents": []any{agent}}, "evidence": []any{index}}
	input := Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: fileview.Disk{Root: root}}
	check := func(want string) {
		t.Helper()
		if got := PlannedBuilderProgress(input)["TASK-042-01"].State; got != want {
			t.Fatalf("want %s got %s", want, got)
		}
	}
	check("integrated")
	delete(cp, "completion_report_sha256")
	put(checkpoint, cp)
	check("reported") // Old checkpoints need fresh verification for planned dispatch.
	cp["completion_report_sha256"] = sha256Hex(b)
	cp["completion_report_path"] = ".claude/other-result.json"
	put(checkpoint, cp)
	check("reported")
	cp["completion_report_path"] = report
	put(checkpoint, cp)
	check("integrated")
	agent["prompt_ref"] = "manifest#a2"
	check("reported")
	agent["prompt_ref"] = "manifest#a1"
	index["invalidated_by"] = "superseded"
	check("reported")
	delete(index, "invalidated_by")
	env["created_at"] = "2026-09-19T12:00:00Z"
	b = put(report, env)
	index["sha256"] = sha256Hex(b)
	check("reported")
	env["created_at"] = "2026-09-19T10:00:00Z"
	env["checks"] = []any{map[string]any{"result": "fail"}}
	b = put(report, env)
	index["sha256"] = sha256Hex(b)
	check("reported")
	task["state"] = "blocked"
	check("blocked")
}

func TestPlannedProgressRejectsUnreachableMergeReceipt(t *testing.T) {
	root := t.TempDir()
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
		"bound_req": map[string]any{"workspace": map[string]any{"dev_branch": "develop"}},
		"entities": map[string]any{
			"tasks":  []any{map[string]any{"id": "TASK-RECEIPT", "state": "review", "owner_agent_ids": []any{"builder"}, "completion_report_ref": report}},
			"agents": []any{map[string]any{"id": "builder", "state": "reported", "prompt_ref": "manifest#a-receipt"}},
		},
		"evidence": []any{map[string]any{"id": "E-receipt", "path": report, "kind": "completion_report", "status": "valid", "baseline_generation": 1, "sha256": sha256Hex(reportBytes)}},
	}
	input := Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: fileview.Disk{Root: root}}
	if got := PlannedBuilderProgress(input)["TASK-RECEIPT"].State; got != "reported" {
		t.Fatalf("unreachable merge receipt released dispatch: state=%q", got)
	}

	// A normal post-delivery commit keeps the receipt valid; the guard must
	// check ancestry rather than requiring target HEAD to equal the receipt.
	qualityGit(t, root, "reset", "--hard", mergeCommit)
	if err := os.WriteFile(filepath.Join(root, "post-delivery.txt"), []byte("follow-up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityGit(t, root, "add", "post-delivery.txt")
	qualityGit(t, root, "commit", "-m", "post-delivery maintenance")
	if got := PlannedBuilderProgress(input)["TASK-RECEIPT"].State; got != "integrated" {
		t.Fatalf("descendant target incorrectly invalidated receipt: state=%q", got)
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
