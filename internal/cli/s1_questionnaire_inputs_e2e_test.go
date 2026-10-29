package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
)

func TestS1CurrentProjectionKeepsOriginalBindingResponsibility(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	path := filepath.Join(root, ".claude/submissions/requirement-binding.md")
	entryQuestionnaireHook(t, root, "SessionStart", path)
	for _, command := range []string{"next", "status"} {
		var out, errout bytes.Buffer
		if code := cli.Run([]string{command, "--root", root}, nil, &out, &errout); code != 0 {
			t.Fatalf("%s failed %d: %s", command, code, &errout)
		}
		var projection struct {
			Stage     string   `json:"stage"`
			Objective string   `json:"objective"`
			Completed []string `json:"completed"`
		}
		if err := json.Unmarshal(out.Bytes(), &projection); err != nil {
			t.Fatal(err)
		}
		if projection.Stage != "S1" || !strings.Contains(projection.Objective, "actual startup decision") || !strings.Contains(projection.Objective, "branch destinations") {
			t.Fatalf("%s lost current S1 responsibility: %s", command, &out)
		}
		for _, unexpected := range []string{"valid_runtime_cursor", "recover a valid runtime cursor", ".claude/loop-state.json", "docs/control/loop-definition.json"} {
			if strings.Contains(out.String(), unexpected) {
				t.Fatalf("%s wrongly sent a valid S1 entry into recovery: %s", command, &out)
			}
		}
		if command == "status" && (len(projection.Completed) != 1 || projection.Completed[0] != "S0") {
			t.Fatalf("S1 status lost completed S0: %s", &out)
		}
	}
}

func TestS1NormalFourAnswersDeriveOriginalMachineFacts(t *testing.T) {
	root := initEntryQuestionnaireRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	path := filepath.Join(root, ".claude/submissions/requirement-binding.md")
	entryQuestionnaireHook(t, root, "SessionStart", path)
	draft, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// These are the only authored answers. No test-built SHA, identity,
	// revision, timestamp or receipt is submitted on this normal path.
	answers := map[string]string{
		"Which requirement is selected?":            filepath.Join(root, "docs/requirements/REQ-098.md"),
		"Who actually authorized this binding?":     "actual-test-human",
		"Which development branch was selected?":    "feature/req",
		"What is the explicit release destination?": "origin/main",
	}
	questionCount := 0
	var lines []string
	for _, line := range strings.Split(string(draft), "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "?") {
			question, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), "?")
			answer, known := answers[question+"?"]
			if !ok || !known {
				t.Fatalf("binding draft added an extra author question: %q", line)
			}
			questionCount++
			line = "- " + question + "? " + answer
		}
		lines = append(lines, line)
	}
	if questionCount != len(answers) {
		t.Fatalf("binding draft has %d questions, want four: %s", questionCount, draft)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	feedback := entryQuestionnaireHook(t, root, "PostToolUse", path)
	state := readJSONMap(t, filepath.Join(root, ".claude/loop-state.json"))
	if lifecycle := state["lifecycle"].(map[string]any); lifecycle["state"] != "planning" || lifecycle["phase"] != "design" {
		t.Fatalf("four real choices did not reach original TR-001/S2: %s", feedback)
	}
	bound := state["bound_req"].(map[string]any)
	if bound["sha256"] != sha256Hex([]byte(lockedReqBody)) || bound["approved_by"] != "actual-test-human" {
		t.Fatalf("original receiver failed to derive actual source or retain real decision: %#v", bound)
	}
	if _, err := time.Parse(time.RFC3339Nano, bound["approved_at"].(string)); err != nil {
		t.Fatalf("original receiver did not generate its actual timestamp: %v", err)
	}
	workspace := bound["workspace"].(map[string]any)
	if workspace["bound_commit"] == "" || workspace["dev_branch"] != "feature/req" || workspace["release_upstream"] != "origin/main" {
		t.Fatalf("original receiver did not bind actual Git destinations/source: %#v", workspace)
	}
	if state["baseline"].(map[string]any)["generation"] != float64(1) || state["binding_receipt"] == nil {
		t.Fatalf("original receiver did not generate generation/receipt: %#v", state)
	}
}
