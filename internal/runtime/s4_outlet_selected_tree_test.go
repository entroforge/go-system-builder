package runtime_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Exercise the original S4 Writer, TR-002 gate/transition, and S5 input projection.
// The original consumer professional checks are covered by CLI composition tests.
// Only the registered report comes from disk; business materials come from Git.
func TestS4FallbackReportCrossesTR002AndS5WithSelectedGitMaterials(t *testing.T) {
	root, state, files := taskPlanningRuntimeFixture(t, true)
	inputs, err := runtime.ResolveTaskPlanningInputs(root, state, files)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := runtime.CurrentTaskPlanningBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildTaskPlanning(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	issued, err := runtime.RecordTaskPlanningPacket(root, statePath, journalPath, packet, files, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatalf("original S4 packet Writer: %v", err)
	}
	indexPath, taskPath := "docs/dev/tasks/index-REQ-001.md", "docs/dev/tasks/TASK-001-01.md"
	writeTaskPlanningFile(t, root, taskPath, "# TASK-001-01: User view\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n> Dispatch policy: waves-v1\n> Primary contract: FE-001\n\n## Delivered Clauses\n\n| Contract | Delivered clauses |\n|---|---|\n| FE-001 | §1 |\n\n## Scope\n\n| Type | Paths |\n|---|---|\n| prospective write paths | `web/ui` |\n\n## Closing Contract\n\nassert owned tests == pass\n")
	writeTaskPlanningFile(t, root, indexPath, "# Dispatch plan\n> REQ: REQ-001\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n- [ ] [TASK](TASK-001-01.md)\n")
	cancelledPath := "docs/dev/tasks/TASK-001-02.md"
	writeTaskPlanningFile(t, root, cancelledPath, "> Status: cancelled\n> Version: v1.0.0\n> Source REQ refs: REQ-001\n")
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-b", "test-development")
	git("add", "docs")
	git("-c", "user.name=S4 Test", "-c", "user.email=s4@example.invalid", "commit", "-m", "accepted professional material")
	view, err := fileview.New(root, "refs/heads/test-development", []fileview.Rule{
		{Path: ".", Source: "git_tree"}, {Path: ".claude/evidence", Source: "disk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	files = view
	// These checkout copies must never qualify the TASK/index/contract inputs.
	for _, path := range []string{taskPath, indexPath, "docs/dev/contracts/FE-001.md"} {
		writeTaskPlanningFile(t, root, path, "# Unaccepted checkout copy\n> Status: draft\n")
	}
	writeTaskPlanningFile(t, root, taskPath, "# Unaccepted checkout TASK\n> Status: draft\n> Source REQ refs: REQ-001\n")
	answer := workpacket.TaskPlanningAnswer{Conclusion: stringPtr("pass"), IndexPath: stringPtr(indexPath), TaskPaths: []string{taskPath, cancelledPath}}
	artifacts, err := runtime.TaskPlanningArtifacts(root, issued.State, packet, answer, files)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := workpacket.TaskPlanningEvidence(packet, "E-S4-001", artifacts, answer, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	const evidencePath = ".claude/evidence/E-S4-001.json"
	writeBytes(t, root, evidencePath, []byte("unknown occupied report"))
	draft := packet.Draft
	draft.Answer = answer
	answerJSON, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: issued.Revision, ID: "E-S4-001", Kind: "planning_task", Path: evidencePath,
		ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: semantic.RuntimeCandidateValidator{},
		Submission: &runtime.SubmissionRequest{PacketID: packet.ID, Owner: "Main", AnswerJSON: answerJSON, EvidenceJSON: data, Files: files},
	})
	if err != nil {
		t.Fatalf("original S4 evidence Writer: %v", err)
	}
	// The controller observes the durable pair after the reception Writer returns.
	accepted, err = runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatalf("observe accepted S4 durable pair: %v", err)
	}
	record, found, err := runtime.FindSubmission(accepted.State, packet.ID)
	if err != nil || !found {
		t.Fatalf("original S4 receipt: %t %v", found, err)
	}
	var actualPath string
	for _, raw := range accepted.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			actualPath, _ = row["path"].(string)
		}
	}
	if !strings.HasPrefix(actualPath, ".claude/evidence-alt-") {
		t.Fatalf("Writer did not register its fallback report: %q", actualPath)
	}
	if _, err := files.ReadFile(actualPath); err == nil {
		t.Fatal("fixture must exclude the fallback report from the selected Git tree")
	}
	if got, err := os.ReadFile(filepath.Join(root, evidencePath)); err != nil || string(got) != "unknown occupied report" {
		t.Fatalf("unknown preferred report changed: %q %v", got, err)
	}
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Root: root, Snapshot: accepted, Files: files,
		TransitionID: "TR-002", GateID: "GATE-PLANNING-TASKS-COMPLETE",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("fallback report and selected-tree business material did not satisfy TR-002: %+v %v", gate, err)
	}
	// A valid disk copy cannot rescue an invalid task in another selected tree.
	acceptedTask, err := files.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	git("checkout", "-b", "rejected-material")
	git("add", taskPath)
	git("-c", "user.name=S4 Test", "-c", "user.email=s4@example.invalid", "commit", "-m", "invalid selected task")
	writeBytes(t, root, taskPath, acceptedTask)
	badView, err := fileview.New(root, "refs/heads/rejected-material", []fileview.Rule{
		{Path: ".", Source: "git_tree"}, {Path: ".claude/evidence", Source: "disk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	badGate, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Root: root, Snapshot: accepted, Files: badView,
		TransitionID: "TR-002", GateID: "GATE-PLANNING-TASKS-COMPLETE",
	})
	if err != nil || badGate.Status != qualitygate.StatusNotReady ||
		len(badGate.Missing) != 1 || badGate.Missing[0] != "document:task:complete" || len(badGate.Conflicts) != 0 {
		t.Fatalf("invalid selected TASK must report the missing complete TASK, despite a valid disk copy: %+v %v", badGate, err)
	}
	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: badView, TransitionID: "TR-002", ExpectedRevision: accepted.Revision,
		ExpectedRuntimeID: packet.Binding.RuntimeID, Actor: "orchestrator", SubmissionFollowupID: record.FollowupID,
	}); err == nil || !strings.Contains(err.Error(), taskPath+`: status is "draft"`) {
		t.Fatalf("TR-002 must diagnose the selected TASK draft status: %v", err)
	}
	advanced, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: files, TransitionID: "TR-002", ExpectedRevision: accepted.Revision,
		ExpectedRuntimeID: packet.Binding.RuntimeID, Actor: "orchestrator", SubmissionFollowupID: record.FollowupID,
	})
	if err != nil {
		t.Fatalf("TR-002 must read the registered disk report and selected-tree batch: %v", err)
	}
	// S5 preparation likewise starts from the committed transition pair.
	advanced, err = runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatalf("observe TR-002 durable pair before S5: %v", err)
	}
	registered := map[string]bool{}
	for _, raw := range advanced.State["documents"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "task" || row["kind"] == "dispatch_plan" {
			registered[row["path"].(string)] = true
		}
	}
	if len(registered) != 2 || !registered[indexPath] || !registered[taskPath] {
		t.Fatalf("TR-002 registered a different batch: %#v", registered)
	}
	inputsS5, err := runtime.S5FormalInputs(root, advanced.State, files)
	if err != nil {
		t.Fatalf("S5 could not consume the actual fallback S4 tuple: %v", err)
	}
	formal := map[string]bool{}
	for _, ref := range inputsS5.References {
		formal[ref.Path] = true
	}
	for _, path := range []string{indexPath, taskPath, packet.Binding.Requirement.Path, "docs/dev/contracts/FE-001.md"} {
		if !formal[path] {
			t.Errorf("S5 formal inputs lost original S4 material %s", path)
		}
	}
	if err := os.Remove(filepath.Join(root, actualPath)); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.S5FormalInputs(root, advanced.State, files); err == nil || !strings.Contains(err.Error(), "read accepted S4 envelope "+actualPath) {
		t.Fatalf("missing actual report must retain its original S5 context: %v", err)
	}
}
