package cli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS4LargeBatchReceivesAllActualTasks(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	original, err := os.ReadFile(filepath.Join(root, "docs/dev/tasks/TASK-039-01.md"))
	if err != nil {
		t.Fatal(err)
	}
	draft := completedTaskPlanningDraft(p)
	index := "# REQ-039 dispatch\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n\n- [ ] [TASK-039-01](TASK-039-01.md)\n"
	for i := 2; i <= 129; i++ {
		id := fmt.Sprintf("TASK-039-%02d", i)
		path := "docs/dev/tasks/" + id + ".md"
		writeContractSource(t, root, path, strings.ReplaceAll(string(original), "internal/fixture/service.go", fmt.Sprintf("internal/fixture/service%d.go", i)))
		index += fmt.Sprintf("- [ ] [%s](%s.md)\n", id, id)
		draft.Answer.TaskPaths = append(draft.Answer.TaskPaths, path)
	}
	writeContractSource(t, root, *draft.Answer.IndexPath, index)
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, draft)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.CurrentStage != "S5" {
		t.Fatalf("full 129-TASK batch failed: %+v %v", receipt, err)
	}
	state := stageSnapshot(t, root).State
	count := 0
	for _, raw := range state["documents"].([]any) {
		if raw.(map[string]any)["kind"] == "task" {
			count++
		}
	}
	if count != 129 {
		t.Fatalf("registered %d tasks, want 129", count)
	}
}

func TestS4GuidedNumericExampleCompletesOriginalReception(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	source := filepath.Join(root, "docs/dev/tasks/TASK-039-01.md")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	path := "docs/dev/tasks/TASK-009-01.md"
	writeContractSource(t, root, path, string(data))
	index := "docs/dev/tasks/index-REQ-039.md"
	writeContractSource(t, root, index, "# REQ-039 dispatch\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n\n- [ ] [TASK-009-01](TASK-009-01.md)\n")
	fixtures.CommitCurrentFixture(t, root)
	guidance := designGuidanceEvent(t, root, "s4-numeric-example", "SessionStart", "Read")
	if !strings.Contains(guidance, "TASK-009-01.md") {
		t.Fatalf("missing real guidance example: %s", guidance)
	}
	draft := completedTaskPlanningDraft(p)
	draft.Answer.TaskPaths = []string{path}
	stageJSON(t, root, p.AnswerPath, draft)
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted || receipt.CurrentStage != "S5" {
		t.Fatalf("guided filename rejected: %+v %v", receipt, err)
	}
}
