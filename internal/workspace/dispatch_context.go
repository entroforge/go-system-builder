package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/projectconventions"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const DispatchContextPath = ".claude/inputs/assignment-context.json"
const PlanDraftPath = ".loop-submissions/plan.json"
const LegacyPlanDraftPath = ".claude/submissions/plan.json"
const ResultDraftPath = ".loop-submissions/completion.json"
const LegacyResultDraftPath = ".claude/submissions/completion.json"
const AnswerDraftPath = ".loop-submissions/stage-answer.json"
const LegacyAnswerDraftPath = ".claude/submissions/stage-answer.json"
const CommitRequestPath = ".loop-submissions/commit.json"
const LegacyCommitRequestPath = ".claude/submissions/commit.json"
const RepairResultDraftPath = ".loop-submissions/repair-result.json"
const LegacyRepairResultDraftPath = ".claude/submissions/repair-result.json"
const PlanSchemaPath = ".claude/inputs/agent-message.schema.json"

type WorkerSubmissionPaths struct {
	Plan         string
	Answer       string
	Completion   string
	Commit       string
	RepairResult string
}

func legacyWorkerSubmissionPaths() WorkerSubmissionPaths {
	return WorkerSubmissionPaths{Plan: LegacyPlanDraftPath, Answer: LegacyAnswerDraftPath, Completion: LegacyResultDraftPath, Commit: LegacyCommitRequestPath, RepairResult: LegacyRepairResultDraftPath}
}

func newWorkerSubmissionPaths() WorkerSubmissionPaths {
	return WorkerSubmissionPaths{Plan: PlanDraftPath, Answer: AnswerDraftPath, Completion: ResultDraftPath, Commit: CommitRequestPath, RepairResult: RepairResultDraftPath}
}

func (p WorkerSubmissionPaths) Contains(path string) bool {
	return path == p.Plan || path == p.Answer || path == p.Completion || path == p.Commit || path == p.RepairResult
}

func (p WorkerSubmissionPaths) ControlPaths() []string {
	paths := []string{p.Plan, p.Completion, p.Commit, p.RepairResult}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			out = append(out, path)
		}
	}
	return out
}

// WorkerSubmissionStatusExcludes preserves the prior .claude submissions
// exclusion and adds only five exact .loop-submissions drafts. Other files
// under .loop-submissions stay visible to Git.
func WorkerSubmissionStatusExcludes() []string {
	return []string{
		":(exclude).claude/submissions/**",
		":(exclude).loop-submissions/plan.json",
		":(exclude).loop-submissions/stage-answer.json",
		":(exclude).loop-submissions/completion.json",
		":(exclude).loop-submissions/commit.json",
		":(exclude).loop-submissions/repair-result.json",
	}
}

type Dispatch = agentcontext.Assignment

type GeneratedRole struct {
	Source         string   `json:"source"`
	Target         string   `json:"target"`
	SHA256         string   `json:"sha256"`
	OriginalSource string   `json:"original_source"`
	OriginalSHA256 string   `json:"original_sha256"`
	CoreSkills     []string `json:"core_skills"`
}

