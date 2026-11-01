package workpacket

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func hypothesisResultTestQuestion() HypothesisResultQuestion {
	return HypothesisResultQuestion{
		HypothesisID: "hypothesis-cache-1", AssignmentID: "assignment-investigator-1",
		Statement:     "A stale cache entry causes the observed response.",
		Invariant:     "A fresh read must reflect the persisted value.",
		Discriminator: "Compare the same request with the cache bypassed.",
		ExpectedOutcomes: HypothesisExpectedOutcomes{
			Support: "The stale response disappears when bypassing the cache.",
			Refute:  "The same stale response remains without the cache.",
		},
		SourceFindingIDs: []string{"finding-001"},
		EvidenceRefs:     []string{"test://hypothesis-probe"},
		Status:           "open",
	}
}

func hypothesisResultTestFinding() HypothesisResultFinding {
	document := json.RawMessage(`{
        "schema_version":"1.0.0",
        "finding_id":"finding-001",
        "claim_id":"claim-cache-read",
        "lens":"qa",
        "severity":"P1",
        "expected":"A fresh read reflects the persisted value.",
        "authority_refs":["REQ-001"],
        "observed":"The response contains the previously persisted value.",
        "observation_mode":"code_inspection",
        "encounter":{
          "journey_summary":"Inspected the read path and found its first divergence.",
          "wall_action":"Read the newly persisted record.",
          "first_bad_checkpoint":"The cache returned the older value.",
          "inspection_entry":"internal/cache/read.go",
          "symbol_trail":"Read -> Lookup -> stale entry"
        },
        "reproducibility":"always",
        "evidence_refs":["test://finding-observation"]
      }`)
	return HypothesisResultFinding{
		FindingID: "finding-001",
		Ref:       ".claude/evidence/loop-REQ-001/g1/findings/finding-001.json",

		Document: document,
	}
}

func hypothesisResultTestRoute() HypothesisResultCaseRoute {
	return HypothesisResultCaseRoute{Status: "investigating"}
}

func hypothesisResultTestInputs() HypothesisResultInputs {
	return HypothesisResultInputs{
		Hypothesis: hypothesisResultTestQuestion(),
		CaseRoute:  hypothesisResultTestRoute(),
		Findings:   []HypothesisResultFinding{hypothesisResultTestFinding()},
		EvidenceRefs: []HypothesisEvidenceRef{
			{Ref: "test://hypothesis-probe", Origin: "hypothesis", Attestation: "execution_anchor"},
			{Ref: "test://finding-observation", Origin: "finding", SourceFindingID: "finding-001", Attestation: "execution_anchor"},
		},
	}
}

func hypothesisResultTestBinding(t *testing.T) HypothesisResultBinding {
	t.Helper()
	question := hypothesisResultTestQuestion()
	return HypothesisResultBinding{
		RuntimeID: "loop-REQ-001", BaselineGeneration: 1,
		CaseID: "investigation-case-001", CaseRef: ".claude/review/investigation/cases/investigation-case-001.json",
		CaseRevision: 2,

		SourceFindingIDs: []string{"finding-001"}, HypothesisID: question.HypothesisID,
		HypothesisSourceFindingIDs: []string{"finding-001"},
		AssignmentID:               question.AssignmentID,
		TeamID:                     "workgroup-s8-1", TaskID: "TASK-001-01", TaskRef: "docs/tasks/TASK-001-01.md",
		ManifestRef: ".claude/workgroups/workgroup-s8-1.json", OwnerAgentID: "investigator-01",
	}
}

