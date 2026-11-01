// s7_status.go provides a read-only board view of the current S7 review
// round: the registered ReviewPlan, every Claim's disposition, assignment
// consumption, current-round Findings, and the sealed batch / clean round
// state. It is the agent's answer to "where do I stand" without parsing
// raw loop-state JSON (L3-S7 §12.2 D7).
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// runS7Command is the `loop-harness s7` dispatcher: `status` is the
// read-only board, `draft` scaffolds a ReviewPlan, `manifest-draft`
// scaffolds the reviewer team-manifest for one plan Assignment, and
// The original Reviewer answer is received on natural events.
func runS7Command(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		name := compactHelpName(args)
		if name == "" {
			name = "<status|draft|manifest-draft>"
		}
		printCommandHelp(stdout, "loop-harness s7 "+name, "S7 actions: prepare the ReviewPlan and reviewer manifest, then inspect the status board; original answers are received naturally.")
		return 0
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != "draft" && args[0] != "manifest-draft") {
		fmt.Fprintln(stderr, "s7 requires <status|draft|manifest-draft>")
		return 2
	}
	if args[0] == "manifest-draft" {
		flags := flag.NewFlagSet("s7 manifest-draft", flag.ContinueOnError)
		flags.SetOutput(stderr)
		root := flags.String("root", ".", "repository root")
		assignmentID := flags.String("assignment", "", "ReviewPlan Assignment id to dispatch (required)")
		out := flags.String("out", "", "write the draft manifest JSON here (default stdout)")
		if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
			return 2
		}
		if *assignmentID == "" {
			fmt.Fprintln(stderr, "s7 manifest-draft requires --assignment <assignment-id>")
			return 2
		}
		return runS7ManifestDraft(*root, *assignmentID, *out, stdout)
	}
	if args[0] == "draft" {
		flags := flag.NewFlagSet("s7 draft", flag.ContinueOnError)
		flags.SetOutput(stderr)
		root := flags.String("root", ".", "repository root")
		out := flags.String("out", "", "write the draft plan JSON here (default stdout)")
		if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
			return 2
		}
		return runS7Draft(*root, *out, stdout, stderr)
	}
	flags := flag.NewFlagSet("s7 status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	explain := flags.Bool("explain", false, "explain how original answers advance the current responsibilities")
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		return 2
	}
	return runS7Status(*root, stdout, *explain)
}

