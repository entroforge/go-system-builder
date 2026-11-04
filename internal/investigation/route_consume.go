package investigation

import (
	"errors"
	"fmt"
	"time"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ConsumeRouteRequest is the Runtime-side acknowledgement of a Case route.
// The Case artifact remains the route authority; this operation only applies
// the one downstream lifecycle effect associated with that route.
type ConsumeRouteRequest struct {
	ExpectedRevision int
	CaseID           string
	Actor            string
	OccurredAt       time.Time
}

// ConsumeCaseRoute closes the S8 route-to-next-stage gap without adding a
// second state machine. s7_no_change starts a fresh verification round,
// s2_spec_rework returns to planning.design, and duplicate records a durable
// canonical-follow-up acknowledgement. REQ changes deliberately remain at
// the human pause boundary because they require a new locked REQ.
func ConsumeCaseRoute(root, statePath, journalPath string, request ConsumeRouteRequest) (runtime.Snapshot, error) {
	if request.CaseID == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "case_id is required")
	}
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return runtime.Snapshot{}, fmt.Errorf("read Runtime before route consumption: %w", err)
	}
	// G18 §2/§3.1: the Runtime write count is no longer a gate on this
	// professional completion. A route consumption used to be refused because
	// the Runtime moved for an unrelated reason; the domain protection is the
	// three-piece set around it:
	//   received row — the consumed route is the received row and one Case
	//     route is consumed once (the route_consumed_at check below and the
	//     in-CAS route recheck);
	//   event dedup — the consumption commits the semantic identity
	//     evt-s8-route-consume-<caseID> and its idempotency key, so the
	//     journal dedups a retried intent;
	//   domain version — the Case's own identity and its persisted route stay
	//     the versions checked here.
	// A caller that names the revision it holds keeps the Runtime CAS as its
	// explicit binding; the convention is -1 to omit the expectation.
	review, ok := current.State["review"].(map[string]any)
	if !ok || review == nil {
		return runtime.Snapshot{}, errors.New("Runtime review section is missing; restore state.review before consuming the Case route")
	}
	pointer, ok := review["investigation"].(map[string]any)
	if !ok || pointer == nil || stringField(pointer["case_id"]) != request.CaseID {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case is not the active InvestigationCase; inspect runtime investigation status --all")
	}
	route := stringField(pointer["route"])
	if route == "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case has no persisted route; complete S8 route before consuming it")
	}
	if stringField(pointer["route_consumed_at"]) != "" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "Case route %q was already consumed at %s; follow the recorded next action", route, stringField(pointer["route_consumed_at"]))
	}
	if route == "human_req_change" {
		return runtime.Snapshot{}, caseWorkflowError(request.CaseID, "human_req_change requires the human pause boundary; run `runtime pause --reason <reason> --approved-by <human>` before `req amend`")
	}
	at := request.OccurredAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	actor := request.Actor
	if actor == "" {
		actor = "orchestrator"
	}
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	// G18 §1: the route consumption is named by its own responsibility (the
	// Case) and carries no Runtime write count. One Case route is consumed
	// once — the consumption records route_consumed_at on the pointer — so this
	// identity stays unique and stays stable across a retried consumption.
	eventID := "evt-s8-route-consume-" + request.CaseID
	return updateRuntime(writer, request.ExpectedRevision, runtime.Mutation{
		EventID:      eventID,
		TransitionID: "S8-ROUTE-CONSUME",
		Event:        "investigation_route_consumed",
		Actor:        actor,
		// G18 §1.3: the idempotency key carries the responsibility coordinate
		// (the Case) and the intent key (the consumed route).
		IdempotencyKey:         fmt.Sprintf("runtime:s8:route-consume:%s:%s", request.CaseID, route),
		RuntimeID:              stringField(current.State["runtime_id"]),
		EvidenceIDs:            []string{stringField(pointer["path"])},
		From:                   routeCursor(current.State),
		To:                     routeCursorFor(route, current.State),
		RequestID:              "s8-route-consume",
		BaselineGeneration:     baselineGenerationValue(current.State),
		GateID:                 "S8-ROUTE-CONSUME",
		ProducerResponsibility: "S8 Route Consumer",
		OccurredAt:             at,
		Apply: func(state map[string]any) error {
			review := state["review"].(map[string]any)
			active := review["investigation"].(map[string]any)
			if stringField(active["case_id"]) != request.CaseID || stringField(active["route"]) != route {
				return fmt.Errorf("active Case route changed during consumption; re-read runtime investigation status --case-id %s", request.CaseID)
			}
			active["route_consumed_at"] = at.UTC().Format(time.RFC3339Nano)
			active["route_consumer"] = "S8-ROUTE-CONSUME"
			switch route {
			case "s7_no_change":
				delete(review, "round")
				review["clean_round"] = nil
				review["plan"] = nil
				review["claims"] = map[string]any{}
				review["assignments"] = map[string]any{}
				review["investigation"] = nil
				review["round_entry"] = map[string]any{"transition_id": "S8-ROUTE-CONSUME", "plan_id": reviewmeta.NewPlanID(stringField(state["runtime_id"]), eventID), "baseline_generation": baselineGenerationValue(state), "change_impact_ref": nil}
				setLifecycleValue(state, "verification", "running")
			case "s2_spec_rework":
				active["status"] = "closed"
				active["next_action"] = "continue planning.design for the specification rework; re-run S3-S7 after the new locked design is ready"
				setLifecycleValue(state, "planning", "design")
			case "duplicate":
				active["status"] = "closed"
				active["next_action"] = "inspect canonical_case_ref and continue the canonical Case; do not investigate or repair this duplicate"
			}
			state["updated_at"] = at.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
}

func routeCursor(state map[string]any) map[string]any {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	return map[string]any{"state": lifecycle["state"], "phase": lifecycle["phase"]}
}

func routeCursorFor(route string, state map[string]any) map[string]any {
	switch route {
	case "s7_no_change":
		return map[string]any{"state": "verification", "phase": "running"}
	case "s2_spec_rework":
		return map[string]any{"state": "planning", "phase": "design"}
	default:
		return routeCursor(state)
	}
}

func baselineGenerationValue(state map[string]any) int {
	baseline, _ := state["baseline"].(map[string]any)
	return integerValueOrZero(baseline["generation"])
}

func setLifecycleValue(state map[string]any, lifecycleState, phase string) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle == nil {
		lifecycle = map[string]any{}
		state["lifecycle"] = lifecycle
	}
	lifecycle["state"] = lifecycleState
	lifecycle["phase"] = phase
	lifecycle["phase_revision"] = integerValueOrZero(lifecycle["phase_revision"]) + 1
}
