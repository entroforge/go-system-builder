package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestUnissuedNativeWorkerSaveIsQuietAndCreatesNoS9Reception(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(f.root, ".claude", "submissions", "runtime-test", "g0", "S9-plan", "r1", "repair-assignment-demo", "packet-forged", "builder-demo", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{
		"title": "forged S9 answer",
	})

	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": path},
		"agent_id":        "builder-demo",
		"cwd":             f.root,
	})
	if err != nil {
		t.Fatal(err)
	}

	originalAnswer, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	originalState, originalJournal := s6CanonicalRuntimePair(t, f.root)
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("PostToolUse exit=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}

	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("ordinary native save created a receipt/protocol: %s", &out)
	}

	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatalf("unissued path changed Runtime revision %d -> %d", before.Revision, after.Revision)
	}
	s6AssertCanonicalRuntimePair(t, f.root, originalState, originalJournal)
	afterAnswer, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(originalAnswer, afterAnswer) {
		t.Fatal("ordinary save rewrote private answer", err)
	}

}

func TestStageAnswerTerminalConsumeRejectsUnissuedS9Path(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(f.root, ".claude", "submissions", "runtime-test", "g0", "S9-plan", "r1", "repair-assignment-demo", "packet-forged", "builder-demo", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{
		"title": "terminal supplied S9 answer",
	})

	var out, errout bytes.Buffer
	code := cli.Run([]string{
		"runtime", "stage-answer", "consume", "--root", f.root,
		"--agent", "builder-demo", "--path", path,
	}, nil, &out, &errout)
	if code == 0 {
		t.Fatalf("terminal --agent was accepted as S9 author identity: stdout=%s stderr=%s", out.String(), errout.String())
	}
	if !strings.Contains(errout.String(), "S9 PlanReport answer path has no current registered issuance") {
		t.Fatalf("S9 terminal rejection did not come from exact registered-path selection: %s", errout.String())
	}
	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatalf("terminal S9 consume changed Runtime revision %d -> %d", before.Revision, after.Revision)
	}
}
