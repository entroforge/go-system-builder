package agentauth

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"

	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// ValidateWorkingAuthorization reuses the existing dispatch validators for
// domain adapters. It is read-only and safe to call inside a Writer Apply.
func ValidateWorkingAuthorization(root string, state map[string]any, owner string) error {
	return validatePlanAuthorization(root, state, owner, "working")
}

// ValidateReportedPlanAuthorization verifies the original activated PLAN chain
// for a reported Agent. Callers must separately verify its accepted completion
// report; this helper grants no delivery or completion permission.
func ValidateReportedPlanAuthorization(root string, state map[string]any, owner string) error {
	return validatePlanAuthorization(root, state, owner, "reported")
}

// ValidateBlockedPlanAuthorization validates the original PLAN/activation
// chain for a blocked Agent. It grants no Stop, blocker-resolution, or write
// authority; callers must separately validate the original blocker receipt.
// This is read-only and deliberately checks the real blocked state without
// cloning or rewriting it to appear working.
func ValidateBlockedPlanAuthorization(root string, state map[string]any, owner string) error {
	return validatePlanAuthorization(root, state, owner, "blocked")
}

func validatePlanAuthorization(root string, state map[string]any, owner, requiredState string) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		if a["id"] != owner {
			continue
		}
		if workstate.AgentActivity(a) != requiredState {
			return fmt.Errorf("Agent %s has not reached the original %s state", owner, requiredState)
		}
		if a["dispatch_mode"] == "plan_checkpoint" {
			ref, err := plancheckpoint.ActivatedRef(root, runtime.Snapshot{State: state}, owner)
			if err != nil {
				return err
			}
			if ref == "" {
				return fmt.Errorf("Agent %s has no validated activated plan", owner)
			}
			return nil
		}
		// Approval-mode and legacy agents retain their original readback hash
		// chain. A one_shot dispatch cannot manufacture this working state.
		if a["dispatch_mode"] != nil && a["dispatch_mode"] != "" && a["dispatch_mode"] != "plan_approval_required" {
			return fmt.Errorf("document submission requires a registered plan/activation chain")
		}
		ref, _ := a["activation_ref"].(string)
		data, err := workpacket.ReadFile(root, ref, 1<<20)
		if err != nil {
			return err
		}
		if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
			return err
		}
		var activation Message
		if err := json.Unmarshal(data, &activation); err != nil {
			return err
		}
		if activation.MessageType != "activation" || activation.AgentID != owner || activation.RuntimeID != state["runtime_id"] {
			return fmt.Errorf("activation does not belong to this Agent")
		}
		readbackRef, _ := a["readback_ref"].(string)
		readback, err := workpacket.ReadFile(root, readbackRef, 1<<20)
		if err != nil {
			return err
		}
		if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", readback); err != nil {
			return err
		}
		var prior Message
		if err := json.Unmarshal(readback, &prior); err != nil {
			return err
		}
		if prior.MessageType != "readback_response" || prior.AgentID != owner || prior.RuntimeID != state["runtime_id"] || prior.TaskID != activation.TaskID {
			return fmt.Errorf("readback does not belong to this activation")
		}
		return VerifyActivationReadbackChain(root, a, activation)
	}
	return fmt.Errorf("Agent %s is not registered", owner)
}
