package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS9EvidenceReferenceIsPreservedWithoutCopyOrExistenceGate(t *testing.T) {
	root, worker, p, input, answer := nativeS9WorkerFixture(t)
	// Evidence is the Worker's professional expression; the listener does not
	// require a referenced file to exist, copy it or authenticate its bytes.
	answer.EvidenceRefs = []string{"path:logs/not-present.txt"}
	result := nativeS9TestResultPath(t, root, p)
	if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
		t.Fatal(err)
	}
	input.Event = "SubagentStop"
	var out, diagnostic bytes.Buffer
	if handled, code := nativeS9Hook(root, input, &out, &diagnostic); !handled || code != 0 {
		t.Fatalf("original Result: %v %d %s", handled, code, &diagnostic)
	}
	snap := repairDispatchSnapshot(t, root)
	registry, err := workspace.Decode(snap.State)
	if err != nil || registry == nil {
		t.Fatalf("original location: %v", err)
	}
	e := registry.Executions[p.Binding.ManifestAssignmentID]
	candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef})
	if err != nil || candidate.Provenance == nil {
		t.Fatalf("accepted original: %v %s", err, &diagnostic)
	}
	var original struct {
		EvidenceRefs []string `json:"evidence_refs"`
	}
	if err := json.Unmarshal(candidate.Provenance.SubmittedAnswer, &original); err != nil || len(original.EvidenceRefs) != 1 || original.EvidenceRefs[0] != answer.EvidenceRefs[0] {
		t.Fatalf("original evidence reference changed: %+v %v", original, err)
	}
	out.Reset()
	diagnostic.Reset()
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic); code != 0 {
		t.Fatalf("actual Main integration: %d %s %s", code, &out, &diagnostic)
	}
}
