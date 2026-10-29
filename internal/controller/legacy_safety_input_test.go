package controller

// Compatibility fixtures for the former tool-policy projection. Production
// graph listening does not build or evaluate this permission input.
import (
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// buildSafetyInput projects the controller request onto the minimal
// policy.Input contract. The Controller never re-reads the runtime here;
// it uses the already-committed snapshot revision so the safety verdict
// matches the cursor the agent just observed.
func buildSafetyInput(req ControlRequest, snapshot runtime.Snapshot, affected []string) policy.Input {
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	stateName, _ := lifecycle["state"].(string)
	phaseName, _ := lifecycle["phase"].(string)
	rt := policy.RuntimeContext{
		RuntimeID:    stringValue(snapshot.State["runtime_id"]),
		Revision:     snapshot.Revision,
		ProjectRoot:  req.Root,
		CurrentState: stateName,
		CurrentPhase: stringValue(phaseName),
		Agent:        req.Runtime.Agent,
		// Preserve trusted authoring grants from hookctx for the final safety
		// verdict. They are exact paths derived from the current execution and
		// packet; dropping them here makes the controller disagree with the
		// agent-scoped policy on legal Worker PLAN and activated answer writes.
		PacketMode:            req.Runtime.PacketMode,
		WorkPacketAnswerPaths: append([]string(nil), req.Runtime.WorkPacketAnswerPaths...),
		WorkerSubmissionPaths: append([]string(nil), req.Runtime.WorkerSubmissionPaths...),
		// RC-04 (S7-3): preserve the dispatched-Assignment facts so the
		// L4 first-write barrier stays awake on the controller safety path,
		// which evaluates policy without a hookctx-resolved AgentContext.
		AssignmentID:    req.Runtime.AssignmentID,
		PlanReportedRef: req.Runtime.PlanReportedRef,
		DispatchMode:    req.Runtime.DispatchMode,
	}
	if bound, ok := snapshot.State["bound_req"].(map[string]any); ok {
		rt.BoundREQID, _ = bound["id"].(string)
		rt.BoundREQPath, _ = bound["path"].(string)
		if meta, ok := bound["metadata"].(map[string]any); ok {
			rt.BoundREQUIImpact, _ = meta["ui_impact"].(string)
		}
	}
	// The locked-artifact screen was previously only alive in unit tests —
	// the wire path never threaded LockedArtifacts or the stage here, so
	// lockedArtifactDecision never ran (S5 final-round F1). Project them
	// with the same loader the hook transport uses.
	rt.LockedArtifacts = hookctx.LockedArtifactsFromSnapshot(snapshot)
	rt.CurrentStage = stageOf(snapshot.State)
	// The reviewer product-write rule allows the ReviewPlan's declared
	// verification artifact workspace (E2E cold-start spec/fixture surface).
	// Without this projection the wire path hard-denies the one write
	// surface a cold-start E2E Reviewer is supposed to use (L3-S7 §8).
	if reviewState, ok := snapshot.State["review"].(map[string]any); ok {
		if plan, ok := reviewState["plan"].(map[string]any); ok {
			rt.VerificationWorkspace, _ = plan["verification_artifact_workspace"].(string)
		}
	}
	if baseline, ok := snapshot.State["baseline"].(map[string]any); ok {
		rt.CurrentBaselineGeneration = int(baseline["generation"].(float64))
	}
	var bindingErr error
	rt.Workspace, bindingErr = workspace.Decode(snapshot.State)
	if bindingErr != nil {
		rt.WorkspaceError = bindingErr.Error()
	}
	return policy.Input{
		CWD:       req.CWD,
		SessionID: req.SessionID,
		Event:     req.Event,
		AgentID:   req.AgentID,
		ToolName:  req.ToolName,
		ToolInput: req.ToolInput,
		TargetID:  req.TargetID,
		Runtime:   rt,
	}
}

// stageOf reads the milestone projection's stage label.
func stageOf(state map[string]any) string {
	if milestone, ok := state["milestone"].(map[string]any); ok {
		if stage, ok := milestone["stage"].(string); ok {
			return stage
		}
	}
	return ""
}
