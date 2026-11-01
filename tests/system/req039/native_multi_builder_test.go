package req039_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/integration"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Both upstream tasks must actually deliver and pass Main's checks before the
// reviewed dependent TASK becomes available. Each result arrives through the
// same native preparation and Stop consumer used by the platform.
func TestNativeS6MultipleBuildersReleaseSuccessor(t *testing.T) {
	root := nativeMultiBuilderRoot(t)
	assertReady := func(want bool) {
		t.Helper()
		err := dispatch.CheckRegistration(root, readSystemState(t, root), "TASK-042-04")
		if (err == nil) != want {
			t.Fatalf("successor ready=%t want=%t: %v", err == nil, want, err)
		}
	}
	assertReady(false)
	deliverNativeMultiBuilder(t, root, "TASK-042-01", "packages/validation", func() { assertReady(false) })
	assertReady(false)
	deliverNativeMultiBuilder(t, root, "TASK-042-02", "web/pages", func() { assertReady(false) })
	assertReady(true)
	runRecheckCLI(t, root, "s6", "prepare", "--task", "TASK-042-04", "--role", "backend-builder")
}

func nativeMultiBuilderRoot(t *testing.T) string {
	t.Helper()
	root := s4AuditRegistrationRoot(t)
	repo := repoRoot(t)
	copyFile := func(src, dest string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repo, src))
		if err != nil {
			t.Fatal(err)
		}
		writeAuditFile(t, root, dest, string(raw))
	}
	copyFile("docs/control/agent-protocol.md", "docs/control/agent-protocol.md")
	copyFile("agents/backend-builder.md", ".claude/agents/backend-builder.md")
	err := filepath.WalkDir(filepath.Join(repo, "docs/rules"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		copyFile(rel, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range catalog.Skills {
		copyFile(filepath.Join("skills", skill.Name, "SKILL.md"), filepath.Join(".claude/skills", skill.Name, "SKILL.md"))
	}
	writeAuditFile(t, root, "docs/dev/contracts/BE-042.md", "# BE-042\n> Status: locked\n> Version: v1.0.0\n\n| 本合同条款 | Promise |\n|---|---|\n| §1 | deliver the reviewed output |\n")
	writeAuditFile(t, root, "go.mod", "module system.multibuilder.fixture\n\ngo 1.26\n")
	for _, item := range []struct{ task, dir string }{{"TASK-042-01", "packages/validation"}, {"TASK-042-02", "web/pages"}} {
		path := "docs/dev/tasks/" + item.task + ".md"
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		writeAuditFile(t, root, path, strings.Replace(string(raw), "assert owned tests == pass", "assert go test ./"+item.dir+" == pass", 1))
		writeAuditFile(t, root, item.dir+"/delivery_test.go", "package delivery\nimport (\"os\"; \"testing\")\nfunc TestDelivery(t *testing.T) { b, e := os.ReadFile(\"result.txt\"); if e != nil || string(b) != \"delivered\\n\" { t.Fatalf(\"delivery: %q %v\", b, e) } }\n")
	}
	state := readSystemState(t, root)
	state["schema_version"] = "1.2.0"
	docs := state["documents"].([]any)
	state["documents"] = append(docs, map[string]any{"id": "BE-042", "kind": "contract", "path": "docs/dev/contracts/BE-042.md", "version": "v1.0.0", "status": "locked", "generation": 1})
	writeSystemState(t, root, state)
	return root
}

func deliverNativeMultiBuilder(t *testing.T, root, task, dir string, beforeIntegration func()) {
	t.Helper()
	runRecheckCLI(t, root, "s6", "prepare", "--task", task, "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(req039fixtures.ReadState(t, root), "native-"+task)
	if err != nil || !found {
		t.Fatalf("prepare %s: found=%t err=%v", task, found, err)
	}
	worker := filepath.Join(t.TempDir(), "worker")
	runGitIn(t, root, "worktree", "add", "-b", "native/"+task, worker, "HEAD")
	brief, plan, result := assignmentrecord.NativePaths(record)
	code, out, errout := systemNativeEvent(t, root, worker, "SubagentStart", filepath.Join(root, brief))
	if code != 0 {
		t.Fatalf("Start %s: %d %s %s", task, code, out, errout)
	}
	assertion, oracle := "Deliver the reviewed output.", "Run the TASK check."
	writeJSON := func(path string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		writeAuditFile(t, worker, path, string(raw))
	}
	output := dir + "/result.txt"
	writeJSON(plan, map[string]any{"objective": "Deliver the reviewed task.", "planned_paths": []string{output}, "assertion_checks": []any{map[string]any{"assertion": assertion, "oracle": oracle, "done_when_indexes": []int{0}}}, "risks_blockers": []string{}})
	writeAuditFile(t, worker, output, "delivered\n")
	runGitIn(t, worker, "add", output)
	runGitIn(t, worker, "commit", "-m", "deliver "+task)
	cmd := exec.Command("go", "test", "./"+dir)
	cmd.Dir = worker
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Worker check %s: %v %s", task, err, output)
	}
	writeJSON(result, map[string]any{"summary": "Implemented and checked the output.", "assertion_evidence": []any{map[string]any{"assertion": assertion, "oracle": oracle, "evidence_refs": []string{output}}}, "risks_blockers": []string{}, "scope_deviations": []string{}, "closing_assessment": map[string]any{"disposition": "complete", "conditions": []any{map[string]any{"done_when_index": 0, "judgment": "satisfied", "plan_assertion_indexes": []int{0}, "rationale": "TASK check passes."}}}})
	code, out, errout = systemNativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result))
	if code != 0 {
		t.Fatalf("Stop %s: %d %s %s", task, code, out, errout)
	}
	ref := completionTaskRef(t, req039fixtures.ReadState(t, root), task)
	if ref == "" {
		t.Fatalf("%s result not received", task)
	}
	beforeIntegration()
	code, out, errout = runTaskIntegrate(t, root, record.AssignmentID)
	if code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("Main integrate %s: %d %s %s", task, code, out, errout)
	}
	data, err := os.ReadFile(assignmentrecord.IntegrationCheckpointPath(root, record.Reference()))
	if err != nil {
		t.Fatal(err)
	}
	var cp integration.Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.VerifiedAt == "" || len(cp.CheckReceipts) == 0 || cp.CompletionReportPath != ref {
		t.Fatalf("Main checks/ACK %s: %+v", task, cp)
	}
	if cp.State != integration.StateComplete && cp.State != integration.StateCleanupPending {
		t.Fatalf("Main ACK %s: %+v", task, cp)
	}
	got, err := os.ReadFile(filepath.Join(root, output))
	if err != nil || string(got) != "delivered\n" {
		t.Fatal(fmt.Errorf("Main output %s: %q %w", task, got, err))
	}
}
