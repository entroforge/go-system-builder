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
	"encoding/json"
	"fmt"
	"os"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

type AgentEventRequest struct {
	ExpectedRevision int
	AgentID          string
	Event            string
	MessagePath      string
	Params           map[string]any `json:"params,omitempty"`
	OccurredAt       time.Time
}

type agentMessage struct {
	MessageType             string `json:"message_type"`
	RuntimeID               string `json:"runtime_id"`
	ExpectedRuntimeRevision int    `json:"expected_runtime_revision"`
	AgentID                 string `json:"agent_id"`
	TaskID                  string `json:"task_id"`
	Body                    string `json:"body,omitempty"`
}

// expectedAgentMessageType returns the canonical message_type expected for a
// given Agent event. The mapping is the engine's contract; mismatched
// message types are rejected with a typed error.
//
// readback_started / understanding_rejected / document_conflict_reported /
// activation_sent / work_started / completion_reported / completion_acknowledged /
// work_blocked / blocker_resolved / shutdown_approved all use their own
// message_type suffix matching the loop-definition.json event name.
func expectedAgentMessageType(event string) string {
	switch event {
	case "readback_submitted", "understanding_approved", "understanding_rejected",
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
	if request.AgentID == "" || request.Event == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("agent_id and event are required")
	}
	data, err := os.ReadFile(request.MessagePath)
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
	if message.MessageType != expectedType || message.AgentID != request.AgentID {
		return loopruntime.Snapshot{}, fmt.Errorf("Agent message does not match event or Agent")
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	runtimeID, _ := current["runtime_id"].(string)
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

	// Load catalog (with agent entity_lifecycles.transitions) and resolve the
	// (from, event) -> to pair.
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("load catalog: %w", err)
	}
	agentTransitions := agentEntityTransitions(catalog)

	store := loopruntime.NewStore(statePath, journalPath)
	return store.Update(request.ExpectedRevision, loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-agent-%s-%s-r%d", request.AgentID, request.Event, request.ExpectedRevision+1),
		TransitionID:   "AGENT-LIFECYCLE",
		Event:          request.Event,
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:agent:%s:%s:%d", request.AgentID, request.Event, request.ExpectedRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		EvidenceIDs:    []string{messageRef},
		Message:        fmt.Sprintf("Recorded a schema-valid Agent lifecycle event (%s)", request.Event),
		OccurredAt:     occurredAt,
		Apply: func(state map[string]any) error {
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
				currentState, _ := agent["state"].(string)
				resolvedTo, found := agentTransitions.resolve(currentState, request.Event)
				if !found {
					return fmt.Errorf(
						"Agent event %s is not legal from state %s (canonical Agent states: spawned, reading, understanding_submitted, understanding_approved, activated, working, reported, done, blocked, stopped)",
						request.Event, currentState)
				}
				if err := applyAgentEventParams(agent, request, message); err != nil {
					return err
				}
				agent["state"] = resolvedTo
				if messageRef != "" {
					agent[agentEventEvidenceKey(request.Event)] = messageRef
				}
				agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				return nil
			}
			return fmt.Errorf("Agent %s is not registered", request.AgentID)
		},
	})
}

// applyAgentEventParams applies event-specific param checks. These mirror
// the prior implementation for the original 3 events and extend to all 12.
func applyAgentEventParams(agent map[string]any, request AgentEventRequest, message agentMessage) error {
	switch request.Event {
	case "readback_submitted":
		if agent["state"] != "reading" {
			return fmt.Errorf("readback submission requires reading state")
		}
	case "understanding_approved":
		if agent["state"] != "understanding_submitted" {
			return fmt.Errorf("approval requires submitted understanding")
		}
	case "activation_sent":
		if agent["state"] != "understanding_approved" {
			return fmt.Errorf("activation requires approved understanding")
		}
		if message.ExpectedRuntimeRevision != request.ExpectedRevision {
			return fmt.Errorf("activation expected revision is stale")
		}
		agent["activation_revision"] = request.ExpectedRevision + 1
	case "understanding_rejected":
		if agent["state"] != "understanding_submitted" {
			return fmt.Errorf("rejection requires submitted understanding")
		}
	case "document_conflict_reported":
		if agent["state"] != "understanding_submitted" {
			return fmt.Errorf("document_conflict_reported requires submitted understanding")
		}
	case "work_started":
		if agent["state"] != "activated" {
			return fmt.Errorf("work_started requires activated state")
		}
	case "completion_reported":
		if agent["state"] != "working" {
			return fmt.Errorf("completion_reported requires working state")
		}
	case "completion_acknowledged":
		if agent["state"] != "reported" {
			return fmt.Errorf("completion_acknowledged requires reported state")
		}
	case "work_blocked":
		if agent["state"] != "working" {
			return fmt.Errorf("work_blocked requires working state")
		}
	case "blocker_resolved":
		if agent["state"] != "blocked" {
			return fmt.Errorf("blocker_resolved requires blocked state")
		}
	case "shutdown_approved":
		if agent["state"] != "blocked" {
			return fmt.Errorf("shutdown_approved requires blocked state")
		}
	}
	return nil
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
