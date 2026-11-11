package repair

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The packet Writer owns these system-generated Main materials. They are
// published before issuance, and their actual paths enter that same packet.
func prepareIssuedPlanQuestionnaire(ctx context.Context, root string, packet *workpacket.RepairPlanReportPacket) error {
	mainRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	display := packet.Draft
	display.Read = append([]workpacket.ReadItem(nil), packet.Draft.Read...)
	for i := range display.Read {
		if !filepath.IsAbs(display.Read[i].Path) {
			display.Read[i].Path = filepath.Join(mainRoot, filepath.FromSlash(display.Read[i].Path))
		}
	}
	data, err := json.MarshalIndent(display, "", "  ")
	if err != nil {
		return err
	}
	view, viewErr := workpacket.PublishS9Questionnaire(ctx, root, packet.ViewPath, append(data, '\n'), repairPlanReportAnswerLimit)
	if viewErr == nil {
		packet.ViewPath = view
	} else {
		packet.ViewPath = ""
	}
	data, err = json.MarshalIndent(packet.Draft, "", "  ")
	if err != nil {
		return err
	}
	answer, answerErr := workpacket.PublishS9Questionnaire(ctx, root, packet.AnswerPath, append(data, '\n'), repairPlanReportAnswerLimit)
	if answerErr == nil {
		packet.AnswerPath = answer
	}
	return ctx.Err()
}
