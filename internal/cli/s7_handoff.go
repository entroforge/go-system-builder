package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// continueS7Acceptance resumes only the original clean-round exit. Result
// reception, the original Gate/TR-009, and material preparation remain separate
// facts. Replaying after promotion cannot run another transition.
func continueS7Acceptance(ctx context.Context, root, statePath, journalPath string, snapshot runtime.Snapshot) (runtime.Snapshot, string) {
	next, message, _ := continueS7AcceptanceResult(ctx, root, statePath, journalPath, snapshot)
	return next, message
}

// The CLI consumes the original outcome directly; file existence and display
// diagnostics cannot establish that preparation succeeded.
func continueS7AcceptanceResult(ctx context.Context, root, statePath, journalPath string, snapshot runtime.Snapshot) (runtime.Snapshot, string, error) {
	retry := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime review-continue --root " + s7CaptureShellQuote(root)
	if lifecycleState(snapshot.State) == "verification" {
		if stringValue(nestedStateValue(snapshot.State, "lifecycle", "phase")) != "clean" || stringValue(mapFieldCLI(mapFieldCLI(snapshot.State, "review"), "plan")["status"]) != "clean" {
			return snapshot, "", nil
		}
		result, err := controller.RunControlCycle(ctx, controller.ControlRequest{Root: root, StatePath: statePath, JournalPath: journalPath, CWD: root, Event: "ReviewResultAccepted", AgentID: "Main"})
		if result.Snapshot.State != nil {
			snapshot = result.Snapshot
		}
		if err != nil || result.Error != "" || !result.QualityGate.TransitionCommitted {
			details := append([]string{}, result.QualityGate.Missing...)
			details = append(details, result.QualityGate.Conflicts...)
			details = append(details, result.Warnings...)
			if result.Error != "" {
				details = append(details, result.Error)
			}
			if err != nil {
				details = append(details, err.Error())
			}
			if len(details) == 0 {
				details = append(details, "original Gate status="+string(result.QualityGate.Status))
			}
			message := "ReviewResult and machine CleanRound are recorded; S7→S10 original Gate/TR-009 continuation is pending: " + strings.Join(details, "; ") + ". Preserve accepted Reviewer answers. Resolve this cause and resume the original exit with: " + retry
			submission.RecordPlanningDiagnostic(root, snapshot.State, "S7 acceptance continuation", message)
			return snapshot, message, fmt.Errorf("%s", message)
		}
	}
	if lifecycleState(snapshot.State) != "acceptance" {
		return snapshot, "", nil
	}
	submission.RecordPlanningDiagnostic(root, snapshot.State, "S7 acceptance continuation", "")
	prefix := "ReviewResult and machine CleanRound are recorded; original TR-009 has entered S10 acceptance. "
	if !runtime.PacketEnabled(snapshot.State) {
		return snapshot, prefix, nil
	}
	if _, _, err := prepareAuditQuestionnaire(root, snapshot.State, "acceptance"); err != nil {
		message := prefix + auditPreparationFailureContext(root, snapshot.State, err)
		submission.RecordPlanningDiagnostic(root, snapshot.State, "S7 acceptance continuation", message)
		return snapshot, message, fmt.Errorf("%s", message)
	}
	return snapshot, prefix + stageAuditQuestionnaireContext(root, policy.Input{Event: "ReviewResultAccepted", AgentID: "Main", CWD: root}, snapshot), nil
}

func auditPrepareRecoveryCommand(root string) string {
	return s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime audit-prepare --root " + s7CaptureShellQuote(root)
}

func auditPreparationFailureContext(root string, state map[string]any, cause error) string {
	kind := currentAuditKind(state)
	path, pathErr := auditQuestionnairePath(root, state, kind)
	if pathErr == nil {
		path = resolveRootPath(root, path)
	} else {
		path = "unresolved (" + pathErr.Error() + ")"
	}
	return fmt.Sprintf("S10 %s is active; its current questionnaire preparation failed or is missing at %s: %v. Resolve this cause, then restore current missing materials through the original preparer: %s. Existing reports are preserved; this recovery does not repeat TR-009 or Reviewer reception.", kind, path, cause, auditPrepareRecoveryCommand(root))
}

// These adapters invoke existing owners, without introducing a second receipt,
// questionnaire or stage Writer.
func runRuntimeS7Recovery(args []string, prepareOnly bool, stdout, stderr io.Writer) int {
	name := "review-continue"
	if prepareOnly {
		name = "audit-prepare"
	}
	flags := flag.NewFlagSet("runtime "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime "+name)
	root := flags.String("root", ".", "repository root")
	state := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journal := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	if err := parseWorkspaceFlags(flags, args); err != nil {
		return 2
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	statePath, journalPath := resolveRootPath(absRoot, *state), resolveRootPath(absRoot, *journal)
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime "+name, err))
		return 1
	}
	if prepareOnly {
		kind := currentAuditKind(snapshot.State)
		if kind == "" {
			fmt.Fprintln(stderr, "runtime audit-prepare requires current S10 acceptance or release_audit responsibility")
			return 1
		}
		if _, _, err := prepareAuditQuestionnaire(absRoot, snapshot.State, kind); err != nil {
			message := auditPreparationFailureContext(absRoot, snapshot.State, err)
			submission.RecordPlanningDiagnostic(absRoot, snapshot.State, "S7 acceptance continuation", message)
			fmt.Fprintln(stderr, message)
			return 1
		}
		submission.RecordPlanningDiagnostic(absRoot, snapshot.State, "S7 acceptance continuation", "")
		fmt.Fprintln(stdout, stageAuditQuestionnaireContext(absRoot, policy.Input{AgentID: "Main"}, snapshot))
		return 0
	}
	if lifecycleState(snapshot.State) != "acceptance" && !(lifecycleState(snapshot.State) == "verification" && stringValue(nestedStateValue(snapshot.State, "lifecycle", "phase")) == "clean" && stringValue(mapFieldCLI(mapFieldCLI(snapshot.State, "review"), "plan")["status"]) == "clean") {
		fmt.Fprintln(stderr, "runtime review-continue requires an original machine CleanRound exit or current S10 acceptance")
		return 1
	}
	next, message, continuationErr := continueS7AcceptanceResult(context.Background(), absRoot, statePath, journalPath, snapshot)
	fmt.Fprintln(stdout, message)
	if continuationErr != nil || lifecycleState(next.State) != "acceptance" {
		return 1
	}
	return 0
}
