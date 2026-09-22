package policy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/workspace"
)

const RuleWorkspaceBoundary = "workspace_boundary"

func workspaceExecution(input Input) (workspace.Execution, bool) {
	b := input.Runtime.Workspace
	if b == nil {
		return workspace.Execution{}, false
	}
	var found workspace.Execution
	count := 0
	for _, e := range b.Executions {
		if e.AgentID == input.EffectiveAgentID() && e.RuntimeID == input.Runtime.RuntimeID && e.BaselineGeneration == input.Runtime.CurrentBaselineGeneration && e.Status == "ready" {
			if input.Runtime.AssignmentID != "" && input.Runtime.AssignmentID != e.AssignmentID {
				continue
			}
			found = e
			count++
		}
	}
	return found, count == 1
}
func mutationRoot(input Input) string {
	if e, ok := workspaceExecution(input); ok {
		return e.Path
	}
	return input.Runtime.ProjectRoot
}
func lockedMutationPath(input Input, path string) string {
	if input.Runtime.Workspace == nil {
		return path
	}
	if !filepath.IsAbs(path) {
		base := input.CWD
		if base == "" {
			base = mutationRoot(input)
		}
		path = filepath.Join(base, path)
	}
	if resolved, err := workspace.CanonicalProspective(path); err == nil {
		if rel, err := filepath.Rel(mutationRoot(input), resolved); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return reviewerRelativePath(input, path)
}
func lockedReferencePath(input Input, path string) string {
	if input.Runtime.Workspace == nil {
		return path
	}
	root := input.Runtime.Workspace.MainRoot
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if resolved, err := workspace.CanonicalProspective(path); err == nil {
		path = resolved
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}
func beneath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// This boundary covers native CWD and explicit filesystem writes. Shell code
// is not an OS sandbox; a Worker with an unresolvable mutation needs a bounded
// execution adapter before it can run that command in the new workspace mode.
func WorkspaceBoundaryDecision(input Input) (Decision, bool) {
	if input.Event != "PreToolUse" {
		return Decision{}, false
	}
	switch input.ToolName {
	case "Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "ToolSearch":
		return Decision{}, false
	}
	b := input.Runtime.Workspace
	deny := func(reason string) (Decision, bool) {
		return Decision{Decision: "deny", RuleID: RuleWorkspaceBoundary, Reason: reason, Recovery: []string{"keep Main in its bound workspace and use the registered Worker checkout for its assignment; inspect workspace status before retrying"}, Retry: RetryAfterRecoveryValidation}, true
	}
	if input.Runtime.WorkspaceError != "" {
		return deny(input.Runtime.WorkspaceError)
	}
	if b == nil {
		return Decision{}, false
	}
	control, err := workspace.Canonical(input.Runtime.ProjectRoot)
	if err != nil || control != b.MainRoot {
		return deny("control root differs from the bound Main workspace")
	}
	if input.CWD == "" {
		return deny("bound workspace requires the platform cwd; do not infer Main from a missing directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.Validate(ctx, b.MainRoot); err != nil {
		return deny(err.Error())
	}
	cwd, err := workspace.Canonical(input.CWD)
	if err != nil {
		return deny(err.Error())
	}
	execution, worker := workspaceExecution(input)
	if !worker && input.EffectiveAgentID() != "" {
		for _, e := range b.Executions {
			if e.AgentID == input.EffectiveAgentID() {
				return deny("Worker execution is stale, ambiguous or not ready; no fallback to Main authority")
			}
		}
	}
	root := b.MainRoot
	if worker {
		root = execution.Path
		if err := b.ValidateExecution(ctx, execution, false); err != nil {
			return deny(err.Error())
		}
	}
	if !beneath(root, cwd) {
		return deny(fmt.Sprintf("execution cwd %s is outside the bound workspace %s", cwd, root))
	}
	for _, other := range b.AllExecutions() {
		if worker && other.AssignmentID == execution.AssignmentID && other.Generation == execution.Generation {
			continue
		}
		if beneath(other.Path, cwd) {
			return deny("Main or another Worker cannot execute mutating tools from this Worker checkout")
		}
	}
	paths, mutating := repairMutationPaths(input)
	if worker && input.ToolName == "Bash" && workspaceAdapterCommand(input, execution) {
		return Decision{}, false
	}
	if worker && input.ToolName == "Bash" {
		if input.ToolInput["command"] == "pwd" {
			return Decision{}, false
		}
		decision, handled := deny("Worker shell requires a registered adapter; Git observation is available without write authority")
		decision.Recovery = []string{workspace.ObservationCommands(execution)["cwd"], workspace.ObservationCommands(execution)["status"], workspace.ObservationCommands(execution)["diff"], workspace.ObservationCommands(execution)["log"]}
		return decision, handled
	}
	if worker && mutating && len(paths) == 0 {
		return deny("Worker command has no provable write paths; use scoped file tools until the bounded command runner is available")
	}
	if input.ToolName == "Bash" && !worker {
		command, _ := input.ToolInput["command"].(string)
		for _, other := range b.AllExecutions() {
			rel, _ := filepath.Rel(b.MainRoot, other.Path)
			if strings.Contains(command, other.Path) || strings.Contains(command, filepath.ToSlash(rel)) {
				// Read-only file tools are explicitly allowed above. Shell commands
				// naming a Worker checkout need the execution adapter, not Main.
				return deny("Main shell command targets a Worker checkout; use read-only file tools to inspect it")
			}
		}
	}
	for _, raw := range paths {
		path := raw
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !beneath(root, path) || !reviewerPathContained(root, filepath.ToSlash(rel)) {
			return deny("write path escapes the execution workspace: " + raw)
		}
		physical, err := workspace.CanonicalProspective(path)
		if err != nil || !beneath(root, physical) {
			return deny("write path resolves outside execution workspace: " + raw)
		}
		path = physical
		physicalRel, _ := filepath.Rel(root, physical)
		if worker && workerControlAsset(filepath.ToSlash(physicalRel)) {
			return deny("Worker cannot modify Git or Harness control assets: " + raw)
		}
		for _, other := range b.AllExecutions() {
			if worker && other.AssignmentID == execution.AssignmentID && other.Generation == execution.Generation {
				continue
			}
			if beneath(other.Path, path) {
				return deny("write targets another execution workspace: " + raw)
			}
		}
		if worker {
			allowed := strings.HasPrefix(filepath.ToSlash(physicalRel), ".claude/submissions/")
			for _, scope := range execution.WritePaths {
				if workspace.PathMatchesScope(filepath.ToSlash(rel), filepath.ToSlash(scope)) && workspace.PathMatchesScope(filepath.ToSlash(physicalRel), filepath.ToSlash(scope)) {
					allowed = true
					break
				}
			}
			if !allowed {
				return deny("write is outside the registered assignment scope: " + raw)
			}
		}
	}
	return Decision{}, false
}

func workspaceAdapterCommand(input Input, e workspace.Execution) bool {
	if e.BootstrapSHA256 == "" || input.ToolName != "Bash" {
		return false
	}
	command, _ := input.ToolInput["command"].(string)
	if command == workspace.WorkerInvocation(e, "commit") || command == workspace.WorkerInvocation(e, "deliver") || command == workspace.WorkerInvocation(e, "begin") || command == workspace.WorkerInvocation(e, "report") {
		return true
	}
	for _, cmd := range workspace.ObservationCommands(e) {
		if command == cmd {
			return true
		}
	}
	for i := range e.Checks {
		if command == workspace.CheckInvocation(e, i) {
			return true
		}
	}
	return false
}

func workerControlAsset(path string) bool {
	for _, prefix := range []string{".git", ".claude/inputs", ".claude/bin", ".claude/agents", ".claude/skills", ".claude/settings.json", ".claude/settings.local.json", ".claude/loop-workspace.json", ".claude/loop-bootstrap.json", ".claude/loop-state.json", ".claude/loop-events.jsonl"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
