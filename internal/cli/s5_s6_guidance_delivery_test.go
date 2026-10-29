package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5MainObservationIsReadOnlyAndRedeliversChangedResponsibility(t *testing.T) {
	f := newStageAnswerFixture(t)
	statePath, journalPath := filepath.Join(f.root, ".claude/loop-state.json"), filepath.Join(f.root, ".claude/loop-events.jsonl")
	before, _ := os.ReadFile(statePath)
	journal, _ := os.ReadFile(journalPath)
	first := designGuidanceEvent(t, f.root, "s5-guidance", "PreToolUse", "Read")
	if !strings.Contains(first, "source root "+f.root) || !strings.Contains(first, "task_analysis.path") {
		t.Fatalf("native dispatch omitted the root needed to resolve actual packet references: %s", first)
	}
	for _, packet := range f.packets {
		if !strings.Contains(first, packet.ViewPath) || !strings.Contains(first, packet.AnswerPath) {
			t.Fatalf("first observation omitted exact independent responsibility: %s", first)
		}
	}
	for _, tool := range []string{"Read", "Write", "Bash", "mcp__sandbox__execute"} {
		repeated := designGuidanceEvent(t, f.root, "s5-guidance", "PreToolUse", tool)
		if !strings.Contains(repeated, "S5 guidance is unchanged") || len(repeated) >= len(first)/2 {
			t.Fatalf("ordinary %s repeated the full S5 context: first=%d repeated=%d %s", tool, len(first), len(repeated), repeated)
		}
	}
	// A source closure that is no longer readable must still be checked at
	// reception, not reconstructed by an unrelated Main read.
	source := filepath.Join(f.root, f.packets[0].Inputs[0].Path)
	sourceBytes, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	observed := designGuidanceEvent(t, f.root, "s5-new-session", "PreToolUse", "Read")
	if !strings.Contains(observed, f.packets[0].ViewPath) || strings.Contains(observed, "packet issuance deferred") {
		t.Fatalf("ordinary observation rebuilt frozen source closure: %s", observed)
	}
	if err := os.WriteFile(source, sourceBytes, 0600); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		designGuidanceEvent(t, f.root, "s5-guidance", event, "Read")
		restored := designGuidanceEvent(t, f.root, "s5-guidance", "PreToolUse", "Read")
		if strings.Contains(restored, "guidance is unchanged") || !strings.Contains(restored, f.packets[0].ViewPath) {
			t.Fatalf("%s lost recovered responsibility: %s", event, restored)
		}
	}
	if err := os.Remove(filepath.Join(f.root, f.packets[0].ViewPath)); err != nil {
		t.Fatal(err)
	}
	missing := designGuidanceEvent(t, f.root, "s5-guidance", "PreToolUse", "Read")
	if strings.Contains(missing, "guidance is unchanged") || !strings.Contains(missing, f.packets[0].ViewPath) || !strings.Contains(missing, "runtime stage-answer issue") || !strings.Contains(missing, "no such file") {
		t.Fatalf("new exact preparation failure hidden: %s", missing)
	}
	if got, _ := os.ReadFile(statePath); !bytes.Equal(got, before) {
		t.Fatal("ordinary S5 observation changed Runtime")
	}
	if got, _ := os.ReadFile(journalPath); !bytes.Equal(got, journal) {
		t.Fatal("ordinary S5 observation changed business journal")
	}
	t.Logf("S5 guidance bytes: first=%d unchanged=%d", len(first), len(designGuidanceEvent(t, f.root, "s5-guidance", "PreToolUse", "Read")))
	stageJSON(t, f.root, f.packets[0].AnswerPath, completedDraft(f.packets[0], "pass"))
	authored, _ := os.ReadFile(filepath.Join(f.root, f.packets[0].AnswerPath))
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", f.root)
	if _, err := os.Stat(filepath.Join(f.root, f.packets[0].ViewPath)); err != nil {
		t.Fatalf("named original recovery did not restore the missing view: %v", err)
	}
	if answer, _ := os.ReadFile(filepath.Join(f.root, f.packets[0].AnswerPath)); !bytes.Equal(answer, authored) {
		t.Fatal("view restoration replaced the professional answer")
	}
	if _, received, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, f.packets[0].ID); err != nil || received {
		t.Fatalf("preparation recovery received a private answer: %v %v", received, err)
	}
}

func TestS5MainObservationPreservesFrozenExecutionRecovery(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	before := stageSnapshot(t, f.root).Revision
	view := filepath.Join(e.Path, workpacket.WorkerViewPath)
	current := designGuidanceEvent(t, f.root, "s5-frozen", "PreToolUse", "Read")
	if !strings.Contains(current, view) || !strings.Contains(current, "continue the registered frozen execution") || strings.Contains(current, filepath.Join(f.root, p.ViewPath)) {
		t.Fatalf("frozen responsibility was projected as a new Main/native source: %s", current)
	}
	if err := os.Remove(view); err != nil {
		t.Fatal(err)
	}
	missing := designGuidanceEvent(t, f.root, "s5-frozen", "PreToolUse", "Read")
	if !strings.Contains(missing, view) || !strings.Contains(missing, "frozen-execution.md") || !strings.Contains(missing, "does not restore a frozen execution copy") || strings.Contains(missing, "runtime stage-answer issue --root") {
		t.Fatalf("frozen missing-file recovery fell back to Main issuance: %s", missing)
	}
	if stageSnapshot(t, f.root).Revision != before {
		t.Fatal("frozen observation mutated Runtime")
	}
}

