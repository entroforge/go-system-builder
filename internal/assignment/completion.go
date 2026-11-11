package assignment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/dispatch"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// CompletionRequest drives the canonical S6 Builder Result registration.
type CompletionRequest struct {
	Context         context.Context `json:"-"`
	NativeAgentID   string
	NativeSessionID string
	// NativePlanPath is supplied by the native Stop receiver, never an activation.
	NativePlanPath   string
	ExpectedRevision int
	AgentID          string
	MessagePath      string
	OccurredAt       time.Time
}

// completionMessage decodes the rich completion-report payload the
// agent-message schema defines (status/summary/changed_paths/checks/...).
// The runtime previously discarded these fields at the AdvanceAgent
// boundary; the canonical result derives the gate-consumable envelope from
// them instead (L3-S6 §7.3 "唯一 Builder Result").
type completionMessage struct {
	MessageType             string             `json:"message_type"`
	RuntimeID               string             `json:"runtime_id"`
	ExpectedRuntimeRevision int                `json:"expected_runtime_revision"`
	AgentID                 string             `json:"agent_id"`
	TaskID                  string             `json:"task_id"`
	Status                  string             `json:"status"`
	Summary                 string             `json:"summary"`
	ChangedPaths            []string           `json:"changed_paths"`
	ReviewedPaths           []string           `json:"reviewed_paths"`
	Checks                  []envelopeCheckOut `json:"checks"`
	ScopeDeviations         []string           `json:"scope_deviations"`
	RequestedEvent          string             `json:"requested_event"`
}

type envelopeCheckOut struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Result  string `json:"result"`
}

// CompleteTask is the compatibility entry point for the original result
// command. It deliberately preserves the old Snapshot return shape while the
// canonical S6 consumer can also expose its immutable receipt.
func CompleteTask(
	root, statePath, journalPath string,
	request CompletionRequest,
) (loopruntime.Snapshot, error) {
	snapshot, _, err := CompleteTaskWithReceipt(root, statePath, journalPath, request)
	return snapshot, err
}

// CompleteTaskWithReceipt dispatches only an explicitly Record-bound S6
// message to the canonical Result consumer. The legacy branch remains in its
// original helper and keeps its existing evidence and lifecycle semantics.
func CompleteTaskWithReceipt(
	root, statePath, journalPath string,
	request CompletionRequest,
) (loopruntime.Snapshot, ResultAcceptanceReceipt, error) {
	if request.AgentID == "" {
		return loopruntime.Snapshot{}, ResultAcceptanceReceipt{}, fmt.Errorf("agent_id is required")
	}
	data, err := os.ReadFile(request.MessagePath)
	if err != nil {
		return loopruntime.Snapshot{}, ResultAcceptanceReceipt{}, fmt.Errorf("read completion message: %w", err)
	}
	if hasCanonicalResultMarker(data) {
		if len(data) > canonicalResultMessageLimit {
			return loopruntime.Snapshot{}, ResultAcceptanceReceipt{}, fmt.Errorf("completion message exceeds the 1 MiB limit")
		}
		// G18 §3.2: the canonical S6 consumer does not assert a Runtime write
		// count. The caller's expected revision is dropped here (-1 is the
		// omission marker), so a legitimate professional completion is not
		// refused because the Runtime moved for an unrelated reason. The
		// received row, the journal-level dedup and the Assignment Record's own
		// revision remain the protection of this acceptance.
		request.ExpectedRevision = -1
		return completeCanonicalTaskWithReceipt(root, statePath, journalPath, request, data)
	}
	snapshot, err := completeTaskLegacy(root, statePath, journalPath, request, data)
	return snapshot, ResultAcceptanceReceipt{}, err
}

