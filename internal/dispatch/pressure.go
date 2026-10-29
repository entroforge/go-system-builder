package dispatch

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

type Pressure struct {
	RuntimeID  string `json:"runtime_id"`
	Generation int    `json:"generation"`
	Paused     bool   `json:"paused"`
	Unreceived int    `json:"unreceived"`
	Max        int    `json:"max_unreceived_results"`
	Resume     int    `json:"resume_unreceived_results"`
}

func ProjectPressure(root string, state map[string]any) (*Pressure, error) {
	config, err := runtime.DispatchConfig(state)
	if err != nil || config == nil {
		return nil, err
	}
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "building" {
		return nil, nil
	}
	loaded, err := hookctx.FromSnapshot(root, "", runtime.Snapshot{State: state})
	if err != nil {
		return nil, err
	}
	rows, err := integration.Pending(root, loaded)
	if err != nil {
		return nil, err
	}
	builders := map[string]bool{}
	resolvedOwners := map[string]bool{}
	for _, a := range loaded.Assignments {
		resolvedOwners[a.OwnerAgentID] = true
		if a.BaselineGeneration != loaded.BaselineGeneration {
			continue
		}
		switch a.RoleFamily {
		case "backend-builder", "frontend-builder", "test-builder":
			builders[a.AssignmentID] = true
		}
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		role, _ := a["role"].(string)
		id, _ := a["id"].(string)
		if (role == "backend-builder" || role == "frontend-builder" || role == "test-builder") && !resolvedOwners[id] {
			return nil, fmt.Errorf("dispatch pressure cannot resolve original Builder assignment for %s; restore its registered manifest before counting results", id)
		}
	}
	u := countUnreceived(rows, builders)
	return pressureFromCount(loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration, *config, u, state["dispatch_pressure"]), nil
}

func countUnreceived(rows []integration.PendingIntegration, builders map[string]bool) int {
	u := 0
	for _, row := range rows {
		if !builders[row.AssignmentID] {
			continue
		}
		if row.Acknowledged && row.CurrentReport {
			continue
		}
		u++
	}
	return u
}

func pressureFromCount(runtimeID string, generation int, config runtime.DispatchBackpressureConfig, u int, previousValue any) *Pressure {
	previous, _ := previousValue.(map[string]any)
	paused, _ := previous["paused"].(bool)
	if previous["runtime_id"] != runtimeID || Number(previous["generation"]) != generation {
		paused = false
	}
	if u >= config.MaxUnreceivedResults {
		paused = true
	} else if u <= config.ResumeUnreceivedResults {
		paused = false
	}
	return &Pressure{RuntimeID: runtimeID, Generation: generation, Paused: paused, Unreceived: u, Max: config.MaxUnreceivedResults, Resume: config.ResumeUnreceivedResults}
}

// ReconcilePressure runs only in an existing original domain Writer mutation.
// The persisted fields are hysteresis, never another assignment/result queue.
func ReconcilePressure(root string, state map[string]any) error {
	p, err := ProjectPressure(root, state)
	if err != nil || p == nil {
		return err
	}
	state["dispatch_pressure"] = map[string]any{"runtime_id": p.RuntimeID, "generation": p.Generation, "paused": p.Paused}
	return nil
}

func CheckConfiguredBackpressure(root string, state map[string]any) error {
	p, err := ProjectPressure(root, state)
	if err != nil {
		return err
	}
	if p != nil && p.Paused {
		return fmt.Errorf("S6 Builder registration queued by explicit dispatch backpressure: %d unreceived; receive original results until <= %d (trigger %d)", p.Unreceived, p.Resume, p.Max)
	}
	return ReconcilePressure(root, state)
}

func applyConfiguredPressure(root string, state map[string]any, board *Board) error {
	p, err := ProjectPressure(root, state)
	if err != nil {
		return err
	}
	board.Pressure = p
	if p != nil && p.Paused {
		board.Next = []string{}
		for i := range board.Rows {
			if board.Rows[i].Eligible {
				board.Rows[i].Dispatchable = false
				board.Rows[i].Reason = "explicit dispatch backpressure; receive original results to the resume threshold"
			}
		}
	}
	return nil
}
