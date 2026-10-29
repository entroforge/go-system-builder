package agentcontext_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestSharedPlanTemplateKeepsProfessionalQuestionsAndOriginalDispatchMode(t *testing.T) {
	for _, mode := range []string{"plan_checkpoint", "plan_approval_required", "one_shot"} {
		d := agentcontext.Assignment{AssignmentID: "assignment-real", Owner: "owner-real", TaskID: "TASK-real", TeamID: "team-real", AgentDefinitionRef: ".claude/agents/backend-builder.md", DispatchMode: mode, DependsOn: []string{"assignment-prerequisite"}}
		planned := []string{"internal/own"}
		plan, questions := agentcontext.PlanTemplate(d, "loop-real", "2026-10-13T00:00:00Z", planned)
		if mode != "plan_checkpoint" {
			if plan != nil || questions != nil {
				t.Fatal("approval/one_shot got an activation shortcut")
			}
			continue
		}
		if plan["agent_id"] != d.Owner || plan["runtime_id"] != "loop-real" || plan["assignment_id"] != d.AssignmentID || plan["objective"] != nil || plan["risks_blockers"] != nil || len(plan["steps"].([]any)) != 0 || len(plan["assertion_checks"].([]any)) != 0 || plan["assignment_record_contract"] != nil {
			t.Fatal("guessed identity or professional fields")
		}
		if len(questions) < 6 || !reflect.DeepEqual(plan["dependencies"], d.DependsOn) {
			t.Fatal("missing actual questions/dependencies")
		}
		plan["planned_paths"].([]string)[0] = "changed"
		if planned[0] != "internal/own" {
			t.Fatal("template changed original scope")
		}
	}
}

func TestPlanAndResultQuestionsDistinguishResultAcceptance(t *testing.T) {
	assertTiming := func(question string) {
		t.Helper()
		if !strings.Contains(question, "before submitting the Result") || !strings.Contains(question, "acceptance receipt proves Result registration afterwards") {
			t.Fatalf("missing Result oracle timing: %s", question)
		}
		if strings.Contains(question, "before submitting the PLAN") {
			t.Fatal("PLAN must be allowed to describe future implementation")
		}
	}
	generic := agentcontext.Assignment{AssignmentID: "assignment-real", Owner: "owner-real", DispatchMode: "plan_checkpoint"}
	_, questions := agentcontext.PlanTemplate(generic, "loop-real", "2026-10-15T00:00:00Z", nil)
	assertTiming(questions["/assertion_checks"])
	for _, closing := range []bool{false, true} {
		record := contextRecord()
		record.ResponsibilityID = "BUILD-WORK-PACKAGE"
		if closing {
			record.IntegrationCheckMode = "post_merge"
			record.ClosingContract = &assignmentrecord.ClosingContract{Contract: assignmentrecord.ClosingContractVersion, WorkerChecks: []assignmentrecord.WorkerCheck{}}
		}
		projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
		if err != nil {
			t.Fatal(err)
		}
		_, planQuestions := agentcontext.PlanTemplate(projected, record.RuntimeID, "2026-10-15T00:00:00Z", nil)
		assertTiming(planQuestions["/assertion_checks"])
		_, resultQuestions := agentcontext.ResultTemplate(projected, record.RuntimeID, "2026-10-15T00:00:00Z")
		assertTiming(resultQuestions["/assertion_evidence"])
	}
}

