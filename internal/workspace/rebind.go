package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type RebindRequest struct {
	RuntimeID    string            `json:"runtime_id"`
	OldMainRoot  string            `json:"old_main_root"`
	OldCommonDir string            `json:"old_common_dir"`
	NewMainRoot  string            `json:"new_main_root"`
	NewCommonDir string            `json:"new_common_dir"`
	Branch       string            `json:"branch"`
	ExpectedHead string            `json:"expected_head"`
	Paths        map[string]string `json:"paths"`
}

// PlanRebind validates an explicit relocation, including repaired native Git
// links. It never guesses a new repository from a remote URL or branch name.
// Git worktree move/repair must have completed before rebinding the Harness.
func (b *ExecutionRegistry) PlanRebind(ctx context.Context, state map[string]any, r RebindRequest) (*ExecutionRegistry, error) {
	if err := b.ValidateAuthority(state); err != nil {
		return nil, err
	}
	if r.RuntimeID != RuntimeID(state) || r.OldMainRoot != b.MainRoot || r.OldCommonDir != b.CommonDir || r.Branch != b.Branch || r.ExpectedHead == "" {
		return nil, fmt.Errorf("rebind expected identity does not match recorded Main")
	}
	if r.OldMainRoot != r.NewMainRoot {
		if _, err := os.Stat(r.OldMainRoot); err == nil {
			return nil, fmt.Errorf("old Main still exists; refusing ambiguous copied Runtime")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	actual, err := inspectCheckout(ctx, r.NewMainRoot)
	if err != nil {
		return nil, fmt.Errorf("repair native Git worktree links before Harness rebind: %w", err)
	}
	if actual.MainRoot != r.NewMainRoot || actual.CommonDir != r.NewCommonDir || actual.Branch != r.Branch || actual.BoundHead != r.ExpectedHead {
		return nil, fmt.Errorf("new Main repository/branch/HEAD differs from explicit relocation request")
	}
	if _, err := Git(ctx, r.NewMainRoot, "cat-file", "-e", b.BoundHead+"^{commit}"); err != nil {
		return nil, fmt.Errorf("original binding commit absent from relocated repository")
	}
	next := *b
	next.MainRoot = r.NewMainRoot
	next.CommonDir = r.NewCommonDir
	next.Executions = map[string]Execution{}
	move := func(e Execution) (Execution, error) {
		path, ok := r.Paths[e.Path]
		if !ok {
			return e, fmt.Errorf("explicit relocation mapping missing for %s", e.Path)
		}
		physical, err := CanonicalProspective(path)
		if err != nil || physical != path {
			return e, fmt.Errorf("relocation path must be canonical: %s", path)
		}
		e.Path = path
		if e.Status != "complete" && e.Status != "retired" {
			if err := next.ValidateExecution(ctx, e, false); err != nil {
				return e, err
			}
			if err := next.ValidateInputs(e); err != nil {
				return e, err
			}
		}
		return e, nil
	}
	seen := map[string]bool{r.NewMainRoot: true}
	for id, e := range b.Executions {
		moved, err := move(e)
		if err != nil {
			return nil, err
		}
		if seen[moved.Path] {
			return nil, fmt.Errorf("relocation maps two execution roots onto one directory")
		}
		seen[moved.Path] = true
		next.Executions[id] = moved
	}
	next.History = nil
	for _, e := range b.History {
		moved, err := move(e)
		if err != nil {
			return nil, err
		}
		if seen[moved.Path] {
			return nil, fmt.Errorf("relocation overlaps retired execution")
		}
		seen[moved.Path] = true
		next.History = append(next.History, moved)
	}
	return &next, nil
}

func (b *ExecutionRegistry) RelocatePointer(e Execution, oldMain string) error {
	path := filepath.Join(e.Path, ".claude/loop-workspace.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p struct {
		Root       string `json:"control_root"`
		ID         string `json:"assignment_id"`
		Runtime    string `json:"runtime_id"`
		Generation int    `json:"execution_generation"`
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	if (p.Root != oldMain && p.Root != b.MainRoot) || p.ID != e.AssignmentID || p.Runtime != e.RuntimeID || p.Generation != e.Generation {
		return fmt.Errorf("preserve conflicting relocation pointer: %s", path)
	}
	physical, err := Canonical(path)
	if err != nil || physical != path {
		return fmt.Errorf("relocation pointer is not a local regular file")
	}
	data, err = json.Marshal(map[string]any{"control_root": b.MainRoot, "assignment_id": e.AssignmentID, "runtime_id": e.RuntimeID, "execution_generation": e.Generation})
	if err != nil {
		return err
	}
	return publishBootstrapFile(path, data, 0600)
}