func TestS5MainObservationOmitsSupersededReception(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "superseded-reviewer", filepath.Join(f.root, p.ViewPath)); code != 0 {
		t.Fatal(diagnostic)
	}
	state := stageSnapshot(t, f.root).State
	for _, raw := range state["entities"].(map[string]any)["submissions"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] == p.ID {
			// Model a retained historical receipt replaced by the original
			// successor. A read must not redispatch or assert it is current.
			row["superseded_by"] = "original-successor-reception"
		}
	}
	stageJSON(t, f.root, ".claude/loop-state.json", state)
	current := designGuidanceEvent(t, f.root, "s5-superseded", "PreToolUse", "Read")
	if strings.Contains(current, "S5 Reviewer "+p.Binding.Owner+"/") || strings.Contains(current, p.ViewPath) {
		t.Fatalf("superseded receipt was projected as current responsibility: %s", current)
	}
	if !strings.Contains(current, f.packets[1].ViewPath) {
		t.Fatal("unrelated current peer was suppressed")
	}
}

func TestS5MainObservationNamesPartiallyIssuedResponsibility(t *testing.T) {
	f := newStageAnswerFixture(t)
	missing := f.packets[1]
	state := stageSnapshot(t, f.root).State
	entities := state["entities"].(map[string]any)
	var retained []any
	for _, raw := range entities["work_packets"].([]any) {
		if raw.(map[string]any)["packet_id"] != missing.ID {
			retained = append(retained, raw)
		}
	}
	entities["work_packets"] = retained
	stageJSON(t, f.root, ".claude/loop-state.json", state)
	before := stageSnapshot(t, f.root).Revision
	output := designGuidanceEvent(t, f.root, "s5-partial", "PreToolUse", "Read")
	if !strings.Contains(output, missing.Binding.Responsibility+" has no current issued questionnaire") || !strings.Contains(output, f.packets[0].ViewPath) || !strings.Contains(output, "runtime stage-answer issue") {
		t.Fatalf("partial original preparation was hidden or guessed: %s", output)
	}
	if stageSnapshot(t, f.root).Revision != before {
		t.Fatal("observation repaired partial issuance")
	}
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", f.root)
	if _, found, err := runtime.FindWorkPacket(stageSnapshot(t, f.root).State, missing.ID); err != nil || !found {
		t.Fatalf("named entry did not restore the original missing issuance: %v %v", found, err)
	}
}

func TestS5ReturnToS6ProvidesPreparationInsteadOfWaitingForUnstartedBuilder(t *testing.T) {
	f := newStageAnswerFixture(t)
	for _, p := range f.packets {
		stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
		if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "opaque-reviewer-"+p.Binding.Owner, filepath.Join(f.root, p.ViewPath)); code != 0 {
			t.Fatalf("native review reception: %d %s", code, diagnostic)
		}
	}
	snapshot := stageSnapshot(t, f.root)
	if snapshot.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatal("last original Stop did not enter S6")
	}
	p := f.packets[len(f.packets)-1]
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s5-to-s6", "cwd": f.root, "tool_name": "Agent", "tool_input": map[string]any{"prompt": filepath.Join(f.root, p.ViewPath)}, "tool_response": map[string]any{"text": filepath.Join(f.root, p.AnswerPath)}})
	var returned, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(body), &returned, &diagnostic); code != 0 {
		t.Fatalf("Main return: %d %s", code, &diagnostic)
	}
	if !strings.Contains(returned.String(), "read-only") || !strings.Contains(returned.String(), "submission_key") || !strings.Contains(returned.String(), "S6") {
		t.Fatalf("Main return lost reception after transition: %s", &returned)
	}
	next := designGuidanceEvent(t, f.root, "s5-to-s6", "PreToolUse", "Read")
	for _, want := range []string{"s6 prepare", "s6 status --advice", "native Agent"} {
		if !strings.Contains(next, want) {
			t.Fatalf("natural S5 return → Main observation omitted %s: %s", want, next)
		}
	}
	for _, stale := range []string{"waits for each assigned Builder", "waits for its Result", "register-workgroup remains the registration step", "questionnaires using the paths supplied by the listener"} {
		if strings.Contains(next, stale) {
			t.Fatalf("unprepared S6 pretended execution had started: %s", next)
		}
	}
	for _, p := range f.packets {
		if _, found, err := runtime.FindSubmission(stageSnapshot(t, f.root).State, p.ID); err != nil || !found {
			t.Fatalf("Main observation lost original reception: %v %v", found, err)
		}
	}
}

func TestS6EntryNamesEligibleTaskThenOriginalPreparedBrief(t *testing.T) {
	root, task, _ := newS6RecordContextFixture(t)
	entry := designGuidanceEvent(t, root, "s6-entry", "SessionStart", "Read")
	if !strings.Contains(entry, task+" is eligible") || !strings.Contains(entry, "s6 prepare --root") || !strings.Contains(entry, "--task '") {
		t.Fatalf("S6 entry omitted its actual ready TASK/preparation: %s", entry)
	}
	stageCLI(t, "s6", "prepare", "--root", root, "--task", task, "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-"+task)
	if err != nil || !found {
		t.Fatal(err)
	}
	brief, _, _ := assignmentrecord.NativePaths(record)
	prepared := designGuidanceEvent(t, root, "s6-entry", "PreToolUse", "Read")
	if !strings.Contains(prepared, brief) || !strings.Contains(prepared, "does not prove the Agent has started") || !strings.Contains(prepared, "worktree isolation") {
		t.Fatalf("prepared record falsely implied running platform work: %s", prepared)
	}
}
