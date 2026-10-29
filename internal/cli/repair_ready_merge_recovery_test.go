package cli

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestNativeS9PublicIntegrationRecoversLostMergeBeforeLaterMainCommit(t *testing.T) {
	root, worker, e, packet, inspection := s9NativeIntegrationCandidate(t)
	command := "git grep -q 'const Restored = true' -- internal/api/one.go && test -f later.txt"
	const interrupted = "actual Git merge completed before receipt write"
	writes, caught := 0, false
	store := &integration.CheckpointStore{Clock: func() time.Time {
		writes++
		if writes == 2 {
			panic(interrupted)
		}
		return time.Now().UTC()
	}, NowString: func(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }}
	func() {
		defer func() {
			if p := recover(); p != nil {
				if p != interrupted {
					panic(p)
				}
				caught = true
			}
		}()
		_, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: e.RuntimeID, RequiredChecks: []string{command}, CheckRunner: integration.CommandCheckRunner, CheckpointStore: store})
		t.Fatalf("did not reach real merge/receipt interruption: %v", err)
	}()
	ready, found, err := store.Load(workspaceExecutionCheckpointPath(root, e))
	if !caught || err != nil || !found || ready.State != integration.StateReady || ready.MergeCommit != "" {
		t.Fatalf("real ready checkpoint missing: %+v %v", ready, err)
	}
	merge := repairLeadGit(t, root, "rev-parse", "HEAD")
	if merge == ready.TargetHead {
		t.Fatal("fixture interrupted before the real Git merge")
	}
	writeRepairLeadFile(t, root, "later.txt", "later synthetic Main contribution\n")
	repairLeadGit(t, root, "add", "later.txt")
	repairLeadGit(t, root, "commit", "-qm", "later Main contribution after lost merge response")
	head, count := repairLeadGit(t, root, "rev-parse", "HEAD"), repairLeadGit(t, root, "rev-list", "--all", "--count")
	args := []string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", command}
	var output, diagnostic bytes.Buffer
	if code := runWorkspaceDelivery(args, &output, &diagnostic); code != 0 {
		t.Fatalf("public native S9 recovery: %d %s %s", code, &output, &diagnostic)
	}
	complete, found, err := store.Load(workspaceExecutionCheckpointPath(root, e))
	if err != nil || !found || complete.State != integration.StateComplete || complete.MergeCommit != merge || complete.TestedHead != head || len(complete.CheckReceipts) != 1 {
		t.Fatalf("public recovery lost original merge/current actual check: %+v %v", complete, err)
	}
	current := repairDispatchSnapshot(t, root)
	if _, found, err := runtime.VerifiedRepairResultReceipt(root, current.State, packet.ID, e.AgentID); err != nil || !found {
		t.Fatalf("recovered native Final lacks its real Main proof: %t %v", found, err)
	}
	if len(repairArtifactRefs(repairPointerCLI(current.State), "result_refs", "result_ref", "result_sha256")) != 1 || repairLeadGit(t, root, "rev-parse", "HEAD") != head || repairLeadGit(t, root, "rev-list", "--all", "--count") != count {
		t.Fatal("recovery duplicated Final or Git merge")
	}
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatalf("original cleanup did not finish: %v", err)
	}
	before := repairDispatchRuntimeBytes(t, root)
	if code := runWorkspaceDelivery(args, &bytes.Buffer{}, &diagnostic); code != 0 {
		t.Fatalf("completed native recovery replay: %d %s", code, &diagnostic)
	}
	repairDispatchAssertUnchanged(t, root, before)
}
