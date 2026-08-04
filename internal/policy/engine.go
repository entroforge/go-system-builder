package policy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/classifier"
)

// Minimal Safety Policy — REQ-039 v2.0.0 §14 / BE-039 v1.0.2 §6.
//
// The enforce path produces exactly two block reasons:
//
//   - locked_artifact_write  — the affected path matches a LockedArtifact
//     manifest entry whose identity is complete (ID/kind/path/version/
//     sha256/locked_from_stage/baseline_generation).
//   - squash_merge           — tokenized resolver proves the command is a
//     git merge --squash or gh pr merge --squash.
//
// All other predicates (activation, scope, Team, UI prototype, clean round,
// policy tamper, runtime integrity, subagent report, teammate idle report,
// permission expansion, etc.) have been retired and now live in Guidance,
// Quality Gate, Transition Guard or Integration precondition (BE-039 §6.3,
// ARCHITECTURE-039 §10.3).

const (
	RuleLockedArtifactWrite = "locked_artifact_write"
	RuleSquashMerge         = "squash_merge"
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
	RuntimeID          string           `json:"runtime_id"`
	Revision           int              `json:"revision"`
	BoundREQID         string           `json:"bound_req_id,omitempty"`
	BoundREQPath       string           `json:"bound_req_path"`
	BoundREQUIImpact   string           `json:"bound_req_ui_impact"`
	Agent              *AgentContext    `json:"agent"`
	CurrentState       string           `json:"current_state"`
	CurrentPhase       string           `json:"current_phase"`
	Paused             bool             `json:"paused"`
	CleanRound         any              `json:"clean_round"`
	CurrentReviewRound int              `json:"current_review_round"`
	EvidenceValidCount int              `json:"evidence_valid_count"`
	OpenBlockingBugs   int              `json:"open_blocking_bugs"`
	Teams              []TeamSummary    `json:"teams,omitempty"`
	LastActivityAt     string           `json:"last_activity_at,omitempty"`
	ProjectRoot        string           `json:"project_root,omitempty"`
	CurrentStage       string           `json:"current_stage,omitempty"`
	LockedArtifacts    []LockedArtifact `json:"locked_artifacts,omitempty"`
}

type Input struct {
	SessionID string          `json:"session_id"`
	Event     string          `json:"hook_event_name"`
	AgentID   string          `json:"agent_id"`
	ToolName  string          `json:"tool_name"`
	ToolInput map[string]any  `json:"tool_input"`
	TargetID  string          `json:"target_id"`
	Facts     map[string]bool `json:"facts"`
	Runtime   RuntimeContext  `json:"runtime_context"`
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
// entry. The minimal policy document only contains entries whose predicate
// is locked_artifact_write or squash_merge; the engine loads the document
// to keep HasRule / PolicyVersion compatibility for Hook adapter tests and
// the schema-version envelope, but does not iterate the rules to produce
// a Decision.
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
	if decision, blocked := lockedArtifactDecision(input); blocked {
		return decision, nil
	}
	if decision, blocked := squashMergeDecision(input); blocked {
		return decision, nil
	}
	return Decision{Decision: "allow"}, nil
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
		Decision:       "block",
		RuleID:         RuleSquashMerge,
		Reason:         RuleSquashMerge,
		ParsedCommand:  parsedCommand,
		Stage:          input.Runtime.CurrentStage,
		Recovery:       []string{"use a normal merge without squash"},
		Retry:          "with_normal_merge",
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
		for _, path := range affectedPaths {
			if artifact.complete() &&
				samePath(path, artifact.Path) {
				recovery := []string{"create a new version through the formal rework path"}
				if input.Runtime.BoundREQID != "" && artifact.Kind != "requirement" {
					recovery = []string{ReworkPath(
						artifact.Kind,
						input.Runtime.BoundREQID,
						artifact.BaselineGeneration+1,
						filepath.Base(artifact.Path),
					)}
				}
				return Decision{
					Decision:       "block",
					RuleID:         RuleLockedArtifactWrite,
					Reason:         RuleLockedArtifactWrite,
					AffectedPath:   path,
					Stage:          input.Runtime.CurrentStage,
					Recovery:       recovery,
					Retry:          "after_rework",
					HumanRequired:  artifact.Kind == "requirement",
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
	Guidance                *Guidance `json:"guidance,omitempty"`
}

func (e *Engine) Envelope(input Input, decision Decision, evaluatedAt time.Time) DecisionEnvelope {
	runtimeID := nullableString(input.Runtime.RuntimeID)
	revision := nullableInt(input.Runtime.RuntimeID != "", input.Runtime.Revision)
	agentID := nullableString(input.AgentID)
	targetID := nullableString(input.TargetID)
	ruleID := nullableString(decision.RuleID)
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
	identity := fmt.Sprintf(
		"%s|%s|%s|%d|%s|%s|%s|%s",
		input.SessionID,
		input.Event,
		input.Runtime.RuntimeID,
		input.Runtime.Revision,
		input.AgentID,
		input.TargetID,
		e.policySHA256,
		strings.Join(decision.MatchedRuleIDs, ","),
	)
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
		MatchedRuleIDs:          matchedRuleIDs,
		Decision:                decision.Decision,
		RuleID:                  ruleID,
		Reason:                  reason,
		Missing:                 missing,
		Recovery:                recovery,
		Retry:                   retry,
		HumanRequired:           humanRequired,
		EvaluatedAt:             evaluatedAt.UTC().Format(time.RFC3339Nano),
		// Guidance is supplied by the Loop Controller, not the policy engine;
		// the minimal safety engine only emits a Decision.
	}
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
		return false
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
