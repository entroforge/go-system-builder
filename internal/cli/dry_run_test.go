package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Include directories and all regular files: an unchanged revision alone would
// miss audit writes, prepared questionnaires, pending recovery and caches.
func dryRunTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[rel] = info.Mode().String()
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			result[rel] = "symlink:" + target
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[rel] = fmt.Sprintf("%s:%x", info.Mode(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDryRunNeverConsumesNativeAnswersOrWritesHookArtifacts(t *testing.T) {
	for _, event := range []string{"SubagentStop", "SubagentStart", "PreToolUse", "PostToolUse", "Stop", "TeammateIdle", "SessionStart", "PreCompact", "PostToolUseFailure", "ConfigChange"} {
		t.Run(event, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			for _, packet := range f.packets {
				stageJSON(t, f.root, packet.AnswerPath, completedDraft(packet, "pass"))
			}
			packet := f.packets[0]
			payload, err := json.Marshal(map[string]any{
				"hook_event_name": event, "session_id": "readonly-preview", "agent_id": "opaque-native-preview",
				"cwd": f.root, "last_assistant_message": filepath.Join(f.root, packet.ViewPath),
				"tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(f.root, packet.AnswerPath)},
				"tool_response": map[string]any{"file_path": filepath.Join(f.root, packet.AnswerPath)},
			})
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(t.TempDir(), "event.json")
			if err := os.WriteFile(fixture, payload, 0600); err != nil {
				t.Fatal(err)
			}
			before := dryRunTree(t, f.root)
			var out, diagnostic bytes.Buffer
			code := cli.Run([]string{"dry-run", "--root", f.root, "--fixture", fixture}, nil, &out, &diagnostic)
			if after := dryRunTree(t, f.root); !reflect.DeepEqual(before, after) {
				var changed []string
				for path, digest := range before {
					if after[path] != digest {
						changed = append(changed, path)
					}
				}
				for path := range after {
					if _, found := before[path]; !found {
						changed = append(changed, path)
					}
				}
				t.Fatalf("dry-run %s changed project files: %v (exit=%d)", event, changed, code)
			}
			if code != 0 {
				t.Fatalf("preview: exit=%d stderr=%s", code, &diagnostic)
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("preview must return JSON: %v %s", err, &out)
			}
			gate, _ := result["quality_gate"].(map[string]any)
			if gate == nil || gate["transition_committed"] != false || gate["transition_guards_evaluated"] != false || result["hook_event"] != event || result["hook_simulated"] != false || result["evaluation_scope"] != "quality_gate" {
				t.Fatalf("preview must identify the event and uncommitted quality diagnostics: %s", &out)
			}
			for _, packet := range f.packets {
				if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, packet.ID); err != nil || accepted {
					t.Fatalf("dry-run consumed a professional answer: accepted=%t err=%v", accepted, err)
				}
				if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "actual-native-reviewer", filepath.Join(f.root, packet.ViewPath)); code != 0 {
					t.Fatalf("real Stop must still receive the answer: %d %s", code, diagnostic)
				}
			}
			if got := stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"]; got != "building" {
				t.Fatalf("real native reception no longer reaches the S5 gate: %v", got)
			}
		})
	}
}

func TestDryRunMissingRuntimeDoesNotInitialize(t *testing.T) {
	root := factory.Clone(t, filepath.Join("..", ".."))
	fixture := filepath.Join(root, "tests/fixtures/hook/pretool-unactivated-write.json")
	before := dryRunTree(t, root)
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"dry-run", "--root", root, "--fixture", fixture}, nil, &out, &diagnostic)
	if code != 1 || !strings.Contains(diagnostic.String(), "read runtime") {
		t.Fatalf("missing Runtime must be reported: %d %s", code, &diagnostic)
	}
	if !reflect.DeepEqual(before, dryRunTree(t, root)) {
		t.Fatal("diagnostics initialized or modified a missing Runtime")
	}
}
