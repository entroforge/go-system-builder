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
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workspace"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Unlike nativeDeliveryFixture this starts through the real TR-003 Writer:
// the common Main start, original journal receipt and exact TASK batch exist.
func s6HandoffFixture(t *testing.T, remainingTask ...bool) (string, string, assignmentrecord.Record) {
	t.Helper()
	root, task, _ := newS6RecordContextFixtureWithReadSources(t, map[string][]byte{"go.mod": []byte("module handoff.fixture\n\ngo 1.26\n"), "src/pkg/input_test.go": []byte("package pkg\nimport \"testing\"\nfunc TestPackage(t *testing.T) {}\n")})
	state := stageSnapshot(t, root).State
	expectedTasks := 1
	if len(remainingTask) > 0 && remainingTask[0] {
		data, err := os.ReadFile(filepath.Join(root, "docs/dev/tasks/TASK-039-01.md"))
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.ReplaceAll(data, []byte("TASK-039-01"), []byte("TASK-039-02"))
		path := "docs/dev/tasks/TASK-039-02.md"
		if err := os.WriteFile(filepath.Join(root, path), data, 0644); err != nil {
			t.Fatal(err)
		}
		state["documents"] = append(state["documents"].([]any), s6RecordContextDocument("TASK-039-02", "task", path, "v1.0.0", data))
		indexPath := filepath.Join(root, "docs/dev/tasks/index-REQ-039.md")
		index, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatal(err)
		}
		index = append(index, []byte("\n- [ ] [remaining reviewed task](TASK-039-02.md)\n")...)
		if err := os.WriteFile(indexPath, index, 0644); err != nil {
			t.Fatal(err)
		}
		for _, raw := range state["documents"].([]any) {
			doc := raw.(map[string]any)
			if doc["kind"] == "dispatch_plan" {
				doc["sha256"] = fixtures.Sha256Hex(index)
			}
		}
		fixtures.CommitCurrentFixture(t, root)
		expectedTasks = 2
	}
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	fixtures.SeedDocumentPassS5(t, root, state, "handoff-spec-reviewer", "handoff-task-reviewer")
	fixtures.WriteState(t, root, state)
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/test-development", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	next, err := transition.Apply(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), transition.Request{Files: view, TransitionID: "TR-003", ExpectedRevision: -1, ExpectedRuntimeID: state["runtime_id"].(string), Actor: "orchestrator", Evidence: map[string]string{"document_review_record": "ev-dv-spec"}, GateEvidenceRefs: []string{"ev-dv-spec", "ev-dv-task"}, GateID: "GATE-DOCUMENT-PASS", GateFingerprint: "sha256:handoff-fixture", ProducerResponsibility: "document_verifier"})
	if err != nil {
		t.Fatalf("real TR-003: %v", err)
	}
	journal, err := runtime.ReadJournalForState(root, next.State)
	if err != nil {
		t.Fatal(err)
	}
	start, err := runtime.RequireCurrentS6MainStart(next.State, journal)
	if err != nil || len(start.TaskSet) != expectedTasks || start.TaskSet[0].TaskID != task {
		t.Fatalf("exact common Main batch: %+v %v", start, err)
	}
	stageCLI(t, "s6", "prepare", "--root", root, "--task", task, "--role", "backend-builder")
	r, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-"+task)
	if err != nil || !found {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "handoff-worker")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/handoff", source, "HEAD"); err != nil {
		t.Fatal(err)
	}
	startBrief, _, _ := assignmentrecord.NativePaths(r)
	if code, output := nativeEvent(t, root, source, "SubagentStart", filepath.Join(root, startBrief), false); code != 0 {
		t.Fatalf("native Start: %d %s", code, output)
	}
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	code, stopDiagnostic := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false)
	if code != 0 {
		t.Fatalf("receive native Result: %d %s", code, stopDiagnostic)
	}
	accepted, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatalf("native Stop did not leave a readable accepted Runtime: %v; Stop diagnostic: %s", err, stopDiagnostic)
	}
	received := false
	for _, raw := range accepted.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == r.OwnerAgentID {
			ref, _ := row["completion_reported_ref"].(string)
			received = ref != ""
		}
	}
	if !received {
		t.Fatalf("Stop returned without the original completion receipt; diagnostic: %s", stopDiagnostic)
	}
	return root, source, r
}

func integrateHandoff(t *testing.T, root string, r assignmentrecord.Record, options ...string) string {
	t.Helper()
	var out, diagnostic bytes.Buffer
	args := append([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}, options...)
	if code := cli.Run(args, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("integration %d: %s %s", code, &out, &diagnostic)
	}
	return out.String()
}

