package policy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/classifier"
)

// Minimal Safety Policy — REQ-039 v2.0.0 §14 / BE-039 v1.0.2 §6.
//
// The enforce path produces the configured safety reasons plus the
// deterministic L4/S9 lifecycle barriers below:
//
//   - locked_artifact_write  — the affected path matches a LockedArtifact
//     manifest entry whose identity is complete (ID/kind/path/version/
//     sha256/locked_from_stage/baseline_generation).
//   - squash_merge           — tokenized resolver proves the command is a
//     git merge --squash or gh pr merge --squash.
//   - reviewer_product_write — during the verification stage a write targets
//     a path outside the control plane (.claude/), the report projections
//     (docs/reports/) and the ReviewPlan's verification artifact workspace;
//     the frozen product baseline tolerates no in-stage writes (L3-S7
//     §1.4.1, §8).
//
// All other predicates (activation, scope, Team, UI prototype, clean round,
// policy tamper, runtime integrity, subagent report, teammate idle report,
// permission expansion, etc.) have been retired and now live in Guidance,
// Quality Gate, Transition Guard or Integration precondition (BE-039 §6.3,
// ARCHITECTURE-039 §10.3).

const (
	RuleLockedArtifactWrite = "locked_artifact_write"
	RuleSquashMerge         = "squash_merge"
	// RuleReviewerProductWrite enforces the S7 frozen-baseline invariant:
	// during the verification stage, Write/Edit/MultiEdit/NotebookEdit may
	// only target the control plane, report projections, and the ReviewPlan's
	// declared verification artifact workspace (L3-S7 §1.4.1, §8).
	RuleReviewerProductWrite = "reviewer_product_write"
	// RuleAssignmentWriteBeforePlan enforces the L4 first-write barrier: a
	// dispatched Worker may not write into the product surface before its
	// PLAN_REPORT is recorded (plan_checkpoint mode). The main session (no
	// Agent context) is unaffected.
	RuleAssignmentWriteBeforePlan = "assignment_write_before_plan"
	// RuleUnauthorizedTaskSelfClaim enforces L4 §1.3 / §16.1: a teammate may
	// not claim a Team task via TaskUpdate (owner=self or status=in_progress)
	// unless the scheduler already dispatched that task to it. Ordinary
	// status updates on the agent's own dispatched tasks stay allowed.
	RuleUnauthorizedTaskSelfClaim = "unauthorized_task_self_claim"
	// RuleRuntimeUnreadable closes the safety boundary when the hook cannot
	// load the runtime facts needed to classify a mutating PreToolUse.
	RuleRuntimeUnreadable = "runtime_unreadable"
	// RuleRepairWriteBeforeExecution keeps S9 planning/reproduction read-only
	// on the product surface. The plan/report artifacts themselves remain
	// writable through the control plane; implementation writes require the
	// explicit BeginRepairExecution checkpoint.
	RuleRepairWriteBeforeExecution = "repair_write_before_execution"
	// RuleRepairAssignmentScope keeps an executing S9 Worker inside the
	// Assignment scope that was derived from the immutable RepairPlan and its
	// validated PlanReport. It is deliberately separate from the generic L4
	// first-write rule so the recovery packet names the repair assignment.
	RuleRepairAssignmentScope = "repair_assignment_scope"
	// RulePhaseProductWrite closes the RC-04 phase bare windows (S8-1,
	// S9-5, S10-4): every post-implementation lifecycle — verification,
	// bug_resolution (all sub-phases including targeted_reverification),
	// acceptance, and release_audit — freezes the product surface exactly
	// like the S7 verification rule. The only product-write exception is the
	// S9 `fixing` phase, which stays governed by repairAssignmentScopeDecision
	// and its RepairAllowedWritePaths. The rule is evaluated on the lifecycle
	// facts alone (no Runtime.Agent dependency), so Investigators, targeted
	// reverifiers and S10 auditors are all covered by one hard deny.
	RulePhaseProductWrite = "phase_product_write"
	// RuleProtectedReleaseCommand wires the data-driven protected-commands
	// table (docs/release_audits/protected_commands.json) into the PreToolUse
	// enforce path (RC-06, S10-3 — formerly dead code: classifier.MatchProtectedCommands
	// was only reachable from tests). git push to protected refs, formal
	// releases, publishes, infra applies and shell wrappers are hard-denied
	// regardless of lifecycle stage; the human release boundary is the only
	// authorized path (forbidden event automated_formal_release).
	RuleProtectedReleaseCommand = "protected_release_command"
)

// AgentContext carries the activated-agent view that downstream packages
// (controller, hookctx, adapter) still consume. The enforce path no longer
// reads it; it remains as a data carrier until BUG-02/03 migrate callers.
type AgentContext struct {
	ID                    string   `json:"id"`
	State                 string   `json:"state"`
	AllowedTools          []string `json:"allowed_tools"`
	AllowedWritePaths     []string `json:"allowed_write_paths"`
	AllowedCommandClasses []string `json:"allowed_command_classes"`
	// DispatchMode is the L4 dispatch mode stamped at register-workgroup
	// (plan_checkpoint default). PlanReportedRef is set by the
	// PostToolUse(SendMessage) observer (or the authoritative
	// readback_submitted event) once the plan checkpoint is recorded.
	DispatchMode    string `json:"dispatch_mode,omitempty"`
	PlanReportedRef string `json:"plan_reported_ref,omitempty"`
	// S9 assignment facts are loaded from the immutable RepairPlan and the
	// validated PlanReport refs. They are never accepted from ToolInput.
	AssignmentID            string   `json:"assignment_id,omitempty"`
	RepairAssignmentID      string   `json:"repair_assignment_id,omitempty"`
	RepairAllowedWritePaths []string `json:"repair_allowed_write_paths,omitempty"`
	RepairPlanReportRef     string   `json:"repair_plan_report_ref,omitempty"`
	// TaskIDs is the scheduler-dispatched task set for this agent
	// (entities.agents[].task_ids); the TaskUpdate self-claim guard reads it
	// to tell an owned status update from an unauthorized claim. TeamID and
	// CompletionReportedRef let the TeammateIdle/SubagentStop control path
	// recognize the exact teammate and its registered Result without
	// guessing from message text (L4 §15.2 P0-1).
	TaskIDs               []string `json:"task_ids,omitempty"`
	TeamID                string   `json:"team_id,omitempty"`
	CompletionReportedRef string   `json:"completion_reported_ref,omitempty"`
}

// TeamSummary is a lightweight view of a registered team manifest consumed by
// the loader and adapter when surfacing team state. The enforce path no
// longer reads it.
type TeamSummary struct {
	ManifestRef       string   `json:"manifest_ref"`
	ResponsibilityIDs []string `json:"responsibility_ids"`
}

// LockedArtifact mirrors the on-disk manifest entry consumed by the policy
// engine. All identity fields must be populated for the manifest to prove
// that a path is locked; an incomplete entry cannot deny.
type LockedArtifact struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Path               string `json:"path"`
	Version            string `json:"version"`
	SHA256             string `json:"sha256"`
	LockedFromStage    string `json:"locked_from_stage"`
	BaselineGeneration int    `json:"baseline_generation"`
}

