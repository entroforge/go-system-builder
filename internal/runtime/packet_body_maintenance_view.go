package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Maintenance restores each subtype's original delivery representation. The
// immutable authority remains the complete packet body, never this view.
func originalPacketMaintenanceView(root string, packet any) (any, error) {
	mainRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	mainRoot, err = filepath.EvalSymlinks(mainRoot)
	if err != nil {
		return nil, err
	}
	project := func(read []workpacket.ReadItem) []workpacket.ReadItem {
		result := append([]workpacket.ReadItem(nil), read...)
		for i := range result {
			if !filepath.IsAbs(result[i].Path) {
				result[i].Path = filepath.Join(mainRoot, filepath.FromSlash(result[i].Path))
			}
		}
		return result
	}
	switch p := packet.(type) {
	case workpacket.ReviewResultPacket:
		return p, nil
	case workpacket.RepairPlanReportPacket:
		display := p.Draft
		display.Read = project(display.Read)
		return display, nil
	case workpacket.RepairResultPacket:
		display := p.Draft
		display.Read = project(display.Read)
		return display, nil
	default:
		return nil, errors.New("unsupported original packet maintenance view")
	}
}

func maintainOriginalPacketView(ctx context.Context, root string, row map[string]any, packet any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	display, err := originalPacketMaintenanceView(root, packet)
	if err != nil {
		return err
	}
	view, err := json.MarshalIndent(display, "", "  ")
	if err != nil {
		return err
	}
	view = append(view, '\n')
	// This is an already registered delivery path. Preserve any occupant and
	// let the original authoring flow continue; the packet body remains the
	// authority, not a byte comparison against the view.
	_, err = workpacket.PublishNewBounded(root, stringValue(row["view_path"]), view, packetbody.Limit)
	if err != nil {
		return err
	}
	return ctx.Err()
}
