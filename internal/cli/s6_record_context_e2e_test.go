package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// This exercises the original Main CLI chain from an explicit S6 manifest
// opt-in through the native context projection and the existing PLAN_REPORT
// reception. Installed Agent and Skill bytes remain disk sources in .claude;
// the locked REQ, design, contract, TASK, and dispatch index are Git sources.
func TestS6NativeAssignmentContextPlanBeginUsesCanonicalRecord(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	beforeRegistration := stageSnapshot(t, root)
	if beforeRegistration.State["schema_version"] != "1.2.0" || beforeRegistration.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatalf("fixture is not the original building 1.2 Main Runtime: schema=%v lifecycle=%v", beforeRegistration.State["schema_version"], beforeRegistration.State["lifecycle"])
	}
	boundREQ := beforeRegistration.State["bound_req"].(map[string]any)
	boundREQPath := boundREQ["path"].(string)
	boundREQBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(boundREQPath)))
	if err != nil || workpacket.Digest(boundREQBytes) != boundREQ["sha256"] {
		t.Fatalf("original bound REQ bytes do not match Runtime: %v", err)
	}
	bindingEvent := s6RecordContextJournalEvent(t, root, 0)
	runtimeID := beforeRegistration.State["runtime_id"].(string)
	bindingEventID, _ := bindingEvent["event_id"].(string)
	bindingIdempotency, _ := bindingEvent["idempotency_key"].(string)
	if bindingEvent["runtime_id"] != runtimeID || !strings.HasPrefix(bindingEventID, "evt-req-workspace-") ||
		!strings.HasPrefix(bindingIdempotency, "runtime:req-workspace:"+runtimeID+":") || bindingEvent["transition_id"] != "REQ-WORKSPACE-BIND" {
		t.Fatalf("original Main workspace binding journal does not bind event/idempotency/Runtime: %v", bindingEvent)
	}
	boundWorkspace := boundREQ["workspace"].(map[string]any)
	head, err := workspace.Git(context.Background(), root, "rev-parse", "refs/heads/test-development")
	if err != nil {
		t.Fatal(err)
	}
	if boundWorkspace["project_root"] != root || boundWorkspace["dev_branch"] != "test-development" || boundWorkspace["bound_commit"] != head {
		t.Fatalf("req workspace CLI did not bind the actual Main Git commit: %v", boundWorkspace)
	}
	var lockedTask bool
	for _, raw := range beforeRegistration.State["documents"].([]any) {
		doc := raw.(map[string]any)
		if doc["id"] == taskID && doc["kind"] == "task" && doc["path"] == taskPath && doc["status"] == "locked" && doc["generation"] == float64(1) {
			lockedTask = true
		}
	}
	if !lockedTask {
		t.Fatal("building Main is missing the locked reviewed TASK registration")
	}

	stageCLI(t, "runtime", "register-workgroup", "--root", root,
		"--manifest", ".claude/workgroups/manifest-s6-native-context.json",
		"--task-id", taskID, "--task", taskPath)
	registered := stageSnapshot(t, root)
	if registered.State["schema_version"] != "1.2.0" || registered.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatalf("registration left the original S6 authority: %v / %v", registered.State["schema_version"], registered.State["lifecycle"])
	}
	record, found, err := assignmentrecord.ForOwner(registered.State, "agent-s6-native-context")
	if err != nil || !found {
		t.Fatalf("original registration did not persist the canonical Assignment Record: found=%v err=%v", found, err)
	}
	if record.Task.ID != taskID || record.Task.Ref != taskPath || record.OwnerAgentID != "agent-s6-native-context" || record.Contract != assignmentrecord.AssignmentRecordContract {
		t.Fatalf("canonical Record lost TASK/owner/contract binding: %+v", record)
	}
	recordDigest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if record.RegistrationRevision != registered.Revision {
		t.Fatalf("Record registration revision=%d, current Main revision=%d", record.RegistrationRevision, registered.Revision)
	}

	output := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID)
	var binding struct {
		Version    int                    `json:"version"`
		Generation int                    `json:"generation"`
		Context    agentcontext.Reference `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(output), &binding); err != nil {
		t.Fatalf("decode native context binding: %v\n%s", err, output)
	}
	if binding.Version != 1 || binding.Generation != 1 || binding.Context.Path == "" {
		t.Fatalf("invalid original native context binding: %+v", binding)
	}
	contextBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(binding.Context.Path)))
	if err != nil || workpacket.Digest(contextBytes) != binding.Context.SHA256 {
		t.Fatalf("native context bytes do not match the committed binding: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", contextBytes); err != nil {
		t.Fatalf("native context schema: %v", err)
	}
	var contextView struct {
		RuntimeID       string                  `json:"runtime_id"`
		Generation      int                     `json:"baseline_generation"`
		Dispatch        agentcontext.Assignment `json:"dispatch"`
		PlanDraft       map[string]any          `json:"plan_draft"`
		Questions       map[string]string       `json:"plan_questions"`
		PlanPath        string                  `json:"plan_draft_path"`
		ResultDraft     map[string]any          `json:"result_draft"`
		ResultQuestions map[string]string       `json:"result_questions"`
		ResultPath      string                  `json:"result_path"`
		ResultEditable  bool                    `json:"result_draft_editable"`
		Schema          agentcontext.Reference  `json:"plan_schema"`
	}
	if err := json.Unmarshal(contextBytes, &contextView); err != nil {
		t.Fatalf("decode native context projection: %v", err)
	}
	if contextView.RuntimeID != record.RuntimeID || contextView.Generation != record.BaselineGeneration {
		t.Fatalf("context does not bind the Record Runtime/baseline: %+v", contextView)
	}
	if contextView.Dispatch.AssignmentRecord == nil || *contextView.Dispatch.AssignmentRecord != record.Reference() ||
		contextView.Dispatch.AssignmentRevision != record.Revision || contextView.Dispatch.AssignmentRecordSHA256 != recordDigest {
		t.Fatalf("native Dispatch differs from canonical Record reference/revision/digest: dispatch=%+v record=%+v sha=%s", contextView.Dispatch, record.Reference(), contextView.Dispatch.AssignmentRecordSHA256)
	}
	if contextView.Dispatch.Owner != record.OwnerAgentID || contextView.Dispatch.TaskID != record.Task.ID || contextView.Dispatch.TeamID != record.TeamID ||
		contextView.Dispatch.Role != record.RoleFamily || contextView.Dispatch.ManifestRef != record.Manifest.Ref {
		t.Fatalf("native Dispatch projected a different registered assignment: %+v", contextView.Dispatch)
	}
	wantResultPath := filepath.ToSlash(filepath.Join(".claude/evidence", record.RuntimeID, "g1", "assignments", record.OwnerAgentID, "a1", "completion.json"))
	if contextView.ResultPath != wantResultPath || contextView.ResultDraft == nil || len(contextView.ResultQuestions) != 4 || !contextView.ResultEditable {
		t.Fatalf("native context omitted the original owner Result draft: path=%q questions=%v", contextView.ResultPath, contextView.ResultQuestions)
	}
	wantResultTuple := map[string]any{
		"runtime_id": record.RuntimeID, "baseline_generation": record.BaselineGeneration,
		"assignment_id": record.AssignmentID, "assignment_revision": record.Revision,
		"assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"assignment_record_sha256":   recordDigest,
		"agent_id":                   record.OwnerAgentID,
		"task_id":                    record.Task.ID,
		"team_id":                    record.TeamID,
	}
	gotResultTuple := map[string]any{}
	for key := range wantResultTuple {
		gotResultTuple[key] = contextView.ResultDraft[key]
	}
	if !s6SameJSONValue(gotResultTuple, wantResultTuple) {
		t.Fatalf("native Result draft differs from the actual original Record tuple: %v", contextView.ResultDraft)
	}
	for _, field := range []string{"summary", "assertion_evidence", "risks_blockers", "scope_deviations"} {
		if contextView.ResultDraft[field] != nil || contextView.ResultQuestions["/"+field] == "" {
			t.Fatalf("native Result draft filled/omitted professional field %s: %v", field, contextView.ResultDraft)
		}
	}
	for _, forbidden := range []string{"plan_ref", "plan_sha256", "status", "checks", "changed_paths", "reviewed_paths"} {
		if _, exists := contextView.ResultDraft[forbidden]; exists {
			t.Errorf("native Result draft invented %s", forbidden)
		}
	}
	resultBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.ResultPath)))
	if err != nil {
		t.Fatalf("native owner Result draft was not published: %v", err)
	}
	var resultDraft map[string]any
	if err := json.Unmarshal(resultBytes, &resultDraft); err != nil || resultDraft["summary"] != nil || resultDraft["assignment_record_sha256"] != recordDigest {
		t.Fatalf("native Result artifact differs from frozen context: %v %v", resultDraft, err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", resultBytes); err == nil {
		t.Fatal("blank S6 Result draft unexpectedly passed the professional completion schema")
	}
	resultDraft["summary"] = "partially authored result must survive context preparation retry"
	stageJSON(t, root, contextView.ResultPath, resultDraft)
	stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID)
	preservedResult, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.ResultPath)))
	wantPreservedResult, marshalErr := json.MarshalIndent(resultDraft, "", "  ")
	wantPreservedResult = append(wantPreservedResult, '\n')
	if err != nil || marshalErr != nil || !bytes.Equal(preservedResult, wantPreservedResult) {
		t.Fatalf("native context retry rewrote the owner's editable Result: %v", err)
	}
	tamperedResult := map[string]any{}
	if err := json.Unmarshal(preservedResult, &tamperedResult); err != nil {
		t.Fatal(err)
	}
	tamperedResult["assignment_record_sha256"] = strings.Repeat("0", 64)
	tamperedBytes, err := json.MarshalIndent(tamperedResult, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tamperedBytes = append(tamperedBytes, '\n')
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(contextView.ResultPath)), tamperedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	beforeBadDraft := stageSnapshot(t, root).Revision
	var badDraftOut, badDraftErr bytes.Buffer
	code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID}, nil, &badDraftOut, &badDraftErr)
	if code == 0 || stageSnapshot(t, root).Revision != beforeBadDraft || !strings.Contains(badDraftErr.String(), "frozen Record tuple") {
		t.Fatalf("native retry accepted editable artifact tuple drift: code=%d stderr=%s", code, badDraftErr.String())
	}
	stillTampered, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.ResultPath)))
	if err != nil || !bytes.Equal(stillTampered, tamperedBytes) {
		t.Fatalf("native retry rewrote a tuple-drifted owner draft: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(contextView.ResultPath)), wantPreservedResult, 0600); err != nil {
		t.Fatal(err)
	}
	if contextView.PlanPath == "" || contextView.PlanDraft["assignment_record_contract"] != assignmentrecord.AssignmentRecordContract ||
		contextView.PlanDraft["assignment_record_sha256"] != recordDigest || contextView.PlanDraft["assignment_revision"] != float64(record.Revision) {
		t.Fatalf("PLAN draft is not bound to its canonical Record: path=%q draft=%v", contextView.PlanPath, contextView.PlanDraft)
	}
	professionalQuestions := []string{"/objective", "/planned_paths", "/assertion_checks", "/dependencies", "/risks_blockers"}
	if len(contextView.Questions) != len(professionalQuestions) {
		t.Fatalf("native PLAN asks %d professional questions, want exactly five: %v", len(contextView.Questions), contextView.Questions)
	}
	for _, question := range professionalQuestions {
		if contextView.Questions[question] == "" {
			t.Errorf("native PLAN template omitted professional question %s", question)
		}
	}
	if contextView.PlanDraft["objective"] != nil || contextView.PlanDraft["risks_blockers"] != nil ||
		len(contextView.PlanDraft["planned_paths"].([]any)) != 0 || len(contextView.PlanDraft["assertion_checks"].([]any)) != 0 {
		t.Fatalf("machine projection supplied an unanswered professional judgment: %v", contextView.PlanDraft)
	}
	wantDependencies, _ := json.Marshal(append([]string{}, record.DependsOn...))
	gotDependencies, _ := json.Marshal(contextView.PlanDraft["dependencies"])
	if !bytes.Equal(gotDependencies, wantDependencies) || !strings.Contains(contextView.Questions["/dependencies"], "workgroup-local") {
		t.Fatalf("native projection lost registered dependencies: %s; %v", gotDependencies, contextView.Questions)
	}
	planBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.PlanPath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", planBytes); err == nil {
		t.Fatal("blank PLAN draft unexpectedly passes the professional PLAN_REPORT schema")
	}
	schemaBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.Schema.Path)))
	if err != nil || workpacket.Digest(schemaBytes) != contextView.Schema.SHA256 {
		t.Fatalf("native PLAN schema source is not the frozen embedded schema: %v", err)
	}

	var plan map[string]any
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	// These are the five explicit professional answers; in particular, the
	// empty dependency/risk lists are author declarations, not template PASS.
	plan["objective"] = "Implement the registered TASK within its reviewed package scope."
	plan["planned_paths"] = []any{"src/pkg/output.go"}
	plan["assertion_checks"] = []any{map[string]any{
		"assertion": "The implementation satisfies the TASK closing assertion.",
		"oracle":    "Run the registered package check and inspect its reported result.",
	}}
	plan["dependencies"] = []any{}
	plan["risks_blockers"] = []any{}
	authoredBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	authoredBytes = append(authoredBytes, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", authoredBytes); err != nil {
		t.Fatalf("fully authored S6 PLAN_REPORT is not schema-valid: %v", err)
	}
	stageJSON(t, root, contextView.PlanPath, plan)

	s6SaveQuestionnaireHook(t, root, root, record.OwnerAgentID, filepath.Join(root, filepath.FromSlash(contextView.PlanPath)))
	afterBegin := stageSnapshot(t, root)
	if afterBegin.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatalf("AgentBegin changed the Main lifecycle: %v", afterBegin.State["lifecycle"])
	}
	var agentState string
	for _, raw := range afterBegin.State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.OwnerAgentID {
			agentState, _ = row["state"].(string)
		}
	}
	if agentState != "working" {
		t.Fatalf("original AgentBegin chain left Agent in %q, want working", agentState)
	}
	afterRecord, found, err := assignmentrecord.ForOwner(afterBegin.State, record.OwnerAgentID)
	if err != nil || !found {
		t.Fatalf("AgentBegin lost canonical Assignment Record: found=%v err=%v", found, err)
	}
	afterDigest, err := afterRecord.Digest()
	if err != nil || afterDigest != recordDigest || afterRecord.Reference() != record.Reference() {
		t.Fatalf("PLAN reception changed canonical Assignment authority: digest=%s err=%v", afterDigest, err)
	}
	finalJournalEvent := s6RecordContextJournalEvent(t, root, 0)
	for _, field := range []string{"event_id", "idempotency_key", "runtime_id", "transition_id"} {
		if finalJournalEvent[field] != bindingEvent[field] {
			t.Fatalf("later S6 CLI chain rewrote original workspace journal %s: before=%v after=%v", field, bindingEvent[field], finalJournalEvent[field])
		}
	}
	finalREQ, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(boundREQPath)))
	if err != nil || !bytes.Equal(finalREQ, boundREQBytes) || workpacket.Digest(finalREQ) != boundREQ["sha256"] {
		t.Fatalf("S6 CLI chain changed originally bound REQ bytes: %v", err)
	}
}

func TestS6NativeClosingContextKeepsAssessmentEditableAcrossRetry(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	const manifestRef = ".claude/workgroups/manifest-s6-native-context.json"
	manifestBytes, err := os.ReadFile(filepath.Join(root, manifestRef))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	assignment := manifest["assignments"].([]any)[0].(map[string]any)
	assignment["integration_check_mode"] = "post_merge"
	assignment["closing_contract"] = map[string]any{"contract": assignmentrecord.ClosingContractVersion, "worker_checks": []any{}}
	assignment["check_profiles"].([]any)[0].(map[string]any)["location"] = "main"
	stageJSON(t, root, manifestRef, manifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", root, "--manifest", manifestRef, "--task-id", taskID, "--task", taskPath)
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "agent-s6-native-context")
	if err != nil || !found || record.ClosingContract == nil || record.IntegrationCheckMode != "post_merge" {
		t.Fatalf("original registration did not preserve the closing contract: found=%t record=%+v err=%v", found, record, err)
	}
	output := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID)
	var binding struct {
		Context agentcontext.Reference `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(output), &binding); err != nil || binding.Context.Path == "" {
		t.Fatalf("decode closing context reference: %v / %s", err, output)
	}
	contextBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(binding.Context.Path)))
	if err != nil || workpacket.Digest(contextBytes) != binding.Context.SHA256 {
		t.Fatalf("read the frozen closing context: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", contextBytes); err != nil {
		t.Fatalf("native schema rejected the original closing projection: %v", err)
	}
	var view struct {
		Dispatch        agentcontext.Assignment `json:"dispatch"`
		PlanQuestions   map[string]string       `json:"plan_questions"`
		ResultDraft     map[string]any          `json:"result_draft"`
		ResultQuestions map[string]string       `json:"result_questions"`
		ResultPath      string                  `json:"result_path"`
	}
	if err := json.Unmarshal(contextBytes, &view); err != nil {
		t.Fatal(err)
	}
	if view.Dispatch.IntegrationCheckMode != "post_merge" || view.Dispatch.ClosingContract == nil ||
		!strings.Contains(view.PlanQuestions["/assertion_checks"], "done_when_indexes") ||
		view.ResultDraft["closing_assessment"] != nil || view.ResultQuestions["/closing_assessment"] == "" || view.ResultPath == "" {
		t.Fatalf("native context omitted frozen closing fields or professional questions: %+v", view)
	}
	resultPath := filepath.Join(root, filepath.FromSlash(view.ResultPath))
	blank, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read original native Result draft: %v", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", blank); err == nil {
		t.Fatal("unanswered Result template unexpectedly passed the full professional schema")
	}
	var result map[string]any
	if err := json.Unmarshal(blank, &result); err != nil {
		t.Fatal(err)
	}
	result["summary"] = "The registered TASK delivery includes the author's closing assessment."
	result["assertion_evidence"] = []any{map[string]any{
		"assertion":     "The implementation satisfies its registered closing condition.",
		"oracle":        "Inspect the implementation and cited delivery evidence.",
		"evidence_refs": []any{"src/pkg/closing-proof.txt"},
	}}
	result["risks_blockers"], result["scope_deviations"] = []any{}, []any{}
	result["closing_assessment"] = map[string]any{
		"disposition": "complete",
		"conditions": []any{map[string]any{
			"done_when_index": 0, "judgment": "satisfied", "plan_assertion_indexes": []any{0},
			"rationale": "The implementation satisfies the frozen condition, with the delivered bytes cited above.",
		}},
	}
	authored, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	authored = append(authored, '\n')
	if err := schema.NewEmbeddedValidator().ValidateBytes("agent-message.schema.json", authored); err != nil {
		t.Fatalf("authored closing Result does not match the full agent-message schema: %v", err)
	}
	if err := os.WriteFile(resultPath, authored, 0600); err != nil {
		t.Fatal(err)
	}
	stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID)
	preserved, err := os.ReadFile(resultPath)
	if err != nil || !bytes.Equal(preserved, authored) {
		t.Fatalf("native context retry rewrote the authored closing assessment: %v", err)
	}
	result["closing_assessment"].(map[string]any)["worker_result"] = "PASS"
	tampered, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tampered = append(tampered, '\n')
	if err := os.WriteFile(resultPath, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	beforeReject := stageSnapshot(t, root).Revision
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID}, nil, &stdout, &stderr)
	if code == 0 || stageSnapshot(t, root).Revision != beforeReject || !strings.Contains(stderr.String(), "unsupported field") {
		t.Fatalf("native context retry accepted an unknown closing claim: code=%d stderr=%s", code, stderr.String())
	}
	stillTampered, err := os.ReadFile(resultPath)
	if err != nil || !bytes.Equal(stillTampered, tampered) {
		t.Fatalf("rejected native closing draft was rewritten: %v", err)
	}
}

