package qualitygate_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestCurrentBuilderReceiptMatrix(t *testing.T) {
	for _, name := range []string{"valid", "no report", "other owner", "wrong assignment", "old generation", "invalidated", "corrupt report", "missing report", "TASK drift", "wrong subject", "wrong producer", "blocked TASK", "empty checks", "failed check", "not run", "missing required command", "scope deviation", "missing closing", "rework", "duplicate index"} {
		t.Run(name, func(t *testing.T) {
			b := newBatchP0Input(t)
			const taskID = "TASK-TEST-01"
			ref := "evidence/ev-completion-" + taskID + ".json"
			b.addCompletion(t, taskID, func(env map[string]any) {
				env["checks"] = []any{map[string]any{"name": "owned", "command": "go test ./...", "result": "pass"}}
				if name == "empty checks" {
					env["checks"] = []any{}
				}
				if name == "failed check" || name == "not run" {
					result := "fail"
					if name == "not run" {
						result = "not_run"
					}
					env["checks"] = []any{map[string]any{"name": "owned", "command": "go test ./...", "result": result}}
				}
				if name == "wrong subject" {
					env["subject_refs"] = []any{}
				}
				if name == "wrong producer" {
					env["producer_agent_id"] = "peer"
				}
				if name == "scope deviation" {
					env["scope_deviations"] = []any{"outside owned scope"}
				}
			})
			entities := b.input.Snapshot.State["entities"].(map[string]any)
			task := entities["tasks"].([]any)[0].(map[string]any)
			task["owner_agent_ids"], task["completion_report_ref"] = []any{"builder-1"}, ref
			agent := map[string]any{"id": "builder-1", "state": "reported", "task_ids": []any{taskID}, "prompt_ref": "manifest#assignment-1", "completion_reported_ref": ref}
			entities["agents"] = []any{agent}
			req := qualitygate.BuilderReceiptRequest{Owner: "builder-1", AssignmentID: "assignment-1", TaskID: taskID, CompletionRef: ref, RequiredChecks: []string{"go test ./..."}, DoneWhen: []string{"approved owned assertion"}}
			index := b.input.Snapshot.State["evidence"].([]any)[0].(map[string]any)
			switch name {
			case "no report", "rework":
				delete(agent, "completion_reported_ref")
				delete(task, "completion_report_ref")
				req.CompletionRef = ""
			case "other owner":
				req.Owner = "peer"
			case "wrong assignment":
				req.AssignmentID = "other"
			case "old generation":
				index["baseline_generation"] = 2
			case "invalidated":
				index["invalidated_by"] = "change-1"
			case "corrupt report":
				b.files[ref] = []byte("{}")
			case "missing report":
				delete(b.files, ref)
			case "TASK drift":
				b.files["docs/dev/tasks/"+taskID+".md"] = []byte("changed")
			case "blocked TASK":
				task["state"] = "blocked"
			case "missing required command":
				req.RequiredChecks = []string{"required additional test"}
			case "missing closing":
				req.DoneWhen = nil
			case "duplicate index":
				b.input.Snapshot.State["evidence"] = append(b.input.Snapshot.State["evidence"].([]any), index)
			}
			before, _ := json.Marshal(b.input.Snapshot.State)
			r := qualitygate.CurrentBuilderReceipt(b.input, req)
			after, _ := json.Marshal(b.input.Snapshot.State)
			if string(before) != string(after) {
				t.Fatal("receipt mutated Runtime")
			}
			if r.WorkComplete != (name == "valid") || r.Integrated {
				t.Fatalf("unexpected receipt: %+v", r)
			}
			if name == "valid" {
				if !r.Accepted || len(r.OwnPending) != 0 || len(r.WaitingOn) != 1 || r.WaitingOn[0].Owner != "main" {
					t.Fatalf("Main waiting confused with own pending: %+v", r)
				}
			} else if len(r.OwnPending)+len(r.WaitingOn) == 0 {
				t.Fatalf("missing own reason: %+v", r)
			}
			data, _ := json.Marshal(r)
			if err := schema.NewEmbeddedValidator().ValidateBytes("builder-receipt.schema.json", data); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "execute_checks") {
				t.Fatal("query claimed execution")
			}
		})
	}
}

