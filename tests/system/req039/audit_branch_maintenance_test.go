package req039_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchAuditMaintenancePreservesReviewedSubjects(t *testing.T) {
	for _, command := range []string{"fingerprint", "reconcile-policy-ref"} {
		t.Run(command, func(t *testing.T) {
			root := s4AuditRegistrationRoot(t)
			statePath := filepath.Join(root, ".claude/loop-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(before, &state); err != nil {
				t.Fatal(err)
			}
			task := "TASK-042-02"
			taskPath := filepath.Join(root, "docs/dev/tasks/"+task+".md")
			b, err := os.ReadFile(taskPath)
			if err != nil {
				t.Fatal(err)
			}
			b = bytes.ReplaceAll(b, []byte("web/pages"), []byte("web/unreviewed"))
			if err := os.WriteFile(taskPath, b, 0o644); err != nil {
				t.Fatal(err)
			}
			runGitIn(t, root, "add", "docs/dev/tasks/"+task+".md")
			runGitIn(t, root, "commit", "-m", "change scope without S5")
			manifest := s4AuditManifest(t, root, "maintenance", task, "web/unreviewed", "web/unreviewed/result.json")
			code, out, errOut := s4AuditRegister(t, root, manifest, task)
			if code == 0 {
				t.Fatalf("precondition: drift should block registration: %s %s", out, errOut)
			}
			if command == "reconcile-policy-ref" {
				policy := state["hook_control"].(map[string]any)["policy_ref"].(map[string]any)["path"].(string)
				path := filepath.Join(root, policy)
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			code = runCLI(t, []string{"runtime", command, "--root", root}, strings.NewReader(""), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("maintenance command failed before subject check: %d %s %s", code, stdout.String(), stderr.String())
			}
			after := readSystemState(t, root)
			for _, field := range []string{"documents", "entities", "bound_req", "evidence"} {
				original, _ := json.Marshal(state[field])
				preserved, _ := json.Marshal(after[field])
				if !bytes.Equal(original, preserved) {
					t.Fatalf("%s changed attested %s", command, field)
				}
			}
			code, out, errOut = s4AuditRegister(t, root, manifest, task)
			if code == 0 {
				t.Fatalf("%s refreshed frozen TASK scope and allowed unreviewed dispatch; output=%s", command, stdout.String())
			}
		})
	}
}
