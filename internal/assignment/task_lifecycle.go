// task_lifecycle.go — TASK-015 TASK entity lifecycle executor.
//
// Per BUG-003 §4b.1 Failure 3 + §4b.2(d), TASK entities had no executor in
// the runtime. TASK-015 ships AdvanceTask which resolves the (from, event)
// pair from loop-definition.json entity_lifecycles.task.transitions and
// applies the transition. The canonical 8 task events are (read from
// catalog, NOT hard-coded):
//
//	internal_review_passed, document_pass_lock, builder_activated,
//	builder_reported, task_closing_contract_passed, task_blocked,
//	task_resumed, task_cancelled.
//
// All 8 transitions are guarded per the catalog; AdvanceTask validates
// each guard before mutating state. Required params:
//   - builder_activated requires builder_activation_recorded evidence
//     (recorded via activation message).
//   - builder_reported requires a builder_report_complete param.
//   - task_closing_contract_passed requires
//     required_verification_evidence_present (a verification_evidence
//     param string).
//   - task_blocked requires a blocker_recorded param (blocker_evidence).
//   - task_resumed requires task_versions_current (caller-supplied).
//   - task_cancelled requires cancellation_reason_recorded param.
package assignment

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

type TaskEventRequest struct {
	ExpectedRevision int
	TaskID           string
	Event            string
	Params           map[string]any `json:"params,omitempty"`
	OccurredAt       time.Time
}

// AdvanceTask applies one TASK lifecycle event.
//
// The full 8-event surface (resolved from loop-definition.json
// entity_lifecycles.task.transitions) is supported. Each event resolves
// its (from, to) pair via the catalog; if no pair matches the task's
// current state, the event is rejected with a typed error.
func AdvanceTask(
	root, statePath, journalPath string,
	request TaskEventRequest,
) (loopruntime.Snapshot, error) {
	if request.TaskID == "" || request.Event == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("task_id and event are required")
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("load catalog: %w", err)
	}
	taskTransitions := taskEntityTransitions(catalog)

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	runtimeID, _ := current["runtime_id"].(string)
	commitRevision, err := commitRevision(request.ExpectedRevision, current)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	mutation := loopruntime.Mutation{
		Audit: loopruntime.AuditEnvelope{
			EventID:        fmt.Sprintf("evt-task-%s-%s-r%d", request.TaskID, request.Event, commitRevision+1),
			TransitionID:   "TASK-LIFECYCLE",
			Event:          request.Event,
			Actor:          "orchestrator",
			IdempotencyKey: fmt.Sprintf("runtime:task:%s:%s:%d", request.TaskID, request.Event, commitRevision),
			RuntimeID:      runtimeID,
			From:           cursor,
			To:             cursor,
			EvidenceIDs:    []string{},
		},
		Message:    fmt.Sprintf("Recorded a TASK lifecycle event (%s on %s)", request.Event, request.TaskID),
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime entities must be an object")
			}
			tasks, _ := entities["tasks"].([]any)
			for _, raw := range tasks {
				task, _ := raw.(map[string]any)
				if task["id"] != request.TaskID {
					continue
				}
				currentState, _ := task["state"].(string)
				resolvedTo, found := taskTransitions.resolve(currentState, request.Event)
				if !found {
					return fmt.Errorf(
						"Task event %s is not legal from state %s (canonical Task states: candidate, reviewed, locked, in_progress, review, blocked, done, cancelled)",
						request.Event, currentState)
				}
				if err := applyTaskEventGuards(task, request, resolvedTo); err != nil {
					return err
				}
				task["state"] = resolvedTo
				task["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
				return nil
			}
			return fmt.Errorf("Task %s is not registered", request.TaskID)
		},
	}
	return updateRuntime(store, request.ExpectedRevision, mutation)
}

