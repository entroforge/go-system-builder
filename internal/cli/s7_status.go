// s7_status.go provides a read-only board view of the current S7 review
// round: the registered ReviewPlan, every Claim's disposition, assignment
// consumption, current-round Findings, and the sealed batch / clean round
// state. It is the agent's answer to "where do I stand" without parsing
// raw loop-state JSON (L3-S7 §12.2 D7).
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// runS7Command is the `loop-harness s7` dispatcher: `status` is the
// read-only board, `draft` scaffolds a ReviewPlan, `manifest-draft`
// scaffolds the reviewer team-manifest for one plan Assignment, and
// `workspace-digest` prints the current verification-artifact digest an
// E2E cold-start ReviewResult must bind (L3-S7 §3.5).
func runS7Command(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		name := compactHelpName(args)
		if name == "" {
			name = "<status|draft|manifest-draft|workspace-digest>"
		}
		printCommandHelp(stdout, "loop-harness s7 "+name, "S7 actions: draft/register the ReviewPlan, dispatch a manifest, submit typed Results, and inspect the status board.")
		return 0
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != "draft" && args[0] != "manifest-draft" && args[0] != "workspace-digest") {
		fmt.Fprintln(stderr, "s7 requires <status|draft|manifest-draft|workspace-digest>")
		return 2
	}
	if args[0] == "workspace-digest" {
		flags := flag.NewFlagSet("s7 workspace-digest", flag.ContinueOnError)
		flags.SetOutput(stderr)
		root := flags.String("root", ".", "repository root")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		return runS7WorkspaceDigest(*root, stdout)
	}
	if args[0] == "manifest-draft" {
		flags := flag.NewFlagSet("s7 manifest-draft", flag.ContinueOnError)
		flags.SetOutput(stderr)
		root := flags.String("root", ".", "repository root")
		assignmentID := flags.String("assignment", "", "ReviewPlan Assignment id to dispatch (required)")
		out := flags.String("out", "", "write the draft manifest JSON here (default stdout)")
		if err := flags.Parse(args[1:]); err != nil {
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
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		return runS7Draft(*root, *out, stdout)
	}
	flags := flag.NewFlagSet("s7 status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	explain := flags.Bool("explain", false, "expand the wave-readiness one-liner into A-completeness / B-admission / next-verb lines")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	return runS7Status(*root, stdout, *explain)
}

// runS7Status reads the current runtime state and prints the S7 review
// board to stdout. It performs no writes. When explain is set, the
// wave-readiness one-liner is expanded into three named lines
// (RC-12 Step A): static-wave A-completeness, behavior-wave B-admission,
// and the exact next CLI verb.
func runS7Status(root string, stdout io.Writer, explain bool) int {
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
	round := 0
	roundBudgeted := false
	maxRounds := 0
	if value, ok := reviewMap["round"].(float64); ok {
		round = int(value)
	}
	// Surface the round budget so the Main agent can confirm "round N of M"
	// without grepping state. Once the current round reaches the limit, the
	// board names the human gateway; the active round is still allowed to drain.
	if cfg, ok := state["configuration"].(map[string]any); ok {
		if repair, ok := cfg["repair"].(map[string]any); ok {
			if max := integerValue(repair["max_full_review_rounds"]); max > 0 {
				fmt.Fprintf(stdout, "S7 review board (round %d of %d)\n", round, max)
				maxRounds = max
				roundBudgeted = true
			}
		}
	}
	if !roundBudgeted {
		fmt.Fprintf(stdout, "S7 review board (round %d)\n", round)
	}
	if maxRounds > 0 && round >= maxRounds {
		fmt.Fprintln(stdout, "budget: exhausted for opening another full S7 round")
		fmt.Fprintln(stdout, "human decision required: increase_budget or return_to_governance")
		fmt.Fprintln(stdout, "next: `loop-harness runtime s7-budget-decision --file <decision.json> --expected-revision <N> --actor <user>`")
	}
	if cfg, ok := state["configuration"].(map[string]any); ok {
		if repair, ok := cfg["repair"].(map[string]any); ok {
			if decision, ok := repair["last_budget_decision"].(map[string]any); ok && decision != nil {
				fmt.Fprintf(stdout, "last_budget_decision: %s evidence=%s\n", decision["decision"], decision["evidence_id"])
			}
		}
	}
	roundEntry, _ := reviewMap["round_entry"].(map[string]any)
	seedRef := stringValue(roundEntry["review_plan_seed_ref"])
	ptr := review.PlanPointerFromState(state)
	if ptr == nil {
		if seedRef != "" {
			fmt.Fprintf(stdout, "seed_projection: missing (seed=%s is present in round_entry but review.plan is absent)\n", seedRef)
			fmt.Fprintln(stdout, "next: reconcile the S9 handoff projection before drafting or registering another plan")
		}
		fmt.Fprintln(stdout, "(no ReviewPlan registered — create one and run `runtime review-plan --file <plan.json>`)")
		return 0
	}
	fmt.Fprintf(stdout, "plan: %s status=%s revision=%d e2e_coverage=%s\n",
		ptr.PlanID, ptr.Status, ptr.Revision, ptr.E2ECoverageState)
	if roundEntry != nil {
		transitionID := stringValue(roundEntry["transition_id"])
		fmt.Fprintf(stdout, "round_entry: %s (%s)\n", transitionID, s7RoundEntryLabel(transitionID))
		for _, field := range []string{"repair_handoff_ref", "change_impact_ref", "review_plan_seed_ref", "implementation_baseline_digest"} {
			if value := stringValue(roundEntry[field]); value != "" {
				fmt.Fprintf(stdout, "  %s=%s\n", field, value)
			}
		}
		if seedRef != "" {
			fmt.Fprintln(stdout, "seed_projection: present (the registered plan is the S9 seed projection; refine only through the controlled revise path)")
		}
	}

	plan, _, planErr := review.LoadPlan(root, state)
	if planErr == nil {
		// Every ReviewResult must bind this exact digest; printing it here
		// saves the reviewer from re-deriving the frozen-baseline hash
		// (the submit-time verifier rejects mismatches).
		fmt.Fprintf(stdout, "subject_digest: %s (every review-result submit must bind exactly this value)\n", review.SubjectDigest(plan))
	}
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
				agent = "(not dispatched — run `runtime register-workgroup`)"
			}
		}
		status := row["status"]
		// A blocked assignment carries a blocker_ref + blocked_at; surface
		// both so the reviewer does not have to dig through state JSON
		// (the recovery verb is `runtime agent-event blocker_resolved`).
		line := fmt.Sprintf("  %s [%s] status=%s agent=%s", id, row["lens"], status, agent)
		if status == "blocked" {
			if ref, _ := row["blocker_ref"].(string); ref != "" {
				line += fmt.Sprintf("\n    blocker_ref=%s (record `runtime agent-event --event blocker_resolved --agent-id <id> --message <file>` after the capture conditions are fixed, then resubmit)", ref)
			}
		}
		if queuedAgent, _ := row["queued_agent_id"].(string); queuedAgent != "" {
			line += fmt.Sprintf("\n    queue_reason=%v (the platform TeammateIdle hook re-wakes %s with this Assignment envelope after the lock is released)", row["queue_reason"], queuedAgent)
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

	// S7 operational metrics summary (L3-S7 §14.2 machine-collectible
	// subset). Read-only: the runtime verbs record these series.
	if summary, err := metrics.FormatS7(root, round); err == nil {
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, summary)
	}

	// Exit state and the single next action.
	fmt.Fprintln(stdout, "")
	pending := s7PendingRequiredClaims(state, plan, planErr == nil)
	if batch, _ := reviewMap["observation_batch"].(map[string]any); batch != nil {
		fmt.Fprintf(stdout, "observation_batch: sealed as %s (%d findings) — the next PreToolUse auto-commits TR-008 (do not invoke the transition CLI)\n",
			batch["batch_id"], len(batchFindingIDs(batch)))
		return 0
	}
	if ptr.Status == "clean" {
		fmt.Fprintln(stdout, "clean round: machine CleanRound registered — the next PreToolUse auto-commits TR-009 (do not invoke the transition CLI)")
		return 0
	}
	if len(pending) > 0 {
		fmt.Fprintf(stdout, "pending required claims: %d — next: dispatch/consume results via `runtime review-result submit`\n", len(pending))
	} else {
		fmt.Fprintln(stdout, "all required claims dispositioned; round consumer closes on the next submit")
	}
	// Wave readiness (RC-12): when a behavior-wave Assignment exists, name
	// the gate. The compact one-liner renders only while the static set is
	// unsettled; `--explain` always renders, so an agent can confirm the
	// B-admission flip after the last static claim settles. Read-only: no
	// new state is consulted.
	if planErr == nil && plan != nil && hasBehaviorWaveAssignment(plan) {
		remaining := review.RemainingStaticClaims(state, plan)
		if explain {
			printS7WaveExplain(stdout, state, plan, remaining)
		} else if !review.StaticClaimsSettled(state, plan) {
			fmt.Fprintf(stdout, "wave readiness: behavior dispatch is blocked — %d static-wave claim(s) still awaiting a disposition (L3-S7 §5.2-5.3)\n", remaining)
		}
	}
	return 0
}