// RuntimeContext is the projection of the Loop Runtime consumed by the policy
// engine and by downstream packages (hookctx loader, controller, hook
// adapter). The enforce path only consults RuntimeID / Revision /
// BoundREQPath / CurrentStage / LockedArtifacts / ProjectRoot to evaluate
// the two retained decisions. The remaining fields are data carriers for
// downstream consumers and are preserved until BUG-02/03 migrate them away.
type RuntimeContext struct {
	RuntimeID                 string           `json:"runtime_id"`
	Revision                  int              `json:"revision"`
	BoundREQID                string           `json:"bound_req_id,omitempty"`
	BoundREQPath              string           `json:"bound_req_path"`
	BoundREQUIImpact          string           `json:"bound_req_ui_impact"`
	Agent                     *AgentContext    `json:"agent"`
	CurrentState              string           `json:"current_state"`
	CurrentPhase              string           `json:"current_phase"`
	Paused                    bool             `json:"paused"`
	CleanRound                any              `json:"clean_round"`
	CurrentReviewRound        int              `json:"current_review_round"`
	EvidenceValidCount        int              `json:"evidence_valid_count"`
	OpenBlockingBugs          int              `json:"open_blocking_bugs"`
	Teams                     []TeamSummary    `json:"teams,omitempty"`
	LastActivityAt            string           `json:"last_activity_at,omitempty"`
	ProjectRoot               string           `json:"project_root,omitempty"`
	CurrentStage              string           `json:"current_stage,omitempty"`
	CurrentBaselineGeneration int              `json:"current_baseline_generation,omitempty"`
	LockedArtifacts           []LockedArtifact `json:"locked_artifacts,omitempty"`
	// VerificationWorkspace is the S7 ReviewPlan's verification artifact
	// write surface (E2E cold-start spec/fixture/evidence). The reviewer
	// product-write deny allows writes only inside it plus the control-plane
	// and report directories (L3-S7 §8).
	VerificationWorkspace string `json:"verification_workspace,omitempty"`
	// S9 repair pointer projection used by the phase/write barriers. The
	// policy engine only needs the lifecycle and immutable plan identity; the
	// hook context loader resolves the per-agent Assignment scope.
	// AssignmentID / PlanReportedRef carry the dispatched-Assignment facts
	// (assignment_id keyed, RC-04 S7-3): the first-write barrier reads these
	// instead of requiring Runtime.Agent, so every PreToolUse evaluation
	// path sees the barrier even when no AgentContext was resolved.
	AssignmentID     string `json:"assignment_id,omitempty"`
	PlanReportedRef  string `json:"plan_reported_ref,omitempty"`
	DispatchMode     string `json:"dispatch_mode,omitempty"`
	RepairStatus     string `json:"repair_status,omitempty"`
	RepairSessionID  string `json:"repair_session_id,omitempty"`
	RepairPlanRef    string `json:"repair_plan_ref,omitempty"`
	RepairPlanSHA256 string `json:"repair_plan_sha256,omitempty"`
}

type Input struct {
	SessionID string          `json:"session_id"`
	Event     string          `json:"hook_event_name"`
	AgentID   string          `json:"agent_id"`
	AgentType string          `json:"agent_type,omitempty"`
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	FilePath  string          `json:"file_path,omitempty"`
	Error     string          `json:"error,omitempty"`
	ToolInput map[string]any  `json:"tool_input"`
	TargetID  string          `json:"target_id"`
	Facts     map[string]bool `json:"facts"`
	Runtime   RuntimeContext  `json:"runtime_context"`
	Source    string          `json:"source,omitempty"`
	// Official Claude Code 2.1.218 TeammateIdle/SubagentStop payload fields
	// (L4 §15.2 P0-1). TeammateIdle carries teammate_name/team_name and no
	// agent_id; SubagentStop carries agent_id/agent_transcript_path/
	// last_assistant_message/stop_hook_active. They are preserved verbatim
	// so controller/policy rules can identify the exact teammate from the
	// platform payload instead of guessing.
	TeammateName         string `json:"teammate_name,omitempty"`
	TeamName             string `json:"team_name,omitempty"`
	TranscriptPath       string `json:"transcript_path,omitempty"`
	AgentTranscriptPath  string `json:"agent_transcript_path,omitempty"`
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	StopHookActive       bool   `json:"stop_hook_active,omitempty"`
}

// EffectiveAgentID resolves the platform-supplied agent identity: SubagentStop
// payloads carry agent_id, TeammateIdle payloads carry teammate_name instead.
// Returns "" when the payload identifies no agent (main session).
func (input Input) EffectiveAgentID() string {
	if input.AgentID != "" {
		return input.AgentID
	}
	return input.TeammateName
}

// Decision is the per-rule outcome emitted by the policy engine. In the
// minimal safety model only "block" or "allow" is produced by the enforce
// path. Guidance is populated by the controller and consumed by the Hook
// adapter; it is preserved on Decision as a data carrier.
type Decision struct {
	Decision       string    `json:"decision"`
	RuleID         string    `json:"rule_id"`
	Reason         string    `json:"reason"`
	AffectedPath   string    `json:"affected_path,omitempty"`
	ParsedCommand  string    `json:"parsed_command,omitempty"`
	Stage          string    `json:"stage,omitempty"`
	Missing        []string  `json:"missing,omitempty"`
	Recovery       []string  `json:"recovery,omitempty"`
	Retry          string    `json:"retry,omitempty"`
	HumanRequired  bool      `json:"human_required"`
	MatchedRuleIDs []string  `json:"matched_rule_ids,omitempty"`
	Guidance       *Guidance `json:"guidance,omitempty"`
	// AdditionalContext is a bounded, lifecycle-only context injection for
	// Claude Code's SessionStart/SubagentStart native output fields. It is
	// deliberately not part of the policy decision schema or audit record;
	// the authoritative state remains the runtime and Guidance.
	AdditionalContext string `json:"-"`
	// ElapsedMS is populated by the Hook transport with the measured policy
	// and controller evaluation duration. It is carried into the durable
	// DecisionEnvelope so timeout pressure can be observed without inventing
	// a timeout verdict inside the Hook process.
	ElapsedMS int64 `json:"-"`
}

// Guidance is the controller's positive scheduling instruction. It is
// emitted alongside the Decision so the Hook adapter can tell the Agent
// where the single lifecycle is and what to do next.
type Guidance struct {
	RuntimeID      string   `json:"runtime_id"`
	Revision       int      `json:"revision"`
	Event          string   `json:"event"`
	Stage          string   `json:"stage"`
	LifecycleState string   `json:"lifecycle_state"`
	LifecyclePhase string   `json:"lifecycle_phase,omitempty"`
	Objective      string   `json:"objective"`
	Action         string   `json:"action"`
	ProtocolRef    string   `json:"protocol_ref"`
	ManualRef      string   `json:"manual_ref"`
	PrimarySkill   string   `json:"primary_skill"`
	Read           []string `json:"read"`
	ReadOrder      []string `json:"read_order,omitempty"`
	Missing        []string `json:"missing"`
	DoneWhen       []string `json:"done_when"`
	Questions      []string `json:"questions,omitempty"`
	Automation     []string `json:"automation,omitempty"`
	Integration    []string `json:"integration,omitempty"`
	HumanRequired  bool     `json:"human_required"`
	Blocked        bool     `json:"blocked"`
	Blocker        string   `json:"blocker,omitempty"`
	Instruction    string   `json:"instruction"`
	Recovery       []string `json:"recovery"`
}

// Rule is the on-disk representation of a docs/hook-policy.json rules[]
// entry. The repository document keeps the two globally configured rules;
// lifecycle barriers require the loaded Runtime/Assignment projection and
// therefore remain deterministic code-level rules. The engine loads the
// document for PolicyVersion and envelope identity, but does not iterate it
// to produce a Decision.
type Rule struct {
	RuleID         string   `json:"rule_id"`
	Event          string   `json:"event"`
	Matcher        string   `json:"matcher"`
	Predicate      string   `json:"predicate"`
	Classification string   `json:"classification"`
	Reason         string   `json:"reason"`
	Missing        []string `json:"missing"`
	Recovery       []string `json:"recovery"`
	Retry          string   `json:"retry"`
	HumanRequired  bool     `json:"human_required"`
}

// document is the on-disk representation of docs/hook-policy.json.
type document struct {
	PolicyID string `json:"policy_id"`
	Version  string `json:"version"`
	Mode     string `json:"mode"`
	Rules    []Rule `json:"rules"`
}

// Engine holds the immutable state derived from a hook-policy.json load.
// The engine no longer walks rules: the two retained decisions are
// implemented as direct code paths so legacy predicates cannot reappear
// through a misconfigured policy document.
type Engine struct {
	rules         map[string]Rule
	policyID      string
	policyVersion string
	policySHA256  string
	mode          string
}

