package runtime

import "testing"

func TestRepairResultResumePendingRequiresOriginalResolutionBeforeExecutionLookup(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	// An ordinary completed execution may have moved to original history.
	// Missing current execution is not evidence of a Main resolution.
	f.plan.state["workspace"].(map[string]any)["executions"] = map[string]any{}
	b := f.packet.Binding.Sources
	pending, err := RepairResultResumePending(f.plan.root, f.plan.state, b.OwnerAgentID, b.RepairAssignmentID)
	if err != nil || pending {
		t.Fatalf("missing execution invented a pending Resume without a resolution: pending=%t err=%v", pending, err)
	}
}

func TestRepairResultResumePendingUsesVerifiedCurrentResolution(t *testing.T) {
	f := newRepairResultBlockerReaderFixture(t)
	attachRepairResultResolutionFixture(t, &f, nil)
	b := f.runtime.packet.Binding.Sources
	pending, err := RepairResultResumePending(f.runtime.plan.root, f.runtime.plan.state, b.OwnerAgentID, b.RepairAssignmentID)
	if err != nil || !pending {
		t.Fatalf("verified current resolution lost its new-attempt requirement: pending=%t err=%v", pending, err)
	}
}
