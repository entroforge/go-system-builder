package assignment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// BugEventRequest drives a BUG entity lifecycle transition.
type BugEventRequest struct {
	BugID            string         `json:"bug_id"`
	Event            string         `json:"event"`
	RuntimeID        string         `json:"runtime_id"`
	ExpectedRevision int            `json:"expected_revision"`
	MessagePath      string         `json:"message_path"`
	Params           map[string]any `json:"params,omitempty"`
}

// AdvanceBug applies one BUG lifecycle event. It mirrors AdvanceAgent: read
// runtime state, validate the event against the BUG lifecycle, locate the
// target BUG entity by ID, check preconditions, mutate in place, and commit
// via store.Update.
//
// The lifecycle itself is defined in loop-definition.json under
// entity_lifecycles.bug. This function hard-codes the preconditions because
// they reference runtime fields the engine does not yet evaluate generically.
func AdvanceBug(root, statePath, journalPath string, request BugEventRequest) (loopruntime.Snapshot, error) {
	if request.BugID == "" || request.Event == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("bug_id and event are required")
	}
	defPath := filepath.Join(root, "docs", "loop-definition.json")
	defData, err := os.ReadFile(defPath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read Loop Definition: %w", err)
	}
	var def struct {
		EntityLifecycles struct {
			Bug struct {
				InitialState string `json:"initial_state"`
				Transitions  []struct {
					Event  string   `json:"event"`
					From   string   `json:"from"`
					To     string   `json:"to"`
					Guards []string `json:"guards"`
				} `json:"transitions"`
			} `json:"bug"`
		} `json:"entity_lifecycles"`
	}
	if err := json.Unmarshal(defData, &def); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("parse Loop Definition: %w", err)
	}

	// Resolve the transition.
	var resolved struct {
		From   string
		To     string
		Guards []string
	}
	found := false
	for _, t := range def.EntityLifecycles.Bug.Transitions {
		if t.Event == request.Event {
			resolved.From = t.From
			resolved.To = t.To
			resolved.Guards = t.Guards
			found = true
			break
		}
	}
	if !found {
		return loopruntime.Snapshot{}, fmt.Errorf("unknown BUG lifecycle event %s", request.Event)
	}

	// Optionally validate the message envelope if a path is provided.
	if request.MessagePath != "" {
		validator := schema.NewValidator(root)
		msgData, err := os.ReadFile(filepath.Join(root, request.MessagePath))
		if err == nil {
			if err := validator.ValidateBytes(
				"agent-message.schema.json", msgData); err != nil {
				return loopruntime.Snapshot{}, fmt.Errorf("message validation: %w", err)
			}
		}
	}

	// Read current runtime state for consistency checks.
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read state: %w", err)
	}
	var currentState map[string]any
	if err := json.Unmarshal(stateData, &currentState); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("parse state: %w", err)
	}
	currentRuntimeID, _ := currentState["runtime_id"].(string)
	if request.RuntimeID != "" && request.RuntimeID != currentRuntimeID {
		return loopruntime.Snapshot{}, fmt.Errorf(
			"runtime_id mismatch: request=%s state=%s", request.RuntimeID, currentRuntimeID)
	}
	commitSequence, err := commitRevision(request.ExpectedRevision, currentState)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	lifecycle, ok := currentState["lifecycle"].(map[string]any)
	if !ok {
		return loopruntime.Snapshot{}, fmt.Errorf("runtime lifecycle must be an object")
	}
	cursor := map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
	occurredAt := time.Now().UTC()

	mutation := loopruntime.Mutation{
		Audit: loopruntime.AuditEnvelope{
			EventID:        fmt.Sprintf("evt-bug-%s-%d", request.BugID, commitSequence+1),
			TransitionID:   "BUG-LIFECYCLE",
			Event:          request.Event,
			Actor:          "orchestrator",
			IdempotencyKey: fmt.Sprintf("bug:%s:%s:%d", request.BugID, request.Event, commitSequence),
			RuntimeID:      currentRuntimeID,
			From:           cursor,
			To:             cursor,
			EvidenceIDs:    []string{},
		},
		OccurredAt: occurredAt,
		Apply: func(state map[string]any) error {
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime entities must be an object")
			}
			bugs, ok := entities["bugs"].([]any)
			if !ok {
				return fmt.Errorf("entities.bugs must be an array")
			}
			var located map[string]any
			for _, raw := range bugs {
				entry, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if id, _ := entry["id"].(string); id == request.BugID {
					located = entry
					break
				}
			}
			if located == nil {
				return fmt.Errorf("BUG %s is not registered in runtime", request.BugID)
			}
			currentStateName, _ := located["state"].(string)
			if currentStateName != resolved.From {
				return fmt.Errorf(
					"BUG %s is in state %s, event %s requires %s",
					request.BugID, currentStateName, request.Event, resolved.From)
			}
			if err := checkBugGuards(located, request, resolved.Guards); err != nil {
				return err
			}
			// Structural identity check (BUG-003 §4b.2(g)): the catalog's
			// retest_started guard is `original_finder_assigned`, but the
			// close path (closing_contract_passed) is what BUG-003 actually
			// wants protected. We enforce it explicitly on every event that
			// could land the BUG in `closed`, regardless of whether the
			// catalog lists the guard. The Builder who registered the BUG
			// must not be the one to close it.
			if resolved.To == "closed" {
				actor, _ := request.Params["actor_agent_id"].(string)
				if actor == "" {
					return fmt.Errorf(
						"BUG %s cannot close: actor_agent_id param required for identity check (BUG_CLOSE_BY_FINDER_FORBIDDEN)",
						request.BugID)
				}
				if OriginalFinderAssigned(located, actor) {
					return fmt.Errorf(
						"BUG %s cannot be closed by actor %q: actor is in original_finder_agent_ids (BUG_CLOSE_BY_FINDER_FORBIDDEN)",
						request.BugID, actor)
				}
			}
			located["state"] = resolved.To
			// Bump attempt_count when entering investigation (a retry).
			if resolved.To == "investigating" {
				attempts := readBugInt(located, "attempt_count")
				nextAttempt := attempts + 1
				// RC-15 (S9-M4/M5): closing_contract_failed also increments
				// same_contract_failure_count so the consecutive-contract-
				// failure cap becomes reachable. The pass path resets the
				// streak counter.
				if request.Event == "closing_contract_failed" {
					located["same_contract_failure_count"] = readBugInt(located, "same_contract_failure_count") + 1
				}
				// Enforce repair limits before committing the retry. The limits
				// come from runtime configuration.repair.
				if err := checkRetryLimits(state, located, nextAttempt); err != nil {
					return err
				}
				located["attempt_count"] = nextAttempt
			}
			if request.Event == "closing_contract_passed" && resolved.To == "closed" {
				located["same_contract_failure_count"] = 0
			}
			// Record the fix path when reported.
			if request.Event == "fix_reported" {
				if path, ok := request.Params["fix_ref"].(string); ok && path != "" {
					located["fix_ref"] = path
				}
			}
			located["updated_at"] = occurredAt.Format(time.RFC3339Nano)
			state["updated_at"] = occurredAt.Format(time.RFC3339Nano)
			return nil
		},
	}
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	return updateRuntime(store, request.ExpectedRevision, mutation)
}