func Load(path string) (*Engine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hook policy: %w", err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode hook policy: %w", err)
	}
	engine := &Engine{
		rules:         make(map[string]Rule, len(doc.Rules)),
		policyID:      doc.PolicyID,
		policyVersion: doc.Version,
		policySHA256:  fmt.Sprintf("%x", sha256.Sum256(data)),
		mode:          doc.Mode,
	}
	for _, rule := range doc.Rules {
		engine.rules[rule.RuleID] = rule
	}
	return engine, nil
}

// Mode returns the policy mode ("enforce" or "audit").
func (e *Engine) Mode() string {
	return e.mode
}

// PolicyVersion returns the policy document's version field.
func (e *Engine) PolicyVersion() string {
	return e.policyVersion
}

// HasRule reports whether the loaded policy contains a rule with the given ID.
// Only the two retained rule IDs are expected in the minimal document.
func (e *Engine) HasRule(id string) bool {
	_, ok := e.rules[id]
	return ok
}

func (e *Engine) Evaluate(input Input) (Decision, error) {
	if decision, blocked := unknownMCPToolDecision(input); blocked {
		return decision, nil
	}
	// RC-06 (S10-3): the protected-release table evaluates first so a
	// protected command is refused even when a later, more lifecycle-specific
	// rule would also match. Fail-closed: an unreadable/malformed table
	// denies the command rather than letting it through unclassified.
	if decision, blocked := protectedReleaseDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := lockedArtifactDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := squashMergeDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := reviewerProductWriteDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := repairPreExecutionWriteDecision(input); blocked {
		return decision, nil
	}
	// RC-04: the phase-level product-write freeze runs after the repair
	// rules so bug_resolution.planning/reproducing keeps its more specific
	// repair_write_before_execution rule id (same deny, named recovery).
	if decision, blocked := phaseProductWriteDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := assignmentWriteBeforePlanDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := taskUpdateSelfClaimDecision(input); blocked {
		return decision, nil
	}
	return Decision{Decision: "allow"}, nil
}

// EvaluateAgentScoped exposes the rules that require a platform-identified
// agent (Runtime.Agent resolved from agent_id/teammate_name). The Controller
// cycle's safety input carries no Agent context, so the Hook transport calls
// this after hookctx resolution. The first-write barrier
// (assignment_write_before_plan) and the TaskUpdate self-claim guard both
// run here — the wire path now enforces the L4 §7.6 first-write barrier
// for any dispatched Worker that writes into the product surface before
// its PLAN_REPORT is recorded (L4 §15.2 P1-3 close-out).
func EvaluateAgentScoped(input Input) (Decision, bool) {
	if decision, blocked := repairAssignmentScopeDecision(input); blocked {
		return decision, true
	}
	if decision, blocked := assignmentWriteBeforePlanDecision(input); blocked {
		return decision, true
	}
	return taskUpdateSelfClaimDecision(input)
}

// taskUpdateSelfClaimDecision blocks every mutation of a task that is not in
// the agent's scheduler-dispatched set. Checking only owner=self or
// status=in_progress is fail-open: a teammate could complete, reassign, or
// clear the owner of somebody else's task and corrupt the control plane.
func taskUpdateSelfClaimDecision(input Input) (Decision, bool) {
	if input.ToolName != "TaskUpdate" {
		return Decision{}, false
	}
	agent := input.Runtime.Agent
	if agent == nil {
		// Main session (no Agent context) owns scheduling; out of scope.
		return Decision{}, false
	}
	taskID, _ := input.ToolInput["taskId"].(string)
	if taskID == "" {
		taskID, _ = input.ToolInput["task_id"].(string)
	}
	if taskID == "" {
		return unauthorizedTaskDecision(agent.ID, "<missing>", "TaskUpdate did not identify a taskId/task_id")
	}
	if contains(agent.TaskIDs, taskID) {
		return Decision{}, false
	}
	return unauthorizedTaskDecision(agent.ID, taskID, "the task is not in the scheduler-dispatched task set")
}

func unauthorizedTaskDecision(agentID, taskID, detail string) (Decision, bool) {
	return Decision{
		Decision: "deny",
		RuleID:   RuleUnauthorizedTaskSelfClaim,
		Reason:   fmt.Sprintf("Agent %s attempted to mutate task %s without a scheduler-dispatched assignment: %s (L4 §1.3)", agentID, taskID, detail),
		Recovery: []string{
			"wait for the scheduler/Main to dispatch an assignment for " + taskID,
			"only update tasks bound to your own assignment; completing your own dispatched task stays allowed",
		},
		Retry: RetryAfterRecoveryValidation,
	}, true
}

// repairPreExecutionWriteDecision is the S9 phase-one hard barrier. Planning
// and reproduction may create/read repair-control artifacts, but they may not
// mutate the implementation surface. This runs in the ordinary policy path
// (without an Agent context) so the main session and every Worker see the
// same checkpoint.
func repairPreExecutionWriteDecision(input Input) (Decision, bool) {
	if input.Event != "PreToolUse" || input.Runtime.CurrentState != "bug_resolution" {
		return Decision{}, false
	}
	if input.Runtime.CurrentPhase != "planning" && input.Runtime.CurrentPhase != "reproducing" {
		return Decision{}, false
	}
	paths, mutating := repairMutationPaths(input)
	if !mutating {
		return Decision{}, false
	}
	if len(paths) == 0 {
		return repairPreExecutionBlock(input, "<dynamic Bash mutation>"), true
	}
	for _, path := range paths {
		if !repairControlPathAllowed(input, path) {
			return repairPreExecutionBlock(input, path), true
		}
	}
	return Decision{}, false
}

func repairPreExecutionBlock(input Input, rawPath string) Decision {
	return Decision{
		Decision:     "deny",
		RuleID:       RuleRepairWriteBeforeExecution,
		AffectedPath: reviewerRelativePath(input, rawPath),
		Reason:       fmt.Sprintf("S9 %s is read-only on the product surface until BeginRepairExecution; %s is not a repair-control artifact", input.Runtime.CurrentPhase, reviewerRelativePath(input, rawPath)),
		Recovery: []string{
			"record one PlanReport per RepairAssignment with a failing pre-fix check",
			"run `BeginRepairExecution` via `runtime repair execution begin --actor <agent>` after every Assignment has a PlanReport",
			"keep plan/reproduction evidence under .claude/review/repair/ or .claude/evidence/; do not write product files yet",
		},
		Retry: RetryAfterRecoveryValidation,
	}
}

// repairAssignmentScopeDecision is the S9 phase-two product-write barrier.
// The Hook context loader derives RepairAssignmentID and RepairAllowedWritePaths
// from the immutable Plan + PlanReport refs; policy only evaluates the
// already-loaded facts and never trusts a Worker-supplied path declaration.
func repairAssignmentScopeDecision(input Input) (Decision, bool) {
	if input.Event != "PreToolUse" || input.Runtime.CurrentState != "bug_resolution" || input.Runtime.CurrentPhase != "fixing" {
		return Decision{}, false
	}
	paths, mutating := repairMutationPaths(input)
	if !mutating {
		return Decision{}, false
	}
	if len(paths) == 0 {
		return repairAssignmentBlock(input, "<dynamic Bash mutation>"), true
	}
	for _, path := range paths {
		if repairControlPathAllowed(input, path) {
			continue
		}
		if input.Runtime.Agent == nil || input.Runtime.Agent.RepairAssignmentID == "" {
			return repairAssignmentBlock(input, path), true
		}
		allowed := false
		for _, rule := range input.Runtime.Agent.RepairAllowedWritePaths {
			if repairPathMatches(input, path, rule) {
				allowed = true
				break
			}
		}
		if !allowed {
			return repairAssignmentBlock(input, path), true
		}
	}
	return Decision{}, false
}

