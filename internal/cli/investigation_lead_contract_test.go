package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// All authored repair/approval content below is mechanism fixture input,
// not an actual human authorization or a model's professional investigation.
func TestS8LeadContractProjectionPreservesWorkAndWaitsForActualAuthority(t *testing.T) {
	root, path := s8LeadContractTestReady(t)
	initial := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadContractContext(root, policy.Input{Event: "PreCompact"}, initial)
	if !strings.Contains(context, path) || !strings.Contains(context, "Not prepared yet") {
		t.Fatalf("missing exact read-only preparation gap: %s", context)
	}
	stageInvestigationLeadContractContext(root, policy.Input{Event: "PreToolUse"}, initial)
	draft, _ := os.ReadFile(path)
	if investigationLeadContractHasProfessionalContent(path, draft) {
		t.Fatal("untouched Contract draft is professional work")
	}
	writeRepairLeadJSON(t, path, s8LeadContractTestAnswer())
	authored, _ := os.ReadFile(path)
	stageInvestigationLeadContractContext(root, policy.Input{Event: "PreToolUse"}, initial)
	if after, _ := os.ReadFile(path); !bytes.Equal(authored, after) {
		t.Fatal("reminder overwrote professional work")
	}
	feedback := s8LeadContractTestSave(t, root, path)
	if !strings.Contains(feedback, "Save is not approval") {
		t.Fatalf("save lost actual approval boundary: %s", feedback)
	}
	_, document, _, _ := investigationLeadSource(root, initial.State)
	finalPath, finalBytes, err := investigationLeadContractProjection(root, path, document)
	if err != nil {
		t.Fatal(err)
	}
	var final map[string]any
	_ = json.Unmarshal(finalBytes, &final)
	if final["revision"] != document["revision"] || final["root_cause_statement"] != document["primary_root_cause"] || final["case_id"] != document["case_id"] {
		t.Fatalf("system failed to carry current Case metadata/cause: %#v", final)
	}
	if stored, _ := os.ReadFile(finalPath); !bytes.Equal(stored, finalBytes) {
		t.Fatal("final review bytes were not fixed")
	}
	for _, event := range []string{"Stop", "PreCompact", "PreToolUse"} {
		context = stageInvestigationLeadContractContext(root, policy.Input{Event: event}, initial)
		if !strings.Contains(context, finalPath) || !strings.Contains(context, "actual human decision") || investigationLeadTestSnapshot(t, root).Revision != initial.Revision {
			t.Fatalf("%s invented approval or lost exact waiting draft: %s", event, context)
		}
	}
	s8LeadContractTestSave(t, root, path)
	if investigationLeadTestSnapshot(t, root).Revision != initial.Revision {
		t.Fatal("save replay wrote Runtime")
	}
	changed := s8LeadContractTestAnswer()
	changed.ArchitectureIntent = "another professionally reviewed repair boundary"
	writeRepairLeadJSON(t, path, changed)
	s8LeadContractTestSave(t, root, path)
	if stored, _ := os.ReadFile(finalPath); !bytes.Equal(stored, finalBytes) {
		t.Fatal("a later edit overwrote already fixed review bytes")
	}
}

