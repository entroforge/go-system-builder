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
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func reviewPlanQuestionnairePath(root string, state map[string]any) string {
	return review.PlanQuestionnairePath(root, state)
}

// A seed pins the baseline before the Planner has answered; every other
// existing pointer remains registered unless its original identity differs.
// Missing legacy coordinates cannot undo an already accepted Plan.
func currentReviewPlanRegistered(state map[string]any) bool {
	ptr := review.PlanPointerFromState(state)
	if ptr == nil || repairSeedAwaitingPlan(state) {
		return false
	}
	facts, _ := state["review"].(map[string]any)
	return reviewmeta.CompareIdentity(ptr.Identity(), reviewmeta.FromReview(facts)) != reviewmeta.Different
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
	if currentReviewPlanRegistered(snapshot.State) {
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
		return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire is unavailable: Main must restore the original current review identity and generation. No path was guessed.")
	}
	if info, err := os.Stat(path); os.IsNotExist(err) && prepare {
		plan, notes := reviewPlanDraftForEntryContext(contextForHook(request), root, snapshot.State)
		if plan == nil {
			return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire preparation failed at " + path + ": " + strings.Join(notes, "; ") + ". " + recovery)
		}
		data, err := json.MarshalIndent(plan, "", "  ")
		if err == nil {
			_, err = prepareInitialReviewPlanQuestionnaire(contextForHook(request), root, path, reviewPlanGuide(notes), append(data, '\n'))
		}
		if err != nil {
			return reviewPlanPreparationFailure("S7 Main ReviewPlan questionnaire preparation failed at " + path + ": " + err.Error() + ". " + recovery)
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
		plan, notes := reviewPlanDraftForEntryContext(contextForHook(request), root, snapshot.State)
		if plan == nil {
			return reviewPlanPreparationFailure("S7 Main guidance preparation failed at " + guide + ": " + strings.Join(notes, "; ") + ". " + recovery)
		}
		if err := prepareS7GuideIfMissing(contextForHook(request), root, path, reviewPlanGuide(notes)); err != nil {
			return reviewPlanPreparationFailure("S7 Main questionnaire is retained at " + path + "; guidance preparation failed at " + guide + ": " + err.Error() + ". " + recovery)
		}
	} else if os.IsNotExist(err) {
		return reviewPlanPreparationFailure("S7 Main planning guidance is not prepared at " + guide + ". " + recovery)
	} else if err != nil {
		return reviewPlanPreparationFailure("S7 Main planning guidance is unavailable at " + guide + ": " + err.Error() + ". " + recovery)
	} else if err == nil && !info.Mode().IsRegular() {
		return reviewPlanPreparationFailure("S7 Main planning guidance is unavailable at " + guide + ": expected a regular file. " + recovery)
	}
	return "S7 Main ReviewPlan questionnaire: " + path + ". Planning sources and unresolved decisions: " + guide + ". Use the actual materials to describe Claims, assertions, methods, applicability and Reviewer responsibilities. For an S9 seed, PLANNER-REFINE identifies work for Main to resolve. Save your actual current expression in this original file and continue refining it as needed; unassigned Claims and unresolved E2E decisions remain pending, not a reason to invent facts or submit another document. Reviewer answers remain independent responsibilities.", nil
}

func reviewPlanDraftForEntry(root string, state map[string]any) (*review.Plan, []string) {
	return reviewPlanDraftForEntryContext(context.Background(), root, state)
}

func reviewPlanDraftForEntryContext(ctx context.Context, root string, state map[string]any) (*review.Plan, []string) {
	if err := ctx.Err(); err != nil {
		return nil, []string{err.Error()}
	}
	if repairCurrentS7Entry(state) {
		if !repairSeedAwaitingPlan(state) {
			return nil, []string{"restore the original S9 seed/ReviewPlan projection for the current TR-012 entry; do not substitute a generic S7 draft"}
		}
		seed, _, err := review.LoadPlan(root, state)
		if err != nil {
			return nil, []string{"restore the exact original S9 seed: " + err.Error()}
		}
		notes := []string{
			"Original TR-012 seed: " + resolveRootPath(root, stringValue(repairPointerCLI(state)["review_plan_seed_ref"])),
			"Preserve the original Plan identity and generation. Resolve PLANNER-REFINE with actual professional judgments as work proceeds; changed paths remain review sources. The seed is not an accepted professional plan.",
		}
		return seed, append(notes, s7PlanningMaterials(ctx, root, state)...)
	}
	plan, notes := review.DraftPlanForRootContext(ctx, root, state)
	if plan != nil {
		notes = append(notes, s7PlanningMaterials(ctx, root, state)...)
	}
	return plan, notes
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
	observed := receiveReviewPlanQuestionnaire(root, request, snapshot, path)
	if observed.Context != "" {
		_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": request.Event, "additionalContext": observed.Context}})
	}
	return true, 0
}

// receiveReviewPlanQuestionnaire is shared by the explicit save adapter and
// natural Main observation. It always invokes the original plan consumer;
// observation does not pretend that a tool saved the file.
func receiveReviewPlanQuestionnaire(root string, request policy.Input, snapshot runtime.Snapshot, path string, input ...[]byte) s7ReviewPlanObservation {
	result := s7ReviewPlanObservation{Attempted: true}
	var output, commandErr bytes.Buffer
	next := "Main prepares and registers eligible Reviewer assignments before native dispatch. Each Reviewer saves its ready original answer; PreToolUse receives it through the original consumer, with successful PostToolUse and Stop/SubagentStop fallback. Reception does not wait for a final message or returned reference; report results to Main when finished."
	var original []byte
	if len(input) > 0 {
		original = input[0]
	} else {
		original, _ = os.ReadFile(path)
	}
	baselineUnknown := false
	if original != nil {
		unchanged, unknown := s7InitialDraftObservation(request, path, snapshot.State, original)
		if unchanged {
			return s7ReviewPlanObservation{ReplaySame: true}
		}
		baselineUnknown = unknown
	}
	{
		received, reception, err := review.ReceivePlanAnswer(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), review.PlanRequest{Context: contextForHook(request), ExpectedRevision: -1, PlanPath: path, AnswerBytes: original})
		if err != nil {
			var author *review.PlanAnswerError
			if nativeReceptionInterrupted(err) {
				next = "ReviewPlan reception was interrupted, not rejected for professional content. The original consumer retains this exact delivery at " + path + " and owns bounded pending/staging recovery. This return has not confirmed completion. An accepted Plan keeps its original receipt and continuation; no answer edit, repeated save, new plan or manual artifact cleanup is requested by timeout alone."
			} else if errors.As(err, &author) {
				// A partial JSON write during editing cannot yet express an
				// actionable field error. Explicit saves still receive feedback.
				// Parseable errors remain visible on natural events too, including
				// shell saves that provide no explicit path.
				savedPath, saved := savedQuestionnairePath(request)
				if (!saved || filepath.Clean(savedPath) != filepath.Clean(path)) && len(input) > 0 && !json.Valid(input[0]) {
					return s7ReviewPlanObservation{}
				}
				next = "Correct the specific original validation feedback in this same questionnaire; no new command or report is required."
			} else {
				next = "Main owns source/environment/Writer recovery; preserve this answer and any original receipt, inspect the concrete failure before resuming. Do not rewrite professional content to repair a system failure."
			}
			fmt.Fprintln(&commandErr, formatFailure("runtime review-plan", err))
		} else if reception == review.PlanReceptionSame {
			result.ReplaySame = true
			if s7SuccessorPending(root, received.State) {
				_, message, _ := continueS7AcceptanceResult(contextForHook(request), root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), received)
				if message != "" {
					result.ReplaySame = false
					result.Context = message
				}
			}
			return result
		} else {
			result.Changed = true
			if baselineUnknown {
				next = "The original generated baseline is unavailable, so whether this was unchanged prefill is unknown. The current original expression was received; no author action is required to prove its history. " + next
			}
			ptr := review.PlanPointerFromState(received.State)
			verb := "registered"
			if reception == review.PlanReceptionRevised {
				verb = "revised"
			}
			fmt.Fprintf(&output, "review-plan: %s %s (status %s).\n", verb, ptr.PlanID, ptr.Status)
			if s7SuccessorPending(root, received.State) {
				_, message, _ := continueS7AcceptanceResult(contextForHook(request), root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), received)
				if message != "" {
					next = message
				}
			} else {
				next += "\n" + s7PendingReviewGuidance(root, received.State)
			}
		}
	}
	result.Context = fmt.Sprintf("S7 Main ReviewPlan questionnaire observed at %s. Next: %s\n%s", path, next, output.String()+commandErr.String())
	return result
}

