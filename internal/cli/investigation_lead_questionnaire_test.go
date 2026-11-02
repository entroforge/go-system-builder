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
)

// Professional answers below are fixture data, not model diagnosis evidence.
func TestS8InvestigationLeadObserverSkipsUntouchedDrafts(t *testing.T) {
	for _, tc := range []struct {
		path, data string
		want       bool
	}{
		{"intake.json", `{"grouping_rationale":""}`, false},
		{"intake.json", `{"grouping_rationale":"one boundary"}`, true},
		{"hypotheses.json", `[]`, false},
		{"hypotheses.json", `[{"hypothesis_id":"machine-id","agent_id":"selected-owner","expected_outcomes":{"support":"","refute":""},"source_finding_ids":[],"evidence_refs":[]}]`, false},
		{"hypotheses.json", `[{"discriminator":"trace the first violating boundary"}]`, true},
		{"hypotheses.json", `[{"evidence_refs":["evidence://one"]}]`, true},
		{"hypotheses.json", `[{`, true},
		{"intake.json", `{"unknown":"authored"}`, true},
	} {
		if got := investigationLeadQuestionnaireHasProfessionalContent(tc.path, []byte(tc.data)); got != tc.want {
			t.Errorf("observer %s %s = %v, want %v", tc.path, tc.data, got, tc.want)
		}
	}
}

func TestS8InvestigationLeadPreparationReadOnlyAndDraftPreserved(t *testing.T) {
	root := investigationLeadTestRoot(t)
	current := investigationLeadTestSnapshot(t, root)
	paths := investigationLeadQuestionnairePaths(root, current.State)
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "intake.json") {
		t.Fatalf("intake path unavailable: %#v", paths)
	}
	for _, event := range []string{"Stop"} {
		context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: event}, current)
		if !strings.Contains(context, paths[0]) || !strings.Contains(context, "Not prepared yet") || !strings.Contains(context, "read-only") {
			t.Fatalf("%s did not disclose normal preparation and exact path: %s", event, context)
		}
		if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
			t.Fatalf("%s prepared a questionnaire: %v", event, err)
		}
	}
	for _, event := range []string{"Idle"} {
		if got := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: event}, current); got != "" {
			t.Fatalf("%s emitted an unsupported questionnaire reminder: %s", event, got)
		}
		if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
			t.Fatalf("%s prepared a questionnaire: %v", event, err)
		}
	}
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreCompact"}, current)
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("read-only PreCompact projection prepared a questionnaire: %v", err)
	}
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	if !strings.Contains(context, paths[0]) || !strings.Contains(context, ".claude/evidence/findings/finding-1.json") || !strings.Contains(context, "grouping_rationale") {
		t.Fatalf("missing question/source paths: %s", context)
	}
	writeRepairLeadJSON(t, paths[0], investigationLeadIntakeAnswer{GroupingRationale: "author draft"})
	before, _ := os.ReadFile(paths[0])
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	after, _ := os.ReadFile(paths[0])
	if !bytes.Equal(before, after) || investigationLeadTestSnapshot(t, root).Revision != current.Revision {
		t.Fatal("preparation overwrote author draft or ingested the Batch")
	}
}

