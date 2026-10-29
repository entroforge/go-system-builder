package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workspace"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

const simulationScopeCell = "`simulation/products/`（SIMULATION 占位区；本演练的交付形态是一个明确标记的 SIMULATION 空 fixture commit，不写产品源码、不做 git add 的产品变更）"

func TestTaskScopeOriginalS4SaveThroughNativeAssignmentAndBrief(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	taskRef := "docs/dev/tasks/TASK-039-01.md"
	task, err := os.ReadFile(filepath.Join(root, taskRef))
	if err != nil {
		t.Fatal(err)
	}
	task = []byte(strings.Replace(string(task), "`internal/fixture/service.go`", simulationScopeCell, 1))
	writeContractSource(t, root, taskRef, string(task))
	for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules", "agents/backend-builder.md"} {
		dest := rel
		if strings.HasPrefix(rel, "agents/") {
			dest = ".claude/" + rel
		}
		copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, dest))
	}
	for _, skill := range catalog.Skills {
		copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), "skills", skill.Name), filepath.Join(root, ".claude/skills", skill.Name))
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("original S4 save: %+v %v", receipt, err)
	}
	settleMainPlanningFollowups(t, root)
	checked, err := semantic.TasksCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-039")
	if err != nil || len(checked.Problems) != 0 || len(checked.TaskContexts) != 1 || !reflect.DeepEqual(checked.TaskContexts[0].Writes, []string{"simulation/products/"}) {
		t.Fatalf("S4/S5 scope: %+v %v", checked, err)
	}
	for _, packet := range registerAcceptedS5Reviewers(t, root, "TASK-039-01", taskRef) {
		stageJSON(t, root, packet.AnswerPath, completedDraft(packet, "pass"))
		service, err = submission.New(root)
		if err != nil {
			t.Fatal(err)
		}
		r, e := service.Save(context.Background(), packet.Binding.Owner, packet.AnswerPath)
		if e != nil || !r.Accepted {
			t.Fatalf("original independent S5 save: %+v %v", r, e)
		}
		settleOriginalS5Followup(t, root, packet)
	}
	if got := stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"]; got != "building" {
		t.Fatalf("S5 did not enter S6: %v", got)
	}
	// Bind the actual development source before the original native preparation.
	stageCLI(t, "req", "workspace", "--root", root, "--dev-branch", "test-development", "--release-upstream", "origin/release")
	stageCLI(t, "s6", "prepare", "--root", root, "--task", "TASK-039-01", "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-TASK-039-01")
	if err != nil || !found || !reflect.DeepEqual(record.EffectiveWritePaths, []string{"simulation/products"}) {
		t.Fatalf("actual Assignment scope: %+v %v", record, err)
	}
	brief, _, _ := assignmentrecord.NativePaths(record)
	body, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "reviewed scope: simulation/products.") || strings.Contains(string(body), "reviewed scope: "+simulationScopeCell) {
		t.Fatalf("brief lost scope: %s", body)
	}
	after, err := os.ReadFile(filepath.Join(root, taskRef))
	if err != nil || !bytes.Equal(after, task) {
		t.Fatalf("frozen TASK changed: %v", err)
	}
	// The original Stop receiver still rejects a real path outside that directory.
	source := filepath.Join(t.TempDir(), "native-scope-worktree")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/scope", source, "HEAD"); err != nil {
		t.Fatal(err)
	}
	nativeAnswer(t, source, record)
	_, plan, result := assignmentrecord.NativePaths(record)
	planBytes, err := os.ReadFile(filepath.Join(source, plan))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, plan), bytes.ReplaceAll(planBytes, []byte("src/pkg/proof.txt"), []byte("outside/proof.txt")), 0600); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root)
	code, output := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), true)
	if code != 0 || !strings.Contains(output, "S6 PLAN path outside/proof.txt is outside the canonical Assignment") || stageSnapshot(t, root).Revision != before.Revision {
		t.Fatalf("out-of-scope Plan lacks original rejection: %d %s", code, output)
	}

}

func TestTaskScopeOriginalS4SaveRejectsTailAtScopeLine(t *testing.T) {
	root, p := newTaskPlanningAnswerFixture(t)
	taskRef := "docs/dev/tasks/TASK-039-01.md"
	body, err := os.ReadFile(filepath.Join(root, taskRef))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, taskRef, strings.Replace(string(body), "`internal/fixture/service.go`", "`simulation/products/` trailing garbage", 1))
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := service.SaveTaskPlanning(context.Background(), p.AnswerPath)
	if err == nil || r.Accepted || !strings.Contains(err.Error(), taskRef+":") || !strings.Contains(err.Error(), "Scope prospective write paths") || !strings.Contains(err.Error(), "trailing garbage") {
		t.Fatalf("S4 rejection not located: %+v", r)
	}
}
