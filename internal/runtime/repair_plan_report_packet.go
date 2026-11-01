package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const repairPlanReportPacketTemplateID = "s9-plan-report-v1"

// Builders may be dispatched while a sibling is already fixing; the original
// issuer, current authority and registration/issue proofs share this scope.
func repairPlanReportDispatchPhase(phase any) bool {
	return phase == "planning" || phase == "reproducing" || phase == "fixing" || phase == "targeted_reverification" || phase == "ready_for_full_review"
}

// RepairPlanReportPackets reads the existing packet registry. It does not
// grant permission to answer or derive a repair plan report from packet inputs.
func RepairPlanReportPackets(state map[string]any) ([]workpacket.RepairPlanReportPacket, error) {
	return RepairPlanReportPacketsContext(context.Background(), state)
}

func RepairPlanReportPacketsContext(ctx context.Context, state map[string]any) ([]workpacket.RepairPlanReportPacket, error) {
	return repairPlanReportPacketsMatchingContext(ctx, state, nil)
}

func FindRepairPlanReportPacket(state map[string]any, id string) (workpacket.RepairPlanReportPacket, bool, error) {
	return FindRepairPlanReportPacketContext(context.Background(), state, id)
}

func FindRepairPlanReportPacketContext(ctx context.Context, state map[string]any, id string) (workpacket.RepairPlanReportPacket, bool, error) {
	if id == "" {
		return workpacket.RepairPlanReportPacket{}, false, fmt.Errorf("S9 packet ID is required")
	}
	rows, err := repairPacketRowsContext(ctx, state, repairPlanReportPacketTemplateID)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, false, err
	}
	for _, row := range rows {
		if row["packet_id"] == id {
			packet, err := readRepairPlanReportRowContext(ctx, row)
			return packet, err == nil, err
		}
	}
	return workpacket.RepairPlanReportPacket{}, false, nil
}

// CurrentRepairPlanReportPackets selects current responsibilities, not write grants.
func CurrentRepairPlanReportPackets(root string, state map[string]any, owner string) ([]workpacket.RepairPlanReportPacket, error) {
	return CurrentRepairPlanReportPacketsContext(context.Background(), root, state, owner)
}

func CurrentRepairPlanReportPacketsContext(ctx context.Context, root string, state map[string]any, owner string) ([]workpacket.RepairPlanReportPacket, error) {
	ctx = controlproof.WithJournalReadCache(WithPacketBodyRoot(ctx, root))
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation, generationErr := integerField(baseline, "generation")
	selected, err := repairPlanReportPacketsMatchingContext(ctx, state, func(b workpacket.RepairPlanReportBinding) bool {
		return (generationErr != nil || generation <= 0 || b.BaselineGeneration == generation) &&
			(owner == "" || b.OwnerAgentID == owner) && b.RuntimeID == state["runtime_id"] && pointer["session_id"] == b.SessionID && pointer["plan_ref"] == b.PlanRef && owners[b.RepairAssignmentID] == b.OwnerAgentID
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range selected {
		if seen[p.Binding.RepairAssignmentID] {
			return nil, fmt.Errorf("S9 current Assignment has multiple issued packets")
		}
		seen[p.Binding.RepairAssignmentID] = true
	}
	return selected, nil
}

func VerifyRepairPlanReportPacketIssue(root string, state map[string]any, p workpacket.RepairPlanReportPacket) error {
	return VerifyRepairPlanReportPacketIssueContext(context.Background(), root, state, p)
}
func VerifyRepairPlanReportPacketIssueContext(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket) error {
	return verifyRepairPlanReportPacketIssue(root, state, p, controlproof.JournalInspection{}, ctx)
}

func verifyRepairPlanReportPacketIssue(root string, state map[string]any, p workpacket.RepairPlanReportPacket, j controlproof.JournalInspection, contexts ...context.Context) error {
	stored, found, err := FindRepairPlanReportPacketContext(repairPacketContext(root, contexts...), state, p.ID)
	if err != nil {
		return fmt.Errorf("S9 packet registered row: %w", err)
	}
	if !found || !reflect.DeepEqual(stored, p) {
		return fmt.Errorf("S9 packet is not exact registered row")
	}
	return verifyRegisteredRepairPlanReportPacketIssue(root, state, p, j, contexts...)
}

// verifyRegisteredRepairPlanReportPacketIssue is used only after this call's
// immutable state row has been decoded and canonically validated by Find.
// It avoids rebuilding that same registry twice, while retaining all original
// journal, approval and on-disk source checks at the issuance revision.
func verifyRegisteredRepairPlanReportPacketIssue(root string, state map[string]any, p workpacket.RepairPlanReportPacket, _ controlproof.JournalInspection, contexts ...context.Context) error {
	if err := repairPacketContext(root, contexts...).Err(); err != nil {
		return err
	}
	if state["runtime_id"] != p.Binding.RuntimeID {
		return fmt.Errorf("S9 packet belongs to another Runtime")
	}
	return nil
}

func VerifyRepairPlanReportSources(root string, state map[string]any, p workpacket.RepairPlanReportPacket, _ int, contexts ...context.Context) error {
	if err := repairPacketContext(root, contexts...).Err(); err != nil {
		return err
	}
	if state["runtime_id"] != p.Binding.RuntimeID {
		return fmt.Errorf("S9 packet belongs to another Runtime")
	}
	return nil
}

func readRepairPacketSource(root, ref, schemaName string) (map[string]any, error) {
	data, err := workpacket.ReadFile(root, ref, 20<<20)
	if err != nil {
		return nil, fmt.Errorf("S9 source %s unavailable: %w", ref, err)
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	if schemaName != "" {
		if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, data); err != nil {
			return nil, err
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, fmt.Errorf("S9 source %s must be object", ref)
	}
	return doc, nil
}
func verifyRepairPlanReportSources(root string, state map[string]any, p workpacket.RepairPlanReportPacket, _ controlproof.JournalInspection, _ int, contexts ...context.Context) error {
	return VerifyRepairPlanReportSources(root, state, p, 0, contexts...)
}

func VerifyRepairPlanReportCurrent(root string, state map[string]any, p workpacket.RepairPlanReportPacket, contexts ...context.Context) error {
	if err := repairPacketContext(root, contexts...).Err(); err != nil {
		return err
	}
	b := p.Binding
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	if state["runtime_id"] != b.RuntimeID || pointer["session_id"] != b.SessionID || owners[b.RepairAssignmentID] != b.OwnerAgentID {
		return fmt.Errorf("S9 original responsibility is not current")
	}
	return nil
}
