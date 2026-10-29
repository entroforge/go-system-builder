package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
)

func TestCanonicalAssignmentRecordWorkspacePlanFreezeAndDecode(t *testing.T) {
	f := newAssignmentRecordWorkspaceFixture(t)
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatalf("Plan canonical Assignment: %v", err)
	}
	if e.AssignmentRecord == nil || *e.AssignmentRecord != f.record.Reference() || e.AssignmentRecordSHA256 != digestAssignmentRecord(t, f.record) {
		t.Fatalf("Plan did not bind the original Record: %+v", e)
	}
	assertRecordSourcesFrozen(t, e.Inputs, f.record)
	if err := ValidateAssignmentRecord(f.state, e); err == nil {
		t.Fatal("execution passed authority validation before the original CheckProfiles were frozen")
	}

	// The existing CLI sequence is Plan -> FreezeCheckProfiles -> Writer
	// ValidateAuthority. The Record is already authoritative at Plan time, but
	// its declared runner profile is attached before the execution is committed.
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatalf("FreezeCheckProfiles: %v", err)
	}
	if err := ValidateAssignmentRecord(f.state, e); err != nil {
		t.Fatalf("execution differs from the original Record after profile freeze: %v", err)
	}
	f.binding.Executions[e.AssignmentID] = e
	if err := f.binding.ValidateAuthority(f.state); err != nil {
		t.Fatalf("Writer authority validation after profile freeze: %v", err)
	}

	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, f.record)
	if err != nil {
		t.Fatalf("project canonical Dispatch: %v", err)
	}
	e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatalf("FreezeDispatchContext: %v", err)
	}
	f.binding.Executions[e.AssignmentID] = e
	if err := f.binding.ValidateAuthority(f.state); err != nil {
		t.Fatalf("Writer authority validation after context freeze: %v", err)
	}
	ctx, err := f.binding.ReadDispatchContext(e)
	if err != nil {
		t.Fatalf("ReadDispatchContext: %v", err)
	}
	if ctx.Dispatch.AssignmentRecord == nil || *ctx.Dispatch.AssignmentRecord != *e.AssignmentRecord || ctx.Dispatch.AssignmentRecordSHA256 != e.AssignmentRecordSHA256 {
		t.Fatalf("frozen Dispatch context changed the canonical Record binding: %+v", ctx.Dispatch)
	}
	nonDefaultResponsibility := ctx
	nonDefaultResponsibility.Dispatch.ResponsibilityIDs = []string{"BUILD-REGISTERED-SPECIALTY"}
	if !canonicalS6BuilderCommitDraft(e, nonDefaultResponsibility) {
		t.Fatal("canonical S6 Builder with a non-default responsibility did not qualify for the commit draft and guidance")
	}
	if !reflect.DeepEqual(ctx.ProductWritePaths, e.WritePaths) || !reflect.DeepEqual(ctx.RequiredChecks, e.Checks) {
		t.Fatal("frozen Dispatch context changed the canonical scope/check projection")
	}
	assertRecordSourcesFrozen(t, ctx.FrozenInputs, f.record)
	for _, skill := range ctx.Selection.Selected {
		if ctx.FrozenInputs[skill.Source] != skill.SHA256 || e.Inputs[skill.Source] != skill.SHA256 {
			t.Fatalf("selected installed Skill was not frozen: %s", skill.Source)
		}
	}
	if ctx.PlanDraft == nil || ctx.PlanDraft["assignment_record_contract"] != assignmentrecord.AssignmentRecordContract || ctx.PlanDraft["assignment_record_sha256"] != e.AssignmentRecordSHA256 || ctx.PlanDraft["assignment_revision"] != float64(f.record.Revision) {
		t.Fatalf("PLAN draft did not project its Record binding: %#v", ctx.PlanDraft)
	}
	if ctx.PlanDraft["objective"] != nil || len(ctx.PlanDraft["planned_paths"].([]any)) != 0 || len(ctx.PlanDraft["assertion_checks"].([]any)) != 0 || ctx.PlanDraft["risks_blockers"] != nil {
		t.Fatalf("workspace auto-filled a professional PLAN answer: %#v", ctx.PlanDraft)
	}
	wantDependencies, _ := json.Marshal(append([]string{}, f.record.DependsOn...))
	gotDependencies, _ := json.Marshal(ctx.PlanDraft["dependencies"])
	if !bytes.Equal(gotDependencies, wantDependencies) || !strings.Contains(ctx.PlanQuestions["/dependencies"], "workgroup-local") {
		t.Fatalf("workspace did not deliver the registered dependency set: %s; %v", gotDependencies, ctx.PlanQuestions)
	}
	if _, exists := ctx.PlanDraft["steps"]; exists {
		t.Fatal("canonical thin PLAN draft still contains steps")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", mustJSONAssignmentRecord(t, ctx.PlanDraft)); err == nil {
		t.Fatal("unanswered professional PLAN draft unexpectedly validated as a completed PLAN_REPORT")
	}
	if ctx.ResultDraft == nil || ctx.ResultPath != ResultDraftPath || len(ctx.ResultQuestions) != 4 || !ctx.ResultDraftEditable {
		t.Fatalf("canonical Builder did not get the original completion transport draft: path=%q questions=%v", ctx.ResultPath, ctx.ResultQuestions)
	}
	if !strings.Contains(ctx.Instruction, "before saving result_path") || !strings.Contains(ctx.Instruction, "Saving Result calls the original report adapter automatically") || !strings.Contains(ctx.Instruction, "Keep expected_head unchanged; fill only message and paths") || strings.Contains(ctx.Instruction, "Reporting closes product-write and commit permission") {
		t.Fatal("canonical context omitted automatic Result reception or retained tool-permission wording")
	}
	for _, field := range []string{"summary", "assertion_evidence", "risks_blockers", "scope_deviations"} {
		if ctx.ResultDraft[field] != nil || ctx.ResultQuestions["/"+field] == "" {
			t.Fatalf("workspace Result filled or omitted professional field %s: draft=%v questions=%v", field, ctx.ResultDraft, ctx.ResultQuestions)
		}
	}
	for _, forbidden := range []string{"plan_ref", "plan_sha256", "checks", "changed_paths", "status"} {
		if _, exists := ctx.ResultDraft[forbidden]; exists {
			t.Errorf("workspace Result draft invented %s", forbidden)
		}
	}

	for _, test := range []struct {
		name   string
		change func(*Execution)
	}{
		{name: "scope drift", change: func(e *Execution) { e.WritePaths = []string{"internal/elsewhere"} }},
		{name: "required checks drift", change: func(e *Execution) { e.Checks = []string{"go test ./..."} }},
		{name: "owner drift", change: func(e *Execution) { e.AgentID = "agent-s6-other" }},
		{name: "revision drift", change: func(e *Execution) { ref := *e.AssignmentRecord; ref.Revision++; e.AssignmentRecord = &ref }},
		{name: "digest drift", change: func(e *Execution) { e.AssignmentRecordSHA256 = hashAssignmentRecord([]byte("other canonical Record")) }},
		{name: "CheckProfile drift", change: func(e *Execution) { e.CheckProfiles[0].Version = "v9" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := e
			mutated.WritePaths = append([]string{}, e.WritePaths...)
			mutated.Checks = append([]string{}, e.Checks...)
			mutated.CheckProfiles = append([]checkexec.Profile{}, e.CheckProfiles...)
			if e.AssignmentRecord != nil {
				ref := *e.AssignmentRecord
				mutated.AssignmentRecord = &ref
			}
			test.change(&mutated)
			if err := ValidateAssignmentRecord(f.state, mutated); err == nil {
				t.Fatal("drifted execution unexpectedly passed canonical authority validation")
			}
			candidate := *f.binding
			candidate.Executions = map[string]Execution{mutated.AssignmentID: mutated}
			if err := candidate.ValidateAuthority(f.state); err == nil {
				t.Fatal("Writer authority validation accepted a drifted canonical execution")
			}
		})
	}

	registry := *f.binding
	registry.Executions = map[string]Execution{e.AssignmentID: e}
	stateWithWorkspace := cloneAssignmentRecordMap(t, f.state)
	stateWithWorkspace["workspace"] = Encode(&registry)
	decoded, err := Decode(stateWithWorkspace)
	if err != nil {
		t.Fatalf("Decode valid original execution binding: %v", err)
	}
	decodedExecution, ok := decoded.Execution(e.AssignmentID, e.RuntimeID, e.BaselineGeneration)
	if !ok || decodedExecution.AssignmentRecord == nil || *decodedExecution.AssignmentRecord != *e.AssignmentRecord || decodedExecution.AssignmentRecordSHA256 != e.AssignmentRecordSHA256 {
		t.Fatalf("Decode lost original canonical binding: %+v", decodedExecution)
	}

	changedState := cloneAssignmentRecordMap(t, stateWithWorkspace)
	workspaceRow := changedState["workspace"].(map[string]any)
	executions := workspaceRow["executions"].(map[string]any)
	executionRow := executions[e.AssignmentID].(map[string]any)
	executionRow["assignment_record_sha256"] = hashAssignmentRecord([]byte("tampered"))
	if _, err := Decode(changedState); err == nil {
		t.Fatal("Decode accepted a modified canonical Record digest")
	}
}