func TestS6CanonicalResultCLIAcceptanceReplayAndCurrentReceipt(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t)
	resultFilesBefore := s6CanonicalResultFiles(t, fixture.root, fixture.record)

	badSchema := s6CanonicalCompletionMessage(fixture, "msg-s6-cli-fake-checks", "Worker supplied fake machine checks")
	badSchema["checks"] = []any{map[string]any{"command": "go test ./src/pkg", "result": "pass"}}
	badSchemaPath := ".claude/messages/s6-result-fake-checks.json"
	stageJSON(t, fixture.root, badSchemaPath, badSchema)
	beforeState, beforeJournal := s6CanonicalRuntimePair(t, fixture.root)
	code, _, errout := s6RunCanonicalTaskComplete(fixture, badSchemaPath)
	if code == 0 || !strings.Contains(errout, "completion message schema") {
		t.Fatalf("schema accepted Worker machine claims: code=%d stderr=%s", code, errout)
	}
	s6AssertCanonicalRuntimePair(t, fixture.root, beforeState, beforeJournal)
	if after := s6CanonicalResultFiles(t, fixture.root, fixture.record); !reflect.DeepEqual(after, resultFilesBefore) {
		t.Fatalf("schema-rejected Result staged assignment artifacts: before=%v after=%v", resultFilesBefore, after)
	}

	badTuple := s6CanonicalCompletionMessage(fixture, "msg-s6-cli-bad-tuple", "Record tuple should match the original binding")
	badTuple["assignment_record_sha256"] = strings.Repeat("0", 64)
	badTuplePath := ".claude/messages/s6-result-bad-tuple.json"
	stageJSON(t, fixture.root, badTuplePath, badTuple)
	beforeState, beforeJournal = s6CanonicalRuntimePair(t, fixture.root)
	code, _, errout = s6RunCanonicalTaskComplete(fixture, badTuplePath)
	if code == 0 || !strings.Contains(errout, "current Assignment Record binding") {
		t.Fatalf("CLI accepted a drifted canonical Record tuple: code=%d stderr=%s", code, errout)
	}
	s6AssertCanonicalRuntimePair(t, fixture.root, beforeState, beforeJournal)
	if after := s6CanonicalResultFiles(t, fixture.root, fixture.record); !reflect.DeepEqual(after, resultFilesBefore) {
		t.Fatalf("tuple-rejected Result staged assignment artifacts: before=%v after=%v", resultFilesBefore, after)
	}

	firstSource := []byte("package pkg\n// first accepted evidence bytes\n")
	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.sourceRef)), firstSource, 0o644); err != nil {
		t.Fatal(err)
	}
	firstMessage := s6CanonicalCompletionMessage(fixture, "msg-s6-cli-result-first", "I implemented the reviewed TASK behavior")
	firstPath := fixture.resultPath
	stageJSON(t, fixture.root, firstPath, firstMessage)
	beforeFirst := stageSnapshot(t, fixture.root)
	firstOutput := stageCLI(t, "runtime", "task-complete", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--message", firstPath)
	first := s6DecodeCanonicalTaskCompleteOutput(t, firstOutput)
	if first.Revision != beforeFirst.Revision+1 || first.Acceptance.ResultRef == "" || first.Acceptance.ResultSHA256 == "" ||
		first.Acceptance.ResultRevision != 1 || len(first.Acceptance.AnswerDigest) != 64 || first.Acceptance.Replayed || !first.Acceptance.Current {
		t.Fatalf("first original CLI Result output = revision %d, receipt %+v", first.Revision, first.Acceptance)
	}
	s6AssertCurrentResultRefs(t, first.State, fixture.record, first.Acceptance.ResultRef)
	firstAccepted := s6ReadCLIResultEnvelope(t, fixture.root, first.Acceptance)
	if firstAccepted.Conclusion != "submitted" || firstAccepted.ResultRevision != 1 || firstAccepted.AnswerDigest != first.Acceptance.AnswerDigest ||
		firstAccepted.AssignmentRecord != fixture.record.Reference() || len(firstAccepted.ResolvedEvidenceRefs) != 1 {
		t.Fatalf("first canonical Result envelope = %+v", firstAccepted)
	}
	firstResolved := firstAccepted.ResolvedEvidenceRefs[0]
	if firstResolved.Ref != fixture.sourceRef || firstResolved.SHA256 != workpacket.Digest(firstSource) {
		t.Fatalf("first Result did not snapshot the actual evidence bytes: %+v", firstResolved)
	}
	firstSnapshot, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(firstResolved.Path)))
	if err != nil || !bytes.Equal(firstSnapshot, firstSource) {
		t.Fatalf("first durable evidence snapshot differs from source: %v", err)
	}
	s6AssertCanonicalBuilderTaskReceipt(t, fixture, first.Acceptance.ResultRef)

	afterFirstState, afterFirstJournal := s6CanonicalRuntimePair(t, fixture.root)
	replayedOutput := stageCLI(t, "runtime", "task-complete", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--message", firstPath)
	replayed := s6DecodeCanonicalTaskCompleteOutput(t, replayedOutput)
	if replayed.Revision != first.Revision || replayed.Acceptance.ResultRef != first.Acceptance.ResultRef ||
		replayed.Acceptance.ResultSHA256 != first.Acceptance.ResultSHA256 || replayed.Acceptance.AnswerDigest != first.Acceptance.AnswerDigest || replayed.Acceptance.ResultRevision != 1 ||
		!replayed.Acceptance.Replayed || !replayed.Acceptance.Current {
		t.Fatalf("same-body CLI replay changed its original receipt or Runtime revision: %+v", replayed)
	}
	s6AssertCanonicalRuntimePair(t, fixture.root, afterFirstState, afterFirstJournal)

	secondSource := []byte("package pkg\n// later professional evidence bytes\n")
	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.sourceRef)), secondSource, 0o644); err != nil {
		t.Fatal(err)
	}
	secondMessage := s6CanonicalCompletionMessage(fixture, "msg-s6-cli-result-second", "The later answer records the updated evidence")
	secondPath := ".claude/messages/s6-result-second.json"
	stageJSON(t, fixture.root, secondPath, secondMessage)
	secondOutput := stageCLI(t, "runtime", "task-complete", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--message", secondPath)
	second := s6DecodeCanonicalTaskCompleteOutput(t, secondOutput)
	if second.Revision != first.Revision+1 || second.Acceptance.ResultRevision != 2 || second.Acceptance.AnswerDigest == first.Acceptance.AnswerDigest || second.Acceptance.ResultRef == first.Acceptance.ResultRef ||
		second.Acceptance.Replayed || !second.Acceptance.Current {
		t.Fatalf("new answer/key did not create the current Result revision 2: revision=%d receipt=%+v", second.Revision, second.Acceptance)
	}
	s6AssertCurrentResultRefs(t, second.State, fixture.record, second.Acceptance.ResultRef)
	secondAccepted := s6ReadCLIResultEnvelope(t, fixture.root, second.Acceptance)
	if len(secondAccepted.ResolvedEvidenceRefs) != 1 || secondAccepted.ResolvedEvidenceRefs[0].SHA256 != workpacket.Digest(secondSource) {
		t.Fatalf("Result revision 2 did not snapshot the new actual source: %+v", secondAccepted.ResolvedEvidenceRefs)
	}
	s6AssertCanonicalBuilderTaskReceipt(t, fixture, second.Acceptance.ResultRef)

	beforeHistoricalState, beforeHistoricalJournal := s6CanonicalRuntimePair(t, fixture.root)
	historicalOutput := stageCLI(t, "runtime", "task-complete", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--message", firstPath)
	historical := s6DecodeCanonicalTaskCompleteOutput(t, historicalOutput)
	if historical.Revision != second.Revision || historical.Acceptance.ResultRef != first.Acceptance.ResultRef ||
		historical.Acceptance.ResultRevision != 1 || !historical.Acceptance.Replayed || historical.Acceptance.Current {
		t.Fatalf("historical CLI replay regressed the current snapshot or receipt: %+v", historical)
	}
	s6AssertCurrentResultRefs(t, historical.State, fixture.record, second.Acceptance.ResultRef)
	s6AssertCanonicalRuntimePair(t, fixture.root, beforeHistoricalState, beforeHistoricalJournal)

	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(firstResolved.Path)), []byte("tampered durable Result snapshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeTamperedState, beforeTamperedJournal := s6CanonicalRuntimePair(t, fixture.root)
	resultFilesBeforeTamperReject := s6CanonicalResultFiles(t, fixture.root, fixture.record)
	code, _, errout = s6RunCanonicalTaskComplete(fixture, firstPath)
	if code == 0 || !strings.Contains(errout, "snapshot") {
		t.Fatalf("CLI accepted a tampered historical durable snapshot: code=%d stderr=%s", code, errout)
	}
	s6AssertCanonicalRuntimePair(t, fixture.root, beforeTamperedState, beforeTamperedJournal)
	if after := s6CanonicalResultFiles(t, fixture.root, fixture.record); !reflect.DeepEqual(after, resultFilesBeforeTamperReject) {
		t.Fatalf("failed historical replay wrote assignment artifacts: before=%v after=%v", resultFilesBeforeTamperReject, after)
	}
}

type s6CanonicalResultCLIFixture struct {
	root, sourceRef string
	resultPath      string
	record          assignmentrecord.Record
	resultDraft     map[string]any
	assertion       string
	oracle          string
}

func readyS6CanonicalResultCLI(t *testing.T) s6CanonicalResultCLIFixture {
	t.Helper()
	root, taskID, taskPath := newS6RecordContextFixture(t)
	stageCLI(t, "runtime", "register-workgroup", "--root", root,
		"--manifest", ".claude/workgroups/manifest-s6-native-context.json",
		"--task-id", taskID, "--task", taskPath)
	registered := stageSnapshot(t, root)
	record, found, err := assignmentrecord.ForOwner(registered.State, "agent-s6-native-context")
	if err != nil || !found {
		t.Fatalf("load canonical S6 Assignment Record: found=%v err=%v", found, err)
	}
	contextOutput := stageCLI(t, "runtime", "assignment-context", "prepare", "--root", root, "--agent-id", record.OwnerAgentID)
	var contextBinding struct {
		Context agentcontext.Reference `json:"context_ref"`
	}
	if err := json.Unmarshal([]byte(contextOutput), &contextBinding); err != nil || contextBinding.Context.Path == "" {
		t.Fatalf("decode native context binding: ref=%+v err=%v output=%s", contextBinding.Context, err, contextOutput)
	}
	contextBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextBinding.Context.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if workpacket.Digest(contextBytes) != contextBinding.Context.SHA256 {
		t.Fatal("native context bytes differ from their original binding")
	}
	var contextView struct {
		PlanPath    string         `json:"plan_draft_path"`
		ResultPath  string         `json:"result_path"`
		ResultDraft map[string]any `json:"result_draft"`
	}
	if err := json.Unmarshal(contextBytes, &contextView); err != nil || contextView.PlanPath == "" || contextView.ResultPath == "" || contextView.ResultDraft == nil {
		t.Fatalf("decode native PLAN/Result draft paths: plan=%q result=%q err=%v", contextView.PlanPath, contextView.ResultPath, err)
	}
	resultBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.ResultPath)))
	if err != nil {
		t.Fatalf("read the actual native Result template: %v", err)
	}
	var resultDraft map[string]any
	if err := json.Unmarshal(resultBytes, &resultDraft); err != nil || !s6SameJSONValue(resultDraft, contextView.ResultDraft) {
		t.Fatalf("native Result artifact differs from its immutable context template: err=%v", err)
	}
	planBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contextView.PlanPath)))
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	assertion := "The implementation satisfies the TASK closing assertion."
	oracle := "Run the registered package check and inspect its reported result."
	plan["objective"] = "Implement the registered TASK within its reviewed package scope."
	plan["planned_paths"] = []any{"src/pkg/output.go"}
	plan["assertion_checks"] = []any{map[string]any{"assertion": assertion, "oracle": oracle}}
	plan["dependencies"] = []any{}
	plan["risks_blockers"] = []any{}
	stageJSON(t, root, contextView.PlanPath, plan)
	// This fixture deliberately tests an existing frozen canonical Record, not
	// a new native delivery. Use its original named compatibility consumer;
	// ordinary Worker saves no longer receive professional submissions.
	stageCLI(t, "runtime", "agent-begin", "--root", root, "--agent-id", record.OwnerAgentID, "--plan", contextView.PlanPath)
	afterBegin := stageSnapshot(t, root)
	var state string
	for _, raw := range afterBegin.State["entities"].(map[string]any)["agents"].([]any) {
		if raw.(map[string]any)["id"] == record.OwnerAgentID {
			state, _ = raw.(map[string]any)["state"].(string)
		}
	}
	if state != "working" {
		t.Fatalf("original CLI AgentBegin left owner in %q", state)
	}
	return s6CanonicalResultCLIFixture{root: root, sourceRef: "src/pkg/result-evidence.txt", resultPath: contextView.ResultPath, record: record, resultDraft: resultDraft, assertion: assertion, oracle: oracle}
}

