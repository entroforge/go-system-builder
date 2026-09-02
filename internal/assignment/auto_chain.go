// auto_chain.go — plan_checkpoint continuous-execution auto-activation
// (L4 §3.3 / SPEC-TASK-1 owner ruling).
//
// When a plan_checkpoint agent sends PLAN_REPORT via SendMessage, the
// PostToolUse(SendMessage) hook observer records the dispatch envelope
// (existing plan_reported_ref behavior). The auto-chain extends this with a
// driven chain that, on the same hook invocation, advances the agent
// reading -> understanding_submitted -> activated -> working, with the
// activation envelope's hash chain bound to the captured plan_report file
// bytes. This removes the three hand-typed `runtime agent-event` commands
// that were blocking ~70% of new agents from ever submitting a Result.
//
// Safety posture:
//   - The auto-chain runs only when dispatch_mode=plan_checkpoint.
//   - plan_approval_required agents keep the human Gate (no auto-advance).
//   - one_shot assignments are not pre-staged (no envelope on disk) so the
//     chain can never activate one — it returns an observation gap instead.
//   - Sender identification gaps fail silent (the hook never invents an
//     agent binding).
//   - Each AdvanceAgent call is a separate Runtime Writer commit via
//     assignment.AdvanceAgent, so the existing dispatch-mode / state /
//     hash-chain guards stay in force (we do NOT bypass
//     verifyActivationReadbackChain). The normal hook path omits the
//     revision assertion; an explicit AgentBegin assertion remains available
//     to recovery/integration callers.
//   - The activation envelope (the file at agent.activation_ref) is
//     rewritten in lockstep with the synthesized activation message so the
//     Hook loader and the runtime see the same allowed_* fields.
package assignment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/entroforge/go-system-builder/internal/identity"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// AutoChainOutcome reports what the auto-chain observed and did.
type AutoChainOutcome struct {
	Chained      bool   `json:"chained"`
	AgentID      string `json:"agent_id"`
	FinalState   string `json:"final_state,omitempty"`
	ActivationID string `json:"activation_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Note         string `json:"note,omitempty"`
}

// AutoChainInput is the minimal fact set the auto-chain needs.
type AutoChainInput struct {
	Root        string
	StatePath   string
	JournalPath string
	// ExpectedRevision is an optional explicit assertion for the recovery or
	// integration caller. The passive PostToolUse hook leaves it negative so
	// each Writer commit uses the current Runtime under lock.
	ExpectedRevision int
	// AgentID is the resolved sender (PostToolUse identifySender output).
	AgentID string
	// PlanPath is the on-disk plan_report JSON. Required for hash binding.
	PlanPath string
	// EnvelopeOverride supplies the activation capability set when the agent
	// row carries no pre-staged envelope (legacy registration predating
	// register-workgroup pre-staging). Only the explicit recovery verb
	// (`runtime agent-begin`) sets this — the passive hook path never
	// synthesizes capabilities for an agent the dispatcher did not vet.
	EnvelopeOverride *ActivationSourceEntry
	// OccurredAt is the wall-clock time stamped on each AdvanceAgent call.
	// Zero means "now".
	OccurredAt time.Time
}

// AutoAdvanceToWorking drives a plan_checkpoint agent from
// reading -> understanding_submitted -> activated -> working in three
// Runtime Writer commits, with the activation envelope's hash chain bound to
// the captured plan_report file bytes.
//
// Returns an outcome with Chained=false (no error) when the input is not
// actionable (agent has wrong dispatch_mode, missing plan path, missing
// pre-staged envelope). The caller (PostToolUse hook) treats such outcomes
// as silent skips and the agent stays in its current state; the recovery
// path is `runtime agent-begin --agent-id <id> --plan <file>`.
//
// Errors are reserved for actual filesystem / schema / Writer failures that
// the caller should surface (so the agent's tool call still proceeds but
// the stderr note names the failure).
func AutoAdvanceToWorking(in AutoChainInput) (AutoChainOutcome, error) {
	if in.AgentID == "" {
		return AutoChainOutcome{Reason: "agent_id is required"}, nil
	}
	if err := identity.ValidateAgentID(in.AgentID); err != nil {
		return AutoChainOutcome{AgentID: in.AgentID, Reason: err.Error()}, nil
	}
	if in.PlanPath == "" {
		return AutoChainOutcome{AgentID: in.AgentID, Reason: "plan_path missing in SendMessage payload — run `runtime agent-begin --agent-id " + in.AgentID + " --plan <plan-report.json>` to recover"}, nil
	}
	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	store := loopruntime.NewWriter(in.StatePath, in.JournalPath, in.Root, semantic.RuntimeCandidateValidator{})
	snapshot, err := store.Snapshot()
	if err != nil {
		return AutoChainOutcome{}, fmt.Errorf("auto-chain: snapshot: %w", err)
	}
	// Locate the agent row.
	agentsRaw, _ := snapshot.State["entities"].(map[string]any)["agents"].([]any)
	var agent map[string]any
	for _, raw := range agentsRaw {
		a, _ := raw.(map[string]any)
		if a == nil {
			continue
		}
		if a["id"] == in.AgentID {
			agent = a
			break
		}
	}
	if agent == nil {
		return AutoChainOutcome{AgentID: in.AgentID, Reason: "agent not found in runtime"}, nil
	}
	// Skip non plan_checkpoint agents (defense in depth — the hook also
	// filters, but a re-entrant call from agent-begin must still gate).
	mode, _ := agent["dispatch_mode"].(string)
	if mode != "plan_checkpoint" {
		return AutoChainOutcome{AgentID: in.AgentID, Reason: "dispatch_mode " + mode + " is not plan_checkpoint (auto-chain is plan_checkpoint only)"}, nil
	}
	currentState, _ := agent["state"].(string)
	activationRef, _ := agent["activation_ref"].(string)
	agentDefRef, _ := agent["definition_ref"].(string)
	teamID, _ := agent["team_id"].(string)
	taskIDs, _ := agent["task_ids"].([]any)
	var taskID string
	if len(taskIDs) > 0 {
		taskID, _ = taskIDs[0].(string)
	}
	var envelopeSource ActivationSourceEntry
	if activationRef == "" {
		// No pre-staged envelope — register-workgroup did not pre-stage this
		// agent (legacy registration). The passive hook path skips silently;
		// only the explicit recovery verb may supply a manifest-sourced
		// override.
		if in.EnvelopeOverride == nil {
			return AutoChainOutcome{AgentID: in.AgentID, Reason: "no pre-staged activation envelope (register-workgroup did not pre-stage this agent); run `runtime agent-begin --agent-id " + in.AgentID + " --plan <plan-report.json>` to recover"}, nil
		}
		envelopeSource = *in.EnvelopeOverride
	} else {
		envelopeSource, err = ResolveAgentActivationEnvelope(in.Root, activationRef)
		if err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: read envelope: %w", err)
		}
	}
	envelopeSource.AgentDefinitionRef = agentDefRef
	envelopeSource.AgentID = in.AgentID
	runtimeID := runtimeIDFromSnapshot(snapshot)
	workgroupID := teamID
	expectedRevision := in.ExpectedRevision

	// Step 1: readback_submitted (plan_report -> reading -> understanding_submitted).
	readbackPath := resolveRootPath(in.Root, in.PlanPath)
	readbackData, err := os.ReadFile(readbackPath)
	if err != nil {
		return AutoChainOutcome{}, fmt.Errorf("auto-chain: read plan file: %w", err)
	}
	// Validate the plan_report against the schema so the chain cannot pass
	// a malformed file through to activation (the runtime would still catch
	// this via AdvanceAgent's message validation, but failing here gives
	// the hook caller a clearer error path).
	var planCheck map[string]any
	if err := json.Unmarshal(readbackData, &planCheck); err != nil {
		return AutoChainOutcome{AgentID: in.AgentID, Reason: "plan file is not valid JSON: " + err.Error()}, nil
	}
	if t, _ := planCheck["message_type"].(string); t != "plan_report" {
		return AutoChainOutcome{AgentID: in.AgentID, Reason: "plan file message_type is " + t + ", not plan_report"}, nil
	}
	// The chain is resumable, not just idempotent: an earlier run may have
	// failed mid-chain (e.g. the activation message could not be built),
	// leaving the agent at understanding_submitted or activated. Re-enter at
	// the step matching the current state instead of stranding the agent.
	switch currentState {
	case "reading":
		submitted, err := AdvanceAgent(in.Root, in.StatePath, in.JournalPath, AgentEventRequest{
			ExpectedRevision: expectedRevision,
			AgentID:          in.AgentID,
			Event:            "readback_submitted",
			MessagePath:      readbackPath,
			OccurredAt:       occurredAt,
		})
		if err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: readback_submitted: %w", err)
		}
		if expectedRevision >= 0 {
			expectedRevision = submitted.Revision
		}
	case "understanding_submitted", "activated":
		// Resume below at the matching step.
	default:
		return AutoChainOutcome{AgentID: in.AgentID, FinalState: currentState, Reason: "agent state is " + currentState + " — auto-chain is idempotent; nothing to do"}, nil
	}
	var activationID, correlationID string
	if currentState == "reading" || currentState == "understanding_submitted" {
		// Step 2: activation_sent (understanding_submitted -> activated).
		activationMessage, err := BuildPlanCheckpointActivationMessage(
			in.Root,
			runtimeID,
			in.AgentID,
			agentDefRef,
			taskID,
			workgroupID,
			expectedRevision,
			in.PlanPath,
			envelopeSource,
			occurredAt.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: build activation: %w", err)
		}
		activationID, _ = activationMessage["activation_id"].(string)
		correlationID, _ = activationMessage["correlation_id"].(string)
		activationMessagePath, err := WriteActivationMessageFile(in.Root, workgroupID, taskID, in.AgentID, activationMessage)
		if err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: write activation message: %w", err)
		}
		// Schema-validate the synthesized message so AdvanceAgent does not
		// produce a different error.
		if err := ValidateActivationMessageBytes(in.Root, mustRead(activationMessagePath)); err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: validate activation message: %w", err)
		}
		activated, err := AdvanceAgent(in.Root, in.StatePath, in.JournalPath, AgentEventRequest{
			ExpectedRevision: expectedRevision,
			AgentID:          in.AgentID,
			Event:            "activation_sent",
			MessagePath:      activationMessagePath,
			OccurredAt:       occurredAt,
		})
		if err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: activation_sent: %w", err)
		}
		if expectedRevision >= 0 {
			expectedRevision = activated.Revision
		}
	} else {
		// Resume from activated: recover the activation identity from the
		// stored activation message so the work_start message correlates
		// with the activation the runtime recorded.
		stored, readErr := os.ReadFile(resolveRootPath(in.Root, activationRef))
		if readErr != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: resume from activated: read stored activation %s: %w", activationRef, readErr)
		}
		var storedMsg map[string]any
		if err := json.Unmarshal(stored, &storedMsg); err != nil {
			return AutoChainOutcome{}, fmt.Errorf("auto-chain: resume from activated: decode stored activation: %w", err)
		}
		activationID, _ = storedMsg["activation_id"].(string)
		correlationID, _ = storedMsg["correlation_id"].(string)
	}

	// Step 3: work_started (activated -> working).
	workStartPath, err := WriteWorkStartMessageFile(
		in.Root,
		workgroupID,
		taskID,
		in.AgentID,
		agentDefRef,
		workgroupID,
		runtimeID,
		activationID,
		correlationID,
		expectedRevision,
		occurredAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return AutoChainOutcome{}, fmt.Errorf("auto-chain: write work_start: %w", err)
	}
	if _, err := AdvanceAgent(in.Root, in.StatePath, in.JournalPath, AgentEventRequest{
		ExpectedRevision: expectedRevision,
		AgentID:          in.AgentID,
		Event:            "work_started",
		MessagePath:      workStartPath,
		OccurredAt:       occurredAt,
	}); err != nil {
		return AutoChainOutcome{}, fmt.Errorf("auto-chain: work_started: %w", err)
	}
	return AutoChainOutcome{
		Chained:      true,
		AgentID:      in.AgentID,
		FinalState:   "working",
		ActivationID: activationID,
		Note:         "plan_checkpoint auto-activation: chained readback_submitted -> activation_sent -> work_started in one PostToolUse(SendMessage) observation",
	}, nil
}

// runtimeIDFromSnapshot extracts runtime_id from the snapshot.
func runtimeIDFromSnapshot(snap loopruntime.Snapshot) string {
	id, _ := snap.State["runtime_id"].(string)
	return id
}

// =============================================================================
// runtime agent-begin — the recovery verb for plan_checkpoint agents whose
// PostToolUse(SendMessage) auto-chain could not drive the lifecycle (e.g.
// the Worker sent PLAN_REPORT without a `plan_ref` payload field, or the
// hook failed before reaching the chain). Performs the same three Writer
// commits the auto-chain performs but driven by an explicit CLI call, so
// the recovery path matches the happy path exactly (no bypass of
// verifyActivationReadbackChain or any other AdvanceAgent guard).
// =============================================================================

// AgentBeginRequest is the wire shape for runtime agent-begin.
type AgentBeginRequest struct {
	ExpectedRevision int
	AgentID          string
	PlanPath         string
	OccurredAt       time.Time
}

// AgentBegin is the public entry point for `runtime agent-begin
// --agent-id <id> --plan <file>`. It performs the same chain the
// PostToolUse auto-chain performs (readback_submitted -> activation_sent ->
// work_started) but driven explicitly. When the agent row carries no
// pre-staged activation envelope (legacy registration predating
// register-workgroup pre-staging), AgentBegin synthesizes the capability
// set from the workgroup manifest row that dispatched the agent — the
// recovery path never invents permissions beyond what the dispatcher
// declared.
//
// The outcome is an AutoChainOutcome so the CLI output is byte-compatible
// between the auto-chain and the recovery verb.
func AgentBegin(root, statePath, journalPath string, req AgentBeginRequest) (loopruntime.Snapshot, AutoChainOutcome, error) {
	if req.AgentID == "" {
		return loopruntime.Snapshot{}, AutoChainOutcome{Reason: "agent_id is required"}, fmt.Errorf("runtime agent-begin: agent_id is required")
	}
	if err := identity.ValidateAgentID(req.AgentID); err != nil {
		return loopruntime.Snapshot{}, AutoChainOutcome{AgentID: req.AgentID, Reason: err.Error()}, fmt.Errorf("runtime agent-begin: %w", err)
	}
	if req.PlanPath == "" {
		return loopruntime.Snapshot{}, AutoChainOutcome{AgentID: req.AgentID, Reason: "plan path is required"}, fmt.Errorf("runtime agent-begin: --plan is required")
	}
	override, err := legacyEnvelopeOverride(root, statePath, journalPath, req.AgentID)
	if err != nil {
		return loopruntime.Snapshot{}, AutoChainOutcome{AgentID: req.AgentID, Reason: err.Error()}, err
	}
	outcome, err := AutoAdvanceToWorking(AutoChainInput{
		Root:             root,
		StatePath:        statePath,
		JournalPath:      journalPath,
		ExpectedRevision: req.ExpectedRevision,
		AgentID:          req.AgentID,
		PlanPath:         req.PlanPath,
		EnvelopeOverride: override,
		OccurredAt:       req.OccurredAt,
	})
	if err != nil {
		return loopruntime.Snapshot{}, outcome, err
	}
	// Re-snapshot to return the final state to the CLI for printing.
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	finalSnap, snapErr := store.Snapshot()
	if snapErr != nil {
		return loopruntime.Snapshot{}, outcome, fmt.Errorf("runtime agent-begin: snapshot: %w", snapErr)
	}
	if !outcome.Chained {
		return finalSnap, outcome, fmt.Errorf("runtime agent-begin: %s", outcome.Reason)
	}
	return finalSnap, outcome, nil
}

// legacyEnvelopeOverride resolves the manifest-sourced capability set for an
// agent whose row carries no pre-staged activation_ref. Returns nil (no
// override) when the agent already has a pre-staged envelope — the happy
// path reads that file instead.
func legacyEnvelopeOverride(root, statePath, journalPath, agentID string) (*ActivationSourceEntry, error) {
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, err := store.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("runtime agent-begin: snapshot: %w", err)
	}
	agentsRaw, _ := snapshot.State["entities"].(map[string]any)["agents"].([]any)
	var agent map[string]any
	for _, raw := range agentsRaw {
		a, _ := raw.(map[string]any)
		if a != nil && a["id"] == agentID {
			agent = a
			break
		}
	}
	if agent == nil {
		return nil, fmt.Errorf("runtime agent-begin: agent %s is not registered", agentID)
	}
	if ref, _ := agent["activation_ref"].(string); ref != "" {
		return nil, nil
	}
	var taskID string
	if taskIDs, _ := agent["task_ids"].([]any); len(taskIDs) > 0 {
		taskID, _ = taskIDs[0].(string)
	}
	if taskID == "" {
		return nil, fmt.Errorf("runtime agent-begin: agent %s has no pre-staged envelope and no task binding to source one from", agentID)
	}
	entry, err := manifestRowForAgent(root, taskID, agentID)
	if err != nil {
		return nil, fmt.Errorf("runtime agent-begin: %w", err)
	}
	return entry, nil
}