func repairAssignmentBlock(input Input, rawPath string) Decision {
	assignment := "<unresolved>"
	if input.Runtime.Agent != nil && input.Runtime.Agent.RepairAssignmentID != "" {
		assignment = input.Runtime.Agent.RepairAssignmentID
	}
	return Decision{
		Decision:     "deny",
		RuleID:       RuleRepairAssignmentScope,
		AffectedPath: reviewerRelativePath(input, rawPath),
		Reason:       fmt.Sprintf("S9 executing Worker assignment %s cannot write %s: the path is outside its immutable Assignment scope", assignment, reviewerRelativePath(input, rawPath)),
		Recovery: []string{
			"reread the current RepairPlan and PlanReport for the dispatched Assignment",
			"keep the change inside the Assignment scope; if the root cause needs a new path, stop and return to S8 for Contract/Plan revision",
			"do not widen scope by editing the hook or runtime; submit a scope deviation for Main to reconcile",
		},
		Retry: RetryAfterRecoveryValidation,
	}
}

func repairMutationPaths(input Input) ([]string, bool) {
	if input.ToolName == "Bash" {
		command, _ := input.ToolInput["command"].(string)
		return bashMutationPaths(command)
	}
	if !isSideEffectTool(input.ToolName) {
		return nil, false
	}
	path := toolPath(input.ToolInput)
	if path == "" {
		return nil, true
	}
	return []string{path}, true
}

func repairControlPathAllowed(input Input, rawPath string) bool {
	rel := reviewerRelativePath(input, rawPath)
	for _, prefix := range []string{".claude/review/repair", ".claude/evidence", "docs/reports"} {
		if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
			return input.Runtime.ProjectRoot == "" || reviewerPathContained(input.Runtime.ProjectRoot, rel)
		}
	}
	return false
}

func repairPathMatches(input Input, rawPath, rule string) bool {
	rel := reviewerRelativePath(input, rawPath)
	rule = reviewerRelativePath(input, rule)
	if rel != rule && !strings.HasPrefix(rel, strings.TrimSuffix(rule, "/")+"/") {
		return false
	}
	return input.Runtime.ProjectRoot == "" || reviewerPathContained(input.Runtime.ProjectRoot, rel)
}

// assignmentWriteBeforePlanDecision is the L4 first-write barrier: a
// dispatched Worker in a pre-plan state (spawned/queued/reading, i.e. before
// its PLAN_REPORT checkpoint) may not mutate the product surface yet.
//
// RC-04 (S7-3) close-out: the barrier is now evaluated on the dispatched
// Assignment identity carried in the lifecycle facts (AssignmentID on the
// runtime projection, resolved from the agent row / workgroup manifest) and
// no longer depends on Runtime.Agent being loaded. The old gate
// (`Runtime.Agent == nil → stand down`) fell asleep on every PreToolUse path
// that evaluated the rule without hookctx resolution — the controller safety
// path and any caller that supplies lifecycle facts without an AgentContext —
// letting a pre-plan Worker write product code unimpeded. The barrier now
// fires whenever a dispatched-but-pre-plan Assignment (or an agent row still
// in spawned/queued/reading with a dispatched Assignment) targets the product
// surface; the main session (no Agent, no Assignment) is out of scope.
//
// Exempt surfaces (the reviewer_product_write allow list, aligned with
// L3-S7 §8 / L4 §10.4):
//
//   - .claude/ — the control plane; plan checkpoint writes live here.
//   - docs/reports/ — reviewer report projections (BUG-039 / L3-S7 §8).
//   - <ReviewPlan.verification_artifact_workspace> — E2E cold-start
//     spec/fixture work the ReviewPlan declared.
//
// Any other product-surface write while the plan checkpoint is still
// missing is blocked so the Worker must send PLAN_REPORT first.
func assignmentWriteBeforePlanDecision(input Input) (Decision, bool) {
	// Dispatched-Assignment fact first (assignment_id keyed barrier): an
	// assignment-bound input is pre-plan until the barrier facts say
	// otherwise, regardless of whether an AgentContext was loaded.
	if input.Runtime.AssignmentID != "" {
		// The assignment's plan checkpoint is recorded (or the mode is
		// one_shot: the final message IS the result) — barrier stands down.
		if input.Runtime.PlanReportedRef != "" || input.Runtime.DispatchMode == "one_shot" {
			return Decision{}, false
		}
	} else {
		// Fallback: the dispatched agent-row fact. A dispatched Worker's row
		// is in spawned/queued/reading until the PLAN_REPORT lands. The main
		// session (no Agent context) is out of scope.
		agent := input.Runtime.Agent
		if agent == nil {
			return Decision{}, false
		}
		if agent.State != "spawned" && agent.State != "queued" && agent.State != "reading" {
			return Decision{}, false
		}
		if agent.PlanReportedRef != "" || agent.DispatchMode == "one_shot" {
			return Decision{}, false
		}
	}
	if input.ToolName == "Bash" {
		command, _ := input.ToolInput["command"].(string)
		paths, mutating := bashMutationPaths(command)
		if !mutating {
			return Decision{}, false
		}
		if len(paths) == 0 || !allFirstWritePathsAllowed(input, paths) {
			return assignmentWriteBeforePlanBlock(input)
		}
		return Decision{}, false
	}
	if !isSideEffectTool(input.ToolName) {
		return Decision{}, false
	}
	if firstWriteSurfaceAllowed(input) {
		return Decision{}, false
	}
	return assignmentWriteBeforePlanBlock(input)
}

func assignmentWriteBeforePlanBlock(input Input) (Decision, bool) {
	agent := input.Runtime.Agent
	subject := "dispatched assignment"
	if input.Runtime.AssignmentID != "" {
		subject = "assignment " + input.Runtime.AssignmentID
	} else if agent != nil {
		subject = fmt.Sprintf("agent %s (%s)", agent.ID, agent.State)
	}
	return Decision{
		Decision: "deny",
		RuleID:   RuleAssignmentWriteBeforePlan,
		Reason:   fmt.Sprintf("%s has no recorded plan checkpoint; send the PLAN_REPORT (message_type plan_report) first — Main stays silent when the plan is aligned (L4 §7.4)", subject),
		Recovery: []string{
			"send the PLAN_REPORT via SendMessage with message_type=plan_report — the PostToolUse(SendMessage) observer records the plan checkpoint (plan_reported_ref) automatically",
			"then continue — no approval wait in plan_checkpoint mode",
		},
	}, true
}

// firstWriteSurfaceAllowed mirrors reviewerProductWriteDecision's allow
// list (the control plane, report projections, and the ReviewPlan's
// verification artifact workspace) so the first-write barrier does not
// block writes that the S7 frozen-baseline rule already exempts (L3-S7
// §1.4.1 / §8 / L4 §10.4).
func firstWriteSurfaceAllowed(input Input) bool {
	if input.ToolName == "Bash" {
		command, _ := input.ToolInput["command"].(string)
		paths, mutating := bashMutationPaths(command)
		return mutating && len(paths) > 0 && allFirstWritePathsAllowed(input, paths)
	}
	rawPath := toolPath(input.ToolInput)
	return rawPath != "" && firstWritePathAllowed(input, rawPath)
}

// firstWritePathAllowed is intentionally broader than reviewerWritePathAllowed.
// The first-write barrier only establishes that a Worker may record its
// checkpoint and control-plane bookkeeping before PLAN_REPORT. The stricter
// frozen-baseline rule still runs in verification and rejects direct runtime
// state writes there. Keeping these surfaces separate prevents the barrier
// from blocking the control-plane handshake it exists to support.
func firstWritePathAllowed(input Input, rawPath string) bool {
	rel := reviewerRelativePath(input, rawPath)
	if rel == ".claude" || strings.HasPrefix(rel, ".claude/") {
		return true
	}
	return reviewerWritePathAllowed(input, rawPath)
}

func allFirstWritePathsAllowed(input Input, paths []string) bool {
	for _, path := range paths {
		if !firstWritePathAllowed(input, path) {
			return false
		}
	}
	return true
}

