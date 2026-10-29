package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestSavedHypothesisResultHookRejectsUnissuedPathWithoutRuntimeWrite(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(f.root, ".claude", "submissions", "loop-test", "g1", "S8", "r1", "assignment-1", "packet-forged", "investigator-spec", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{"answer": map[string]any{"result": "inconclusive"}})
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": path},
		"teammate_name":   "investigator-spec",
		"cwd":             f.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("PostToolUse exit=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	if !strings.Contains(out.String(), "S8 SUBMISSION RECEIPT") || !strings.Contains(out.String(), "no current registered issuance") {
		t.Fatalf("unissued S8 path did not receive a fail-closed S8 receipt: %s", out.String())
	}
	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatalf("unissued path changed Runtime revision %d -> %d", before.Revision, after.Revision)
	}
}

func TestStageAnswerTerminalConsumeRejectsUnissuedS8Path(t *testing.T) {
	f := newStageAnswerFixture(t)
	before := stageSnapshot(t, f.root)
	path := filepath.Join(f.root, ".claude", "submissions", "loop-test", "g1", "S8", "r1", "assignment-1", "packet-forged", "investigator-spec", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{"answer": map[string]any{"result": "inconclusive"}})
	var out, errout bytes.Buffer
	code := cli.Run([]string{
		"runtime", "stage-answer", "consume", "--root", f.root,
		"--agent", "investigator-spec", "--path", path,
	}, nil, &out, &errout)
	if code == 0 {
		t.Fatalf("terminal --agent was accepted as S8 author identity: stdout=%s stderr=%s", out.String(), errout.String())
	}
	if !strings.Contains(errout.String(), "S8 answer path has no current registered issuance") {
		t.Fatalf("S8 terminal rejection did not come from exact registered-path selection: %s", errout.String())
	}
	if after := stageSnapshot(t, f.root); after.Revision != before.Revision {
		t.Fatalf("terminal S8 consume changed Runtime revision %d -> %d", before.Revision, after.Revision)
	}
}

func TestSavedHypothesisResultHookDoesNotTreatUnissuedTeammatePathAsCurrent(t *testing.T) {
	f := newStageAnswerFixture(t)
	path := filepath.Join(f.root, ".claude", "submissions", "loop-test", "g1", "S8", "r1", "assignment-1", "packet-forged", "investigator-spec", "answer.json")
	stageJSON(t, f.root, filepath.ToSlash(strings.TrimPrefix(path, f.root+string(filepath.Separator))), map[string]any{"answer": map[string]any{"result": "inconclusive"}})
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": path},
		"teammate_name":   "investigator-spec",
		"cwd":             f.root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("PostToolUse exit=%d stderr=%s stdout=%s", code, errout.String(), out.String())
	}
	if !strings.Contains(out.String(), "S8 SUBMISSION RECEIPT") || !strings.Contains(out.String(), "no current registered issuance") {
		t.Fatalf("unissued teammate path was not rejected by original packet routing: %s", out.String())
	}
}