func TestCanonicalResultDraftBootstrapKeepsAuthorBytesOnRetry(t *testing.T) {
	f := newAssignmentRecordWorkspaceFixture(t)
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, f.record)
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		".claude/agents/backend-builder.md":           "---\nname: backend-builder\ndescription: fixture\n---\nBuild the registered TASK.\n",
		".claude/skills/agent-dispatch/SKILL.md":      "# dispatch fixture\n",
		".claude/skills/backend-engineering/SKILL.md": "# backend fixture\n",
		".claude/skills/code-quality/SKILL.md":        "# quality fixture\n",
		".claude/skills/testing-strategy/SKILL.md":    "# test fixture\n",
	} {
		full := filepath.Join(f.binding.MainRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.binding.Materialize(f.ctx, e); err != nil {
		t.Fatalf("materialize canonical Builder execution: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err != nil {
		t.Fatalf("bootstrap canonical Builder execution: %v", err)
	}
	path := filepath.Join(e.Path, filepath.FromSlash(ResultDraftPath))
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original completion draft: %v", err)
	}
	var authored map[string]any
	if err := json.Unmarshal(initial, &authored); err != nil {
		t.Fatal(err)
	}
	authored["summary"] = "owner-authored result must survive a bootstrap retry"
	authored["assertion_evidence"] = []any{map[string]any{"assertion": "authored assertion", "oracle": "authored oracle", "evidence_refs": []any{"internal/result.txt"}}}
	authored["risks_blockers"] = []any{}
	authored["scope_deviations"] = []any{}
	authoredBytes, err := json.MarshalIndent(authored, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	authoredBytes = append(authoredBytes, '\n')
	if err := os.WriteFile(path, authoredBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err != nil {
		t.Fatalf("bootstrap retry overwrote or rejected editable Result: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, authoredBytes) {
		t.Fatalf("bootstrap retry changed owner Result bytes: err=%v", err)
	}
	if err := f.binding.ValidateBootstrap(e); err != nil {
		t.Fatalf("editable Result was incorrectly treated as an immutable bootstrap asset: %v", err)
	}
	var tampered map[string]any
	if err := json.Unmarshal(actual, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["assignment_revision"] = f.record.Revision + 1
	tamperedBytes, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tamperedBytes = append(tamperedBytes, '\n')
	if err := os.WriteFile(path, tamperedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err == nil {
		t.Fatal("bootstrap accepted an editable Result from another Record revision")
	}
	afterRejectedRetry, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(afterRejectedRetry, tamperedBytes) {
		t.Fatalf("bootstrap retry rewrote a tuple-drifted Result: %v", err)
	}
}

func TestClosingContractWorkspaceContextKeepsEmptyWorkerChecksSeparateFromMainChecks(t *testing.T) {
	f := newClosingAssignmentRecordWorkspaceFixture(t)
	if f.record.ClosingContract == nil || f.record.IntegrationCheckMode != "post_merge" || f.record.ClosingContract.WorkerChecks == nil || len(f.record.ClosingContract.WorkerChecks) != 0 {
		t.Fatalf("fixture did not preserve an explicit empty Worker check contract: %+v", f.record)
	}
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatalf("Plan closing-contract Assignment: %v", err)
	}
	if e.CheckLocation != "main" {
		t.Fatalf("post_merge check location was not projected from the original Record: %q", e.CheckLocation)
	}
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatalf("freeze original Main CheckProfiles: %v", err)
	}
	wrongLocation := e
	wrongLocation.CheckLocation = "worker"
	if err := ValidateAssignmentRecord(f.state, wrongLocation); err == nil {
		t.Fatal("canonical closing Record allowed Main required checks to become Worker checks")
	}
	if err := ValidateAssignmentRecord(f.state, e); err != nil {
		t.Fatalf("original Main check binding differs from closing Record: %v", err)
	}
	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, f.record)
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatalf("freeze closing-contract context: %v", err)
	}
	ctx, err := f.binding.ReadDispatchContext(e)
	if err != nil {
		t.Fatalf("read closing-contract context: %v", err)
	}
	if ctx.Dispatch.IntegrationCheckMode != "post_merge" || ctx.Dispatch.ClosingContract == nil || len(ctx.Dispatch.ClosingContract.WorkerChecks) != 0 ||
		len(ctx.RequiredChecks) != 1 || ctx.RequiredChecks[0] != f.record.RequiredChecks[0] || ctx.CheckProfiles[0].Location != "main" {
		t.Fatalf("Main/Worker check sets were conflated in context: %+v", ctx)
	}
	if !strings.Contains(ctx.PlanQuestions["/assertion_checks"], "done_when_indexes") ||
		!strings.Contains(ctx.PlanQuestions["/assertion_checks"], "0: "+f.record.DoneWhen[0]) {
		t.Fatalf("PLAN question omits complete DoneWhen mapping: %v", ctx.PlanQuestions)
	}
	if ctx.ResultDraft["closing_assessment"] != nil || ctx.ResultQuestions["/closing_assessment"] == "" || len(ctx.ResultQuestions) != 5 {
		t.Fatalf("closing assessment is not an unanswered, guided professional field: draft=%v questions=%v", ctx.ResultDraft, ctx.ResultQuestions)
	}
	if !strings.Contains(ctx.Instruction, "condition judgments and rationales") || !strings.Contains(ctx.Instruction, "closing assessment's DoneWhen/PLAN indexes") {
		t.Fatalf("workspace instructions omit the closing answer contract: %s", ctx.Instruction)
	}
}

func TestClosingResultDraftBootstrapPreservesAuthorBytesAndRejectsUnknownFields(t *testing.T) {
	f := newClosingAssignmentRecordWorkspaceFixture(t)
	e, err := f.binding.Plan(f.ctx, f.state, f.record.AssignmentID, f.record.OwnerAgentID, f.record.EffectiveWritePaths, f.record.RequiredChecks, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatalf("Plan closing-contract Assignment: %v", err)
	}
	e, err = f.binding.FreezeCheckProfiles(e, f.record.CheckProfiles)
	if err != nil {
		t.Fatalf("freeze Main CheckProfiles: %v", err)
	}
	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, f.record)
	if err != nil {
		t.Fatal(err)
	}
	e, err = f.binding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatalf("freeze closing dispatch context: %v", err)
	}
	for path, body := range map[string]string{
		".claude/agents/backend-builder.md":           "---\nname: backend-builder\ndescription: fixture\n---\nBuild the registered TASK.\n",
		".claude/skills/agent-dispatch/SKILL.md":      "# dispatch fixture\n",
		".claude/skills/backend-engineering/SKILL.md": "# backend fixture\n",
		".claude/skills/code-quality/SKILL.md":        "# quality fixture\n",
		".claude/skills/testing-strategy/SKILL.md":    "# test fixture\n",
	} {
		full := filepath.Join(f.binding.MainRoot, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.binding.Materialize(f.ctx, e); err != nil {
		t.Fatalf("materialize closing execution: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err != nil {
		t.Fatalf("bootstrap closing execution: %v", err)
	}
	commitPath := filepath.Join(e.Path, filepath.FromSlash(CommitRequestPath))
	commitDraftBytes, err := os.ReadFile(commitPath)
	if err != nil {
		t.Fatalf("read bootstrap commit request draft: %v", err)
	}
	var commitDraft CommitRequest
	if err := json.Unmarshal(commitDraftBytes, &commitDraft); err != nil {
		t.Fatalf("decode bootstrap commit request draft: %v", err)
	}
	if commitDraft.ExpectedHead != e.BaseCommit || commitDraft.Message != "" || commitDraft.Paths == nil || len(commitDraft.Paths) != 0 {
		t.Fatalf("bootstrap did not supply only the registered starting HEAD: draft=%+v base=%s", commitDraft, e.BaseCommit)
	}
	authoredCommit, err := json.MarshalIndent(CommitRequest{ExpectedHead: commitDraft.ExpectedHead, Message: "Preserve the existing S6 commit request", Paths: []string{e.WritePaths[0]}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	authoredCommit = append(authoredCommit, '\n')
	if err := os.WriteFile(commitPath, authoredCommit, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Path, filepath.FromSlash(ResultDraftPath))
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read closing Result draft: %v", err)
	}
	var authored map[string]any
	if err := json.Unmarshal(initial, &authored); err != nil {
		t.Fatal(err)
	}
	if authored["closing_assessment"] != nil {
		t.Fatalf("bootstrap prefilled professional closing judgment: %v", authored["closing_assessment"])
	}
	authored["summary"] = "The registered implementation is delivered with a condition-level assessment."
	authored["assertion_evidence"] = []any{map[string]any{"assertion": "The owned condition is satisfied.", "oracle": "Inspect the authored implementation and evidence.", "evidence_refs": []any{"internal/workspace/result.txt"}}}
	authored["risks_blockers"], authored["scope_deviations"] = []any{}, []any{}
	authored["closing_assessment"] = map[string]any{
		"disposition": "complete",
		"conditions": []any{map[string]any{
			"done_when_index": float64(0), "judgment": "satisfied", "plan_assertion_indexes": []any{float64(0)},
			"rationale": "The reviewed behavior and its cited evidence satisfy the frozen condition.",
		}},
	}
	authoredBytes, err := json.MarshalIndent(authored, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	authoredBytes = append(authoredBytes, '\n')
	if err := os.WriteFile(path, authoredBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err != nil {
		t.Fatalf("bootstrap retry rejected or rewrote the author's closing answer: %v", err)
	}
	commitAfterRetry, err := os.ReadFile(commitPath)
	if err != nil || !bytes.Equal(commitAfterRetry, authoredCommit) {
		t.Fatalf("bootstrap retry overwrote the existing commit request: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, authoredBytes) {
		t.Fatalf("bootstrap retry changed authored closing Result bytes: %v", err)
	}
	if err := f.binding.ValidateBootstrap(e); err != nil {
		t.Fatalf("editable closing Result leaked into the immutable bootstrap receipt: %v", err)
	}
	authored["closing_assessment"].(map[string]any)["worker_status"] = "PASS"
	tampered, err := json.MarshalIndent(authored, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tampered = append(tampered, '\n')
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.binding.Bootstrap(e, executable); err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("bootstrap accepted a nested machine claim in closing_assessment: %v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, tampered) {
		t.Fatalf("rejected closing Result was rewritten: %v", err)
	}
}

func TestCanonicalRecordOptInLeavesLegacyWorkspaceExecutionUnbound(t *testing.T) {
	f := newAssignmentRecordWorkspaceFixture(t)
	legacyState := cloneAssignmentRecordMap(t, f.state)
	entities := legacyState["entities"].(map[string]any)
	delete(entities, "assignments")
	legacyBinding := *f.binding
	legacyBinding.Executions = map[string]Execution{}

	e, err := legacyBinding.Plan(f.ctx, legacyState, "assignment-legacy", "agent-legacy", []string{"legacy/"}, []string{}, []string{f.record.Task.Ref})
	if err != nil {
		t.Fatalf("legacy Plan: %v", err)
	}
	if e.AssignmentRecord != nil || e.AssignmentRecordSHA256 != "" {
		t.Fatalf("legacy execution acquired a synthetic canonical Record: %+v", e)
	}
	if err := ValidateAssignmentRecord(legacyState, e); err != nil {
		t.Fatalf("legacy execution changed under optional Record validation: %v", err)
	}

	dispatch := agentcontext.Assignment{
		AssignmentID: "assignment-legacy", Owner: "agent-legacy", TaskID: f.record.Task.ID,
		TeamID: f.record.TeamID, Role: f.record.RoleFamily, AgentDefinitionRef: f.record.AgentDefinition.Ref,
		ManifestRef: f.record.Manifest.Ref, DispatchMode: "plan_checkpoint", ResponsibilityIDs: []string{f.record.ResponsibilityID},
		AuthorizationSkillRefs: []string{"backend-engineering"}, DependsOn: []string{},
	}
	e, err = legacyBinding.FreezeDispatchContext(e, dispatch, nil)
	if err != nil {
		t.Fatalf("legacy Dispatch context: %v", err)
	}
	ctx, err := legacyBinding.ReadDispatchContext(e)
	if err != nil {
		t.Fatalf("read legacy Dispatch context: %v", err)
	}
	if _, hasMarker := ctx.PlanDraft["assignment_record_contract"]; hasMarker {
		t.Fatal("legacy PLAN template acquired the S6 Record marker")
	}
	if _, hasSteps := ctx.PlanDraft["steps"]; !hasSteps {
		t.Fatal("legacy PLAN template lost its existing steps field")
	}
}

type assignmentRecordWorkspaceFixture struct {
	binding *ExecutionRegistry
	ctx     context.Context
	state   map[string]any
	record  assignmentrecord.Record
}

func newAssignmentRecordWorkspaceFixture(t *testing.T) *assignmentRecordWorkspaceFixture {
	return newAssignmentRecordWorkspaceFixtureWithContract(t, false, false)
}

func newClosingAssignmentRecordWorkspaceFixture(t *testing.T) *assignmentRecordWorkspaceFixture {
	return newAssignmentRecordWorkspaceFixtureWithContract(t, true, false)
}

func newOwnedCheckAssignmentWorkspaceFixture(t *testing.T) *assignmentRecordWorkspaceFixture {
	return newAssignmentRecordWorkspaceFixtureWithContract(t, true, true)
}

func newAssignmentRecordWorkspaceFixtureWithContract(t *testing.T, withClosing, withWorkerCheck bool) *assignmentRecordWorkspaceFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "s6-record-test"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}} {
		if _, err := Git(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n.worktrees/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input.md"), []byte("legacy input\n"), 0600); err != nil {
		t.Fatal(err)
	}

	const (
		runtimeID        = "loop-workspace-record-test"
		teamID           = "workgroup-workspace-record-test"
		owner            = "agent-workspace-record-test"
		assignmentID     = "assignment-workspace-record-test"
		taskID           = "TASK-042-01"
		manifestRef      = "docs/workgroups/s6/manifest.json"
		taskRef          = "docs/dev/tasks/TASK-042-01.md"
		roleRef          = "agents/backend-builder.md"
		skillRef         = "skills/backend-engineering/SKILL.md"
		dispatchSkillRef = "skills/agent-dispatch/SKILL.md"
		testingSkillRef  = "skills/testing-strategy/SKILL.md"
		qualitySkillRef  = "skills/code-quality/SKILL.md"
		dependencyRef    = "checks/profile-dependency.txt"
		fixtureRef       = "checks/profile-fixture.json"
	)
	taskBytes := []byte("# TASK-042-01\n\n> Version: v1.0.0\n\nImplement the registered workspace behavior.\n")
	roleBytes := []byte("test fixture backend-builder source\n")
	skillBytes := []byte("test fixture backend-engineering source\n")
	dispatchSkillBytes := []byte("test fixture agent-dispatch source\n")
	testingSkillBytes := []byte("test fixture testing-strategy source\n")
	qualitySkillBytes := []byte("test fixture code-quality source\n")
	dependencyBytes := []byte("check profile dependency\n")
	fixtureBytes := []byte("{\"fixture\":true}\n")
	taskSHA := hashAssignmentRecord(taskBytes)
	roleSHA := hashAssignmentRecord(roleBytes)
	skillSHA := hashAssignmentRecord(skillBytes)
	dependencySHA := hashAssignmentRecord(dependencyBytes)
	fixtureSHA := hashAssignmentRecord(fixtureBytes)
	profile := checkexec.Profile{
		ID: "profile-workspace-check", Version: "1.0.0", CheckIndex: 0, Command: "go test ./internal/workspace",
		Location: map[bool]string{true: "main", false: "worker"}[withClosing], TimeoutSeconds: 60, EnvironmentPolicy: "inherited", Environment: []checkexec.EnvFact{},
		Dependencies: []checkexec.FileRef{{Path: dependencyRef, SHA256: dependencySHA}},
		Fixtures:     []checkexec.FileRef{{Path: fixtureRef, SHA256: fixtureSHA}},
		InputClosure: "whole_tree", SideEffects: "unknown", ReusePolicy: "never",
	}
	workerChecks := []assignmentrecord.WorkerCheck{}
	if withWorkerCheck {
		workerProfile := checkexec.Profile{
			ID: "profile-workspace-owned-check", Version: "1.0.0", CheckIndex: 0, Command: "true",
			Location: "worker", TimeoutSeconds: 10, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{},
			Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree",
			SideEffects: "none", ReusePolicy: "same_input_same_responsibility",
		}
		workerChecks = []assignmentrecord.WorkerCheck{{Index: 0, Command: workerProfile.Command, Profile: workerProfile}}
	}
	assignmentManifest := map[string]any{
		"assignment_id": assignmentID, "responsibility_id": "BUILD-WORK-PACKAGE", "role_family": "backend-builder",
		"agent_id": owner, "agent_definition_ref": roleRef, "skill_refs": []string{"backend-engineering"},
		"read_paths": []string{"docs/dev/tasks/"}, "scope": []string{"internal/"},
		"write_paths": []string{"internal/workspace/"}, "output_paths": []string{}, "depends_on": []string{},
		"required_checks": []string{profile.Command}, "check_profiles": []checkexec.Profile{profile},
		"done_when": []string{"registered workspace behavior is implemented and checked"}, "dispatch_mode": "plan_checkpoint",
		"reuse_decision": "create", "grouping_rationale": "One workspace responsibility and one bound check profile.", "status": "planned",
	}
	if withClosing {
		assignmentManifest["integration_check_mode"] = "post_merge"
		assignmentManifest["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": workerChecks}
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-workspace-record-test", "version": "v1.2.0", "runtime_id": runtimeID,
		"req_id": "REQ-042", "baseline_generation": 3, "review_round": nil,
		"platform_team_id": "platform-workspace-record-test", "workgroup_id": teamID, "workgroup_kind": "builder", "status": "active",
		"documents": []any{map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": taskSHA}},
		"risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "builder implementation is assigned",
			"assignment_ids": []string{assignmentID}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments":      []any{assignmentManifest},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "One owner is sufficient for the bounded workspace responsibility.",
		"validation": map[string]any{
			"result": "pass", "missing_responsibilities": []string{}, "unresolved_conflicts": []string{}, "warnings": []string{},
			"validated_at": "2026-10-14T00:00:00Z",
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		t.Fatalf("fixture manifest schema: %v", err)
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		t.Fatalf("fixture manifest semantics: %v", err)
	}
	for ref, data := range map[string][]byte{
		manifestRef: manifestBytes, taskRef: taskBytes, roleRef: roleBytes, skillRef: skillBytes,
		dispatchSkillRef: dispatchSkillBytes, testingSkillRef: testingSkillBytes, qualitySkillRef: qualitySkillBytes,
		dependencyRef: dependencyBytes, fixtureRef: fixtureBytes,
	} {
		writeAssignmentRecordFile(t, root, ref, data)
	}
	if _, err := Git(ctx, root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Git(ctx, root, "commit", "-m", "canonical Assignment workspace fixtures"); err != nil {
		t.Fatal(err)
	}
	head, err := Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	bound := Binding{root, "s6-record-test", "main", head}
	saveControlState(t, root, map[string]any{"bound_req": map[string]any{"workspace": bound.Map()}})
	binding, err := New(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"bound_req":  map[string]any{"workspace": bound.Map()},
		"runtime_id": runtimeID, "revision": 12, "lifecycle": map[string]any{"state": "building"},
		"baseline": map[string]any{"generation": 3},
		"documents": []any{map[string]any{
			"id": taskID, "kind": "task", "path": taskRef, "version": "v1.0.0", "sha256": taskSHA,
			"status": "locked", "generation": 3,
		}},
		"entities": map[string]any{
			"assignments": []any{},
			"tasks":       []any{map[string]any{"id": taskID, "path": taskRef, "sha256": taskSHA, "owner_agent_ids": []string{owner}}},
			"teams":       []any{map[string]any{"id": teamID, "kind": "builder", "manifest_ref": manifestRef}},
			"agents": []any{map[string]any{
				"id": owner, "team_id": teamID, "task_ids": []string{taskID}, "role": "backend-builder",
				"definition_ref": roleRef, "dispatch_mode": "plan_checkpoint",
			}},
		},
	}
	record, err := assignmentrecord.BuildRecord(manifestBytes, manifestRef, state, taskBytes, taskID, assignmentID, []string{"internal/workspace/"}, []assignmentrecord.ContextSource{
		{Kind: "agent_definition", Ref: roleRef, ID: "backend-builder", SHA256: roleSHA},
		{Kind: "authorized_skill", Ref: skillRef, ID: "backend-engineering", Version: "1.0.0", SHA256: skillSHA},
	})
	if err != nil {
		t.Fatalf("BuildRecord: %v", err)
	}
	state["revision"] = record.RegistrationRevision
	stateEntities := state["entities"].(map[string]any)
	stateEntities["assignments"] = []any{assignmentRecordMap(t, record)}
	return &assignmentRecordWorkspaceFixture{binding: binding, ctx: ctx, state: state, record: record}
}

func assertRecordSourcesFrozen(t *testing.T, frozen map[string]string, record assignmentrecord.Record) {
	t.Helper()
	for _, source := range record.ContextSources {
		if strings.Contains(source.Ref, "#") {
			continue
		}
		if frozen[source.Ref] != source.SHA256 {
			t.Errorf("canonical source is not frozen: %s got=%s want=%s", source.Ref, frozen[source.Ref], source.SHA256)
		}
	}
}

func assignmentRecordMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func cloneAssignmentRecordMap(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func mustJSONAssignmentRecord(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func digestAssignmentRecord(t *testing.T, record assignmentrecord.Record) string {
	t.Helper()
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func hashAssignmentRecord(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeAssignmentRecordFile(t *testing.T, root, ref string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
