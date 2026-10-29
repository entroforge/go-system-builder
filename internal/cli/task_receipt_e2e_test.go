package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func reportOwnedCoreCheck(t *testing.T, f stageAnswerFixture, e workspace.Execution, wantPass bool) {
	t.Helper()
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = e.Path
	output, checkErr := cmd.CombinedOutput()
	if (checkErr == nil) != wantPass {
		t.Fatalf("owned check pass=%v want %v: %s", checkErr == nil, wantPass, output)
	}
	checkPath := ".claude/evidence/receipt-owned-check.txt"
	os.MkdirAll(filepath.Dir(filepath.Join(e.Path, checkPath)), 0700)
	if err := os.WriteFile(filepath.Join(e.Path, checkPath), output, 0600); err != nil {
		t.Fatal(err)
	}
	examples, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(examples, &messages); err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if m["message_type"] != "completion_report" {
			continue
		}
		m["runtime_id"], m["agent_id"], m["agent_definition_ref"] = e.RuntimeID, e.AgentID, ".claude/agents/backend-builder.md"
		m["task_id"], m["team_id"] = f.taskID, "workgroup-core-builder"
		m["summary"] = "Recorded the actual owned regression outcome for the scoped candidate"
		m["changed_paths"] = []string{"src/value.go", "src/value_test.go"}
		result := "fail"
		if wantPass {
			result = "pass"
		}
		m["checks"] = []any{map[string]any{"name": "owned value regression", "command": "go test ./...", "result": result, "evidence_ref": checkPath}}
		m["evidence_refs"] = []string{checkPath}
		stageJSON(t, e.Path, ".loop-submissions/completion.json", m)
		stageCLI(t, "runtime", "workspace", "report", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
		return
	}
	t.Fatal("missing original completion-report schema example")
}

func commitCoreReceiptCandidate(t *testing.T, e workspace.Execution, expected int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.Path, "src/value.go"), []byte("package src\nfunc Value() int { return 2 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body := "package src\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=2 { t.Fatal(\"value\") } }\n"
	if expected != 2 {
		body = "package src\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=99 { t.Fatal(\"value\") } }\n"
	}
	if err := os.WriteFile(filepath.Join(e.Path, "src/value_test.go"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	head, err := workspace.Git(context.Background(), e.Path, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	stageJSON(t, e.Path, ".loop-submissions/commit.json", workspace.CommitRequest{ExpectedHead: head, Message: "Record the owned regression candidate", Paths: []string{"src/value.go", "src/value_test.go"}})
	stageCLI(t, "runtime", "workspace", "commit", "--root", e.Path, "--assignment", e.AssignmentID, "--agent", e.AgentID)
}

func TestBuilderReceiptOriginalFailureReworkAndCurrentResubmission(t *testing.T) {
	f := coreBuilderFixture(t)
	e := prepareCoreBuilder(t, f, "backend-core")
	beginCoreBuilder(t, e)
	commitCoreReceiptCandidate(t, e, 99)
	reportOwnedCoreCheck(t, f, e, false)
	failed := assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, false, false)
	if !failed.DeliveryRecorded || !failed.Accepted || len(failed.OwnPending) == 0 {
		t.Fatalf("registered failed checks became complete: %+v", failed)
	}
	var out, errOut bytes.Buffer
	cli.Run([]string{"runtime", "task-integrate", "--root", f.root, "--assignment-id", e.AssignmentID, "--agent-id", e.AgentID}, nil, &out, &errOut)
	cp, found, err := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(f.root, e.RuntimeID, e.BaselineGeneration, e.AssignmentID))
	if err != nil || !found || cp.TestedHead != "" || (cp.State != integration.StatePreserved && cp.State != integration.StateBlocked) {
		t.Fatalf("actual failing integration not preserved: %+v %v %s %s", cp, err, out.String(), errOut.String())
	}
	stageCLI(t, "runtime", "workspace", "rework", "--root", f.root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--reason", "Correct the owned regression oracle and rerun the declared check")
	reopened := assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, false, false)
	if reopened.DeliveryRecorded || reopened.Accepted || reopened.ResponsibilityStatus != "not_submitted" {
		t.Fatalf("old report replayed after original rework: %+v", reopened)
	}
	if _, err := os.Stat(filepath.Join(f.root, failed.DomainRefs[0])); err != nil {
		t.Fatal("original report was not preserved", err)
	}
	commitCoreReceiptCandidate(t, e, 2)
	reportOwnedCoreCheck(t, f, e, true)
	current := assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, true, false)
	if current.DomainRefs[0] == failed.DomainRefs[0] {
		t.Fatal("rework reused old canonical report")
	}
	stageCLI(t, "runtime", "task-integrate", "--root", f.root, "--assignment-id", e.AssignmentID, "--agent-id", e.AgentID)
	assertCoreBuilderReceipt(t, f.root, e.AgentID, e.AssignmentID, true, true)
}
