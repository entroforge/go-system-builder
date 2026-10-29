package transition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/productbaseline"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

const s6StartRef = "refs/heads/test-development"

func prepareS6StartRuntime(t *testing.T) string {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "document_verification", "", 18)
	fixtures.SeedDocumentPassS5(t, root, state, "s6-dv-spec", "s6-dv-task")
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	fixtures.WriteState(t, root, state)
	return root
}

func s6StartView(t *testing.T, root string) *fileview.View {
	t.Helper()
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, s6StartRef, catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func applyS6TR003(t *testing.T, root string, view *fileview.View) runtime.Snapshot {
	t.Helper()
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: view, TransitionID: "TR-003", ExpectedRevision: -1,
		ExpectedRuntimeID: "loop-req039-ct", Actor: "orchestrator",
		Evidence:         map[string]string{"document_review_record": "ev-dv-spec"},
		GateEvidenceRefs: []string{"ev-dv-spec", "ev-dv-task"},
		GateID:           "GATE-DOCUMENT-PASS", GateFingerprint: "sha256:s6-main-start-test",
		ProducerResponsibility: "document_verifier",
	})
	if err != nil {
		t.Fatalf("real TR-003 Writer: %v", err)
	}
	return next
}

func TestTR003CapturesPinnedMainStartAndOriginalJournalReceipt(t *testing.T) {
	root := prepareS6StartRuntime(t)
	view := s6StartView(t, root)
	wantCommit := view.Commit
	next := applyS6TR003(t, root, view)

	journal, err := runtime.ReadJournalForState(root, next.State)
	if err != nil {
		t.Fatal(err)
	}
	start, err := runtime.RequireCurrentS6MainStart(next.State, journal)
	if err != nil {
		t.Fatal(err)
	}
	if start.StartCommit != wantCommit {
		t.Fatalf("captured start commit=%s, pinned TR-003 View commit=%s", start.StartCommit, wantCommit)
	}
	if start.ProjectRoot != root || start.DevelopmentBranch != "test-development" {
		t.Fatalf("captured root/branch=%s/%s", start.ProjectRoot, start.DevelopmentBranch)
	}
	if len(start.TaskSet) != 1 || start.TaskSet[0].TaskID != "TASK-039-01" || start.TaskSet[0].Path != "docs/dev/tasks/TASK-039-01-loop-definition.md" {
		t.Fatalf("captured exact TR-003 task set=%+v", start.TaskSet)
	}

	// A later Main commit and same-generation register action must retain the
	// original TR-003 start instead of rebasing the generation silently.
	note := filepath.Join(root, "s6-baseline-later-main-change.txt")
	if err := os.WriteFile(note, []byte("later commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtures.CommitCurrentFixture(t, root)
	relocked := s6StartView(t, root)
	before, _ := json.Marshal(start)
	result, err := callRegisterExecutionBatch(root, next.State, relocked)
	if err != nil {
		t.Fatalf("same-generation register retry: %v", err)
	}
	var after productbaseline.S6MainStart
	if err := json.Unmarshal(mustBaselineStartJSON(t, next.State), &after); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(after)
	if !reflect.DeepEqual(got, before) || !strings.Contains(result.Detail, start.ReceiptSHA256) {
		t.Fatalf("same-generation retry changed the start: before=%s after=%s detail=%q", before, got, result.Detail)
	}

	// Removing the saved start after an already committed TR-003 is history
	// loss, not permission to infer a replacement from today's branch.
	missing := cloneState(t, next.State)
	delete(missing["baseline"].(map[string]any), "s6_main_starts")
	if _, err := callRegisterExecutionBatch(root, missing, relocked); err == nil || !strings.Contains(err.Error(), "refusing to infer") {
		t.Fatalf("missing historical start was backfilled: %v", err)
	}
}

func TestTR003RejectsPinnedViewAfterMainHeadDrifts(t *testing.T) {
	root := prepareS6StartRuntime(t)
	stale := s6StartView(t, root)
	tracked := filepath.Join(root, "docs", "requirements", "REQ-039-loop-control-plane.md")
	if err := os.WriteFile(tracked, []byte("# REQ-039\n\n> Status: locked\n> Version: v2.0.0\n\nMain moved after the gate.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixtures.CommitCurrentFixture(t, root)
	if _, err := applyS6Transition(root, stale); err == nil || !strings.Contains(err.Error(), "file source moved") {
		t.Fatalf("stale pinned TR-003 View accepted: %v", err)
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	unchanged, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Revision != 18 {
		t.Fatalf("stale-view rejection changed Runtime revision to %d", unchanged.Revision)
	}
	if starts, err := productbaseline.ReadS6MainStarts(unchanged.State); err != nil || len(starts) != 0 {
		t.Fatalf("stale-view rejection left baseline starts=%+v err=%v", starts, err)
	}
}

func applyS6Transition(root string, view *fileview.View) (runtime.Snapshot, error) {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	return transition.Apply(root, statePath, journalPath, transition.Request{
		Files: view, TransitionID: "TR-003", ExpectedRevision: -1,
		ExpectedRuntimeID: "loop-req039-ct", Actor: "orchestrator",
		Evidence:         map[string]string{"document_review_record": "ev-dv-spec"},
		GateEvidenceRefs: []string{"ev-dv-spec", "ev-dv-task"},
		GateID:           "GATE-DOCUMENT-PASS", GateFingerprint: "sha256:s6-main-start-test",
		ProducerResponsibility: "document_verifier",
	})
}

func callRegisterExecutionBatch(root string, state map[string]any, view *fileview.View) (transition.ActionResult, error) {
	fn, ok := transition.LookupAction("register_execution_batch")
	if !ok {
		return transition.ActionResult{}, os.ErrNotExist
	}
	baseline := state["baseline"].(map[string]any)
	journal := state["journal"].(map[string]any)
	revision := testInt(state["revision"])
	sequence := testInt(journal["last_sequence"]) + 1
	return fn(state, &transition.ActionContext{
		Root: root, Spec: transition.TransitionSpec{ID: "TR-003"},
		Evidence: map[string]string{"document_review_record": "ev-dv-spec"},
		Request:  &transition.Request{Files: view}, EventID: "evt-same-generation-relock",
		BeforeRevision: revision, AfterRevision: revision + 1, TransitionSequence: sequence,
		Params: map[string]any{"baseline_generation": baseline["generation"]},
	})
}

func testInt(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func mustBaselineStartJSON(t *testing.T, state map[string]any) []byte {
	t.Helper()
	starts, err := productbaseline.ReadS6MainStarts(state)
	if err != nil || len(starts) != 1 {
		t.Fatalf("read single captured start: starts=%d err=%v", len(starts), err)
	}
	data, err := json.Marshal(starts[0])
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cloneState(t *testing.T, state map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