func reviewPlanGuidePath(path string) string { return strings.TrimSuffix(path, ".json") + ".md" }
func reviewPlanGuide(notes []string) string {
	var b strings.Builder
	b.WriteString("# S7 planning sources and decisions\n\nThis is derived guidance, not an accepted Plan or an execution permit. Complete and save the original JSON; continue editing it when your actual professional judgment changes. Natural events receive it without an extra submission. A candidate is not a reusable-asset declaration or a passing result.\n\n")
	b.WriteString("Use available product inputs and change paths to plan the actual review. Existing frozen subjects are optional historical source information; an empty frozen_subjects list is valid and does not require a SHA or a product scan. Preserve actual deleted paths as review scope without inventing a file digest. Main supplies professional Claims with source_refs, assignments and boundaries, applicability decisions, and environment/asset arrangements.\n\n")
	for _, note := range notes {
		b.WriteString("- " + note + "\n")
	}
	b.WriteString("\nArrange E2E work from actual product behavior, available scenarios and environment. Record known behavior_surface and applicable assets when decided; unresolved choices and unknown details remain visible planning work. When selecting an existing asset, the optional field shape is `\"e2e_assets\": [{\"case_ref\": \"<actual scenario reference>\", \"path\": \"<actual asset path>\"}]`; replace both examples with actual values. Optional string fields include `asset_id`, `selector_ref`, `route_ref` and `environment`. If no asset is selected or the choice is unresolved, omit the list or leave the choice unresolved; do not invent values. For cold_start, verification_artifact_workspace names a script and fixture area under e2e-workspace/; reuse suitable existing assets and prepare missing ones as needed.\n\nAfter the original plan is received, Main gets the exact preparation path for each available Assignment. Prepare its manifest and use the returned original workgroup registration command; the system prepares its view and answer on a subsequent natural event. Main then carries those paths to the native Reviewer. This is the existing dispatch path, not an extra plan submission.\n\nFollow the actual changed providers and consumers through available architecture, contracts, shared models and journeys. Identify missing relationship or environment material for Main without inventing a module graph or coverage proof.\n")
	return b.String()
}

func reviewPlanPreparationRecovery(root, path string) string {
	return "Main repairs the stated source/filesystem failure, then resumes original S7 preparation with `loop-harness s7 draft --root " + s7CaptureShellQuote(root) + " --out " + s7CaptureShellQuote(path) + "`; preserve any existing professional answer. Existing integration and transition facts remain recorded; do not resubmit Builder delivery or merge again."
}
