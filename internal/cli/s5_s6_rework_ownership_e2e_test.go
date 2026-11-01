package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// A real S5 finding leads through the planning writers to fresh independent
// reviews; historical reviewers are not Builder ownership or capacity.
func TestS5ReworkOriginalReviewsReachS6BuilderPreparation(t *testing.T) {
	root, first := currentS5ReworkFixture(t)
	initial := stageSnapshot(t, root)
	for _, raw := range initial.State["entities"].(map[string]any)["tasks"].([]any) {
		task := raw.(map[string]any)
		if task["id"] == "TASK-039-01" {
			if owners, _ := task["owner_agent_ids"].([]any); len(owners) != 0 || workstate.TaskActivity(task) != "candidate" {
				t.Fatalf("S5 reviewer was registered as Builder: %v", task)
			}
		}
	}
	repo := fixtures.RepoRoot(t)
	copyS6RecordContextTree(t, filepath.Join(repo, "agents/backend-builder.md"), filepath.Join(root, ".claude/agents/backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyS6RecordContextTree(t, filepath.Join(repo, "skills", skill.Name, "SKILL.md"), filepath.Join(root, ".claude/skills", skill.Name, "SKILL.md"))
	}
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	old := first[0]
	draft := completedDraft(old, "fix_required")
	draft.Answer.Findings = []workpacket.Finding{{Path: "docs/dev/tasks/TASK-039-01.md#scope", Expected: "actual task scope", Observed: "one finding requiring planning correction"}}
	receipt := saveStageHook(t, root, root, old.Binding.Owner, old.AnswerPath, draft)
	if !receipt.Accepted {
		t.Fatalf("original S5 finding was not received: %+v", receipt)
	}
	settleOriginalS5Followup(t, root, old)
	if life := stageSnapshot(t, root).State["lifecycle"].(map[string]any); life["state"] != "planning" || life["phase"] != "design" {
		t.Fatalf("original S5 finding did not return to design: %v", life)
	}
	var design workpacket.DesignPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &design); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", design.AnswerPath)
	settleMainPlanningFollowups(t, root)
	var contracts workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &contracts); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, contracts.AnswerPath, completedContractDraft(contracts))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", contracts.AnswerPath)
	settleMainPlanningFollowups(t, root)
	var tasks workpacket.TaskPlanningPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &tasks); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, tasks.AnswerPath, completedTaskPlanningDraft(tasks))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", tasks.AnswerPath)
	settleMainPlanningFollowups(t, root)
	snapshot := stageSnapshot(t, root)
	second, err := runtime.S5Packets(root, snapshot.State, "")
	if err != nil || len(second) != 2 {
		t.Fatalf("fresh S5 original responsibilities: %d %v", len(second), err)
	}
	for i, p := range second {
		stageJSON(t, root, p.AnswerPath, completedDraft(p, "pass"))
		if code, _, diag := nativeReviewEvent(t, root, "SubagentStop", "new-reviewer-"+p.Binding.Owner, filepath.Join(root, p.ViewPath)); code != 0 {
			t.Fatalf("S5 original return %d: %d %s", i, code, diag)
		}
	}
	snapshot = stageSnapshot(t, root)
	if snapshot.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatalf("fresh S5 did not enter S6: %v", snapshot.State["lifecycle"])
	}
	if _, found, err := runtime.FindSubmission(snapshot.State, old.ID); err != nil || !found {
		t.Fatalf("original finding disappeared: %v %v", found, err)
	}
	status := stageCLI(t, "s6", "status", "--root", root, "--capacity", "1", "--json")
	if !strings.Contains(status, "TASK-039-01") || !strings.Contains(status, "\"dispatchable\":true") {
		t.Fatalf("historical review blocked actual Builder: %s", status)
	}
	prepared := stageCLI(t, "s6", "prepare", "--root", root, "--task", "TASK-039-01", "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-TASK-039-01")
	if err != nil || !found || record.Task.ID != "TASK-039-01" {
		t.Fatalf("original Builder responsibility missing: %+v %v", record, err)
	}
	brief, _, _ := assignmentrecord.NativePaths(record)
	if _, err := os.Stat(filepath.Join(root, brief)); err != nil || !strings.Contains(prepared, brief) {
		t.Fatalf("original Builder brief: %v %s", err, prepared)
	}
}
