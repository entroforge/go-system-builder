package runtime

import (
	"context"
	"testing"
)

// Selecting current work must not claim an older generation, while an exact
// packet lookup remains available for the original historical responsibility.
func TestS9CurrentSelectionKeepsExactHistoricalLookup(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	externalizeS9FixtureRows(t, f.plan.root, f.plan.state)
	baseline := f.plan.state["baseline"].(map[string]any)
	baseline["generation"] = 3
	ctx := WithPacketBodyRoot(context.Background(), f.plan.root)
	if _, found, err := FindRepairPlanReportPacketContext(ctx, f.plan.state, f.plan.packet.ID); err != nil || !found {
		t.Fatalf("original historical Plan packet was not available by ID: found=%v err=%v", found, err)
	}
	if _, found, err := FindRepairResultPacketContext(ctx, f.plan.state, f.packet.ID); err != nil || !found {
		t.Fatalf("original historical Result packet was not available by ID: found=%v err=%v", found, err)
	}
	if packets, err := CurrentRepairPlanReportPacketsContext(ctx, f.plan.root, f.plan.state, f.plan.packet.Binding.OwnerAgentID); err != nil || len(packets) != 0 {
		t.Fatalf("historical Plan claimed current responsibility: packets=%d err=%v", len(packets), err)
	}
	if packets, err := CurrentRepairResultPacketsContext(ctx, f.plan.root, f.plan.state, f.packet.Binding.Sources.OwnerAgentID); err != nil || len(packets) != 0 {
		t.Fatalf("historical Result claimed current responsibility: packets=%d err=%v", len(packets), err)
	}
}

// A missing current generation is not proof that an existing responsibility
// belongs elsewhere. Preserve the previous selection rather than adding a gate.
func TestS9CurrentSelectionDoesNotGuessMissingGeneration(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	externalizeS9FixtureRows(t, f.plan.root, f.plan.state)
	delete(f.plan.state["baseline"].(map[string]any), "generation")
	ctx := WithPacketBodyRoot(context.Background(), f.plan.root)
	if packets, err := CurrentRepairPlanReportPacketsContext(ctx, f.plan.root, f.plan.state, f.plan.packet.Binding.OwnerAgentID); err != nil || len(packets) != 1 || packets[0].ID != f.plan.packet.ID {
		t.Fatalf("missing generation changed Plan selection: packets=%v err=%v", packets, err)
	}
	if packets, err := CurrentRepairResultPacketsContext(ctx, f.plan.root, f.plan.state, f.packet.Binding.Sources.OwnerAgentID); err != nil || len(packets) != 1 || packets[0].ID != f.packet.ID {
		t.Fatalf("missing generation changed Result selection: packets=%v err=%v", packets, err)
	}
	if packets, err := CurrentRepairPlanReportPacketsContext(ctx, f.plan.root, f.plan.state, "different-owner"); err != nil || len(packets) != 0 {
		t.Fatalf("wrong owner acquired Plan responsibility: packets=%v err=%v", packets, err)
	}
	if packets, err := CurrentRepairResultPacketsContext(ctx, f.plan.root, f.plan.state, "different-owner"); err != nil || len(packets) != 0 {
		t.Fatalf("wrong owner acquired Result responsibility: packets=%v err=%v", packets, err)
	}
}