// runS7Status reads the current runtime state and prints the S7 review board.
// An optional explanation describes the natural answer handoff.
func runS7Status(root string, stdout io.Writer, explain bool) int {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stdout, "s7 status: resolve root: %v\n", err)
		return 1
	}
	root = absoluteRoot
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	store := runtime.NewStore(statePath, journalPath)
	snapshot, err := store.Snapshot()
	if err != nil {
		// RC-18 (F-M2): an uninitialized or locked Runtime surfaces as a bare
		// lock/read error here; name the recovery path instead, mirroring the
		// s10 status guidance.
		fmt.Fprintf(os.Stderr, "s7 status: read runtime: %v; next: the Runtime may not be initialized or its lock is stuck — run `loop-harness init` to initialize, or `loop-harness runtime recover inspect` before starting S7\n", err)
		return 1
	}
	state := snapshot.State

	reviewMap, _ := state["review"].(map[string]any)
	fmt.Fprintln(stdout, "S7 review board")
	roundEntry, _ := reviewMap["round_entry"].(map[string]any)
	seedRef := stringValue(roundEntry["review_plan_seed_ref"])
	ptr := review.PlanPointerFromState(state)
	if repairSeedAwaitingPlan(state) {
		fmt.Fprintf(stdout, "seed_projection: awaiting professional refinement (seed=%s)\n", seedRef)
		fmt.Fprintln(stdout, stageReviewPlanQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root}, runtime.Snapshot{State: state}))
		return 0
	}
	if ptr == nil {
		if seedRef != "" {
			fmt.Fprintf(stdout, "seed_projection: missing (seed=%s is present in round_entry but review.plan is absent)\n", seedRef)
			fmt.Fprintln(stdout, "next: reconcile the S9 handoff projection before drafting or registering another plan")
		}
		path := review.PlanQuestionnairePath(root, state)
		if path == "" {
			fmt.Fprintln(stdout, "ReviewPlan questionnaire preparation gap: Main/system must restore the current Runtime generation and original Plan identity.")
		} else if _, err := os.Stat(path); err != nil {
			fmt.Fprintf(stdout, "ReviewPlan questionnaire: %s; preparation gap: %v; Main/system must prepare this exact questionnaire through the original listener.\n", path, err)
		} else {
			fmt.Fprintf(stdout, "ReviewPlan questionnaire: %s; Main fills the professional Claims, oracles, methods and assignments, then saves this file for original reception.\n", path)
		}
		return 0
	}
	fmt.Fprintf(stdout, "plan: %s status=%s revision=%d e2e_coverage=%s\n",
		ptr.PlanID, ptr.Status, ptr.Revision, ptr.E2ECoverageState)
	if roundEntry != nil {
		transitionID := stringValue(roundEntry["transition_id"])
		fmt.Fprintf(stdout, "round_entry: %s (%s)\n", transitionID, s7RoundEntryLabel(transitionID))
		for _, field := range []string{"repair_handoff_ref", "change_impact_ref", "review_plan_seed_ref"} {
			if value := stringValue(roundEntry[field]); value != "" {
				fmt.Fprintf(stdout, "  %s=%s\n", field, value)
			}
		}
		if seedRef != "" {
			fmt.Fprintln(stdout, "seed_projection: present (the registered plan is the S9 seed projection; refine only through the controlled revise path)")
		}
	}

	plan, _, planErr := review.LoadPlan(root, state)

	dispositions := review.Dispositions(state)
	claimsByID := map[string]review.Claim{}
	assignmentByClaim := map[string]string{}
	if planErr == nil {
		for _, claim := range plan.Claims {
			claimsByID[claim.ClaimID] = claim
		}
		for _, assignment := range plan.Assignments {
			for _, claimID := range assignment.ClaimIDs {
				assignmentByClaim[claimID] = assignment.AssignmentID
			}
		}
	}

	// Claims grouped by lens, in plan order when loadable.
	fmt.Fprintln(stdout, "\nclaims:")
	claimOrder := make([]string, 0, len(dispositions))
	if planErr == nil {
		for _, claim := range plan.Claims {
			claimOrder = append(claimOrder, claim.ClaimID)
		}
	} else {
		for claimID := range dispositions {
			claimOrder = append(claimOrder, claimID)
		}
		sort.Strings(claimOrder)
	}
	for _, claimID := range claimOrder {
		disp, ok := dispositions[claimID]
		if !ok {
			claim, inPlan := claimsByID[claimID]
			if !inPlan {
				continue
			}
			disp = review.ClaimDisposition{
				Lens: claim.Lens, Applicability: claim.Applicability,
				Disposition: "planned", AssignmentID: assignmentByClaim[claimID],
			}
		}
		line := fmt.Sprintf("  %s [%s] %s", claimID, disp.Lens, disp.Disposition)
		if disp.Applicability == "not_applicable" {
			line += " (plan-level N/A)"
		}
		if disp.AssignmentID != "" {
			line += fmt.Sprintf(" <- %s", disp.AssignmentID)
		}
		if claim, ok := claimsByID[claimID]; ok {
			focus, target := claim.FocusKey, claim.Target
			if focus == "" {
				focus = "-"
			}
			if target == "" {
				target = "-"
			}
			line += fmt.Sprintf(" focus=%s target=%s", focus, target)
		}
		if len(disp.FindingIDs) > 0 {
			line += fmt.Sprintf(" findings=%v", disp.FindingIDs)
		}
		fmt.Fprintln(stdout, line)
	}

	// Assignments with agent binding and consumption state.
	reviewAssignments, _ := reviewMap["assignments"].(map[string]any)
	fmt.Fprintln(stdout, "\nassignments:")
	assignmentIDs := make([]string, 0, len(reviewAssignments))
	for id := range reviewAssignments {
		assignmentIDs = append(assignmentIDs, id)
	}
	sort.Strings(assignmentIDs)
	for _, id := range assignmentIDs {
		row, _ := reviewAssignments[id].(map[string]any)
		if row == nil {
			continue
		}
		agent, _ := row["agent_id"].(string)
		if agent == "" {
			if queuedAgent, _ := row["queued_agent_id"].(string); queuedAgent != "" {
				agent = fmt.Sprintf("(queued for %s)", queuedAgent)
			} else {
				agent = "(not dispatched — Main selects the real Reviewer for original registration and dispatch)"
			}
		}
		status := row["status"]
		// An unfinished responsibility keeps its actual owner and original answer.
		line := fmt.Sprintf("  %s [%s] status=%s agent=%s", id, row["lens"], status, agent)
		if status == "blocked" {
			line += "\n    " + strings.TrimSpace(s7BlockedAssignmentGuidance(root, state, id))
		}
		if queuedAgent, _ := row["queued_agent_id"].(string); queuedAgent != "" {
			line += fmt.Sprintf("\n    queue_reason=%v; Main coordinates %s's assignment", row["queue_reason"], queuedAgent)
		}
		fmt.Fprintln(stdout, line)
	}

	// Current-round findings.
	findings := review.RoundFindings(state)
	if len(findings) > 0 {
		fmt.Fprintln(stdout, "\nfindings:")
		for _, row := range findings {
			fmt.Fprintf(stdout, "  %s [%s/%s] claim=%s finder=%s\n",
				row["finding_id"], row["lens"], row["severity"], row["claim_id"], row["original_finder"])
		}
	}

	// Exit state and the single next action.
	fmt.Fprintln(stdout, "")
	pending := s7PendingRequiredClaims(state, plan, planErr == nil)
	if len(findings) > 0 {
		fmt.Fprintln(stdout, "Actual Findings are handed to S8; Main continues or safely stops other work as appropriate.")
		return 0
	}
	if ptr.Status == "clean" {
		fmt.Fprintln(stdout, "Applicable review responsibilities completed without a Finding; the original TR-009 prepares S10 acceptance.")
		return 0
	}
	fmt.Fprintf(stdout, "pending responsibility claims: %d %v\n", len(pending), pending)
	printS7PacketQuestionnaires(root, stdout, state, plan, pending)
	if explain {
		fmt.Fprintln(stdout, "Reviewers save their original answers; natural events receive each answer and continue the applicable stage outlet. Reception and professional completion are separate facts.")
	}
	return 0
}

