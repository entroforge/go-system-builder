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
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	stateBefore, _ := os.ReadFile(statePath)
	journalBefore, _ := os.ReadFile(journalPath)
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
				expected = "differs from its exact projected bytes"
			}
			out.Reset()
			if handled, _ := runObservedQuestionnaireSaves(root, request, &out, &diagnostic); !handled || !strings.Contains(out.String(), expected) {
				t.Fatalf("changed/missing/invalid draft bypassed original save feedback: %s", &out)
			}
			out.Reset()
			if handled, _ := receiveSavedQuestionnaire(root, explicit, &out, &diagnostic); !handled || !strings.Contains(out.String(), expected) {
				t.Fatalf("explicit save bypassed the original consumer: %s", &out)
			}
		})
	}
	stateAfter, _ := os.ReadFile(statePath)
	journalAfter, _ := os.ReadFile(journalPath)
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("passive projection/explicit save changed Runtime or inferred approval")
	}
}