// An Investigator's `new_hypotheses` entry is only a suggestion (L3-S8:162,
// :269, :479): Main decides whether to adopt it. The Main questionnaire keeps
// the author's own rows, and only a row Main saves in its own array becomes a
// registered, dispatched responsibility.
func TestS8InvestigationLeadWorkerQuestionsMergeWithoutReplacingAuthorRows(t *testing.T) {
	root := investigationLeadTestRoot(t)
	_, path := investigationLeadTestIngest(t, root)
	q := investigationLeadTestQuestion("the original registered mechanism")
	writeRepairLeadJSON(t, path, []investigationLeadQuestion{q})
	investigationLeadTestSave(t, root, path)
	var authored []investigationLeadQuestion
	readRepairLeadJSON(t, path, &authored)
	current := investigationLeadTestSnapshot(t, root)
	workerQuestion := map[string]any{"hypothesis_id": "hypothesis-worker-question", "assignment_id": "assignment-worker-question", "statement": "an independent alias may cause the symptom", "invariant": "one authority", "discriminator": "inspect the alias boundary", "expected_outcomes": map[string]any{"support": "alias changes interpretation", "refute": "alias preserves interpretation"}, "source_finding_ids": []any{"finding-1"}, "evidence_refs": []any{"evidence://worker-question"}}
	_, err := investigation.SubmitHypothesisResult(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), investigation.HypothesisResultRequest{ExpectedRevision: current.Revision, CaseID: stringValue(investigationPointer(current.State)["case_id"]), HypothesisID: authored[0].HypothesisID, AssignmentID: "assignment-" + authored[0].HypothesisID, Method: "inspect the source", EvidenceRefs: []string{"evidence://worker-result"}, SourceBoundaryRefs: []string{"finding:finding-1"}, Observed: "one alternative remains", Counterfactual: "distinguish alias behavior", Result: "inconclusive", NewHypotheses: []map[string]any{workerQuestion}})
	if err != nil {
		t.Fatal(err)
	}
	current = investigationLeadTestSnapshot(t, root)
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil {
		t.Fatal(err)
	}
	// The Worker's suggestion is preserved as a suggestion: it does not become
	// a registered responsibility of the Case.
	if len(objectSliceAny(document["hypotheses"])) != 1 {
		t.Fatalf("worker suggestion became a registered responsibility: %#v", document["hypotheses"])
	}
	for _, row := range objectSliceAny(document["hypotheses"]) {
		if stringValue(row["hypothesis_id"]) == "hypothesis-worker-question" {
			t.Fatalf("worker suggestion was registered as a Hypothesis: %#v", row)
		}
	}
	if rows := objectSliceAny(document["hypothesis_results"]); len(rows) != 1 || stringValue(rows[0]["hypothesis_id"]) != authored[0].HypothesisID {
		t.Fatalf("worker result was not attached to the original question: %#v", rows)
	}
	// The author's edited original row survives preparation, and the suggestion
	// is not merged into Main's array.
	authored[0].Statement = "an author edit that preparation must preserve"
	writeRepairLeadJSON(t, path, authored)
	current = investigationLeadTestSnapshot(t, root)
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	readRepairLeadJSON(t, path, &authored)
	if len(authored) != 1 || authored[0].Statement != "an author edit that preparation must preserve" {
		t.Fatalf("author row was replaced or the worker suggestion was merged into the author array: %#v", authored)
	}
	for _, row := range authored {
		if row.HypothesisID == "hypothesis-worker-question" || row.Statement == "an independent alias may cause the symptom" {
			t.Fatalf("worker suggestion entered the author array: %#v", row)
		}
	}
	// Main's own array without the suggestion registers and dispatches nothing
	// new; the author's own question keeps its original dispatch.
	authored[0].Statement = q.Statement
	writeRepairLeadJSON(t, path, authored)
	feedback := investigationLeadTestSave(t, root, path)
	if got := investigationLeadTestSnapshot(t, root).Revision; got != current.Revision {
		t.Fatalf("worker suggestion was registered or dispatched without Main's own row: revision %d -> %d, feedback=%s", current.Revision, got, feedback)
	}
	if strings.Contains(feedback, "owner=investigator-worker-question") {
		t.Fatalf("worker suggestion was dispatched without Main's own row: %s", feedback)
	}
	if owner := "owner=investigator-" + strings.TrimPrefix(authored[0].HypothesisID, "hypothesis-"); !strings.Contains(feedback, owner) {
		t.Fatalf("the author's own question lost its original dispatch: %s", feedback)
	}
	_, document, _, _ = investigationLeadSource(root, investigationLeadTestSnapshot(t, root).State)
	if len(objectSliceAny(document["hypotheses"])) != 1 {
		t.Fatalf("worker suggestion became a registered responsibility: %#v", document["hypotheses"])
	}
	// Main adopts the suggestion as its own row: only then is it registered and
	// dispatched to its own Investigator.
	authored = append(authored, investigationLeadQuestion{HypothesisID: "hypothesis-worker-question", Statement: "an independent alias may cause the symptom", Invariant: "one authority", Discriminator: "inspect the alias boundary", ExpectedOutcomes: map[string]string{"support": "alias changes interpretation", "refute": "alias preserves interpretation"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"evidence://worker-question"}})
	writeRepairLeadJSON(t, path, authored)
	feedback = investigationLeadTestSave(t, root, path)
	if !strings.Contains(feedback, "owner=investigator-worker-question") || !strings.Contains(feedback, "TASK=") {
		t.Fatalf("Main's own adopted row was not registered and dispatched: %s", feedback)
	}
	adopted := investigationLeadTestSnapshot(t, root)
	_, document, _, _ = investigationLeadSource(root, adopted.State)
	registered := objectSliceAny(document["hypotheses"])
	if len(registered) != 2 {
		t.Fatalf("Main's own adopted row was not registered: %#v", registered)
	}
	adoptedRow := false
	for _, row := range registered {
		if stringValue(row["hypothesis_id"]) == "hypothesis-worker-question" {
			adoptedRow = true
		}
	}
	if !adoptedRow {
		t.Fatalf("Main's own adopted row was not registered: %#v", registered)
	}
	if adopted.Revision != current.Revision+2 {
		t.Fatalf("the adopted row did not produce one registration and one dispatch revision: %d -> %d, feedback=%s", current.Revision, adopted.Revision, feedback)
	}
}