// completeTaskLegacy retains the original completion_report path for
// Assignments that did not explicitly opt into the S6 v1.2 Record contract.
// CompleteTaskWithReceipt rejects canonical opt-in owners before this legacy
// path can accept a worker-authored legacy conclusion.
//
// completeTaskLegacy preserves the pre-S6 completion_report flow for an
// Assignment that has no canonical Record opt-in. It validates the original
// completion message and atomically updates the Agent, TASK and evidence
// index through the same Runtime Writer.
func completeTaskLegacy(
	root, statePath, journalPath string,
	request CompletionRequest,
	data []byte,
) (loopruntime.Snapshot, error) {
	if request.AgentID == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("agent_id is required")
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("completion message schema: %w", err)
	}
	var message completionMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode completion message: %w", err)
	}
	if message.MessageType != "completion_report" {
		return loopruntime.Snapshot{}, fmt.Errorf("message_type %q is not a completion_report", message.MessageType)
	}
	if message.AgentID != request.AgentID {
		return loopruntime.Snapshot{}, fmt.Errorf("completion message does not match Agent")
	}
	if message.Status != "completed" {
		return loopruntime.Snapshot{}, fmt.Errorf(
			"task-complete registers completed results only; status %q belongs on the work_blocked path", message.Status)
	}
	if message.TaskID == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("completion message carries no task_id")
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	if err := rejectLegacyCompletionForS6Owner(root, current, request.AgentID); err != nil {
		return loopruntime.Snapshot{}, err
	}
	runtimeID, _ := current["runtime_id"].(string)
	if message.RuntimeID != "" && message.RuntimeID != runtimeID {
		return loopruntime.Snapshot{}, fmt.Errorf("completion message runtime does not match current runtime")
	}
	if _, err := commitRevision(request.ExpectedRevision, current); err != nil {
		return loopruntime.Snapshot{}, err
	}
	baseline, _ := current["baseline"].(map[string]any)
	generation := 0
	if baseline != nil {
		if value, ok := baseline["generation"].(float64); ok {
			generation = int(value)
		}
	}

	// Derive the canonical envelope. Subject refs bind the result to the
	// registered TASK document so the batch gate's fingerprint matching
	// holds without the Builder hand-copying coordinates. A resubmission
	// (fix → re-run) escalates the evidence id -r2, -r3, … instead of
	// colliding: the newer envelope appends later in the evidence array
	// and the gate's per-task selection takes the last one, so a corrected
	// result supersedes the failed one while history is preserved.
	evidenceID := completionEvidenceID(current, message.TaskID, generation)
	envelopeRel := filepath.ToSlash(filepath.Join(
		".claude", "evidence", runtimeID, fmt.Sprintf("g%d", generation),
		"assignments", request.AgentID, evidenceID+".json"))
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             evidenceID,
		"kind":                    "completion_report",
		"runtime_id":              runtimeID,
		"baseline_generation":     generation,
		"producer_agent_id":       request.AgentID,
		"producer_responsibility": "BUILD-WORK-PACKAGE",
		"subject_refs":            taskSubjectRefs(current, generation, message.TaskID),
		"conclusion":              "completed",
		"task_id":                 message.TaskID,
		"summary":                 message.Summary,
		"changed_paths":           message.ChangedPaths,
		"reviewed_paths":          message.ReviewedPaths,
		"checks":                  message.Checks,
		"scope_deviations":        message.ScopeDeviations,
		"created_at":              time.Now().UTC().Format(time.RFC3339Nano),
	}
	envelopeData, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("encode completion envelope: %w", err)
	}
	// The original report is retained at its actual evidence path; registering
	// the responsibility does not require authenticating its bytes.
	envelopeBytes := append(envelopeData, '\n')
	envelopeAbs := filepath.Join(root, filepath.FromSlash(envelopeRel))
	if err := os.MkdirAll(filepath.Dir(envelopeAbs), 0o755); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("create completion evidence dir: %w", err)
	}
	if err := os.WriteFile(envelopeAbs, envelopeBytes, 0o644); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("write completion envelope: %w", err)
	}

	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("load catalog: %w", err)
	}
	agentTransitions := agentEntityTransitions(catalog)
	taskTransitions := taskEntityTransitions(catalog)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(request.Context)
	mutation := loopruntime.Mutation{
		// The identity of a Builder Result is the reporting Agent and the
		// domain delivery it registers. The delivery id is the domain's own
		// "which submission" number derived from the accepted delivery history
		// (G18 §2); the Runtime write counter is an audit coordinate only.
		EventID:            fmt.Sprintf("evt-complete-%s-%s", request.AgentID, evidenceID),
		TransitionID:       "BUILDER-RESULT",
		Event:              "completion_reported",
		Actor:              "orchestrator",
		IdempotencyKey:     fmt.Sprintf("runtime:complete:%s:%s", request.AgentID, evidenceID),
		RuntimeID:          runtimeID,
		BaselineGeneration: generation,
		From:               cursor,
		To:                 cursor,
		EvidenceIDs:        []string{evidenceID},
		Message: fmt.Sprintf("Registered the canonical Builder Result for %s (completion evidence %s)",
			message.TaskID, evidenceID),
		OccurredAt: occurredAt,
		// G18 §1/§3.2: the identity of a Builder Result names the reporting
		// Agent and the delivery it registers, so the same delivery presented
		// twice produces the same event id. The journal's global event-id
		// uniqueness check runs before the Apply closure, so without this
		// judgment the caller would receive a storage message instead of the
		// domain's own outcome. The store calls the Precheck under its lock,
		// after the revision CAS, so a repeated delivery keeps the domain
		// outcome and the refused delivery writes neither Runtime state nor a
		// journal fact.
		Precheck: func(state map[string]any) error {
			return completionDeliveryRefusal(state, request, message, agentTransitions, taskTransitions, evidenceID)
		},
		Apply: func(state map[string]any) error {
			if err := applyCompletionAgent(state, request, message, agentTransitions, envelopeRel, occurredAt); err != nil {
				return err
			}
			if err := applyCompletionTask(state, message, taskTransitions, envelopeRel, occurredAt); err != nil {
				return err
			}
			if err := appendCompletionEvidence(state, evidenceID, envelopeRel, request.AgentID, generation, message.ChangedPaths, occurredAt); err != nil {
				return err
			}
			return dispatch.ReconcilePressure(root, state)
		},
	}
	return updateRuntime(store, request.ExpectedRevision, mutation)
}

