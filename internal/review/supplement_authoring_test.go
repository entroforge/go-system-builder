package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupplementProfessionalInputComputesMachineFieldsWithoutRewritingAuthor(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath, _ := supplementFixture(t, root)
	file := filepath.Join(root, "new-observation.json")
	evidenceBytes := []byte("real additional observation")
	if err := os.WriteFile(filepath.Join(root, "observed.txt"), evidenceBytes, 0600); err != nil {
		t.Fatal(err)
	}
	// This is the ordinary explicit entry's actual attribution declaration,
	// not an invented Runtime identity or a canonical machine envelope.
	professional := []byte(`{"author":"agent-qa-1","new_observation":"The second request reproduces the same deviation.","evidence_refs":["path:observed.txt"]}`)
	if err := os.WriteFile(file, professional, 0600); err != nil {
		t.Fatal(err)
	}
	findingBefore := mustReadTestFile(t, findingArtifactPath(root, "finding-qa-1"))
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	receipt, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: file, InRoundNote: true, OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	artifact := mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(receipt.Path)))
	var supplement Supplement
	if err := json.Unmarshal(artifact, &supplement); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(supplement.SupplementID, "supplement-") || supplement.SchemaVersion != "1.0.0" || supplement.SupplementsFindingID != "finding-qa-1" || supplement.Author != "agent-qa-1" || supplement.CreatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("original machine bindings missing: %+v", supplement)
	}
	if err := verifySupplementHash(artifact, supplement.Hash); err != nil {
		t.Fatal(err)
	}
	if len(supplement.EvidenceRefs) != 1 || supplement.EvidenceRefs[0] != "path:observed.txt#sha256="+sha256Of(evidenceBytes) {
		t.Fatalf("new evidence not fixed at original reception: %v", supplement.EvidenceRefs)
	}
	if !bytes.Equal(professional, mustReadTestFile(t, file)) || !bytes.Equal(findingBefore, mustReadTestFile(t, findingArtifactPath(root, "finding-qa-1"))) {
		t.Fatal("supplement rewrote authoring input or original Finding")
	}
	stateBefore, journalBefore := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	replayed, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: file, InRoundNote: true, OccurredAt: now.Add(time.Hour)})
	if err != nil || replayed.SupplementID != receipt.SupplementID || replayed.SHA256 != receipt.SHA256 {
		t.Fatalf("same professional input did not reuse original accepted object: %+v %v", replayed, err)
	}
	if !bytes.Equal(stateBefore, mustReadTestFile(t, statePath)) || !bytes.Equal(journalBefore, mustReadTestFile(t, journalPath)) || !bytes.Equal(artifact, mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(receipt.Path)))) {
		t.Fatal("normal replay generated another time, artifact or Writer event")
	}
	if err := os.WriteFile(filepath.Join(root, "observed.txt"), []byte("later drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: file, InRoundNote: true}); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("same input signed changed bytes into a second observation: %v", err)
	}
	if !bytes.Equal(stateBefore, mustReadTestFile(t, statePath)) || !bytes.Equal(journalBefore, mustReadTestFile(t, journalPath)) {
		t.Fatal("source drift appended or refreshed a supplement")
	}
	rows, err := SupplementsForFinding(root, statePath, "finding-qa-1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("replay/drift manufactured observations: %d %v", len(rows), err)
	}
	newObservation := bytes.ReplaceAll(professional, []byte("The second request reproduces the same deviation."), []byte("The later request now returned different output; this is a new observation."))
	if err := os.WriteFile(file, newObservation, 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err := SubmitSupplement(root, statePath, journalPath, SupplementRequest{FindingID: "finding-qa-1", FilePath: file, InRoundNote: true})
	if err != nil || fresh.SupplementID == receipt.SupplementID {
		t.Fatalf("genuinely new professional observation was not appendable: %+v %v", fresh, err)
	}
	if !bytes.Equal(artifact, mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(receipt.Path)))) {
		t.Fatal("a new observation rewrote the old supplement")
	}
}

func TestSupplementDoesNotInferNewAuthorFromOldFinderOrSkipAuthorization(t *testing.T) {
	for _, scenario := range []string{"missing-author", "unauthorized-replacement", "bound-caller-mismatch", "professional-followup-missing-discriminator"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath, _ := supplementFixture(t, root)
			input := map[string]any{"new_observation": "Actual new observation", "author": "agent-qa-1"}
			request := SupplementRequest{FindingID: "finding-qa-1", FilePath: filepath.Join(root, "observation.json"), InRoundNote: true}
			want := ""
			switch scenario {
			case "missing-author":
				delete(input, "author")
				want = "no actual author attribution"
			case "unauthorized-replacement":
				input["author"] = "agent-other"
				want = "not the original finder"
			case "bound-caller-mismatch":
				request.AuthorAgentID = "agent-other"
				want = "bound caller"
			case "professional-followup-missing-discriminator":
				request.InRoundNote = false
				want = "discriminator"
			}
			data, _ := json.Marshal(input)
			if err := os.WriteFile(request.FilePath, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			stateBefore, journalBefore := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
			if _, err := SubmitSupplement(root, statePath, journalPath, request); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("wanted %q, got %v", want, err)
			}
			if !bytes.Equal(stateBefore, mustReadTestFile(t, statePath)) || !bytes.Equal(journalBefore, mustReadTestFile(t, journalPath)) {
				t.Fatal("rejected attribution/gate changed Runtime")
			}
		})
	}
}
