package hook

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
)

// The systemMessage recovery packet is the only channel agents reliably read
// without parsing JSON; an UNKNOWN verdict must carry its conflicts verbatim
// (2026-08-28 S10 walkthrough: a fully-coached manifest conflict was invisible
// because only missing[] was rendered).
func TestPreToolUseUnknownPacketIncludesConflicts(t *testing.T) {
	qg := controller.QualityGateResult{
		Status:              controller.StatusUnknown,
		GateID:              "GATE-ACCEPTANCE-COMPLETE",
		CandidateTransition: "TR-015",
		ErrorCode:           qualitygate.ErrorGateUnknown,
		Conflicts:           []string{"s10:acceptance_manifest:x:evidence_ref_missing:y"},
	}
	packet := formatPreToolUseRecoveryPacket(policy.Decision{Decision: "allow"}, qg)
	if !strings.Contains(packet, "Conflicts: s10:acceptance_manifest:x:evidence_ref_missing:y") {
		t.Fatalf("packet missing conflict text: %q", packet)
	}
	if !strings.Contains(packet, "LOOP_GATE_UNKNOWN") {
		t.Fatalf("packet lost error code: %q", packet)
	}
}
