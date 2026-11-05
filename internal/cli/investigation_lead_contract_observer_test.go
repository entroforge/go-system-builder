package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestS8ObservedContractProjectionSkipsOnlyMatchingCurrentDraft(t *testing.T) {
	root, path := s8LeadContractTestReady(t)
	writeRepairLeadJSON(t, path, s8LeadContractTestAnswer())
	s8LeadContractTestSave(t, root, path)
	current := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, current.State)
	data, _ := os.ReadFile(path)
	finalPath, finalBytes, err := investigationLeadContractProjectionBytes(root, data, document)
	if err != nil {
		t.Fatal(err)
	}
	if !s8ObservedContractAlreadyProjected(root, path, data, current.State, document) {
		t.Fatal("exact current projected draft was not skipped")
	}
	// The observer has already read the answer. Prove the skip helper consumes
	// those bytes and does not open the answer file a second time.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !s8ObservedContractAlreadyProjected(root, path, data, current.State, document) {
		t.Fatal("projection helper re-read the already-read answer")
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"changed answer", "missing final draft", "mismatched final draft", "bad JSON", "unknown field", "stale answer source", "changed Case", "missing Case"} {
		t.Run(name, func(t *testing.T) {
			inputPath, input := path, data
			caseData, _ := json.Marshal(document)
			var observedCase map[string]any
			if err := json.Unmarshal(caseData, &observedCase); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "changed answer":
				changed := s8LeadContractTestAnswer()
				changed.ArchitectureIntent = "a changed professional repair intent"
				input, _ = json.Marshal(changed)
			case "missing final draft":
				if err := os.Remove(finalPath); err != nil {
					t.Fatal(err)
				}
			case "mismatched final draft":
				if err := os.WriteFile(finalPath, []byte("different immutable bytes"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "bad JSON":
				input = []byte(`{"architecture_intent":`)
			case "unknown field":
				input = []byte(`{"unknown_professional_field":"edited"}`)
			case "stale answer source":
				inputPath = filepath.Join(filepath.Dir(path), "contract-"+strings.Repeat("0", 64)+".json")
			case "changed Case":
				observedCase["revision"] = float64(integerValue(document["revision"]) + 1)
			case "missing Case":
				observedCase = nil
			}
			t.Cleanup(func() {
				if err := os.WriteFile(finalPath, finalBytes, 0o644); err != nil {
					t.Fatal(err)
				}
			})
			if s8ObservedContractAlreadyProjected(root, inputPath, input, current.State, observedCase) {
				t.Fatal("changed, missing or invalid source was hidden from the original consumer")
			}
		})
	}
}

func TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedValidation(t *testing.T) {
	root, path := s8LeadContractTestReady(t)
	writeRepairLeadJSON(t, path, s8LeadContractTestAnswer())
	s8LeadContractTestSave(t, root, path)
	current := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, current.State)
	finalPath, finalBytes, _ := investigationLeadContractProjection(root, path, document)
	oldDecisionPath, err := investigationContractDecisionTargetFor(root, current.State, finalBytes)
	if err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	stateBefore, _ := os.ReadFile(statePath)
	journalBefore, _ := os.ReadFile(journalPath)
	evidenceBefore := len(investigationLeadTestSnapshot(t, root).State["evidence"].([]any))
	request := policy.Input{Event: "PostToolUse", ToolName: "Bash", CWD: root}
	var out, diagnostic bytes.Buffer
	if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); handled || out.Len() != 0 {
		t.Fatalf("unchanged projected Contract was passively resubmitted: %s", &out)
	}
	explicit := repairLeadSaveInput(root, path, "")
	if handled, _ := receiveSavedQuestionnaire(root, explicit, &out, &diagnostic); !handled || !strings.Contains(out.String(), "Save is not approval") {
		t.Fatalf("explicit save bypassed the original Contract consumer: %s", &out)
	}
	for _, name := range []string{"missing final draft", "changed answer", "bad JSON", "invalid unit scope", "mismatched final draft"} {
		t.Run(name, func(t *testing.T) {
			writeRepairLeadJSON(t, path, s8LeadContractTestAnswer())
			if err := os.WriteFile(finalPath, finalBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			expected := "Save is not approval"
			occupied := false
			switch name {
			case "missing final draft":
				if err := os.Remove(finalPath); err != nil {
					t.Fatal(err)
				}
			case "changed answer":
				changed := s8LeadContractTestAnswer()
				changed.ArchitectureIntent = "a newly authored repair intent"
				writeRepairLeadJSON(t, path, changed)
			case "bad JSON":
				if err := os.WriteFile(path, []byte(`{"architecture_intent":`), 0o644); err != nil {
					t.Fatal(err)
				}
				expected = "incomplete or invalid"
			case "invalid unit scope":
				changed := s8LeadContractTestAnswer()
				changed.RepairUnits[0]["scope"] = []string{"internal/outside-approved-scope"}
				writeRepairLeadJSON(t, path, changed)
				expected = "original unit scope validation"
			case "mismatched final draft":
				if err := os.WriteFile(finalPath, []byte("different bytes"), 0o644); err != nil {
					t.Fatal(err)
				}
				// The occupied review draft is immutable: the system neither
				// rewrites it nor presents it. It publishes another draft whose
				// bytes are exactly the projected bytes.
				// L3-S8-finding-investigation.md:377 keeps the machine from
				// claiming it can detect an unrecorded external byte
				// modification from a path or a version.
				occupied = true
			}
			out.Reset()
			if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); !handled || !strings.Contains(out.String(), expected) {
				t.Fatalf("changed/missing/invalid draft bypassed original save feedback: %s", &out)
			}
			out.Reset()
			if handled, _ := receiveSavedQuestionnaire(root, explicit, &out, &diagnostic); !handled || !strings.Contains(out.String(), expected) {
				t.Fatalf("explicit save bypassed the original consumer: %s", &out)
			}
			if occupied {
				receipt := out.String()
				// (1) The occupied draft keeps the external bytes. The system
				// must not "repair" it back to the projected bytes.
				occupiedBytes, err := os.ReadFile(finalPath)
				if err != nil || string(occupiedBytes) != "different bytes" {
					t.Fatalf("occupied review draft was rewritten by the system: %q err=%v", occupiedBytes, err)
				}
				// (2) The receipt names another review draft, and that draft
				// carries the exact projected bytes.
				projected := ""
				entries, err := os.ReadDir(filepath.Dir(finalPath))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasPrefix(entry.Name(), "draft-") {
						continue
					}
					candidate := filepath.Join(filepath.Dir(finalPath), entry.Name())
					if candidate == finalPath {
						continue
					}
					body, err := os.ReadFile(candidate)
					if err != nil || !bytes.Equal(body, finalBytes) {
						continue
					}
					projected = candidate
				}
				if projected == "" {
					t.Fatalf("no other review draft carries the exact projected bytes: %s", receipt)
				}
				if !strings.Contains(receipt, projected) {
					t.Fatalf("receipt did not name the new review draft %s: %s", projected, receipt)
				}
				// (3) The tampered draft is never presented as the review draft.
				if strings.Contains(receipt, finalPath) {
					t.Fatalf("receipt presented the tampered review draft: %s", receipt)
				}
				// (4) The approval application for the new draft is another
				// original file; the earlier one is not redirected at the new
				// draft and is not removed.
				newDecisionPath, err := investigationContractDecisionTargetFor(root, current.State, finalBytes)
				if err != nil {
					t.Fatal(err)
				}
				if newDecisionPath.path == oldDecisionPath.path {
					t.Fatalf("the new review draft reused the earlier approval application: %s", newDecisionPath.path)
				}
				if !strings.Contains(receipt, newDecisionPath.path) {
					t.Fatalf("receipt did not name the new approval application %s: %s", newDecisionPath.path, receipt)
				}
				if _, err := os.Stat(oldDecisionPath.path); err != nil {
					t.Fatalf("the earlier approval application was removed: %v", err)
				}
				// (5) The machine does not claim it detected an external byte
				// modification from a path or a version. The retired report is
				// absent, and no approval is inferred.
				for _, forbidden := range []string{"differs from its exact projected bytes", "is approved", "approval granted"} {
					if strings.Contains(receipt, forbidden) {
						t.Fatalf("receipt made a retired approval/detection claim (%q): %s", forbidden, receipt)
					}
				}
			}
		})
	}
	stateAfter, _ := os.ReadFile(statePath)
	journalAfter, _ := os.ReadFile(journalPath)
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("passive projection/explicit save changed Runtime or inferred approval")
	}
	// Reverse: no approval evidence was registered at all.
	if evidenceAfter := len(investigationLeadTestSnapshot(t, root).State["evidence"].([]any)); evidenceAfter != evidenceBefore {
		t.Fatalf("passive projection/explicit save registered approval evidence: %d -> %d", evidenceBefore, evidenceAfter)
	}
}
