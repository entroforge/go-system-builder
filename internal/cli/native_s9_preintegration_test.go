package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9RejectsChangedDeliverySourcesBeforeAnyMainIntegration(t *testing.T) {
	for _, changed := range []string{"candidate bytes", "registered definition"} {
		t.Run(changed, func(t *testing.T) {
			root, worker, packet, input, _ := nativeS9WorkerFixture(t)
			input.Event = "SubagentStop"
			var out, diagnostic bytes.Buffer
			if _, code := nativeS9Hook(root, input, &out, &diagnostic); code != 0 {
				t.Fatalf("original native candidate reception failed: %s", &diagnostic)
			}
			snapshot := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snapshot.State)
			if err != nil || registry == nil {
				t.Fatalf("original registry unavailable: %v", err)
			}
			execution := registry.Executions[packet.Binding.ManifestAssignmentID]
			wantDiagnostic := "candidate bytes differ"
			if changed == "candidate bytes" {
				path := filepath.Join(root, execution.DeliveryRef)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				// Valid JSON and the same professional meaning, but different
				// bytes from the already accepted immutable source.
				if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				current := snapshot.State["workspace"].(map[string]any)["executions"].(map[string]any)[execution.AssignmentID].(map[string]any)
				current["write_paths"] = []any{"internal/unrelated/**"}
				data, err := json.Marshal(snapshot.State)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
				wantDiagnostic = "original registered execution differs"
			}
			before := repairDispatchRuntimeBytes(t, root)
			head, err := workspace.Git(context.Background(), root, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			productPath := filepath.Join(root, "internal/api/one.go")
			product, err := os.ReadFile(productPath)
			if err != nil {
				t.Fatal(err)
			}
			out.Reset()
			diagnostic.Reset()
			code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID, "--check-command", "go test ./internal/api && git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic)
			if code == 0 || !strings.Contains(diagnostic.String(), wantDiagnostic) {
				t.Errorf("changed source was accepted or only rejected after integration: code=%d stderr=%s", code, &diagnostic)
			}
			afterHead, err := workspace.Git(context.Background(), root, "rev-parse", "HEAD")
			if err != nil || afterHead != head {
				t.Errorf("Main HEAD changed before rejecting the original source: %s -> %s (%v)", head, afterHead, err)
			}
			afterProduct, err := os.ReadFile(productPath)
			if err != nil || !bytes.Equal(product, afterProduct) {
				t.Error("Main product changed before rejecting the original source")
			}
			repairDispatchAssertUnchanged(t, root, before)
			if _, err := os.Stat(worker); err != nil {
				t.Errorf("unreceived Worker was cleaned despite source rejection: %v", err)
			}
		})
	}
}