// Read only the original plan, assignment and issued-packet facts. Domain
// statuses remain domain facts; this board introduces no new work states.
func printS7PacketQuestionnaires(root string, stdout io.Writer, state map[string]any, plan *review.Plan, pending []string) {
	if plan == nil {
		fmt.Fprintln(stdout, "Reviewer questionnaire preparation gap: Main/system must restore the registered ReviewPlan before resolving current responsibilities.")
		return
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "No pending responsibility is shown; Main follows the accepted original answers and current stage routing.")
		return
	}
	missing := map[string]bool{}
	for _, id := range pending {
		missing[id] = true
	}
	facts, _ := state["review"].(map[string]any)
	assignments, _ := facts["assignments"].(map[string]any)
	packets, packetErr := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(context.Background(), root), state, "")
	packetByAssignment := map[string]int{}
	for i, packet := range packets {
		packetByAssignment[packet.Binding.AssignmentID] = i
	}
	if packetErr != nil {
		fmt.Fprintf(stdout, "Reviewer questionnaire preparation gap: %v; Main/system must restore the original current packets.\n", packetErr)
	}
	for _, assignment := range plan.Assignments {
		var claims []string
		for _, id := range assignment.ClaimIDs {
			if missing[id] {
				claims = append(claims, id)
			}
		}
		if len(claims) == 0 {
			continue
		}
		row, _ := assignments[assignment.AssignmentID].(map[string]any)
		owner := stringValue(row["agent_id"])
		if owner == "" {
			if queued := stringValue(row["queued_agent_id"]); queued != "" {
				fmt.Fprintf(stdout, "dispatch pending: %s claims=%v queued Reviewer=%s reason=%v; Main follows the original queue/dependency release.\n", assignment.AssignmentID, claims, queued, row["queue_reason"])
			} else {
				fmt.Fprintf(stdout, "dispatch pending: %s claims=%v; Main selects a real Reviewer and completes original registration/dispatch when dependencies allow.\n", assignment.AssignmentID, claims)
			}
			continue
		}
		path := ""
		if i, issued := packetByAssignment[assignment.AssignmentID]; issued {
			packet := packets[i]
			binding := packet.Binding
			source, err := questionnaireSourceRoot(root, state, assignment.AssignmentID, binding.OwnerAgentID, binding.RuntimeID, binding.BaselineGeneration)
			if err == nil {
				path, _ = filepath.Abs(filepath.Join(source, filepath.FromSlash(packet.AnswerPath)))
			}
		}
		if path == "" {
			fmt.Fprintf(stdout, "Reviewer questionnaire preparation gap: %s owner=%s claims=%v; Main/system must prepare the original current packet and exact answer path.\n", assignment.AssignmentID, owner, claims)
		} else if _, err := os.Stat(path); err != nil {
			fmt.Fprintf(stdout, "Reviewer questionnaire: %s; preparation gap: %v; Main/system must restore this issued answer for %s claims=%v.\n", path, err, assignment.AssignmentID, claims)
		} else {
			fmt.Fprintf(stdout, "Reviewer questionnaire: %s; %s owner=%s pending claims=%v; fill the professional answers and save this file for the original consumer.\n", path, assignment.AssignmentID, owner, claims)
		}
	}
}

func s7RoundEntryLabel(transitionID string) string {
	switch transitionID {
	case "TR-012":
		return "S9 handoff seed"
	case "TR-022":
		return "S8 no-repair re-entry"
	case "TR-006":
		return "S6 delivery entry"
	case "TR-016":
		return "acceptance re-entry"
	default:
		if transitionID == "" {
			return "entry source unknown"
		}
		return "entry source recorded"
	}
}

