package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestOriginalPlanHandoffUsesResponsibilityAndActualContent(t *testing.T) {
	root := t.TempDir()
	original := map[string]any{"runtime_id": "loop-plan", "assignment_id": "assignment-" + strings.Repeat("a", 300), "agent_id": "agent-worker", "message_id": "msg-plan", "objective": "original", "business": map[string]any{"sha256": "do not remove"}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	first, err := importPlanReportBytes(root, data)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := importPlanReportBytes(root, data)
	if err != nil || replay != first {
		t.Fatalf("replay=%q first=%q err=%v", replay, first, err)
	}
	original["objective"] = "changed"
	changed, _ := json.Marshal(original)
	second, err := importPlanReportBytes(root, changed)
	if err != nil || second == first {
		t.Fatalf("new content path=%q first=%q err=%v", second, first, err)
	}
	for _, rel := range []string{first, second} {
		for _, component := range strings.Split(rel, "/") {
			if len(component) > 125 {
				t.Fatalf("oversized path component %d", len(component))
			}
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"sha256":"do not remove"`) {
			t.Fatalf("business field changed: %s", data)
		}
	}
}

func TestNativeCheckReceiptReplayUsesOriginalPathAndFacts(t *testing.T) {
	one := assignment.NativeOwnedCheckReceipt{Command: "go test ./...", ToolReceipt: checkexec.ToolReceipt{Path: "/tmp/receipt-one", Command: "go test ./..."}}
	other := one
	other.ToolReceipt.Path = "/tmp/receipt-two"
	if sameNativeCheckReceipt(one, other) {
		t.Fatal("different receipts collapsed")
	}
	other = one
	other.Command = "false"
	if sameNativeCheckReceipt(one, other) {
		t.Fatal("different actual command collapsed")
	}
}
