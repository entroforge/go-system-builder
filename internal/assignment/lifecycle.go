// lifecycle.go — TASK-015 Agent event surface executor.
//
// Per BUG-003 §4b.1 Failure 2 + §4b.2(c), the runtime's Agent event surface
// was limited to three events (readback_submitted, understanding_approved,
// activated). TASK-015 replaces that with a table-driven dispatcher that
// resolves every (from, event) pair from loop-definition.json's
// entity_lifecycles.agent.transitions. The canonical 12 events are
// (read from catalog, NOT hard-coded):
//
//	readback_started, readback_submitted, understanding_approved,
//	understanding_rejected, document_conflict_reported, activation_sent,
//	work_started, completion_reported, completion_acknowledged,
//	work_blocked, blocker_resolved, shutdown_approved.
//
// Each handler:
//   - reads the message envelope (schema-validated via the agent-message
//     schema),
//   - resolves the (current_state, event) -> next_state transition via
//     transition.LoadCatalog,
//   - validates any required params,
//   - mutates the agent's state in place,
//   - records evidence references.
//
// The CLI flag help text and error messages reference the full 12-event
// list (not "3 events") so future callers see the complete surface.
package assignment

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/agentauth"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"os"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/repair"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type AgentEventRequest struct {
	ExpectedRevision int
	AgentID          string
	Event            string
	MessagePath      string
	Params           map[string]any `json:"params,omitempty"`
	OccurredAt       time.Time
	// Process-local proof, never decoded from an authored message or CLI flags.
	IssuedRepairResultBlocker *repair.IssuedRepairResultBlockerCapability `json:"-"`
	// Main-issued S9 resolution proof. It is mutually exclusive with the
	// Worker blocker proof and only authorizes blocker_resolved.
	IssuedRepairResultResolution *repair.IssuedRepairResultResolutionCapability `json:"-"`
}

type issuedRepairResultLifecycleCapability interface {
	MessageRef() string
	EvidenceIDs() []string
	ValidateLifecycleRequest(root, owner, event, messagePath string, beforeRevision, generation int) error
	AcceptedReplay(ctx context.Context, root string, state map[string]any, owner, event, messagePath string) (bool, error)
	ApplyLifecycleCAS(ctx context.Context, root string, state map[string]any, messageBytes []byte, owner, taskID string, beforeRevision, generation int) error
}

type agentMessage = agentauth.Message

// expectedAgentMessageType returns the canonical message_type expected for a
// given Agent event. The mapping is the engine's contract; mismatched
// message types are rejected with a typed error.
//
// readback_submitted accepts BOTH the plan_checkpoint plan_report and the
// approval-mode readback_response — applyAgentEventParams enforces the
// dispatch-mode-specific choice once the agent row is in hand (the message
// is validated before the agent is located).
//
// readback_started / understanding_rejected / document_conflict_reported /
// understanding_approved / activation_sent / work_started / completion_reported /
// completion_acknowledged / work_blocked / blocker_resolved /
// shutdown_approved all use their own message_type.
func expectedAgentMessageType(event string) string {
	switch event {
	case "readback_submitted":
		return "plan_report|readback_response"
	case "understanding_approved", "understanding_rejected",
		"readback_started", "document_conflict_reported":
		return "readback_response"
	case "activation_sent":
		return "activation"
	case "work_started":
		return "work_start"
	case "completion_reported":
		return "completion_report"
	case "completion_acknowledged":
		return "completion_ack"
	case "work_blocked":
		return "blocker_report"
	case "blocker_resolved":
		return "blocker_resolution"
	case "shutdown_approved":
		return "shutdown_approval"
	}
	return ""
}

// AdvanceAgent applies one Agent lifecycle event.
//
// The full 12-event surface (resolved from loop-definition.json
// entity_lifecycles.agent.transitions) is supported. Each event resolves
// its (from, to) pair via the catalog; if no pair matches the agent's
// current state, the event is rejected with a typed error.
func AdvanceAgent(
	root, statePath, journalPath string,
	request AgentEventRequest,
) (loopruntime.Snapshot, error) {
	return AdvanceAgentWithContext(context.Background(), root, statePath, journalPath, request)
}