func TestBuildHypothesisResultPacketIsStableAndSchemasCompile(t *testing.T) {
	binding := hypothesisResultTestBinding(t)
	inputs := hypothesisResultTestInputs()
	p, err := BuildHypothesisResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildHypothesisResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(p)
	second, _ := json.Marshal(again)
	if string(first) != string(second) {
		t.Fatal("same S8 source closure must produce a stable packet")
	}
	if p.TemplateID != "s8-hypothesis-result-v1" || p.ResultID != "hypothesis-result-"+p.ID || p.IssuanceEventID != "evt-hypothesis-result-packet-"+p.ID {
		t.Fatalf("S8 packet/result/event identity mismatch: %+v", p)
	}
	if p.ViewPath == "" || p.AnswerPath == "" || !strings.HasSuffix(p.AnswerPath, "/investigator-01/answer.json") {
		t.Fatalf("S8 paths do not isolate the current owner: view=%s answer=%s", p.ViewPath, p.AnswerPath)
	}
	for name, value := range map[string]any{"hypothesis-result-work-packet.schema.json": p, "stage-answer-s8.schema.json": p.Draft} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes(name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	pretty, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var restored HypothesisResultPacket
	if err := json.Unmarshal(pretty, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, restored) {
		t.Fatal("packet JSON round-trip changed canonical Finding/evidence source bytes")
	}

	otherInputs := hypothesisResultTestInputs()
	otherInputs.Findings[0].Document = json.RawMessage(`{"evidence_refs":["test://finding-observation"],"reproducibility":"always","encounter":{"symbol_trail":"Read -> Lookup -> stale entry","inspection_entry":"internal/cache/read.go","first_bad_checkpoint":"The cache returned the older value.","wall_action":"Read the newly persisted record.","journey_summary":"Inspected the read path and found its first divergence."},"observation_mode":"code_inspection","observed":"The response contains the previously persisted value.","authority_refs":["REQ-001"],"expected":"A fresh read reflects the persisted value.","severity":"P1","lens":"qa","claim_id":"claim-cache-read","finding_id":"finding-001","schema_version":"1.0.0"}`)
	other, err := BuildHypothesisResult(binding, otherInputs)
	if err != nil || other.ID != p.ID {
		t.Fatalf("Finding document key order must not change packet identity: id=%s err=%v", other.ID, err)
	}
}

func TestValidateHypothesisResultAnswerReturnsOnlyCompleteProfessionalFields(t *testing.T) {
	p, err := BuildHypothesisResult(hypothesisResultTestBinding(t), hypothesisResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	draftBytes, err := json.Marshal(p.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, fields := ValidateHypothesisResultAnswer(p, draftBytes); len(fields) == 0 || fields[0].Pointer != "/answer/result" {
		t.Fatalf("empty draft should identify the first missing professional field: %#v", fields)
	}
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	answer := draft["answer"].(map[string]any)
	answer["method"] = "Bypassing the cache isolated the stale response."
	answer["evidence_refs"] = []any{"test://hypothesis-probe", "test://fresh-read"}
	answer["source_boundary_refs"] = []any{"finding-001.encounter.first_bad_checkpoint"}
	answer["observed"] = "The cache bypass returned the current persisted value."
	answer["counterfactual_or_discriminator"] = "A stale value without cache bypass would refute this hypothesis."
	answer["result"] = "supported"
	answer["explains_finding_ids"] = []any{"finding-001"}
	answer["does_not_explain"] = []any{}
	answer["new_hypotheses"] = []any{map[string]any{
		"statement":          "An invalidation race may independently stale the response.",
		"invariant":          "Every invalidation must precede the next read.",
		"discriminator":      "Compare invalidation and read sequence identifiers.",
		"expected_outcomes":  map[string]any{"support": "Read precedes invalidation.", "refute": "Invalidation precedes read."},
		"source_finding_ids": []any{"finding-001"}, "evidence_refs": []any{"test://invalidation-race"},
	}}
	complete, _ := json.Marshal(draft)
	got, fields := ValidateHypothesisResultAnswer(p, complete)
	if len(fields) != 0 {
		t.Fatalf("complete S8 professional answer rejected: %#v", fields)
	}
	var projection map[string]any
	if err := json.Unmarshal(got, &projection); err != nil {
		t.Fatal(err)
	}
	if len(projection) != 9 || projection["result"] != "supported" {
		t.Fatalf("validator did not return exactly nine professional fields: %#v", projection)
	}
	for _, forbidden := range []string{"hypothesis_id", "assignment_id", "status", "route", "causal_model", "repair_contract", "approved_by"} {
		if _, exists := projection[forbidden]; exists {
			t.Fatalf("machine or approval field %q escaped into the professional projection", forbidden)
		}
	}
	newHypothesis := projection["new_hypotheses"].([]any)[0].(map[string]any)
	if len(newHypothesis) != 6 {
		t.Fatalf("new Hypothesis must contain only authored question/source fields: %#v", newHypothesis)
	}

	for name, mutate := range map[string]func(map[string]any){
		"unknown professional field": func(answer map[string]any) { answer["root_cause"] = "cache" },
		"null result":                func(answer map[string]any) { answer["result"] = nil },
		"new hypothesis ID": func(answer map[string]any) {
			answer["new_hypotheses"].([]any)[0].(map[string]any)["hypothesis_id"] = "hypothesis-authored"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			_ = json.Unmarshal(complete, &changed)
			mutate(changed["answer"].(map[string]any))
			data, _ := json.Marshal(changed)
			if _, fields := ValidateHypothesisResultAnswer(p, data); len(fields) == 0 {
				t.Fatal("invalid S8 answer was accepted")
			}
		})
	}
	var additional map[string]any
	_ = json.Unmarshal(complete, &additional)
	additional["answer"].(map[string]any)["explains_finding_ids"] = []any{"finding-outside"}
	additionalData, _ := json.Marshal(additional)
	if _, fields := ValidateHypothesisResultAnswer(p, additionalData); len(fields) != 0 {
		t.Fatalf("professional finding observation became an extra source gate: %#v", fields)
	}
	duplicate := strings.Replace(string(complete), `"result":"supported"`, `"result":"supported","result":"refuted"`, 1)
	if duplicate == string(complete) {
		t.Fatal("test fixture did not locate serialized result field")
	}
	duplicateAnswer, fields := ValidateHypothesisResultAnswer(p, []byte(duplicate))
	if len(fields) != 0 {
		t.Fatalf("current JSON reader could not read the saved answer: %#v", fields)
	}
	var duplicateProjection map[string]any
	if err := json.Unmarshal(duplicateAnswer, &duplicateProjection); err != nil || duplicateProjection["result"] != "refuted" {
		t.Fatalf("JSON reader did not preserve its actual result: %s %v", duplicateAnswer, err)
	}

	var editedQuestion map[string]any
	_ = json.Unmarshal(complete, &editedQuestion)
	editedQuestion["questions"].(map[string]any)["method"].(map[string]any)["question"] = "Edited machine question"
	edited, _ := json.Marshal(editedQuestion)
	if _, fields := ValidateHypothesisResultAnswer(p, edited); len(fields) == 0 || fields[0].Pointer != "/" {
		t.Fatalf("edited machine question was accepted: %#v", fields)
	}
}

func hypothesisStringPtr(value string) *string { return &value }

func TestHypothesisFollowupSuggestionsDoNotRequireAdditionalSourceProof(t *testing.T) {
	for _, human := range []bool{false, true} {
		t.Run(map[bool]string{false: "batch", true: "human"}[human], func(t *testing.T) {
			binding, inputs := hypothesisResultTestBinding(t), hypothesisResultTestInputs()
			if human {
				binding.SourceFindingIDs, binding.HypothesisSourceFindingIDs = []string{}, []string{}
				inputs.Hypothesis.SourceFindingIDs = []string{}
				inputs.Findings = []HypothesisResultFinding{}
				inputs.EvidenceRefs = inputs.EvidenceRefs[:1]
			}
			p, err := BuildHypothesisResult(binding, inputs)
			if err != nil {
				t.Fatal(err)
			}
			result := "inconclusive"
			answer := HypothesisResultAnswer{Method: "compare the input boundary", EvidenceRefs: []string{"test://observation"}, SourceBoundaryRefs: []string{"test://boundary"}, Observed: "the discriminator requires a second observation", CounterfactualOrDiscriminator: "compare the next layer", Result: &result, ExplainsFindingIDs: []string{}, DoesNotExplain: []string{}, NewHypotheses: []HypothesisNewQuestion{{Statement: "the next boundary loses the value", Invariant: "values survive the next boundary", Discriminator: "compare before and after the next layer", ExpectedOutcomes: HypothesisExpectedOutcomes{Support: "value lost", Refute: "value retained"}, SourceFindingIDs: []string{}, EvidenceRefs: []string{"test://next-boundary"}}}}
			draft := p.Draft
			draft.Answer, _ = json.Marshal(answer)
			data, _ := json.Marshal(draft)
			_, fields := ValidateHypothesisResultAnswer(p, data)
			if human && len(fields) != 0 {
				t.Fatalf("human follow-up demanded a fake Finding: %+v", fields)
			}
			if !human && len(fields) != 0 {
				t.Fatalf("suggested follow-up was incorrectly made a reception proof: %+v", fields)
			}
		})
	}
}

func TestS8QuestionPreservesOrdinaryReferenceWithoutRegistration(t *testing.T) {
	binding := hypothesisResultTestBinding(t)
	inputs := hypothesisResultTestInputs()
	inputs.Hypothesis.EvidenceRefs = []string{"notes/actual-observation.txt"}
	inputs.EvidenceRefs = []HypothesisEvidenceRef{
		{Ref: "notes/actual-observation.txt", Origin: "hypothesis", Attestation: "reference"},
		{Ref: "test://finding-observation", Origin: "finding", SourceFindingID: "finding-001", Attestation: "reference"},
	}
	packet, err := BuildHypothesisResult(binding, inputs)
	if err != nil {
		t.Fatalf("ordinary real reference became an issuance gate: %v", err)
	}
	if packet.Inputs.Hypothesis.EvidenceRefs[0] != "notes/actual-observation.txt" {
		t.Fatal("reference rewritten")
	}
}
