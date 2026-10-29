package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"github.com/entroforge/go-system-builder/tests/fixtures/factory"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func currentS5ReworkFixture(t *testing.T) (string, []workpacket.Packet) {
	t.Helper()
	root, p := newTaskPlanningAnswerFixture(t)
	for _, rel := range []string{"docs/control/agent-protocol.md", "docs/rules"} {
		copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, rel))
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.SaveTaskPlanning(context.Background(), p.AnswerPath); err != nil || !r.Accepted {
		t.Fatalf("S4 original reception: %+v %v", r, err)
	}
	settleMainPlanningFollowups(t, root)
	return root, registerAcceptedS5Reviewers(t, root, "TASK-039-01", "docs/dev/tasks/TASK-039-01.md")
}

// A bounded Hook may finish the professional reception before Main's control
// followup. Resume that original reception, never save the answer again.
func settleOriginalS5Followup(t *testing.T, root string, p workpacket.Packet) {
	t.Helper()
	before, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, p.ID)
	if err != nil || !found || before.ContentHash == "" || before.EvidenceID == "" {
		t.Fatalf("original S5 reception missing: %+v found=%t err=%v", before, found, err)
	}
	if before.ControlStatus != "pending" {
		return
	}
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Recover(context.Background(), p.Binding.Owner, 1)
	if err != nil || len(recovered) != 1 || !recovered[0].Accepted || !recovered[0].WorkComplete || recovered[0].ConsumerPending {
		t.Fatalf("Main failed to resume the original S5 reception: %+v err=%v", recovered, err)
	}
	after, found, err := runtime.FindSubmission(stageSnapshot(t, root).State, p.ID)
	if err != nil || !found || after.ControlStatus == "pending" || after.Key != before.Key || after.ContentHash != before.ContentHash || after.EvidenceID != before.EvidenceID {
		t.Fatalf("S5 recovery lost or replaced the accepted answer: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestS5ReworkRetiresOriginalWorkersAndPreservesReceivedReplay(t *testing.T) {
	root, packets := currentS5ReworkFixture(t)
	binary := factory.BuildHarness(t)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	for _, p := range packets {
		stageCLI(t, "runtime", "workspace", "prepare", "--root", root, "--assignment", p.Binding.AssignmentID, "--agent", p.Binding.Owner)
	}
	before := stageSnapshot(t, root)
	registry, err := workspace.Decode(before.State)
	if err != nil || registry == nil || len(registry.Executions) != 2 {
		t.Fatalf("original review executions: %+v %v", registry, err)
	}
	manifestPaths := map[string]string{}
	for _, raw := range before.State["entities"].(map[string]any)["teams"].([]any) {
		row := raw.(map[string]any)
		path := row["manifest_ref"].(string)
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		manifestPaths[path] = string(data)
	}
	answers := map[string]workpacket.Draft{}
	for i, p := range packets {
		e := registry.Executions[p.Binding.AssignmentID]
		paths, err := registry.WorkerSubmissionPaths(e)
		if err != nil {
			t.Fatal(err)
		}
		verdict := "pass"
		if i == 1 {
			verdict = "fix_required"
		}
		draft := completedDraft(p, verdict)
		if verdict == "fix_required" {
			draft.Answer.Findings = []workpacket.Finding{{Path: "docs/dev/tasks/TASK-039-01.md#scope", Expected: "all deliverables named", Observed: "a concrete missing output"}}
		}
		answers[p.ID] = draft
		receipt := saveStageHook(t, root, e.Path, p.Binding.Owner, paths.Answer, draft, binary)
		t.Logf("S5 original receipt: %+v", receipt)
		if !receipt.Accepted || !receipt.WorkComplete {
			t.Fatalf("independent judgment was not received: %+v", receipt)
		}
		settleOriginalS5Followup(t, root, p)
		if i == 0 {
			current, err := workspace.Decode(stageSnapshot(t, root).State)
			if err != nil || len(current.Executions) != 2 || len(current.History) != 0 {
				t.Fatalf("partial review prematurely retired the current pair: %+v %v", current, err)
			}
		}
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["state"] != "planning" {
		t.Fatalf("original finding did not route TR-004: %v", after.State["lifecycle"])
	}
	design, err := runtime.CurrentDesignPacket(after.State)
	if err != nil {
		t.Fatal(err)
	}
	if _, issued, err := runtime.FindDesignPacket(after.State, design.ID); err != nil || !issued {
		t.Fatalf("S5 save did not prepare the new S2 entry: %v", err)
	}
	for _, path := range []string{design.ViewPath, design.AnswerPath} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("current S2 path unavailable %s: %v", path, err)
		}
	}
	retired, err := workspace.Decode(after.State)
	if err != nil || len(retired.Executions) != 0 || len(retired.History) != 2 {
		t.Fatalf("original executions were not moved to history: %+v %v", retired, err)
	}
	for _, e := range retired.History {
		if e.Status != "retired" {
			t.Fatalf("historical execution remained dispatchable: %+v", e)
		}
		if _, err := os.Stat(e.Path); err != nil {
			t.Fatalf("rework removed the original worktree: %v", err)
		}
	}
	for _, raw := range after.State["entities"].(map[string]any)["agents"].([]any) {
		if workstate.AgentActivity(raw.(map[string]any)) != "stopped" {
			t.Fatalf("superseded review responsibility remained active: %#v", raw)
		}
	}
	for path, data := range manifestPaths {
		actual, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(actual) != data {
			t.Fatalf("rework overwrote the historical manifest %s: %v", path, err)
		}
	}
	for _, p := range packets {
		e := registry.Executions[p.Binding.AssignmentID]
		paths, err := registry.WorkerSubmissionPaths(e)
		if err != nil {
			t.Fatal(err)
		}
		stateBytes, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
		journalBytes, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		r := saveStageHook(t, e.Path, e.Path, p.Binding.Owner, paths.Answer, answers[p.ID], binary)
		stateAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
		journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
		if !r.Accepted || !r.WorkComplete || string(stateBytes) != string(stateAfter) || string(journalBytes) != string(journalAfter) {
			t.Fatalf("retired Worker replay lost the original receipt or wrote control: %+v", r)
		}
	}
}

