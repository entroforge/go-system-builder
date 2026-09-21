package req039_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS4ResultBindingThroughTaskIntegrate(t *testing.T) {
	root := freshRoot(t)
	seedIntegrableAssignment(t, root)
	// Count actual shell checks independently of report/checkpoint timestamps.
	counter := filepath.Join(t.TempDir(), "checks")
	manifestPath := filepath.Join(root, ".claude/workgroups/REQ-039/TASK-039-01/manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	command := "printf x >> '" + strings.ReplaceAll(counter, "'", "'\\''") + "'"
	manifest["assignments"].([]any)[0].(map[string]any)["required_checks"] = []string{command}
	manifestBytes, _ = json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	report := ".claude/evidence/loop-system-test/g1/assignments/assignment-ti/completion.json"
	cpPath := filepath.Join(root, ".claude/evidence/loop-system-test/g1/worktree/assignment-ti/checkpoint.json")
	check := func() map[string]any {
		t.Helper()
		code, out, errout := runTaskIntegrate(t, root, "assignment-ti")
		if code != 0 || strings.Contains(errout, "integration failed") {
			t.Fatalf("task-integrate: %d %s %s", code, out, errout)
		}
		b, err := os.ReadFile(cpPath)
		if err != nil {
			t.Fatal(err)
		}
		var cp map[string]any
		if err := json.Unmarshal(b, &cp); err != nil {
			t.Fatal(err)
		}
		rb, err := os.ReadFile(filepath.Join(root, report))
		if err != nil {
			t.Fatal(err)
		}
		if cp["state"] != "complete" || cp["completion_report_path"] != report || cp["completion_report_sha256"] != fmt.Sprintf("%x", sha256.Sum256(rb)) {
			t.Fatalf("checkpoint does not bind verified report: %s", b)
		}
		return cp
	}
	initial, err := os.ReadFile(filepath.Join(root, report))
	if err != nil {
		t.Fatal(err)
	}
	var initialBody map[string]any
	if err := json.Unmarshal(initial, &initialBody); err != nil {
		t.Fatal(err)
	}
	initialBody["created_at"] = "2026-09-19T10:00:00Z"
	initial, _ = json.Marshal(initialBody)
	if err := os.WriteFile(filepath.Join(root, report), initial, 0o644); err != nil {
		t.Fatal(err)
	}
	first := check()
	checksBefore, err := os.ReadFile(counter)
	if err != nil || len(checksBefore) == 0 {
		t.Fatalf("integration checks did not run: %v", err)
	}
	head := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	// The first run has already removed the worktree. Refreshing report content
	// must still have a recovery path without a second merge.
	rb, err := os.ReadFile(filepath.Join(root, report))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(rb, &body); err != nil {
		t.Fatal(err)
	}
	body["changed_paths"] = []string{"internal/refreshed.go"}
	rb, _ = json.Marshal(body)
	if err := os.WriteFile(filepath.Join(root, report), rb, 0o644); err != nil {
		t.Fatal(err)
	}
	second := check()
	checksAfter, err := os.ReadFile(counter)
	if err != nil || len(checksAfter) <= len(checksBefore) {
		t.Fatalf("new Result did not rerun checks: %v", err)
	}
	if first["completion_report_sha256"] == second["completion_report_sha256"] || first["verified_at"] == second["verified_at"] {
		t.Fatal("changed Result did not receive fresh verification")
	}
	if after := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop")); after != head {
		t.Fatal("Result refresh repeated the merge")
	}
	before, _ := os.ReadFile(cpPath)
	if err := os.Remove(filepath.Join(root, report)); err != nil {
		t.Fatal(err)
	}
	code, out, errout := runTaskIntegrate(t, root, "assignment-ti")
	if code == 0 && !strings.Contains(strings.ToLower(out+errout), "block") && !strings.Contains(strings.ToLower(out+errout), "fail") {
		t.Fatalf("missing report silently accepted: %s %s", out, errout)
	}
	after, _ := os.ReadFile(cpPath)
	if string(before) != string(after) {
		t.Fatal("missing report changed verified checkpoint")
	}
}