// reviewerProductWriteDecision enforces the S7 frozen-baseline invariant
// (L3-S7 §1.4.1, §8): during the verification stage nobody writes product
// code or locked specs in the main checkout — any such write is baseline
// drift. The only allowed surfaces are the control plane (.claude/), the
// human-readable report projections (docs/reports/) and the ReviewPlan's
// declared verification artifact workspace (E2E cold-start spec/fixture).
func reviewerProductWriteDecision(input Input) (Decision, bool) {
	if input.Runtime.CurrentState != "verification" {
		return Decision{}, false
	}
	paths := []string{}
	mutating := false
	if input.ToolName == "Bash" {
		command, _ := input.ToolInput["command"].(string)
		paths, mutating = bashMutationPaths(command)
		if !mutating {
			return Decision{}, false
		}
	} else {
		if !isSideEffectTool(input.ToolName) {
			return Decision{}, false
		}
		rawPath := toolPath(input.ToolInput)
		if rawPath == "" {
			return Decision{}, false
		}
		paths = []string{rawPath}
	}
	for _, path := range paths {
		if !reviewerWritePathAllowed(input, path) {
			return reviewerProductWriteBlock(input, path), true
		}
	}
	if len(paths) > 0 {
		return Decision{}, false
	}
	// A write-capable Bash command whose target cannot be identified is
	// fail-closed during verification; a dynamic script can otherwise mutate
	// the frozen checkout while appearing pathless to the hook.
	return reviewerProductWriteBlock(input, "<dynamic Bash mutation>"), true
}

func reviewerProductWriteBlock(input Input, rawPath string) Decision {
	rel := reviewerRelativePath(input, rawPath)
	return Decision{
		Decision:     "deny",
		RuleID:       RuleReviewerProductWrite,
		Reason:       fmt.Sprintf("verification stage freezes the product baseline; %s is outside the reviewer write surfaces (.claude/, docs/reports/, and the ReviewPlan verification_artifact_workspace)", rel),
		AffectedPath: rel,
		Recovery: []string{
			"write review evidence and result drafts under .claude/ or docs/reports/",
			"E2E cold-start spec/fixture writes belong in the ReviewPlan verification_artifact_workspace",
			"if the product implementation must change, submit a ReviewResult with verdict=finding instead (L3-S7: Reviewers never repair)",
		},
		Retry: RetryAfterRecoveryValidation,
	}
}

// phaseProductWriteDecision closes the RC-04 phase bare windows (S8-1,
// S9-5, S10-4). The S7 verification freeze previously covered only
// state=="verification"; every later lifecycle — bug_resolution
// investigation/bug_report_review/repair_readback/planning/reproducing/
// targeted_reverification/ready_for_full_review, acceptance, and
// release_audit — was writable by Investigator/Repairer/reverification/
// audit agents. This rule applies the same frozen-baseline verdict to all
// of them, evaluated on lifecycle facts only (no Runtime.Agent dependency),
// so any agent or the main session sees the same hard deny.
//
// The one product-write exception is S9 bug_resolution.fixing: an executing
// repair Worker may write inside its immutable Assignment scope. The
// repairPreExecutionWriteDecision / repairAssignmentScopeDecision rules own
// that phase, so this rule stands down there instead of double-deny.
//
// Allow surfaces mirror reviewerProductWriteDecision: the control plane
// (.claude/), the report projections (docs/reports/ + the S10 evidence
// baselines docs/reports/acceptance/ and docs/release_audits/), and the
// ReviewPlan's verification artifact workspace.
func phaseProductWriteDecision(input Input) (Decision, bool) {
	if input.Event != "PreToolUse" || !phaseProductWriteFrozen(input) {
		return Decision{}, false
	}
	paths := []string{}
	mutating := false
	if input.ToolName == "Bash" {
		command, _ := input.ToolInput["command"].(string)
		paths, mutating = bashMutationPaths(command)
		if !mutating {
			return Decision{}, false
		}
	} else {
		if !isSideEffectTool(input.ToolName) {
			return Decision{}, false
		}
		rawPath := toolPath(input.ToolInput)
		if rawPath == "" {
			return Decision{}, false
		}
		paths = []string{rawPath}
	}
	for _, path := range paths {
		if !phaseWritePathAllowed(input, path) {
			return phaseProductWriteBlock(input, path), true
		}
	}
	if len(paths) > 0 {
		return Decision{}, false
	}
	// A write-capable Bash command whose target cannot be identified is
	// fail-closed while the product baseline is frozen; a dynamic script can
	// otherwise mutate the locked implementation while appearing pathless to
	// the hook.
	return phaseProductWriteBlock(input, "<dynamic Bash mutation>"), true
}

// phaseProductWriteFrozen reports whether the current lifecycle freezes the
// product surface (RC-04 contract item 1): verification (already handled by
// reviewerProductWriteDecision and excluded to keep its rule id stable) and
// every non-fixing bug_resolution phase, acceptance, and release_audit.
func phaseProductWriteFrozen(input Input) bool {
	switch input.Runtime.CurrentState {
	case "bug_resolution":
		// S9 fixing is the only product-write exception; the repair-scope
		// rules own its write surface.
		return input.Runtime.CurrentPhase != "fixing"
	case "acceptance", "release_audit":
		return true
	default:
		return false
	}
}

// phaseWritePathAllowed extends the reviewer allow list with the two S10
// evidence baselines (docs/reports/acceptance/ for ACC artifacts and
// docs/release_audits/ for release-audit reports — docs/rules/change-control
// §2) so audit work stays inside the control/report surfaces.
func phaseWritePathAllowed(input Input, rawPath string) bool {
	rel := reviewerRelativePath(input, rawPath)
	for _, prefix := range []string{".claude/evidence/", "docs/reports/", "docs/release_audits/"} {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return input.Runtime.ProjectRoot == "" || reviewerPathContained(input.Runtime.ProjectRoot, rel)
		}
	}
	if workspace := strings.TrimSuffix(filepath.ToSlash(input.Runtime.VerificationWorkspace), "/"); workspace != "" {
		if rel == workspace || strings.HasPrefix(rel, workspace+"/") {
			return input.Runtime.ProjectRoot == "" || reviewerPathContained(input.Runtime.ProjectRoot, rel)
		}
	}
	return false
}

func phaseProductWriteBlock(input Input, rawPath string) Decision {
	rel := reviewerRelativePath(input, rawPath)
	cursor := input.Runtime.CurrentState
	if input.Runtime.CurrentPhase != "" {
		cursor += "." + input.Runtime.CurrentPhase
	}
	return Decision{
		Decision:     "deny",
		RuleID:       RulePhaseProductWrite,
		Reason:       fmt.Sprintf("%s freezes the product surface; %s is outside the allowed write surfaces (.claude/, docs/reports/, docs/release_audits/, and the ReviewPlan verification_artifact_workspace)", cursor, rel),
		AffectedPath: rel,
		Recovery: []string{
			"write investigation, repair and audit artifacts under .claude/ or docs/reports/ (release-audit reports also allow docs/release_audits/)",
			"E2E cold-start spec/fixture writes belong in the ReviewPlan verification_artifact_workspace",
			"product implementation changes are legal only in bug_resolution.fixing through BeginRepairExecution with an approved RepairContract and a dispatched repair Assignment (S9); verification findings must go through a ReviewResult with verdict=finding instead",
		},
		Retry: RetryAfterRecoveryValidation,
	}
}

func reviewerRelativePath(input Input, rawPath string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(strings.Trim(rawPath, "\"'")), "./")
	if abs, err := filepath.Abs(rawPath); err == nil && input.Runtime.ProjectRoot != "" {
		if rootAbs, err := filepath.Abs(input.Runtime.ProjectRoot); err == nil {
			if r, err := filepath.Rel(rootAbs, abs); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				rel = filepath.ToSlash(r)
			}
		}
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
}

func reviewerWritePathAllowed(input Input, rawPath string) bool {
	rel := reviewerRelativePath(input, rawPath)
	allowed := []string{".claude/evidence/", "docs/reports/"}
	if workspace := strings.TrimSuffix(filepath.ToSlash(input.Runtime.VerificationWorkspace), "/"); workspace != "" {
		allowed = append(allowed, workspace+"/")
	}
	for _, prefix := range allowed {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return input.Runtime.ProjectRoot == "" || reviewerPathContained(input.Runtime.ProjectRoot, rel)
		}
	}
	return false
}

