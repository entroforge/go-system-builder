package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
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
	if code := runWorkspaceDelivery([]string{"integrate", "--root", root, "--assignment", e.AssignmentID, "--agent", e.AgentID, "--check-command", "git grep -q 'const Restored = true' -- internal/api/one.go"}, &out, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "cleanup pending") {
		t.Fatalf("expected a verified Main integration with an independent cleanup pending: code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
	}
	// The integration itself is verified. A pending worktree cleanup is a
	// resource to handle, not a failed integration, and it does not require the
	// author to delete private material first.
	if !strings.Contains(diagnostic.String(), "integration verified") {
		t.Fatalf("Main integration did not report a verified integration: %s %s", &out, &diagnostic)
	}
	cp, found, err := integration.DefaultCheckpointStore().Load(workspaceExecutionCheckpointPath(root, e))
	if err != nil || !found || cp.State != integration.StateCleanupPending || cp.VerifiedAt == "" {
		t.Fatalf("Main verification and ACK checkpoint: %+v %v", cp, err)
	}
	// The referenced evidence file does not exist. It was neither copied into
	// Main's area nor used as an existence gate: the verification above stands.
	if _, err := os.Stat(filepath.Join(root, "logs/not-present.txt")); !os.IsNotExist(err) {
		t.Fatalf("the absent evidence reference was copied into Main's area: %v", err)
	}
	// The preserved private material and its location are reported honestly,
	// and the original worktree was not force-deleted.
	if !strings.Contains(diagnostic.String(), "preserve worktree") {
		t.Fatalf("the pending cleanup did not report the preserved worktree: %s", &diagnostic)
	}
	if _, err := os.Stat(worker); err != nil {
		t.Fatalf("the pending cleanup force-deleted the original worktree: %v", err)
	}
	// The accepted original Final and its evidence reference survived the
	// pending cleanup: the resource to handle did not revoke the reception.
	after := repairDispatchSnapshot(t, root)
	finals := repairArtifactRefs(repairPointerCLI(after.State), "result_refs", "result_ref")
	if len(finals) != 1 {
		t.Fatalf("the pending cleanup revoked the accepted original Final: %v", finals)
	}
}