// completionDeliveryRefusal is the read-only mirror of the Apply closure's
// completion judgments. The Apply closure keeps the authority under
// concurrency; this mirror is what the mutation's Precheck runs, so a repeated
// delivery keeps the domain's own outcome instead of the journal's global
// event-id collision.
//
// The judgments run in the Apply closure's order, so the first refusal is the
// same one the Apply closure produces. The mirror writes nothing: it reads
// the Runtime state and leaves both the Runtime and the journal untouched.
func completionDeliveryRefusal(
	state map[string]any,
	request CompletionRequest,
	message completionMessage,
	agentTransitions agentEntityTransitionsResolver,
	taskTransitions taskEntityTransitionsResolver,
	evidenceID string,
) error {
	if err := completionAgentRefusal(state, request, message, agentTransitions); err != nil {
		return err
	}
	if err := completionTaskRefusal(state, message, taskTransitions); err != nil {
		return err
	}
	return completionEvidenceRefusal(state, evidenceID)
}

// completionAgentRefusal mirrors the Agent judgment the Apply closure runs
// first: the reporting Agent must exist, must own the TASK and must be able
// to record the Builder Result. A repeated Result on an Agent that already
// reported stays acceptable — the Agent keeps reporting and the latest Result
// ref replaces the previous one — while a completed Assignment refuses every
// further Result until an explicit revision names the next attempt.
func completionAgentRefusal(
	state map[string]any,
	request CompletionRequest,
	message completionMessage,
	agentTransitions agentEntityTransitionsResolver,
) error {
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
		if !stringArrayContains(taskIDs, message.TaskID) {
			return fmt.Errorf("completion message TASK is outside assignment")
		}
		version := fmt.Sprint(state["schema_version"])
		projection, err := workstate.AgentProjection(agent, version)
		if err != nil {
			return fmt.Errorf("resolve completion Agent activity: %w", err)
		}
		currentState := projection.Activity
		if currentState == "reported" || currentState == "done" {
			if workstate.Unified(version) && currentState == "done" {
				return fmt.Errorf("completed Assignment requires an explicit revision before accepting another Result")
			}
			return nil
		}
		if _, found := agentTransitions.resolve(currentState, "completion_reported"); !found {
			return fmt.Errorf(
				"completion requires working state (canonical Agent states: spawned, reading, understanding_submitted, understanding_approved, activated, working, reported, done, blocked, stopped), agent is %s",
				currentState)
		}
		return nil
	}
	return fmt.Errorf("Agent %s is not registered", request.AgentID)
}

