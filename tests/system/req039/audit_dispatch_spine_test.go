package req039_test

// This audit drives the waves-v1 dispatch-plan registration through the real
// PreToolUse Hook. The fixture deliberately commits the plan before S4, then
// dirties that file only after S5 evidence is written: production evaluation
// must continue to read the pinned development Git tree, while the runtime
// documents and exact review subjects retain the committed bytes.

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

type dispatchSpineFixture struct {
	planPath  string
	planBytes []byte
	taskPaths []string
	taskBytes map[string][]byte
}

func seedDispatchSpineFixture(t *testing.T, root string, state map[string]any) dispatchSpineFixture {
	t.Helper()
	req039fixtures.WritePlanningContractPass(t, root, state)
	source := filepath.Join(repoRoot(t), "docs", "examples", "dispatch-plan", "project", "docs", "dev", "tasks")
	fixture := dispatchSpineFixture{taskBytes: map[string][]byte{}}
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := entry.Name()
		name = strings.ReplaceAll(name, "REQ-042", "REQ-039")
		name = strings.ReplaceAll(name, "TASK-042", "TASK-039")
		rel := filepath.ToSlash(filepath.Join("docs/dev/tasks", name))
		text := string(data)
		for _, replacement := range []struct{ old, new string }{
			{"REQ-042", "REQ-039"},
			{"TASK-042", "TASK-039"},
			{"BE-042", "BE-039"},
			{"CONTRACTS-042", "CONTRACTS-039"},
		} {
			text = strings.ReplaceAll(text, replacement.old, replacement.new)
		}
		data = []byte(text)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			return err
		}
		if strings.HasPrefix(filepath.Base(rel), "index-") {
			fixture.planPath = rel
			fixture.planBytes = append([]byte(nil), data...)
		} else if strings.HasPrefix(filepath.Base(rel), "TASK-") {
			fixture.taskPaths = append(fixture.taskPaths, rel)
			fixture.taskBytes[rel] = append([]byte(nil), data...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(fixture.taskPaths)
	if fixture.planPath == "" || len(fixture.taskPaths) != 6 {
		t.Fatalf("dispatch fixture incomplete: plan=%q tasks=%v", fixture.planPath, fixture.taskPaths)
	}
	return fixture
}

func writeDispatchPlanningTaskEvidence(t *testing.T, root string, state map[string]any, fixture dispatchSpineFixture) {
	t.Helper()
	subjects := make([]any, 0, len(fixture.taskPaths))
	for _, path := range fixture.taskPaths {
		data := fixture.taskBytes[path]
		subjects = append(subjects, map[string]any{
			"path": path, "version": "v1.0.0", "sha256": req039fixtures.Sha256Hex(data),
		})
	}
	scope := make([]any, 0, len(fixture.taskPaths))
	for _, path := range fixture.taskPaths {
		scope = append(scope, path)
	}
	envelope := req039fixtures.EvidenceEnvelope(state, "ev-tasks", "planning_task", "dispatch-planner-1", "Task Planner", "pass", map[string]any{
		"review_round": 1,
		"subject_refs": subjects,
	})
	entry := req039fixtures.WriteEvidenceEnvelope(t, root, state, "ev-tasks", "planning_task", "dispatch-planner-1", "Task Planner", envelope, scope)
	req039fixtures.AppendEvidence(state, entry)
}

func dispatchDocumentSubjects(state map[string]any) map[string]bool {
	want := map[string]bool{}
	documents, _ := state["documents"].([]any)
	for _, raw := range documents {
		doc, _ := raw.(map[string]any)
		if doc == nil {
			continue
		}
		path, _ := doc["path"].(string)
		version, _ := doc["version"].(string)
		sha, _ := doc["sha256"].(string)
		if path != "" && version != "" && sha != "" {
			want[path+"|"+version+"|"+sha] = true
		}
	}
	return want
}

func assertDispatchReviewSubjectsExact(t *testing.T, root string, state map[string]any) {
	t.Helper()
	want := dispatchDocumentSubjects(state)
	for _, name := range []string{"ev-dv-spec.json", "ev-dv-task.json"} {
		data, err := os.ReadFile(filepath.Join(root, "evidence", name))
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			SubjectRefs []struct {
				Path    string `json:"path"`
				Version string `json:"version"`
				SHA256  string `json:"sha256"`
			} `json:"subject_refs"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, subject := range envelope.SubjectRefs {
			got[subject.Path+"|"+subject.Version+"|"+subject.SHA256] = true
		}
		if len(got) != len(want) {
			t.Fatalf("%s subject count=%d, want exact runtime document count=%d; got=%v want=%v", name, len(got), len(want), got, want)
		}
		for key := range want {
			if !got[key] {
				t.Fatalf("%s omitted exact subject %s; got=%v", name, key, got)
			}
		}
	}
}

func assertDispatchDocuments(t *testing.T, state map[string]any, fixture dispatchSpineFixture, wantStatus string) {
	t.Helper()
	var planDocs, taskDocs []map[string]any
	documents, _ := state["documents"].([]any)
	for _, raw := range documents {
		doc, _ := raw.(map[string]any)
		if doc == nil {
			continue
		}
		switch doc["kind"] {
		case "dispatch_plan":
			planDocs = append(planDocs, doc)
		case "task":
			taskDocs = append(taskDocs, doc)
		}
	}
	if len(planDocs) != 1 {
		t.Fatalf("dispatch plan registration count=%d, want 1: %#v", len(planDocs), planDocs)
	}
	plan := planDocs[0]
	if plan["path"] != fixture.planPath || plan["status"] != wantStatus || plan["sha256"] != req039fixtures.Sha256Hex(fixture.planBytes) {
		t.Fatalf("dispatch plan registration=%#v, want path/status/hash %s/%s/%s", plan, fixture.planPath, wantStatus, req039fixtures.Sha256Hex(fixture.planBytes))
	}
	if len(taskDocs) != len(fixture.taskPaths) {
		t.Fatalf("TASK denominator=%d, want %d; plan must remain a separate document: %#v", len(taskDocs), len(fixture.taskPaths), taskDocs)
	}
	wantTasks := map[string]bool{}
	for _, path := range fixture.taskPaths {
		wantTasks[strings.TrimSuffix(filepath.Base(path), ".md")] = true
	}
	for _, task := range taskDocs {
		id, _ := task["id"].(string)
		if !wantTasks[id] || task["status"] != wantStatus {
			t.Fatalf("unexpected dispatch TASK registration: %#v", task)
		}
		path, _ := task["path"].(string)
		if task["sha256"] != req039fixtures.Sha256Hex(fixture.taskBytes[path]) {
			t.Fatalf("TASK %s hash does not match committed bytes: %#v", id, task)
		}
	}
}

func requireDispatchHookTransition(t *testing.T, runner *req039fixtures.CLIRunner, root, session, tool string, input map[string]any, wantTransition, wantState, wantPhase string) map[string]any {
	t.Helper()
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", req039fixtures.PreToolUseBody(session, tool, input))
	if code != 0 && code != 2 {
		t.Fatalf("Hook %s failed: code=%d stdout=%s stderr=%s", session, code, stdout, stderr)
	}
	qg := req039fixtures.ParseHookQualityGate(t, stdout)
	state := req039fixtures.ReadState(t, root)
	lifecycle, phase := req039fixtures.Lifecycle(state)
	if lifecycle != wantState || phase != wantPhase || req039fixtures.LastTransitionID(state) != wantTransition {
		t.Fatalf("Hook %s did not commit %s: lifecycle=%s.%s last=%s qg=%v stdout=%s stderr=%s", session, wantTransition, lifecycle, phase, req039fixtures.LastTransitionID(state), qg, stdout, stderr)
	}
	return state
}

// TestAuditDispatchPlanHookSpineCommittedSubjectsAndFreeze drives the real
// S3→S4→S5→S6 planning path. It proves that the dispatch plan is registered
// as its own document, exact S5 subjects include it, the plan does not enter
// the TASK denominator, and the same committed bytes remain authoritative
// when the working copy is dirtied before TR-003.
func TestAuditDispatchPlanHookSpineCommittedSubjectsAndFreeze(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	state := systemPlanningState(t, root, "contracts", 1)
	fixture := seedDispatchSpineFixture(t, root, state)
	writeDispatchPlanningTaskEvidence(t, root, state, fixture)
	writeSystemState(t, root, state)
	baselineHead := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))

	state = requireDispatchHookTransition(t, runner, root, "dispatch-spine-s3", "Edit",
		map[string]any{"file_path": "docs/dev/contracts/BE-039.md"}, "PTR-PLAN-02", "planning", "tasks")
	for _, raw := range state["documents"].([]any) {
		doc, _ := raw.(map[string]any)
		if doc != nil && doc["kind"] == "dispatch_plan" {
			t.Fatal("PTR-PLAN-02 must not register the S4 dispatch plan before TR-002")
		}
	}

	state = requireDispatchHookTransition(t, runner, root, "dispatch-spine-s4", "Edit",
		map[string]any{"file_path": fixture.taskPaths[0]}, "TR-002", "document_verification", "")
	assertDispatchDocuments(t, state, fixture, "complete")
	if got := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); got != baselineHead {
		t.Fatalf("TR-002 Hook changed the committed planning tree: before=%s after=%s", baselineHead, got)
	}

	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteDocumentVerificationPassEvidence(t, root, state, "dispatch-dv-spec", "dispatch-dv-task")
	writeSystemState(t, root, state)
	state = req039fixtures.ReadState(t, root)
	assertDispatchReviewSubjectsExact(t, root, state)

	// This dirty edit must remain outside the formal Git input. Evidence and
	// state were committed first so the modified plan is still uncommitted
	// when TR-003 evaluates the pinned development ref.
	dirty := append(append([]byte(nil), fixture.planBytes...), []byte("\n# uncommitted operator note\n")...)
	if err := os.WriteFile(filepath.Join(root, fixture.planPath), dirty, 0o644); err != nil {
		t.Fatal(err)
	}
	status := runGitIn(t, root, "status", "--porcelain", "--", fixture.planPath)
	if !strings.Contains(status, "M") {
		t.Fatalf("dispatch plan was not left dirty for the Git-source probe: %q", status)
	}
	headBeforeS5 := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))

	state = requireDispatchHookTransition(t, runner, root, "dispatch-spine-s5", "Edit",
		map[string]any{"file_path": "docs/dev/contracts/BE-039.md"}, "TR-003", "building", "")
	assertDispatchDocuments(t, state, fixture, "locked")
	if got := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); got != headBeforeS5 {
		t.Fatalf("TR-003 Hook changed HEAD while consuming dirty plan: before=%s after=%s", headBeforeS5, got)
	}
	if status := runGitIn(t, root, "status", "--porcelain", "--", fixture.planPath); !strings.Contains(status, "M") {
		t.Fatalf("TR-003 unexpectedly cleaned or committed dirty dispatch plan: %q", status)
	}

	for _, path := range append([]string{fixture.planPath}, fixture.taskPaths[0]) {
		code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse",
			req039fixtures.PreToolUseBody("dispatch-freeze-"+filepath.Base(path), "Edit", map[string]any{"file_path": path}))
		// The Hook protocol carries a deny in JSON while the CLI exits zero;
		// direct CLI adapters may use the conventional exit-2 safety result.
		if (code != 0 && code != 2) || !strings.Contains(stdout, `"permissionDecision":"deny"`) || !strings.Contains(strings.ToLower(stdout+stderr), "locked_artifact_write") {
			t.Fatalf("S6 frozen dispatch artifact was not protected: path=%s code=%d stdout=%s stderr=%s", path, code, stdout, stderr)
		}
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("dispatch Hook spine invoked manual transition CLI %d times", runner.ManualTransitionCalls)
	}
}
