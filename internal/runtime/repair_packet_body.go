package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func repairPacketContext(root string, contexts ...context.Context) context.Context {
	ctx := context.Background()
	if len(contexts) != 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	return WithPacketBodyRoot(ctx, root)
}

// Validate every light identity/format before selecting a body. Historical
// body availability is checked when that exact history is requested, never by
// silently rebuilding its questions from the current template.
func repairPacketRowsContext(ctx context.Context, state map[string]any, template string) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entities, _ := state["entities"].(map[string]any)
	raw := entities["work_packets"]
	if raw == nil {
		return nil, nil
	}
	rows, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("Runtime work_packets is not an array")
	}
	seen := map[string]bool{}
	selected := []map[string]any{}
	for i, raw := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, ok := raw.(map[string]any)
		id := stringValue(row["packet_id"])
		if !ok || id == "" || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate Runtime work_packets[%d] identity", i)
		}
		seen[id] = true
		_, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil {
			return nil, err
		}
		if external {
			if err := validatePacketBodyIndexRow(row); err != nil {
				return nil, err
			}
		}
		if row["template_id"] != template {
			continue
		}

		if !external {
			allowed := map[string]bool{"packet_id": true, "template_id": true, "template_sha256": true, "binding": true, "inputs": true, "report_id": template == repairPlanReportPacketTemplateID, "result_id": template == repairResultPacketTemplateID, "issuance_event_id": true, "view_path": true, "answer_path": true, "draft": true}
			for key := range row {
				if !allowed[key] {
					return nil, fmt.Errorf("unknown S9 packet field %q", key)
				}
			}
		}
		selected = append(selected, row)
	}
	sort.Slice(selected, func(i, j int) bool {
		return stringValue(selected[i]["packet_id"]) < stringValue(selected[j]["packet_id"])
	})
	return selected, nil
}