// completionTaskRefusal mirrors the TASK judgment the Apply closure runs
// after the Agent judgment. A TASK that already reached review or done keeps
// its lifecycle state and only advances its canonical Result reference.
func completionTaskRefusal(
	state map[string]any,
	message completionMessage,
	taskTransitions taskEntityTransitionsResolver,
) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime entities must be an object")
	}
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task["id"] != message.TaskID {
			continue
		}
		version := fmt.Sprint(state["schema_version"])
		projection, err := workstate.TaskProjection(task, version)
		if err != nil {
			return fmt.Errorf("resolve completion TASK activity: %w", err)
		}
		currentState := projection.Activity
		switch currentState {
		case "review", "done":
			return nil
		}
		if _, found := taskTransitions.resolve(currentState, "builder_reported"); !found {
			return fmt.Errorf(
				"TASK %s cannot accept a Builder Result from state %s (canonical Task states: candidate, reviewed, locked, in_progress, review, blocked, done, cancelled)",
				message.TaskID, currentState)
		}
		return nil
	}
	// No task row: agent-centric completion still stands; the batch gate
	// evaluates the registered document batch, not entity rows.
	return nil
}

// completionEvidenceRefusal mirrors the evidence judgment the Apply closure
// runs last: one delivery identity is registered once. A delivery that names
// an identity the Runtime already holds is a conflict rather than a new
// submission, and the domain refuses it with its own message.
//
// A re-delivery of the same completion is not that conflict: the evidence id
// escalates per accepted submission, so a re-delivery names a new delivery
// identity and stays acceptable.
func completionEvidenceRefusal(state map[string]any, evidenceID string) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item != nil && item["id"] == evidenceID {
			return fmt.Errorf("evidence %s is already registered", evidenceID)
		}
	}
	return nil
}

// applyCompletionAgent advances the agent row exactly like the
// completion_reported event path (working → reported) and records the
// canonical Result ref, including resubmissions.
func applyCompletionAgent(
	state map[string]any,
	request CompletionRequest,
	message completionMessage,
	agentTransitions agentEntityTransitionsResolver,
	resultRef string,
	occurredAt time.Time,
) error {
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
		if !stringArrayContains(taskIDs, message.TaskID) {
			return fmt.Errorf("completion message TASK is outside assignment")
		}
		version := fmt.Sprint(state["schema_version"])
		projection, err := workstate.AgentProjection(agent, version)
		if err != nil {
			return fmt.Errorf("resolve completion Agent activity: %w", err)
		}
		currentState := projection.Activity
		if currentState == "reported" || currentState == "done" {
			if workstate.Unified(version) && currentState == "done" {
				return fmt.Errorf("completed Assignment requires an explicit revision before accepting another Result")
			}
			config, configErr := loopruntime.DispatchConfig(state)
			if configErr != nil {
				return configErr
			}
			life, _ := state["lifecycle"].(map[string]any)
			if config != nil && life["state"] == "building" {
				if err := workstate.RecordAgentActivity(state, agent, "reported", "completion_reported"); err != nil {
					return fmt.Errorf("record repeated completion Agent activity: %w", err)
				}
				delete(agent, "completion_acknowledged_ref")
				delete(agent, "completion_acknowledged_reports")
			}
			// Keep the latest canonical Result aligned with the TASK.
			agent["completion_reported_ref"] = resultRef
			agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		}
		resolvedTo, found := agentTransitions.resolve(currentState, "completion_reported")
		if !found {
			return fmt.Errorf(
				"completion requires working state (canonical Agent states: spawned, reading, understanding_submitted, understanding_approved, activated, working, reported, done, blocked, stopped), agent is %s",
				currentState)
		}
		if err := workstate.RecordAgentActivity(state, agent, resolvedTo, "completion_reported"); err != nil {
			return fmt.Errorf("record completion Agent work state: %w", err)
		}
		config, configErr := loopruntime.DispatchConfig(state)
		if configErr != nil {
			return configErr
		}
		life, _ := state["lifecycle"].(map[string]any)
		if config != nil && life["state"] == "building" {
			delete(agent, "completion_acknowledged_ref")
			delete(agent, "completion_acknowledged_reports")
		}
		if resultRef != "" {
			agent["completion_reported_ref"] = resultRef
		}
		agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
		return nil
	}
	return fmt.Errorf("Agent %s is not registered", request.AgentID)
}

