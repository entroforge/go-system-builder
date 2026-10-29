package submission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func submissionHypothesisPacket(t *testing.T) workpacket.HypothesisResultPacket {
	t.Helper()
	question := workpacket.HypothesisResultQuestion{
		HypothesisID: "hypothesis-cache-1", AssignmentID: "assignment-investigator-1",
		Statement: "A stale cache entry causes the observed response.", Invariant: "A fresh read reflects the persisted value.",
		Discriminator: "Compare the same request with the cache bypassed.", Status: "open",
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: "Bypass removes the stale response.", Refute: "The stale response remains."},
		SourceFindingIDs: []string{"finding-001"}, EvidenceRefs: []string{"test://hypothesis-probe"},
	}
	route := workpacket.HypothesisResultCaseRoute{Status: "investigating"}
	questionSHA, err := workpacket.HypothesisQuestionDigest(question)
	if err != nil {
		t.Fatal(err)
	}
	routeSHA, err := workpacket.HypothesisCaseRouteDigest(route)
	if err != nil {
		t.Fatal(err)
	}
	finding := workpacket.HypothesisResultFinding{
		FindingID: "finding-001", Ref: ".claude/evidence/findings/finding-001.json", SHA256: strings.Repeat("d", 64),
		Document: json.RawMessage(`{"schema_version":"1.0.0","finding_id":"finding-001","claim_id":"claim-cache-read","lens":"qa","severity":"P1","expected":"A fresh read reflects the persisted value.","authority_refs":["REQ-001"],"observed":"The response is stale.","observation_mode":"code_inspection","encounter":{"journey_summary":"Read path inspected.","wall_action":"Read the value.","first_bad_checkpoint":"Cache returned older value.","inspection_entry":"internal/cache/read.go","symbol_trail":"Read -> Lookup"},"reproducibility":"always","evidence_refs":["test://finding-observation"]}`),
	}
	binding := workpacket.HypothesisResultBinding{
		RuntimeID: "loop-REQ-001", BaselineGeneration: 1, ReviewRound: 1,
		CaseID: "investigation-case-001", CaseRef: ".claude/review/investigation/cases/investigation-case-001-r2.json",
		CaseSHA256: strings.Repeat("a", 64), CaseRevision: 2, CaseRouteSHA256: routeSHA,
		ObservationBatchID: "observation-batch-001", ObservationBatchRef: ".claude/evidence/observation-batch-001.json",
		ObservationBatchSHA256: strings.Repeat("b", 64), BaselineDigest: strings.Repeat("c", 64),
		SourceFindingIDs: []string{"finding-001"}, HypothesisID: question.HypothesisID,
		HypothesisQuestionSHA256: questionSHA, HypothesisSourceFindingIDs: []string{"finding-001"},
		AssignmentID: question.AssignmentID, AssignmentSHA256: strings.Repeat("e", 64),
		TeamID: "workgroup-s8-1", TaskID: "TASK-001-01", TaskRef: "docs/tasks/TASK-001-01.md", TaskSHA256: strings.Repeat("f", 64),
		ManifestRef: ".claude/workgroups/workgroup-s8-1.json", ManifestSHA256: strings.Repeat("1", 64), OwnerAgentID: "investigator-01",
	}
	packet, err := workpacket.BuildHypothesisResult(binding, workpacket.HypothesisResultInputs{
		Hypothesis: question, CaseRoute: route, Findings: []workpacket.HypothesisResultFinding{finding},
		EvidenceRefs: []workpacket.HypothesisEvidenceRef{
			{Ref: "test://hypothesis-probe", Origin: "hypothesis", Attestation: "execution_anchor"},
			{Ref: "test://finding-observation", Origin: "finding", SourceFindingID: "finding-001", Attestation: "execution_anchor"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestHypothesisResultPathResolutionUsesOnlyExactIssuedPath(t *testing.T) {
	root := t.TempDir()
	packet := submissionHypothesisPacket(t)
	state := map[string]any{"runtime_id": packet.Binding.RuntimeID, "baseline": map[string]any{"generation": packet.Binding.BaselineGeneration}}
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	selected, selectedRoot, found, err := hypothesisResultPathInRoots(root, state, path, []workpacket.HypothesisResultPacket{packet}, false)
	if err != nil || !found || selected.ID != packet.ID || filepath.Clean(selectedRoot) != filepath.Clean(root) {
		t.Fatalf("exact issued path not selected: found=%v selected=%s root=%s err=%v", found, selected.ID, selectedRoot, err)
	}
	forged := strings.Replace(path, "/answer.json", "/other.json", 1)
	if _, _, found, err := hypothesisResultPathInRoots(root, state, forged, []workpacket.HypothesisResultPacket{packet}, false); err != nil || found {
		t.Fatalf("sibling path selected: found=%v err=%v", found, err)
	}
}

func TestHypothesisResultPathResolutionRejectsSymlinkSource(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	packet := submissionHypothesisPacket(t)
	parts := strings.Split(filepath.ToSlash(packet.AnswerPath), "/")
	parent := filepath.Join(root, filepath.FromSlash(strings.Join(parts[:len(parts)-2], "/")))
	if err := os.MkdirAll(filepath.Dir(parent), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": packet.Binding.RuntimeID, "baseline": map[string]any{"generation": packet.Binding.BaselineGeneration}}
	path := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if _, _, found, err := hypothesisResultPathInRoots(root, state, path, []workpacket.HypothesisResultPacket{packet}, false); err != nil || found {
		t.Fatalf("symlink source selected: found=%v err=%v", found, err)
	}
}

func TestHypothesisResultReceiptProjectionIsReadOnlyDomainCompletion(t *testing.T) {
	packet := submissionHypothesisPacket(t)
	snapshot := loopruntime.Snapshot{Revision: 12, State: map[string]any{"lifecycle": map[string]any{"state": "bug_resolution", "phase": "investigation"}}}
	accepted := loopruntime.HypothesisResultReceipt{
		PacketID: packet.ID, OwnerAgentID: packet.Binding.OwnerAgentID,
		AnswerSHA256: strings.Repeat("a", 64), AcceptedIdempotencyKey: "runtime:investigation-case:case:12",
		SuccessorCaseRef: ".claude/review/investigation/cases/case-r3.json", SuccessorCaseRevision: 3,
	}
	receipt, err := acceptedHypothesisResultReceipt(snapshot, packet, accepted)
	if err != nil || !receipt.Accepted || !receipt.WorkComplete || receipt.ResponsibilityStatus != "accepted" || len(receipt.DomainRefs) != 1 || receipt.DomainRefs[0] != accepted.SuccessorCaseRef {
		t.Fatalf("accepted Case receipt projection mismatch: %+v err=%v", receipt, err)
	}
	accepted.OwnerAgentID = "other"
	if _, err := acceptedHypothesisResultReceipt(snapshot, packet, accepted); err == nil {
		t.Fatal("receipt for another owner projected as this Assignment's completion")
	}
}
