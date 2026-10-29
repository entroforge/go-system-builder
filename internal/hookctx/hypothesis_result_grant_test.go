package hookctx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS8FreshWriteNeedsOpenHypothesisAndNoPriorResult(t *testing.T) {
	root := t.TempDir()
	casePath := ".claude/review/investigation/cases/case-r2.json"
	caseRevision := 2
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(casePath)), 0o700); err != nil {
		t.Fatal(err)
	}
	packet := workpacket.HypothesisResultPacket{Binding: workpacket.HypothesisResultBinding{
		CaseID: "case-1", CaseRef: casePath, CaseRevision: 2, HypothesisID: "hypothesis-1", AssignmentID: "assignment-1",
		HypothesisQuestionSHA256: "", SourceFindingIDs: []string{"finding-1"},
		ObservationBatchID: "batch-1", ObservationBatchRef: ".claude/evidence/batch.json", ObservationBatchSHA256: "batch-sha",
		BaselineDigest: "baseline-digest",
	}}
	question := workpacket.HypothesisResultQuestion{
		HypothesisID: "hypothesis-1", AssignmentID: "assignment-1", Statement: "registered statement",
		Invariant: "registered invariant", Discriminator: "registered discriminator",
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{Support: "holds", Refute: "fails"},
		SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"evidence://finding-1"}, Status: "open",
	}
	questionDigest, err := workpacket.HypothesisQuestionDigest(question)
	if err != nil {
		t.Fatal(err)
	}
	packet.Binding.HypothesisQuestionSHA256 = questionDigest
	writeCase := func(status string, results []map[string]any, want bool) {
		t.Helper()
		question.Status = status
		document := map[string]any{
			"case_id": "case-1", "revision": caseRevision,
			"source_finding_ids":   []string{"finding-1"},
			"observation_batch_id": "batch-1", "observation_batch_ref": ".claude/evidence/batch.json", "observation_batch_sha256": "batch-sha",
			"baseline_digest":    "baseline-digest",
			"hypotheses":         []any{map[string]any{"hypothesis_id": question.HypothesisID, "assignment_id": question.AssignmentID, "statement": question.Statement, "invariant": question.Invariant, "discriminator": question.Discriminator, "expected_outcomes": question.ExpectedOutcomes, "source_finding_ids": question.SourceFindingIDs, "evidence_refs": question.EvidenceRefs, "status": status}},
			"hypothesis_results": results,
		}
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		packet.Binding.CaseSHA256 = hex.EncodeToString(digest[:])
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(casePath)), data, 0o600); err != nil {
			t.Fatal(err)
		}
		state := map[string]any{"review": map[string]any{"investigation": map[string]any{
			"case_id": "case-1", "path": casePath, "sha256": packet.Binding.CaseSHA256, "revision": caseRevision,
		}}}
		if got := s8HypothesisResultOpen(root, state, packet); got != want {
			t.Fatalf("s8HypothesisResultOpen(status=%s, results=%v)=%v, want %v", status, results, got, want)
		}
	}
	writeCase("open", nil, true)
	writeCase("open", []map[string]any{{"hypothesis_id": "other", "assignment_id": "assignment-1"}}, true)
	writeCase("supported", nil, false)
	writeCase("open", []map[string]any{{"hypothesis_id": "hypothesis-1", "assignment_id": "assignment-1"}}, false)
	writeCase("open", []map[string]any{{"hypothesis_id": "hypothesis-1", "assignment_id": "other-assignment"}}, true)
	casePath = ".claude/review/investigation/cases/case-r3.json"
	caseRevision = 3
	writeCase("open", nil, true)
}
