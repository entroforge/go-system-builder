package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

// The native S9 brief and installed method explicitly recommend path:logs/...
// for local evidence. Follow that guidance without adding an evidence commit
// or changing tool permissions, then exercise the original receiver.
func TestAcceptanceS9PreferredLogEvidenceDoesNotBlockCandidate(t *testing.T) {
	root, worker, packet, input, answer := nativeS9WorkerFixture(t)
	writeRepairLeadFile(t, worker, "logs/check.txt", "synthetic fixture check output for this original delivery\n")
	answer.EvidenceRefs = []string{"path:logs/check.txt"}
	result := nativeS9TestResultPath(t, root, packet)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("original Stop did not run: %v %d %s %s", handled, code, &out, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil || registry.Executions[packet.Binding.ManifestAssignmentID].DeliveryRef == "" {
		t.Fatalf("following preferred path:logs/check.txt evidence did not produce the original candidate: %v; %s", err, &diagnostic)
	}
	if _, err := os.Stat(filepath.Join(worker, "logs/check.txt")); err != nil {
		t.Fatalf("receiver removed author's original evidence: %v", err)
	}
}