func TestS8InvestigationLeadSaveOriginalConsumersReplayConflictAndReorder(t *testing.T) {
	root := investigationLeadTestRoot(t)
	intake, questionsPath := investigationLeadTestIngest(t, root)
	before := investigationLeadTestSnapshot(t, root)
	feedback := investigationLeadTestSave(t, root, intake)
	if !strings.Contains(feedback, "already received") || investigationLeadTestSnapshot(t, root).Revision != before.Revision {
		t.Fatalf("intake replay changed Runtime: %s", feedback)
	}
	writeRepairLeadJSON(t, intake, investigationLeadIntakeAnswer{GroupingRationale: "different grouping"})
	if feedback := investigationLeadTestSave(t, root, intake); !strings.Contains(feedback, "conflicts") {
		t.Fatalf("grouping edit was silently accepted: %s", feedback)
	}
	questions := []investigationLeadQuestion{investigationLeadTestQuestion("the boundary discards the error"), investigationLeadTestQuestion("the cache returns an older value")}
	questions[1].AgentID = "investigator-selected"
	writeRepairLeadJSON(t, questionsPath, questions)
	feedback = investigationLeadTestSave(t, root, questionsPath)
	if !strings.Contains(feedback, "TASK=") || !strings.Contains(feedback, "manifest=") || !strings.Contains(feedback, "platform to start/resume") || !strings.Contains(feedback, "owner=investigator-selected") {
		t.Fatalf("save did not expose real dispatch and Main platform responsibility: %s", feedback)
	}
	current := investigationLeadTestSnapshot(t, root)
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil || len(objectSliceAny(document["hypotheses"])) != 2 || len(objectSliceAny(mapFieldCLI(current.State, "entities")["teams"])) != 2 {
		t.Fatalf("original Case/dispatch not committed: document=%#v err=%v feedback=%s", document, err, feedback)
	}
	readRepairLeadJSON(t, questionsPath, &questions)
	if questions[0].HypothesisID == "" || questions[1].HypothesisID == "" {
		t.Fatal("system did not retain original question references")
	}
	stateBefore, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journalBefore, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	answerBefore, _ := os.ReadFile(questionsPath)
	feedback = investigationLeadTestSave(t, root, questionsPath)
	answerAfter, _ := os.ReadFile(questionsPath)
	if !bytes.Equal(answerBefore, answerAfter) {
		t.Fatal("unchanged save rewrote the answer view")
	}
	questions[0], questions[1] = questions[1], questions[0]
	questions[1].HypothesisID = "" // Original professional content also resolves its existing ID.
	writeRepairLeadJSON(t, questionsPath, questions)
	feedback = investigationLeadTestSave(t, root, questionsPath)
	stateAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	journalAfter, _ := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatalf("same/reordered questions created new domain writes: %s", feedback)
	}
	readRepairLeadJSON(t, questionsPath, &questions)
	questions[0].Statement = "edit of an accepted statement"
	writeRepairLeadJSON(t, questionsPath, questions)
	feedback = investigationLeadTestSave(t, root, questionsPath)
	if !strings.Contains(feedback, "conflicts with registered Hypothesis") {
		t.Fatalf("changed registered question was silently overwritten: %s", feedback)
	}
	if investigationLeadTestSnapshot(t, root).Revision != current.Revision {
		t.Fatal("conflicting answer changed Runtime")
	}
}

