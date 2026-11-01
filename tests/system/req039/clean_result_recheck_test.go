package req039_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/integration"
)

// Retrying an already accepted native Result must not repeat the real merge.
// A clean worktree stays removed; retained private data stays untouched.
func TestNativeTaskIntegrationRetryAfterCleanDoesNotRemerge(t *testing.T) {
	root, worker, record := systemNativeBuilder(t)
	ref := receiveSystemNativeBuilder(t, root, worker, record)
	code, out, errout := runTaskIntegrate(t, root, record.AssignmentID)
	if code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("initial Main integration: %d %s %s", code, out, errout)
	}
	_, workerErr := os.Stat(worker)
	workerRemoved := os.IsNotExist(workerErr)
	if workerErr != nil && !workerRemoved {
		t.Fatal(workerErr)
	}
	path := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	readCheckpoint := func() integration.Checkpoint {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cp integration.Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			t.Fatal(err)
		}
		return cp
	}
	first := readCheckpoint()
	if (first.State != integration.StateComplete && first.State != integration.StateCleanupPending) || first.VerifiedAt == "" || len(first.CheckReceipts) == 0 || first.CompletionReportPath != ref {
		t.Fatalf("initial Main checks/ACK: %+v", first)
	}
	before := strings.TrimSpace(runGitIn(t, root, "rev-list", "--count", "HEAD"))
	code, out, errout = runTaskIntegrate(t, root, record.AssignmentID)
	if code != 0 || !strings.Contains(errout, "integrated") {
		t.Fatalf("retry Main integration: %d %s %s", code, out, errout)
	}
	after := readCheckpoint()
	if (after.State != integration.StateComplete && after.State != integration.StateCleanupPending) || after.CompletionReportPath != ref || after.VerifiedAt == "" || len(after.CheckReceipts) == 0 {
		t.Fatalf("retry lost original completion: %+v", after)
	}
	if count := strings.TrimSpace(runGitIn(t, root, "rev-list", "--count", "HEAD")); count != before {
		t.Fatal("retry repeated Main merge")
	}
	if _, err := os.Stat(worker); workerRemoved && !os.IsNotExist(err) {
		t.Fatalf("retry recreated worker: %v", err)
	} else if !workerRemoved && err != nil {
		t.Fatalf("retry removed preserved worker: %v", err)
	}
}
