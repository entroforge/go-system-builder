package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Once the original Result has been received, later private authoring changes
// do not revoke it. Actual Main integration and checks run, while private files
// are preserved if ordinary cleanup cannot retire them.
func TestS9NativePrivateChangesDoNotRevokeAcceptedFinal(t *testing.T) {
	for _, change := range []struct{ name, content string }{
		{"valid-new-draft", `{"verdict":"blocked","residual_risks":["later work"]}`},
		{"malformed-draft", `{"verdict":`},
		{"duplicate-key", `{"verdict":"completed","verdict":"blocked"}`},
	} {
		t.Run(change.name, func(t *testing.T) {
			root, worker, plan, _, _ := nativeS9WorkerFixture(t)
			var diag bytes.Buffer
			if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, plan, worker, false, &diag, nil); err != nil {
				t.Fatalf("original Result: %v %s", err, &diag)
			}
			snapshot := repairDispatchSnapshot(t, root)
			packets, err := runtime.CurrentRepairResultPackets(root, snapshot.State, plan.Binding.OwnerAgentID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("original packet: %d %v", len(packets), err)
			}
			registry, err := workspace.Decode(snapshot.State)
			if err != nil || registry == nil {
				t.Fatalf("original work location: %v", err)
			}
			execution := registry.Executions[plan.Binding.ManifestAssignmentID]
			_, resultPath := nativeS9TestAuthorPaths(t, root, plan)
			private := filepath.Join(worker, resultPath)
			if err := os.WriteFile(private, []byte(change.content), 0600); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			diag.Reset()
			code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID, "--check-command", "test -f internal/api/one.go"}, &out, &diag)
			if !strings.Contains(diag.String(), "integration verified") {
				t.Fatalf("accepted Result did not reach Main integration: code=%d %s %s", code, &out, &diag)
			}
			final, found, err := runtime.VerifiedRepairResultReceipt(root, repairDispatchSnapshot(t, root).State, packets[0].ID, plan.Binding.OwnerAgentID)
			if err != nil || !found || !final.WorkComplete {
				t.Fatalf("accepted Final missing: %+v %t %v", final, found, err)
			}
			if got, err := os.ReadFile(private); err != nil || string(got) != change.content {
				t.Fatalf("later private work changed: %q %v", got, err)
			}
		})
	}
}
