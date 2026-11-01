package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS9FinalOriginalWriterSkipsOccupiedPathAndReusesRecordedResult(t *testing.T) {
	root, worker, plan, _, _ := nativeS9WorkerFixture(t)
	var diagnostic bytes.Buffer
	if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, plan, worker, false, &diagnostic, nil); err != nil {
		t.Fatalf("original Result: %v %s", err, &diagnostic)
	}
	snapshot := repairDispatchSnapshot(t, root)
	packets, err := runtime.CurrentRepairResultPackets(root, snapshot.State, plan.Binding.OwnerAgentID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("original Result question: %d %v", len(packets), err)
	}
	packet := packets[0]
	occupied := filepath.Join(root, filepath.FromSlash(repair.ArtifactStoragePath("results", packet.ResultID)))
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte("unrelated file"), 0600); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("original location: %v", err)
	}
	execution := registry.Executions[plan.Binding.ManifestAssignmentID]
	var out bytes.Buffer
	diagnostic.Reset()
	code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID, "--check-command", "test -f internal/api/one.go"}, &out, &diagnostic)
	if code != 1 || !strings.Contains(diagnostic.String(), "integration verified") || !strings.Contains(diagnostic.String(), "cleanup pending") || !strings.Contains(diagnostic.String(), "Final remains accepted") {
		t.Fatalf("original Main integration and cleanup status: code=%d %s %s", code, &out, &diagnostic)
	}
	checkpoint, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, execution))
	if err != nil || !found || checkpoint.State != integration.StateCleanupPending || checkpoint.VerifiedAt == "" || len(checkpoint.CheckReceipts) == 0 {
		t.Fatalf("actual integration/checks must remain recorded while cleanup is pending: %+v found=%t err=%v", checkpoint, found, err)
	}
	product, err := os.ReadFile(filepath.Join(root, "internal/api/one.go"))
	if err != nil || !bytes.Contains(product, []byte("Restored = true")) {
		t.Fatalf("actual Main product was not integrated: %q %v", product, err)
	}
	planPath, answerPath := nativeS9TestAuthorPaths(t, root, plan)
	for _, path := range []string{planPath, answerPath} {
		if _, err := os.Stat(filepath.Join(worker, path)); err != nil {
			t.Fatalf("private answer %s must remain available: %v", path, err)
		}
	}
	after := repairDispatchSnapshot(t, root)
	refs := repairArtifactRefs(repairPointerCLI(after.State), "result_refs", "result_ref")
	if len(refs) != 1 {
		t.Fatalf("expected one registered Result, got %+v", refs)
	}
	selected := refs[0].Path
	if selected == "" || filepath.Join(root, filepath.FromSlash(selected)) == occupied {
		t.Fatalf("original Writer claimed occupied path: %q", selected)
	}
	original, err := os.ReadFile(occupied)
	if err != nil || string(original) != "unrelated file" {
		t.Fatalf("unknown file changed: %q %v", original, err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(selected)))
	if err != nil {
		t.Fatal(err)
	}
	var result repair.RepairResult
	if err := json.Unmarshal(data, &result); err != nil || result.Receipt == nil || result.Receipt.ResultRef != selected || result.Receipt.Provenance.PacketID != packet.ID {
		t.Fatalf("Writer did not record actual Result reference: %+v %v", result.Receipt, err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	paths := make([]string, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, ref, err := repair.SubmitIssuedRepairResult(context.Background(), root, statePath, journalPath, repair.SubmitIssuedRepairResultRequest{ExpectedRevision: -1, PacketID: packet.ID, OwnerAgentID: plan.Binding.OwnerAgentID})
			errs[i], paths[i] = err, ref.Path
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil || paths[i] != selected {
			t.Fatalf("recorded Result replay %d: path=%q err=%v", i, paths[i], errs[i])
		}
	}
	if got, err := os.ReadFile(occupied); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("replay changed unknown occupant: %q %v", got, err)
	}
}
