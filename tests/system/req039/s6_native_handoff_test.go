package req039_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/transition"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// The shared positive fixture uses the original S6 preparation and Stop
// consumer. It does not manufacture a completed Agent or a receipt.
func systemNativeBuilder(t *testing.T) (root, worker string, record assignmentrecord.Record) {
	t.Helper()
	root = req039fixtures.FreshRoot(t)
	copyTree := func(source, target string) {
		t.Helper()
		if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			dest := filepath.Join(target, rel)
			if entry.IsDir() {
				return os.MkdirAll(dest, 0755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0644)
		}); err != nil {
			t.Fatal(err)
		}
	}
	repo := repoRoot(t)
	copyTree(filepath.Join(repo, "docs/control/agent-protocol.md"), filepath.Join(root, "docs/control/agent-protocol.md"))
	copyTree(filepath.Join(repo, "docs/rules"), filepath.Join(root, "docs/rules"))
	copyTree(filepath.Join(repo, "agents/backend-builder.md"), filepath.Join(root, ".claude/agents/backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyTree(filepath.Join(repo, "skills", skill.Name, "SKILL.md"), filepath.Join(root, ".claude/skills", skill.Name, "SKILL.md"))
	}
	state := systemPlanningState(t, root, "tasks", 8)
	state["schema_version"] = "1.2.0"
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	sources := map[string]string{
		"docs/architecture/ARCH-039.md":   "# ARCH-039\n> Status: locked\n> Version: v1.0.0\n",
		"docs/dev/contracts/BE-039.md":    "# BE-039\n> Status: locked\n> Version: v1.0.0\n\n| 本合同条款 | Promise |\n|---|---|\n| §1 | preserve the reviewed delivery |\n",
		"docs/dev/tasks/TASK-039-01.md":   "# TASK-039-01\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Dispatch policy: waves-v1\n> Primary contract: BE-039\n\n## Scope\n\n| Type | Paths |\n|---|---|\n| prospective write paths | internal |\n\n## Delivered Clauses\n\n| Contract | Clauses |\n|---|---|\n| BE-039 | §1 |\n\n## Dependencies\n\n| Dependency | Purpose |\n|---|---|\n| — | — |\n\n## 7. Closing Contract\n\nassert go test ./internal == pass\n",
		"docs/dev/tasks/index-REQ-039.md": "# REQ-039 dispatch plan\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n\n- [ ] [reviewed delivery](TASK-039-01.md)\n",
		"go.mod":                          "module system.handoff.fixture\n\ngo 1.26\n",
		"internal/input.go":               "package internal\n",
		"internal/proof_test.go":          "package internal\nimport (\"os\"; \"testing\")\nfunc TestReviewedDelivery(t *testing.T) { got, err := os.ReadFile(\"proof.txt\"); if err != nil || string(got) != \"actual reviewed delivery\\n\" { t.Fatalf(\"delivery: %q %v\", got, err) } }\n",
	}
	for path, data := range sources {
		writeAuditFile(t, root, path, data)
	}
	documents := []any{}
	for _, doc := range []struct{ id, kind, path, version string }{
		{"REQ-039", "req", "docs/requirements/REQ-039.md", "v1.0.0"},
		{"ARCH-039", "design", "docs/architecture/ARCH-039.md", "v1.0.0"},
		{"BE-039", "contract", "docs/dev/contracts/BE-039.md", "v1.0.0"},
		{"TASK-039-01", "task", "docs/dev/tasks/TASK-039-01.md", "v1.0.0"},
		{"dispatch-plan:REQ-039", "dispatch_plan", "docs/dev/tasks/index-REQ-039.md", "1"},
	} {
		documents = append(documents, map[string]any{"id": doc.id, "kind": doc.kind, "path": doc.path, "version": doc.version, "status": "locked", "generation": 1})
	}
	state["documents"] = documents
	// Establish the actual reviewed S6 task batch through the original Writer,
	// so the final integration can run its existing TR-006/S7 successor.
	req039fixtures.SeedDocumentPassS5(t, root, state, "system-spec-reviewer", "system-task-reviewer")
	writeSystemState(t, root, state)
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/test-development", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transition.Apply(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), transition.Request{Files: view, TransitionID: "TR-003", ExpectedRevision: -1, ExpectedRuntimeID: state["runtime_id"].(string), Actor: "orchestrator", Evidence: map[string]string{"document_review_record": "ev-dv-spec"}, GateEvidenceRefs: []string{"ev-dv-spec", "ev-dv-task"}, GateID: "GATE-DOCUMENT-PASS", ProducerResponsibility: "document_verifier"})
	if err != nil {
		t.Fatalf("original reviewed batch entry: %v", err)
	}
	runRecheckCLI(t, root, "s6", "prepare", "--task", "TASK-039-01", "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(req039fixtures.ReadState(t, root), "native-TASK-039-01")
	if err != nil || !found || record.Contract != assignmentrecord.NativeDeliveryContract {
		t.Fatalf("original S6 registration: %+v found=%t err=%v", record, found, err)
	}
	worker = filepath.Join(t.TempDir(), "native-builder")
	runGitIn(t, root, "worktree", "add", "-b", "native/system-handoff", worker, "HEAD")
	return root, worker, record
}

func systemNativeEvent(t *testing.T, root, worker, event, text string) (int, string, string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"hook_event_name": event, "session_id": "system-native-handoff", "agent_id": "opaque-system-builder", "agent_type": "backend-builder", "cwd": worker, "last_assistant_message": text, "stop_hook_active": false})
	if err != nil {
		t.Fatal(err)
	}
	return runHook(t, root, event, string(raw))
}