// reviewerPathContained closes the filesystem half of the write-surface
// check. Lexical prefixes alone allow an existing `.claude/evidence` symlink
// (or a symlinked E2E workspace parent) to redirect an otherwise authorized
// write outside the repository. Missing leaves are valid; every existing
// ancestor must still resolve beneath ProjectRoot.
func reviewerPathContained(root, rel string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	abs := filepath.Join(rootAbs, filepath.FromSlash(rel))
	relToRoot, err := filepath.Rel(rootAbs, abs)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return false
	}
	for current := abs; ; current = filepath.Dir(current) {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			resolvedRel, relErr := filepath.Rel(resolvedRoot, resolved)
			return relErr == nil && resolvedRel != ".." && !strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) && !filepath.IsAbs(resolvedRel)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func allReviewWritePathsAllowed(input Input, paths []string) bool {
	for _, path := range paths {
		if !reviewerWritePathAllowed(input, path) {
			return false
		}
	}
	return true
}

var (
	bashRedirectPattern      = regexp.MustCompile(`(^|[[:space:]])([0-9]*>>?|[0-9]?&>)[[:space:]]*("[^"]+"|'[^']+'|[^[:space:]&;|]+)`)
	bashPythonOpenPattern    = regexp.MustCompile(`open[[:space:]]*\([[:space:]]*["']([^"']+)["'][[:space:]]*,[[:space:]]*["'][^"']*[wax+][^"']*["']`)
	bashInterpreterPattern   = regexp.MustCompile(`\b(python[0-9]*|node|nodejs|npm|npx|yarn|pnpm|make|cargo)\b`)
	bashHeredocPattern       = regexp.MustCompile(`<<-?\s*["']?\w*`)
	bashInterpreterProbeArgs = regexp.MustCompile(`(^|[[:space:]])(--version|-V|--help|-h)([[:space:]]|$)`)
)

func isInterpreterReadOnlyProbe(lower string) bool {
	// Only version/help probes are safe. Everything else in the interpreter
	// family is conservatively treated as a potential write (fail-closed).
	if bashInterpreterProbeArgs.MatchString(lower) {
		return true
	}
	// `npm --version` and friends embed the flag after the tool name; also
	// handle `python --version` style without relying on word boundaries.
	for _, probe := range []string{"--version", "--help", " -v ", " -h "} {
		if strings.Contains(lower, probe) {
			return true
		}
	}
	fields := strings.Fields(lower)
	if len(fields) == 2 && (fields[1] == "--version" || fields[1] == "-v" || fields[1] == "--help" || fields[1] == "-h" || fields[1] == "version" || fields[1] == "help") {
		return true
	}
	return false
}

// bashMutationPaths is intentionally a small conservative classifier, not a
// shell parser. It catches common write forms and fails closed for dynamic
// mutators; read/test commands remain outside the S7 write rule.
//
// RC-03 (S7-2) extension: the interpreter family (python/node/npm/make/cargo/
// go run/heredoc/inline script) is treated as potentially mutating —
// `python3 gen_fixtures.py` must not bypass the frozen baseline. Commands
// whose target cannot be statically extracted are still marked mutating with
// an empty path slice so the caller fails closed via the dynamic-mutation
// branch instead of allowing a bypass.
func bashMutationPaths(command string) ([]string, bool) {
	lower := strings.ToLower(strings.TrimSpace(command))
	trimmed := strings.TrimSpace(command)
	paths := []string{}
	mutating := false
	add := func(path string) {
		path = strings.Trim(path, "\"'")
		if path != "" {
			paths = append(paths, path)
		}
	}

	// Heredoc / here-string / inline-script markers: `<<`, `<<<`, `<<-`.
	// `cat <<'EOF' > path` is already caught by the redirect pattern, but a
	// bare `python3 <<'PY'` or `node <<'JS'` heredoc without an explicit
	// redirect still executes a script that can mutate the product surface.
	if bashHeredocPattern.MatchString(command) || strings.Contains(command, "<<<") {
		mutating = true
	}
	// Interpreter family invocation — even without an extractable literal path
	// the command is an inline script execution that can write arbitrarily.
	// Checked here (before early return) so `python3 gen_fixtures.py` or
	// `node -e '...'` never bypasses as non-mutating.
	if bashInterpreterPattern.MatchString(lower) {
		// Explicitly read-only probe allowlist: version/help probes are not
		// product mutations. Everything else in this family is conservatively
		// treated as a potential write (fail-closed); a true read-only script
		// that is denied can be re-expressed as a `Read` tool call.
		if !isInterpreterReadOnlyProbe(lower) {
			mutating = true
		}
	}
	// `go run` is an interpreter-family invocation hidden behind the `go`
	// tool; `go generate` is already handled in the switch below.
	if strings.Contains(lower, "go run") {
		mutating = true
	}
	for _, match := range bashRedirectPattern.FindAllStringSubmatch(command, -1) {
		mutating = true
		add(match[3])
	}
	if strings.Contains(lower, "sed -i") || strings.Contains(lower, "sed  -i") || strings.Contains(lower, "perl -i") {
		mutating = true
		fields := strings.Fields(command)
		if len(fields) > 0 {
			add(fields[len(fields)-1])
		}
	}
	if strings.Contains(lower, "open(") || strings.Contains(lower, "open (") {
		if matches := bashPythonOpenPattern.FindAllStringSubmatch(command, -1); len(matches) > 0 {
			mutating = true
			for _, match := range matches {
				add(match[1])
			}
		} else if strings.Contains(lower, "write") || strings.Contains(lower, "truncate") {
			mutating = true
		}
	}
	// Shell hooks commonly hide writes in a short Python/Node expression. The
	// hook cannot safely prove the target of these APIs in all cases, so treat
	// the command as a mutation even when no literal path was extracted; the
	// caller then fails closed rather than allowing a dynamic write to bypass
	// the verification surface.
	if strings.Contains(lower, "path(") && (strings.Contains(lower, ".write_text") || strings.Contains(lower, ".write_bytes") || strings.Contains(lower, ".unlink") || strings.Contains(lower, ".mkdir")) {
		mutating = true
	}
	if strings.Contains(lower, "fs.writefilesync") || strings.Contains(lower, "fs.appendfilesync") || strings.Contains(lower, "fs.rmsync") || strings.Contains(lower, "fs.mkdirSync") || strings.Contains(lower, "fs.mkdirasync") || strings.Contains(lower, ".writefilesync") || strings.Contains(lower, ".appendfilesync") || strings.Contains(lower, ".rmsync") || strings.Contains(lower, ".mkdirsync") {
		mutating = true
	}
	// Inline script flags: `python -c`, `python3 -m`, `node -e`, etc. already
	// covered by the interpreter-family check above, but make the intent
	// explicit so a future allowlist does not accidentally re-open them.
	if strings.Contains(lower, "python") && (strings.Contains(trimmed, " -c ") || strings.Contains(trimmed, " -m ") || strings.Contains(lower, " -c'") || strings.Contains(lower, " -c\"")) {
		mutating = true
	}
	if strings.Contains(lower, "node ") && (strings.Contains(trimmed, " -e ") || strings.Contains(lower, " -e'") || strings.Contains(lower, " -e\"") || strings.Contains(lower, "--eval")) {
		mutating = true
	}
	fields := strings.Fields(command)
	if len(fields) > 0 {
		base := strings.TrimPrefix(filepath.Base(strings.Trim(fields[0], "\"'")), "env")
		switch base {
		case "tee":
			mutating = true
			for _, field := range fields[1:] {
				if !strings.HasPrefix(field, "-") {
					add(field)
					break
				}
			}
		case "cp", "mv", "install":
			mutating = true
			if len(fields) > 1 {
				add(fields[len(fields)-1])
			}
		case "rm", "touch", "mkdir":
			mutating = true
			for _, field := range fields[1:] {
				if !strings.HasPrefix(field, "-") {
					add(field)
				}
			}
		case "go":
			if len(fields) > 1 && (fields[1] == "generate" || fields[1] == "run") {
				mutating = true
			}
		case "git":
			for _, field := range fields[1:] {
				if contains([]string{"apply", "checkout", "restore", "clean", "reset", "mv", "rm", "commit"}, strings.TrimLeft(field, "-")) {
					mutating = true
					break
				}
			}
		case "python", "python3", "python2", "node", "nodejs", "npm", "npx", "yarn", "pnpm", "make", "cargo":
			// Interpreter / build-tool family: already marked mutating above;
			// attempt to extract a trailing file argument as the affected path
			// for a more precise denial message, but mutating stays true even
			// when no literal can be extracted (fail-closed).
			if !isInterpreterReadOnlyProbe(lower) {
				mutating = true
				isInlineScript := strings.Contains(lower, " -c ") || strings.Contains(lower, " -c'") || strings.Contains(lower, " -c\"") || strings.Contains(lower, " -m ") || strings.Contains(lower, " -m'") || strings.Contains(lower, " -m\"") || strings.Contains(lower, " -e ") || strings.Contains(lower, " -e'") || strings.Contains(lower, " -e\"") || strings.Contains(lower, "--eval") || strings.Contains(lower, "open(") || strings.Contains(command, "<<")
				if !isInlineScript {
					for i := len(fields) - 1; i >= 1; i-- {
						if !strings.HasPrefix(fields[i], "-") && !strings.Contains(fields[i], "=") {
							// Skip the subcommand itself for npm/make/cargo.
							add(fields[i])
							break
						}
					}
				}
			}
		}
	}
	if strings.HasPrefix(lower, "ln ") || strings.Contains(lower, " ln -") {
		mutating = true
	}
	if strings.Contains(lower, "git") && strings.Contains(lower, " apply") {
		mutating = true
	}
	return paths, mutating
}