// applyCompletionTask applies the builder_reported TASK side effect with
// the canonical envelope as the completion_report_ref. A task row already
// past the reporting point (review/done) retains its lifecycle state but
// advances its canonical Result reference. Illegal states fail closed.
func applyCompletionTask(
	state map[string]any,
	message completionMessage,
	taskTransitions taskEntityTransitionsResolver,
	envelopeRel string,
	occurredAt time.Time,
) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime entities must be an object")
	}
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task["id"] != message.TaskID {
			continue
		}
		version := fmt.Sprint(state["schema_version"])
		projection, err := workstate.TaskProjection(task, version)
		if err != nil {
			return fmt.Errorf("resolve completion TASK activity: %w", err)
		}
		currentState := projection.Activity
		switch currentState {
		case "review", "done":
			task["completion_report_ref"] = envelopeRel
			return nil
		}
		resolvedTo, found := taskTransitions.resolve(currentState, "builder_reported")
		if !found {
			return fmt.Errorf(
				"TASK %s cannot accept a Builder Result from state %s (canonical Task states: candidate, reviewed, locked, in_progress, review, blocked, done, cancelled)",
				message.TaskID, currentState)
		}
		if err := workstate.RecordTaskActivity(state, task, resolvedTo, "builder_reported"); err != nil {
			return fmt.Errorf("record completion TASK work state: %w", err)
		}
		task["completion_report_ref"] = envelopeRel
		task["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
		return nil
	}
	// No task row: agent-centric completion still stands; the batch gate
	// evaluates the registered document batch, not entity rows.
	return nil
}

// appendCompletionEvidence registers the derived envelope in the evidence
// index with the same field shape RecordEvidence produces.
func appendCompletionEvidence(
	state map[string]any,
	evidenceID, envelopeRel, agentID string,
	generation int,
	changedPaths []string,
	occurredAt time.Time,
) error {
	items, ok := state["evidence"].([]any)
	if !ok {
		return fmt.Errorf("runtime evidence must be an array")
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item != nil && item["id"] == evidenceID {
			return fmt.Errorf("evidence %s is already registered", evidenceID)
		}
	}
	scopeRefs := make([]any, 0, len(changedPaths))
	seenPaths := make(map[string]bool, len(changedPaths))
	for _, path := range changedPaths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" || seenPaths[path] {
			continue
		}
		seenPaths[path] = true
		scopeRefs = append(scopeRefs, path)
	}
	items = append(items, map[string]any{
		"id":                  evidenceID,
		"kind":                "completion_report",
		"path":                envelopeRel,
		"status":              "valid",
		"baseline_generation": generation,
		"produced_by":         []any{agentID},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   "BUILD-WORK-PACKAGE",
		"scope_refs":          scopeRefs,
	})
	state["evidence"] = items
	state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return nil
}

// taskSubjectRefs binds the completion envelope to the registered TASK
// document (path/version) when the batch registration carries one.
func taskSubjectRefs(state map[string]any, generation int, taskID string) []any {
	documents, _ := state["documents"].([]any)
	for _, raw := range documents {
		document, _ := raw.(map[string]any)
		if document == nil || document["id"] != taskID || document["kind"] != "task" {
			continue
		}
		if value, ok := document["generation"].(float64); !ok || int(value) != generation {
			continue
		}
		return []any{map[string]any{
			"path":    document["path"],
			"version": document["version"],
		}}
	}
	return []any{}
}

// completionEvidenceID returns the canonical id for a TASK's Builder
// Result at one generation, escalating -r2/-r3… when earlier submissions
// already registered the base id (fix-and-resubmit path).
func completionEvidenceID(state map[string]any, taskID string, generation int) string {
	base := fmt.Sprintf("ev-completion-%s-g%d", taskID, generation)
	items, _ := state["evidence"].([]any)
	attempt := 1
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		id, _ := item["id"].(string)
		if id == base || strings.HasPrefix(id, base+"-r") {
			attempt++
		}
	}
	if attempt == 1 {
		return base
	}
	return fmt.Sprintf("%s-r%d", base, attempt)
}