func canonicalRepairPlanReportRowContext(ctx context.Context, row map[string]any) (workpacket.RepairPlanReportPacket, error) {
	var packet workpacket.RepairPlanReportPacket
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	body, err := packetbody.Read(ctx, row)
	if err != nil {
		return packet, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return packet, err
	}
	if err := json.Unmarshal(data, &packet); err != nil {
		return packet, err
	}
	rebuilt, err := workpacket.RebuildRepairPlanReport(packet)
	if err != nil || !reflect.DeepEqual(rebuilt, packet) {
		return packet, fmt.Errorf("S9 PlanReport packet differs from original canonical body: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	return packet, nil
}

func canonicalRepairResultRowContext(ctx context.Context, row map[string]any) (workpacket.RepairResultPacket, error) {
	var packet workpacket.RepairResultPacket
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	body, err := packetbody.Read(ctx, row)
	if err != nil {
		return packet, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return packet, err
	}
	if err := json.Unmarshal(data, &packet); err != nil {
		return packet, err
	}
	rebuilt, err := workpacket.RebuildRepairResult(packet)
	if err != nil || !reflect.DeepEqual(rebuilt, packet) {
		return packet, fmt.Errorf("S9 Result packet differs from original canonical body: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	return packet, nil
}

func repairPlanReportPacketsMatchingContext(ctx context.Context, state map[string]any, match func(workpacket.RepairPlanReportBinding) bool) ([]workpacket.RepairPlanReportPacket, error) {
	rows, err := repairPacketRowsContext(ctx, state, repairPlanReportPacketTemplateID)
	if err != nil {
		return nil, err
	}
	packets := []workpacket.RepairPlanReportPacket{}
	for _, row := range rows {
		var binding workpacket.RepairPlanReportBinding
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return nil, err
		}
		if err := decodeRepairPacketBinding(data, &binding); err != nil {
			return nil, err
		}
		if match != nil && !match(binding) {
			continue
		}
		packet, err := readRepairPlanReportRowContext(ctx, row)
		if err != nil {
			return nil, err
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

// Select by retained binding first, then include the exact predecessor/child
// graph needed to reject a missing, cross-source or forked Resume lineage.
func repairResultPacketsMatchingContext(ctx context.Context, state map[string]any, match func(workpacket.RepairResultBinding) bool) ([]workpacket.RepairResultPacket, error) {
	rows, err := repairPacketRowsContext(ctx, state, repairResultPacketTemplateID)
	if err != nil {
		return nil, err
	}
	bindings := map[string]workpacket.RepairResultBinding{}
	needed := map[string]bool{}
	for _, row := range rows {
		var binding workpacket.RepairResultBinding
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return nil, err
		}
		if err := decodeRepairPacketBinding(data, &binding); err != nil {
			return nil, err
		}
		id := stringValue(row["packet_id"])
		bindings[id] = binding
		needed[id] = match == nil || match(binding)
	}
	for changed := true; changed; {
		changed = false
		for id, binding := range bindings {
			if binding.Resume == nil {
				continue
			}
			parent := binding.Resume.PredecessorPacketID
			if needed[id] && !needed[parent] {
				needed[parent] = true
				changed = true
			}
			if needed[parent] && !needed[id] {
				needed[id] = true
				changed = true
			}
		}
	}
	packets := []workpacket.RepairResultPacket{}
	for _, row := range rows {
		if !needed[stringValue(row["packet_id"])] {
			continue
		}
		packet, err := readRepairResultRowContext(ctx, row)
		if err != nil {
			return nil, err
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func repairResultPacketsForBindingContext(ctx context.Context, state map[string]any, binding workpacket.RepairResultBinding) ([]workpacket.RepairResultPacket, error) {
	return repairResultPacketsMatchingContext(ctx, state, func(candidate workpacket.RepairResultBinding) bool {
		return sameRepairResultResumeSlot(candidate, binding)
	})
}

// RepairResultPacketsForSourcesContext reads retained attempts for the current
// source tuple, including resolved predecessors needed by original dispatch.
// It supplies no current execution or reception authority.
func RepairResultPacketsForSourcesContext(ctx context.Context, root string, state map[string]any, owner string) ([]workpacket.RepairResultPacket, error) {
	ctx = WithPacketBodyRoot(ctx, root)
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	gen, _ := integerField(baseline, "generation")
	return repairResultPacketsMatchingContext(ctx, state, func(binding workpacket.RepairResultBinding) bool {
		b := binding.Sources
		return (owner == "" || b.OwnerAgentID == owner) && b.RuntimeID == state["runtime_id"] && b.BaselineGeneration == gen && pointer["session_id"] == b.SessionID && pointer["plan_ref"] == b.PlanRef
	})
}

// RepairPlanReportPacketsForSourcesContext retains original owners for issue
// collision checks; it does not apply today's dispatch-owner selector.
func RepairPlanReportPacketsForSourcesContext(ctx context.Context, root string, state map[string]any, owner string) ([]workpacket.RepairPlanReportPacket, error) {
	ctx = WithPacketBodyRoot(ctx, root)
	review, _ := state["review"].(map[string]any)
	pointer, _ := review["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	gen, _ := integerField(baseline, "generation")
	return repairPlanReportPacketsMatchingContext(ctx, state, func(b workpacket.RepairPlanReportBinding) bool {
		return (owner == "" || b.OwnerAgentID == owner) && b.RuntimeID == state["runtime_id"] && b.BaselineGeneration == gen && pointer["session_id"] == b.SessionID && pointer["plan_ref"] == b.PlanRef
	})
}

func decodeRepairPacketBinding(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

// RepairResultPacketsForOwnerContext selects only the named owner's retained
// history. Callers still compare its original registered slot themselves.
func RepairResultPacketsForOwnerContext(ctx context.Context, state map[string]any, owner string) ([]workpacket.RepairResultPacket, error) {
	return repairResultPacketsMatchingContext(ctx, state, func(binding workpacket.RepairResultBinding) bool {
		return owner == "" || binding.Sources.OwnerAgentID == owner
	})
}

// Old inline records retain their original strict typed decoding contract.
// Newly externalized bodies additionally prove their original canonical form.
func readRepairPlanReportRowContext(ctx context.Context, row map[string]any) (workpacket.RepairPlanReportPacket, error) {
	_, external, err := packetbody.ReferenceFromIndex(row)
	if err != nil {
		return workpacket.RepairPlanReportPacket{}, err
	}
	if external {
		return canonicalRepairPlanReportRowContext(ctx, row)
	}
	var packet workpacket.RepairPlanReportPacket
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	data, err := json.Marshal(row)
	if err != nil {
		return packet, err
	}
	if err := json.Unmarshal(data, &packet); err != nil {
		return packet, err
	}
	return packet, ctx.Err()
}
func readRepairResultRowContext(ctx context.Context, row map[string]any) (workpacket.RepairResultPacket, error) {
	_, external, err := packetbody.ReferenceFromIndex(row)
	if err != nil {
		return workpacket.RepairResultPacket{}, err
	}
	if external {
		return canonicalRepairResultRowContext(ctx, row)
	}
	var packet workpacket.RepairResultPacket
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	data, err := json.Marshal(row)
	if err != nil {
		return packet, err
	}
	if err := json.Unmarshal(data, &packet); err != nil {
		return packet, err
	}
	return packet, ctx.Err()
}

// Path selection uses only the retained exact answer/view coordinates. The
// original consumer still binds an absolute path to its registered source root.
func repairPacketPathMatches(row map[string]any, input string, result bool) bool {
	clean := filepath.ToSlash(filepath.Clean(input))
	for _, key := range []string{"answer_path", "view_path"} {
		candidate := stringValue(row[key])
		if candidate != "" && (clean == candidate || strings.HasSuffix(clean, "/"+candidate)) {
			return true
		}
	}
	if result {
		answer := stringValue(row["answer_path"])
		if strings.HasSuffix(answer, "/answer.json") {
			resolution := strings.TrimSuffix(answer, "answer.json") + "main-resolution.json"
			if clean == resolution || strings.HasSuffix(clean, "/"+resolution) {
				return true
			}
		}
	}
	return false
}
func RepairPlanReportPacketsForPathContext(ctx context.Context, state map[string]any, input string) ([]workpacket.RepairPlanReportPacket, error) {
	rows, err := repairPacketRowsContext(ctx, state, repairPlanReportPacketTemplateID)
	if err != nil {
		return nil, err
	}
	packets := []workpacket.RepairPlanReportPacket{}
	for _, row := range rows {
		if repairPacketPathMatches(row, input, false) {
			packet, err := readRepairPlanReportRowContext(ctx, row)
			if err != nil {
				return nil, err
			}
			packets = append(packets, packet)
		}
	}
	return packets, ctx.Err()
}
func RepairResultPacketsForPathContext(ctx context.Context, state map[string]any, input string) ([]workpacket.RepairResultPacket, error) {
	rows, err := repairPacketRowsContext(ctx, state, repairResultPacketTemplateID)
	if err != nil {
		return nil, err
	}
	packets := []workpacket.RepairResultPacket{}
	for _, row := range rows {
		if repairPacketPathMatches(row, input, true) {
			packet, err := readRepairResultRowContext(ctx, row)
			if err != nil {
				return nil, err
			}
			packets = append(packets, packet)
		}
	}
	return packets, ctx.Err()
}
