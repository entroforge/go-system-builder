package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func reviewPlanQuestionnairePath(root string, state map[string]any) string {
	return review.PlanQuestionnairePath(root, state)
}

func stageReviewPlanQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	message, _ := prepareReviewPlanQuestionnaireContext(root, request, snapshot)
	return message
}

func reviewPlanPreparationFailure(message string) (string, error) {
	return message, errors.New(message)
}

func prepareReviewPlanQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) (string, error) {
	return prepareReviewPlanQuestionnaireEntry(root, request, snapshot, false)
}

func prepareReviewPlanQuestionnaireEntry(root string, request policy.Input, snapshot runtime.Snapshot, repairCompletion bool) (string, error) {
	prepare := request.Event != "Stop" && request.Event != "SubagentStop" && request.Event != "TeammateIdle"
	if !mainQuestionnaireViewer(request) {
		return "", nil
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "verification" {
		return "", nil
	}
	facts, _ := snapshot.State["review"].(map[string]any)
	round := tolerantInt(facts["round"])
	if ptr := review.PlanPointerFromState(snapshot.State); ptr != nil && ptr.ReviewRound == round && !repairSeedAwaitingPlan(snapshot.State) {
		return "", nil
	}
	// TR-012 seed preparation belongs to the actual completion outlet. Ordinary
	// Main projections remain read-only, including Pre, status and recovery.
	seedEntry := repairCurrentS7Entry(snapshot.State)
	if seedEntry {
		main, err := repairExitMainRoot(root, snapshot.State)
		if err != nil {
			return reviewPlanPreparationFailure("S7 original repair preparation cannot resolve Main-root: " + err.Error())
		}
		root = main
		prepare = repairCompletion
	}
	path := reviewPlanQuestionnairePath(root, snapshot.State)
	recovery := reviewPlanPreparationRecovery(root, path)
	if seedEntry {
		recovery = "Restore the stated source/filesystem failure, then resume original S9 completion with `" + repairExitRecovery(root) + "`; preserve the existing professional answer and accepted Handoff."
	}
	if path == "" {
		return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire is unavailable: Main must restore the original current round and generation. No path was guessed.")
	}
	if info, err := os.Stat(path); os.IsNotExist(err) && prepare {
		plan, notes := reviewPlanDraftForEntryContext(contextForHook(request), root, snapshot.State, round)
		if plan == nil {
			return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire preparation failed at " + path + ": " + strings.Join(notes, "; ") + ". " + recovery)
		}
		data, err := json.MarshalIndent(plan, "", "  ")
		if err == nil {
			err = prepareQuestionnaireText(path, string(data)+"\n")
		}
		if err != nil {
			return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire preparation failed at " + path + ": " + err.Error() + ". " + recovery)
		}
		if err := prepareQuestionnaireText(reviewPlanGuidePath(path), reviewPlanGuide(notes)); err != nil {
			return reviewPlanPreparationFailure("S7 Main questionnaire was prepared at " + path + "; Main must restore its planning guidance: " + err.Error() + ". " + recovery)
		}
	} else if err != nil {
		if os.IsNotExist(err) {
			return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire is not prepared at " + path + "; Main owns this preparation gap. " + recovery)
		}
		return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire is unavailable at " + path + ": " + err.Error() + ". " + recovery)
	} else if !info.Mode().IsRegular() {
		return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire is unavailable at " + path + ": expected a regular file. " + recovery)
	}
	guide := reviewPlanGuidePath(path)
	if info, err := os.Stat(guide); os.IsNotExist(err) && prepare {
		plan, notes := reviewPlanDraftForEntryContext(contextForHook(request), root, snapshot.State, round)
		if plan == nil {
			return reviewPlanPreparationFailure("S7 Main guidance preparation failed at " + guide + ": " + strings.Join(notes, "; ") + ". " + recovery)
		}
		if err := prepareQuestionnaireText(guide, reviewPlanGuide(notes)); err != nil {
			return reviewPlanPreparationFailure("S7 Main questionnaire is retained at " + path + "; guidance preparation failed at " + guide + ": " + err.Error() + ". " + recovery)
		}
	} else if os.IsNotExist(err) {
		return reviewPlanPreparationFailure("S7 Main planning guidance is not prepared at " + guide + ". " + recovery)
	} else if err != nil {
		return reviewPlanPreparationFailure("S7 Main planning guidance is unavailable at " + guide + ": " + err.Error() + ". " + recovery)
	} else if err == nil && !info.Mode().IsRegular() {
		return reviewPlanPreparationFailure("S7 Main planning guidance is unavailable at " + guide + ": expected a regular file. " + recovery)
	}
	return "S7 Main ReviewPlan questionnaire: " + path + ". Planning sources/candidates and unresolved decisions: " + guide + " (diagnostic only; if missing, Main restores preparation, never rewrites the professional answer). For each Claim, name the actual target, a falsifiable assertion, observable pass/fail oracle and concrete review or execution method using its source refs. For an S9 seed, PLANNER-REFINE marks unanswered professional fields, including assertion and method; replace them with actual judgments. cold_start describes coverage, not a browser requirement: choose browser or non_browser in each required E2E Assignment behavior_surface from actual sources, or justify not_applicable. For genuine E2E N/A, retain its Claim with applicability=not_applicable, source_refs, na_rationale and na_checklist_id; omit only its Assignment, set e2e_coverage_state=not_applicable and verification_artifact_workspace=null. Compatible same-lens Claims may share claim_ids when scope, focus, methods and non-overlap boundaries fit and dependencies do not become self-waiting; update claim_ids/focus_keys/non_overlap_boundary to reflect the covered scope, preserve individual Claim identities, assertions, oracles and results, plus waves, independence and E2E coverage limits. Complete assignments and applicable coverage; resolve TODO(planner)/PLANNER-REFINE with real professional facts, then save this exact file for the original ReviewPlan consumer. Keep the supplied baseline; missing source or environment belongs to Main preparation, not invented evidence. Reviewer answers remain independent responsibilities.", nil
}

func reviewPlanDraftForEntry(root string, state map[string]any, round int) (*review.Plan, []string) {
	return reviewPlanDraftForEntryContext(context.Background(), root, state, round)
}

func reviewPlanDraftForEntryContext(ctx context.Context, root string, state map[string]any, round int) (*review.Plan, []string) {
	if err := ctx.Err(); err != nil {
		return nil, []string{err.Error()}
	}
	if repairCurrentS7Entry(state) {
		if !repairSeedAwaitingPlan(state) {
			return nil, []string{"restore the original S9 seed/ReviewPlan projection for the current TR-012 round; do not substitute a generic S7 draft"}
		}
		seed, _, err := review.LoadPlan(root, state)
		if err != nil {
			return nil, []string{"restore the exact original S9 seed: " + err.Error()}
		}
		return seed, []string{
			"Original TR-012 seed: " + resolveRootPath(root, stringValue(repairPointerCLI(state)["review_plan_seed_ref"])),
			"Preserve its plan id, round, generation and pinned baseline. Resolve PLANNER-REFINE with actual professional coverage, assertions, methods and E2E applicability before saving the original questionnaire; the seed is not an accepted professional plan.",
		}
	}
	return review.DraftPlanForRootContext(ctx, root, state, round)
}

func runSavedReviewPlanQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).WithContext(contextForHook(request)).Snapshot()
	if err != nil {
		return false, 0
	}
	expected := reviewPlanQuestionnairePath(root, snapshot.State)
	if expected == "" || filepath.Clean(path) != filepath.Clean(expected) {
		return false, 0
	}
	var output, commandErr bytes.Buffer
	next := "Main prepares and registers eligible Reviewer assignments before native dispatch. Each Reviewer saves its ready original answer; PreToolUse receives it through the original consumer, with successful PostToolUse and Stop/SubagentStop fallback. Reception does not wait for a final message or returned reference; report results to Main when finished."
	roundFacts, _ := snapshot.State["review"].(map[string]any)
	if ptr := review.PlanPointerFromState(snapshot.State); ptr != nil && ptr.ReviewRound == tolerantInt(roundFacts["round"]) && !repairSeedAwaitingPlan(snapshot.State) {
		_, _, loadErr := review.LoadPlan(root, snapshot.State)
		authored, readErr := os.ReadFile(path)
		registered, registeredErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(ptr.Path)))
		if loadErr != nil || readErr != nil || registeredErr != nil {
			next = "Main restores the original accepted plan/source before checking replay; preserve the answer and original receipt."
			fmt.Fprintf(&output, "ReviewPlan replay source unavailable: %v\n", errors.Join(loadErr, readErr, registeredErr))
		} else if !review.SamePlanAnswer(authored, registered) {
			next = "Use the original named revision/rework path for changed professional content; preserve the accepted plan."
			fmt.Fprintln(&output, "The current ReviewPlan is already registered; changed answers require the original named revision/rework path. Preserve the accepted plan.")
		} else {
			fmt.Fprintln(&output, "The same ReviewPlan was already received; no Runtime write or duplicate registration was performed.")
			next += "\n" + s7PendingReviewGuidance(root, snapshot.State)
		}
	} else {
		received, err := review.RegisterPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), review.PlanRequest{Context: contextForHook(request), ExpectedRevision: -1, PlanPath: path})
		if err != nil {
			var author *review.PlanAnswerError
			if nativeReceptionInterrupted(err) {
				next = "ReviewPlan reception was interrupted, not rejected for professional content. The original consumer retains this exact delivery at " + path + " and owns bounded pending/staging recovery. This return has not confirmed completion. An accepted Plan keeps its original receipt and continuation; no answer edit, repeated save, new plan or manual artifact cleanup is requested by timeout alone."
			} else if errors.As(err, &author) {
				next = "Correct the specific original validation feedback in this same questionnaire and save; no new command or report is required."
			} else {
				next = "Main owns source/environment/Writer recovery; preserve this answer and any original receipt, inspect the concrete failure before resuming. Do not rewrite professional content to repair a system failure."
			}
			fmt.Fprintln(&commandErr, formatFailure("runtime review-plan", err))
		} else {
			ptr := review.PlanPointerFromState(received.State)
			fmt.Fprintf(&output, "review-plan: registered %s for round %d (status %s).\n", ptr.PlanID, ptr.ReviewRound, ptr.Status)
			next += "\n" + s7PendingReviewGuidance(root, received.State)
		}
	}
	context := fmt.Sprintf("S7 Main ReviewPlan questionnaire saved at %s. Next: %s\n%s", path, next, output.String()+commandErr.String())
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": context}})
	return true, 0
}

