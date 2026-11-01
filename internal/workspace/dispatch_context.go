package workspace

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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
	OriginalSource string   `json:"original_source"`
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
	SubmissionRoot      string                         `json:"submission_root,omitempty"`
	ResultPath          string                         `json:"result_path,omitempty"`
	ResultDraftEditable bool                           `json:"result_draft_editable,omitempty"`
	WorkPacketRef       string                         `json:"work_packet_ref,omitempty"`
	AnswerPath          string                         `json:"answer_path,omitempty"`
	Instruction         string                         `json:"instruction"`
}

// WorkerSubmissionPaths derives paths only from the frozen context. It never
// inspects the workspace to choose a format or path.
func (c DispatchContext) WorkerSubmissionPaths() (WorkerSubmissionPaths, error) {
	if c.SubmissionRoot != "" {
		root := c.SubmissionRoot
		if root != ".loop-submissions" {
			if !strings.HasPrefix(root, ".loop-submissions-candidate-") {
				return WorkerSubmissionPaths{}, fmt.Errorf("invalid recorded Worker submission location")
			}
			n, err := strconv.Atoi(strings.TrimPrefix(root, ".loop-submissions-candidate-"))
			if err != nil || n < 1 {
				return WorkerSubmissionPaths{}, fmt.Errorf("invalid recorded Worker submission location")
			}
		}
		paths := WorkerSubmissionPaths{Plan: root + "/plan.json", Answer: root + "/stage-answer.json", Completion: root + "/completion.json", Commit: root + "/commit.json", RepairResult: root + "/repair-result.json"}
		if c.PlanPath == "" {
			paths.Plan = ""
		} else if c.PlanPath != paths.Plan {
			return WorkerSubmissionPaths{}, fmt.Errorf("recorded Worker plan location differs")
		}
		if c.AnswerPath != "" && c.AnswerPath != paths.Answer {
			return WorkerSubmissionPaths{}, fmt.Errorf("recorded Worker answer location differs")
		}
		if c.ResultPath != "" && c.ResultPath != paths.Completion {
			return WorkerSubmissionPaths{}, fmt.Errorf("recorded Worker completion location differs")
		}
		return paths, nil
	}
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

func contextPathComponent(id string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(id))
	if encoded == "" {
		return "empty"
	}
	var parts []string
	for len(encoded) > 100 {
		parts = append(parts, encoded[:100])
		encoded = encoded[100:]
	}
	parts = append(parts, encoded)
	return strings.Join(parts, "/")
}

// The registered Execution inputs identify the actual context selected by
// its original Writer, including an alternate path when a file is occupied.
func dispatchContextSource(e Execution) (string, bool, error) {
	current := fmt.Sprintf(".claude/assignment-context/dispatch/%s/g%d/%s/e%d/context.json", contextPathComponent(e.RuntimeID), e.BaselineGeneration, contextPathComponent(e.AssignmentID), e.Generation)
	found := ""
	for path := range e.Inputs {
		if path != current && !(strings.HasPrefix(path, ".claude/assignment-context/dispatch/candidate-") && strings.HasSuffix(path, "/context.json")) {
			continue
		}
		if found != "" {
			return "", true, fmt.Errorf("original dispatch context association is ambiguous")
		}
		found = path
	}
	if found != "" {
		return found, true, nil
	}
	return current, false, nil
}

// publishDispatchArtifact only records paths exclusively created by this
// Writer. Unknown occupants stay untouched, even if their bytes happen to match.
func publishDispatchArtifact(root, original, candidatePrefix, filename string, data []byte, limit int64) (string, error) {
	for n := 0; ; n++ {
		path := original
		if n > 0 {
			path = filepath.ToSlash(filepath.Join(".claude/assignment-context", fmt.Sprintf("%s%d", candidatePrefix, n), filename))
		}
		created, err := workpacket.PublishNewBounded(root, path, data, limit)
		if err != nil {
			return "", err
		}
		if created {
			return path, nil
		}
		if err := workpacket.CheckPath(root, ".claude/assignment-context/dispatch-publication-probe/file.json"); err != nil {
			return "", fmt.Errorf("dispatch context storage unavailable: %w", err)
		}
	}
}
func DispatchContextSource(e Execution) string { path, _, _ := dispatchContextSource(e); return path }
func HasDispatchContext(e Execution) bool      { _, found, _ := dispatchContextSource(e); return found }

