package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

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
	// PublishBounded preserves an existing regular view verbatim; only a
	// missing view receives the original subtype's exact delivery projection.
	existing, err := workpacket.PublishBounded(root, stringValue(row["view_path"]), view, packetbody.Limit)
	if err != nil {
		return err
	}
	if bytes.Equal(existing, view) {
		return ctx.Err()
	}
	if err := validateOriginalPacketMaintenanceView(ctx, existing, packet, display); err != nil {
		return fmt.Errorf("original packet view conflicts during maintenance %s: %w", stringValue(row["packet_id"]), err)
	}
	return ctx.Err()
}

func validateOriginalPacketMaintenanceView(ctx context.Context, data []byte, packet, display any) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	if _, external := row["storage_format"]; external {
		return errors.New("issued view cannot replace original content with a body reference")
	}
	if _, external := row["body_ref"]; external {
		return errors.New("issued view has an unexpected body reference")
	}
	if _, complete := row["packet_id"]; complete {
		// Earlier framework consumers may have delivered the full envelope.
		// Its closed canonical packet must be exactly the original authority.
		observed, err := canonicalPacketBodyRowContext(ctx, row)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(observed, packet) {
			return nil
		}
		return errors.New("issued view differs from its original packet")
	}
	// S9's original professional view is the complete frozen Draft with
	// Read paths projected to the exact original Main root. Strict typed
	// decoding rejects extra fields; equality includes questions and the
	// original default answer, not the private author-editable answer file.
	var observed any
	switch display.(type) {
	case workpacket.RepairPlanReportDraft:
		var draft workpacket.RepairPlanReportDraft
		if err := json.Unmarshal(data, &draft); err != nil {
			return err
		}
		observed = draft
	case workpacket.RepairResultDraft:
		var draft workpacket.RepairResultDraft
		if err := json.Unmarshal(data, &draft); err != nil {
			return err
		}
		observed = draft
	default:
		return errors.New("issued S7 view is missing its complete original packet")
	}
	if !reflect.DeepEqual(observed, display) {
		return errors.New("issued professional view differs from its original questions, reads or default answer")
	}
	return ctx.Err()
}
