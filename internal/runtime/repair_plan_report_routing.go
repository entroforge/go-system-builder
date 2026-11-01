package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairPlanReportRoute is a light registered coordinate, never an issued body
// or a reception proof. Consumers must read and verify the selected packet.
type RepairPlanReportRoute struct {
	ID         string
	BriefPath  string
	ViewPath   string
	AnswerPath string
	Binding    workpacket.RepairPlanReportBinding
}

// CurrentRepairPlanReportRoutesContext applies the original current binding
// selection without opening unrelated packet bodies.
func CurrentRepairPlanReportRoutesContext(ctx context.Context, state map[string]any) ([]RepairPlanReportRoute, error) {
	rows, err := repairPacketRowsContext(ctx, state, repairPlanReportPacketTemplateID)
	if err != nil {
		return nil, err
	}
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	owners, _ := pointer["assignment_owners"].(map[string]any)
	routes := []RepairPlanReportRoute{}
	seen := map[string]bool{}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var b workpacket.RepairPlanReportBinding
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return nil, err
		}
		if err := decodeRepairPacketBinding(data, &b); err != nil {
			return nil, err
		}
		if b.RuntimeID != state["runtime_id"] || pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef || owners[b.RepairAssignmentID] != b.OwnerAgentID {
			continue
		}
		if seen[b.RepairAssignmentID] {
			return nil, fmt.Errorf("S9 current Assignment has multiple issued packets")
		}
		seen[b.RepairAssignmentID] = true
		view, answer := stringValue(row["view_path"]), stringValue(row["answer_path"])
		if answer == "" {
			return nil, fmt.Errorf("S9 packet %s has missing routing paths", stringValue(row["packet_id"]))
		}
		routes = append(routes, RepairPlanReportRoute{ID: stringValue(row["packet_id"]), BriefPath: stringValue(row["brief_path"]), ViewPath: view, AnswerPath: answer, Binding: b})
	}
	return routes, ctx.Err()
}
