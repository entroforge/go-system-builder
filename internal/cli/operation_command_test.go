package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// An optional already-built binary exercises the exact same public CLI flow.
// Ordinary CI keeps the in-process composition test and requires no installs.
func operationCLI(t *testing.T, args ...string) (int, []byte, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	if binary := os.Getenv("FRAMEWORK_REPAIR_BINARY"); binary != "" {
		command := exec.Command(binary, args...)
		command.Stdout = &out
		command.Stderr = &diagnostic
		err := command.Run()
		code := 0
		if err != nil {
			if exited, ok := err.(*exec.ExitError); ok {
				code = exited.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return code, out.Bytes(), diagnostic.String()
	}
	code := cli.Run(args, strings.NewReader(""), &out, &diagnostic)
	return code, out.Bytes(), diagnostic.String()
}

func TestRuntimeOperationCLICommitQueryReplayAndPending(t *testing.T) {
	root := worktreeProjectRoot(t)
	plan := minimalReviewPlan(t, t.TempDir(), "review-plan-operation-cli")
	args := []string{"runtime", "review-plan", "--root", root, "--file", plan, "--operation-id", "cli-plan-1", "--expected-revision", "1"}
	code, first, diagnostic := operationCLI(t, args...)
	if code != 0 {
		t.Fatalf("commit: %d %s %s", code, first, diagnostic)
	}
	var committed map[string]any
	if err := json.Unmarshal(first, &committed); err != nil {
		t.Fatal(err)
	}
	if committed["operation_receipt"] == nil {
		t.Fatalf("missing durable receipt: %s", first)
	}
	sp, jp := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	stateBefore, _ := os.ReadFile(sp)
	journalBefore, _ := os.ReadFile(jp)
	code, replayed, diagnostic := operationCLI(t, args...)
	if code != 0 || !bytes.Equal(first, replayed) {
		t.Fatalf("response-loss retry: %d %s %s", code, replayed, diagnostic)
	}
	code, queried, diagnostic := operationCLI(t, "runtime", "operation", "--root", root, "--id", "cli-plan-1")
	if code != 0 {
		t.Fatalf("query: %d %s %s", code, queried, diagnostic)
	}
	var inspected map[string]any
	if err := json.Unmarshal(queried, &inspected); err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(committed["operation_receipt"])
	two, _ := json.Marshal(inspected["receipt"])
	if inspected["status"] != "committed" || !bytes.Equal(one, two) {
		t.Fatalf("query returned another receipt: %s", queried)
	}
	stateAfter, _ := os.ReadFile(sp)
	journalAfter, _ := os.ReadFile(jp)
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("query/retry consumed twice")
	}
	var changed map[string]any
	data, _ := os.ReadFile(plan)
	if err := json.Unmarshal(data, &changed); err != nil {
		t.Fatal(err)
	}
	changed["claims"].([]any)[0].(map[string]any)["oracle"] = "changed business assertion"
	data, _ = json.Marshal(changed)
	if err := os.WriteFile(plan, data, 0644); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostic = operationCLI(t, args...)
	if code == 0 || !strings.Contains(diagnostic, "operation ID") {
		t.Fatalf("same ID/different input: %d %s", code, diagnostic)
	}
	marker := []byte(`{"schema_version":"2.0.0"}`)
	if err := os.WriteFile(sp+".commit-pending.json", marker, 0644); err != nil {
		t.Fatal(err)
	}
	code, queried, diagnostic = operationCLI(t, "runtime", "operation", "--root", root, "--id", "cli-plan-1")
	if code != 0 || !bytes.Contains(queried, []byte("recovery_required")) {
		t.Fatalf("pending query: %d %s %s", code, queried, diagnostic)
	}
	if legacy := os.Getenv("FRAMEWORK_LEGACY_BINARY"); legacy != "" {
		command := exec.Command(legacy, "runtime", "reconcile", "--root", root)
		output, err := command.CombinedOutput()
		if err == nil || !bytes.Contains(output, []byte("unsupported pending runtime commit schema")) {
			t.Fatalf("legacy writer ignored new publication protocol: %v %s", err, output)
		}
	}
	stateAfter, _ = os.ReadFile(sp)
	journalAfter, _ = os.ReadFile(jp)
	markerAfter, _ := os.ReadFile(sp + ".commit-pending.json")
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) || !bytes.Equal(marker, markerAfter) {
		t.Fatal("pending inspection/legacy writer changed durable inputs")
	}
}