func TestS8InvestigationLeadPartialDispatchRecovery(t *testing.T) {
	root := investigationLeadTestRoot(t)
	_, path := investigationLeadTestIngest(t, root)
	definition := filepath.Join(root, ".claude/agents/investigator.md")
	definitionBytes, err := os.ReadFile(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(definition); err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, path, []investigationLeadQuestion{investigationLeadTestQuestion("a source alias changes interpretation")})
	feedback := investigationLeadTestSave(t, root, path)
	if !strings.Contains(feedback, "is registered; its original dispatch is still outstanding") {
		t.Fatalf("partial commit did not identify the remaining original consumer: %s", feedback)
	}
	partial := investigationLeadTestSnapshot(t, root)
	_, document, _, _ := investigationLeadSource(root, partial.State)
	if len(objectSliceAny(document["hypotheses"])) != 1 {
		t.Fatal("fixture did not reach Hypothesis-committed/dispatch-missing boundary")
	}
	if err := os.WriteFile(definition, definitionBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	feedback = investigationLeadTestSave(t, root, path)
	completed := investigationLeadTestSnapshot(t, root)
	_, document, _, _ = investigationLeadSource(root, completed.State)
	if len(objectSliceAny(document["hypotheses"])) != 1 || completed.Revision != partial.Revision+1 || !strings.Contains(feedback, "TASK=") {
		t.Fatalf("retry did not only finish original dispatch: partial=%d complete=%d feedback=%s", partial.Revision, completed.Revision, feedback)
	}
}

func investigationLeadTestQuestion(statement string) investigationLeadQuestion {
	return investigationLeadQuestion{Statement: statement, Invariant: "source authority is preserved", Discriminator: "inspect the first boundary; stop if evidence cannot distinguish the mechanisms", ExpectedOutcomes: map[string]string{"support": "the first boundary loses authority", "refute": "authority survives the boundary"}, SourceFindingIDs: []string{"finding-1"}, EvidenceRefs: []string{"evidence://finding-1"}}
}

func investigationLeadTestSave(t *testing.T, root, path string) string {
	t.Helper()
	var out, diagnostic bytes.Buffer
	handled, code := runSavedInvestigationLeadQuestionnaire(root, repairLeadSaveInput(root, path, ""), &out, &diagnostic)
	if !handled || code != 0 {
		t.Fatalf("save not handled: handled=%v code=%d out=%s diagnostic=%s", handled, code, &out, &diagnostic)
	}
	return out.String()
}

func investigationLeadTestIngest(t *testing.T, root string) (string, string) {
	t.Helper()
	current := investigationLeadTestSnapshot(t, root)
	path := investigationLeadQuestionnairePaths(root, current.State)[0]
	stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	writeRepairLeadJSON(t, path, investigationLeadIntakeAnswer{GroupingRationale: "the sealed symptoms are one provisional causal group"})
	feedback := investigationLeadTestSave(t, root, path)
	current = investigationLeadTestSnapshot(t, root)
	next := investigationLeadQuestionnairePaths(root, current.State)
	if investigationPointer(current.State) == nil || len(next) == 0 || !strings.Contains(feedback, next[0]) {
		t.Fatalf("intake did not prepare the next exact Main path: %s", feedback)
	}
	if _, err := os.Stat(next[0]); err != nil {
		t.Fatalf("next hypothesis questionnaire is missing: %v", err)
	}
	return path, next[0]
}

func investigationLeadTestSnapshot(t *testing.T, root string) runtime.Snapshot {
	t.Helper()
	current, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func investigationLeadTestRoot(t *testing.T) string {
	t.Helper()
	root := repairLeadFreshRoot(t)
	state := repairLeadBaseState(t, root, "bug_resolution", "investigation", 0)
	finding := map[string]any{"schema_version": "1.0.0", "finding_id": "finding-1", "claim_id": "claim-intake-1", "lens": "qa", "severity": "P1", "expected": "the invariant holds", "authority_refs": []string{"REQ-039"}, "observed": "the boundary loses authority", "observation_mode": "code_inspection", "encounter": map[string]any{"journey_summary": "inspect the first violating boundary", "inspection_entry": "internal/example", "symbol_trail": "entry -> boundary", "wall_action": "inspect", "first_bad_checkpoint": "authority is lost", "terminal_state": "review stopped"}, "reproducibility": "always", "evidence_refs": []string{"evidence://finding-1"}}
	findingData, _ := json.MarshalIndent(finding, "", "  ")
	findingPath := ".claude/evidence/findings/finding-1.json"
	writeRepairLeadFile(t, root, findingPath, string(findingData))
	mapFieldCLI(state, "entities")["findings"] = []any{map[string]any{"finding_id": "finding-1", "path": findingPath, "claim_id": "claim-intake-1", "assignment_id": "assignment-intake", "lens": "qa", "severity": "P1", "observation_mode": "code_inspection", "original_finder": "agent-intake", "review_plan_id": "review-plan-r1", "created_at": "2026-10-01T00:00:00Z"}}
	mapFieldCLI(state, "review")["round_entry"] = map[string]any{"transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil, "plan_id": "review-plan-r1"}
	writeRepairLeadFile(t, root, ".claude/agents/investigator.md", "# Investigator\n")
	writeRepairLeadFile(t, root, "docs/control/agent-protocol.md", "# Original Agent protocol\n")
	repairLeadWriteState(t, root, state)
	return root
}

func TestS8InvestigationLeadEvidenceReferencesExposeOriginalIDsAndPaths(t *testing.T) {
	root := investigationLeadTestRoot(t)
	_, questionsPath := investigationLeadTestIngest(t, root)
	current := investigationLeadTestSnapshot(t, root)
	generation := tolerantInt(mapFieldCLI(current.State, "baseline")["generation"])
	round := tolerantInt(mapFieldCLI(current.State, "review")["round"])
	current.State["evidence"] = []any{
		map[string]any{"id": "EVID-S8-CURRENT", "path": ".claude/evidence/findings/finding-1.json", "status": "valid", "baseline_generation": generation, "review_round": round, "invalidated_by": nil},
		map[string]any{"id": "EVID-S8-OLD-GENERATION", "path": "old-generation.json", "status": "valid", "baseline_generation": generation - 1, "review_round": round},
		map[string]any{"id": "EVID-S8-OLD-ROUND", "path": "old-round.json", "status": "valid", "baseline_generation": generation, "review_round": round - 1},
		map[string]any{"id": "EVID-S8-INVALID", "path": "invalid.json", "status": "invalid", "baseline_generation": generation, "review_round": round},
		map[string]any{"id": "EVID-S8-INVALIDATED", "path": "invalidated.json", "status": "valid", "baseline_generation": generation, "review_round": round, "invalidated_by": "original-invalidation"},
	}
	before, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	answerBefore, _ := os.ReadFile(questionsPath)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "Stop"}, current)
	for _, expected := range []string{questionsPath, "EVID-S8-CURRENT -> " + filepath.Join(root, ".claude/evidence/findings/finding-1.json"), `evidence_refs=["EVID-S8-CURRENT"]`, "Both reference fields are string arrays"} {
		if !strings.Contains(context, expected) {
			t.Fatalf("Main authoring context omitted %q: %s", expected, context)
		}
	}
	for _, excluded := range []string{"EVID-S8-OLD-GENERATION", "EVID-S8-INVALID", "EVID-S8-INVALIDATED"} {
		if strings.Contains(context, excluded) {
			t.Fatalf("inapplicable original evidence advertised as a usable reference: %s", excluded)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	answerAfter, _ := os.ReadFile(questionsPath)
	if !bytes.Equal(before, after) || !bytes.Equal(answerBefore, answerAfter) {
		t.Fatal("reference display wrote Runtime or professional answer")
	}
	current.State["evidence"] = []any{}
	context = investigationLeadEvidenceReferenceContext(root, current.State)
	if !strings.Contains(context, "No valid current-generation evidence IDs") || !strings.Contains(context, "does not invalidate the supplied frozen sources") {
		t.Fatalf("absence of an index entry incorrectly invalidated supplied original evidence: %s", context)
	}
}

func TestS8FirstQuestionReusesDeliveredFrozenFindingReference(t *testing.T) {
	root := investigationLeadTestRoot(t)
	_, path := investigationLeadTestIngest(t, root)
	current := investigationLeadTestSnapshot(t, root)
	context := stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, current)
	if !strings.Contains(context, `Selected Finding finding-1 frozen evidence_refs=["evidence://finding-1"]`) || !strings.Contains(context, filepath.Join(root, ".claude/evidence/findings/finding-1.json")) {
		t.Fatalf("first question omitted reusable original source: %s", context)
	}
	question := investigationLeadTestQuestion("the original frozen boundary explains the symptom")
	question.EvidenceRefs = []string{"evidence://finding-1"}
	writeRepairLeadJSON(t, path, []investigationLeadQuestion{question})
	feedback := investigationLeadTestSave(t, root, path)
	current = investigationLeadTestSnapshot(t, root)
	_, document, _, err := investigationLeadSource(root, current.State)
	if err != nil || len(objectSliceAny(document["hypotheses"])) != 1 {
		t.Fatalf("delivered frozen reference could not register first question: %v %s", err, feedback)
	}
	registered := objectSliceAny(document["hypotheses"])[0]
	if refs := stringSliceAny(registered["evidence_refs"]); len(refs) != 1 || refs[0] != "evidence://finding-1" {
		t.Fatalf("original reference changed: %#v", registered)
	}
}
