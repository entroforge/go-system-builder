package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// s9BriefObservation belongs to the current Hook event; it does not establish
// professional reception or own another state machine.
type s9BriefObservation struct {
	Context   string
	Attempted bool
}

// observeMainS9Brief restores the display at the original issued reference.
func observeMainS9Brief(root string, request policy.Input) s9BriefObservation {
	if request.Event != "PreToolUse" || !mainQuestionnaireViewer(request) {
		return s9BriefObservation{}
	}
	ctx := contextForHook(request)
	reader, ok := ctx.Value(hookReaderKey{}).(*hookReader)
	if !ok || reader.Kind != "Main" || reader.snapshotErr != nil || reader.snapshot.State == nil {
		return s9BriefObservation{} // The original routing path owns Runtime failures.
	}
	snap := reader.snapshot
	if !runtime.PacketEnabled(snap.State) || !repairDispatchCurrent(snap.State) {
		return s9BriefObservation{}
	}
	routes, err := nativeS9Routes(ctx, snap.State)
	if err != nil {
		return s9BriefObservation{Context: fmt.Sprintf("S9 original responsibility observation is unavailable: %v. Preserve all accepted work; Main/system owns the original source gap.", err)}
	}
	var lines []string
	attempted := false
	for _, route := range routes {
		if ctx.Err() != nil {
			lines = append(lines, "S9 original brief preparation was interrupted; preserve accepted work and original sources.")
			break
		}
		brief := nativeS9RouteBrief(route)
		if brief == "" {
			lines = append(lines, fmt.Sprintf("S9 packet for %s has no issued brief reference; Main/system owns this issuance gap.", route.Binding.OwnerAgentID))
			continue
		}
		briefPath := filepath.Join(root, filepath.FromSlash(brief))
		if _, err := os.Stat(briefPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			lines = append(lines, fmt.Sprintf("S9 brief for %s is unavailable at %s: %v. Main/system owns the original source gap.", route.Binding.OwnerAgentID, briefPath, err))
			continue
		}
		p, readErr := nativeS9RoutePacket(ctx, root, snap.State, route)
		if readErr != nil {
			lines = append(lines, fmt.Sprintf("S9 original responsibility for %s at control root %s, brief %s is unconfirmed: %v. Preserve accepted work; Main and framework maintainers own this source fault.", route.Binding.OwnerAgentID, root, briefPath, readErr))
			continue
		}
		// Consult the original Result consumer only for a missing display. Its
		// read-only snapshot reports pending/errors without recovering a Writer
		// transaction or treating the Plan receipt as Result acceptance.
		service, receiptErr := submission.NewContext(ctx, root, true)
		var resultContext string
		if receiptErr == nil {
			var packets []workpacket.RepairResultPacket
			packets, receiptErr = runtime.CurrentRepairResultPacketsContext(ctx, root, snap.State, route.Binding.OwnerAgentID)
			for _, packet := range packets {
				if receiptErr != nil || packet.Binding.Sources.RepairAssignmentID != p.Binding.RepairAssignmentID {
					continue
				}
				var receipt submission.Receipt
				receipt, receiptErr = service.RepairResultReceipt(route.Binding.OwnerAgentID, packet.ID)
				if receiptErr == nil && receipt.Accepted && (receipt.ResponsibilityStatus == "candidate_accepted" || strings.HasPrefix(receipt.ResponsibilityStatus, "main_result_accepted")) {
					resultContext = fmt.Sprintf("S9 Result for %s is already received. %s", route.Binding.OwnerAgentID, receipt.NextAction)
				}
			}
		}
		if receiptErr != nil {
			lines = append(lines, fmt.Sprintf("S9 Result receipt for %s is unconfirmed: %v. Preserve accepted work; Main/system owns the original source gap.", route.Binding.OwnerAgentID, receiptErr))
			continue
		}
		if resultContext != "" {
			lines = append(lines, resultContext)
			continue
		}
		// Check existing professional facts before preparing display material.
		_, accepted, receiptErr := runtime.VerifiedRepairPlanReportReceipt(root, snap.State, p.ID, route.Binding.OwnerAgentID, ctx)
		if receiptErr != nil {
			lines = append(lines, fmt.Sprintf("S9 receipt for %s is unconfirmed: %v. Preserve any accepted work and original sources.", route.Binding.OwnerAgentID, receiptErr))
			continue
		}
		if accepted {
			lines = append(lines, fmt.Sprintf("S9 PlanReport for %s is already received; a missing brief does not require the Builder to repeat it.", route.Binding.OwnerAgentID))
		}
		attempted = true
		message, err := restoreNativeS9Brief(ctx, root, p)
		if err != nil {
			lines = append(lines, fmt.Sprintf("S9 original preparation for %s remains incomplete at brief %s: %v. Existing registration and any accepted work remain; Main/system owns the original source gap.", route.Binding.OwnerAgentID, briefPath, err))
			continue
		}
		lines = append(lines, message)
	}
	return s9BriefObservation{Context: strings.Join(lines, "\n"), Attempted: attempted}
}
