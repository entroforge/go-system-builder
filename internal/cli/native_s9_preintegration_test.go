package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// S9 no longer proves the accepted source again. The Blueprint removes the
// machine proof of the delivery ("baseline/SHA", "typed/flat" comparison and
// Candidate/Final re-verification) from the reception and integration
// precondition chain: the framework does not compare stored hashes, Git OIDs
// or per-file bytes to re-prove that the accepted source is unchanged, and it
// does not re-compare the registered execution definition either. Main decides
// on the actual professional content.
//
// Therefore a changed delivery source is no longer a rejection reason: Main
// integrates the actual candidate work, and the removed proof messages must
// not come back.
func TestNativeS9IntegratesChangedDeliverySourcesWithoutHashMachineProof(t *testing.T) {
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
			if execution.DeliveryRef == "" {
				t.Fatalf("registered delivery path unavailable: %#v", execution)
			}
			registered := snapshot.State["workspace"].(map[string]any)["executions"].(map[string]any)[execution.AssignmentID].(map[string]any)
			// The registered delivery keeps its path only. No digest is
			// registered next to it as a delivery qualification.
			for key := range registered {
				lower := strings.ToLower(key)
				if strings.Contains(lower, "sha") || strings.Contains(lower, "digest") || strings.Contains(lower, "hash") || strings.Contains(lower, "fingerprint") {
					t.Errorf("registered execution re-registered a delivery digest %q: %#v", key, registered)
				}
			}
			switch changed {
			case "candidate bytes":
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
			case "registered definition":
				// The registered execution definition is no longer compared
				// against the accepted candidate either.
				registered["write_paths"] = []any{"internal/unrelated/**"}
				data, err := json.Marshal(snapshot.State)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".claude/loop-state.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := repairDispatchRuntimeBytes(t, root)
			head, err := workspace.Git(context.Background(), root, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			productPath := filepath.Join(root, "internal/api/one.go")
			out.Reset()
			diagnostic.Reset()
			code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID, "--check-command", "go test ./internal/api && git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic)
			// The removed machine proof must not decide the changed source.
			for _, forbidden := range []string{"candidate bytes differ", "registered delivery SHA256", "original registered execution differs"} {
				if strings.Contains(diagnostic.String(), forbidden) {
					t.Errorf("changed source was rejected by the removed %q machine proof: code=%d stderr=%s", forbidden, code, &diagnostic)
				}
			}
			// Main integrates the actual professional content instead.
			if code != 1 || !strings.Contains(diagnostic.String(), "integration verified") || !strings.Contains(diagnostic.String(), "cleanup pending") {
				t.Errorf("Main integration did not complete for the changed source: code=%d stderr=%s", code, &diagnostic)
			}
			afterHead, err := workspace.Git(context.Background(), root, "rev-parse", "HEAD")
			if err != nil || afterHead == head {
				t.Errorf("Main HEAD did not advance to the integrated candidate: %s -> %s (%v)", head, afterHead, err)
			}
			product, err := os.ReadFile(productPath)
			if err != nil || !strings.Contains(string(product), "const Restored = true") {
				t.Errorf("integrated Main product missing the candidate change: %v %s", err, product)
			}
			// The integration is recorded by the Runtime; it is no longer a
			// read-only replay.
			integrated := repairDispatchSnapshot(t, root)
			if integrated.Revision <= snapshot.Revision {
				t.Errorf("Main integration did not record the verified result: %d -> %d", snapshot.Revision, integrated.Revision)
			}
			if bytes.Equal(before[0], repairDispatchRuntimeBytes(t, root)[0]) {
				t.Error("Main integration did not write the verified result into the Runtime state")
			}
			results, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), root), integrated.State)
			if err != nil || len(results) != 1 {
				t.Fatalf("integrated result responsibility missing: %v", err)
			}
			if _, found, err := runtime.VerifiedRepairResultReceipt(root, integrated.State, results[0].ID, packet.Binding.OwnerAgentID); err != nil || !found {
				t.Errorf("verified Final receipt missing after integration: found=%t err=%v", found, err)
			}
			// Cleanup stays independent of the integration: unknown cleanup
			// authorization preserves the Worker worktree.
			if _, err := os.Stat(worker); err != nil {
				t.Errorf("Worker worktree was removed although cleanup authorization is unknown: %v", err)
			}
		})
	}
}
