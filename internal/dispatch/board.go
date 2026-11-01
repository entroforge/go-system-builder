// Package dispatch projects the approved S4 plan and current S6 facts. It does
// not spawn platform agents, mutate checkboxes or introduce a second scheduler.
package dispatch

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

type Board struct {
	Pressure        *Pressure              `json:"configured_backpressure,omitempty"`
	RuntimeID       string                 `json:"-"`
	Generation      int                    `json:"-"`
	ReceptionAdvice *ReceptionAdvice       `json:"reception_advice,omitempty"`
	Plan            *semantic.DispatchPlan `json:"plan,omitempty"`
	Rows            []semantic.DispatchRow `json:"rows,omitempty"`
	Next            []string               `json:"next,omitempty"`
	Legacy          bool                   `json:"legacy"`
	Revision        any                    `json:"runtime_revision"`
}

func Number(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

func sourceBoard(state map[string]any, legacy bool) Board {
	baseline, _ := state["baseline"].(map[string]any)
	runtimeID, _ := state["runtime_id"].(string)
	return Board{Legacy: legacy, Revision: state["revision"], RuntimeID: runtimeID, Generation: Number(baseline["generation"])}
}
func View(root string, state map[string]any) (*fileview.View, error) {
	ref, e := fileview.DevelopmentRef(state)
	if e != nil {
		return nil, e
	}
	catalog, e := transition.LoadCatalog(root)
	if e != nil {
		return nil, e
	}
	return fileview.New(root, ref, catalog.Definition.FileSources)
}
func Load(root string, state map[string]any, capacity int) (Board, error) {
	if !qualitygate.HasDispatchPlan(state) {
		b := sourceBoard(state, true)
		err := applyConfiguredPressure(root, state, &b)
		return b, err
	}
	files, err := View(root, state)
	if err != nil {
		return Board{}, err
	}
	return LoadWithFiles(root, state, files, capacity)
}
func LoadWithFiles(root string, state map[string]any, files fileview.Reader, capacity int) (Board, error) {
	board := sourceBoard(state, false)
	bound, _ := state["bound_req"].(map[string]any)
	req, _ := bound["id"].(string)
	p, err := semantic.LoadDispatchPlan(root, files, req)
	if err != nil {
		return board, err
	}
	board.Plan = p
	baseline, _ := state["baseline"].(map[string]any)
	generation := Number(baseline["generation"])
	docs, _ := state["documents"].([]any)
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		if Number(d["generation"]) != generation || (d["kind"] != "task" && d["kind"] != "dispatch_plan") {
			continue
		}
		path, _ := d["path"].(string)
		if _, e := files.ReadFile(filepath.Join(root, path)); e != nil {
			return board, fmt.Errorf("read registered dispatch input %s: %w", path, e)
		}
	}
	return ProjectSelectedPlan(root, state, files, p, capacity)
}

