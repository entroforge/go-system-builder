package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// The existing bounded display cache records attempts, never reception facts.
// Packet identity isolates owners, rounds and revisions without reading drafts.
func nativeS7DiagnosticKind(packet string) string { return "S7 native Stop " + packet }

func nativeS7ReturnDiagnostic(root string, state map[string]any, p workpacket.ReviewResultPacket) string {
	return nativeS7IndexReturnDiagnostic(root, state, runtime.ReviewResultPacketIndex{ID: p.ID, Binding: p.Binding, ViewPath: p.ViewPath, AnswerPath: p.AnswerPath})
}

func nativeS7IndexReturnDiagnostic(root string, state map[string]any, p runtime.ReviewResultPacketIndex) string {
	return nativeS7IndexReturnDiagnosticContext(context.Background(), root, state, p)
}

func nativeS7IndexReturnDiagnosticContext(ctx context.Context, root string, state map[string]any, p runtime.ReviewResultPacketIndex) string {
	references := fmt.Sprintf("S7 owner %s; packet %s; view %s; answer %s. ", p.Binding.OwnerAgentID, p.ID, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath))
	if diagnostic := submission.PlanningDiagnosticContext(ctx, root, state, nativeS7DiagnosticKind(p.ID)); diagnostic != "" {
		return references + diagnostic + ". This is the last attempted reception, not a fresh validation or an accepted result. The original consumer owns interrupted transaction recovery; Main restores a named source/environment gap. Only a specific professional omission requires an author correction in this original responsibility. "
	}
	return references + "No original Stop diagnostic is recorded; check the actual native execution/return before deciding recovery. "
}