func TestCurrentBuilderReceiptIntegrationIdentityAndReachability(t *testing.T) {
	qualityGit := func(t *testing.T, root string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=Receipt Test", "-c", "user.email=receipt@example.com"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	b := newBatchP0Input(t)
	root := t.TempDir()
	b.input.Root = root
	qualityGit(t, root, "init", "--initial-branch=develop")
	os.WriteFile(filepath.Join(root, "base"), []byte("base"), 0600)
	qualityGit(t, root, "add", "base")
	qualityGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(qualityGit(t, root, "rev-parse", "HEAD"))
	qualityGit(t, root, "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(root, "change"), []byte("change"), 0600)
	qualityGit(t, root, "add", "change")
	qualityGit(t, root, "commit", "-m", "change")
	qualityGit(t, root, "checkout", "develop")
	qualityGit(t, root, "merge", "--no-ff", "feature", "-m", "receive")
	merge := strings.TrimSpace(qualityGit(t, root, "rev-parse", "HEAD"))
	b.input.Snapshot.State["bound_req"] = map[string]any{"workspace": map[string]any{"dev_branch": "develop"}}
	const taskID = "TASK-TEST-01"
	ref := "evidence/ev-completion-" + taskID + ".json"
	b.addCompletion(t, taskID, func(env map[string]any) {
		env["created_at"] = "2026-10-13T01:00:00Z"
		env["checks"] = []any{map[string]any{"name": "owned", "command": "check", "result": "pass"}}
	})
	entities := b.input.Snapshot.State["entities"].(map[string]any)
	task := entities["tasks"].([]any)[0].(map[string]any)
	task["owner_agent_ids"], task["completion_report_ref"] = []any{"builder-1"}, ref
	entities["agents"] = []any{map[string]any{"id": "builder-1", "state": "reported", "task_ids": []any{taskID}, "prompt_ref": "manifest#assignment-1", "completion_reported_ref": ref}}
	req := qualitygate.BuilderReceiptRequest{Owner: "builder-1", AssignmentID: "assignment-1", TaskID: taskID, CompletionRef: ref, RequiredChecks: []string{"check"}, DoneWhen: []string{"owned assertion"}}
	checkpoint := ".claude/evidence/loop-test/g1/worktree/assignment-1/checkpoint.json"
	baseline := map[string]any{"task_id": taskID, "assignment_id": "assignment-1", "baseline_generation": 1, "state": "verified", "verified_at": "2026-10-13T02:00:00Z", "completion_report_path": ref, "completion_report_sha256": sha256Hex(b.files[ref]), "target_branch": "develop", "merge_commit": merge}
	for _, name := range []string{"valid", "task", "assignment", "generation", "report path", "report SHA", "timestamp", "state", "target", "merge", "unreachable"} {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(baseline)
			var cp map[string]any
			json.Unmarshal(body, &cp)
			switch name {
			case "task":
				cp["task_id"] = "peer"
			case "assignment":
				cp["assignment_id"] = "peer"
			case "generation":
				cp["baseline_generation"] = 2
			case "report path":
				cp["completion_report_path"] = "other"
			case "report SHA":
				cp["completion_report_sha256"] = "other"
			case "timestamp":
				cp["verified_at"] = "2026-10-13T00:00:00Z"
			case "state":
				cp["state"] = "merged"
			case "target":
				cp["target_branch"] = "feature"
			case "merge":
				delete(cp, "merge_commit")
			case "unreachable":
				qualityGit(t, root, "reset", "--hard", base)
			}
			b.files[checkpoint], _ = json.Marshal(cp)
			r := qualitygate.CurrentBuilderReceipt(b.input, req)
			if !r.WorkComplete || r.Integrated != (name == "valid") || len(r.OwnPending) != 0 {
				t.Fatalf("integration validity leaked into author work: %+v", r)
			}
			if name != "valid" && (len(r.WaitingOn) != 1 || r.WaitingOn[0].Owner != "main") {
				t.Fatalf("missing Main responsibility: %+v", r)
			}
		})
	}
}
