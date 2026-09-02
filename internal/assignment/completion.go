package assignment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// CompletionRequest drives the canonical S6 Builder Result registration.
type CompletionRequest struct {
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

// CompleteTask is the canonical S6 completion path: ONE command produces
// the Builder Result fact once. It validates the completion message,
// derives the completion_report evidence envelope from it (identity,
// fingerprints and subject refs are tool-generated — the Builder does not
// hand-write a second artifact), and atomically advances the Agent
// (working→reported), applies the TASK builder_reported side effect, and
// appends the evidence index entry under a single revision CAS
// (L3-S6 §7.3 / P1). The legacy dual write — agent-event + a separate
// `runtime evidence add` — is no longer required on this path.
func CompleteTask(
	root, statePath, journalPath string,
	request CompletionRequest,
) (loopruntime.Snapshot, error) {
	if request.AgentID == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("agent_id is required")
	}
	data, err := os.ReadFile(request.MessagePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read completion message: %w", err)
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
	runtimeID, _ := current["runtime_id"].(string)
	if message.RuntimeID != "" && message.RuntimeID != runtimeID {
		return loopruntime.Snapshot{}, fmt.Errorf("completion message runtime does not match current runtime")
	}
	commitRevision, err := commitRevision(request.ExpectedRevision, current)
	if err != nil {
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
	// The persisted bytes (trailing newline included) are what the
	// evidence index fingerprints — hash exactly what lands on disk.
	envelopeBytes := append(envelopeData, '\n')
	envelopeAbs := filepath.Join(root, filepath.FromSlash(envelopeRel))
	if err := os.MkdirAll(filepath.Dir(envelopeAbs), 0o755); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("create completion evidence dir: %w", err)
	}
	if err := os.WriteFile(envelopeAbs, envelopeBytes, 0o644); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("write completion envelope: %w", err)
	}
	envelopeSHA := sha256Of(envelopeBytes)

	messageRef := repositoryPath(root, request.MessagePath)
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

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	mutation := loopruntime.Mutation{
		EventID:        fmt.Sprintf("evt-complete-%s-%s-r%d", request.AgentID, message.TaskID, commitRevision+1),
		TransitionID:   "BUILDER-RESULT",
		Event:          "completion_reported",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:complete:%s:%s:%d", request.AgentID, message.TaskID, commitRevision),
		RuntimeID:      runtimeID,
		From:           cursor,
		To:             cursor,
		EvidenceIDs:    []string{evidenceID},
		Message: fmt.Sprintf("Registered the canonical Builder Result for %s (completion evidence %s)",
			message.TaskID, evidenceID),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			if err := applyCompletionAgent(state, request, message, agentTransitions, messageRef, occurredAt); err != nil {
				return err
			}
			if err := applyCompletionTask(state, message, taskTransitions, envelopeRel, occurredAt); err != nil {
				return err
			}
			return appendCompletionEvidence(state, evidenceID, envelopeRel, envelopeSHA, request.AgentID, generation, message.ChangedPaths, occurredAt)
		},
	}
	return updateRuntime(store, request.ExpectedRevision, mutation)
}

// applyCompletionAgent advances the agent row exactly like the
// completion_reported event path (working → reported) and records the
// message ref.
func applyCompletionAgent(
	state map[string]any,
	request CompletionRequest,
	message completionMessage,
	agentTransitions agentEntityTransitionsResolver,
	messageRef string,
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
		currentState, _ := agent["state"].(string)
		if currentState == "reported" || currentState == "done" {
			// Idempotent resubmission: the agent already reported.
			return nil
		}
		resolvedTo, found := agentTransitions.resolve(currentState, "completion_reported")
		if !found {
			return fmt.Errorf(
				"completion requires working state (canonical Agent states: spawned, reading, understanding_submitted, understanding_approved, activated, working, reported, done, blocked, stopped), agent is %s",
				currentState)
		}
		agent["state"] = resolvedTo
		if messageRef != "" {
			agent["completion_reported_ref"] = messageRef
		}
		agent["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
		return nil
	}
	return fmt.Errorf("Agent %s is not registered", request.AgentID)
}

// applyCompletionTask applies the builder_reported TASK side effect with
// the canonical envelope as the completion_report_ref. A task row already
// past the reporting point (review/done) is left untouched — the agent
// idempotency above covers resubmission; a row in an illegal state fails
// closed instead of drifting.
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
		currentState, _ := task["state"].(string)
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
		task["state"] = resolvedTo
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
	evidenceID, envelopeRel, envelopeSHA, agentID string,
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
		"sha256":              envelopeSHA,
		"status":              "valid",
		"baseline_generation": generation,
		"review_round":        nil,
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
// document (path/version/sha256) when the batch registration carries one.
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
			"sha256":  document["sha256"],
		}}
	}
	return []any{}
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
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