// applyTaskEventGuards enforces the per-event guard requirements. The
// guards are checked against the supplied params and (where relevant) the
// task entity's existing fields. The catalog guards (task_manifest_complete
// etc.) are intentionally reified here so the runtime fails closed before
// reaching the engine.
func applyTaskEventGuards(task map[string]any, request TaskEventRequest, resolvedTo string) error {
	switch request.Event {
	case "internal_review_passed":
		// task_manifest_complete: the task must have a non-empty path and
		// sha256 (manifest = the locked task spec).
		if path, _ := task["path"].(string); path == "" {
			return fmt.Errorf("guard task_manifest_complete failed: task path is empty")
		}
		if sha, _ := task["sha256"].(string); sha == "" {
			return fmt.Errorf("guard task_manifest_complete failed: task sha256 is empty")
		}
	case "document_pass_lock":
		// joint_document_pass: caller supplies evidence that document_pass
		// was jointly verified. No additional runtime check beyond the
		// param presence; the engine handles cross-loop verification.
		ev, _ := request.Params["document_pass_ref"].(string)
		if ev == "" {
			return fmt.Errorf("guard joint_document_pass failed: document_pass_ref param required")
		}
		task["document_pass_ref"] = ev
	case "builder_activated":
		// builder_activation_recorded: caller supplies the activation ref.
		ev, _ := request.Params["activation_ref"].(string)
		if ev == "" {
			return fmt.Errorf("guard builder_activation_recorded failed: activation_ref param required")
		}
		task["activation_ref"] = ev
	case "builder_reported":
		// builder_report_complete: caller supplies the completion report ref.
		ev, _ := request.Params["completion_report_ref"].(string)
		if ev == "" {
			return fmt.Errorf("guard builder_report_complete failed: completion_report_ref param required")
		}
		task["completion_report_ref"] = ev
	case "task_closing_contract_passed":
		// required_verification_evidence_present: caller supplies the
		// verification evidence reference.
		ev, _ := request.Params["verification_evidence"].(string)
		if ev == "" {
			return fmt.Errorf("guard required_verification_evidence_present failed: verification_evidence param required")
		}
		task["verification_evidence"] = ev
	case "task_blocked":
		ev, _ := request.Params["blocker_evidence"].(string)
		if ev == "" {
			return fmt.Errorf("guard blocker_recorded failed: blocker_evidence param required")
		}
		task["blocker_evidence"] = ev
	case "task_resumed":
		// task_versions_current: caller-supplied; cannot be verified in
		// isolation. We require the caller to attest via a boolean param.
		if ok, _ := request.Params["versions_current"].(bool); !ok {
			return fmt.Errorf("guard task_versions_current failed: versions_current=true param required")
		}
	case "task_cancelled":
		if reason, _ := request.Params["cancellation_reason"].(string); reason == "" {
			return fmt.Errorf("guard cancellation_reason_recorded failed: cancellation_reason param required")
		} else {
			task["cancellation_reason"] = reason
		}
	}
	return nil
}

type taskEntityTransitionsMap map[string]string

type taskEntityTransitionsResolver struct {
	m taskEntityTransitionsMap
}

func (r taskEntityTransitionsResolver) resolve(from, event string) (string, bool) {
	if r.m == nil {
		return "", false
	}
	to, ok := r.m[from+"|"+event]
	return to, ok
}

func taskEntityTransitions(catalog *transition.Catalog) taskEntityTransitionsResolver {
	res := taskEntityTransitionsMap{}
	if catalog == nil || catalog.Definition == nil {
		return taskEntityTransitionsResolver{m: res}
	}
	life, ok := catalog.Definition.EntityLifecycles["task"]
	if !ok {
		return taskEntityTransitionsResolver{m: res}
	}
	for _, t := range life.Transitions {
		res[t.From+"|"+t.Event] = t.To
	}
	return taskEntityTransitionsResolver{m: res}
}

// canonicalTaskEventList returns the canonical 8 TASK events, ordered.
// Used by tests that need to enumerate the surface without parsing
// loop-definition.json directly.
func canonicalTaskEventList() []string {
	return []string{
		"internal_review_passed",
		"document_pass_lock",
		"builder_activated",
		"builder_reported",
		"task_closing_contract_passed",
		"task_blocked",
		"task_resumed",
		"task_cancelled",
	}
}
