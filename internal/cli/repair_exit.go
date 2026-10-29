package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const repairHandoffPendingInstruction = "The final required Targeted SubagentStop attempts the original Handoff automatically. If it remains incomplete, preserve the accepted observations and use the exact runtime repair handoff complete recovery supplied with the failure; a real budget decision remains necessary when the limit is reached. No extra Handoff answer or repeated Targeted report is needed; ordinary reminders do not retry the transition"

func repairExitRecovery(root string) string {
	return fmt.Sprintf("%s runtime repair handoff complete --root %s", s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")), s7CaptureShellQuote(root))
}

// Use the original locked REQ destination, never the current checkout as a
// substitute authority. This finite read does not validate the Git closure.
func repairExitMainRoot(root string, state map[string]any) (string, error) {
	binding, err := checkpointREQWorkspace(state)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(binding.ProjectRoot) {
		return "", fmt.Errorf("original REQ Main-root is not absolute: %s", binding.ProjectRoot)
	}
	main, err := workspace.Canonical(binding.ProjectRoot)
	if err != nil {
		return "", fmt.Errorf("original REQ Main-root unavailable: %w", err)
	}
	requested, err := workspace.Canonical(root)
	if err != nil {
		return "", err
	}
	if requested != main {
		return "", fmt.Errorf("S9 completion root %s differs from original REQ Main-root %s", requested, main)
	}
	if _, marked, err := workspace.PointerRoot(main); err != nil {
		return "", err
	} else if marked {
		return "", fmt.Errorf("original REQ Main-root is a Worker checkout")
	}
	return main, nil
}

// A received Targeted conclusion can outlive a failed successor. Keep that
// failure distinct from author-owned answer omissions when returning to Main.
type repairExitFailure struct {
	cause   error
	message string
}

func (e *repairExitFailure) Error() string { return e.message }
func (e *repairExitFailure) Unwrap() error { return e.cause }

func repairCurrentS7Entry(state map[string]any) bool {
	p := repairPointerCLI(state)
	entry := mapFieldCLI(mapFieldCLI(state, "review"), "round_entry")
	return lifecycleState(state) == "verification" && stringValue(entry["transition_id"]) == "TR-012" &&
		integerValue(entry["round"]) == integerValue(nestedStateValue(state, "review", "round")) &&
		integerValue(entry["baseline_generation"]) == integerValue(nestedStateValue(state, "baseline", "generation")) &&
		stringValue(p["review_plan_seed_ref"]) != "" && entry["review_plan_seed_ref"] == p["review_plan_seed_ref"] &&
		stringValue(p["handoff_ref"]) != "" && entry["repair_handoff_ref"] == p["handoff_ref"]
}

// TR-012 installs the seed's pointer to pin its baseline. That projection is
// not the Planner's first professional registration; RegisterPlan still owns
// checking the untouched seed and all refinement requirements.
func repairSeedAwaitingPlan(state map[string]any) bool {
	if !repairCurrentS7Entry(state) {
		return false
	}
	plan := review.PlanPointerFromState(state)
	p := repairPointerCLI(state)
	return plan != nil && plan.Revision == 1 && plan.Path == stringValue(p["review_plan_seed_ref"]) && plan.SHA256 == stringValue(p["review_plan_seed_sha256"])
}

// This projection never retries a Writer or prepares files. The explicit
// completion outlet owns recovery after partial Targeted/Handoff success.
func repairHandoffProjectionContext(root string, state map[string]any) string {
	p := repairPointerCLI(state)
	if stringValue(p["handoff_ref"]) == "" && stringValue(p["status"]) != "ready_for_full_review" {
		if lifecycleState(state) == "bug_resolution" {
			switch stringValue(p["status"]) {
			case "targeted_reverification":
				return "Main follows the remaining original independent Targeted verification responsibilities; accepted observations remain recorded."
			case "blocked":
				if stringValue(p["failure_route"]) == "blocked" {
					return "Main resolves the recorded environmental blocker, then resumes the original independent Targeted verification responsibility; accepted observations remain recorded."
				}
				return stringValue(p["next_action"])
			}
		}
		return ""
	}
	if stringValue(p["handoff_ref"]) != "" && !repairCurrentS7Entry(state) {
		return "The original repair Handoff is recorded; follow the current lifecycle responsibility without resuming this completed repair or its former S7 entry."
	}
	if stringValue(p["handoff_ref"]) == "" && lifecycleState(state) != "bug_resolution" {
		return "This original repair is no longer the current lifecycle responsibility; follow the current stage without resuming its former completion action."
	}
	main, err := repairExitMainRoot(root, state)
	if err != nil {
		return "S9 original completion context cannot resolve its original Main-root: " + err.Error() + ". Preserve accepted facts; no recovery command was inferred."
	}
	root = main
	message := ""
	if stringValue(p["handoff_ref"]) == "" {
		message = "S9 original completion recovery: `" + repairExitRecovery(root) + "`. Preserve accepted Targeted observations; this outlet only completes the original Handoff and S7 preparation."
	}
	if ref := stringValue(p["handoff_ref"]); ref != "" {
		message += " Recorded Handoff: " + resolveRootPath(root, ref) + "."
	} else if session := stringValue(p["session_id"]); session != "" {
		message += " Original Handoff source: " + filepath.Join(root, ".claude/review/repair/handoffs", "repair-handoff-"+dispatchSlug(session)+".json") + "."
	}
	if s7BudgetGateRequired(state, root) {
		path, _, err := s7BudgetDecisionApplicationForState(root, state)
		if err == nil {
			message += " Actual review budget is exhausted; the original human decision is required before completion. Original human budget application: " + path + "."
		}
	}
	if seed := stringValue(p["review_plan_seed_ref"]); repairCurrentS7Entry(state) {
		message += " Fresh S7 seed: " + resolveRootPath(root, seed) + "."
		if plan := review.PlanPointerFromState(state); plan != nil && plan.ReviewRound == integerValue(nestedStateValue(state, "review", "round")) && !repairSeedAwaitingPlan(state) {
			return message + " The current S7 ReviewPlan is already registered; follow its current independent review responsibilities without submitting it again."
		}
		if plan := review.PlanPointerFromState(state); plan == nil || plan.ReviewRound != integerValue(nestedStateValue(state, "review", "round")) {
			return message + " The original S9 seed/ReviewPlan projection is missing for this TR-012 entry; resume `" + repairExitRecovery(root) + "` to restore preparation without resubmitting accepted Targeted work."
		}
		if path := reviewPlanQuestionnairePath(root, state); path != "" {
			message += " S7 Main ReviewPlan questionnaire: " + path + "; planning guide: " + reviewPlanGuidePath(path) + ". Complete and save the original questionnaire for registration."
			for _, source := range []string{resolveRootPath(root, seed), path, reviewPlanGuidePath(path)} {
				if info, err := os.Stat(source); err != nil || !info.Mode().IsRegular() {
					message += " Preparation is incomplete at " + source + "; resume `" + repairExitRecovery(root) + "`."
				}
			}
		}
	}
	return message
}

// Only actual completion and explicit recovery call this function; ordinary
// Pre/status projections cannot advance a round or manufacture a decision.
func completeRepairExit(root string) (string, error) {
	original, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return err.Error(), err
	}
	root, err = repairExitMainRoot(root, original.State)
	if err != nil {
		return err.Error(), err
	}
	ref, snapshot, err := completeRepairHandoff(root)
	if err != nil {
		message := "Original Handoff remains incomplete: " + err.Error() + ". "
		if ref.Path != "" {
			message += "Existing Handoff source: " + resolveRootPath(root, ref.Path) + ". "
		}
		if s7BudgetGateRequired(snapshot.State, root) {
			path, prepareErr := prepareS7BudgetDecisionQuestionnaire(root, snapshot.State)
			if prepareErr != nil {
				message += "Original budget application preparation failed: " + prepareErr.Error() + ". "
			} else {
				message += "Actual review budget is exhausted; original human decision application: " + path + ". Only its real human decision can authorize the successor. "
			}
		}
		return message + repairHandoffProjectionContext(root, snapshot.State), err
	}
	message := "Handoff recorded at " + resolveRootPath(root, ref.Path) + "."
	if !repairCurrentS7Entry(snapshot.State) {
		return message, nil
	}
	// System-owned preparation at the actual completion outlet, even when the
	// triggering event was a Worker stop. This does not receive a plan answer.
	guidance, prepareErr := prepareReviewPlanQuestionnaireEntry(root, policy.Input{Event: "PostToolUse", CWD: root}, snapshot, true)
	if guidance != "" {
		message += " " + guidance
	}
	return message + " " + repairHandoffProjectionContext(root, snapshot.State), prepareErr
}

func runRuntimeRepairHandoffComplete(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("runtime repair handoff complete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bindUsage(fs, "runtime repair handoff complete")
	root := fs.String("root", ".", "repository root")
	if err := parseWorkspaceFlags(fs, args); err != nil {
		return 2
	}
	message, err := completeRepairExit(*root)
	if err != nil {
		fmt.Fprintln(stderr, message)
		return 1
	}
	snapshot, readErr := runtime.NewStore(filepath.Join(*root, ".claude/loop-state.json"), filepath.Join(*root, ".claude/loop-events.jsonl")).Snapshot()
	if readErr != nil {
		fmt.Fprintln(stderr, readErr)
		return 1
	}
	return encodeJSON(stdout, map[string]any{"revision": snapshot.Revision, "next_action": message})
}