// Start points to the original prepared brief; the brief owns the exact
// professional paths and the author's worktree-relative save instructions.
func assertSystemNativeStartBrief(t *testing.T, root, output string, record assignmentrecord.Record) {
	t.Helper()
	var envelope struct {
		Output struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("Start envelope: %v: %s", err, output)
	}
	brief, plan, result := assignmentrecord.NativePaths(record)
	if envelope.Output.Event != "SubagentStart" {
		t.Fatalf("Start event = %q", envelope.Output.Event)
	}
	for _, want := range []string{record.AssignmentID, filepath.Join(root, brief)} {
		if !strings.Contains(envelope.Output.Context, want) {
			t.Fatalf("Start omitted original responsibility %q: %s", want, output)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, brief))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Plan: " + plan, "Result: " + result, "relative to your actual worktree", "Plan before implementation"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("prepared brief omitted author instruction %q: %s", want, data)
		}
	}
}

func receiveSystemNativeBuilder(t *testing.T, root, worker string, record assignmentrecord.Record) string {
	t.Helper()
	brief, plan, result := assignmentrecord.NativePaths(record)
	code, out, errout := systemNativeEvent(t, root, worker, "SubagentStart", filepath.Join(root, brief))
	if code != 0 {
		t.Fatalf("original Start: %d %s %s", code, out, errout)
	}
	assertSystemNativeStartBrief(t, root, out, record)

	assertion, oracle := "The reviewed TASK delivery is implemented.", "Inspect the committed proof and run the TASK check."
	writeJSON := func(path string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		writeAuditFile(t, worker, path, string(raw))
	}
	writeJSON(plan, map[string]any{"objective": "Implement the reviewed TASK and check its committed output.", "planned_paths": []string{"internal/proof.txt"}, "assertion_checks": []any{map[string]any{"assertion": assertion, "oracle": oracle, "done_when_indexes": []int{0}}}, "risks_blockers": []string{}})
	writeAuditFile(t, worker, "internal/proof.txt", "actual reviewed delivery\n")
	runGitIn(t, worker, "add", "internal/proof.txt")
	runGitIn(t, worker, "commit", "-m", "deliver reviewed native work")
	// Execute the actual TASK self-check, independently of Main's rerun.
	check := exec.Command("go", "test", "./internal")
	check.Dir = worker
	check.Env = append(os.Environ(), "GOWORK=off")
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("Worker check: %v %s", err, output)
	}

	writeJSON(result, map[string]any{"summary": "Implemented and checked the committed nonempty proof.", "assertion_evidence": []any{map[string]any{"assertion": assertion, "oracle": oracle, "evidence_refs": []string{"internal/proof.txt"}}}, "risks_blockers": []string{}, "scope_deviations": []string{}, "closing_assessment": map[string]any{"disposition": "complete", "conditions": []any{map[string]any{"done_when_index": 0, "judgment": "satisfied", "plan_assertion_indexes": []int{0}, "rationale": "The committed nonempty proof satisfies the TASK condition."}}}})
	mainHead := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	code, out, errout = systemNativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result))
	if code != 0 {
		t.Fatalf("original Stop reception: %d %s %s", code, out, errout)
	}
	if head := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")); head != mainHead {
		t.Fatal("short Worker Stop performed Main merge")
	}
	ref := completionTaskRef(t, req039fixtures.ReadState(t, root), record.Task.ID)
	if ref == "" {
		t.Fatal("Stop returned without durable original Result")
	}
	// Accepted Stop replay preserves the original immutable receipt.
	beforeRef, err := os.ReadFile(filepath.Join(root, ref))
	if err != nil {
		t.Fatal(err)
	}
	code, out, errout = systemNativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result))
	if code != 0 {
		t.Fatalf("accepted Stop replay: %d %s %s", code, out, errout)
	}
	afterRef, err := os.ReadFile(filepath.Join(root, ref))
	if err != nil || !bytes.Equal(beforeRef, afterRef) {
		t.Fatalf("accepted replay changed receipt: %v", err)
	}
	return ref
}