// manifestRowForAgent reads .claude/workgroups/<REQ>/<taskID>/manifest.json
// and extracts the capability row for agentID (matching on agent_id, or the
// single row when the manifest declares exactly one assignment).
func manifestRowForAgent(root, taskID, agentID string) (*ActivationSourceEntry, error) {
	pattern := filepath.Join(root, ".claude", "workgroups", "*", taskID, "manifest.json")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("no workgroup manifest found for task %s; cannot synthesize an activation envelope", taskID)
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var manifest struct {
			Assignments []struct {
				AgentID            string   `json:"agent_id"`
				AgentDefinitionRef string   `json:"agent_definition_ref"`
				SkillRefs          []string `json:"skill_refs"`
				WritePaths         []string `json:"write_paths"`
				OutputPaths        []string `json:"output_paths"`
			} `json:"assignments"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		for _, row := range manifest.Assignments {
			if row.AgentID == agentID || (row.AgentID == "" && len(manifest.Assignments) == 1) {
				return &ActivationSourceEntry{
					AgentID:            agentID,
					AgentDefinitionRef: row.AgentDefinitionRef,
					SkillRefs:          row.SkillRefs,
					WritePaths:         row.WritePaths,
					OutputPaths:        row.OutputPaths,
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("no manifest assignment row for agent %s under task %s; cannot synthesize an activation envelope", agentID, taskID)
}

// resolveRootPath turns a possibly-relative path into an absolute one
// anchored at the project root. Mirrors the helper in the cli/run.go
// transport.
func resolveRootPath(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// mustRead reads a file or panics. Used only after a successful Write, so
// the panic is genuinely unreachable and saves a lengthier error path.
func mustRead(p string) []byte {
	data, err := os.ReadFile(p)
	if err != nil {
		panic(fmt.Sprintf("mustRead: %v", err))
	}
	return data
}
