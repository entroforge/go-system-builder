package dispatch

import (
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"sort"
)

// Reception is a projection of original assignments and integration checkpoints.
// It is not a queue and cannot qualify a report or authorize integration.
type Reception struct {
	AssignmentID      string   `json:"assignment_id"`
	TaskID            string   `json:"task_id,omitempty"`
	AgentID           string   `json:"agent_id"`
	State             string   `json:"state"`
	Failure           string   `json:"failure,omitempty"`
	Command           []string `json:"command_argv"`
	ReworkCommand     []string `json:"rework_command_argv,omitempty"`
	PendingCommand    []string `json:"pending_command_argv,omitempty"`
	CheckpointRef     string   `json:"checkpoint_ref,omitempty"`
	NeedsReworkReason bool     `json:"needs_rework_reason,omitempty"`
}
type ReceptionAction struct {
	Kind           string   `json:"action_kind"`
	Subject        string   `json:"subject"`
	Owner          string   `json:"owner"`
	Argv           []string `json:"argv,omitempty"`
	ReworkCommand  []string `json:"rework_command_argv,omitempty"`
	PendingCommand []string `json:"pending_command_argv,omitempty"`
	Reason         string   `json:"reason"`
	CheckpointRef  string   `json:"checkpoint_ref,omitempty"`
	Question       string   `json:"question,omitempty"`
}
type ReceptionAdvice struct {
	IntegrationObservation *integration.WorkspaceObservation `json:"integration_observation,omitempty"`
	ConfiguredPressure     *Pressure                         `json:"configured_backpressure,omitempty"`
	Action                 ReceptionAction                   `json:"current_action"`
	Pending                []ReceptionAction                 `json:"pending_actions"`
	DispatchBudget         *int                              `json:"dispatch_budget"`
	EligibleTasks          []string                          `json:"eligible_tasks"`
	RecommendedTasks       []string                          `json:"recommended_tasks"`
	Running                int                               `json:"running"`
	Unreceived             int                               `json:"unreceived"`
	Failed                 int                               `json:"failed"`
	VerifiedAwaitingAck    int                               `json:"verified_awaiting_ack"`
	Cleanup                int                               `json:"cleanup"`
	Backpressure           bool                              `json:"backpressure"`
	AdvisoryOnly           bool                              `json:"advisory_only"`
}

// WithIntegrationObservation suppresses a duplicate receive recommendation
// only from the real original lock signal. It never changes registration.
func WithIntegrationObservation(advice ReceptionAdvice, observation integration.WorkspaceObservation) ReceptionAdvice {
	advice.IntegrationObservation = &observation
	if observation.LockState == "busy" {
		advice.Action = ReceptionAction{Kind: "observe_integration", Owner: "main", Subject: observation.LockRef, Argv: []string{"loop-harness", "runtime", "workspace", "integration-status", "--root", observation.Checkout}, Reason: "original Main workspace lock is busy; observe this resource before retrying reception; no Assignment or PASS inferred"}
	}
	return advice
}

// AdviseReception keeps the approved board and required set intact. Unknown
// capacity is null, and an explicit ceiling affects recommendations only.
func AdviseReception(board Board, pending []Reception, capacity *int, maxUnreceived *int) ReceptionAdvice {
	out := ReceptionAdvice{Pending: []ReceptionAction{}, EligibleTasks: []string{}, RecommendedTasks: []string{}, AdvisoryOnly: true}
	out.ConfiguredPressure = board.Pressure
	if board.Pressure != nil && (maxUnreceived == nil || *maxUnreceived > board.Pressure.Max) {
		maximum := board.Pressure.Max
		maxUnreceived = &maximum
	}
	consumers := map[string]int{}
	if board.Plan != nil {
		for _, task := range board.Plan.Tasks {
			seen := map[string]bool{}
			for _, id := range append(append([]string{}, task.Dependencies...), task.Before...) {
				if !seen[id] {
					consumers[id]++
					seen[id] = true
				}
			}
		}
	}
	for _, row := range board.Rows {
		if row.State == workstate.Active && !row.DeliveryRecorded {
			out.Running++
		}
		if row.State == workstate.Pending && row.Eligible {
			out.EligibleTasks = append(out.EligibleTasks, row.Task.ID)
		}
	}
	rows := append([]Reception{}, pending...)
	isCleanup := func(state string) bool { return state == "cleanup_pending" || state == "acknowledged" }
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if isCleanup(a.State) != isCleanup(b.State) {
			return !isCleanup(a.State)
		}
		if consumers[a.TaskID] != consumers[b.TaskID] {
			return consumers[a.TaskID] > consumers[b.TaskID]
		}
		if a.TaskID != b.TaskID {
			return a.TaskID < b.TaskID
		}
		return a.AssignmentID < b.AssignmentID
	})
	for _, row := range rows {
		a := ReceptionAction{
			Kind:           "receive_integration",
			Subject:        row.AssignmentID,
			Owner:          "main",
			Argv:           append([]string{}, row.Command...),
			ReworkCommand:  append([]string(nil), row.ReworkCommand...),
			PendingCommand: append([]string(nil), row.PendingCommand...),
			Reason:         "consume original reported result before dispatching more work",
			CheckpointRef:  row.CheckpointRef,
		}
		switch row.State {
		case "cleanup_pending", "acknowledged":
			out.Cleanup++
			a.Kind = "finish_cleanup"
			a.Reason = "finish original acknowledged integration cleanup"
		case "verified":
			out.VerifiedAwaitingAck++
			a.Kind = "acknowledge_integration"
			a.Reason = "continue original verified checkpoint acknowledgement"
		case "preserved", "blocked", "rework_pending":
			out.Unreceived++
			out.Failed++
			a.Kind = "recover_integration"
			a.Reason = "recover original preserved/blocked checkpoint: " + row.Failure
		default:
			out.Unreceived++
		}
		if row.NeedsReworkReason {
			a.Question = "Describe the required correction or environment recovery before choosing the original retry/rework route."
		}
		out.Pending = append(out.Pending, a)
	}
	if capacity != nil {
		budget := *capacity - out.Running
		if budget < 0 {
			budget = 0
		}
		if maxUnreceived != nil {
			remaining := *maxUnreceived - out.Unreceived - out.VerifiedAwaitingAck
			if remaining < 0 {
				remaining = 0
			}
			if remaining < budget {
				budget = remaining
				out.Backpressure = true
			}
		}
		out.DispatchBudget = &budget
		if board.Pressure != nil && board.Pressure.Paused {
			budget = 0
			out.Backpressure = true
		}
		for _, id := range board.Next {
			if len(out.RecommendedTasks) >= budget {
				break
			}
			out.RecommendedTasks = append(out.RecommendedTasks, id)
		}
	}
	if len(out.Pending) > 0 {
		out.Action = out.Pending[0]
	} else if len(out.RecommendedTasks) > 0 {
		out.Action = ReceptionAction{Kind: "dispatch_ready", Subject: out.RecommendedTasks[0], Owner: "main", Reason: "choose original eligible TASK and perform its existing registration checks"}
	} else if capacity == nil && len(out.EligibleTasks) > 0 {
		out.Action = ReceptionAction{Kind: "dispatch_ready", Owner: "main", Reason: "choose eligible TASKs via original s6 prepare; capacity is optional"}
	} else {
		out.Action = ReceptionAction{Kind: "wait_or_observe", Owner: "main", Reason: "no current receivable result or selected dispatch slot"}
	}
	return out
}
