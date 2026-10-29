package assignmentresult

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func acceptedFixture(t *testing.T) (map[string]any, assignmentrecord.Record, string, map[string][]byte) {
	t.Helper()
	record := resultTestRecord(t)
	sha, _ := record.Digest()
	planRef := ".claude/plans/accepted-s6.json"
	answer := validResultAnswer()
	plan, _ := json.Marshal(map[string]any{"message_type": "plan_report", "agent_id": record.OwnerAgentID, "runtime_id": record.RuntimeID, "assignment_id": record.AssignmentID, "assignment_revision": record.Revision, "assignment_record_sha256": sha, "assertion_checks": []PlanAssertion{{Assertion: answer.AssertionEvidence[0].Assertion, Oracle: answer.AssertionEvidence[0].Oracle}}})
	prefix := fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
	ref := prefix + "result-1.json"
	sourceRef := prefix + "sources/proof.txt"
	source := []byte("actual original source snapshot")
	digest, _ := AnswerDigest(answer)
	result := AcceptedResult{SchemaVersion: "1.0.0", EvidenceID: "ev-result-1", Kind: "completion_report", RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration, ProducerAgentID: record.OwnerAgentID, ProducerResponsibility: "BUILD-WORK-PACKAGE", SubjectRefs: []SubjectReference{{ID: record.Task.ID, Kind: "task", Path: record.Task.Ref, Version: record.Task.Version, SHA256: record.Task.SHA256}}, Conclusion: "submitted", TaskID: record.Task.ID, AssignmentRecord: record.Reference(), AssignmentRecordSHA256: sha, ResultRevision: 1, SubmissionKey: "msg-result-1", AnswerDigest: digest, Answer: answer, PlanRef: planRef, PlanSHA256: digestBytes(plan), ResolvedEvidenceRefs: []ResolvedEvidence{{Ref: answer.AssertionEvidence[0].EvidenceRefs[0], Path: sourceRef, SHA256: digestBytes(source)}}, CreatedAt: "2026-10-14T00:00:00Z"}
	body, _ := json.Marshal(result)
	rawRecord, _ := json.Marshal(record)
	var row map[string]any
	_ = json.Unmarshal(rawRecord, &row)
	state := map[string]any{"runtime_id": record.RuntimeID, "revision": 10, "baseline": map[string]any{"generation": record.BaselineGeneration}, "entities": map[string]any{
		"assignments": []any{row}, "teams": []any{map[string]any{"id": record.TeamID, "kind": "builder", "manifest_ref": record.Manifest.Ref}},
		"agents": []any{map[string]any{"id": record.OwnerAgentID, "team_id": record.TeamID, "role": record.RoleFamily, "definition_ref": record.AgentDefinition.Ref, "dispatch_mode": record.DispatchMode, "task_ids": []any{record.Task.ID}}},
		"tasks":  []any{map[string]any{"id": record.Task.ID, "path": record.Task.Ref, "sha256": record.Task.SHA256, "owner_agent_ids": []any{record.OwnerAgentID}}},
	}, "evidence": []any{map[string]any{"id": result.EvidenceID, "kind": "completion_report", "path": ref, "sha256": digestBytes(body), "status": "valid", "baseline_generation": record.BaselineGeneration, "responsibility_id": "BUILD-WORK-PACKAGE", "produced_by": []any{record.OwnerAgentID}}}}
	return state, record, ref, map[string][]byte{ref: body, planRef: plan, sourceRef: source}
}

func TestLoadAcceptedRequiresOriginalRegistryAndDurableSources(t *testing.T) {
	state, record, ref, files := acceptedFixture(t)
	read := func(p string) ([]byte, error) {
		b, ok := files[p]
		if !ok {
			return nil, fmt.Errorf("missing %s", p)
		}
		return b, nil
	}
	result, err := LoadAccepted(state, record, ref, read)
	if err != nil || result.Conclusion != "submitted" {
		t.Fatalf("LoadAccepted = %s, %v", result.Conclusion, err)
	}
	history, err := AcceptedHistory(state, record, read)
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %d, %v", len(history), err)
	}
	files[result.ResolvedEvidenceRefs[0].Path] = []byte("changed snapshot")
	if _, err := LoadAccepted(state, record, ref, read); err == nil {
		t.Fatal("changed snapshot accepted")
	}
}