// s7PendingRequiredClaims displays work whose original responsibility has not
// been consumed. It is an observation for Main, not a separate exit gate.
func s7PendingRequiredClaims(state map[string]any, plan *review.Plan, planLoaded bool) []string {
	if !planLoaded || plan == nil {
		return review.UndispositionedRequired(state)
	}
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	complete := make(map[string]bool)
	for _, assignment := range plan.Assignments {
		row, _ := assignments[assignment.AssignmentID].(map[string]any)
		if row["status"] == "consumed" || row["status"] == "result_submitted" {
			for _, id := range assignment.ClaimIDs {
				complete[id] = true
			}
		}
	}
	pending := make([]string, 0)
	for _, claim := range plan.Claims {
		if claim.Applicability == "not_applicable" || complete[claim.ClaimID] {
			continue
		}
		pending = append(pending, claim.ClaimID)
	}
	sort.Strings(pending)
	return pending
}

// readLifecycleCursor reports the active lifecycle state/phase for
// disclosure messages; both default to "unknown" so an absent lifecycle
// block never silently produces a stage-less error.
func readLifecycleCursor(state map[string]any) (string, string) {
	stage, phase := "unknown", "unknown"
	lc, _ := state["lifecycle"].(map[string]any)
	if value, _ := lc["state"].(string); value != "" {
		stage = value
	}
	if value, _ := lc["phase"].(string); value != "" {
		phase = value
	}
	return stage, phase
}

// runS7Draft scaffolds a ReviewPlan from the current runtime facts
// (L3-S7 §4.2 planner assist). Read-only: it never mutates state; the
// planner reviews the TODO markers before registering.
//
// Gating rationale (L3-S7 §11.1, blue/L3-S7-verification-round.md): the
// review round is opened by the S6→S7 transition, not by handcrafting
// `review.round`. A draft emitted outside the verification stage would
// be register-rejected (state != verification) and a Planner encouraged
// to fix the stage instead of the plan. We surface this disclosure with
// the current stage, the legal entry path (TR-006/TR-012/TR-016), and one
// next action so the agent does not invent a parallel lifecycle.
func runS7Draft(root, out string, stdout, stderr io.Writer) int {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "read runtime: %v\n", err)
		return 1
	}
	if identity := reviewmeta.FromReview(mapFieldCLI(snapshot.State, "review")); identity.PlanID == "" {
		stage, phase := readLifecycleCursor(snapshot.State)
		fmt.Fprintf(stdout,
			"current stage is %s (phase=%s); S7 enters automatically when S6 commits a TASK batch via TR-006 (bug_resolution re-entry: TR-012; acceptance re-entry: TR-016) — complete the current stage's missing work (see `loop-harness next` / `loop-harness ready`) instead of handcrafting a ReviewPlan\n",
			stage, phase)
		return 1
	}
	if stage, _ := readLifecycleCursor(snapshot.State); stage != "verification" {
		fmt.Fprintf(stdout,
			"current stage is %s; S7 ReviewPlan drafting belongs to the verification stage. Finish the current stage's work and follow its original handoff.\n",
			stage)
		return 1
	}
	if out != "" {
		requested, _ := filepath.Abs(out)
		materials, _ := filepath.Abs(filepath.Join(root, ".claude/submissions"))
		if strings.HasPrefix(requested, materials+string(filepath.Separator)) && filepath.Base(out) == "review-plan.json" && filepath.Clean(out) != filepath.Clean(reviewPlanQuestionnairePath(root, snapshot.State)) {
			fmt.Fprintln(stderr, "preserve existing ReviewPlan materials; prepare the current questionnaire at its supplied path")
			return 1
		}
	}
	// The original draft verb also restores listener-owned materials. Preserve
	// an existing professional answer while repairing its derived guide.
	if out != "" && filepath.Clean(out) == filepath.Clean(reviewPlanQuestionnairePath(root, snapshot.State)) {
		message, prepareErr := prepareReviewPlanQuestionnaireContext(root, policy.Input{Event: "IntegrationComplete", AgentID: "Main", CWD: root}, snapshot)
		fmt.Fprintln(stdout, message)
		if prepareErr != nil {
			return 1
		}
		return 0
	}
	plan, notes := review.DraftPlanForRoot(root, snapshot.State)
	if plan == nil {
		fmt.Fprintln(stderr, "prepare S7 draft: "+strings.Join(notes, "; "))
		return 1
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode draft: %v\n", err)
		return 1
	}
	if out != "" {
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintf(stderr, "write draft: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "draft ReviewPlan written to %s\n", out)
	} else {
		fmt.Fprintln(stdout, string(data))
	}
	for _, note := range notes {
		fmt.Fprintf(stderr, "note: %s\n", note)
	}
	fmt.Fprintln(stderr, "note: fill the professional planning details you know; unresolved work remains visible for Main to continue")
	return 0
}