func TestS5DirectTR003CannotSkipCurrentIndependentPair(t *testing.T) {
	root, packets := currentS5ReworkFixture(t)
	p := packets[0]
	stageJSON(t, root, p.AnswerPath, completedDraft(p, "pass"))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Save(context.Background(), p.Binding.Owner, p.AnswerPath)
	if err != nil || !r.Accepted || len(r.DomainRefs) != 1 {
		t.Fatalf("first independent reception: %+v %v", r, err)
	}
	before := stageSnapshot(t, root)
	stateBytes, _ := os.ReadFile(s.StatePath)
	journalBytes, _ := os.ReadFile(s.JournalPath)
	_, err = transition.Apply(root, s.StatePath, s.JournalPath, transition.Request{
		TransitionID: "TR-003", Actor: "orchestrator", ExpectedRevision: before.Revision,
		Evidence: map[string]string{"document_review_record": r.DomainRefs[0]}, Files: s.Files,
	})
	if err == nil {
		t.Fatal("direct TR-003 skipped the second current independent review")
	}
	afterState, _ := os.ReadFile(s.StatePath)
	afterJournal, _ := os.ReadFile(s.JournalPath)
	if stageSnapshot(t, root).Revision != before.Revision || string(stateBytes) != string(afterState) || string(journalBytes) != string(afterJournal) {
		t.Fatal("rejected incomplete review pair changed the Runtime/journal")
	}
}

func TestS5ReworkCancelsUnreceivedPeerWithoutAcceptingANewAnswer(t *testing.T) {
	root, packets := currentS5ReworkFixture(t)
	binary := factory.BuildHarness(t)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	for _, p := range packets {
		stageCLI(t, "runtime", "workspace", "prepare", "--root", root, "--assignment", p.Binding.AssignmentID, "--agent", p.Binding.Owner)
	}
	registry, err := workspace.Decode(stageSnapshot(t, root).State)
	if err != nil || registry == nil {
		t.Fatal(err)
	}
	first, peer := packets[0], packets[1]
	e := registry.Executions[first.Binding.AssignmentID]
	paths, err := registry.WorkerSubmissionPaths(e)
	if err != nil {
		t.Fatal(err)
	}
	draft := completedDraft(first, "fix_required")
	draft.Answer.Findings = []workpacket.Finding{{Path: "docs/dev/tasks/TASK-039-01.md#scope", Expected: "all deliverables named", Observed: "a concrete missing output"}}
	if r := saveStageHook(t, root, e.Path, first.Binding.Owner, paths.Answer, draft, binary); !r.Accepted {
		t.Fatalf("original finding reception failed: %+v", r)
	} else {
		t.Logf("S5 original receipt: %+v", r)
	}
	settleOriginalS5Followup(t, root, first)
	retired, err := workspace.Decode(stageSnapshot(t, root).State)
	if err != nil || len(retired.Executions) != 0 || len(retired.History) != 2 {
		t.Fatalf("unreceived peer was not retired: %+v %v", retired, err)
	}
	peerExecution := registry.Executions[peer.Binding.AssignmentID]
	peerPaths, err := registry.WorkerSubmissionPaths(peerExecution)
	if err != nil {
		t.Fatal(err)
	}
	stageJSON(t, peerExecution.Path, peerPaths.Answer, completedDraft(peer, "pass"))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, _ := os.ReadFile(s.StatePath)
	beforeJournal, _ := os.ReadFile(s.JournalPath)
	if r, err := s.Save(context.Background(), peer.Binding.Owner, filepath.Join(peerExecution.Path, peerPaths.Answer)); err == nil || r.Accepted {
		t.Fatalf("retired unreceived peer submitted a new judgment: %+v %v", r, err)
	}
	if code, msg := stageHookCode(t, peerExecution.Path, peerExecution.Path, peer.Binding.Owner, "Stop", ""); code != 0 || strings.Contains(msg, "workspace root unavailable") {
		t.Fatalf("cancelled peer was asked to complete an obsolete questionnaire: %d %s", code, msg)
	}
	afterState, _ := os.ReadFile(s.StatePath)
	afterJournal, _ := os.ReadFile(s.JournalPath)
	if string(beforeState) != string(afterState) || string(beforeJournal) != string(afterJournal) {
		t.Fatal("retired unreceived peer changed the Runtime/journal")
	}
}