func TestS8LeadContractMatchesRegisteredDecisionThenOriginalCASAndReplay(t *testing.T) {
	root, path := s8LeadContractTestReady(t)
	writeRepairLeadJSON(t, path, s8LeadContractTestAnswer())
	s8LeadContractTestSave(t, root, path)
	current := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, current.State)
	finalPath, finalBytes, _ := investigationLeadContractProjection(root, path, document)
	var final map[string]any
	_ = json.Unmarshal(finalBytes, &final)
	register := func(id, hash string) {
		current = investigationLeadTestSnapshot(t, root)
		rel := ".claude/decisions/" + id + ".json"
		if err := os.MkdirAll(filepath.Join(root, ".claude/decisions"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeRepairLeadJSON(t, filepath.Join(root, rel), map[string]any{"decision": "approve_contract", "decision_id": id, "runtime_id": current.State["runtime_id"], "case_id": document["case_id"], "contract_id": final["repair_contract_id"], "approval_hash": hash})
		_, err := runtime.RecordEvidence(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), runtime.EvidenceRequest{ExpectedRevision: current.Revision, ID: id, Kind: "human_decision", Path: rel, ProducedBy: []string{"fixture-human"}, ScopeRefs: []string{"s8_contract_approval:" + stringValue(current.State["runtime_id"])}, Validator: semantic.RuntimeCandidateValidator{}})
		if err != nil {
			t.Fatal(err)
		}
	}
	register("ev-wrong-draft", strings.Repeat("0", 64))
	current = investigationLeadTestSnapshot(t, root)
	stageInvestigationLeadContractContext(root, policy.Input{Event: "PreToolUse"}, current)
	if investigationLeadTestSnapshot(t, root).Revision != current.Revision {
		t.Fatal("unmatched artifact approved a different draft")
	}
	register("ev-exact-draft", workpacket.Digest(finalBytes))
	current = investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadContractContext(root, policy.Input{Event: "PreToolUse"}, current)
	approved := investigationLeadTestSnapshot(t, root)
	if stringValue(mapFieldCLI(approved.State, "lifecycle")["phase"]) != "planning" || !strings.Contains(context, "Original approval recorded") {
		t.Fatalf("matching genuine-fixture artifact did not reach original S9 entry: %s", context)
	}
	for _, staleInstruction := range []string{"Current approval responsibility", "Current designated Driver reviewer", "Assertion IDs are positional", "Fill violated_invariant"} {
		if strings.Contains(context, staleInstruction) {
			t.Fatalf("approved S9 handoff repeated finished S8 work: %s", context)
		}
	}
	for _, name := range []string{"builder-dispatch.json", "independent-verifier-dispatch.json"} {
		path, err := repairLeadQuestionnairePath(root, approved.State, name)
		if err != nil || questionnaireMissing(path) {
			t.Fatalf("approved S9 entry missing division %s: %v", path, err)
		}
	}
	if data, _ := os.ReadFile(finalPath); !bytes.Equal(data, finalBytes) {
		t.Fatal("approval changed reviewed draft bytes")
	}
	feedback := s8LeadContractTestSave(t, root, path)
	if !strings.Contains(feedback, "already recorded") || investigationLeadTestSnapshot(t, root).Revision != approved.Revision {
		t.Fatalf("approved replay was not read-only: %s", feedback)
	}
	changed := s8LeadContractTestAnswer()
	changed.ArchitectureIntent = "changed after approval"
	writeRepairLeadJSON(t, path, changed)
	if feedback := s8LeadContractTestSave(t, root, path); !strings.Contains(feedback, "conflicts") {
		t.Fatalf("approved answer edit escaped conflict: %s", feedback)
	}
}

