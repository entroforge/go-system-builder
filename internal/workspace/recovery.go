package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReplaceExecution reserves a fresh execution without reusing or deleting the
// old tree. Callers must hold the workspace lock and old launch lease, verify
// the expected generation, and reject existing integration/delivery evidence.
func (b *ExecutionRegistry) ReplaceExecution(ctx context.Context, state map[string]any, id, owner string, expected int) (Execution, error) {
	old, ok := b.Execution(id, RuntimeID(state), Generation(state))
	if !ok || old.Generation != expected || old.AgentID != owner {
		return Execution{}, fmt.Errorf("replacement execution identity changed")
	}
	if old.DeliveryRef != "" {
		return Execution{}, fmt.Errorf("immutable delivery requires domain recovery before replacement")
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return Execution{}, err
	}
	if err := b.ValidateInputs(old); err != nil {
		return Execution{}, err
	}
	// Plan against a copy: validation failure cannot erase the active execution.
	next := *b
	next.Executions = map[string]Execution{}
	for k, v := range b.Executions {
		next.Executions[k] = v
	}
	delete(next.Executions, id)
	paths := []string{}
	for p := range old.Inputs {
		paths = append(paths, p)
	}
	e, err := next.Plan(ctx, state, id, owner, old.WritePaths, old.Checks, paths)
	if err != nil {
		return Execution{}, err
	}
	// Retain the originally frozen source, not Main's newer HEAD.
	e.BaseCommit = old.BaseCommit
	e.Inputs = old.Inputs
	e.Generation = old.Generation + 1
	key := fmt.Sprintf("%s-g%d-e%d", id, e.BaselineGeneration, e.Generation)
	e.Path = filepath.Join(b.MainRoot, ".worktrees", key)
	e.Branch = "codex/" + key
	old.Status = "retired"
	b.History = append(b.History, old)
	b.Executions[id] = e
	return e, nil
}

// AdoptExecution requires an explicitly supplied historical base and hashes.
// Neither the old base nor input provenance is inferred from current HEAD.
func (b *ExecutionRegistry) AdoptExecution(ctx context.Context, state map[string]any, e Execution) (Execution, error) {
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return e, err
	}
	if !safeID.MatchString(e.AssignmentID) || !safeID.MatchString(e.AgentID) || e.RuntimeID != RuntimeID(state) || e.BaselineGeneration != Generation(state) || e.Generation < 1 || e.TargetBranch != b.Branch || len(e.Inputs) == 0 {
		return e, fmt.Errorf("adoption requires explicit historical identity and input hashes")
	}
	if _, exists := b.Executions[e.AssignmentID]; exists {
		return e, fmt.Errorf("assignment already has an execution; resume or replace it explicitly")
	}
	if e.BaseCommit == "" {
		return e, fmt.Errorf("historical base_commit is required; current HEAD is not a substitute")
	}
	base, err := Git(ctx, b.MainRoot, "rev-parse", "--verify", e.BaseCommit+"^{commit}")
	if err != nil || base != e.BaseCommit {
		return e, fmt.Errorf("adoption requires the full historical commit SHA")
	}
	path, err := Canonical(e.Path)
	if err != nil || path != e.Path || path == b.MainRoot {
		return e, fmt.Errorf("adoption requires a canonical distinct Worker root")
	}
	for _, other := range b.AllExecutions() {
		if other.Path == e.Path || other.Branch == e.Branch {
			return e, fmt.Errorf("historical tree is already owned")
		}
	}
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		if _, err := os.Lstat(filepath.Join(e.Path, ".claude", name)); err == nil {
			return e, fmt.Errorf("historical Worker contains a second Runtime/journal; reconcile explicitly before adoption")
		} else if !os.IsNotExist(err) {
			return e, err
		}
	}
	if err = b.ValidateExecution(ctx, e, false); err != nil {
		return e, err
	}
	if err = b.ValidateInputs(e); err != nil {
		return e, err
	}
	paths, err := GitBytes(ctx, e.Path, "diff", "--name-only", "--no-renames", "-z", e.BaseCommit, "HEAD")
	if err != nil {
		return e, err
	}
	for _, p := range strings.Split(string(paths), "\x00") {
		if p == "" {
			continue
		}
		allowed := false
		for _, scope := range e.WritePaths {
			if PathMatchesScope(p, scope) {
				allowed = true
			}
		}
		if !allowed {
			return e, fmt.Errorf("historical change outside assignment scope: %s", p)
		}
	}
	e.Status = "preparing"
	e.PlatformSessionID = ""
	e.DeliveryRef = ""
	e.DeliverySHA256 = ""
	e.BootstrapSHA256 = ""
	return e, nil
}

func (b *ExecutionRegistry) AllExecutions() []Execution {
	out := append([]Execution{}, b.History...)
	for _, e := range b.Executions {
		out = append(out, e)
	}
	return out
}
