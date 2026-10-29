package assignmentresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestDraftProjectsRecordBindingAndNullAnswer(t *testing.T) {
	record := resultTestRecord(t)
	draft, err := DraftFromRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if draft.RuntimeID != record.RuntimeID || draft.BaselineGeneration != record.BaselineGeneration ||
		draft.AssignmentID != record.AssignmentID || draft.AssignmentRevision != record.Revision ||
		draft.AssignmentRecordContract != record.Contract || draft.AssignmentRecordSHA256 != digest ||
		draft.AgentID != record.OwnerAgentID || draft.AgentDefinitionRef != record.AgentDefinition.Ref ||
		draft.TaskID != record.Task.ID || draft.TeamID != record.TeamID {
		t.Fatalf("Draft machine binding does not project the canonical Record: %+v", draft)
	}
	if draft.Summary != nil || draft.AssertionEvidence != nil || draft.RisksBlockers != nil || draft.ScopeDeviations != nil || draft.ClosingAssessment != nil {
		t.Fatalf("professional fields must begin as null placeholders: %+v", draft)
	}

	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"summary":null`, `"assertion_evidence":null`, `"risks_blockers":null`, `"scope_deviations":null`, `"closing_assessment":null`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("draft JSON lacks explicit null placeholder %s: %s", field, data)
		}
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s6-result.schema.json", data); err != nil {
		t.Fatalf("schema rejects incomplete scaffold: %v", err)
	}
}

func TestValidateAnswerRequiresExactAcceptedPlanCoverage(t *testing.T) {
	plan := []PlanAssertion{
		{Assertion: "the API returns the stored value", Oracle: "read response JSON contains the submitted value"},
		{Assertion: "invalid input is rejected", Oracle: "request returns HTTP 400"},
	}
	answer := validResultAnswer()
	answer.AssertionEvidence = []AssertionEvidence{
		{Assertion: plan[0].Assertion, Oracle: plan[0].Oracle, EvidenceRefs: []string{"runner://check/1", "git://diff/1"}},
		{Assertion: plan[1].Assertion, Oracle: plan[1].Oracle, EvidenceRefs: []string{"runner://check/2"}},
	}
	if err := ValidateAnswer(answer, plan); err != nil {
		t.Fatalf("ValidateAnswer() rejected exact coverage: %v", err)
	}
	reordered := cloneAnswer(answer)
	reordered.AssertionEvidence[0], reordered.AssertionEvidence[1] = reordered.AssertionEvidence[1], reordered.AssertionEvidence[0]
	if err := ValidateAnswer(reordered, plan); err != nil {
		t.Fatalf("exact assertion coverage should not depend on answer order: %v", err)
	}

	tests := []struct {
		name   string
		answer Answer
		plan   []PlanAssertion
	}{
		{name: "no accepted plan", answer: answer},
		{name: "missing assertion", answer: func() Answer { a := answer; a.AssertionEvidence = a.AssertionEvidence[:1]; return a }(), plan: plan},
		{name: "extra assertion", answer: func() Answer {
			a := answer
			a.AssertionEvidence = append(cloneAssertionEvidence(a.AssertionEvidence), AssertionEvidence{Assertion: "extra", Oracle: "extra", EvidenceRefs: []string{"ref"}})
			return a
		}(), plan: plan},
		{name: "oracle drift", answer: func() Answer {
			a := answer
			a.AssertionEvidence = cloneAssertionEvidence(a.AssertionEvidence)
			a.AssertionEvidence[0].Oracle += " changed"
			return a
		}(), plan: plan},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateAnswer(test.answer, test.plan); err == nil {
				t.Fatal("ValidateAnswer() accepted invalid assertion coverage")
			}
		})
	}
}

func TestValidateAnswerRequiresProfessionalFieldsButAllowsEmptyRiskLists(t *testing.T) {
	plan := []PlanAssertion{{Assertion: "value persists", Oracle: "readback matches"}}
	answer := Answer{
		Summary: "Implemented and inspected the persistence flow.",
		AssertionEvidence: []AssertionEvidence{{
			Assertion:    plan[0].Assertion,
			Oracle:       plan[0].Oracle,
			EvidenceRefs: []string{"worker://assertion/evidence"},
		}},
		RisksBlockers:   []string{},
		ScopeDeviations: []string{},
	}
	if err := ValidateAnswer(answer, plan); err != nil {
		t.Fatalf("empty risk/scope arrays should be accepted: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*Answer)
	}{
		{name: "blank summary", mutate: func(a *Answer) { a.Summary = " \n " }},
		{name: "null risks array", mutate: func(a *Answer) { a.RisksBlockers = nil }},
		{name: "null scope array", mutate: func(a *Answer) { a.ScopeDeviations = nil }},
		{name: "missing evidence refs", mutate: func(a *Answer) { a.AssertionEvidence[0].EvidenceRefs = nil }},
		{name: "blank evidence ref", mutate: func(a *Answer) { a.AssertionEvidence[0].EvidenceRefs = []string{"  "} }},
		{name: "duplicate evidence ref", mutate: func(a *Answer) { a.AssertionEvidence[0].EvidenceRefs = []string{"ref", "ref"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneAnswer(answer)
			test.mutate(&candidate)
			if err := ValidateAnswer(candidate, plan); err == nil {
				t.Fatal("ValidateAnswer() accepted an incomplete answer")
			}
		})
	}
}

func TestAnswerDigestCoversOnlyProfessionalAnswer(t *testing.T) {
	answer := validResultAnswer()
	first, err := AnswerDigest(answer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AnswerDigest(cloneAnswer(answer))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("AnswerDigest() must be stable lowercase SHA-256: %q, %q", first, second)
	}
	answer.Summary += " Further detail."
	third, err := AnswerDigest(answer)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("professional answer change must change AnswerDigest")
	}
	answer = validResultAnswer()
	answer.ClosingAssessment = validClosingAssessment()
	withClosing, err := AnswerDigest(answer)
	if err != nil {
		t.Fatal(err)
	}
	if withClosing == first {
		t.Fatal("new closing_assessment must be included in AnswerDigest")
	}
	legacyJSON, err := json.Marshal(struct {
		Summary           string              `json:"summary"`
		AssertionEvidence []AssertionEvidence `json:"assertion_evidence"`
		RisksBlockers     []string            `json:"risks_blockers"`
		ScopeDeviations   []string            `json:"scope_deviations"`
	}{Summary: answer.Summary, AssertionEvidence: answer.AssertionEvidence, RisksBlockers: answer.RisksBlockers, ScopeDeviations: answer.ScopeDeviations})
	if err != nil {
		t.Fatal(err)
	}
	legacyDigest := sha256.Sum256(legacyJSON)
	legacyHex := hex.EncodeToString(legacyDigest[:])
	answer.ClosingAssessment = nil
	withoutClosing, err := AnswerDigest(answer)
	if err != nil || withoutClosing != legacyHex {
		t.Fatalf("omitted closing_assessment changed the original four-field digest: got %s want %s err %v", withoutClosing, legacyHex, err)
	}
}

func TestValidateClosingAssessmentBindsEveryFrozenConditionToAcceptedPlanEvidence(t *testing.T) {
	record := resultTestRecord(t)
	record.ClosingContract = &assignmentrecord.ClosingContract{Contract: S6ClosingContract, WorkerChecks: []assignmentrecord.WorkerCheck{}}
	plan := []PlanAssertion{{Assertion: "value persists", Oracle: "readback matches", DoneWhenIndexes: []int{0}}}
	answer := validResultAnswer()
	answer.AssertionEvidence[0].Assertion = plan[0].Assertion
	answer.AssertionEvidence[0].Oracle = plan[0].Oracle
	answer.ClosingAssessment = validClosingAssessment()
	if err := ValidateAnswer(answer, plan); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClosingAssessment(record, plan, answer); err != nil {
		t.Fatalf("valid complete assessment rejected: %v", err)
	}
	if !ClosingAssessmentComplete(answer) {
		t.Fatal("complete, fully satisfied assessment not recognized")
	}

	mutations := []struct {
		name   string
		mutate func(*Answer, *[]PlanAssertion)
	}{
		{name: "missing condition", mutate: func(a *Answer, _ *[]PlanAssertion) { a.ClosingAssessment.Conditions = nil }},
		{name: "condition outside Record", mutate: func(a *Answer, _ *[]PlanAssertion) { a.ClosingAssessment.Conditions[0].DoneWhenIndex = 1 }},
		{name: "condition cites unmapped assertion", mutate: func(a *Answer, p *[]PlanAssertion) { (*p)[0].DoneWhenIndexes = nil }},
		{name: "condition cites assertion without source evidence", mutate: func(a *Answer, _ *[]PlanAssertion) { a.AssertionEvidence = nil }},
		{name: "complete with incomplete condition", mutate: func(a *Answer, _ *[]PlanAssertion) { a.ClosingAssessment.Conditions[0].Judgment = "incomplete" }},
		{name: "empty rationale", mutate: func(a *Answer, _ *[]PlanAssertion) { a.ClosingAssessment.Conditions[0].Rationale = "  " }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneAnswer(answer)
			candidatePlan := append([]PlanAssertion{}, plan...)
			candidatePlan[0].DoneWhenIndexes = append([]int{}, plan[0].DoneWhenIndexes...)
			test.mutate(&candidate, &candidatePlan)
			if err := ValidateClosingAssessment(record, candidatePlan, candidate); err == nil {
				t.Fatal("invalid closing assessment was accepted")
			}
		})
	}

	for _, disposition := range []string{"incomplete", "blocked"} {
		t.Run("valid "+disposition, func(t *testing.T) {
			candidate := cloneAnswer(answer)
			candidate.ClosingAssessment.Disposition = disposition
			candidate.ClosingAssessment.Conditions[0].Judgment = disposition
			if err := ValidateClosingAssessment(record, plan, candidate); err != nil {
				t.Fatalf("legitimate %s assessment must be accepted for projection: %v", disposition, err)
			}
			if ClosingAssessmentComplete(candidate) {
				t.Fatalf("%s assessment cannot be complete", disposition)
			}
		})
	}

	t.Run("legacy record preserves missing closing", func(t *testing.T) {
		legacy := resultTestRecord(t)
		old := validResultAnswer()
		if err := ValidateClosingAssessment(legacy, nil, old); err != nil {
			t.Fatalf("legacy answer without closing contract changed: %v", err)
		}
		old.ClosingAssessment = validClosingAssessment()
		if err := ValidateClosingAssessment(legacy, plan, old); err == nil {
			t.Fatal("assessment without frozen Record closing contract was accepted")
		}
	})
}

func TestDraftAnswerSchemaIsClosedAndWithAnswerDoesNotClaimPass(t *testing.T) {
	plan := []PlanAssertion{{Assertion: "value persists", Oracle: "readback matches"}}
	answer := validResultAnswer()
	answer.AssertionEvidence[0].Assertion = plan[0].Assertion
	answer.AssertionEvidence[0].Oracle = plan[0].Oracle
	draft, err := DraftFromRecord(resultTestRecord(t))
	if err != nil {
		t.Fatal(err)
	}
	draft, err = draft.WithAnswer(answer, plan)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s6-result.schema.json", data); err != nil {
		t.Fatalf("schema rejects complete professional answer: %v", err)
	}
	if strings.Contains(string(data), `"status"`) || strings.Contains(string(data), `"checks"`) || strings.Contains(string(data), `"changed_paths"`) || strings.Contains(string(data), `"verified"`) {
		t.Fatalf("Result draft introduced machine completion claims: %s", data)
	}
	var withUnknown map[string]json.RawMessage
	if err := json.Unmarshal(data, &withUnknown); err != nil {
		t.Fatal(err)
	}
	withUnknown["changed_paths"] = json.RawMessage(`[]`)
	unknownData, err := json.Marshal(withUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer-s6-result.schema.json", unknownData); err == nil {
		t.Fatal("closed Result answer schema accepted changed_paths")
	}
}

func validResultAnswer() Answer {
	return Answer{
		Summary: "Implemented the assigned behavior and reviewed the result.",
		AssertionEvidence: []AssertionEvidence{{
			Assertion:    "value persists",
			Oracle:       "readback matches",
			EvidenceRefs: []string{"worker://assertion/evidence"},
		}},
		RisksBlockers:   []string{},
		ScopeDeviations: []string{},
	}
}

func cloneAnswer(answer Answer) Answer {
	answer.AssertionEvidence = cloneAssertionEvidence(answer.AssertionEvidence)
	answer.RisksBlockers = cloneStrings(answer.RisksBlockers)
	answer.ScopeDeviations = cloneStrings(answer.ScopeDeviations)
	answer.ClosingAssessment = cloneClosingAssessment(answer.ClosingAssessment)
	return answer
}

func validClosingAssessment() *ClosingAssessment {
	return &ClosingAssessment{Disposition: "complete", Conditions: []ClosingCondition{{
		DoneWhenIndex: 0, Judgment: "satisfied", PlanAssertionIndexes: []int{0},
		Rationale: "The cited accepted assertion evidence covers the frozen condition.",
	}}}
}

func resultTestRecord(t *testing.T) assignmentrecord.Record {
	t.Helper()
	taskBytes := []byte("# TASK-042-01\n\n> Version: v1.0.0\n")
	taskSHA := resultTestSHA(taskBytes)
	manifest := map[string]any{
		"schema_version":             "1.0.0",
		"assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id":                "team-manifest-result-test",
		"version":                    "v1.2.0",
		"runtime_id":                 "loop-result-test",
		"baseline_generation":        3,
		"workgroup_id":               "workgroup-result-test",
		"workgroup_kind":             "builder",
		"documents": []any{map[string]any{
			"id": "TASK-042-01", "path": "docs/dev/tasks/TASK-042-01.md",
			"version": "v1.0.0", "sha256": taskSHA,
		}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-result-test", "responsibility_id": "BUILD-WORK-PACKAGE",
			"role_family": "backend-builder", "agent_id": "agent-result-test",
			"agent_definition_ref": ".claude/agents/backend-builder.md",
			"skill_refs":           []string{"backend-engineering"}, "read_paths": []string{"docs/dev/tasks/"},
			"scope": []string{"internal/"}, "write_paths": []string{"internal/pkg/"},
			"output_paths": []string{}, "depends_on": []string{},
			"required_checks": []string{"go test ./internal/pkg"}, "done_when": []string{"owned checks pass"},
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runtimeState := map[string]any{
		"runtime_id": "loop-result-test",
		"revision":   float64(8),
		"lifecycle":  map[string]any{"state": "building"},
		"baseline":   map[string]any{"generation": float64(3)},
		"documents": []any{map[string]any{
			"id": "TASK-042-01", "kind": "task", "path": "docs/dev/tasks/TASK-042-01.md",
			"version": "v1.0.0", "sha256": taskSHA, "status": "locked", "generation": float64(3),
		}},
		"entities": map[string]any{
			"tasks": []any{map[string]any{
				"id": "TASK-042-01", "path": "docs/dev/tasks/TASK-042-01.md", "sha256": taskSHA,
				"owner_agent_ids": []string{"agent-result-test"},
			}},
			"teams": []any{map[string]any{
				"id": "workgroup-result-test", "kind": "builder", "manifest_ref": "docs/workgroups/s6/result-test.json",
			}},
			"agents": []any{map[string]any{
				"id": "agent-result-test", "team_id": "workgroup-result-test", "task_ids": []string{"TASK-042-01"},
				"role": "backend-builder", "definition_ref": ".claude/agents/backend-builder.md", "dispatch_mode": "plan_checkpoint",
			}},
		},
	}
	record, err := assignmentrecord.BuildRecord(
		manifestBytes,
		"docs/workgroups/s6/result-test.json",
		runtimeState,
		taskBytes,
		"TASK-042-01",
		"assignment-result-test",
		[]string{"internal"},
		[]assignmentrecord.ContextSource{{
			Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: strings.Repeat("a", 64),
		}, {
			Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", Version: "1.0.0", SHA256: strings.Repeat("b", 64),
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func resultTestSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