// ProjectSelectedPlan reuses the caller's selected plan while projecting the
// current Runtime's ownership, dependencies and integration facts. It checks
// the existing registration identities but does not re-read TASKs or index.
// files remains the original source reader for existing progress consumers.
func ProjectSelectedPlan(root string, state map[string]any, files fileview.Reader, p *semantic.DispatchPlan, capacity int) (Board, error) {
	board := sourceBoard(state, false)
	board.Plan = p
	if p == nil {
		return board, fmt.Errorf("selected dispatch plan is unavailable")
	}
	bound, _ := state["bound_req"].(map[string]any)
	if req, _ := bound["id"].(string); p.REQ != req {
		return board, fmt.Errorf("selected dispatch plan belongs to a different REQ")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation := Number(baseline["generation"])
	docs, _ := state["documents"].([]any)
	expected := map[string]string{}
	found := false
	for _, raw := range docs {
		d, _ := raw.(map[string]any)
		if Number(d["generation"]) != generation {
			continue
		}
		if d["kind"] != "task" && d["kind"] != "dispatch_plan" {
			continue
		}
		path, _ := d["path"].(string)
		if d["kind"] == "task" {
			id, _ := d["id"].(string)
			expected[id] = path
		} else {
			if path != p.Path {
				return board, fmt.Errorf("dispatch plan identity mismatch")
			}
			found = true
		}
	}
	if len(p.Problems) > 0 {
		return board, fmt.Errorf("dispatch plan: %s", strings.Join(p.Problems, "; "))
	}
	if p.Path == "" {
		return board, fmt.Errorf("registered dispatch plan disappeared; return to planning")
	}
	if !found {
		return board, fmt.Errorf("dispatch plan has not passed S4 registration")
	}
	for _, t := range p.Tasks {
		path, exists := expected[t.ID]
		if !exists {
			return board, fmt.Errorf("unregistered TASK %s", t.ID)
		}
		if filepath.Clean(filepath.FromSlash(path)) != filepath.Clean(filepath.FromSlash(t.Path)) {
			return board, fmt.Errorf("registered TASK %s path %s differs from dispatch plan path %s", t.ID, path, t.Path)
		}
		delete(expected, t.ID)
	}
	if len(expected) > 0 {
		return board, fmt.Errorf("registered TASK set differs from plan")
	}
	progress := qualitygate.PlannedBuilderProgress(qualitygate.Input{Root: root, Snapshot: runtime.Snapshot{State: state}, Files: files})
	facts := map[string]semantic.DispatchFact{}
	active := 0
	for id, p := range progress {
		facts[id] = semantic.DispatchFact{State: p.State, Unknown: p.Unknown, Reason: p.Reason, Integrated: p.Integrated, DeliveryRecorded: p.DeliveryRecorded}
		if p.State == workstate.Active && !p.DeliveryRecorded {
			active++
		}
	}
	slots := capacity - active
	if slots < 0 {
		slots = 0
	}
	board.Rows, board.Next = semantic.NextDispatch(p, facts, slots)
	if err := applyConfiguredPressure(root, state, &board); err != nil {
		return board, err
	}
	if v, ok := files.(interface{ Verify() error }); ok {
		if err := v.Verify(); err != nil {
			return board, err
		}
	}
	return board, nil
}

// CheckRegistration executes inside the existing runtime mutation. Independent
// peers remain candidates; only real dependencies/conflicts/owners prevent dispatch.
func CheckRegistration(root string, state map[string]any, taskID string) error {
	if state["state"] != "building" { // lifecycle state is normally nested
		life, _ := state["lifecycle"].(map[string]any)
		if life["state"] != "building" {
			return nil
		}
	}
	b, e := Load(root, state, 1<<20)
	if e != nil {
		return e
	}
	if b.Legacy {
		return nil
	}
	return CheckBoardRegistration(b, taskID)
}

// CheckBoardRegistration uses the freshly projected Runtime facts in board.
func CheckBoardRegistration(b Board, taskID string) error {
	if b.Legacy {
		return nil
	}
	// Evaluate only actual constraints: suggested priority is not an authorization gate.
	facts := map[string]bool{}
	for _, r := range b.Rows {
		facts[r.Task.ID] = r.Integrated && r.State == workstate.Done
	}
	for _, r := range b.Rows {
		if r.Task.ID != taskID {
			continue
		}
		if r.Unknown {
			return fmt.Errorf("TASK %s has unknown current ownership: %s", taskID, r.Reason)
		}
		switch r.State {
		case workstate.Done, workstate.Active, workstate.Blocked, workstate.Cancelled:
			return fmt.Errorf("TASK %s already has an owner/result: %s", taskID, r.State)
		}
		for _, dep := range append(append([]string{}, r.Task.Dependencies...), r.Task.Before...) {
			if !facts[dep] {
				return fmt.Errorf("TASK %s awaits verified integration of %s", taskID, dep)
			}
		}
		for _, other := range b.Rows {
			switch other.State {
			case workstate.Active, workstate.Blocked:
				if semantic.DispatchConflict(r.Task, other.Task) {
					return fmt.Errorf("TASK %s conflicts with active %s", taskID, other.Task.ID)
				}
			}
		}
		return nil
	}
	return fmt.Errorf("TASK %s is outside approved dispatch plan", taskID)
}

// ValidateWorkPackage binds the actual workgroup input and write authorization
// to the reviewed TASK, rather than merely checking the requested TASK ID.
func ValidateWorkPackage(root string, state map[string]any, taskID, taskPath string, _ []byte, writes []string, writers int) error {
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "building" || !qualitygate.HasDispatchPlan(state) {
		return nil
	}
	b, e := Load(root, state, 0)
	if e != nil {
		return e
	}
	return ValidateBoardWorkPackage(root, b, taskID, taskPath, writes, writers)
}

// ValidateBoardWorkPackage checks the original selected TASK and write scope
// without reopening or comparing a second copy of its source material.
func ValidateBoardWorkPackage(root string, b Board, taskID, taskPath string, writes []string, writers int) error {
	if writers != 1 {
		return fmt.Errorf("planned TASK %s requires exactly one writer assignment", taskID)
	}
	for _, row := range b.Rows {
		if row.Task.ID != taskID {
			continue
		}
		abs, _ := filepath.Abs(taskPath)
		expected, _ := filepath.Abs(filepath.Join(root, row.Task.Path))
		if abs != expected {
			return fmt.Errorf("workgroup TASK path differs from reviewed plan")
		}
		if len(writes) == 0 {
			return fmt.Errorf("planned Builder must declare write_paths")
		}
		for _, write := range writes {
			normalized := filepath.ToSlash(filepath.Clean(write))
			if filepath.IsAbs(write) || normalized == ".." || strings.HasPrefix(normalized, "../") {
				return fmt.Errorf("invalid write path %s", write)
			}
			allowed := false
			for _, scope := range row.Task.Writes {
				scope = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(scope)), "/")
				if scope == "." || normalized == scope || strings.HasPrefix(normalized, scope+"/") {
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("write path %s exceeds reviewed TASK %s", write, taskID)
			}
		}
		return nil
	}
	return fmt.Errorf("TASK outside reviewed plan: %s", taskID)
}