// checkBugGuards evaluates the guard list for a BUG transition. Guards that
// require evidence are satisfied by the presence of the corresponding param.
func checkBugGuards(bug map[string]any, request BugEventRequest, guards []string) error {
	for _, guard := range guards {
		switch guard {
		case "finding_source_present":
			if _, ok := bug["path"].(string); !ok || bug["path"] == "" {
				return fmt.Errorf("guard %s failed: BUG has no finding source path", guard)
			}
		case "canonical_id_assigned":
			if _, ok := bug["id"].(string); !ok || bug["id"] == "" {
				return fmt.Errorf("guard %s failed: BUG has no canonical id", guard)
			}
		case "rejection_reason_recorded":
			reason, _ := request.Params["rejection_reason"].(string)
			if reason == "" {
				return fmt.Errorf("guard %s failed: rejection_reason param required", guard)
			}
			bug["rejection_reason"] = reason
		case "canonical_bug_reference_present":
			ref, _ := request.Params["duplicate_of"].(string)
			if ref == "" {
				return fmt.Errorf("guard %s failed: duplicate_of param required", guard)
			}
			bug["duplicate_of"] = ref
		case "repair_task_and_builder_present":
			task, _ := request.Params["repair_task_id"].(string)
			builder, _ := request.Params["repair_builder_agent_id"].(string)
			if task == "" || builder == "" {
				return fmt.Errorf("guard %s failed: repair_task_id and repair_builder_agent_id required", guard)
			}
			bug["repair_task_id"] = task
			bug["repair_builder_agent_id"] = builder
		case "repair_evidence_present":
			fix, _ := request.Params["fix_ref"].(string)
			if fix == "" {
				return fmt.Errorf("guard %s failed: fix_ref param required", guard)
			}
			bug["fix_ref"] = fix
		case "original_finder_assigned":
			actor, _ := request.Params["actor_agent_id"].(string)
			if actor == "" {
				// Fall back to top-level ActorAgentID; the BUG lifecycle
				// request does not currently expose this, but the catalog
				// guard contract still requires a present actor.
				return fmt.Errorf(
					"guard %s failed: actor_agent_id param required for identity check", guard)
			}
			if OriginalFinderAssigned(bug, actor) {
				return fmt.Errorf(
					"guard %s failed: actor %q is an original finder of BUG %s; Builder cannot close its own BUG (BUG_CLOSE_BY_FINDER_FORBIDDEN)",
					guard, actor, bugIDFor(bug))
			}
		case "targeted_reverification_complete":
			ev, _ := request.Params["reverification_evidence"].(string)
			if ev == "" {
				return fmt.Errorf("guard %s failed: reverification_evidence param required", guard)
			}
			bug["reverification_evidence"] = ev
		case "failure_evidence_recorded":
			ev, _ := request.Params["failure_evidence"].(string)
			if ev == "" {
				return fmt.Errorf("guard %s failed: failure_evidence param required", guard)
			}
			bug["failure_evidence"] = ev
		case "root_cause_evidence_complete":
			ev, _ := request.Params["root_cause_evidence"].(string)
			if ev == "" {
				return fmt.Errorf("guard %s failed: root_cause_evidence param required", guard)
			}
			bug["root_cause_evidence"] = ev
		case "bug_closing_contract_complete":
			ev, _ := request.Params["closing_contract"].(string)
			if ev == "" {
				return fmt.Errorf("guard %s failed: closing_contract param required", guard)
			}
			bug["closing_contract"] = ev
		default:
			// Unknown guards are treated as satisfied (trust the loop-definition).
		}
	}
	return nil
}

