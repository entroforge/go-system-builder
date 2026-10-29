package hookctx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskCanonicalReportFallbackKeepsActualProducerAndTask(t *testing.T) {
	root := t.TempDir()
	ref := "report.json"
	if err := os.WriteFile(filepath.Join(root, ref), []byte(`{"producer_agent_id":"owner-A","task_id":"TASK-042-01"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !ownCompletionReport(root, ref, "owner-A", "TASK-042-01") {
		t.Fatal("own canonical source ignored")
	}
	if ownCompletionReport(root, ref, "owner-B", "TASK-042-01") || ownCompletionReport(root, ref, "owner-A", "TASK-042-02") {
		t.Fatal("another owner/task report was borrowed")
	}
	if ownCompletionReport(root, "missing.json", "owner-A", "TASK-042-01") {
		t.Fatal("unknown report invented a producer")
	}
}