func s6CanonicalCompletionMessage(fixture s6CanonicalResultCLIFixture, messageID, summary string) map[string]any {
	// Start from the actual ResultPath artifact so this CLI acceptance chain
	// exercises the context-produced machine tuple instead of copying it from
	// a separate fixture or reconstructing it from the Assignment Record.
	message := make(map[string]any, len(fixture.resultDraft))
	for key, value := range fixture.resultDraft {
		message[key] = value
	}
	message["message_id"] = messageID
	message["correlation_id"] = "corr-" + strings.TrimPrefix(messageID, "msg-")
	message["summary"] = summary
	message["assertion_evidence"] = []any{map[string]any{
		"assertion": fixture.assertion, "oracle": fixture.oracle, "evidence_refs": []string{fixture.sourceRef},
	}}
	message["risks_blockers"] = []string{}
	message["scope_deviations"] = []string{}
	return message
}

func s6SameJSONValue(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func s6RunCanonicalTaskComplete(fixture s6CanonicalResultCLIFixture, messagePath string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "task-complete", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--message", messagePath}, nil, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func s6DecodeCanonicalTaskCompleteOutput(t *testing.T, output string) struct {
	Revision   int
	State      map[string]any
	Acceptance assignment.ResultAcceptanceReceipt `json:"acceptance_receipt"`
} {
	t.Helper()
	var decoded struct {
		Revision   int
		State      map[string]any
		Acceptance assignment.ResultAcceptanceReceipt `json:"acceptance_receipt"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode original task-complete output: %v\n%s", err, output)
	}
	return decoded
}

func s6ReadCLIResultEnvelope(t *testing.T, root string, receipt assignment.ResultAcceptanceReceipt) assignmentresult.AcceptedResult {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(receipt.ResultRef)))
	if err != nil {
		t.Fatalf("read original Result envelope: %v", err)
	}
	if workpacket.Digest(body) != receipt.ResultSHA256 {
		t.Fatal("CLI acceptance receipt SHA differs from the original Result bytes")
	}
	var result assignmentresult.AcceptedResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Conclusion != "submitted" {
		t.Fatalf("canonical Result conclusion = %q, want submitted", result.Conclusion)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"status", "checks", "changed_paths", "reviewed_paths"} {
		if _, exists := raw[forbidden]; exists {
			t.Fatalf("canonical Result contains Worker machine field %q", forbidden)
		}
	}
	return result
}

func s6AssertCurrentResultRefs(t *testing.T, state map[string]any, record assignmentrecord.Record, resultRef string) {
	t.Helper()
	entities := state["entities"].(map[string]any)
	agentFound, taskFound := false, false
	for _, raw := range entities["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.OwnerAgentID {
			agentFound = true
			if row["completion_reported_ref"] != resultRef {
				t.Fatalf("current Agent Result pointer=%v, want %s", row["completion_reported_ref"], resultRef)
			}
		}
	}
	for _, raw := range entities["tasks"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.Task.ID {
			taskFound = true
			if row["completion_report_ref"] != resultRef {
				t.Fatalf("current TASK Result pointer=%v, want %s", row["completion_report_ref"], resultRef)
			}
		}
	}
	if !agentFound || !taskFound {
		t.Fatalf("canonical current Result pointers require registered Agent/TASK rows: agent_found=%t task_found=%t", agentFound, taskFound)
	}
}

func s6AssertCanonicalBuilderTaskReceipt(t *testing.T, fixture s6CanonicalResultCLIFixture, currentRef string) {
	t.Helper()
	output := stageCLI(t, "runtime", "task-receipt", "--root", fixture.root, "--agent-id", fixture.record.OwnerAgentID, "--assignment-id", fixture.record.AssignmentID)
	var receipt qualitygate.BuilderReceipt
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatalf("decode current Builder task receipt: %v\n%s", err, output)
	}
	if !receipt.Accepted || receipt.WorkComplete || receipt.Integrated || receipt.ProfessionalStatus != "accepted_unassessed" ||
		receipt.ResponsibilityStatus != "accepted_incomplete" || receipt.ResultRef != currentRef {
		t.Fatalf("accepted professional Result was misrepresented as completed: %+v", receipt)
	}
	foundPending := false
	for _, waiting := range receipt.WaitingOn {
		if waiting.Owner == "main" && waiting.Reason == "integration_pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatalf("Builder receipt lost its unverified Main integration requirement: %+v", receipt.WaitingOn)
	}
}

func s6CanonicalRuntimePair(t *testing.T, root string) ([]byte, []byte) {
	t.Helper()
	state, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return state, journal
}

func s6AssertCanonicalRuntimePair(t *testing.T, root string, state, journal []byte) {
	t.Helper()
	actualState, actualJournal := s6CanonicalRuntimePair(t, root)
	if !bytes.Equal(actualState, state) || !bytes.Equal(actualJournal, journal) {
		t.Fatal("canonical task-complete replay or rejection changed the original state/journal pair")
	}
}

func s6CanonicalResultFiles(t *testing.T, root string, record assignmentrecord.Record) map[string]string {
	t.Helper()
	base := filepath.Join(root, ".claude", "evidence", record.RuntimeID, "g1", "assignments", record.OwnerAgentID)
	files := map[string]string{}
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = workpacket.Digest(data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("snapshot current Assignment artifact files: %v", err)
	}
	return files
}

func newS6RecordContextFixture(t *testing.T) (root, taskID, taskPath string) {
	return newS6RecordContextFixtureWithReadSources(t, nil)
}

func newS6RecordContextFixtureWithReadSources(t *testing.T, extraSources map[string][]byte) (root, taskID, taskPath string) {
	t.Helper()
	root = fixtures.FreshRoot(t)
	repo := fixtures.RepoRoot(t)
	copyS6RecordContextTree(t, filepath.Join(repo, "docs/control/agent-protocol.md"), filepath.Join(root, "docs/control/agent-protocol.md"))
	copyS6RecordContextTree(t, filepath.Join(repo, "docs/rules"), filepath.Join(root, "docs/rules"))
	copyS6RecordContextTree(t, filepath.Join(repo, "agents/backend-builder.md"), filepath.Join(root, ".claude/agents/backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyS6RecordContextTree(t,
			filepath.Join(repo, "skills", skill.Name, "SKILL.md"),
			filepath.Join(root, ".claude/skills", skill.Name, "SKILL.md"))
	}

	const (
		reqID      = "REQ-039"
		taskIDSeed = "TASK-039-01"
		contractID = "BE-039"
	)
	taskPath = "docs/dev/tasks/TASK-039-01.md"
	indexPath := "docs/dev/tasks/index-REQ-039.md"
	manifestRef := ".claude/workgroups/manifest-s6-native-context.json"
	reqPath := "docs/requirements/REQ-039-loop-control-plane.md"
	designPath := "docs/architecture/ARCH-039-loop-control-plane.md"
	contractPath := "docs/dev/contracts/BE-039.md"
	reqBytes := []byte("# REQ-039 · native context Assignment Record fixture\n\n> Status: locked\n> Version: v2.0.0\n> Dispatch policy: waves-v1\n")
	designBytes := []byte("# ARCH-039 · native context Assignment Record fixture\n\n> Status: locked\n> Version: v1.0.0\n")
	contractBytes := []byte("# BE-039 · native context Assignment Record fixture\n\n> Status: locked\n> Version: v1.0.0\n\n## Requirement clauses\n\n- REQ-039 §1: preserve the original registered Main and assignment authority.\n")
	taskBytes := []byte("# TASK-039-01: Native context Assignment Record fixture\n\n" +
		"> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Dispatch policy: waves-v1\n> Primary contract: BE-039\n\n" +
		"## Scope\n\n| Type | Paths |\n| --- | --- |\n| prospective write paths | src/pkg |\n\n" +
		"## Delivered Clauses\n\n| Contract | Clauses |\n| --- | --- |\n| BE-039 | §1 |\n\n" +
		"## Dependencies\n\n| Dependency | Purpose |\n| --- | --- |\n| — | — |\n\n" +
		"## 7. Closing Contract\n\nassert go test ./src/pkg == pass\n")
	indexBytes := []byte("# REQ-039 dispatch plan\n\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1 · one reviewed assignment\n\n- [ ] [native context Assignment Record fixture](TASK-039-01.md)\n")
	sourceFiles := map[string][]byte{
		reqPath: reqBytes, designPath: designBytes, contractPath: contractBytes,
		taskPath: taskBytes, indexPath: indexBytes,
		"src/pkg/input.go":   []byte("package pkg\n"),
		"testdata/input.txt": []byte("registered profile input\n"),
	}
	for path, data := range extraSources {
		sourceFiles[path] = data
	}
	for path, data := range sourceFiles {
		s6WriteRecordContextFile(t, root, path, data)
	}

	state := fixtures.BaseState(t, root, "building", "", 8)
	state["schema_version"] = "1.2.0"
	entities := state["entities"].(map[string]any)
	entities["assignments"] = []any{}
	state["evidence"] = []any{}
	state["documents"] = []any{
		s6RecordContextDocument(reqID, "req", reqPath, "v2.0.0", reqBytes),
		s6RecordContextDocument("ARCH-039", "design", designPath, "v1.0.0", designBytes),
		s6RecordContextDocument(contractID, "contract", contractPath, "v1.0.0", contractBytes),
		s6RecordContextDocument(taskIDSeed, "task", taskPath, "v1.0.0", taskBytes),
		s6RecordContextDocument("dispatch-plan:REQ-039", "dispatch_plan", indexPath, "1", indexBytes),
	}
	fixtures.WriteState(t, root, state)
	// Prove that the original CLI rejects a branch whose committed REQ bytes
	// differ from the bound baseline without changing the Runtime pair. Then
	// establish the actual Main binding through that same original CLI.
	const mismatchedBranch = "s6-context-mismatched-req"
	if _, err := workspace.Git(context.Background(), root, "checkout", "-b", mismatchedBranch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(reqPath)), []byte("# REQ-039 changed after lock\n\n> Status: locked\n> Version: v2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "add", reqPath); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "commit", "-m", "mismatched branch REQ bytes"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(context.Background(), root, "checkout", "test-development"); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	stateBeforeReject, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBeforeReject, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var rejectedOut, rejectedErr bytes.Buffer
	rejectedCode := cli.Run([]string{"req", "workspace", "--root", root, "--dev-branch", mismatchedBranch, "--release-upstream", "origin/release"}, nil, &rejectedOut, &rejectedErr)
	if rejectedCode == 0 || !strings.Contains(rejectedErr.String(), "declared branch does not contain the bound REQ bytes") {
		t.Fatalf("REQ-mismatched development branch was not rejected: code=%d stderr=%s stdout=%s", rejectedCode, rejectedErr.String(), rejectedOut.String())
	}
	stateAfterReject, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfterReject, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBeforeReject, stateAfterReject) || !bytes.Equal(journalBeforeReject, journalAfterReject) {
		t.Fatal("rejected mismatched branch changed the original Runtime state/journal pair")
	}
	stageCLI(t, "req", "workspace", "--root", root, "--dev-branch", "test-development", "--release-upstream", "origin/release")

	profile := map[string]any{
		"id": "profile-s6-native-context", "version": "v1.0.0", "check_index": 0,
		"command": "go test ./src/pkg", "location": "worker", "timeout_seconds": 30,
		"environment_policy": "declared_only", "environment": []any{},
		"dependencies":  []any{map[string]any{"path": "src/pkg/input.go", "sha256": workpacket.Digest([]byte("package pkg\n"))}},
		"fixtures":      []any{map[string]any{"path": "testdata/input.txt", "sha256": workpacket.Digest([]byte("registered profile input\n"))}},
		"input_closure": "whole_tree", "side_effects": "none", "reuse_policy": "never",
	}
	documents := []any{
		s6ManifestReference(reqID, reqPath, "v2.0.0", reqBytes),
		s6ManifestReference("ARCH-039", designPath, "v1.0.0", designBytes),
		s6ManifestReference(contractID, contractPath, "v1.0.0", contractBytes),
		s6ManifestReference(taskIDSeed, taskPath, "v1.0.0", taskBytes),
		s6ManifestReference("dispatch-plan:REQ-039", indexPath, "1", indexBytes),
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "assignment_record_contract": assignmentrecord.AssignmentRecordContract,
		"manifest_id": "team-manifest-s6-native-context", "version": "v1.0.0",
		"runtime_id": state["runtime_id"], "req_id": reqID, "baseline_generation": 1, "review_round": nil,
		"platform_team_id": "platform-s6-native-context", "workgroup_id": "workgroup-s6-native-context",
		"workgroup_kind": "builder", "status": "planned", "documents": documents, "risk_tags": []any{},
		"responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "BUILD-WORK-PACKAGE", "disposition": "assigned", "trigger": "one current locked reviewed TASK",
			"assignment_ids": []any{"assignment-s6-native-context"}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-s6-native-context", "responsibility_id": "BUILD-WORK-PACKAGE",
			"role_family": "backend-builder", "agent_id": "agent-s6-native-context",
			"agent_definition_ref": ".claude/agents/backend-builder.md", "skill_refs": agentcontext.CoreSkills("backend-builder"),
			"read_paths": []any{taskPath, contractPath}, "scope": []any{"src/pkg"},
			"write_paths": []any{"src/pkg"}, "output_paths": []any{}, "depends_on": []any{},
			"required_checks": []any{"go test ./src/pkg"}, "done_when": []any{"the reviewed package change and registered check are complete"},
			"dispatch_mode": "plan_checkpoint", "check_profiles": []any{profile},
			"reuse_decision": "create", "grouping_rationale": "one owner for one reviewed TASK", "status": "planned",
		}},
		"separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "one independent owner for the single reviewed TASK",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-10-14T00:00:00Z"},
	}
	stageJSON(t, root, manifestRef, manifest)
	return root, taskIDSeed, taskPath
}

func s6RecordContextDocument(id, kind, path, version string, data []byte) map[string]any {
	return map[string]any{"id": id, "kind": kind, "path": path, "version": version, "sha256": workpacket.Digest(data), "status": "locked", "generation": 1}
}

func s6ManifestReference(id, path, version string, data []byte) map[string]any {
	return map[string]any{"id": id, "path": path, "version": version, "sha256": workpacket.Digest(data)}
}

func s6RecordContextJournalEvent(t *testing.T, root string, index int) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if index < 0 || index >= len(lines) || len(lines[index]) == 0 {
		t.Fatalf("Runtime journal event %d is unavailable", index)
	}
	var event map[string]any
	if err := json.Unmarshal(lines[index], &event); err != nil {
		t.Fatalf("decode Runtime journal event %d: %v", index, err)
	}
	return event
}

func s6WriteRecordContextFile(t *testing.T, root, path string, data []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyS6RecordContextTree(t *testing.T, source, target string) {
	t.Helper()
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		s6WriteRecordContextFile(t, filepath.Dir(target), filepath.Base(target), data)
		return
	}
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		output := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(output, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}
