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
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workspace"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// newS6MainBaselineFixture starts a Runtime through the real TR-003 Writer, so
// the current generation owns the original common Main baseline. The baseline
// is a structural fact of the committed original stage: the journal keeps the
// original transition receipt and the Runtime keeps the registered common Main
// start of this generation. The S6 batch recheck needs that baseline before it
// may read the current batch.
func newS6MainBaselineFixture(t *testing.T, sources map[string][]byte) (string, string) {
	t.Helper()
	root, task, _ := newS6RecordContextFixtureWithReadSources(t, sources)
	state := stageSnapshot(t, root).State
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	req039fixtures.SeedDocumentPassS5(t, root, state, "s6-baseline-spec-reviewer", "s6-baseline-task-reviewer")
	req039fixtures.WriteState(t, root, state)
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/test-development", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	next, err := transition.Apply(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), transition.Request{
		Files: view, TransitionID: "TR-003", ExpectedRevision: -1,
		ExpectedRuntimeID: state["runtime_id"].(string), Actor: "orchestrator",
		Evidence:         map[string]string{"document_review_record": "ev-dv-spec"},
		GateEvidenceRefs: []string{"ev-dv-spec", "ev-dv-task"},
		GateID:           "GATE-DOCUMENT-PASS", ProducerResponsibility: "document_verifier"})
	if err != nil {
		t.Fatalf("real TR-003: %v", err)
	}
	journal, err := runtime.ReadJournalForState(root, next.State)
	if err != nil {
		t.Fatal(err)
	}
	start, err := runtime.RequireCurrentS6MainStart(next.State, journal)
	if err != nil || len(start.TaskSet) != 1 || start.TaskSet[0].TaskID != task {
		t.Fatalf("exact common Main batch: %+v %v", start, err)
	}
	return root, task
}

func TestS6UnchangedPreToolUseReusesObservation(t *testing.T) {
	root, task := newS6MainBaselineFixture(t, map[string][]byte{"go.mod": []byte("module observation.fixture\n\ngo 1.26\n"), "src/pkg/input_test.go": []byte("package pkg\nimport \"testing\"\nfunc TestPackage(t *testing.T) {}\n")})
	runner := &req039fixtures.CLIRunner{}
	body := req039fixtures.PreToolUseBody("s6-observation", "Agent", map[string]any{"subagent_type": "s6-prior-delegation-marker"})
	run := func() string {
		t.Helper()
		code, out, errOut := req039fixtures.RunHook(t, runner, root, "PreToolUse", body)
		if code != 0 {
			t.Fatalf("PreToolUse: code=%d stderr=%s", code, errOut)
		}
		return out
	}
	// The ordinary wire omits ReadOrder and the policy audit envelope does
	// not carry Guidance. Check the original persisted role path, then use
	// the preflight Questions actually rendered by compactGuidance to detect
	// accidental reuse of the prior Agent's Guidance for a Main Read.
	first := run()
	initial := req039fixtures.ReadState(t, root)
	initialMilestone := initial["milestone"].(map[string]any)
	readOrder, err := json.Marshal(initialMilestone["read_order"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(readOrder, []byte(".claude/agents/s6-prior-delegation-marker.md")) {
		t.Fatal("initial Agent milestone did not include its delegation preflight")
	}
	if !strings.Contains(first, "Preflight questions:") {
		t.Fatal("initial Agent wire did not render its preflight Questions")
	}
	body = req039fixtures.PreToolUseBody("s6-observation", "Read", map[string]any{"file_path": "README.md"})
	var mainRead map[string]any
	if err := json.Unmarshal([]byte(body), &mainRead); err != nil {
		t.Fatal(err)
	}
	mainRead["agent_id"] = "Main"
	mainBody, err := json.Marshal(mainRead)
	if err != nil {
		t.Fatal(err)
	}
	body = string(mainBody)
	before := controller.MetricsGateEvaluations
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	stateBytes, journalBytes := read(statePath), read(journalPath)
	for i := 0; i < 3; i++ {
		out := run()
		if strings.Contains(out, "Preflight questions:") {
			t.Fatal("prior Agent preflight leaked into the current Main Read")
		}
		gate, _ := req039fixtures.ParseQualityGate(t, out)
		if gate["transition_committed"] == true {
			t.Fatal("an existing observation was replayed as a new transition")
		}
	}
	if controller.MetricsGateEvaluations != before {
		t.Fatal("unchanged ordinary Read reran gate/source evaluation")
	}
	if !bytes.Equal(stateBytes, read(statePath)) || !bytes.Equal(journalBytes, read(journalPath)) {
		t.Fatal("unchanged ordinary Read rewrote Runtime or journal")
	}

	// A new revision with the same stage outcome must advance the existing
	// observation anchor once, then become read-only again.
	unchangedOutcome := req039fixtures.ReadState(t, root)
	unchangedOutcome["revision"] = req039fixtures.Revision(unchangedOutcome) + 1
	req039fixtures.WriteState(t, root, unchangedOutcome)
	run()
	observed := req039fixtures.ReadState(t, root)
	milestone := observed["milestone"].(map[string]any)
	if milestone["source_revision"] != observed["revision"] {
		t.Fatalf("same-outcome observation did not advance source_revision: %v / %v", milestone["source_revision"], observed["revision"])
	}
	before = controller.MetricsGateEvaluations
	stateBytes, journalBytes = read(statePath), read(journalPath)
	run()
	if controller.MetricsGateEvaluations != before || !bytes.Equal(stateBytes, read(statePath)) || !bytes.Equal(journalBytes, read(journalPath)) {
		t.Fatal("same-outcome observation did not settle after its new revision")
	}

	// Model a newly recorded batch through the original native delivery: the
	// real S6 preparation, the real Builder Result and Main's real merge. Its
	// new Runtime revision must defeat observation reuse.
	stageCLI(t, "s6", "prepare", "--root", root, "--task", task, "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-"+task)
	if err != nil || !found {
		t.Fatalf("native registration: %+v %v", record, err)
	}
	source := filepath.Join(t.TempDir(), "native-worktree")
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", "-b", "native/observation", source, "HEAD"); err != nil {
		t.Fatal(err)
	}
	nativeCommit(t, source)
	nativeAnswer(t, source, record)
	brief, _, result := assignmentrecord.NativePaths(record)
	if code, output := nativeEvent(t, root, source, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(source, result), false); code != 0 {
		t.Fatalf("original Builder Result was not received: %d %s", code, output)
	}
	before = controller.MetricsGateEvaluations
	run()
	if controller.MetricsGateEvaluations == before {
		t.Fatal("changed Runtime facts did not execute the original gate")
	}
	// The recorded batch reaches TR-006 at Main's integration exit, the only
	// place that commits it.
	var integrated, diagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID, "--agent-id", record.OwnerAgentID}, nil, &integrated, &diagnostic); code != 0 {
		t.Fatalf("Main integration failed: %d %s %s", code, &integrated, &diagnostic)
	}
	if lifecycle, phase := req039fixtures.Lifecycle(req039fixtures.ReadState(t, root)); lifecycle != "verification" || phase != "planned" {
		t.Fatalf("changed ready batch did not advance through TR-006: %s/%s", lifecycle, phase)
	}
}
