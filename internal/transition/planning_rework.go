package transition

import (
	"fmt"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"strings"
)

func guardPlanningReworkScope(state map[string]any, _ map[string]string) error {
	life, _ := state["lifecycle"].(map[string]any)
	bound, _ := state["bound_req"].(map[string]any)
	if !loopruntime.PacketEnabled(state) || life["state"] != "planning" || (life["phase"] != "contracts" && life["phase"] != "tasks") || bound["status"] != "locked" {
		return fmt.Errorf("professional rework requires the current S3/S4 Main entry and original locked REQ")
	}
	return nil
}

func actionPlanningProfessionalRework(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	reason := strings.TrimSpace(stringValue(ctx.Params["reason"]))
	if ctx.Request == nil || ctx.Request.Actor != "orchestrator" || ctx.Request.ProducerResponsibility != "Main" || reason == "" || ctx.Request.RequestID == "" {
		return ActionResult{}, fmt.Errorf("professional rework requires the named Main request and a concrete reason")
	}
	if err := workspace.RequireMain(ctx.Root); err != nil {
		return ActionResult{}, err
	}
	if err := loopruntime.ApplyPlanningRework(ctx.Root, state, ctx.Spec.To, reason, ctx.EventID, ctx.Request.Files); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Status: "committed", MutationApplied: true, Detail: reason}, nil
}