func TestS8LeadContractCaseChangePreservesOldReviewAndReportsStale(t *testing.T) {
	root, path := s8LeadContractTestReady(t)
	writeRepairLeadJSON(t, path, s8LeadContractTestAnswer())
	s8LeadContractTestSave(t, root, path)
	current := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, current.State)
	finalPath, finalBytes, _ := investigationLeadContractProjection(root, path, document)
	_, err := investigation.UpdateCase(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), investigation.CaseRevisionRequest{ExpectedRevision: current.Revision, CaseID: stringValue(document["case_id"]), Operation: "case_revised", Mutate: func(doc map[string]any) error {
		doc["grouping_rationale"] = "additional bounded causal evidence"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if feedback := s8LeadContractTestSave(t, root, path); !strings.Contains(feedback, "stale") {
		t.Fatalf("Case change silently rebound an old review: %s", feedback)
	}
	if data, _ := os.ReadFile(finalPath); !bytes.Equal(data, finalBytes) {
		t.Fatal("Case change rewrote fixed draft")
	}
}

func s8LeadContractTestReady(t *testing.T) (string, string) {
	t.Helper()
	root, _ := investigationLeadDispositionResults(t, "supported", "refuted")
	current := investigationLeadTestSnapshot(t, root)
	_, err := investigation.UpdateCaseRoute(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), investigation.RouteRequest{ExpectedRevision: current.Revision, CaseID: stringValue(investigationPointer(current.State)["case_id"]), Route: "s9_repair", RouteReason: "mechanism fixture cause is established", PrimaryRootCause: "the first boundary drops authority", CausalModel: map[string]any{"trigger": "request", "violated_invariant": "source authority", "faulty_mechanism": "boundary discards authority", "propagation": "downstream fallback", "symptoms": []any{"finding-1"}}, BlastRadius: map[string]any{"paths": []any{"internal/example"}}, DetectionGap: map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://gap"}}})
	if err != nil {
		t.Fatal(err)
	}
	return root, investigationLeadContractPaths(root, investigationLeadTestSnapshot(t, root).State)[0]
}

func s8LeadContractTestAnswer() investigationLeadContractAnswer {
	return investigationLeadContractAnswer{ViolatedInvariant: "preserve source authority", ArchitectureIntent: "one authoritative boundary", RepairUnits: []map[string]any{{"id": "repair-unit-1", "description": "restore boundary", "scope": []string{"internal/example"}, "assertion_ids": []string{"all"}}}, ProspectiveScope: []string{"internal/example"}, ForbiddenScope: []string{"docs/requirements"}, CompatibilityMigration: "compatible; rollback the bounded change", SymptomAssertions: []string{"finding-1 resolves"}, RootInvariantAssertions: []string{"authority survives boundary"}, DetectionGapAssertions: []string{"boundary drift fails regression"}, RegressionSurfaces: []string{"internal/example"}, StopEscalationConditions: []string{"REQ semantics must change"}}
}

func s8LeadContractTestSave(t *testing.T, root, path string) string {
	t.Helper()
	var out bytes.Buffer
	if handled, code := runSavedInvestigationLeadContract(root, repairLeadSaveInput(root, path, ""), &out); !handled || code != 0 {
		t.Fatalf("Contract save not handled: %v %d %s", handled, code, &out)
	}
	return out.String()
}

func TestS8ContractGuidanceSupportsMultipleUnitsWithoutGuessing(t *testing.T) {
	root, path := s8LeadContractTestReady(t)
	current := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadContractContext(root, policy.Input{Event: "PreToolUse"}, current)
	for _, text := range []string{"starting at 1", "symptom-1, symptom-2", "root-1, root-2", "gap-1, gap-2", "must stand alone", "Every assertion must be assigned", "depends_on", "no cycles", "resource_locks", "repository-relative file or directory paths", "directory includes descendants", "glob patterns are not interpreted", "inside prospective_scope and outside forbidden_scope", "behavioral prohibitions in architecture_intent"} {
		if !strings.Contains(context, text) {
			t.Fatalf("Contract author omitted %q: %s", text, context)
		}
	}
	answer := s8LeadContractTestAnswer()
	answer.RepairUnits = []map[string]any{
		{"id": "boundary", "description": "restore boundary", "scope": []string{"internal/example"}, "assertion_ids": []string{"symptom-1", "root-1"}},
		{"id": "regression", "description": "protect original boundary", "scope": []string{"internal/example"}, "assertion_ids": []string{"gap-1"}, "depends_on": []string{"boundary"}, "resource_locks": []string{"fixture-environment"}},
	}
	writeRepairLeadJSON(t, path, answer)
	feedback := s8LeadContractTestSave(t, root, path)
	if !strings.Contains(feedback, "Save is not approval") {
		t.Fatalf("documented multi-unit contract did not save: %s", feedback)
	}
	_, doc, _, _ := investigationLeadSource(root, current.State)
	finalPath, expected, err := investigationLeadContractProjection(root, path, doc)
	stored, readErr := os.ReadFile(finalPath)
	if err != nil || readErr != nil || !bytes.Equal(stored, expected) {
		t.Fatalf("multi-unit review bytes not fixed: %v %v", err, readErr)
	}
}
