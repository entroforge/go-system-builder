package investigation

import (
	"errors"
	"fmt"
	"time"

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
	if request.ExpectedRevision >= 0 && current.Revision != request.ExpectedRevision {
		return runtime.Snapshot{}, fmt.Errorf("%w: expected Runtime revision %d but it is %d; next: runtime investigation status --case-id %s", runtime.ErrStaleRevision, request.ExpectedRevision, current.Revision, request.CaseID)
	}
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
	commitRevision := runtimeCommitRevision(request.ExpectedRevision, current.State)
	return updateRuntime(writer, request.ExpectedRevision, runtime.Mutation{
		EventID:                fmt.Sprintf("evt-s8-route-consume-%s-r%d", request.CaseID, commitRevision+1),
		TransitionID:           "S8-ROUTE-CONSUME",
		Event:                  "investigation_route_consumed",
		Actor:                  actor,
		IdempotencyKey:         fmt.Sprintf("runtime:s8:route-consume:%s:%d", request.CaseID, commitRevision),
		RuntimeID:              stringField(current.State["runtime_id"]),
		EvidenceIDs:            []string{stringField(pointer["path"])},
		From:                   routeCursor(current.State),
		To:                     routeCursorFor(route, current.State),
		RequestID:              "s8-route-consume",
		BaselineGeneration:     baselineGenerationValue(current.State),
		GateID:                 "S8-ROUTE-CONSUME",
		GateFingerprint:        "sha256:s8-route-consume-v1",
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
				round := integerValueOrZero(review["round"]) + 1
				review["round"] = round
				review["clean_round"] = nil
				review["plan"] = nil
				review["claims"] = map[string]any{}
				review["assignments"] = map[string]any{}
				review["observation_batch"] = nil
				review["investigation"] = nil
				review["round_entry"] = map[string]any{"transition_id": "S8-ROUTE-CONSUME", "round": round, "baseline_generation": baselineGenerationValue(state), "change_impact_ref": nil}
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