// AdvanceAgentWithContext extends the original lifecycle CAS with opaque S9
// blocker and Main resolution capabilities. Only those typed branches acquire
// the original Main workspace lock; legacy callers retain their lock behavior.
func AdvanceAgentWithContext(ctx context.Context, root, statePath, journalPath string, request AgentEventRequest) (loopruntime.Snapshot, error) {
	blockerCapability := request.IssuedRepairResultBlocker
	resolutionCapability := request.IssuedRepairResultResolution
	if blockerCapability != nil && resolutionCapability != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("S9 Agent lifecycle accepts only one typed capability")
	}
	if blockerCapability != nil && request.Event != "work_blocked" {
		return loopruntime.Snapshot{}, fmt.Errorf("S9 blocker capability only authorizes work_blocked")
	}
	if resolutionCapability != nil && request.Event != "blocker_resolved" {
		return loopruntime.Snapshot{}, fmt.Errorf("S9 resolution capability only authorizes blocker_resolved")
	}
	var capability issuedRepairResultLifecycleCapability
	if blockerCapability != nil {
		capability = blockerCapability
	} else if resolutionCapability != nil {
		capability = resolutionCapability
	}
	if request.AgentID == "" || request.Event == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("agent_id and event are required")
	}
	if err := identity.ValidateAgentID(request.AgentID); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("runtime agent-event: %w", err)
	}
	if capability != nil {
		refs := capability.EvidenceIDs()
		if capability.MessageRef() == "" || len(refs) != 2 || refs[0] != capability.MessageRef() || refs[0] == refs[1] {
			return loopruntime.Snapshot{}, fmt.Errorf("S9 typed lifecycle capability has no complete immutable lifecycle refs")
		}
	}
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	var recovered *loopruntime.Snapshot
	if capability != nil {
		if ctx == nil {
			return loopruntime.Snapshot{}, fmt.Errorf("typed S9 lifecycle requires the original caller context")
		}
		lockedCtx, unlock, err := integration.LockWorkspace(ctx, root)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		defer unlock()
		ctx = lockedCtx
		snapshot, err := store.Snapshot()
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		replayed, err := capability.AcceptedReplay(ctx, root, snapshot.State, request.AgentID, request.Event, request.MessagePath)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		if replayed {
			return snapshot, nil
		}
		baseline, _ := snapshot.State["baseline"].(map[string]any)
		if err := capability.ValidateLifecycleRequest(root, request.AgentID, request.Event, request.MessagePath, snapshot.Revision, dispatch.Number(baseline["generation"])); err != nil {
			return loopruntime.Snapshot{}, err
		}
		recovered = &snapshot
	}
	var data []byte
	var err error
	if capability != nil {
		data, err = workpacket.ReadFile(root, capability.MessageRef(), 1<<20)
	} else {
		data, err = os.ReadFile(request.MessagePath)
	}
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read Agent message: %w", err)
	}
	if err := schema.NewValidator(root).ValidateBytes(
		"agent-message.schema.json",
		data,
	); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("Agent message schema: %w", err)
	}
	var message agentMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode Agent message: %w", err)
	}
	expectedType := expectedAgentMessageType(request.Event)
	if expectedType == "" {
		return loopruntime.Snapshot{}, fmt.Errorf(
			"unsupported Agent event %q; canonical 12 events: readback_started, readback_submitted, understanding_approved, understanding_rejected, document_conflict_reported, activation_sent, work_started, completion_reported, completion_acknowledged, work_blocked, blocker_resolved, shutdown_approved",
			request.Event)
	}
	typeMatches := message.MessageType == expectedType
	if !typeMatches && strings.Contains(expectedType, "|") {
		for _, candidate := range strings.Split(expectedType, "|") {
			if message.MessageType == candidate {
				typeMatches = true
				break
			}
		}
	}
	if !typeMatches || message.AgentID != request.AgentID {
		return loopruntime.Snapshot{}, fmt.Errorf("Agent message does not match event or Agent (event %q expects message_type %q, got %q)", request.Event, expectedType, message.MessageType)
	}

	var current map[string]any
	if recovered != nil {
		current = recovered.State
	} else {
		stateData, err := os.ReadFile(statePath)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
		}
		if err := json.Unmarshal(stateData, &current); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
		}
	}
	validateIssuedTarget := func(state map[string]any) error {
		if request.Event != "work_blocked" && request.Event != "blocker_resolved" {
			return nil
		}
		protected, err := issuedRepairResultLifecycleTarget(state, request.AgentID, loopruntime.WithPacketBodyRoot(ctx, root))
		if err != nil {
			return err
		}
		if protected && capability == nil {
			return fmt.Errorf("issued S9 target requires its private typed lifecycle capability; preserve the answer and use its original blocker receiver or Main resolution")
		}
		if capability != nil && !protected {
			return fmt.Errorf("S9 typed lifecycle capability does not match a retained issued target")
		}
		return nil
	}
	if err := validateIssuedTarget(current); err != nil {
		return loopruntime.Snapshot{}, err
	}
	runtimeID, _ := current["runtime_id"].(string)
	baseline, _ := current["baseline"].(map[string]any)
	generation := dispatch.Number(baseline["generation"])
	if generation < 1 {
		return loopruntime.Snapshot{}, fmt.Errorf("Agent lifecycle requires the current positive baseline generation")
	}
	commitRevision, err := commitRevision(request.ExpectedRevision, current)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	if message.RuntimeID != "" && message.RuntimeID != runtimeID {
		return loopruntime.Snapshot{}, fmt.Errorf("Agent message runtime does not match current runtime")
	}
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	messageRef := repositoryPath(root, request.MessagePath)
	evidenceIDs := []string{messageRef}
	if capability != nil {
		messageRef = capability.MessageRef()
		evidenceIDs = capability.EvidenceIDs()
		if err := capability.ValidateLifecycleRequest(root, request.AgentID, request.Event, request.MessagePath, commitRevision, generation); err != nil {
			return loopruntime.Snapshot{}, err
		}
		if len(evidenceIDs) != 2 || evidenceIDs[0] == evidenceIDs[1] || evidenceIDs[0] != messageRef {
			return loopruntime.Snapshot{}, fmt.Errorf("S9 blocker capability has incomplete lifecycle evidence refs")
		}
	}

	// Typed professional blockers record the current active responsibility
	// directly. Generic lifecycle calls still resolve their catalog transition.
	var agentTransitions agentEntityTransitionsResolver
	if blockerCapability == nil {
		catalog, err := transition.LoadCatalog(root)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("load catalog: %w", err)
		}
		agentTransitions = agentEntityTransitions(catalog)
	}

	mutation := loopruntime.Mutation{
		EventID:            fmt.Sprintf("evt-agent-%s-%s-r%d", request.AgentID, request.Event, commitRevision+1),
		TransitionID:       "AGENT-LIFECYCLE",
		Event:              request.Event,
		Actor:              "orchestrator",
		IdempotencyKey:     fmt.Sprintf("runtime:agent:%s:%s:%d", request.AgentID, request.Event, commitRevision),
		RuntimeID:          runtimeID,
		BaselineGeneration: generation,
		From:               cursor,
		To:                 cursor,
		EvidenceIDs:        evidenceIDs,
		Message:            fmt.Sprintf("Recorded a schema-valid Agent lifecycle event (%s)", request.Event),
		OccurredAt:         occurredAt,
		Apply: func(state map[string]any) error {
			currentBaseline, _ := state["baseline"].(map[string]any)
			if state["runtime_id"] != runtimeID || dispatch.Number(currentBaseline["generation"]) != generation {
				return fmt.Errorf("Agent lifecycle runtime/generation changed before its original CAS")
			}
			if err := validateIssuedTarget(state); err != nil {
				return err
			}
			record, canonical, err := assignmentrecord.ForOwner(state, request.AgentID)
			if err != nil {
				return err
			}
			var authored map[string]any
			if err := json.Unmarshal(data, &authored); err != nil {
				return err
			}
			_, hasCanonicalPlan := authored["assignment_record_contract"]
			if request.Event == "readback_submitted" && message.MessageType == "plan_report" && (canonical || hasCanonicalPlan) {
				if err := plancheckpoint.Validate(root, loopruntime.Snapshot{State: state}, request.AgentID, messageRef); err != nil {
					return err
				}
			}
			if canonical && request.Event == "activation_sent" {
				if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) {
					return assignmentinputs.Read(root, ref)
				}); err != nil {
					return err
				}
				var projection map[string]any
				if err := json.Unmarshal(data, &projection); err != nil {
					return err
				}
				if err := assignmentrecord.ValidateProjection(record, projection); err != nil {
					return err
				}
				if record.DispatchMode == "plan_checkpoint" {
					entities, _ := state["entities"].(map[string]any)
					agents, _ := entities["agents"].([]any)
					for _, raw := range agents {
						a, _ := raw.(map[string]any)
						if a["id"] == request.AgentID {
							ref, _ := a["readback_ref"].(string)
							if err := plancheckpoint.Validate(root, loopruntime.Snapshot{State: state}, request.AgentID, ref); err != nil {
								return err
							}
						}
					}
				}
			}
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime entities must be an object")
			}
			agents, _ := entities["agents"].([]any)
			for _, raw := range agents {
				agent, _ := raw.(map[string]any)
				if agent["id"] != request.AgentID {
					continue
				}
				taskIDs, _ := agent["task_ids"].([]any)
				if message.TaskID != "" && !stringArrayContains(taskIDs, message.TaskID) {
					return fmt.Errorf("Agent message TASK is outside assignment")
				}
				projection, err := workstate.AgentProjection(agent, fmt.Sprint(state["schema_version"]))
				if err != nil {
					return fmt.Errorf("resolve Agent authorization activity: %w", err)
				}
				currentState := projection.Activity
				resolvedTo, found := agentTransitions.resolve(currentState, request.Event)
				if blockerCapability != nil && projection.Status == workstate.Active {
					resolvedTo, found = "blocked", true
				}
				if !found {
					return fmt.Errorf(
						"Agent event %s is not legal from state %s (canonical Agent states: spawned, reading, understanding_submitted, understanding_approved, activated, working, reported, done, blocked, stopped)",
						request.Event, currentState)
				}
				if err := applyAgentEventParams(root, agent, fmt.Sprint(state["schema_version"]), request, message, commitRevision); err != nil {
					return err
				}
				if capability != nil {
					if err := capability.ApplyLifecycleCAS(ctx, root, state, data, request.AgentID, message.TaskID, commitRevision, generation); err != nil {
						return err
					}
					if request.Event == "work_blocked" && blockerCapability != nil && blockerCapability.ClearResolvedBlockerIndex() {
						// A Resume successor proves the immediately preceding resolution
						// and retires its Agent pointer index in this same original CAS.
						// The immutable history remains in evidence and the journal.
						agent["blocker_resolved_ref"] = nil
					}
				}
				if err := workstate.RecordAgentActivity(state, agent, resolvedTo, request.Event); err != nil {
					return fmt.Errorf("record Agent work state and lifecycle event: %w", err)
				}
				if messageRef != "" {
					agent[agentEventEvidenceKey(request.Event)] = messageRef
					if request.Event == "readback_submitted" && agent["dispatch_mode"] == "plan_checkpoint" {
						// Commit the observation and authoritative readback in the same Writer mutation.
						agent["plan_reported_ref"] = messageRef
					}
				}
				agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				config, configErr := loopruntime.DispatchConfig(state)
				if configErr != nil {
					return configErr
				}
				life, _ := state["lifecycle"].(map[string]any)
				if config != nil && life["state"] == "building" && request.Event == "completion_acknowledged" {
					bindings := map[string]any{}
					tasks, _ := entities["tasks"].([]any)
					for _, rawTask := range tasks {
						task, _ := rawTask.(map[string]any)
						owners, _ := task["owner_agent_ids"].([]any)
						if !stringArrayContains(owners, request.AgentID) {
							continue
						}
						ref, _ := task["completion_report_ref"].(string)
						entries, _ := state["evidence"].([]any)
						for _, rawEvidence := range entries {
							ev, _ := rawEvidence.(map[string]any)
							producers, _ := ev["produced_by"].([]any)
							baseline, _ := state["baseline"].(map[string]any)
							if ref != "" && ev["path"] == ref && ev["kind"] == "completion_report" && ev["status"] == "valid" && ev["invalidated_by"] == nil && stringArrayContains(producers, request.AgentID) && dispatch.Number(ev["baseline_generation"]) == dispatch.Number(baseline["generation"]) {
								bindings[ref] = ev["sha256"]
							}
						}
					}
					agent["completion_acknowledged_reports"] = bindings
				}
				state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				return dispatch.ReconcilePressure(root, state)
			}
			return fmt.Errorf("Agent %s is not registered", request.AgentID)
		},
	}
	return updateRuntime(store, request.ExpectedRevision, mutation)
}