type DispatchContext struct {
	CheckProfiles       []checkexec.Profile            `json:"check_profiles,omitempty"`
	WorkerChecks        []assignmentrecord.WorkerCheck `json:"worker_checks,omitempty"`
	Conventions         *projectconventions.Bundle     `json:"conventions,omitempty"`
	GeneratedRole       *GeneratedRole                 `json:"generated_role,omitempty"`
	Version             string                         `json:"schema_version"`
	RuntimeID           string                         `json:"runtime_id"`
	BaselineGeneration  int                            `json:"baseline_generation"`
	ExecutionGeneration int                            `json:"execution_generation"`
	Dispatch            Dispatch                       `json:"dispatch"`
	ProductWritePaths   []string                       `json:"product_write_paths"`
	RequiredChecks      []string                       `json:"required_checks"`
	FrozenInputs        map[string]string              `json:"frozen_inputs"`
	Selection           agentcontext.Selection         `json:"context_selection"`
	PlanDraft           map[string]any                 `json:"plan_draft,omitempty"`
	PlanQuestions       map[string]string              `json:"plan_questions,omitempty"`
	PlanPath            string                         `json:"plan_path,omitempty"`
	ResultDraft         map[string]any                 `json:"result_draft,omitempty"`
	ResultQuestions     map[string]string              `json:"result_questions,omitempty"`
	ResultPath          string                         `json:"result_path,omitempty"`
	ResultDraftEditable bool                           `json:"result_draft_editable,omitempty"`
	WorkPacketRef       string                         `json:"work_packet_ref,omitempty"`
	AnswerPath          string                         `json:"answer_path,omitempty"`
	Instruction         string                         `json:"instruction"`
}

// WorkerSubmissionPaths derives paths only from the frozen context. It never
// inspects the workspace to choose a format or path.
func (c DispatchContext) WorkerSubmissionPaths() (WorkerSubmissionPaths, error) {
	paths := legacyWorkerSubmissionPaths()
	if c.PlanPath == PlanDraftPath || (c.WorkPacketRef != "" && c.AnswerPath == AnswerDraftPath) || c.AnswerPath == AnswerDraftPath || c.ResultPath == ResultDraftPath {
		paths = newWorkerSubmissionPaths()
	}
	switch c.PlanPath {
	case "":
	case PlanDraftPath:
	default:
		return WorkerSubmissionPaths{}, fmt.Errorf("unsupported frozen Worker plan path %q", c.PlanPath)
	}
	if c.Version == "1.0.0" && c.PlanPath == "" && c.PlanDraft == nil {
		paths.Plan = ""
	}
	if c.WorkPacketRef != "" && c.AnswerPath != paths.Answer {
		return WorkerSubmissionPaths{}, fmt.Errorf("frozen S5 answer path does not match its submission directory")
	}
	if c.AnswerPath != "" && c.AnswerPath != paths.Answer {
		return WorkerSubmissionPaths{}, fmt.Errorf("unsupported frozen Worker answer path %q", c.AnswerPath)
	}
	if c.ResultPath != "" && c.ResultPath != paths.Completion {
		return WorkerSubmissionPaths{}, fmt.Errorf("unsupported frozen Worker completion path %q", c.ResultPath)
	}
	return paths, nil
}

func DispatchContextSource(e Execution) string {
	return fmt.Sprintf(".claude/assignment-context/%s/g%d/%s-e%d.json", workpacket.Digest([]byte(e.RuntimeID)), e.BaselineGeneration, e.AssignmentID, e.Generation)
}
func HasDispatchContext(e Execution) bool { _, ok := e.Inputs[DispatchContextSource(e)]; return ok }

const builderSubmissionOrder = "For source changes, finish editing and any owned checks, write the explicit commit request at submission_paths.commit, and run the exact commit_command before saving the Result. The original save listener submits the Result automatically. Once accepted, its source snapshot is submitted; corrections use the original rework or revision flow. Main owns integration and its required checks."
const canonicalS6CommitDraftGuidance = "For this canonical S6 Builder execution, bootstrap already created the CommitRequest draft at submission_paths.commit with expected_head bound to the registered Worker starting HEAD. Keep expected_head unchanged; fill only message and paths with the actual changed product files inside product_write_paths, then run the supplied commit_command. This request is a delivery input, not another questionnaire."

func canonicalS6BuilderCommitDraft(e Execution, c DispatchContext) bool {
	if e.Purpose == DocumentReviewPurpose || e.AssignmentRecord == nil || len(e.WritePaths) == 0 ||
		c.PlanDraft == nil || c.ResultDraft == nil || !c.ResultDraftEditable || c.WorkPacketRef != "" {
		return false
	}
	switch c.Dispatch.Role {
	case "backend-builder", "frontend-builder", "test-builder":
	default:
		return false
	}
	paths, err := c.WorkerSubmissionPaths()
	return err == nil && paths.Plan == PlanDraftPath && paths.Completion == ResultDraftPath && paths.Commit == CommitRequestPath
}

