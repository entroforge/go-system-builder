package req039_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/hookctx"
)

// Exercise registration -> real worktree -> canonical completion -> integration
// -> successor eligibility. Agent execution is seeded; platform spawning is not
// simulated as a successful operation.
func TestBranchDeliveryReleasesSuccessorWithSiblingOutput(t *testing.T) {
	root := s4AuditRegistrationRoot(t)
	runGitIn(t, root, "config", "user.name", "Fixture")
	runGitIn(t, root, "config", "user.email", "fixture@example.com")
	run := func(args ...string) string {
		t.Helper()
		var out, errout bytes.Buffer
		args = append(args, "--root", root)
		if code := runCLI(t, args, strings.NewReader(""), &out, &errout); code != 0 {
			t.Fatalf("%v: %d %s %s", args, code, out.String(), errout.String())
		}
		return out.String()
	}
	assertSuccessor := func(want bool) {
		t.Helper()
		err := dispatch.CheckRegistration(root, readSystemState(t, root), "TASK-042-04")
		if (err == nil) != want {
			t.Fatalf("successor ready=%v: %v", want, err)
		}
	}
	assertSuccessor(false)
	for _, item := range []struct{ suffix, task, scope string }{
		{"shared", "TASK-042-01", "packages/validation"},
		{"page", "TASK-042-02", "web/pages"},
	} {
		task, aid, agent := item.task, "assignment-audit-"+item.suffix, "builder-audit-"+item.suffix
		output := item.scope + "/generated/result.json"
		manifest := s4AuditManifest(t, root, item.suffix, task, item.scope+"/source", output)
		canonicalManifest := filepath.Join(root, ".claude/workgroups/REQ-039", task, "manifest.json")
		if err := os.MkdirAll(filepath.Dir(canonicalManifest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(manifest, canonicalManifest); err != nil {
			t.Fatal(err)
		}
		manifest = canonicalManifest
		manifestBytes, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var manifestBody map[string]any
		if err := json.Unmarshal(manifestBytes, &manifestBody); err != nil {
			t.Fatal(err)
		}
		manifestBody["assignments"].([]any)[0].(map[string]any)["required_checks"] = []string{"git rev-parse --verify HEAD"}
		manifestBytes, err = json.Marshal(manifestBody)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, manifestBytes, 0644); err != nil {
			t.Fatal(err)
		}
		if code, out, errout := s4AuditRegister(t, root, manifest, task); code != 0 {
			t.Fatalf("register: %d %s %s", code, out, errout)
		}
		run("runtime", "worktree-create", "--assignment-id", aid)
		loaded, err := hookctx.LoadFull(root, "")
		if err != nil {
			t.Fatal(err)
		}
		worker := ""
		for _, a := range loaded.Assignments {
			if a.AssignmentID == aid {
				worker = a.WorktreePath
			}
		}
		if worker == "" {
			t.Fatal("missing worktree")
		}
		target := filepath.Join(worker, output)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		runGitIn(t, worker, "add", output)
		runGitIn(t, worker, "commit", "-m", "deliver declared output outside source directory")

		state := readSystemState(t, root)
		for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
			a := raw.(map[string]any)
			if a["id"] == agent {
				a["state"] = "working"
			}
		}
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		message := filepath.Join(root, ".claude/messages/"+agent+".json")
		writeAuditCompletionMessage(t, message, agent, task)
		data, err = os.ReadFile(message)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		body["changed_paths"] = []string{output}
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(message, data, 0644); err != nil {
			t.Fatal(err)
		}
		run("runtime", "task-complete", "--agent-id", agent, "--message", message)
		assertSuccessor(false) // A report alone never releases its consumers.
		code, out, errout := runTaskIntegrate(t, root, aid)
		if code != 0 || !strings.Contains(errout, "integrated") {
			t.Fatalf("integrate: %d %s %s", code, out, errout)
		}
		if _, err := os.Stat(filepath.Join(root, output)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(worker); !os.IsNotExist(err) {
			t.Fatalf("worker not cleaned: %v", err)
		}
	}
	assertSuccessor(true) // TASK-042-03 is unrelated and has not been dispatched.
	// A new canonical Result after cleanup must revoke readiness until rechecked,
	// then recover without remerging or recreating the cleaned worktree.
	head := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	run("runtime", "task-complete", "--agent-id", "builder-audit-page", "--message", filepath.Join(root, ".claude/messages/builder-audit-page.json"))
	assertSuccessor(false)
	if code, out, errout := runTaskIntegrate(t, root, "assignment-audit-page"); code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("recheck resubmitted Result: %d %s %s", code, out, errout)
	}
	if current := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); current != head {
		t.Fatal("resubmission repeated the merge")
	}
	assertSuccessor(true)

	manifest := s4AuditManifest(t, root, "successor", "TASK-042-04", "web/features", "web/features/result.json")
	if code, out, errout := s4AuditRegister(t, root, manifest, "TASK-042-04"); code != 0 {
		t.Fatalf("successor register: %d %s %s", code, out, errout)
	}
}
