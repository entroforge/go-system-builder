package qualitygate_test

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/mainreceipt"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestCanonicalBuilderReceiptAcceptsMainRecheckHeadAndPinsBoundBranch(t *testing.T) {
	fixture := newCanonicalReceiptFixture(t)
	installCanonicalMainFacts(t, fixture)
	root := fixture.input.Root

	checkpointRef := path.Join(".claude/evidence", fixture.record.RuntimeID, "g3", "worktree", fixture.record.AssignmentID, "checkpoint.json")
	checkpointPath := filepath.Join(root, filepath.FromSlash(checkpointRef))
	checkpointBytes, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint map[string]any
	if err := json.Unmarshal(checkpointBytes, &checkpoint); err != nil {
		t.Fatal(err)
	}

	// Main checks may be rerun on a descendant of the actual merge, and the
	// development branch may advance again after those checks. The original
	// Source/Target/Merge coordinates remain bound to the accepted Result.
	gitReceipt(t, root, "commit", "--allow-empty", "-m", "recheck required Main profiles")
	testedHead := strings.TrimSpace(gitReceipt(t, root, "rev-parse", "HEAD"))
	gitReceipt(t, root, "commit", "--allow-empty", "-m", "later Main development")
	checkpoint["tested_head"] = testedHead
	checkpoint["canonical_main_facts"].(map[string]any)["tested_head"] = testedHead
	writeCanonicalReceiptFile(t, root, checkpointRef, canonicalReceiptJSON(t, checkpoint))

	receipt := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !receipt.Accepted || !receipt.Integrated || receipt.WorkComplete {
		accepted, acceptedErr := assignmentresult.LoadAccepted(fixture.input.Snapshot.State, fixture.record, fixture.request.CompletionRef, func(ref string) ([]byte, error) {
			return workpacket.ReadFile(root, ref, 20<<20)
		})
		resultBytes, resultErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(fixture.request.CompletionRef)))
		finalHead := gitReceipt(t, root, "rev-parse", "HEAD")
		_, proofErr := mainreceipt.Verify(context.Background(), mainreceipt.Input{
			AuthorityRoot: root, Record: fixture.record, Accepted: accepted, ResultRef: fixture.request.CompletionRef,
			ResultSHA256: canonicalReceiptSHA256(resultBytes), ExecutionGeneration: 0,
			AuthorizedMainRoots: []string{root}, ExpectedTargetBranch: "main", FinalTargetHead: finalHead,
		})
		t.Fatalf("verified recheck descendant on the pinned bound branch was not accepted as Main-only evidence: accepted=%v result=%v shared proof=%v receipt=%+v", acceptedErr, resultErr, proofErr, receipt)
	}

	// Repointing the bound branch so it no longer contains the recorded merge
	// invalidates the same checkpoint; the CP does not choose its own final head.
	targetHead := checkpoint["target_head"].(string)
	gitReceipt(t, root, "reset", "--hard", targetHead)
	changed := qualitygate.CurrentBuilderReceipt(fixture.input, fixture.request)
	if !changed.Accepted || changed.Integrated || changed.WorkComplete || len(changed.WaitingOn) != 1 || changed.WaitingOn[0].Reason != "integration_receipt_invalid" {
		t.Fatalf("Main receipt survived after the bound development branch lost its merge: %+v", changed)
	}
}
