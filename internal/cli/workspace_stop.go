package cli

import (
	"context"
	"time"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// A registered top-level Worker Stop belongs to that execution, not Main's
// global Stop queue. S5 retains its issued-packet-specific preflight.
func workspaceWorkerStopPreflight(root string, input policy.Input) (bool, policy.Decision) {
	if input.Event != "Stop" || input.EffectiveAgentID() == "" || input.Runtime.Workspace == nil {
		return false, policy.Decision{}
	}
	b, state, err := workspace.Load(root)
	if err != nil || b == nil {
		return false, policy.Decision{}
	}
	for _, e := range b.Executions {
		if e.AgentID != input.EffectiveAgentID() || e.RuntimeID != workspace.RuntimeID(state) || e.BaselineGeneration != workspace.Generation(state) || e.Purpose == workspace.DocumentReviewPurpose {
			continue
		}
		blocked := func(reason string) (bool, policy.Decision) {
			return true, policy.Decision{Decision: "deny", RuleID: "workspace_worker_stop_pending", Reason: reason, Recovery: []string{"inspect your original execution and report; Main owns integration and other responsibilities"}}
		}
		cwd, err := workspace.Canonical(input.CWD)
		if err != nil || cwd != e.Path || e.Status != "ready" {
			return blocked("Worker Stop must identify its exact ready registered execution root")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = b.ValidateExecution(ctx, e, false)
		cancel()
		if err != nil {
			return blocked(err.Error())
		}
		loaded, err := hookctx.LoadFull(root, e.AgentID)
		if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil {
			return blocked("Worker Stop cannot read its original assignment facts")
		}
		input.Event = "SubagentStop"
		if decision, denied := hook.StopIdleDecision(root, input); denied {
			return true, decision
		}
		return true, policy.Decision{Decision: "allow"}
	}
	return false, policy.Decision{}
}