func TestAssignmentRecordProjectionAndThinPlanTemplate(t *testing.T) {
	record := contextRecord()
	base := agentcontext.Assignment{AssignmentID: record.AssignmentID, Owner: "stale-owner", EffectiveWritePaths: []string{"elsewhere"}}
	projected, err := agentcontext.ApplyRecord(base, record)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Owner != record.OwnerAgentID || projected.TaskID != record.Task.ID || projected.TeamID != record.TeamID || projected.Role != record.RoleFamily || projected.AssignmentRevision != record.Revision {
		t.Fatalf("machine identity did not project from Record: %+v", projected)
	}
	if !reflect.DeepEqual(projected.EffectiveWritePaths, record.EffectiveWritePaths) || !reflect.DeepEqual(projected.RequiredChecks, record.RequiredChecks) || !reflect.DeepEqual(projected.DependsOn, record.DependsOn) {
		t.Fatalf("scope/check/dependency projection differs from Record: %+v", projected)
	}
	if projected.AssignmentRecord == nil || *projected.AssignmentRecord != record.Reference() || projected.AssignmentRecordSHA256 == "" {
		t.Fatalf("Record reference/digest missing from context projection: %+v", projected)
	}

	plan, questions := agentcontext.PlanTemplate(projected, record.RuntimeID, "2026-10-13T00:00:00Z", []string{"caller-must-not-prefill"})
	if plan == nil {
		t.Fatal("canonical Record projection did not produce the S6 PLAN template")
	}
	if plan["assignment_record_contract"] != assignmentrecord.AssignmentRecordContract || plan["assignment_record_sha256"] != projected.AssignmentRecordSHA256 || plan["assignment_revision"] != record.Revision {
		t.Fatalf("PLAN did not bind the actual Record revision/digest: %#v", plan)
	}
	if plan["objective"] != nil || len(plan["planned_paths"].([]string)) != 0 || len(plan["assertion_checks"].([]any)) != 0 || plan["risks_blockers"] != nil {
		t.Fatalf("professional PLAN answers were prefilled: %#v", plan)
	}
	if !reflect.DeepEqual(plan["dependencies"], record.DependsOn) || !strings.Contains(questions["/dependencies"], `workgroup-local`) || !strings.Contains(questions["/dependencies"], `assignment-prerequisite`) {
		t.Fatalf("known registered dependencies were left to guessing: %#v %#v", plan, questions)
	}
	if _, exists := plan["steps"]; exists {
		t.Fatal("v1.2 PLAN template must not ask for the generic steps field")
	}
	if _, exists := questions["/steps"]; exists || len(questions) != 5 {
		t.Fatalf("v1.2 questions should cover only the five professional fields: %#v", questions)
	}
	if _, exists := questions["/dependencies"]; !exists {
		t.Fatal("the registered dependency field still needs its meaning explained")
	}

	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"assignment_record"`) || !strings.Contains(string(encoded), `"assignment_record_sha256"`) {
		t.Fatalf("Assignment JSON omits its canonical Record binding: %s", encoded)
	}
}

func TestPlanTemplateRejectsIncompleteOrMismatchedRecordBinding(t *testing.T) {
	record := contextRecord()
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	projected.AssignmentRecordSHA256 = ""
	if plan, _ := agentcontext.PlanTemplate(projected, record.RuntimeID, "2026-10-13T00:00:00Z", nil); plan != nil {
		t.Fatal("incomplete Record projection fell back to the legacy PLAN template")
	}
	projected, err = agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	projected.AssignmentRecord.Revision++
	if plan, _ := agentcontext.PlanTemplate(projected, record.RuntimeID, "2026-10-13T00:00:00Z", nil); plan != nil {
		t.Fatal("mismatched Record revision was accepted")
	}
}

func TestResultTemplateIsCanonicalBuilderOnlyAndLeavesProfessionalFieldsBlank(t *testing.T) {
	record := contextRecord()
	record.ResponsibilityID = "BUILD-WORK-PACKAGE"
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	const occurredAt = "2026-10-14T00:00:00Z"
	draft, questions := agentcontext.ResultTemplate(projected, record.RuntimeID, occurredAt)
	if draft == nil || len(questions) != 4 {
		t.Fatalf("canonical Builder did not get one Result draft and four professional guidance questions: %#v %#v", draft, questions)
	}
	if draft["schema_version"] != "1.0.0" || draft["message_type"] != "completion_report" ||
		draft["runtime_id"] != record.RuntimeID || draft["baseline_generation"] != record.BaselineGeneration ||
		draft["assignment_id"] != record.AssignmentID || draft["assignment_revision"] != record.Revision ||
		draft["assignment_record_contract"] != assignmentrecord.AssignmentRecordContract ||
		draft["assignment_record_sha256"] != projected.AssignmentRecordSHA256 || draft["agent_id"] != record.OwnerAgentID ||
		draft["agent_definition_ref"] != record.AgentDefinition.Ref || draft["task_id"] != record.Task.ID || draft["team_id"] != record.TeamID ||
		draft["bug_id"] != nil || draft["occurred_at"] != occurredAt {
		t.Fatalf("Result machine binding differs from canonical Record: %#v", draft)
	}
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(draft, draftBytes); err != nil {
		t.Fatalf("issued blank Result draft did not match its tuple: %v", err)
	}
	for _, field := range []string{"summary", "assertion_evidence", "risks_blockers", "scope_deviations"} {
		if draft[field] != nil {
			t.Errorf("template supplied professional answer %s: %#v", field, draft[field])
		}
		if questions["/"+field] == "" {
			t.Errorf("template omitted professional question for %s", field)
		}
	}
	if _, exists := draft["closing_assessment"]; exists || len(questions) != 4 {
		t.Fatal("legacy Record shape acquired closing assessment fields")
	}
	if _, exists := questions["/message_id"]; exists {
		t.Fatal("machine Result identity is still presented as an author question")
	}
	if !strings.Contains(questions["/assertion_evidence"], "original begin flow copies") {
		t.Fatal("assertion/oracle rows are not described as supplied by the original PLAN flow")
	}
	for _, want := range []string{"go test ./internal/agentcontext", "state the reason or blocker", "Main's integration rerun is separate"} {
		if !strings.Contains(questions["/summary"], want) {
			t.Errorf("Result summary guidance omitted %q: %s", want, questions["/summary"])
		}
	}
	for _, forbidden := range []string{"plan_ref", "plan_sha256", "status", "checks", "changed_paths", "reviewed_paths", "conclusion"} {
		if _, exists := draft[forbidden]; exists {
			t.Errorf("Result template invented non-answer field %s", forbidden)
		}
	}

	for _, ineligible := range []agentcontext.Assignment{
		{AssignmentID: projected.AssignmentID, Owner: projected.Owner, TaskID: projected.TaskID, TeamID: projected.TeamID, AgentDefinitionRef: projected.AgentDefinitionRef, ResponsibilityIDs: []string{"DOCUMENT-REVIEW"}},
		func() agentcontext.Assignment {
			d := projected
			d.ResponsibilityIDs = []string{"BUILD-CONTEXT"}
			return d
		}(),
	} {
		if got, gotQuestions := agentcontext.ResultTemplate(ineligible, record.RuntimeID, occurredAt); got != nil || gotQuestions != nil {
			t.Fatalf("non-canonical/non-Builder assignment got a Result draft: %#v %#v", got, gotQuestions)
		}
	}
}

func TestClosingContractProjectsDoneWhenCoverageAndAssessmentDraft(t *testing.T) {
	record := contextRecord()
	record.ResponsibilityID = "BUILD-WORK-PACKAGE"
	record.DoneWhen = []string{"the package behavior is delivered", "the registered owner check is completed"}
	record.RequiredChecks = []string{"go vet ./...", "go test ./..."}
	record.IntegrationCheckMode = "post_merge"
	record.ClosingContract = &assignmentrecord.ClosingContract{
		Contract:     assignmentrecord.ClosingContractVersion,
		WorkerChecks: []assignmentrecord.WorkerCheck{},
	}
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	if projected.ClosingContract == nil || projected.IntegrationCheckMode != "post_merge" || projected.ClosingContract.WorkerChecks == nil || len(projected.ClosingContract.WorkerChecks) != 0 {
		t.Fatalf("explicit empty Worker check set was lost or changed: %+v", projected)
	}
	projected.ClosingContract.WorkerChecks = append(projected.ClosingContract.WorkerChecks, assignmentrecord.WorkerCheck{Index: 0})
	if len(record.ClosingContract.WorkerChecks) != 0 {
		t.Fatal("context projection aliases the authoritative Record closing contract")
	}
	projected, err = agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}

	plan, planQuestions := agentcontext.PlanTemplate(projected, record.RuntimeID, "2026-10-14T00:00:00Z", nil)
	if plan == nil || !strings.Contains(planQuestions["/assertion_checks"], "done_when_indexes") || !strings.Contains(planQuestions["/assertion_checks"], "required_checks") ||
		!strings.Contains(planQuestions["/assertion_checks"], "0: "+record.DoneWhen[0]) || !strings.Contains(planQuestions["/assertion_checks"], "1: "+record.DoneWhen[1]) {
		t.Fatalf("PLAN guidance does not require complete DoneWhen index coverage: %v", planQuestions)
	}
	result, questions := agentcontext.ResultTemplate(projected, record.RuntimeID, "2026-10-14T00:00:00Z")
	if result == nil || result["closing_assessment"] != nil || len(questions) != 5 || !strings.Contains(questions["/closing_assessment"], "copies done_when_index and plan_assertion_indexes") || !strings.Contains(questions["/closing_assessment"], "satisfied/incomplete/blocked") {
		t.Fatalf("closing Result draft/guidance missing: draft=%v questions=%v", result, questions)
	}
	for _, want := range []string{"go vet ./...", "go test ./...", "empty Worker runner contract does not waive"} {
		if !strings.Contains(questions["/summary"]+questions["/closing_assessment"], want) {
			t.Errorf("Result guidance omitted TASK self-check instruction %q: %v", want, questions)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(result, encoded); err != nil {
		t.Fatalf("blank closing assessment draft failed frozen tuple validation: %v", err)
	}
	var projectedDraft map[string]any
	if err := json.Unmarshal(encoded, &projectedDraft); err != nil {
		t.Fatal(err)
	}
	projectedDraft["closing_assessment"] = map[string]any{
		"disposition": nil,
		"conditions": []any{
			map[string]any{"done_when_index": 0, "judgment": nil, "plan_assertion_indexes": []any{0}, "rationale": nil},
			map[string]any{"done_when_index": 1, "judgment": nil, "plan_assertion_indexes": []any{1}, "rationale": nil},
		},
	}
	projectedBytes, err := json.Marshal(projectedDraft)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(result, projectedBytes); err != nil {
		t.Fatalf("system-projected closing scaffold with explicit blank professional fields failed: %v", err)
	}
	var authored map[string]any
	if err := json.Unmarshal(encoded, &authored); err != nil {
		t.Fatal(err)
	}
	authored["closing_assessment"] = map[string]any{
		"disposition": "incomplete",
		"conditions": []any{map[string]any{
			"done_when_index": float64(1), "judgment": "incomplete",
			"plan_assertion_indexes": []any{float64(0)}, "rationale": "This condition remains in progress.",
		}},
	}
	encoded, err = json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(result, encoded); err != nil {
		t.Fatalf("editable closing-assessment object rejected: %v", err)
	}
	authored["closing_assessment"].(map[string]any)["machine_check"] = "PASS"
	encoded, err = json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(result, encoded); err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("editable closing assessment accepted an unknown nested claim: %v", err)
	}
}

func TestResultQuestionsMatchOnlyExactLegacySummaryAndClosingQuestions(t *testing.T) {
	record := contextRecord()
	record.ResponsibilityID = "BUILD-WORK-PACKAGE"
	record.RequiredChecks = []string{"go vet ./...", "go test ./..."}
	record.DoneWhen = []string{"owned files are written"}
	record.IntegrationCheckMode = "post_merge"
	record.ClosingContract = &assignmentrecord.ClosingContract{Contract: assignmentrecord.ClosingContractVersion, WorkerChecks: []assignmentrecord.WorkerCheck{}}
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	_, current := agentcontext.ResultTemplate(projected, record.RuntimeID, "2026-10-14T00:00:00Z")
	for _, want := range []string{"go vet ./...", "go test ./...", "If a command was not run", "Main's integration rerun is separate"} {
		if !strings.Contains(current["/summary"], want) {
			t.Fatalf("current Result summary omitted %q: %s", want, current["/summary"])
		}
	}
	if !strings.Contains(current["/closing_assessment"], "empty Worker runner contract does not waive") {
		t.Fatalf("empty Worker runner contract incorrectly waives TASK self-check: %s", current["/closing_assessment"])
	}

	clone := func(in map[string]string) map[string]string {
		out := make(map[string]string, len(in))
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	legacy := clone(current)
	legacy["/summary"] = "Summarize the actual delivery for the registered TASK after reading its current accepted PLAN_REPORT."
	legacy["/closing_assessment"] = "Give disposition complete, incomplete, or blocked. The saved Result draft copies done_when_index and plan_assertion_indexes from the accepted PLAN mapping; preserve those indexes and fill each condition's judgment satisfied/incomplete/blocked and a non-empty rationale. Complete requires every judgment satisfied; incomplete or blocked must identify the corresponding unsatisfied judgment. 0: owned files are written The frozen contract contains no Worker runner checks; Main required checks remain separately owned."
	legacy["/message_id"] = "Keep this identity for an exact replay. Choose a new unique message_id before submitting a changed professional answer."
	if !agentcontext.ResultQuestionsMatch(current, legacy) {
		t.Fatal("exact previously frozen summary/closing questions were not accepted")
	}
	if !agentcontext.ResultQuestionsMatch(current, clone(current)) {
		t.Fatal("current command-aware questions did not match their own frozen values")
	}

	for name, mutate := range map[string]func(map[string]string){
		"modified legacy summary": func(q map[string]string) { q["/summary"] += " Report passing checks even if they were not run." },
		"modified legacy closing": func(q map[string]string) { q["/closing_assessment"] += " Skip the TASK self-check." },
		"other question changed":  func(q map[string]string) { q["/risks_blockers"] += " Add machine status." },
		"unknown question added":  func(q map[string]string) { q["/invented"] = "new protocol" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := clone(legacy)
			mutate(changed)
			if agentcontext.ResultQuestionsMatch(current, changed) {
				t.Fatal("modified or additional frozen question was accepted")
			}
		})
	}
}

func TestValidateResultDraftPreservesAnswerButRejectsTupleDriftAndMachineClaims(t *testing.T) {
	record := contextRecord()
	record.ResponsibilityID = "BUILD-WORK-PACKAGE"
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	template, _ := agentcontext.ResultTemplate(projected, record.RuntimeID, "2026-10-14T00:00:00Z")
	encoded, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(template, encoded); err != nil {
		t.Fatalf("original Result template rejected: %v", err)
	}
	var authored map[string]any
	if err := json.Unmarshal(encoded, &authored); err != nil {
		t.Fatal(err)
	}
	authored["message_id"] = "msg-result-new-answer"
	authored["summary"] = "owner answer stays editable"
	authored["assertion_evidence"] = []any{}
	authored["risks_blockers"] = []any{}
	authored["scope_deviations"] = []any{}
	encoded, err = json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentcontext.ValidateResultDraft(template, encoded); err != nil {
		t.Fatalf("valid editable answer/replay identity was rejected: %v", err)
	}

	for name, change := range map[string]func(map[string]any){
		"record tuple drift":     func(draft map[string]any) { draft["assignment_record_sha256"] = strings.Repeat("f", 64) },
		"extra machine claim":    func(draft map[string]any) { draft["checks"] = []any{"PASS"} },
		"invalid submission key": func(draft map[string]any) { draft["message_id"] = " " },
		"missing answer field":   func(draft map[string]any) { delete(draft, "assertion_evidence") },
		"unexpected closing assessment without contract": func(draft map[string]any) {
			draft["closing_assessment"] = map[string]any{"disposition": "complete"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var candidate map[string]any
			if err := json.Unmarshal(encoded, &candidate); err != nil {
				t.Fatal(err)
			}
			change(candidate)
			body, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := agentcontext.ValidateResultDraft(template, body); err == nil {
				t.Fatal("accepted editable Result with invalid tuple/field")
			}
		})
	}
}

func TestResultTemplateRejectsUnboundOrMismatchedRecordAndInvalidTime(t *testing.T) {
	record := contextRecord()
	record.ResponsibilityID = "BUILD-WORK-PACKAGE"
	projected, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	validAt := "2026-10-14T00:00:00Z"
	projected.AssignmentRecordSHA256 = strings.Repeat("z", 64)
	if draft, _ := agentcontext.ResultTemplate(projected, record.RuntimeID, validAt); draft != nil {
		t.Fatal("malformed Record SHA received a Result draft")
	}
	projected, err = agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		t.Fatal(err)
	}
	if draft, _ := agentcontext.ResultTemplate(projected, "loop-other", validAt); draft != nil {
		t.Fatal("Result draft accepted a different Runtime")
	}
	if draft, _ := agentcontext.ResultTemplate(projected, record.RuntimeID, "not-a-time"); draft != nil {
		t.Fatal("Result draft accepted invalid occurrence time")
	}
}

func TestSharedReadingOrderUsesActualPathPrefixAndOmitsSkillDuplication(t *testing.T) {
	inputs := map[string]string{"docs/requirements/REQ.md": "r", "docs/dev/contracts/BE.md": "b", "docs/dev/tasks/TASK.md": "t", ".claude/skills/agent-dispatch/SKILL.md": "s", "docs/rules/testing.md": "g"}
	native := agentcontext.InputReadingOrder(inputs, "")
	worker := agentcontext.InputReadingOrder(inputs, ".claude/inputs/")
	if len(native) != 4 || native[0] != "docs/dev/tasks/TASK.md" || native[1] != "docs/dev/contracts/BE.md" || len(worker) != 4 {
		t.Fatal("lost bottom-up source order")
	}
	for i := range native {
		if worker[i] != ".claude/inputs/"+native[i] {
			t.Fatal("entrances changed source order")
		}
	}
}

func contextRecord() assignmentrecord.Record {
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	hashC := strings.Repeat("c", 64)
	hashD := strings.Repeat("d", 64)
	manifest := assignmentrecord.ContextSource{Kind: "manifest", Ref: "docs/workgroups/s6/manifest.json", ID: "team-manifest", Version: "v1.2.0", SHA256: hashA}
	task := assignmentrecord.ContextSource{Kind: "task", Ref: "docs/dev/tasks/TASK-001.md", ID: "TASK-001", Version: "v1.0.0", SHA256: hashB}
	role := assignmentrecord.ContextSource{Kind: "agent_definition", Ref: ".claude/agents/backend-builder.md", ID: "backend-builder", SHA256: hashC}
	skill := assignmentrecord.ContextSource{Kind: "authorized_skill", Ref: ".claude/skills/backend-engineering/SKILL.md", ID: "backend-engineering", Version: "v1.0.0", SHA256: hashD}
	return assignmentrecord.Record{
		SchemaVersion: assignmentrecord.AssignmentRecordSchema, Contract: assignmentrecord.AssignmentRecordContract,
		RuntimeID: "loop-context-test", BaselineGeneration: 4, RegistrationRevision: 12,
		AssignmentID: "assignment-context-test", Revision: 3, Task: task, Manifest: manifest, AgentDefinition: role,
		TeamID: "workgroup-context-test", ResponsibilityID: "BUILD-CONTEXT", OwnerAgentID: "agent-s6-context",
		RoleFamily: "backend-builder", DispatchMode: "plan_checkpoint",
		ReadPaths: []string{"docs/dev/tasks/TASK-001.md"}, SkillRefs: []string{"backend-engineering"},
		DependsOn: []string{"assignment-prerequisite"}, DoneWhen: []string{"owned files are written"},
		RequiredChecks: []string{"go test ./internal/agentcontext"}, CheckProfiles: []checkexec.Profile{},
		ScopeBasis: assignmentrecord.ScopeBasis{
			ManifestScope: []string{"internal/"}, ManifestWritePaths: []string{"internal/component/"},
			ManifestOutputPaths: []string{}, TaskWritePaths: []string{"internal/"},
		},
		EffectiveWritePaths: []string{"internal/component"},
		ContextSources:      []assignmentrecord.ContextSource{role, skill, manifest, task},
	}
}