// applyAgentEventParams applies event-specific param checks. These mirror
// the prior implementation for the original 3 events and extend to all 12.
func applyAgentEventParams(root string, agent map[string]any, schemaVersion string, request AgentEventRequest, message agentMessage, commitRevision int) error {
	projection, err := workstate.AgentProjection(agent, schemaVersion)
	if err != nil {
		return fmt.Errorf("resolve Agent authorization activity: %w", err)
	}
	currentState := projection.Activity
	switch request.Event {
	case "readback_submitted":
		if currentState != "reading" {
			return fmt.Errorf("readback submission requires reading state")
		}
		// The plan_checkpoint flow submits a plan_report message type; the
		// approval flow submits the classic readback_response. The message
		// type is validated against the agent's dispatch mode here, where
		// the agent row is in hand (expectedAgentMessageType accepts both).
		// Agents registered before the dispatch_mode field (legacy states)
		// keep the approval semantics — register-workgroup stamps every new
		// agent with an explicit mode.
		mode, _ := agent["dispatch_mode"].(string)
		switch mode {
		case "plan_checkpoint":
			if message.MessageType != "plan_report" {
				return fmt.Errorf("dispatch_mode %s requires a plan_report message (PLAN_REPORT then continue); readback_response belongs to plan_approval_required", mode)
			}
		case "plan_approval_required", "":
			if message.MessageType != "readback_response" {
				return fmt.Errorf("dispatch_mode %q requires a readback_response message", mode)
			}
		default:
			return fmt.Errorf("unknown dispatch_mode %q (canonical: plan_checkpoint, plan_approval_required, one_shot)", mode)
		}
	case "understanding_approved":
		if currentState != "understanding_submitted" {
			return fmt.Errorf("approval requires submitted understanding")
		}
	case "activation_sent":
		mode, _ := agent["dispatch_mode"].(string)
		// L4 §3.3: plan_checkpoint agents activate straight off the plan
		// report (continuous execution); approval-mode (and legacy) agents
		// must wait for understanding_approved.
		legalFrom := map[string]bool{"understanding_approved": true}
		if mode == "plan_checkpoint" {
			legalFrom["understanding_submitted"] = true
		}
		if !legalFrom[currentState] {
			return fmt.Errorf("activation from %s requires dispatch_mode plan_checkpoint with a submitted plan report (or understanding_approved first)", currentState)
		}
		if request.ExpectedRevision >= 0 && message.ExpectedRuntimeRevision != request.ExpectedRevision {
			return fmt.Errorf("activation expected revision is stale")
		}
		if err := verifyActivationReadbackChain(root, agent, message); err != nil {
			return err
		}
		agent["activation_revision"] = commitRevision + 1
	case "understanding_rejected":
		if currentState != "understanding_submitted" {
			return fmt.Errorf("rejection requires submitted understanding")
		}
	case "document_conflict_reported":
		if currentState != "understanding_submitted" {
			return fmt.Errorf("document_conflict_reported requires submitted understanding")
		}
	case "work_started":
		if currentState != "activated" {
			return fmt.Errorf("work_started requires activated state")
		}
	case "completion_reported":
		if currentState != "working" {
			return fmt.Errorf("completion_reported requires working state")
		}
	case "completion_acknowledged":
		if currentState != "reported" {
			return fmt.Errorf("completion_acknowledged requires reported state")
		}
	case "work_blocked":
		if request.IssuedRepairResultBlocker != nil {
			if projection.Status != workstate.Active {
				return fmt.Errorf("S9 professional blocker requires an active responsibility")
			}
		} else if currentState != "working" {
			return fmt.Errorf("work_blocked requires working state")
		}
	case "blocker_resolved":
		if currentState != "blocked" {
			return fmt.Errorf("blocker_resolved requires blocked state")
		}
	case "shutdown_approved":
		if currentState != "blocked" {
			return fmt.Errorf("shutdown_approved requires blocked state")
		}
	}
	return nil
}