func readBugInt(bug map[string]any, key string) int {
	switch n := bug[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// OriginalFinderAssigned implements the identity check required by BUG-003
// §4b.2(g) and TASK-015 §6. It returns true when the supplied actor agent
// id appears in bug.original_finder_agent_ids. This is an IDENTITY check,
// not a presence check: a non-empty original_finder_agent_ids array is
// necessary but not sufficient — the actor must specifically be in the
// array.
//
// Callers must treat a true return as "reject the operation" (e.g., the
// Builder cannot close its own BUG). The typed error code is
// BUG_CLOSE_BY_FINDER_FORBIDDEN.
//
// The function is exported so tests in bug_lifecycle_test.go (and any other
// test file) can exercise it directly without re-implementing the loop.
func OriginalFinderAssigned(bug map[string]any, actor string) bool {
	if bug == nil || actor == "" {
		return false
	}
	finders, ok := bug["original_finder_agent_ids"].([]any)
	if !ok {
		return false
	}
	for _, finder := range finders {
		if s, _ := finder.(string); s == actor {
			return true
		}
	}
	return false
}

// bugIDFor is a defensive id extractor used in error messages so the
// caller does not have to repeat the type-assertion boilerplate.
func bugIDFor(bug map[string]any) string {
	id, _ := bug["id"].(string)
	return id
}

// checkRetryLimits enforces the runtime configuration.repair limits before a
// BUG is sent back to investigation for another attempt. RC-09 (S9-7): this
// is a thin adapter over the canonical transition.CheckRepairLimit — the
// limit semantics live in exactly one place (internal/transition), and this
// path raises the same typed *transition.RepairLimitError the GTR-004 bridge
// recognizes, instead of a locally-formatted error the dispatcher cannot
// catch.
//
// max_same_contract_failures stays local: it caps consecutive Closing
// Contract failures for the same BUG and has no CheckRepairLimit equivalent.
// RC-15 (S9-M5): it raises the same typed *transition.RepairLimitError so the
// GTR-004 bridge (adapter.DispatchRepairLimitExceeded) can pause the Loop.
//
// Both limits default to 0 (unlimited) when the configuration is absent, so
// the check is opt-in via the runtime configuration block.
func checkRetryLimits(state map[string]any, bug map[string]any, nextAttempt int) error {
	if limit := transition.CheckRepairLimit(state, withAttemptCount(bug, nextAttempt)); limit != nil {
		return limit
	}
	configuration, ok := state["configuration"].(map[string]any)
	if !ok {
		return nil
	}
	repair, ok := configuration["repair"].(map[string]any)
	if !ok {
		return nil
	}
	maxSameContract := readBugInt(repair, "max_same_contract_failures")
	if maxSameContract > 0 {
		sameContractFailures := readBugInt(bug, "same_contract_failure_count")
		if sameContractFailures >= maxSameContract {
			id, _ := bug["id"].(string)
			return fmt.Errorf(
				"BUG %s exceeded max_same_contract_failures (%d): contract may be wrong; pause the Loop: %w",
				id, maxSameContract, &transition.RepairLimitError{BugID: id, Attempts: sameContractFailures, Max: maxSameContract})
		}
	}
	return nil
}

// withAttemptCount returns a shallow view of bug whose attempt_count reads as
// nextAttempt. The lifecycle evaluates the limit against the attempt it is
// about to commit, which is one higher than the persisted count.
func withAttemptCount(bug map[string]any, nextAttempt int) map[string]any {
	view := make(map[string]any, len(bug)+1)
	for key, value := range bug {
		view[key] = value
	}
	view["attempt_count"] = nextAttempt
	view["id"], _ = bug["id"].(string)
	return view
}