// printS7WaveExplain renders the three-line --explain expansion of the wave
// gate (RC-12 Step A): A-completeness counts the static-wave claims that
// still need a final disposition, B-admission states whether the behavior
// wave may dispatch right now, and next-verb names the exact CLI action
// that unblocks or advances it. Read-only: it reuses the registered plan
// and runtime projection; no new state is consulted.
func printS7WaveExplain(stdout io.Writer, state map[string]any, plan *review.Plan, remaining int) {
	fmt.Fprintf(stdout, "explain A-completeness: %d required static-wave claim(s) still awaiting a pass/finding/blocked disposition (static settled=%t)\n",
		remaining, review.StaticClaimsSettled(state, plan))
	if remaining > 0 {
		fmt.Fprintln(stdout, "explain B-admission: behavior-wave dispatch is BLOCKED — `runtime register-workgroup` rejects a behavior Assignment until A settles (L3-S7 §5.2-5.3)")
		fmt.Fprintln(stdout, "explain next-verb: `loop-harness runtime review-result submit --assignment-id <static-assignment-id> --result <result.json>` to settle the remaining static claims, then re-run `loop-harness s7 status --explain`")
		return
	}
	fmt.Fprintln(stdout, "explain B-admission: behavior-wave dispatch is ADMITTED — every required static claim has a final disposition")
	fmt.Fprintln(stdout, "explain next-verb: `loop-harness s7 manifest-draft --assignment <behavior-assignment-id> --out <manifest.json>` then `loop-harness runtime register-workgroup --manifest <manifest.json> --task-id <TASK> --task <task.md>`")
}

