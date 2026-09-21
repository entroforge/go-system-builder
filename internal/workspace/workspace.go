// Package workspace owns the binding between one control runtime and its
// execution worktrees. Git directories are never inferred from branch names.
package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/processtree"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

type ExecutionRegistry struct {
	History    []Execution          `json:"execution_history,omitempty"`
	Version    int                  `json:"binding_version"`
	MainRoot   string               `json:"main_root"`
	CommonDir  string               `json:"common_dir"`
	Branch     string               `json:"integration_branch"`
	BoundHead  string               `json:"bound_head"`
	Executions map[string]Execution `json:"executions"`
}
type Execution struct {
	ReworkRef          string            `json:"rework_ref,omitempty"`
	PlatformSessionID  string            `json:"platform_session_id,omitempty"`
	AssignmentID       string            `json:"assignment_id"`
	RuntimeID          string            `json:"runtime_id"`
	BaselineGeneration int               `json:"baseline_generation"`
	Generation         int               `json:"execution_generation"`
	AgentID            string            `json:"agent_id"`
	Path               string            `json:"worktree_path"`
	Branch             string            `json:"branch"`
	TargetBranch       string            `json:"target_branch"`
	BaseCommit         string            `json:"base_commit"`
	Status             string            `json:"status"`
	WritePaths         []string          `json:"write_paths"`
	Checks             []string          `json:"required_checks"`
	Inputs             map[string]string `json:"inputs"`
	BootstrapSHA256    string            `json:"bootstrap_sha256,omitempty"`
	DeliveryRef        string            `json:"delivery_ref,omitempty"`
	DeliverySHA256     string            `json:"delivery_sha256,omitempty"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Git(ctx context.Context, root string, args ...string) (string, error) {
	out, err := gitRaw(ctx, root, args...)
	return strings.TrimSpace(out), err
}

func GitBytes(ctx context.Context, root string, args ...string) ([]byte, error) {
	value, err := gitRaw(ctx, root, args...)
	return []byte(value), err
}
func gitRaw(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	c.WaitDelay = time.Second
	var output bytes.Buffer
	c.Stdout, c.Stderr = &output, &output
	err := processtree.Run(c)
	out := output.Bytes()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
func Canonical(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}
func Decode(state map[string]any) (*ExecutionRegistry, error) {
	raw, ok := state["workspace"]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var v ExecutionRegistry
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if v.Version != 1 || v.MainRoot == "" || v.CommonDir == "" || v.Branch == "" {
		return nil, fmt.Errorf("invalid workspace binding")
	}
	if v.Executions == nil {
		v.Executions = map[string]Execution{}
	}
	if err := v.ValidateAuthority(state); err != nil {
		return nil, err
	}
	return &v, nil
}
func Encode(v *ExecutionRegistry) map[string]any {
	data, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}
func Load(root string) (*ExecutionRegistry, map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return nil, nil, err
	}
	var state map[string]any
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, nil, err
	}
	v, err := Decode(state)
	return v, state, err
}
func New(ctx context.Context, root string) (*ExecutionRegistry, error) {
	b, err := inspectCheckout(ctx, root)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(b.MainRoot, ".claude/loop-state.json"))
	if err != nil {
		return nil, fmt.Errorf("read REQ workspace authority: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if err := b.ValidateAuthority(state); err != nil {
		return nil, err
	}
	return b, nil
}

// inspectCheckout reads Git identity only; it grants no execution authority.
func inspectCheckout(ctx context.Context, root string) (*ExecutionRegistry, error) {
	root, err := Canonical(root)
	if err != nil {
		return nil, err
	}
	top, err := Git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top, err = Canonical(top)
	if err != nil || top != root {
		return nil, fmt.Errorf("workspace binding requires the worktree root")
	}
	branch, err := Git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("bind a named local branch, not detached HEAD: %w", err)
	}
	common, err := Git(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	common, err = Canonical(common)
	if err != nil {
		return nil, err
	}
	head, err := Git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	return &ExecutionRegistry{Version: 1, MainRoot: root, CommonDir: common, Branch: branch, BoundHead: head, Executions: map[string]Execution{}}, nil
}
func (b *ExecutionRegistry) Validate(ctx context.Context, root string) error {
	actual, err := New(ctx, root)
	if err != nil {
		return err
	}
	if actual.MainRoot != b.MainRoot || actual.CommonDir != b.CommonDir || actual.Branch != b.Branch {
		return fmt.Errorf("workspace binding mismatch: expected %s on %s; found %s on %s; do not auto-checkout", b.MainRoot, b.Branch, actual.MainRoot, actual.Branch)
	}
	return nil
}
func (b *ExecutionRegistry) Execution(id, runtimeID string, generation int) (Execution, bool) {
	e, ok := b.Executions[id]
	return e, ok && e.RuntimeID == runtimeID && e.BaselineGeneration == generation
}
func Generation(state map[string]any) int {
	b, _ := state["baseline"].(map[string]any)
	switch n := b["generation"].(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
func RuntimeID(state map[string]any) string { s, _ := state["runtime_id"].(string); return s }
func ControlFile(path string) bool {
	p := filepath.ToSlash(path)
	switch p {
	case ".claude/loop-state.json", ".claude/loop-events.jsonl", ".claude/loop-metrics.json":
		return true
	}
	for _, prefix := range []string{".claude/evidence/", ".claude/workgroups/", ".claude/workspace-launch/", ".claude/review/", ".claude/hook"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return strings.HasPrefix(p, ".claude/") && (strings.HasSuffix(p, ".lock") || strings.HasSuffix(p, ".lock.process"))
}

// CleanInputs includes untracked inputs; it never adds, stashes or deletes them.
func (b *ExecutionRegistry) CleanInputs(ctx context.Context) error {
	out, err := gitRaw(ctx, b.MainRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	var dirty []string
	rows := strings.Split(out, "\x00")
	for i := 0; i < len(rows); i++ {
		row := rows[i]
		if len(row) >= 3 && strings.ContainsAny(row[:2], "RC") {
			i++
		}
		if row == "" {
			continue
		}
		if len(row) < 4 {
			dirty = append(dirty, row)
			continue
		}
		p := row[3:]
		if ControlFile(p) {
			continue
		}
		registered := false
		for _, e := range b.AllExecutions() {
			rel, _ := filepath.Rel(b.MainRoot, e.Path)
			if filepath.ToSlash(strings.TrimSuffix(p, "/")) == filepath.ToSlash(rel) {
				registered = true
			}
		}
		if !registered {
			dirty = append(dirty, p)
		}
	}
	if len(dirty) > 0 {
		return fmt.Errorf("uncommitted inputs must be resolved before dispatch/integration (no automatic add/stash): %s", strings.Join(dirty, ", "))
	}
	return nil
}

// Plan freezes a commit and the exact input bytes, before any Git side effect.
func (b *ExecutionRegistry) Plan(ctx context.Context, state map[string]any, id, agent string, scope, checks []string, inputPaths []string) (Execution, error) {
	if err := b.ValidateAuthority(state); err != nil {
		return Execution{}, err
	}
	if RuntimeID(state) == "" {
		return Execution{}, fmt.Errorf("runtime identity is required")
	}
	if !safeID.MatchString(id) || !safeID.MatchString(agent) {
		return Execution{}, fmt.Errorf("invalid assignment or agent identity")
	}
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return Execution{}, err
	}
	if err := b.CleanInputs(ctx); err != nil {
		return Execution{}, err
	}
	if e, ok := b.Execution(id, RuntimeID(state), Generation(state)); ok {
		if !reflect.DeepEqual(e.WritePaths, append([]string{}, scope...)) || !reflect.DeepEqual(e.Checks, append([]string{}, checks...)) {
			return Execution{}, fmt.Errorf("execution scope/checks changed; retain the original execution for explicit recovery")
		}
		if e.AgentID != agent {
			return Execution{}, fmt.Errorf("assignment already belongs to %s", e.AgentID)
		}
		for _, p := range inputPaths {
			if _, ok := e.Inputs[filepath.ToSlash(filepath.Clean(p))]; !ok {
				return Execution{}, fmt.Errorf("execution lacks frozen input %s; explicit migration is required", p)
			}
		}
		if err := b.ValidateInputs(e); err != nil {
			return Execution{}, err
		}
		return e, nil
	}
	head, err := Git(ctx, b.MainRoot, "rev-parse", "HEAD")
	if err != nil {
		return Execution{}, err
	}
	gen := 1
	if old, ok := b.Executions[id]; ok {
		gen = old.Generation + 1
	}
	key := fmt.Sprintf("%s-g%d-e%d", id, Generation(state), gen)
	e := Execution{AssignmentID: id, RuntimeID: RuntimeID(state), BaselineGeneration: Generation(state), Generation: gen, AgentID: agent, Path: filepath.Join(b.MainRoot, ".worktrees", key), Branch: "codex/" + key, TargetBranch: b.Branch, BaseCommit: head, Status: "preparing", WritePaths: append([]string{}, scope...), Checks: append([]string{}, checks...), Inputs: map[string]string{}}
	for _, p := range inputPaths {
		clean := filepath.Clean(p)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return Execution{}, fmt.Errorf("input escapes control root: %s", p)
		}
		resolved, err := Canonical(filepath.Join(b.MainRoot, clean))
		if err != nil {
			return Execution{}, err
		}
		rel, err := filepath.Rel(b.MainRoot, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Execution{}, fmt.Errorf("input symlink escapes control root: %s", p)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return Execution{}, err
		}
		hash := sha256.Sum256(data)
		e.Inputs[filepath.ToSlash(clean)] = hex.EncodeToString(hash[:])
	}
	return e, nil
}
func (b *ExecutionRegistry) Materialize(ctx context.Context, e Execution) error {
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return err
	}
	if err := b.ValidateInputs(e); err != nil {
		return err
	}
	if _, err := os.Lstat(e.Path); os.IsNotExist(err) {
		if _, err = Git(ctx, b.MainRoot, "worktree", "add", "-b", e.Branch, e.Path, e.BaseCommit); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := b.ValidateExecution(ctx, e, true); err != nil {
		return err
	}
	return b.PublishPointer(e)
}

func (b *ExecutionRegistry) PublishPointer(e Execution) error {
	// Only the control pointer is copied. Runtime, evidence, secrets and mutable
	// settings are not duplicated. The registered control state authenticates it.
	dir := filepath.Join(e.Path, ".claude")
	physical, err := CanonicalProspective(dir)
	if err != nil || physical != dir {
		return fmt.Errorf("Worker control directory must not be a symlink")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"control_root": b.MainRoot, "assignment_id": e.AssignmentID, "runtime_id": e.RuntimeID, "execution_generation": e.Generation})
	pointer := filepath.Join(dir, "loop-workspace.json")
	if info, err := os.Lstat(pointer); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("Worker control pointer must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if old, err := os.ReadFile(pointer); err == nil && string(old) != string(data) {
		return fmt.Errorf("preserve conflicting Worker control pointer for explicit recovery")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return publishBootstrapFile(pointer, data, 0600)
}

// ValidateInputs detects control-plane artifact drift even for ignored files
// which do not appear in the committed Worker tree.
func (b *ExecutionRegistry) ValidateInputs(e Execution) error {
	for path, expected := range e.Inputs {
		resolved, err := Canonical(filepath.Join(b.MainRoot, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(b.MainRoot, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("frozen input escapes Main: %s", path)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != expected {
			return fmt.Errorf("frozen execution input changed: %s; preserve the execution for recovery", path)
		}
	}
	return nil
}
func (b *ExecutionRegistry) ValidateExecution(ctx context.Context, e Execution, initial bool) error {
	top, err := Git(ctx, e.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	top, err = Canonical(top)
	if err != nil || top != e.Path {
		return fmt.Errorf("worker path is not its registered worktree root")
	}
	common, err := Git(ctx, e.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	common, err = Canonical(common)
	if err != nil || common != b.CommonDir {
		return fmt.Errorf("worker belongs to another repository")
	}
	branch, err := Git(ctx, e.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != e.Branch {
		return fmt.Errorf("worker branch mismatch")
	}
	head, err := Git(ctx, e.Path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if initial && head != e.BaseCommit {
		return fmt.Errorf("unrecorded work in preparing worktree; preserve it for explicit recovery")
	}
	if _, err := Git(ctx, e.Path, "merge-base", "--is-ancestor", e.BaseCommit, head); err != nil {
		return fmt.Errorf("worker history no longer descends from its dispatch baseline")
	}
	if e.BootstrapSHA256 != "" {
		if err := b.ValidateBootstrap(e); err != nil {
			return err
		}
	}
	return nil
}

// CanonicalProspective resolves existing symlink ancestors while preserving
// a not-yet-created leaf; file tools commonly create new paths.
func CanonicalProspective(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := abs
	// Initialization may target a directory that does not exist yet. Resolve
	// its nearest existing ancestor, still detecting an enclosing Worker.
	var suffix []string
	for {
		canonical, err := Canonical(current)
		if err == nil {
			current = canonical
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		current = filepath.Join(current, suffix[i])
	}
	return current, nil
}

// PointerRoot finds a worker pointer without crossing a nested Git checkout.
// An unmarked path is returned unchanged; it is never guessed from branch names.
func PointerRoot(root string) (string, bool, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false, err
	}
	current, err := CanonicalProspective(abs)
	if err != nil {
		return "", false, err
	}
	for {
		if _, err := os.Lstat(filepath.Join(current, ".claude/loop-workspace.json")); err == nil {
			return current, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return abs, false, nil
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, false, nil
		}
		current = parent
	}
}

// ResolveControl never trusts a pointer alone: it must match the active
// Runtime snapshot, execution generation, main root and Git repository.
func ResolveControl(ctx context.Context, root string) (string, error) {
	worker, marked, err := PointerRoot(root)
	if err != nil {
		return "", err
	}
	if !marked {
		return worker, nil
	}
	data, err := os.ReadFile(filepath.Join(worker, ".claude/loop-workspace.json"))
	if err != nil {
		return "", err
	}
	var p struct {
		Root    string `json:"control_root"`
		ID      string `json:"assignment_id"`
		Runtime string `json:"runtime_id"`
		Gen     int    `json:"execution_generation"`
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return "", err
	}
	if !filepath.IsAbs(p.Root) || p.ID == "" || p.Runtime == "" || p.Gen < 1 {
		return "", fmt.Errorf("invalid worker control pointer")
	}
	main, err := Canonical(p.Root)
	if err != nil {
		return "", err
	}
	snapshot, err := loopruntime.NewStore(filepath.Join(main, ".claude/loop-state.json"), filepath.Join(main, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return "", fmt.Errorf("worker control runtime unavailable: %w", err)
	}
	b, err := Decode(snapshot.State)
	if err != nil {
		return "", err
	}
	if b == nil {
		return "", fmt.Errorf("worker control binding missing")
	}
	if main != b.MainRoot || main == worker {
		return "", fmt.Errorf("worker pointer does not identify its bound main workspace")
	}
	e, ok := b.Execution(p.ID, RuntimeID(snapshot.State), Generation(snapshot.State))
	if !ok || e.AssignmentID != p.ID || e.Path != worker || e.RuntimeID != p.Runtime || e.Generation != p.Gen || e.Status != "ready" || e.TargetBranch != b.Branch {
		return "", fmt.Errorf("stale or unregistered worker pointer")
	}
	// A second control state is not a fallback: it is an ambiguous authority.
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		if _, err := os.Lstat(filepath.Join(worker, ".claude", name)); err == nil {
			return "", fmt.Errorf("worker contains duplicate control file %s; preserve it for explicit recovery", name)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if err = b.Validate(ctx, main); err != nil {
		return "", err
	}
	if err = b.ValidateExecution(ctx, e, false); err != nil {
		return "", err
	}
	return main, nil
}

// RequireMain rejects initialization and binding from a registered Worker,
// even when the Worker pointer or its control Runtime is currently damaged.
func RequireMain(root string) error {
	worker, marked, err := PointerRoot(root)
	if err != nil {
		return err
	}
	if marked {
		return fmt.Errorf("%s is a registered Worker workspace; initialize/bind only in the main workspace; do not create a second Runtime", worker)
	}
	return nil
}

// ValidateAuthority treats the execution registry as a projection of the locked
// REQ destinations. Neither current checkout nor assignment metadata can replace it.
func (b *ExecutionRegistry) ValidateAuthority(state map[string]any) error {
	req, _ := state["bound_req"].(map[string]any)
	raw, _ := req["workspace"].(map[string]any)
	bytes, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var authority Binding
	if err := json.Unmarshal(bytes, &authority); err != nil {
		return err
	}
	if authority.ProjectRoot == "" || authority.DevBranch == "" || authority.ReleaseUpstream == "" || authority.BoundCommit == "" {
		return fmt.Errorf("REQ workspace authority is missing; explicitly bind development and release destinations before execution")
	}
	if filepath.Clean(authority.ProjectRoot) != filepath.Clean(b.MainRoot) || strings.TrimPrefix(authority.DevBranch, "refs/heads/") != b.Branch {
		return fmt.Errorf("execution registry differs from REQ-bound project root/development branch")
	}
	for _, e := range b.Executions {
		if e.TargetBranch != b.Branch {
			return fmt.Errorf("execution %s target differs from REQ-bound development branch", e.AssignmentID)
		}
	}
	return nil
}