// protectedReleaseDecision implements RC-06 (S10-3): the protected-commands
// table drives a hard deny on the PreToolUse enforce path. The table is
// data-driven (docs/release_audits/protected_commands.json) and matched via
// classifier.Resolve + classifier.MatchProtectedCommands, which were
// previously reachable only from tests. ProjectRoot comes from the runtime
// projection; when it is empty the table cannot be located and the check is
// skipped (the safety path without a runtime root has no data-driven reason
// to evaluate, and unknownMCPTool/locked-artifact rules still apply).
func protectedReleaseDecision(input Input) (Decision, bool) {
	if input.ToolName != "Bash" {
		return Decision{}, false
	}
	command, _ := input.ToolInput["command"].(string)
	if strings.TrimSpace(command) == "" {
		return Decision{}, false
	}
	root := input.Runtime.ProjectRoot
	if strings.TrimSpace(root) == "" {
		return Decision{}, false
	}
	table, err := classifier.LoadProtectedCommands(root)
	if err != nil || len(table) == 0 {
		// Fail closed: a broken or missing table means the release surface
		// is unclassified — refuse the Bash call with the same rule id so
		// the caller sees the boundary instead of a silent allow.
		return Decision{
			Decision:       "deny",
			RuleID:         RuleProtectedReleaseCommand,
			Reason:         "protected_commands table unreadable (" + err.Error() + "); refusing unclassified Bash",
			ParsedCommand:  command,
			Stage:          input.Runtime.CurrentStage,
			Recovery:       []string{"repair docs/release_audits/protected_commands.json before running shell commands"},
			Retry:          RetryAfterRecoveryValidation,
			HumanRequired:  false,
			MatchedRuleIDs: []string{RuleProtectedReleaseCommand},
		}, true
	}
	resolved, err := classifier.Resolve(command)
	if err != nil {
		return Decision{}, false
	}
	// Attribution: the squash-merge shape has its own retained rule
	// (squashMergeDecision below, BE-039 §6.2). Defer to it so wire
	// consumers keep seeing reason=squash_merge for that family.
	if parsed, ok := classifier.ParseSquashMerge(command); ok {
		_ = parsed
		return Decision{}, false
	}
	matched, reason, err := classifier.MatchProtectedCommands(resolved, table)
	if err != nil || !matched {
		return Decision{}, false
	}
	if reason == "" {
		reason = "protected release command"
	}
	return Decision{
		Decision:       "deny",
		RuleID:         RuleProtectedReleaseCommand,
		Reason:         reason,
		ParsedCommand:  command,
		Stage:          input.Runtime.CurrentStage,
		Recovery:       []string{"the human release boundary (TR-017 → TR-023) is the only authorized release path"},
		Retry:          RetryNever,
		HumanRequired:  false,
		MatchedRuleIDs: []string{RuleProtectedReleaseCommand},
	}, true
}

func squashMergeDecision(input Input) (Decision, bool) {
	if input.ToolName != "Bash" {
		return Decision{}, false
	}
	command, _ := input.ToolInput["command"].(string)
	parsedCommand, matched := classifier.ParseSquashMerge(command)
	if !matched {
		return Decision{}, false
	}
	return Decision{
		Decision:       "deny",
		RuleID:         RuleSquashMerge,
		Reason:         RuleSquashMerge,
		ParsedCommand:  parsedCommand,
		Stage:          input.Runtime.CurrentStage,
		Recovery:       []string{"use a normal merge without squash"},
		Retry:          RetryAfterRecoveryValidation,
		HumanRequired:  false,
		MatchedRuleIDs: []string{RuleSquashMerge},
	}, true
}

func lockedArtifactDecision(input Input) (Decision, bool) {
	if !isSideEffectTool(input.ToolName) {
		return Decision{}, false
	}
	affectedPaths := provenMutationPaths(input)
	for _, artifact := range input.Runtime.LockedArtifacts {
		// Stage-aware locking (L3-S5 §5): registration projects the artifact
		// into LockedArtifacts immediately, but the write-block for a
		// CURRENT-generation artifact is active only from its lock stage
		// onward (contract/task/design lock at S6) — before that, the
		// planning and document-verification stages (including the TR-004
		// repair loop) stay writable. Superseded generations are immutable
		// history at every stage.
		if artifact.BaselineGeneration > 0 &&
			input.Runtime.CurrentBaselineGeneration > artifact.BaselineGeneration {
			// superseded generation — always locked
		} else if !lockStageReached(input.Runtime.CurrentStage, artifact.LockedFromStage) {
			continue
		}
		for _, path := range affectedPaths {
			if artifact.complete() &&
				samePath(path, artifact.Path) {
				recovery := []string{"create a new version through the formal rework path"}
				if input.Runtime.BoundREQID != "" && artifact.Kind != "req" {
					recovery = []string{ReworkPath(
						artifact.Kind,
						input.Runtime.BoundREQID,
						artifact.BaselineGeneration+1,
						filepath.Base(artifact.Path),
					)}
				}
				decision := "block"
				retry := RetryNever
				humanRequired := artifact.Kind == "req"
				if !humanRequired {
					decision = "deny"
					retry = RetryAfterRecoveryValidation
				}
				return Decision{
					Decision:       decision,
					RuleID:         RuleLockedArtifactWrite,
					Reason:         RuleLockedArtifactWrite,
					AffectedPath:   path,
					Stage:          input.Runtime.CurrentStage,
					Recovery:       recovery,
					Retry:          retry,
					HumanRequired:  humanRequired,
					MatchedRuleIDs: []string{RuleLockedArtifactWrite},
				}, true
			}
		}
	}
	return Decision{}, false
}

// ReworkPath returns the versioned rework path for a locked artifact. S5
// after lock requires writes to land under docs/{kind}/versions/{REQ-ID}/
// g{generation}/{canonical-file-name} so the old generation stays
// immutable until manifest CAS retires it (REQ-039 §10.1.1).
func ReworkPath(kind, reqID string, generation int, canonicalFileName string) string {
	return filepath.Join(
		"docs",
		kind,
		"versions",
		reqID,
		fmt.Sprintf("g%d", generation),
		canonicalFileName,
	)
}