// hasBehaviorWaveAssignment reports whether the plan dispatches any
// behavior-wave Assignment (the wave gated behind static-claim settlement).
func hasBehaviorWaveAssignment(plan *review.Plan) bool {
	for _, assignment := range plan.Assignments {
		if assignment.ExecutionWave == "behavior" {
			return true
		}
	}
	return false
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

// s7PendingRequiredClaims reads the plan as the coverage authority and the
// runtime projection as the consumption authority. This matters during a
// partially projected/recovered board: an absent state.review.claims row is
// still a pending required Claim, not evidence that the round is complete.
func s7PendingRequiredClaims(state map[string]any, plan *review.Plan, planLoaded bool) []string {
	if !planLoaded || plan == nil {
		return review.UndispositionedRequired(state)
	}
	dispositions := review.Dispositions(state)
	pending := make([]string, 0)
	for _, claim := range plan.Claims {
		if claim.Applicability == "not_applicable" {
			continue
		}
		disposition, ok := dispositions[claim.ClaimID]
		if !ok {
			pending = append(pending, claim.ClaimID)
			continue
		}
		switch disposition.Disposition {
		case "pass", "finding", "blocked":
		default:
			pending = append(pending, claim.ClaimID)
		}
	}
	sort.Strings(pending)
	return pending
}

func batchFindingIDs(batch map[string]any) []any {
	ids, _ := batch["finding_ids"].([]any)
	return ids
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
func runS7Draft(root, out string, stdout io.Writer) int {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintf(stdout, "read runtime: %v\n", err)
		return 1
	}
	round := 0
	if reviewMap, ok := snapshot.State["review"].(map[string]any); ok {
		if value, ok := reviewMap["round"].(float64); ok {
			round = int(value)
		}
	}
	if round < 1 {
		stage, phase := readLifecycleCursor(snapshot.State)
		fmt.Fprintf(stdout,
			"current stage is %s (phase=%s); S7 enters automatically when S6 commits a TASK batch via TR-006 (bug_resolution re-entry: TR-012; acceptance re-entry: TR-016) — complete the current stage's missing work (see `loop-harness next` / `loop-harness ready`) instead of handcrafting a ReviewPlan\n",
			stage, phase)
		return 1
	}
	if stage, _ := readLifecycleCursor(snapshot.State); stage != "verification" {
		fmt.Fprintf(stdout,
			"current stage is %s; S7 (verification) ReviewPlan drafting is only legal while the lifecycle stage is `verification` — round=%d is open but the ReviewPlan register-verb (`runtime review-plan --file <plan.json>`) will reject with `a ReviewPlan can only be registered in the verification stage`. Finish the current stage's missing work first.\n",
			stage, round)
		return 1
	}
	plan, notes := review.DraftPlanForRoot(root, snapshot.State, round)
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintf(stdout, "encode draft: %v\n", err)
		return 1
	}
	if out != "" {
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintf(stdout, "write draft: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "draft ReviewPlan written to %s\n", out)
	} else {
		fmt.Fprintln(stdout, string(data))
	}
	for _, note := range notes {
		fmt.Fprintf(stdout, "note: %s\n", note)
	}
	fmt.Fprintln(stdout, "note: replace every TODO(planner) marker (target/assertion/oracle/method/na_rationale) with real facts before registration — the registration gate rejects the literal marker; if a whole lens ends up with zero required Claims, fill coverage_justification instead")
	return 0
}

// runS7WorkspaceDigest prints the current verification-artifact digest of
// the registered plan's cold-start workspace. An E2E ReviewResult must bind
// exactly this value as verification_artifact_digest (L3-S7 §3.5); computing
// it by hand is error-prone, so the submit-time error message points here.
func runS7WorkspaceDigest(root string, stdout io.Writer) int {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		fmt.Fprintf(stdout, "read runtime: %v\n", err)
		return 1
	}
	ptr := review.PlanPointerFromState(snapshot.State)
	if ptr == nil || ptr.VerificationArtifactWorkspace == "" {
		fmt.Fprintln(stdout, "no verification artifact workspace is pinned (the plan is not registered or e2e_coverage_state is not cold_start); E2E results do not bind a workspace digest")
		return 0
	}
	digest, err := review.WorkspaceDigest(root, ptr.VerificationArtifactWorkspace)
	if err != nil {
		fmt.Fprintf(stdout, "compute workspace digest: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s  %s\n", digest, ptr.VerificationArtifactWorkspace)
	return 0
}
