package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func occupyPlanningStage(t *testing.T, root, stage string) {
	t.Helper()
	snap := stageSnapshot(t, root)
	id := snap.State["runtime_id"].(string)
	path := filepath.Join(root, ".claude/submissions", id, "g1", stage)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unknown file"), 0600); err != nil {
		t.Fatal(err)
	}
}
func expectPlanningHookReception(t *testing.T, root, path, id, stage string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, path)}, "cwd": root})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("hook %d: %s %s", code, out.String(), errout.String())
	}
	snap := stageSnapshot(t, root)
	_, found, err := runtime.FindSubmission(snap.State, id)
	if err != nil || !found {
		t.Fatalf("%s candidate was not received: %v %s %s", stage, err, out.String(), errout.String())
	}
}
func TestS3CandidateAnswerReachesOriginalHook(t *testing.T) {
	root, p := newContractAnswerFixture(t, func(root string) {
		path := "docs/dev/contracts/CONTRACTS-039.md"
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, path, string(data)+"| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
		occupyPlanningStage(t, root, "S3")
	})
	if !strings.Contains(p.AnswerPath, "/S3/") || !strings.Contains(p.AnswerPath, "planning-candidate-") {
		t.Fatalf("wrong candidate: %s", p.AnswerPath)
	}
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	expectPlanningHookReception(t, root, p.AnswerPath, p.ID, "S3")
}
func TestS4CandidateAnswerReachesOriginalHook(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t, func(root string) { occupyPlanningStage(t, root, "S4") })
	if !strings.Contains(p.AnswerPath, "/S4/") || !strings.Contains(p.AnswerPath, "planning-candidate-") {
		t.Fatalf("wrong candidate: %s", p.AnswerPath)
	}
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	expectPlanningHookReception(t, root, p.AnswerPath, p.ID, "S4")
}