// verifyActivationReadbackChain fail-closes the activation envelope's
// hash-chain fields against the registered readback file: the envelope's
// approved_readback_sha256 must equal the byte hash of the file recorded at
// readback_submitted, and approved_readback_message_id must equal that
// file's message_id. The fields were schema-required before but never
// checked — ceremony; this makes them a guarantee.
func verifyActivationReadbackChain(root string, agent map[string]any, message agentMessage) error {
	return agentauth.VerifyActivationReadbackChain(root, agent, message)
}

// agentEventEvidenceKey returns the canonical evidence field name on the
// Agent entity for the given event. Most events reuse readback_ref /
// activation_ref; the rest use the message_ref suffix matching the event.
func agentEventEvidenceKey(event string) string {
	switch event {
	case "readback_started", "readback_submitted",
		"understanding_approved", "understanding_rejected",
		"document_conflict_reported":
		return "readback_ref"
	case "activation_sent":
		return "activation_ref"
	}
	return event + "_ref"
}

// agentEntityTransitions indexes the agent lifecycle transitions from the
// loaded catalog by (from, event) -> to. Built once per AdvanceAgent call.
type agentEntityTransitionsMap map[string]string // "from|event" -> to

type agentEntityTransitionsResolver struct {
	m agentEntityTransitionsMap
}

