package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS9PreferredEvidenceReachesOriginalMainIntegration(t *testing.T) {
	root, worker, p, input, answer := nativeS9WorkerFixture(t)
	writeRepairLeadFile(t, worker, "logs/check.txt", "actual original fixture check output\n")
	answer.EvidenceRefs = []string{"path:logs/check.txt"}
	result := nativeS9TestResultPath(t, root, p)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("Stop: %v %d %s", handled, code, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	if e.DeliveryRef == "" {
		t.Fatalf("candidate missing: %s", &diagnostic)
	}
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256})
	if err != nil || candidate.Provenance == nil || len(candidate.Provenance.LocalEvidence) == 0 {
		t.Fatalf("missing immutable captured evidence: %v", err)
	}
	out.Reset()
	diagnostic.Reset()
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic); code != 0 {
		t.Fatalf("Main integration/Final: %d %s %s", code, &out, &diagnostic)
	}
	found := false
	for _, local := range candidate.Provenance.LocalEvidence {
		if local.SourcePath != "logs/check.txt" {
			continue
		}
		found = true
		data, err := workpacket.ReadFile(root, local.Path, 20<<20)
		if err != nil || string(data) != "actual original fixture check output\n" || workpacket.Digest(data) != local.SHA256 {
			t.Fatalf("original evidence was lost during cleanup: %v", err)
		}
	}
	if !found {
		t.Fatal("preferred log was not retained in original provenance")
	}
}
