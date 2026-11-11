package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS3OriginalHookUsesSelectedSourceThroughS4WithDifferentCheckout(t *testing.T) {
	root, packet := newContractAnswerFixture(t, func(root string) {
		path := "docs/dev/contracts/CONTRACTS-039.md"
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, path, string(data)+"| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
		for _, path := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
			data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), path))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, path, string(data))
		}
	})
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	view, ok := service.Files.(*fileview.View)
	if !ok {
		t.Fatal("original source is not the declared file view")
	}
	if kind, err := view.Source("docs/dev/contracts/BE-039.md"); err != nil || kind != "git_tree" {
		t.Fatalf("fixture must exercise the original Git source: %s %v", kind, err)
	}
	unknown := "docs/dev/contracts/FE-CHECKOUT-ONLY.md"
	writeContractSource(t, root, unknown, "# unknown checkout-only candidate\n")
	writeContractSource(t, root, "docs/dev/contracts/BE-039.md", "# checkout-only modified content\n")
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}, "cwd": root})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("original S3 Hook: code=%d output=%s error=%s", code, out.String(), errout.String())
	}
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("original S3 not received and advanced: %+v %v output=%s", record, err, out.String())
	}
	life := snap.State["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "tasks" {
		t.Fatalf("S4 not reached: %+v", life)
	}
	prepared := false
	for _, raw := range snap.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s4-task-planning-v1" {
			prepared = true
		}
	}
	if !prepared {
		t.Fatalf("S4 original packet not prepared: %s", out.String())
	}
	data, err := os.ReadFile(filepath.Join(root, unknown))
	if err != nil || !strings.Contains(string(data), "unknown checkout-only") {
		t.Fatalf("unknown checkout file changed: %q %v", data, err)
	}
}
