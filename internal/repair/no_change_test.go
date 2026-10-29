package repair_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestEmptyChangesetFingerprintsRealRangeWithoutInventingArtifacts(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "No Change Fixture")
	runGit(t, root, "config", "commit.gpgsign", "false")
	writeFile(t, root, "product.txt", "existing product\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial fixture product")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	var ids []string
	for _, session := range []string{"repair-session-no-change-one", "repair-session-no-change-two"} {
		changes, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: session, BaseRef: head, HeadRef: head})
		if err != nil {
			t.Fatalf("real empty range rejected: %v", err)
		}
		if changes.Artifacts == nil || len(changes.Artifacts) != 0 || changes.Digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
			t.Fatalf("empty range is not an honest empty content set: %+v", changes)
		}
		ref, err := repair.PersistChangeset(root, changes)
		if err != nil {
			t.Fatalf("Session identity collided at immutable storage: %v", err)
		}
		validated, err := repair.ValidateChangeset(root, ref)
		if err != nil || validated.SessionID != session || len(validated.Artifacts) != 0 {
			t.Fatalf("persisted empty changeset: %+v %v", validated, err)
		}
		ids = append(ids, changes.ChangesetID)
	}
	if ids[0] == ids[1] {
		t.Fatal("different original Sessions share a global immutable empty Changeset identity")
	}
	if _, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "repair-session-invalid", BaseRef: "not-a-real-ref", HeadRef: "not-a-real-ref"}); err == nil {
		t.Fatal("an invalid source became an empty diff")
	}
	if _, err := repair.ComputeChangeset(root, repair.ChangesetRequest{}); err == nil {
		t.Fatal("missing coordinates became a no-change observation")
	}
}

func TestNoChangeImpactRequiresProfessionalJudgmentWithoutFakeChangeType(t *testing.T) {
	for _, scenario := range []string{"empty-with-judgment", "missing-decisions", "missing-rationale", "changed-without-type"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			request := repair.ChangeImpactRequest{
				ImpactID: "impact-no-change", RuntimeID: "loop-no-change", ReqID: "REQ-039", BaselineGeneration: 1,
				SourceCaseIDs: []string{"investigation-case-no-change"}, ChangeTypes: []string{}, ChangedArtifacts: []repair.ArtifactRef{},
				Decisions:                 []repair.ImpactDecision{{SourceID: "investigation-case-no-change", TargetID: "root-1", Relation: "requires-verification", RuleID: "IM-CONTRACT", Decision: "reverify", Scope: []string{}, Rationale: "The existing product needs no edit; independently check the original Contract observations", RecoveryEvidence: []string{}}},
				RequiredReverificationIDs: []string{"reverify-no-change"}, AnalyzedBy: "Main",
			}
			switch scenario {
			case "missing-decisions":
				request.Decisions = nil
			case "missing-rationale":
				request.Decisions[0].Rationale = " "
			case "changed-without-type":
				request.ChangedArtifacts = []repair.ArtifactRef{{Path: "product.txt", SHA256: strings.Repeat("a", 64)}}
			}
			impact, ref, err := repair.CreateChangeImpact(root, request)
			if scenario != "empty-with-judgment" {
				if err == nil {
					t.Fatal("unfinished professional judgment was accepted")
				}
				if _, err := os.Stat(filepath.Join(root, ".claude/review/repair/change-impact/impact-no-change.json")); !os.IsNotExist(err) {
					t.Fatalf("rejected professional judgment wrote an artifact: %v", err)
				}
				return
			}
			if err != nil || impact.ChangedArtifacts == nil || len(impact.ChangeTypes) != 0 || len(impact.Decisions) != 1 || len(impact.RequiredReverificationIDs) != 1 {
				t.Fatalf("honest no-change Impact: %+v %v", impact, err)
			}
			if _, err := repair.ValidateChangeImpact(root, ref); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNoFinalIsNotAnAcceptedEmptyRepairBatch(t *testing.T) {
	for _, state := range []map[string]any{{}, {"review": map[string]any{"repair": map[string]any{"session_id": "repair-session-no-final"}}}} {
		if _, err := repair.CurrentRepairResultChangeset(t.TempDir(), state); err == nil {
			t.Fatal("missing original Final facts were projected as a legitimate no-change contribution")
		}
	}
}
