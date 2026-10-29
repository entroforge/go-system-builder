package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9PrivateScratchFollowsOriginalCandidateAndCleanup(t *testing.T) {
	root, worker, packet, _, professional := nativeS9WorkerFixture(t)
	_, resultPath, err := nativeS9AuthorPaths(root, packet)
	if err != nil {
		t.Fatal(err)
	}
	area := filepath.ToSlash(filepath.Dir(resultPath))
	captured := area + "/logs/referenced.json"
	writeRepairLeadFile(t, worker, captured, "actual synthetic check evidence\n")
	writeRepairLeadFile(t, worker, area+"/logs/unreferenced.txt", "ordinary private scratch\n")
	writeRepairLeadFile(t, worker, area+"/nested/scratch.bin", "other private scratch\n")
	out := filepath.Join(t.TempDir(), "external-owner-data")
	if err := os.WriteFile(out, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(worker, filepath.FromSlash(area), "nested", "link")); err != nil {
		t.Fatal(err)
	}
	professional.EvidenceRefs = append(professional.EvidenceRefs, captured)
	writeRepairLeadJSON(t, filepath.Join(worker, filepath.FromSlash(resultPath)), professional)
	var diagnostic, stdout bytes.Buffer
	if err := receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, packet, worker, false, &diagnostic, nil); err != nil {
		t.Fatalf("original Candidate must accept its private scratch: %v %s", err, diagnostic.String())
	}
	snapshot := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snapshot.State)
	if err != nil || registry == nil {
		t.Fatalf("registered original execution: %v", err)
	}
	execution := registry.Executions[packet.Binding.ManifestAssignmentID]
	if execution.DeliveryRef == "" {
		t.Fatal("original Candidate was not recorded")
	}
	code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID, "--check-command", "test -f internal/api/one.go"}, &stdout, &diagnostic)
	if code != 0 {
		t.Fatalf("original Main integration and cleanup: code=%d stdout=%s stderr=%s", code, stdout.String(), diagnostic.String())
	}
	after := repairDispatchSnapshot(t, root)
	if len(repairArtifactRefs(repairPointerCLI(after.State), "result_refs", "result_ref", "result_sha256")) != 1 {
		t.Fatal("original Main Final was not registered")
	}
	if _, err := os.Lstat(worker); !os.IsNotExist(err) {
		t.Fatalf("verified and ACKed Worker was not cleaned: %v", err)
	}
	if raw, err := os.ReadFile(out); err != nil || string(raw) != "preserve" {
		t.Fatalf("private symlink target changed: %q %v", raw, err)
	}
}