func TestLoadAcceptedValidatesFrozenClosingAssessmentAndSources(t *testing.T) {
	state, record, ref, files := acceptedFixture(t)
	record.ClosingContract = &assignmentrecord.ClosingContract{Contract: S6ClosingContract, WorkerChecks: []assignmentrecord.WorkerCheck{}}
	record.IntegrationCheckMode = "post_merge"
	answer := validResultAnswer()
	answer.ClosingAssessment = validClosingAssessment()
	plan := map[string]any{
		"message_type": "plan_report", "agent_id": record.OwnerAgentID, "runtime_id": record.RuntimeID,
		"assignment_id": record.AssignmentID, "assignment_revision": record.Revision,
		"assignment_record_sha256": mustRecordDigest(t, record),
		"assertion_checks": []any{map[string]any{
			"assertion": answer.AssertionEvidence[0].Assertion, "oracle": answer.AssertionEvidence[0].Oracle,
			"done_when_indexes": []int{0},
		}},
	}
	planBytes, _ := json.Marshal(plan)
	answerDigest, _ := AnswerDigest(answer)
	var result AcceptedResult
	if err := json.Unmarshal(files[ref], &result); err != nil {
		t.Fatal(err)
	}
	result.AssignmentRecordSHA256 = mustRecordDigest(t, record)
	result.Answer, result.AnswerDigest = answer, answerDigest
	result.PlanSHA256 = digestBytes(planBytes)
	resultBytes, _ := json.Marshal(result)
	files[result.PlanRef], files[ref] = planBytes, resultBytes
	state["entities"].(map[string]any)["assignments"] = []any{mustRecordMap(t, record)}
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = digestBytes(resultBytes)
	read := func(path string) ([]byte, error) {
		body, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("missing %s", path)
		}
		return body, nil
	}
	if _, err := LoadAccepted(state, record, ref, read); err != nil {
		t.Fatalf("valid frozen closing assessment rejected: %v", err)
	}

	for _, test := range []string{"missing assessment", "condition map", "condition source"} {
		t.Run(test, func(t *testing.T) {
			candidateState, candidateRecord, candidateRef, candidateFiles := acceptedFixture(t)
			candidateRecord.ClosingContract = &assignmentrecord.ClosingContract{Contract: S6ClosingContract, WorkerChecks: []assignmentrecord.WorkerCheck{}}
			candidateRecord.IntegrationCheckMode = "post_merge"
			var candidate AcceptedResult
			_ = json.Unmarshal(candidateFiles[candidateRef], &candidate)
			candidate.AssignmentRecordSHA256 = mustRecordDigest(t, candidateRecord)
			candidatePlan := map[string]any{
				"message_type": "plan_report", "agent_id": candidateRecord.OwnerAgentID, "runtime_id": candidateRecord.RuntimeID,
				"assignment_id": candidateRecord.AssignmentID, "assignment_revision": candidateRecord.Revision,
				"assignment_record_sha256": mustRecordDigest(t, candidateRecord),
				"assertion_checks": []any{map[string]any{
					"assertion":         candidate.Answer.AssertionEvidence[0].Assertion,
					"oracle":            candidate.Answer.AssertionEvidence[0].Oracle,
					"done_when_indexes": []int{0},
				}},
			}
			candidate.Answer.ClosingAssessment = validClosingAssessment()
			switch test {
			case "missing assessment":
				candidate.Answer.ClosingAssessment = nil
			case "condition map":
				candidatePlan["assertion_checks"].([]any)[0].(map[string]any)["done_when_indexes"] = []int{1}
			case "condition source":
				candidate.Answer.ClosingAssessment.Conditions[0].PlanAssertionIndexes = []int{1}
			}
			candidate.AnswerDigest, _ = AnswerDigest(candidate.Answer)
			planData, _ := json.Marshal(candidatePlan)
			candidate.PlanSHA256 = digestBytes(planData)
			body, _ := json.Marshal(candidate)
			candidateFiles[candidate.PlanRef], candidateFiles[candidateRef] = planData, body
			candidateState["entities"].(map[string]any)["assignments"] = []any{mustRecordMap(t, candidateRecord)}
			candidateState["evidence"].([]any)[0].(map[string]any)["sha256"] = digestBytes(body)
			candidateRead := func(path string) ([]byte, error) {
				value, ok := candidateFiles[path]
				if !ok {
					return nil, fmt.Errorf("missing %s", path)
				}
				return value, nil
			}
			if _, err := LoadAccepted(candidateState, candidateRecord, candidateRef, candidateRead); err == nil {
				t.Fatal("invalid frozen closing assessment accepted")
			}
		})
	}
}

func mustRecordDigest(t *testing.T, record assignmentrecord.Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func mustRecordMap(t *testing.T, record assignmentrecord.Record) map[string]any {
	t.Helper()
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestLoadAcceptedRejectsIndependentCopiesAndHistoryDrift(t *testing.T) {
	for _, test := range []string{"registry missing", "index missing", "index digest", "record digest", "plan bytes", "answer digest", "invalid acceptance time", "duplicate revision", "unknown field"} {
		t.Run(test, func(t *testing.T) {
			state, record, ref, files := acceptedFixture(t)
			read := func(p string) ([]byte, error) {
				b, ok := files[p]
				if !ok {
					return nil, fmt.Errorf("missing")
				}
				return b, nil
			}
			var result AcceptedResult
			_ = json.Unmarshal(files[ref], &result)
			switch test {
			case "registry missing":
				delete(state["entities"].(map[string]any), "assignments")
			case "index missing":
				state["evidence"] = []any{}
			case "index digest":
				state["evidence"].([]any)[0].(map[string]any)["sha256"] = "bad"
			case "record digest":
				result.AssignmentRecordSHA256 = "bad"
			case "plan bytes":
				files[result.PlanRef] = []byte("changed")
			case "answer digest":
				result.AnswerDigest = "bad"
			case "invalid acceptance time":
				result.CreatedAt = "yesterday"
			case "duplicate revision":
				state["evidence"] = append(state["evidence"].([]any), state["evidence"].([]any)[0])
			case "unknown field":
				files[ref] = append(files[ref][:len(files[ref])-1], []byte(`,"checks":[]}`)...)
			}
			if test == "record digest" || test == "answer digest" || test == "invalid acceptance time" {
				files[ref], _ = json.Marshal(result)
			}
			if test == "record digest" || test == "answer digest" || test == "invalid acceptance time" || test == "unknown field" {
				state["evidence"].([]any)[0].(map[string]any)["sha256"] = digestBytes(files[ref])
			}
			if _, err := LoadAccepted(state, record, ref, read); err == nil {
				t.Fatalf("%s accepted", test)
			}
		})
	}
}
