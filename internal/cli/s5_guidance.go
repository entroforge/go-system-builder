package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Current registered questions are sufficient for ordinary guidance. This is
// not S5Packets: it neither rebuilds the formal source closure nor issues work.
// The original receiver still checks the exact source and current validity.
func observedS5Packets(state map[string]any) ([]workpacket.Packet, error) {
	life := mapFieldCLI(state, "lifecycle")
	var packets []workpacket.Packet
	owners := map[string]bool{}
	for _, row := range objectSliceAny(mapFieldCLI(state, "entities")["work_packets"]) {
		if row["template_id"] != "s5-document-review-v1" {
			continue
		}
		p, found, err := runtime.FindWorkPacket(state, stringValue(row["packet_id"]))
		if err != nil {
			return nil, err
		}
		if !found || p.Binding.RuntimeID != state["runtime_id"] || p.Binding.Generation != tolerantInt(mapFieldCLI(state, "baseline")["generation"]) || p.Binding.StageEntry != tolerantInt(life["phase_revision"]) {
			continue
		}
		if receipt, received, err := runtime.FindSubmission(state, p.ID); err != nil {
			return nil, err
		} else if received && (receipt.RevisionPacketID != "" || receipt.SupersededBy != "") {
			continue
		}
		if owners[p.Binding.Owner] {
			return nil, fmt.Errorf("multiple current S5 questionnaires for %s; Main must resolve the original responsibility", p.Binding.Owner)
		}
		owners[p.Binding.Owner] = true
		packets = append(packets, p)
	}
	sort.Slice(packets, func(i, j int) bool { return packets[i].Binding.Owner < packets[j].Binding.Owner })
	return packets, nil
}

func observedS5Paths(root string, state map[string]any, p workpacket.Packet) (view, answer string, frozen bool, err error) {
	registry, err := workspace.Decode(state)
	if err != nil {
		return "", "", false, err
	}
	if registry != nil {
		e, declared, err := workspace.ReviewExecution(state, p)
		if err != nil {
			return "", "", declared, err
		}
		if declared {
			if e.Status != "ready" {
				return "", "", true, fmt.Errorf("registered independent execution is not ready; Main restores its original preparation")
			}
			paths, err := registry.WorkerSubmissionPaths(e)
			if err != nil {
				return "", "", true, err
			}
			return filepath.Join(e.Path, workpacket.WorkerViewPath), filepath.Join(e.Path, filepath.FromSlash(paths.Answer)), true, nil
		}
	}
	if p.Binding.ExecutionRequired {
		return "", "", true, fmt.Errorf("registered independent execution is missing; Main restores its original source, never Main authoring")
	}
	return filepath.Join(root, filepath.FromSlash(p.ViewPath)), filepath.Join(root, filepath.FromSlash(p.AnswerPath)), false, nil
}

func stageS5ObservedContext(root string, state map[string]any) string {
	packets, err := observedS5Packets(state)
	if err != nil {
		return "S5 original responsibility projection unavailable; Main owns recovery: " + err.Error()
	}
	if len(packets) == 0 {
		return stageS5PreparationGuidance(root, state)
	}
	var lines []string
	responsibilities := map[string]bool{}
	for _, p := range packets {
		responsibilities[p.Binding.Responsibility] = true
		prefix := fmt.Sprintf("S5 Reviewer %s/%s: ", p.Binding.Owner, p.Binding.Responsibility)
		receipt, received, err := runtime.FindSubmission(state, p.ID)
		if err != nil {
			lines = append(lines, prefix+"Main must inspect the original reception: "+err.Error())
			continue
		}
		if received {
			line := prefix + "original reception recorded as " + receipt.Key + "; keep the received judgment unchanged. Main owns peer dispatch and stage followup; this observation does not revalidate or receive the answer."
			if receipt.ControlStatus == "pending" {
				line += fmt.Sprintf(" Review answer accepted; do not submit again. Stage exit followup remains pending (system_followup %s): Main inspects the stage exit failure, then uses `loop-harness runtime stage-answer recover --root %s`; do not ask the reviewer to submit again.", receipt.FollowupID, s7CaptureShellQuote(root))
			}
			lines = append(lines, line)
			continue
		}
		view, answer, frozen, err := observedS5Paths(root, state, p)
		if err != nil {
			lines = append(lines, prefix+err.Error())
			continue
		}
		missing := false
		for _, path := range []string{view, answer} {
			info, err := os.Stat(path)
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("expected a regular questionnaire file")
			}
			if err != nil {
				recovery := "Main restores the original files with " + submission.PlanningRecoveryCommand(root, "issue") + "; preserve existing authored answers."
				if frozen {
					recovery = fmt.Sprintf("Main restores this registered frozen execution's original files and source using %s; stage-answer issue in Main does not restore a frozen execution copy. Preserve existing authored answers and the original execution; do not replace it with Main authoring.", filepath.Join(root, ".claude/skills/agent-dispatch/frozen-execution.md"))
				}
				lines = append(lines, prefix+fmt.Sprintf("registered file unavailable: %s: %v. %s", path, err, recovery))
				missing = true
			}
		}
		if missing {
			continue
		}
		if frozen {
			lines = append(lines, prefix+fmt.Sprintf("continue the registered frozen execution using exact view %s and private answer %s. Preserve its original source and named recovery contract at %s; this observation does not create a new native execution or replace its original receiver.", view, answer, filepath.Join(root, ".claude/skills/agent-dispatch/frozen-execution.md")))
			continue
		}
		lines = append(lines, prefix+fmt.Sprintf("Main carries exact view %s, private answer %s and source root %s into a separate native Agent context. Resolve the packet draft.read, task_analysis.path and method paths relative to that source root; open the analysis reference before reading its structural_check. Save the professional answer and return the exact reference for SubagentStop reception. Registration does not prove the platform has started; use the actual native task to dispatch or resume. A real blocker returns to Main without a fabricated conclusion.", view, answer, root))
	}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		if !responsibilities[responsibility] {
			lines = append(lines, fmt.Sprintf("S5 %s has no current issued questionnaire. Main restores the original preparation with %s; preserve the other current responsibility and do not guess an answer path.", responsibility, submission.PlanningRecoveryCommand(root, "issue")))
		}
	}
	return strings.Join(lines, "\n") + submission.PlanningDiagnostic(root, state, "preparation")
}

func applyS5ObservedGuidance(guidance *policy.Guidance) {
	guidance.Action = "Main follows the current independent review responsibilities and recorded receptions below; dispatch, resume or recover the actual missing work."
	guidance.Missing = nil // Exact pending responsibilities are projected below.
	guidance.ReadOrder = []string{"the exact current S5 questionnaire references below"}
	guidance.Read = append([]string(nil), guidance.ReadOrder...)
	guidance.Automation = []string{"ordinary observation does not issue questionnaires, receive answers or advance S5", "native SubagentStop calls the original consumer and valid reception runs the original S5 outlet; existing frozen executions retain their original consumer", "preparation and pending followup recover through their named original entry; received reviewers do not resubmit"}
	guidance.Recovery = []string{"Main restores the exact missing source or original responsibility named below; ordinary tools remain available"}
}