func TestS6LastIntegrationHandsOffToS7InOriginalCLI(t *testing.T) {
	root, _, r := s6HandoffFixture(t)
	output := integrateHandoff(t, root, r)
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["state"] != "verification" || !strings.Contains(output, "TR-006") || !strings.Contains(output, "ReviewPlan questionnaire:") {
		t.Fatalf("same CLI did not hand off: %s lifecycle=%v", output, after.State["lifecycle"])
	}
	var receipt struct {
		Handoff string `json:"handoff"`
	}
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"review-plan", ".md"} {
		if !strings.Contains(receipt.Handoff, name) {
			t.Fatalf("missing exact planning materials: %s", output)
		}
	}
	// PostToolUse is a projection of the CLI receipt, never a second Writer.
	for _, owner := range []string{"", "Main"} {
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "agent_id": owner, "tool_name": "Bash", "tool_input": map[string]any{"command": "loop-harness runtime task-integrate --assignment-id " + r.AssignmentID}, "tool_response": map[string]any{"stdout": output}})
		var hookOut, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(payload), &hookOut, &diagnostic); code != 0 {
			t.Fatalf("PostToolUse: %d %s", code, &diagnostic)
		}
		if stageSnapshot(t, root).Revision != after.Revision {
			t.Fatal("PostToolUse repeated the handoff Writer")
		}
		if !strings.Contains(hookOut.String(), "TR-006") {
			t.Fatalf("PostToolUse lost original receipt: %s", &hookOut)
		}
	}
	// Repeating the original CLI consumes its durable checkpoint, not a
	// second merge, delivery or review round.
	beforeHead, _ := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	integrateHandoff(t, root, r)
	afterHead, _ := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if beforeHead != afterHead || stageSnapshot(t, root).State["review"].(map[string]any)["round"] != after.State["review"].(map[string]any)["round"] {
		t.Fatal("CLI retry duplicated merge or review round")
	}
}

func TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate(t *testing.T) {
	root, source, r := nativeDeliveryFixture(t)
	nativeCommit(t, source)
	nativeAnswer(t, source, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	if code, output := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false); code != 0 {
		t.Fatalf("receive: %d %s", code, output)
	}
	output := integrateHandoff(t, root, r)
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "building" || !strings.Contains(output, "integration is recorded") || !strings.Contains(output, "no original TR-003 common Main baseline") {
		t.Fatalf("missing common Main start passed Gate: %s", output)
	}
}

func TestS6S7PreparationFailureKeepsTransitionAndRestoresOriginalMaterials(t *testing.T) {
	root, _, r := s6HandoffFixture(t)
	// A filesystem obstruction exercises failure after the real TR-006 commit.
	blocked := filepath.Join(root, ".claude/submissions", stageSnapshot(t, root).State["runtime_id"].(string), "g1/S7")
	if err := os.MkdirAll(filepath.Dir(blocked), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	output := integrateHandoff(t, root, r)
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["state"] != "verification" || (!strings.Contains(output, "preparation failed") && !strings.Contains(output, "unavailable")) || !strings.Contains(output, "s7 draft") {
		t.Fatalf("failure hid durable handoff/recovery: %s", output)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	path := review.PlanQuestionnairePath(root, after.State)
	stageCLI(t, "s7", "draft", "--root", root, "--out", path)
	guide := strings.TrimSuffix(path, ".json") + ".md"
	if _, err := os.Stat(guide); err != nil {
		t.Fatal(err)
	}
	// A missing guide is repaired without replacing Main's authored answer.
	authored := []byte("retained professional draft\n")
	if err := os.WriteFile(path, authored, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(guide); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "s7", "draft", "--root", root, "--out", path)
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, authored) {
		t.Fatalf("recovery replaced professional answer: %s %v", got, err)
	}
	if stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("material recovery repeated Runtime/ReviewRound Writer")
	}
}

func TestS6NonLastIntegrationNamesRemainingResponsibility(t *testing.T) {
	root, _, r := s6HandoffFixture(t, true)
	output := integrateHandoff(t, root, r)
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "building" || !strings.Contains(output, "TASK-039-02") || !strings.Contains(output, "do not reopen this completed Builder") {
		t.Fatalf("non-last integration lost remaining responsibility: %s", output)
	}
}

func TestS6ActualMainCheckFailureCannotHandOff(t *testing.T) {
	root, _, r := s6HandoffFixture(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sh"), []byte("#!/bin/sh\nprintf 'intentional Main check failure\\n' >&2\nexit 29\n"), 0700); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+originalPath)
	output := integrateHandoff(t, root, r)
	var receipt struct {
		Blocked bool   `json:"blocked"`
		Handoff string `json:"handoff"`
	}
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Blocked || receipt.Handoff != "" || stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatalf("failed actual Main check entered S7: %s", output)
	}
	mergedHead, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// Resolving the check environment allows re-entry through the original
	// command. Its existing merge is retained; only missing checks/closure run.
	t.Setenv("PATH", originalPath)
	recovered := integrateHandoff(t, root, r, "--retry-preserved")
	afterHead, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil || afterHead != mergedHead {
		t.Fatalf("re-entering integration changed the recorded merge: %v", err)
	}
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "verification" || !strings.Contains(recovered, "TR-006") {
		t.Fatalf("original command did not resume checks and stage handoff: %s", recovered)
	}
}
