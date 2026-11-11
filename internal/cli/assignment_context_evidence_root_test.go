package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is the original assignment-context questionnaire projection, not the
// separate native S6 prepare/brief or the worker's private worktree answer.
func TestNativeContextDraftsUseActualEvidenceRootAndReplayBinding(t *testing.T) {
	for _, occupant := range []string{"file", "internal-symlink", "external-symlink"} {
		t.Run(occupant, func(t *testing.T) {
			root, taskID, taskPath := newS6RecordContextFixture(t)
			const owner = "agent-s6-native-context"
			evidence := filepath.Join(root, ".claude/evidence")
			target := t.TempDir()
			if occupant == "internal-symlink" {
				target = filepath.Join(root, ".claude/unknown-target")
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := []byte("unknown source, never a draft\n")
			if err := os.WriteFile(filepath.Join(target, "unknown.txt"), sentinel, 0600); err != nil {
				t.Fatal(err)
			}
			if occupant == "file" {
				if err := os.WriteFile(evidence, sentinel, 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(target, evidence); err != nil {
				t.Fatal(err)
			}
			// A whole alternate root is also unknown. Both publications must skip it.
			alternate := filepath.Join(root, ".claude/evidence-alt-1")
			if err := os.WriteFile(alternate, sentinel, 0600); err != nil {
				t.Fatal(err)
			}
			stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", ".claude/workgroups/manifest-s6-native-context.json", "--task-id", taskID, "--task", taskPath)
			prepare := func() []byte {
				return []byte(stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", owner))
			}
			first := prepare()
			var binding struct {
				Context struct {
					Path string `json:"path"`
				} `json:"context_ref"`
			}
			if err := json.Unmarshal(first, &binding); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(binding.Context.Path, ".claude/assignment-context/") {
				t.Fatalf("context root changed: %+v", binding)
			}
			body, err := os.ReadFile(filepath.Join(root, binding.Context.Path))
			if err != nil {
				t.Fatal(err)
			}
			var view struct {
				Plan   string `json:"plan_draft_path"`
				Result string `json:"result_path"`
				Schema struct {
					Path string `json:"path"`
				} `json:"plan_schema"`
				Instruction string `json:"instruction"`
			}
			if err := json.Unmarshal(body, &view); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(view.Schema.Path, ".claude/assignment-context/") {
				t.Fatalf("schema root changed: %+v", view)
			}
			for _, path := range []string{view.Plan, view.Result} {
				if !strings.HasPrefix(path, ".claude/evidence-alt-2/") {
					t.Fatalf("did not register actual safe draft: %s", path)
				}
				draft, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || !json.Valid(draft) {
					t.Fatalf("draft %s invalid: %v", path, err)
				}
			}
			if !strings.Contains(view.Instruction, "save the original answer at its provided path") || !strings.Contains(view.Instruction, "canonical S6 Builder Result") {
				t.Fatalf("lost original author instruction: %s", view.Instruction)
			}
			revision := stageSnapshot(t, root).Revision
			if again := prepare(); !bytes.Equal(first, again) || stageSnapshot(t, root).Revision != revision {
				t.Fatal("replay did not retain original binding")
			}
			if got, err := os.ReadFile(alternate); err != nil || !bytes.Equal(got, sentinel) {
				t.Fatalf("alternate occupant changed: %q %v", got, err)
			}
			if occupant == "file" {
				if got, err := os.ReadFile(evidence); err != nil || !bytes.Equal(got, sentinel) {
					t.Fatalf("root occupant changed: %q %v", got, err)
				}
			} else if got, err := os.Readlink(evidence); err != nil || got != target {
				t.Fatalf("root symlink changed: %s %v", got, err)
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 1 || entries[0].Name() != "unknown.txt" {
				t.Fatalf("symlink target changed: %v %v", entries, err)
			}
			if got, err := os.ReadFile(filepath.Join(target, "unknown.txt")); err != nil || !bytes.Equal(got, sentinel) {
				t.Fatalf("target bytes changed: %q %v", got, err)
			}
		})
	}
}