const builderSubmissionOrder = "For source changes, finish editing and any owned checks, write the explicit commit request at submission_paths.commit, and run the exact commit_command before saving the Result. The original save listener submits the Result automatically. Once accepted, its source snapshot is submitted; corrections use the original rework or revision flow. Main owns integration and its required checks."
const canonicalS6CommitDraftGuidance = "For this canonical S6 Builder execution, bootstrap already created the CommitRequest draft at submission_paths.commit. Fill message and paths with the actual changed product files inside product_write_paths, then run the supplied commit_command. This request is a delivery input, not another questionnaire."

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
	return err == nil && paths.Plan != "" && paths.Completion == c.ResultPath && paths.Commit != ""
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
		if d.AssignmentRecord == nil || *d.AssignmentRecord != *e.AssignmentRecord || !reflect.DeepEqual(d.EffectiveWritePaths, e.WritePaths) || !reflect.DeepEqual(d.RequiredChecks, e.Checks) {
			return e, fmt.Errorf("dispatch context differs from the original canonical Assignment execution")
		}
		if d.ClosingContract != nil && !sameWorkerChecks(d.ClosingContract.WorkerChecks, e.WorkerChecks) {
			return e, fmt.Errorf("dispatch closing contract differs from the frozen WorkerChecks")
		}
		if len(e.WorkerChecks) != 0 && (d.ClosingContract == nil || !sameWorkerChecks(d.ClosingContract.WorkerChecks, e.WorkerChecks)) {
			return e, fmt.Errorf("dispatch context omits the frozen nonempty closing contract")
		}
	} else if d.AssignmentRecord != nil {
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
	for path := range e.Inputs {
		cloned[path] = path
	}
	e.Inputs = cloned
	// Preserve exact original manifest skill_refs as the capability source.
	inputs := map[string]string{}
	for path := range e.Inputs {
		inputs[path] = path
	}
	for _, skill := range selection.Selected {
		inputs[skill.Source] = skill.Source
		e.Inputs[skill.Source] = skill.Source
	}
	if err := conventions.ValidateInputs(e.Inputs); err != nil {
		return e, err
	}
	_, s9, err := b.s9AuthoringState()
	if err != nil {
		return e, err
	}
	domainQuestionnaire := s9 || domainQuestionnaireOwnsProgress(e, d)
	c := DispatchContext{CheckProfiles: copyDispatchProfiles(e.CheckProfiles), WorkerChecks: copyDispatchChecks(e.WorkerChecks), Conventions: conventions, Version: "1.0.0", RuntimeID: e.RuntimeID, BaselineGeneration: e.BaselineGeneration, ExecutionGeneration: e.Generation, Dispatch: copyDispatch(d), ProductWritePaths: append([]string{}, e.WritePaths...), RequiredChecks: append([]string{}, e.Checks...), FrozenInputs: inputs, Selection: selection}
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
		if _, present := e.Inputs[source]; source != d.AgentDefinitionRef || !present {
			return e, fmt.Errorf("core role differs from the original frozen role source")
		}
		generated, err := agentcontext.RenderCoreRole(d.Role, original)
		if err != nil {
			return e, err
		}
		path, err := publishDispatchArtifact(b.MainRoot, DispatchContextSource(e)+".role.md", "role-candidate-", "role.md", generated, 1<<20)
		if err != nil {
			return e, err
		}
		c.GeneratedRole = &GeneratedRole{Source: path, Target: ".claude/agents/backend-builder.md", OriginalSource: source, CoreSkills: agentcontext.CoreSkills(d.Role)}
		c.Selection.Mode = "enabled"
		e.Inputs[path] = path
		c.FrozenInputs[path] = path
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
	path, err := publishDispatchArtifact(b.MainRoot, DispatchContextSource(e), "dispatch/candidate-", "context.json", data, 2<<20)
	if err != nil {
		return e, err
	}
	e.Inputs[path] = path
	return e, nil
}

