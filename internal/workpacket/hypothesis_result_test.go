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
		SHA256:    strings.Repeat("d", 64),
		Document:  document,
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
	questionDigest, err := HypothesisQuestionDigest(question)
	if err != nil {
		t.Fatal(err)
	}
	routeDigest, err := HypothesisCaseRouteDigest(hypothesisResultTestRoute())
	if err != nil {
		t.Fatal(err)
	}
	return HypothesisResultBinding{
		RuntimeID: "loop-REQ-001", BaselineGeneration: 1, ReviewRound: 1,
		CaseID: "investigation-case-001", CaseRef: ".claude/review/investigation/cases/investigation-case-001.json",
		CaseSHA256: strings.Repeat("a", 64), CaseRevision: 2, CaseRouteSHA256: routeDigest,
		ObservationBatchID: "observation-batch-001", ObservationBatchRef: ".claude/evidence/loop-REQ-001/g1/review/observation-batch-001.json",
		ObservationBatchSHA256: strings.Repeat("b", 64), BaselineDigest: strings.Repeat("c", 64),
		SourceFindingIDs: []string{"finding-001"}, HypothesisID: question.HypothesisID,
		HypothesisQuestionSHA256: questionDigest, HypothesisSourceFindingIDs: []string{"finding-001"},
		AssignmentID: question.AssignmentID, AssignmentSHA256: strings.Repeat("e", 64),
		TeamID: "workgroup-s8-1", TaskID: "TASK-001-01", TaskRef: "docs/tasks/TASK-001-01.md", TaskSHA256: strings.Repeat("f", 64),
		ManifestRef: ".claude/workgroups/workgroup-s8-1.json", ManifestSHA256: strings.Repeat("1", 64), OwnerAgentID: "investigator-01",
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
	if p.TemplateID != "s8-hypothesis-result-v1" || p.ResultID != "hypothesis-result-"+strings.TrimPrefix(p.ID, "packet-") || p.IssuanceEventID != "evt-hypothesis-result-packet-"+p.ID {
		t.Fatalf("S8 packet/result/event identity mismatch: %+v", p)
	}
	if !strings.Contains(p.ViewPath, "/S8/r1/") || !strings.Contains(p.AnswerPath, "/S8/r1/") || !strings.HasSuffix(p.AnswerPath, "/investigator-01/answer.json") {
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
	digest, err := HypothesisResultPacketDigest(p)
	if err != nil || !digestPattern.MatchString(digest) {
		t.Fatalf("packet digest = %q, err=%v", digest, err)
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
	restoredDigest, err := HypothesisResultPacketDigest(restored)
	if err != nil || restoredDigest != digest {
		t.Fatalf("restored packet digest = %q, want %q; err=%v", restoredDigest, digest, err)
	}

	otherInputs := hypothesisResultTestInputs()
	otherInputs.Findings[0].Document = json.RawMessage(`{"evidence_refs":["test://finding-observation"],"reproducibility":"always","encounter":{"symbol_trail":"Read -> Lookup -> stale entry","inspection_entry":"internal/cache/read.go","first_bad_checkpoint":"The cache returned the older value.","wall_action":"Read the newly persisted record.","journey_summary":"Inspected the read path and found its first divergence."},"observation_mode":"code_inspection","observed":"The response contains the previously persisted value.","authority_refs":["REQ-001"],"expected":"A fresh read reflects the persisted value.","severity":"P1","lens":"qa","claim_id":"claim-cache-read","finding_id":"finding-001","schema_version":"1.0.0"}`)
	other, err := BuildHypothesisResult(binding, otherInputs)
	if err != nil || other.ID != p.ID {
		t.Fatalf("Finding document key order must not change packet identity: id=%s err=%v", other.ID, err)
	}
}

func TestHypothesisResultRouteAndEvidenceClosuresFailClosed(t *testing.T) {
	binding := hypothesisResultTestBinding(t)
	inputs := hypothesisResultTestInputs()
	if _, err := BuildHypothesisResult(binding, inputs); err != nil {
		t.Fatalf("valid execution-anchor-only evidence should build: %v", err)
	}

	indexed := hypothesisResultTestInputs()
	baseline, round := 1, 1
	indexed.EvidenceRefs[0] = HypothesisEvidenceRef{
		Ref: "evidence/hypothesis-probe.json", Origin: "hypothesis", Attestation: "runtime_index",
		Kind: "finding", Path: ".claude/evidence/loop-REQ-001/g1/hypothesis-probe.json", SHA256: strings.Repeat("2", 64),
		Status: "valid", BaselineGeneration: &baseline, ReviewRound: &round, InvalidatedBy: nil,
	}
	indexed.Hypothesis.EvidenceRefs = []string{"evidence/hypothesis-probe.json"}
	updatedQuestionDigest, _ := HypothesisQuestionDigest(indexed.Hypothesis)
	binding.HypothesisQuestionSHA256 = updatedQuestionDigest
	if _, err := BuildHypothesisResult(binding, indexed); err != nil {
		t.Fatalf("current indexed evidence tuple should build: %v", err)
	}

	bad := indexed
	bad.EvidenceRefs = append([]HypothesisEvidenceRef(nil), indexed.EvidenceRefs...)
	bad.EvidenceRefs[0].ReviewRound = new(int)
	*bad.EvidenceRefs[0].ReviewRound = 2
	if _, err := BuildHypothesisResult(binding, bad); err == nil || !strings.Contains(err.Error(), "index tuple") {
		t.Fatalf("wrong-round evidence was accepted: %v", err)
	}

	anchor := hypothesisResultTestInputs()
	anchor.EvidenceRefs[0].SHA256 = strings.Repeat("9", 64)
	if _, err := BuildHypothesisResult(hypothesisResultTestBinding(t), anchor); err == nil || !strings.Contains(err.Error(), "fabricated") {
		t.Fatalf("execution anchor with fabricated SHA was accepted: %v", err)
	}

	consumed := hypothesisResultTestRoute()
	consumed.Route = hypothesisStringPtr("investigate_more")
	consumed.RouteReason = hypothesisStringPtr("Continue after the previous result.")
	consumed.RouteConsumedAt = hypothesisStringPtr("2026-10-14T10:00:00Z")
	if _, err := HypothesisCaseRouteDigest(consumed); err == nil {
		t.Fatal("consumed Case route was eligible for new issuance")
	}
	changedRoute := hypothesisResultTestInputs()
	changedRoute.CaseRoute.Route = hypothesisStringPtr("investigate_more")
	changedRoute.CaseRoute.RouteReason = hypothesisStringPtr("Continue investigation.")
	if _, err := BuildHypothesisResult(hypothesisResultTestBinding(t), changedRoute); err == nil {
		t.Fatal("route tuple drift without corresponding binding digest was accepted")
	}
	allowedReentry := changedRoute.CaseRoute
	allowedDigest, err := HypothesisCaseRouteDigest(allowedReentry)
	if err != nil {
		t.Fatalf("unconsumed investigate_more route should remain eligible: %v", err)
	}
	allowedBinding := hypothesisResultTestBinding(t)
	allowedBinding.CaseRouteSHA256 = allowedDigest
	if _, err := BuildHypothesisResult(allowedBinding, changedRoute); err != nil {
		t.Fatalf("bound unconsumed investigate_more route should build: %v", err)
	}

	changedFinding := hypothesisResultTestInputs()
	changedFinding.Findings[0].FindingID = "finding-other"
	if _, err := BuildHypothesisResult(hypothesisResultTestBinding(t), changedFinding); err == nil {
		t.Fatal("changed Finding exact set was accepted")
	}
}

func TestHypothesisResultPreservesTypedFindingSourcesWithoutIndexAliases(t *testing.T) {
	for _, change := range []string{"unchanged", "hash", "path", "owner", "index metadata", "alias"} {
		t.Run(change, func(t *testing.T) {
			inputs := hypothesisResultTestInputs()
			ref := "path:observations/source.txt#sha256=" + strings.Repeat("a", 64)
			inputs.Findings[0].Document = json.RawMessage(strings.Replace(string(inputs.Findings[0].Document), "test://finding-observation", ref, 1))
			inputs.EvidenceRefs[1] = HypothesisEvidenceRef{Ref: ref, Origin: "finding", SourceFindingID: "finding-001", Attestation: "finding_source", Path: "observations/source.txt", SHA256: strings.Repeat("a", 64)}
			switch change {
			case "hash":
				inputs.EvidenceRefs[1].SHA256 = strings.Repeat("b", 64)
			case "path":
				inputs.EvidenceRefs[1].Path = "other.txt"
			case "owner":
				inputs.EvidenceRefs[1].SourceFindingID = "finding-other"
			case "index metadata":
				inputs.EvidenceRefs[1].Status = "valid"
			case "alias":
				inputs.EvidenceRefs[1].Attestation = "runtime_index"
			}
			packet, err := BuildHypothesisResult(hypothesisResultTestBinding(t), inputs)
			if change != "unchanged" {
				if err == nil {
					t.Fatal("forged original Finding source accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(packet)
			if err := schema.NewEmbeddedValidator().ValidateBytes("hypothesis-result-work-packet.schema.json", data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHypothesisQuestionDigestExcludesIdentityButBindsAllQuestionFields(t *testing.T) {
	question := hypothesisResultTestQuestion()
	base, err := HypothesisQuestionDigest(question)
	if err != nil {
		t.Fatal(err)
	}
	question.HypothesisID = "hypothesis-renamed"
	question.AssignmentID = "assignment-other"
	question.Status = "refuted"
	identityOnly, err := HypothesisQuestionDigest(question)
	if err != nil || identityOnly != base {
		t.Fatalf("question digest should leave identity/status to the separate binding: %q != %q (%v)", identityOnly, base, err)
	}
	question.Statement += " changed"
	changed, err := HypothesisQuestionDigest(question)
	if err != nil || changed == base {
		t.Fatalf("question digest failed to bind the question contents: %q, %v", changed, err)
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
	if _, fields := ValidateHypothesisResultAnswer(p, draftBytes); len(fields) == 0 || fields[0].Pointer != "/answer/method" {
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
		"outside Finding":            func(answer map[string]any) { answer["explains_finding_ids"] = []any{"finding-outside"} },
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
	duplicate := strings.Replace(string(complete), `"result":"supported"`, `"result":"supported","result":"refuted"`, 1)
	if duplicate == string(complete) {
		t.Fatal("test fixture did not locate serialized result field")
	}
	if _, fields := ValidateHypothesisResultAnswer(p, []byte(duplicate)); len(fields) == 0 {
		t.Fatal("duplicate JSON keys were accepted")
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

func TestHumanQuestionFollowupNeedsNoInventedFindingButBatchKeepsSourceSubset(t *testing.T) {
	for _, human := range []bool{false, true} {
		t.Run(map[bool]string{false: "batch", true: "human"}[human], func(t *testing.T) {
			binding, inputs := hypothesisResultTestBinding(t), hypothesisResultTestInputs()
			if human {
				binding.ObservationBatchID, binding.ObservationBatchRef, binding.ObservationBatchSHA256 = "", "", ""
				binding.SourceFindingIDs, binding.HypothesisSourceFindingIDs = []string{}, []string{}
				inputs.Hypothesis.SourceFindingIDs = []string{}
				binding.HypothesisQuestionSHA256, _ = HypothesisQuestionDigest(inputs.Hypothesis)
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
			if !human && (len(fields) == 0 || !strings.Contains(fields[0].Pointer, "source_finding_ids")) {
				t.Fatalf("Batch follow-up lost its source subset requirement: %+v", fields)
			}
		})
	}
}

func TestHistoricalHypothesisResultTemplateRestoresOnlyKnownIssuance(t *testing.T) {
	data, err := schema.ReadAsset("stage-form-s8-template-before-guidance.json")
	if err != nil {
		t.Fatal(err)
	}
	old, err := buildHypothesisResultWithTemplate(hypothesisResultTestBinding(t), hypothesisResultTestInputs(), data)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := RebuildHypothesisResult(old)
	if err != nil || !reflect.DeepEqual(old, rebuilt) {
		t.Fatalf("historical rebuild: %v", err)
	}
	if _, err := HypothesisResultPacketDigest(old); err != nil {
		t.Fatal(err)
	}
	current, err := BuildHypothesisResult(old.Binding, old.Inputs)
	if err != nil || current.ID == old.ID {
		t.Fatalf("new work must use current template: %v", err)
	}
	old.Draft.Instruction = "tampered"
	if _, err := HypothesisResultPacketDigest(old); err == nil {
		t.Fatal("accepted edited frozen questions")
	}
	old.TemplateSHA = strings.Repeat("f", 64)
	if _, err := RebuildHypothesisResult(old); err == nil {
		t.Fatal("accepted unknown template")
	}
}

func TestHypothesisResultBeforeShapeGuidanceRemainsFrozen(t *testing.T) {
	data, err := schema.ReadAsset("stage-form-s8-template-before-answer-shape.json")
	if err != nil {
		t.Fatal(err)
	}
	old, err := buildHypothesisResultWithTemplate(hypothesisResultTestBinding(t), hypothesisResultTestInputs(), data)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := RebuildHypothesisResult(old)
	if err != nil || !reflect.DeepEqual(old, rebuilt) {
		t.Fatalf("old issued questionnaire drifted: %v", err)
	}
	current, err := BuildHypothesisResult(old.Binding, old.Inputs)
	if err != nil || current.ID == old.ID {
		t.Fatalf("new guidance did not use a new template identity: %v", err)
	}
}