func reviewPlanGuidePath(path string) string { return strings.TrimSuffix(path, ".json") + ".md" }
func reviewPlanGuide(notes []string) string {
	var b strings.Builder
	b.WriteString("# S7 planning sources and decisions\n\nThis is derived guidance, not an accepted Plan or an execution permit. Complete the original JSON and save once. A candidate is not a reusable-asset declaration or a passing result.\n\n")
	b.WriteString("Product baseline, frozen_subjects, coverage_inventory and derived digests are machine projections: preserve them and have Main restore original preparation/consumer inputs if they disagree. Deleted paths remain frozen with kind=deleted and the proven deletion preimage SHA-256; they remain in the Claim coverage denominator and have no current-file SHA to supply. Main supplies professional Claims with exact source_refs, actual assignments and boundaries, applicability decisions, and environment/asset arrangements. Missing Claim coverage calls for a focused assertion and oracle, not edits to the frozen objects.\n\n")
	for _, note := range notes {
		b.WriteString("- " + note + "\n")
	}
	b.WriteString("\nFor each required E2E Assignment, set behavior_surface to browser or non_browser from explicit behavior sources before registering this Plan. Browser CASE discovery can establish browser; ui_impact=none or an empty browser inventory cannot establish general E2E N/A. When no source establishes the surface, Main resolves this named Plan decision; do not guess from method prose. Historical registered manifests stay frozen. Choose the existing E2E state from actual circumstances; unresolved discovery does not require rebuilding scripts. Select exact cases.json#CASE references and actual script paths; the original receiver fills omitted asset IDs/digests. Supply real selector, route and environment facts rather than generated labels. For cold_start, verification_artifact_workspace must be repository-relative under e2e-workspace/; the original registration prepares that isolated script/fixture surface. Reuse confirmed scripts at their original paths and create only missing assets in that workspace.\n\nREQ prototype links bound automatic discovery hints, not the professional regression boundary. Follow the actual changed providers and consumers through approved architecture/contracts, shared models, API/event/auth/configuration and journey/data paths. When removing a dependency or changing its owner, inspect the previously known consumers as well. Use existing declared sources and identify missing relationship/environment material for Main; no mandatory module graph or whole-project fallback is implied. Explain scope from actual dependencies and keep each scenario's exact source.\n")
	return b.String()
}

func reviewPlanPreparationRecovery(root, path string) string {
	return "Main repairs the stated source/filesystem failure, then resumes original S7 preparation with `loop-harness s7 draft --root " + s7CaptureShellQuote(root) + " --out " + s7CaptureShellQuote(path) + "`; preserve any existing professional answer. Existing integration and transition facts remain recorded; do not resubmit Builder delivery or merge again."
}
