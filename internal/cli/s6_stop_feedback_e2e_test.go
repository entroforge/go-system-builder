package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS6StopFeedbackKeepsExactQuestionnaireWithoutDuplicateMessages(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const owner = "agent-s6-native-context"
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", ".claude/workgroups/manifest-s6-native-context.json", "--task-id", taskID, "--task", taskPath)
	var prepared struct {
		Context struct {
			Path string `json:"path"`
		} `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", owner)), &prepared); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, prepared.Context.Path))
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		PlanPath   string `json:"plan_draft_path"`
		ResultPath string `json:"result_path"`
	}
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	beforeState, beforeJournal := s6CanonicalRuntimePair(t, root)
	code, output := stageHookCode(t, root, root, owner, "SubagentStop", filepath.Join(root, "product.go"))
	if code != 2 {
		t.Fatalf("pending Builder stop must continue same responsibility: code=%d output=%s", code, output)
	}
	for _, path := range []string{view.PlanPath, view.ResultPath} {
		if path == "" || !strings.Contains(output, path) {
			t.Fatalf("stop feedback omitted exact questionnaire %q: %s", path, output)
		}
	}
	for _, duplicate := range []string{"send the PLAN_REPORT via SendMessage", "register the Result (`runtime task-complete`"} {
		if strings.Contains(output, duplicate) {
			t.Fatalf("stop feedback teaches duplicate submission: %s", output)
		}
	}
	s6AssertCanonicalRuntimePair(t, root, beforeState, beforeJournal)
}
