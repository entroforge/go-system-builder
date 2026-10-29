package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkerFrozenContextQuestionnaireUsesActualDispatchAndOriginalConsumer(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	sourcePath := workspace.DispatchContextSource(e)
	if e.Inputs[sourcePath] == "" {
		t.Fatal("new execution did not freeze its context")
	}
	data, err := os.ReadFile(filepath.Join(e.Path, workspace.DispatchContextPath))
	if err != nil {
		t.Fatal(err)
	}
	var c workspace.DispatchContext
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if c.Dispatch.Owner != p.Binding.Owner || c.Dispatch.AssignmentID != p.Binding.AssignmentID || c.Dispatch.TaskID != f.taskID || c.Dispatch.TeamID != "workgroup-stage-answer" || c.Selection.Mode != "shadow" || len(c.ProductWritePaths) != 0 {
		t.Fatalf("guessed dispatch/authority: %+v", c)
	}
	if c.ResultDraft != nil || c.ResultQuestions != nil || c.ResultPath != "" {
		t.Fatal("legacy/S5 document review gained a canonical S6 Result draft")
	}
	if c.PlanPath != "" || c.PlanDraft != nil || c.PlanQuestions != nil || c.AnswerPath != workspace.AnswerDraftPath {
		t.Fatalf("S5 must expose its domain answer without a generic PLAN: %+v", c)
	}
	// This compatibility field carries registered methodology references;
	// its name does not create a generic activation requirement.
	manifestBytes, err := os.ReadFile(filepath.Join(f.root, ".claude/workgroups/REQ-039/review/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Assignments []struct {
			Owner  string   `json:"agent_id"`
			Skills []string `json:"skill_refs"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	var registeredSkills []string
	for _, item := range manifest.Assignments {
		if item.Owner == e.AgentID {
			registeredSkills = item.Skills
		}
	}
	if len(registeredSkills) == 0 || !reflect.DeepEqual(c.Selection.AuthorizationSkillRefs, registeredSkills) {
		t.Fatalf("selection changed original registered skill references: got=%v want=%v", c.Selection.AuthorizationSkillRefs, registeredSkills)
	}
	for _, skill := range c.Selection.Selected {
		actual, err := os.ReadFile(filepath.Join(e.Path, skill.Source))
		if err != nil || workpacket.Digest(actual) != skill.SHA256 {
			t.Fatalf("installed Read source unavailable: %s %v", skill.Source, err)
		}
	}
	if len(c.ReadingOrder()) < 3 || !strings.Contains(c.ReadingOrder()[0], "docs/dev/tasks/") {
		t.Fatalf("lost bottom-up TASK entrance: %v", c.ReadingOrder())
	}
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := registry.WorkerSubmissionPaths(e)
	if err != nil || paths.Plan != "" || paths.Answer != workspace.AnswerDraftPath {
		t.Fatalf("frozen S5 submission paths should contain only the answer: %#v %v", paths, err)
	}
	output := stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	var prepared map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &prepared); err != nil {
		t.Fatalf("decode prepared S5 view: %v: %s", err, output)
	}
	for _, forbidden := range []string{"plan_draft_path", "begin_command", "answer_command"} {
		if _, exists := prepared[forbidden]; exists {
			t.Fatalf("ordinary S5 prepare exposed redundant %s: %s", forbidden, output)
		}
	}
	var preparedAnswer string
	if err := json.Unmarshal(prepared["answer_path"], &preparedAnswer); err != nil || preparedAnswer != workspace.AnswerDraftPath {
		t.Fatalf("prepare omitted exact S5 answer path: %q %v", preparedAnswer, err)
	}
	var preparedPaths map[string]string
	if err := json.Unmarshal(prepared["submission_paths"], &preparedPaths); err != nil || !reflect.DeepEqual(preparedPaths, map[string]string{"answer": workspace.AnswerDraftPath}) {
		t.Fatalf("prepare did not project only the exact S5 answer path: %#v %v", preparedPaths, err)
	}
	for _, required := range []string{"receipt_command", "review_check_command"} {
		if _, exists := prepared[required]; !exists {
			t.Fatalf("prepare omitted original S5 consumer guidance %s: %s", required, output)
		}
	}
	if _, err := os.Stat(filepath.Join(e.Path, workspace.PlanDraftPath)); !os.IsNotExist(err) {
		t.Fatalf("S5 preparation created a generic PLAN file: %v", err)
	}
	sourceBefore, err := os.ReadFile(filepath.Join(f.root, sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateBootstrap(e); err != nil {
		t.Fatalf("questionnaire save guidance changed the readonly receipt: %v", err)
	}
	// Preparing the existing ready execution preserves its frozen context.
	stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID)
	sourceAfter, _ := os.ReadFile(filepath.Join(f.root, sourcePath))
	if !bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("ready retry rewrote frozen context bytes")
	}
	if !saveStageHook(t, f.root, e.Path, p.Binding.Owner, workpacket.WorkerAnswerPath, completedDraft(p, "pass")).WorkComplete {
		t.Fatal("original consumer did not receive the saved S5 answer")
	}
}

func TestWorkerLifecycleWireHasOneUTF8BudgetAndLocalFullEntrances(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	body, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStart", "agent_id": e.AgentID, "cwd": e.Path})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "SubagentStart"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("wire %d: %s", code, errout.String())
	}
	var envelope struct {
		Specific struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	context := envelope.Specific.Context
	if !utf8.ValidString(context) || utf8.RuneCountInString(context) > 800 {
		t.Fatalf("bad context budget: %d %s", utf8.RuneCountInString(context), context)
	}
	for _, path := range []string{workspace.DispatchContextPath, workpacket.WorkerViewPath, workpacket.WorkerAnswerPath} {
		if !strings.Contains(context, path) {
			t.Fatalf("lost complete local entrance %s: %s", path, context)
		}
	}
	if strings.Contains(context, f.packets[1].Binding.Owner) {
		t.Fatal("foreign responsibility delivered to Worker")
	}
}

func TestWorkerReplacementRebindsFrozenContextAndPreservesOldAssets(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	old := f.prepare(t, p)
	data, err := os.ReadFile(filepath.Join(old.Path, workspace.DispatchContextPath))
	if err != nil {
		t.Fatal(err)
	}
	var c workspace.DispatchContext
	json.Unmarshal(data, &c)
	before := stageSnapshot(t, f.root)
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "workspace", "replace", "--root", f.root, "--assignment", old.AssignmentID, "--agent", old.AgentID, "--execution-generation", "1", "--reason", "Check original S5 runner boundary", "--check-location", "main"}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("S5 replacement granted a product runner or mutated rejected intent")
	}
	stageCLI(t, "runtime", "workspace", "replace", "--root", f.root, "--assignment", old.AssignmentID, "--agent", old.AgentID, "--execution-generation", "1", "--reason", "Recover the original undelivered execution with the same frozen sources")
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	next := registry.Executions[old.AssignmentID]
	if next.Generation != 2 || next.Inputs[workspace.DispatchContextSource(next)] == "" || next.Inputs[workspace.DispatchContextSource(old)] != "" {
		t.Fatal("replacement reused old context generation")
	}
	stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", next.AssignmentID, "--agent", next.AgentID)
	nextData, err := os.ReadFile(filepath.Join(next.Path, workspace.DispatchContextPath))
	if err != nil {
		t.Fatal(err)
	}
	var newContext workspace.DispatchContext
	json.Unmarshal(nextData, &newContext)
	if newContext.ExecutionGeneration != 2 || !reflect.DeepEqual(newContext.Selection, c.Selection) || !reflect.DeepEqual(newContext.Dispatch, c.Dispatch) {
		t.Fatal("recovery replacement changed original profile/responsibility")
	}
	oldData, _ := os.ReadFile(filepath.Join(old.Path, workspace.DispatchContextPath))
	if !bytes.Equal(oldData, data) {
		t.Fatal("replacement overwrote old context")
	}
	f.begin(t, p, &next)
	if !saveStageHook(t, f.root, next.Path, p.Binding.Owner, workpacket.WorkerAnswerPath, completedDraft(p, "pass")).WorkComplete {
		t.Fatal("replacement did not retain original answer admission")
	}
	before = stageSnapshot(t, f.root)
	out.Reset()
	errout.Reset()
	if code := cli.Run([]string{"runtime", "workspace", "replace", "--root", f.root, "--assignment", next.AssignmentID, "--agent", next.AgentID, "--execution-generation", "2", "--reason", "Attempt to erase received responsibility"}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("replacement erased a received document review")
	}
}

func TestWorkerContextSourceDriftRejectsWithoutOverwritingReadyData(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	source := filepath.Join(f.root, workspace.DispatchContextSource(e))
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, f.root)
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID}, nil, &out, &errout); code == 0 || stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("ready context was silently rebuilt")
	}
	local, _ := os.ReadFile(filepath.Join(e.Path, workspace.DispatchContextPath))
	if !bytes.Equal(local, original) {
		t.Fatal("ready Worker bytes were overwritten")
	}
}