func (r agentEntityTransitionsResolver) resolve(from, event string) (string, bool) {
	if r.m == nil {
		return "", false
	}
	to, ok := r.m[from+"|"+event]
	return to, ok
}

// agentEntityTransitions extracts the agent entity lifecycle transitions
// from the catalog. The catalog's EntityTransitions is keyed by
// "agent:<transition-id>"; we rebuild a (from, event) -> to index here.
func agentEntityTransitions(catalog *transition.Catalog) agentEntityTransitionsResolver {
	res := agentEntityTransitionsMap{}
	if catalog == nil || catalog.Definition == nil {
		return agentEntityTransitionsResolver{m: res}
	}
	life, ok := catalog.Definition.EntityLifecycles["agent"]
	if !ok {
		return agentEntityTransitionsResolver{m: res}
	}
	for _, t := range life.Transitions {
		res[t.From+"|"+t.Event] = t.To
	}
	return agentEntityTransitionsResolver{m: res}
}

// ResolveAgentTransition exposes the catalog-driven (from, event) -> to
// resolution to sibling packages (the S7 review submit advances reviewer
// agents with the same completion_reported event).
func ResolveAgentTransition(catalog *transition.Catalog, from, event string) (string, bool) {
	return agentEntityTransitions(catalog).resolve(from, event)
}

// canonicalAgentEventList returns the canonical 12 Agent events, ordered.
// This is used by tests that need to enumerate the surface without parsing
// loop-definition.json directly.
func canonicalAgentEventList() []string {
	return []string{
		"readback_started",
		"readback_submitted",
		"understanding_approved",
		"understanding_rejected",
		"document_conflict_reported",
		"activation_sent",
		"work_started",
		"completion_reported",
		"completion_acknowledged",
		"work_blocked",
		"blocker_resolved",
		"shutdown_approved",
	}
}

// stringArrayContains is a defensive helper used to check if a []any
// contains a target string. It is intentionally permissive about element
// types so it can be used both for schema-decoded maps (where the elements
// are string) and for directly-constructed slices in tests.
func stringArrayContains(values []any, target string) bool {
	for _, value := range values {
		if s, _ := value.(string); s == target {
			return true
		}
	}
	return false
}