func (b *ExecutionRegistry) ReadDispatchContext(e Execution) (DispatchContext, error) {
	path, ok, err := dispatchContextSource(e)
	if err != nil {
		return DispatchContext{}, err
	}
	if !ok {
		return DispatchContext{}, fmt.Errorf("legacy execution has no original assignment context")
	}
	data, err := workpacket.ReadFile(b.MainRoot, path, 2<<20)
	if err != nil {
		return DispatchContext{}, fmt.Errorf("original assignment context is unavailable: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var c DispatchContext
	if err := decoder.Decode(&c); err != nil {
		return c, err
	}
	if !sameCheckProfiles(c.CheckProfiles, e.CheckProfiles) || !sameWorkerChecks(c.WorkerChecks, e.WorkerChecks) {
		return c, fmt.Errorf("context check profiles differ from original execution")
	}
	if c.Version != "1.0.0" || c.RuntimeID != e.RuntimeID || c.BaselineGeneration != e.BaselineGeneration || c.ExecutionGeneration != e.Generation || c.Dispatch.AssignmentID != e.AssignmentID || c.Dispatch.Owner != e.AgentID {
		return c, fmt.Errorf("assignment context belongs to another execution")
	}
	if !reflect.DeepEqual(c.Dispatch.AssignmentRecord, e.AssignmentRecord) || !reflect.DeepEqual(c.ProductWritePaths, e.WritePaths) || !reflect.DeepEqual(c.RequiredChecks, e.Checks) {
		return c, fmt.Errorf("assignment context differs from the original execution authority")
	}
	if _, err := c.WorkerSubmissionPaths(); err != nil {
		return c, err
	}
	if err := c.Conventions.ValidateInputs(c.FrozenInputs); err != nil {
		return c, err
	}
	for path := range c.FrozenInputs {
		if _, present := e.Inputs[path]; !present {
			return c, fmt.Errorf("assignment context input is unavailable: %s", path)
		}
	}
	if c.Selection.Mode == "enabled" {
		g := c.GeneratedRole
		if g == nil || c.Dispatch.Role != "backend-builder" || g.Target != ".claude/agents/backend-builder.md" || g.OriginalSource != c.Dispatch.AgentDefinitionRef {
			return c, fmt.Errorf("enabled role binding differs from original execution")
		}
		original, err := workpacket.ReadFile(b.MainRoot, g.OriginalSource, 1<<20)
		if err != nil {
			return c, fmt.Errorf("original fixed role is unverifiable")
		}
		generated, err := workpacket.ReadFile(b.MainRoot, g.Source, 1<<20)
		if err != nil {
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

// ReserveWorkerSubmissionPaths creates a new private submission directory in
// the actual worktree and returns a context referencing it. The caller records
// the returned Execution with its original Runtime Writer before bootstrapping.
func (b *ExecutionRegistry) ReserveWorkerSubmissionPaths(e Execution) (Execution, error) {
	if !HasDispatchContext(e) {
		return e, nil
	}
	c, err := b.ReadDispatchContext(e)
	if err != nil {
		return e, err
	}
	if c.SubmissionRoot != "" {
		return e, nil
	}
	root, err := os.OpenRoot(e.Path)
	if err != nil {
		return e, err
	}
	defer root.Close()
	var submissionRoot string
	for n := 0; ; n++ {
		submissionRoot = ".loop-submissions"
		if n > 0 {
			submissionRoot = fmt.Sprintf(".loop-submissions-candidate-%d", n)
		}
		if err := root.Mkdir(submissionRoot, 0700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return e, err
		}
		break
	}
	// The already recorded standard paths need no second context or Runtime
	// write when their containing directory was created by this Writer.
	if submissionRoot == ".loop-submissions" {
		return e, nil
	}
	c.SubmissionRoot = submissionRoot
	if c.PlanPath != "" {
		c.PlanPath = submissionRoot + "/plan.json"
	}
	if c.AnswerPath != "" {
		c.AnswerPath = submissionRoot + "/stage-answer.json"
	}
	if c.ResultPath != "" {
		c.ResultPath = submissionRoot + "/completion.json"
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return e, err
	}
	data = append(data, '\n')
	previous := DispatchContextSource(e)
	path, err := publishDispatchArtifact(b.MainRoot, previous, "dispatch/candidate-", "context.json", data, 2<<20)
	if err != nil {
		return e, err
	}
	inputs := make(map[string]string, len(e.Inputs))
	for k, v := range e.Inputs {
		if k != previous {
			inputs[k] = v
		}
	}
	inputs[path] = path
	e.Inputs = inputs
	return e, nil
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

func (b *ExecutionRegistry) ValidateDispatchContext(e Execution) error {
	if !HasDispatchContext(e) {
		return nil
	}
	source, err := workpacket.ReadFile(b.MainRoot, DispatchContextSource(e), 2<<20)
	if err != nil {
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
	for path := range old.Inputs {
		if path != DispatchContextSource(old) {
			inputs[path] = path
		}
	}
	for path := range c.FrozenInputs {
		c.FrozenInputs[path] = path
	}
	c.Dispatch = copyDispatch(c.Dispatch)
	c.CheckProfiles = copyDispatchProfiles(c.CheckProfiles)
	c.WorkerChecks = copyDispatchChecks(c.WorkerChecks)
	c.ExecutionGeneration = next.Generation
	if c.PlanDraft != nil {
		c.PlanDraft["occurred_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return next, err
	}
	data = append(data, '\n')
	path, err := publishDispatchArtifact(b.MainRoot, DispatchContextSource(next), "dispatch/candidate-", "context.json", data, 2<<20)
	if err != nil {
		return next, err
	}
	inputs[path] = path
	next.Inputs = inputs
	return next, nil
}

func sameCheckProfiles(a, b []checkexec.Profile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameCheckProfile(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Project only actual check declarations into a new context. Historical
// machine hashes belong to the original records and do not authorize work.
func copyDispatchProfiles(in []checkexec.Profile) []checkexec.Profile {
	if in == nil {
		return nil
	}
	out := append(make([]checkexec.Profile, 0, len(in)), in...)
	for i := range out {
		if out[i].Dependencies != nil {
			out[i].Dependencies = append(make([]checkexec.FileRef, 0, len(out[i].Dependencies)), out[i].Dependencies...)
		}
		if out[i].Fixtures != nil {
			out[i].Fixtures = append(make([]checkexec.FileRef, 0, len(out[i].Fixtures)), out[i].Fixtures...)
		}
	}
	return out
}
func copyDispatchChecks(in []assignmentrecord.WorkerCheck) []assignmentrecord.WorkerCheck {
	if in == nil {
		return nil
	}
	out := append(make([]assignmentrecord.WorkerCheck, 0, len(in)), in...)
	for i := range out {
		out[i].Profile = copyDispatchProfiles([]checkexec.Profile{out[i].Profile})[0]
	}
	return out
}
func copyDispatch(d Dispatch) Dispatch {
	if d.ClosingContract != nil {
		copyOf := *d.ClosingContract
		copyOf.WorkerChecks = copyDispatchChecks(d.ClosingContract.WorkerChecks)
		d.ClosingContract = &copyOf
	}
	return d
}
