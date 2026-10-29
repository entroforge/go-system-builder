package investigation_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func approvedContractRecoveryFixture(t *testing.T) (*intakeFixture, investigation.ContractRequest, runtime.Snapshot, [][]byte) {
	t.Helper()
	f := newIntakeFixture(t, []string{"finding-1"})
	setContractLifecycle(t, f)
	if _, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: 0, GroupingRationale: "sealed recovery fixture"}); err != nil {
		t.Fatal(err)
	}
	prepareCaseForContractApproval(t, f)
	draft := writeContractDraft(t, f.root, []string{"finding-1"})
	sha, evidence, _ := registerContractApprovalEvidence(t, f, draft)
	req := investigation.ContractRequest{ExpectedRevision: -1, CaseID: "investigation-case-observation-batch-r1", ContractPath: draft, ApprovedBy: "main-session", ApprovalHash: sha, ApprovalEvidenceID: evidence}
	beforeState, beforeJournal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	accepted, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, req)
	if err != nil {
		t.Fatal(err)
	}
	return f, req, accepted, [][]byte{beforeState, beforeJournal, mustRead(t, f.statePath), mustRead(t, f.journalPath)}
}

func TestApproveContractReplaySurvivesMissingPrivateDraft(t *testing.T) {
	f, req, accepted, original := approvedContractRecoveryFixture(t)
	if err := os.Remove(req.ContractPath); err != nil {
		t.Fatal(err)
	}
	replay, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, req)
	if err != nil || replay.Revision != accepted.Revision {
		t.Fatalf("accepted approval required rewriting its missing private draft: %v", err)
	}
	if !bytes.Equal(original[2], mustRead(t, f.statePath)) || !bytes.Equal(original[3], mustRead(t, f.journalPath)) {
		t.Fatal("approval replay wrote another acceptance")
	}
	if err := os.WriteFile(req.ContractPath, []byte(`{"changed":"professional content"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, req); err == nil {
		t.Fatal("changed draft was treated as the accepted approval")
	}
	if !bytes.Equal(original[2], mustRead(t, f.statePath)) || !bytes.Equal(original[3], mustRead(t, f.journalPath)) {
		t.Fatal("conflicting retry rewrote accepted approval")
	}
}

func TestApproveContractRecoversOriginalSealedPairAndReplaysOnce(t *testing.T) {
	for _, window := range []string{"pending-before-state", "state-before-journal", "journal-before-marker-cleanup"} {
		t.Run(window, func(t *testing.T) {
			f, req, accepted, original := approvedContractRecoveryFixture(t)
			state, journal := original[0], original[1]
			if window != "pending-before-state" {
				state = original[2]
			}
			if window == "journal-before-marker-cleanup" {
				journal = original[3]
			}
			if err := os.WriteFile(f.statePath, state, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.journalPath, journal, 0600); err != nil {
				t.Fatal(err)
			}
			writeInvestigationJSON(t, f.statePath+".commit-pending.json", buildS8SealedCommitMarker(t, original[0], original[1], original[2], original[3]))
			if _, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot(); err == nil {
				t.Fatal("observation mutated a pending approval")
			}
			writer := runtime.NewWriter(f.statePath, f.journalPath, f.root, semantic.RuntimeCandidateValidator{})
			if recovered, err := writer.RecoverPendingOperations(); err != nil || !recovered {
				t.Fatalf("original pending recovery: %t %v", recovered, err)
			}
			replay, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, req)
			if err != nil || replay.Revision != accepted.Revision {
				t.Fatalf("original approval replay: %v", err)
			}
			if !bytes.Equal(original[2], mustRead(t, f.statePath)) || !bytes.Equal(original[3], mustRead(t, f.journalPath)) {
				t.Fatal("recovery changed approved facts or consumed authority twice")
			}
			pointer := replay.State["review"].(map[string]any)["investigation"].(map[string]any)
			for _, key := range []string{"path", "repair_contract_ref"} {
				if _, err := os.Stat(filepath.Join(f.root, pointer[key].(string))); err != nil {
					t.Fatalf("approved artifact disappeared: %v", err)
				}
			}
			if strings.Count(string(mustRead(t, f.journalPath)), `"transition_id":"S8-REPAIR-CONTRACT-APPROVAL"`) != 1 {
				t.Fatal("approval was duplicated")
			}
		})
	}
}