func provenMutationPaths(input Input) []string {
	if input.ToolName != "Bash" {
		path := toolPath(input.ToolInput)
		if path == "" {
			return nil
		}
		return []string{path}
	}
	command, _ := input.ToolInput["command"].(string)
	resolved, err := classifier.Resolve(command)
	if err != nil || !resolved.Mutates {
		return nil
	}
	return resolved.AffectedPaths
}

func (artifact LockedArtifact) complete() bool {
	return artifact.ID != "" &&
		artifact.Kind != "" &&
		artifact.Path != "" &&
		artifact.Version != "" &&
		artifact.SHA256 != "" &&
		artifact.LockedFromStage != "" &&
		artifact.BaselineGeneration > 0
}

type DecisionEnvelope struct {
	SchemaVersion           string    `json:"schema_version"`
	DecisionID              string    `json:"decision_id"`
	PolicyID                string    `json:"policy_id"`
	PolicyVersion           string    `json:"policy_version"`
	PolicySHA256            string    `json:"policy_sha256"`
	HookEvent               string    `json:"hook_event"`
	SessionID               string    `json:"session_id"`
	RuntimeID               *string   `json:"runtime_id"`
	ObservedRuntimeRevision *int      `json:"observed_runtime_revision"`
	AgentID                 *string   `json:"agent_id"`
	TargetID                *string   `json:"target_id"`
	MatchedRuleIDs          []string  `json:"matched_rule_ids"`
	Decision                string    `json:"decision"`
	RuleID                  *string   `json:"rule_id"`
	Reason                  string    `json:"reason"`
	Missing                 []string  `json:"missing"`
	Recovery                []string  `json:"recovery"`
	Retry                   string    `json:"retry"`
	HumanRequired           bool      `json:"human_required"`
	EvaluatedAt             string    `json:"evaluated_at"`
	ElapsedMS               int64     `json:"elapsed_ms"`
	Guidance                *Guidance `json:"guidance,omitempty"`
}

func (e *Engine) Envelope(input Input, decision Decision, evaluatedAt time.Time) DecisionEnvelope {
	runtimeID := nullableString(input.Runtime.RuntimeID)
	revision := nullableInt(input.Runtime.RuntimeID != "", input.Runtime.Revision)
	agentID := nullableString(input.AgentID)
	targetID := nullableString(input.TargetID)
	reason := decision.Reason
	missing := append([]string(nil), decision.Missing...)
	recovery := append([]string(nil), decision.Recovery...)
	retry := decision.Retry
	humanRequired := decision.HumanRequired
	matchedRuleIDs := append([]string(nil), decision.MatchedRuleIDs...)
	if decision.Decision == "allow" {
		reason = "No policy rule blocked or warned on this action."
		missing = []string{}
		recovery = []string{}
		retry = "not_applicable"
		humanRequired = false
		matchedRuleIDs = []string{}
	}
	if missing == nil {
		missing = []string{}
	}
	if recovery == nil {
		recovery = []string{}
	}
	retry = canonicalRetry(decision.Decision, retry)
	identity := fmt.Sprintf(
		"%s|%s|%s|%d|%s|%s|%s|%s|%s|%s",
		input.SessionID,
		input.Event,
		input.Runtime.RuntimeID,
		input.Runtime.Revision,
		input.AgentID,
		input.TargetID,
		input.ToolUseID,
		e.policySHA256,
		strings.Join(decision.MatchedRuleIDs, ","),
		eventFingerprint(input),
	)
	wireRuleID := canonicalWireRuleID(decision.RuleID)
	wireMatchedRuleIDs := canonicalWireRuleIDs(matchedRuleIDs)
	return DecisionEnvelope{
		SchemaVersion:           "1.1.0",
		DecisionID:              fmt.Sprintf("hook-decision-%x", sha256.Sum256([]byte(identity))),
		PolicyID:                e.policyID,
		PolicyVersion:           e.policyVersion,
		PolicySHA256:            e.policySHA256,
		HookEvent:               input.Event,
		SessionID:               input.SessionID,
		RuntimeID:               runtimeID,
		ObservedRuntimeRevision: revision,
		AgentID:                 agentID,
		TargetID:                targetID,
		MatchedRuleIDs:          wireMatchedRuleIDs,
		Decision:                decision.Decision,
		RuleID:                  nullableString(wireRuleID),
		Reason:                  reason,
		Missing:                 missing,
		Recovery:                recovery,
		Retry:                   retry,
		HumanRequired:           humanRequired,
		EvaluatedAt:             evaluatedAt.UTC().Format(time.RFC3339Nano),
		ElapsedMS:               decision.ElapsedMS,
		// Guidance is supplied by the Loop Controller, not the policy engine;
		// the minimal safety engine only emits a Decision.
	}
}

// eventFingerprint separates distinct native observation payloads when the
// platform does not provide a ToolUseID. It deliberately hashes only stable
// input facts so retries of the same event remain idempotent while different
// ConfigChange/PostToolUseFailure payloads are not collapsed by the outbox.
func eventFingerprint(input Input) string {
	payload := struct {
		ToolName  string         `json:"tool_name,omitempty"`
		FilePath  string         `json:"file_path,omitempty"`
		Error     string         `json:"error,omitempty"`
		Source    string         `json:"source,omitempty"`
		ToolInput map[string]any `json:"tool_input,omitempty"`
	}{
		ToolName:  input.ToolName,
		FilePath:  input.FilePath,
		Error:     input.Error,
		Source:    input.Source,
		ToolInput: input.ToolInput,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("event-%x", sum[:])
}

// canonicalWireRuleID is the boundary adapter between the internal rule
// names used by recovery anchors and the Hook envelope schema's stable
// HOOK_* identifiers. Internal callers may continue to compare the lowercase
// constants; serialized contracts get one canonical representation.
func canonicalWireRuleID(ruleID string) string {
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return ""
	}
	if strings.HasPrefix(ruleID, "HOOK_") {
		return ruleID
	}
	var builder strings.Builder
	builder.WriteString("HOOK_")
	lastUnderscore := false
	for _, r := range strings.ToUpper(ruleID) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.TrimRight(builder.String(), "_")
}

func canonicalWireRuleIDs(ruleIDs []string) []string {
	if len(ruleIDs) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(ruleIDs))
	seen := make(map[string]struct{}, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		canonical := canonicalWireRuleID(ruleID)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result
}

func matches(matcher, toolName string) bool {
	if matcher == "*" {
		return true
	}
	for _, value := range strings.Split(matcher, "|") {
		if value == toolName {
			return true
		}
	}
	return false
}

func isSideEffectTool(tool string) bool {
	switch tool {
	case "Write", "Edit", "MultiEdit", "Bash", "NotebookEdit":
		return true
	default:
		return IsMCPTool(tool) && !isVerifiedReadOnlyMCPTool(tool)
	}
}

func toolPath(input map[string]any) string {
	for _, key := range []string{"file_path", "path", "notebook_path"} {
		if value, ok := input[key].(string); ok {
			return filepath.Clean(value)
		}
	}
	return ""
}

func hasPathPrefix(path string, prefixes []string) bool {
	cleanPath := filepath.Clean(path)
	for _, prefix := range prefixes {
		cleanPrefix := filepath.Clean(prefix)
		if cleanPath == cleanPrefix || strings.HasPrefix(cleanPath, cleanPrefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func samePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullableInt(ok bool, value int) *int {
	if !ok {
		return nil
	}
	return &value
}

// lockStageReached reports whether the current lifecycle stage has reached
// the artifact's lock stage ("S6" locks from S6 onward). Unparseable stages
// fail closed (treat as reached) — an unreadable stage must not silently
// unlock a baseline artifact.
func lockStageReached(current, lockStage string) bool {
	c, okC := stageNumber(current)
	l, okL := stageNumber(lockStage)
	if !okC || !okL {
		return true
	}
	return c >= l
}

func stageNumber(stage string) (int, bool) {
	if len(stage) < 2 || stage[0] != 'S' {
		return 0, false
	}
	n := 0
	for _, r := range stage[1:] {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