func domainQuestionnaireOwnsProgress(e Execution, d Dispatch) bool {
	if e.Purpose == DocumentReviewPurpose {
		return true
	}
	switch d.Role {
	case "document-verifier", "delivery-verifier", "qa", "e2e-tester", "investigator":
		return true
	default:
		return false
	}
}

func workerPlanDraft(e Execution, d Dispatch, runtimeID, occurredAt string, planned []string) (map[string]any, map[string]string) {
	if d.DispatchMode != "plan_checkpoint" || domainQuestionnaireOwnsProgress(e, d) {
		return nil, nil
	}
	return agentcontext.PlanTemplate(d, runtimeID, occurredAt, planned)
}

// FreezeDispatchContext extends only a new execution's original input set.
// Its JSON is a projection, never a dispatch or activation authority.
func (b *ExecutionRegistry) FreezeDispatchContext(e Execution, d Dispatch, p *workpacket.Packet, requested ...string) (Execution, error) {
	return b.freezeDispatchContext(e, d, p, nil, requested...)
}
func (b *ExecutionRegistry) FreezeDispatchContextWithConventions(e Execution, d Dispatch, p *workpacket.Packet, conventions *projectconventions.Bundle, requested ...string) (Execution, error) {
	return b.freezeDispatchContext(e, d, p, conventions, requested...)
}
func (b *ExecutionRegistry) freezeDispatchContext(e Execution, d Dispatch, p *workpacket.Packet, conventions *projectconventions.Bundle, requested ...string) (Execution, error) {
	if e.AssignmentRecord != nil {
		if d.AssignmentRecord == nil || *d.AssignmentRecord != *e.AssignmentRecord || d.AssignmentRecordSHA256 != e.AssignmentRecordSHA256 || !reflect.DeepEqual(d.EffectiveWritePaths, e.WritePaths) || !reflect.DeepEqual(d.RequiredChecks, e.Checks) {
			return e, fmt.Errorf("dispatch context differs from the original canonical Assignment execution")
		}
		if d.ClosingContract != nil && !sameWorkerChecks(d.ClosingContract.WorkerChecks, e.WorkerChecks) {
			return e, fmt.Errorf("dispatch closing contract differs from the frozen WorkerChecks")
		}
		if len(e.WorkerChecks) != 0 && (d.ClosingContract == nil || !sameWorkerChecks(d.ClosingContract.WorkerChecks, e.WorkerChecks)) {
			return e, fmt.Errorf("dispatch context omits the frozen nonempty closing contract")
		}
	} else if d.AssignmentRecord != nil || d.AssignmentRecordSHA256 != "" {
		return e, fmt.Errorf("dispatch context cannot add canonical Assignment authority")
	}
	if HasDispatchContext(e) {
		return e, b.ValidateInputs(e)
	}
	if e.Status != "preparing" || d.AssignmentID != e.AssignmentID || d.Owner != e.AgentID || d.TaskID == "" || d.TeamID == "" {
		return e, fmt.Errorf("context requires a new originally registered execution/owner")
	}
	profile := "shadow"
	if len(requested) > 0 && requested[0] != "" {
		profile = requested[0]
	}
	if profile != "shadow" && profile != "backend-core" {
		return e, fmt.Errorf("unknown execution context profile")
	}
	if profile == "backend-core" && (d.Role != "backend-builder" || p != nil || e.Purpose == DocumentReviewPurpose) {
		return e, fmt.Errorf("backend-core requires the first backend Builder execution")
	}
	selection, err := agentcontext.Select(b.MainRoot, d.Role, d.ResponsibilityIDs, d.AuthorizationSkillRefs)
	if err != nil {
		return e, err
	}
	cloned := map[string]string{}
	for path, hash := range e.Inputs {
		cloned[path] = hash
	}
	e.Inputs = cloned
	// Preserve exact original manifest skill_refs as the capability source.
	inputs := map[string]string{}
	for path, hash := range e.Inputs {
		inputs[path] = hash
	}
	for _, skill := range selection.Selected {
		inputs[skill.Source] = skill.SHA256
		e.Inputs[skill.Source] = skill.SHA256
	}
	if err := conventions.ValidateInputs(e.Inputs); err != nil {
		return e, err
	}
	_, s9, err := b.s9AuthoringState()
	if err != nil {
		return e, err
	}
	domainQuestionnaire := s9 || domainQuestionnaireOwnsProgress(e, d)
	c := DispatchContext{CheckProfiles: e.CheckProfiles, WorkerChecks: copyWorkerChecks(e.WorkerChecks), Conventions: conventions, Version: "1.0.0", RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration, ExecutionGeneration: e.Generation, Dispatch: d, ProductWritePaths: append([]string{}, e.WritePaths...), RequiredChecks: append([]string{}, e.Checks...), FrozenInputs: inputs, Selection: selection}
	if !domainQuestionnaire && (d.DispatchMode == "plan_checkpoint" || d.DispatchMode == "plan_approval_required") {
		c.PlanPath = PlanDraftPath
	}
	if !domainQuestionnaire {
		c.Instruction = "Read this registered assignment, its frozen input files and installed Skill sources before forming its professional plan. Shadow selection is advisory; original approval and permissions remain authoritative. Fill the supplied plan fields yourself; a draft is not approval or activation."
	} else {
		c.Instruction = "Read this registered assignment, its frozen input files and installed Skill sources. Follow the Hook's exact domain questionnaire view and answer paths, complete the professional answer, and save the exact answer path. The original consumer validates the answer and updates the graph. An explicit plan_approval_required mode still waits for its original approval; the questionnaire does not grant approval."
	}
	if profile == "backend-core" {
		original, source, err := catalog.ReadAgentAsset(b.MainRoot, d.Role)
		if err != nil {
			return e, err
		}
		if source != d.AgentDefinitionRef || e.Inputs[source] != workpacket.Digest(original) {
			return e, fmt.Errorf("core role differs from the original frozen role source")
		}
		generated, err := agentcontext.RenderCoreRole(d.Role, original)
		if err != nil {
			return e, err
		}
		hash := workpacket.Digest(generated)
		path := ".claude/assignment-context/roles/" + hash + ".md"
		published, err := workpacket.PublishBounded(b.MainRoot, path, generated, 1<<20)
		if err != nil || !bytes.Equal(published, generated) {
			return e, fmt.Errorf("fixed generated role source is unavailable or conflicting: %v", err)
		}
		c.GeneratedRole = &GeneratedRole{Source: path, Target: ".claude/agents/backend-builder.md", SHA256: hash, OriginalSource: source, OriginalSHA256: workpacket.Digest(original), CoreSkills: agentcontext.CoreSkills(d.Role)}
		c.Selection.Mode = "enabled"
		e.Inputs[path] = hash
		c.FrozenInputs[path] = hash
		c.Instruction = "Read this fixed enabled context before planning. The Worker backend role preloads only core Skills; Read every applicable selected extra Skill at its frozen installed source. Loading selection changes no permissions: original manifest/activation SkillRefs, human approvals and domain gates remain authoritative. Fill the supplied professional plan fields yourself."
	}
	if p != nil {
		c.WorkPacketRef = workpacket.WorkerViewPath
		c.AnswerPath = AnswerDraftPath
		c.Instruction += " For S5, Read work_packet_ref and its authority-relative subjects below .claude/inputs. Fill only answer.conclusion/findings at the exact answer_path and save it; Main owns peer work, control followup and human decisions. An ordinary plan_checkpoint review has no separate generic PLAN_REPORT or activation step. An explicit plan_approval_required assignment still waits for its original approval."
	}
	if s9 {
		c.Instruction = "Read the current S9 professional questionnaire and supplied actual sources. Personally save PlanReport before work and RepairResult after implementation or no-change testing. Main supplies missing preparation and receives actual Git/check results; no generic plan, activation or completion proof is required."
	}
	contextAt := time.Now().UTC().Format(time.RFC3339Nano)
	planned := append([]string{}, e.WritePaths...)
	if p != nil {
		planned = []string{p.AnswerPath}
	}
	if !s9 {
		c.PlanDraft, c.PlanQuestions = workerPlanDraft(e, d, e.RuntimeID, contextAt, planned)
	}
	c.ResultDraft, c.ResultQuestions = agentcontext.ResultTemplate(d, e.RuntimeID, contextAt)
	if c.ResultDraft != nil {
		c.ResultPath = ResultDraftPath
		c.ResultDraftEditable = true
		c.Instruction += " Commit source changes through the original workspace commit adapter before saving result_path; the registered launcher supplies the exact submission paths and commands. Saving Result calls the original report adapter automatically. Once accepted, its source snapshot is submitted; corrections use the original rework or revision flow."
		if canonicalS6BuilderCommitDraft(e, c) {
			c.Instruction += " " + canonicalS6CommitDraftGuidance
		}
		if _, hasClosing := c.ResultDraft["closing_assessment"]; hasClosing {
			c.Instruction += " After the original accepted PLAN_REPORT and actual TASK work, edit result_path as the canonical S6 Builder Result. The save listener fills assertion/oracle rows and the closing assessment's DoneWhen/PLAN indexes from the accepted PLAN; add actual evidence_refs and author the summary, risks, scope deviations, disposition, condition judgments and rationales. This is your professional assessment; do not claim runner PASS or an integration conclusion. The save listener supplies the Result identity."
		} else {
			c.Instruction += " After the original accepted PLAN_REPORT and actual TASK work, edit result_path as the canonical S6 Builder Result. The save listener fills assertion/oracle rows from the accepted PLAN; add actual evidence_refs and author the summary, risks and scope deviations using result_questions and the full agent-message schema. The save listener supplies the Result identity. This submitted Result is not a PASS or an integration receipt."
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return e, err
	}
	data = append(data, '\n')
	path := DispatchContextSource(e)
	existing, err := workpacket.PublishBounded(b.MainRoot, path, data, 2<<20)
	if err != nil {
		return e, err
	}
	// A crash before registration can leave the same draft projection. Its
	// professional fields stay unanswered; compare facts, not creation time.
	if !bytes.Equal(existing, data) {
		var previous DispatchContext
		if err := json.Unmarshal(existing, &previous); err != nil {
			return e, err
		}
		if previous.PlanDraft != nil && c.PlanDraft != nil {
			c.PlanDraft["occurred_at"] = previous.PlanDraft["occurred_at"]
		}
		if previous.ResultDraft != nil && c.ResultDraft != nil {
			c.ResultDraft["occurred_at"] = previous.ResultDraft["occurred_at"]
		}
		expected, err := json.MarshalIndent(c, "", "  ")
		if err != nil || !bytes.Equal(existing, append(expected, '\n')) {
			return e, fmt.Errorf("existing frozen dispatch context differs; preserve it for recovery")
		}
	}
	e.Inputs[path] = workpacket.Digest(existing)
	return e, nil
}

func (b *ExecutionRegistry) ReadDispatchContext(e Execution) (DispatchContext, error) {
	path := DispatchContextSource(e)
	hash, ok := e.Inputs[path]
	if !ok {
		return DispatchContext{}, fmt.Errorf("legacy execution has no frozen assignment context")
	}
	data, err := workpacket.ReadFile(b.MainRoot, path, 2<<20)
	if err != nil || workpacket.Digest(data) != hash {
		return DispatchContext{}, fmt.Errorf("frozen assignment context source changed")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var c DispatchContext
	if err := decoder.Decode(&c); err != nil {
		return c, err
	}
	if !reflect.DeepEqual(c.CheckProfiles, e.CheckProfiles) || !sameWorkerChecks(c.WorkerChecks, e.WorkerChecks) {
		return c, fmt.Errorf("context check profiles differ from original execution")
	}
	if c.Version != "1.0.0" || c.RuntimeID != e.RuntimeID || c.BaselineGeneration != e.BaselineGeneration || c.ExecutionGeneration != e.Generation || c.Dispatch.AssignmentID != e.AssignmentID || c.Dispatch.Owner != e.AgentID {
		return c, fmt.Errorf("assignment context belongs to another execution")
	}
	if !reflect.DeepEqual(c.Dispatch.AssignmentRecord, e.AssignmentRecord) || c.Dispatch.AssignmentRecordSHA256 != e.AssignmentRecordSHA256 || !reflect.DeepEqual(c.ProductWritePaths, e.WritePaths) || !reflect.DeepEqual(c.RequiredChecks, e.Checks) {
		return c, fmt.Errorf("assignment context differs from the original execution authority")
	}
	if err := validateResultDispatchContext(c); err != nil {
		return c, err
	}
	if err := c.Conventions.ValidateInputs(c.FrozenInputs); err != nil {
		return c, err
	}
	for path, hash := range c.FrozenInputs {
		if e.Inputs[path] != hash {
			return c, fmt.Errorf("assignment context input binding differs")
		}
	}
	if c.Selection.Mode == "enabled" {
		g := c.GeneratedRole
		if g == nil || c.Dispatch.Role != "backend-builder" || g.Target != ".claude/agents/backend-builder.md" || g.OriginalSource != c.Dispatch.AgentDefinitionRef || e.Inputs[g.Source] != g.SHA256 || e.Inputs[g.OriginalSource] != g.OriginalSHA256 {
			return c, fmt.Errorf("enabled role binding differs from original execution")
		}
		original, err := workpacket.ReadFile(b.MainRoot, g.OriginalSource, 1<<20)
		if err != nil || workpacket.Digest(original) != g.OriginalSHA256 {
			return c, fmt.Errorf("original fixed role is unverifiable")
		}
		generated, err := workpacket.ReadFile(b.MainRoot, g.Source, 1<<20)
		if err != nil || workpacket.Digest(generated) != g.SHA256 {
			return c, fmt.Errorf("generated fixed role is unverifiable")
		}
		if err := agentcontext.VerifyCoreRole(original, generated, g.CoreSkills); err != nil {
			return c, err
		}
	} else if c.GeneratedRole != nil {
		return c, fmt.Errorf("generated role is present without an enabled profile")
	}
	return c, nil
}

func (b *ExecutionRegistry) WorkerSubmissionPaths(e Execution) (WorkerSubmissionPaths, error) {
	if !HasDispatchContext(e) {
		return legacyWorkerSubmissionPaths(), nil
	}
	c, err := b.ReadDispatchContext(e)
	if err != nil {
		return WorkerSubmissionPaths{}, err
	}
	return c.WorkerSubmissionPaths()
}

func validateResultDispatchContext(c DispatchContext) error {
	paths, err := c.WorkerSubmissionPaths()
	if err != nil {
		return err
	}
	occurredAt := ""
	if c.ResultDraft != nil {
		occurredAt, _ = c.ResultDraft["occurred_at"].(string)
	}
	probe, questions := agentcontext.ResultTemplate(c.Dispatch, c.RuntimeID, "2000-01-01T00:00:00Z")
	if probe == nil {
		if c.ResultDraft != nil || c.ResultQuestions != nil || c.ResultPath != "" || c.ResultDraftEditable {
			return fmt.Errorf("Result draft is present without a canonical S6 Builder binding")
		}
		return nil
	}
	if occurredAt == "" {
		return fmt.Errorf("canonical S6 Builder dispatch is missing its Result draft")
	}
	expected, _ := agentcontext.ResultTemplate(c.Dispatch, c.RuntimeID, occurredAt)
	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	actualBytes, err := json.Marshal(c.ResultDraft)
	if err != nil {
		return err
	}
	if !bytes.Equal(actualBytes, expectedBytes) || !agentcontext.ResultQuestionsMatch(questions, c.ResultQuestions) || c.ResultPath != paths.Completion || !c.ResultDraftEditable {
		return fmt.Errorf("Result draft differs from its original canonical Assignment Record projection")
	}
	return nil
}

func (b *ExecutionRegistry) ValidateDispatchContext(e Execution) error {
	if !HasDispatchContext(e) {
		return nil
	}
	source, err := workpacket.ReadFile(b.MainRoot, DispatchContextSource(e), 2<<20)
	if err != nil || workpacket.Digest(source) != e.Inputs[DispatchContextSource(e)] {
		return fmt.Errorf("assignment context source is unverifiable")
	}
	local, err := workpacket.ReadFile(e.Path, DispatchContextPath, 2<<20)
	if err != nil || !bytes.Equal(local, source) {
		return fmt.Errorf("Worker assignment context differs from its registered source")
	}
	if _, err := b.ReadDispatchContext(e); err != nil {
		return err
	}
	schemaBytes, err := workpacket.ReadFile(e.Path, PlanSchemaPath, 2<<20)
	if err != nil {
		return err
	}
	expected, err := schema.ReadAsset("agent-message.schema.json")
	// Existing bootstrap hashes remain authority across releases. Only verify
	// schema presence here; ValidateBootstrap proves its original exact bytes.
	if err != nil || len(schemaBytes) == 0 || len(expected) == 0 {
		return fmt.Errorf("Worker plan schema is unavailable")
	}
	return nil
}

func (c DispatchContext) ReadingOrder() []string {
	return agentcontext.InputReadingOrder(c.FrozenInputs, ".claude/inputs/")
}

// ReplaceDispatchContext retains the original frozen knowledge/profile when
// the existing recovery operation replaces a tree. Only the new execution
// generation is rebound; old assets and authored plans remain untouched.
func (b *ExecutionRegistry) ReplaceDispatchContext(old, next Execution) (Execution, error) {
	if !HasDispatchContext(old) {
		return next, nil
	}
	c, err := b.ReadDispatchContext(old)
	if err != nil {
		return next, err
	}
	if next.AssignmentID != old.AssignmentID || next.AgentID != old.AgentID || next.RuntimeID != old.RuntimeID || next.BaselineGeneration != old.BaselineGeneration || next.Generation <= old.Generation {
		return next, fmt.Errorf("context replacement changed the original responsibility")
	}
	inputs := map[string]string{}
	for path, hash := range old.Inputs {
		if path != DispatchContextSource(old) {
			inputs[path] = hash
		}
	}
	c.ExecutionGeneration = next.Generation
	if c.PlanDraft != nil {
		c.PlanDraft["occurred_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return next, err
	}
	data = append(data, '\n')
	path := DispatchContextSource(next)
	existing, err := workpacket.PublishBounded(b.MainRoot, path, data, 2<<20)
	if err != nil {
		return next, err
	}
	if !bytes.Equal(existing, data) {
		var prior DispatchContext
		if err := json.Unmarshal(existing, &prior); err != nil {
			return next, err
		}
		if prior.PlanDraft != nil && c.PlanDraft != nil {
			c.PlanDraft["occurred_at"] = prior.PlanDraft["occurred_at"]
		}
		expected, err := json.MarshalIndent(c, "", "  ")
		if err != nil || !bytes.Equal(existing, append(expected, '\n')) {
			return next, fmt.Errorf("replacement context differs from its original frozen source")
		}
	}
	inputs[path] = workpacket.Digest(existing)
	next.Inputs = inputs
	return next, nil
}
