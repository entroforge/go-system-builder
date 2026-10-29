package cli

import (
	"fmt"
	"io"
	"strings"
)

// wantsHelp is checked before subcommand-specific flag parsing. The standard
// flag package writes ErrHelp to stderr and returns exit 2 for nested commands;
// agents need help to be a successful, discoverable action instead.
func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

func printCommandHelp(w io.Writer, usage, detail string) {
	fmt.Fprintf(w, "Usage: %s\n\n", usage)
	if detail != "" {
		fmt.Fprintln(w, detail)
	}
	fmt.Fprintln(w, "Run `loop-harness actions` for the canonical stage action sequence and compatibility notes.")
}

// runActions is intentionally a small, stable index rather than another
// scheduler. It is the one agent-facing map from stage responsibility to the
// runtime verb that owns the mutation.
func runActions(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && !wantsHelp(args) {
		fmt.Fprintf(stderr, "actions: unknown option %q\n", args[0])
		return 2
	}
	printActionCatalog(stdout)
	return 0
}

func printActionCatalog(w io.Writer) {
	fmt.Fprintln(w, "Canonical Agent action catalog")
	fmt.Fprintln(w, "Use the exact current task brief or questionnaire. Main saves its own professional answers through their original consumers. For S7/S9, Workers save their original answers for PreToolUse observation, with successful PostToolUse and Stop/SubagentStop fallback through the original consumers. S5/S6/S8 retain their original SubagentStop reception. Main owns real dispatch, integration, required checks and faithfully records actual human decisions. Do not use `runtime transition` directly.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S6 Builder implementation")
	fmt.Fprintln(w, "  1. Main selects an eligible reviewed TASK, runs `s6 prepare --task <TASK> --role <Builder-role>` and passes its exact task brief into Claude Code native Agent with worktree isolation (`worktree.baseRef: head`). No platform IDs, Team or framework launcher.")
	fmt.Fprintln(w, "  2. Builder saves its professional plan, continues implementation and actual TASK self-checks, commits products and saves Result in its own worktree. No generic begin, activation or per-tool receipt. Return the exact brief and absolute Result reference; state actual blockers in the native return. SubagentStart supplies short context; SubagentStop calls the original Result consumer.")
	fmt.Fprintln(w, "  3. Main runs `runtime task-integrate --assignment-id <id>` for actual scope/commit validation, merge, required Main checks, ACK and cleanup. The exact verified batch passes the original Gate/TR-006 into S7.")
	fmt.Fprintln(w, "  4. Native code/report correction uses `s6 prepare --task <TASK> --role <role> --revise-assignment <id> --revision-intent-key <stable-key>` through the original revision Writer, followed by native execution. Frozen s6-v1.2/workspace assignments retain their original `runtime workspace pending` recovery only; never relaunch one responsibility through both routes.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S7 complete verification")
	fmt.Fprintln(w, "  1. Main completes the Hook-provided ReviewPlan questionnaire and saves it for the original RegisterPlan consumer.")
	fmt.Fprintln(w, "  2. Main creates manifests for actual responsibilities with `s7 manifest-draft --assignment <id>` and registers them with `runtime register-workgroup`, then actually dispatches native Agents with the exact view/answer paths. Each Reviewer saves its professional answer; PreToolUse observes the ready original, with successful PostToolUse and Stop/SubagentStop fallback through the original ReviewResult consumer. No extra reference return or submission is required for reception.")
	fmt.Fprintln(w, "  3. Main follows current Hook and original receipt guidance for Claim coverage, blockers, and the next action; `s7 status` is optional diagnosis. The original consumer seals Findings or records a clean round.")
	fmt.Fprintln(w, "  Site-lost recovery: the listener names the original Assignment, Reviewer, blocker source and reason. Main restores the actual conditions and uses the supplied `runtime agent-event --event blocker_resolved --agent-id ... --body` prefix with its real restoration explanation. The entry binds the original machine facts and existing lifecycle consumer; then resume the same Reviewer answer. Existing --message remains supported. Repeating `s7 manifest-draft --out` preserves the existing manifest and recovers its actual registration commands.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S8 root-cause investigation")
	fmt.Fprintln(w, "  1. After S7 seals an ObservationBatch, the next Main PreToolUse commits TR-008 and supplies the exact S8 intake path. Main fills its provisional grouping rationale without asserting a common root cause and saves it; the original ingest consumer creates the Case. `runtime investigation ingest --root <root> --grouping-rationale <evidence-based grouping>` remains an explicit recovery entry.")
	fmt.Fprintln(w, "  2. Main uses the supplied hypothesis/dispatch questions or the original `runtime investigation hypothesis register` and `runtime investigation dispatch` commands to record real responsibilities, then actually dispatches native Investigators with exact references. Each Investigator saves and returns its answer; SubagentStop invokes the original Case consumer.")
	fmt.Fprintln(w, "  3. Main follows `runtime investigation status`, routes the Case using the supported results and evidence, then uses the original RepairContract approval route. A human approval or bounded-repair grant must be real; a technical review is not a human decision.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S9 complete repair and return")
	fmt.Fprintln(w, "  1. Contract approval prepares the original RepairSession, RepairPlan and Main division questions. Main saves the actual Builder division or uses original `runtime repair dispatch`, then carries each prepared brief into native Agent/worktree isolation. Each Builder saves its private professional PlanReport before implementation and continues. PreToolUse observes ready originals, with successful PostToolUse and Stop/SubagentStop fallback through the original PlanReport consumer and original Result consumer.")
	fmt.Fprintln(w, "  2. Builders perform repair and self-checks and save the private Result; no extra path return or submission is required for reception. Main follows the supplied `runtime workspace integrate` command with necessary --check-command values; do not wait for Final before integration or redispatch an accepted candidate.")
	fmt.Fprintln(w, "  3. Main saves the Hook-provided ChangeImpact answer for its original consumer. Main actually dispatches an independent verifier with the exact TargetedReverification answer path; the verifier saves it for the same natural-event reception through the original independent consumer.")
	fmt.Fprintln(w, "  4. Once all necessary work is complete, its original outlet runs the existing handoff consumer and prepares the fresh S7 seed. Follow any concrete remaining duty; do not wait for another Hook or write a Handoff report. Main plans and dispatches the fresh complete S7 round.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  S9 reception recovery only: when current feedback supplies the exact original native flat input, saved typed answer or Targeted answer, Main runs the supplied `runtime stage-answer consume --root <Main-root> --path <exact-original-input>` through the original adapter/consumer. Preserve accepted PlanReport and original responsibility; no repeated Worker Start/Stop or rewritten answer. Missing exact input is a Main/system preparation gap. Accepted candidate continues Main integration/checks; accepted Targeted continues the original Handoff. Natural reception adds no manual submission. Other stages retain their named recovery boundaries.")
	fmt.Fprintln(w, "  S9 recovery only: missing ChangeImpact uses `runtime repair impact prepare --root <project-root>`; accepted Targeted with unfinished Handoff/S7 preparation uses `runtime repair handoff complete --root <project-root>`. Preserve accepted work; neither is an extra normal author submission.")
	fmt.Fprintln(w, "  S7 recovery: `runtime review-plan revive --help` and `runtime review-plan revise --help` explain unchanged-baseline revival versus a controlled Claim revision. Accepted CleanRound continuation uses `runtime review-continue`; missing current S10 materials use `runtime audit-prepare`.")
	fmt.Fprintln(w, "S10/S11")
	fmt.Fprintln(w, "  S10: use `s10 status` for read-only progress. Fill the Hook-provided acceptance/audit manifest and save it once; the original consumer validates it, derives machine metadata, and records the evidence.")
	fmt.Fprintln(w, "  S11: present the actual materials and wait for the human's explicit choice. Main faithfully fills disposition and optional reason in the exact pending application and saves normally. An empty choice remains a waiting draft; no release command or second approval is required.")
	fmt.Fprintln(w, "  S9 never goes directly to S10; every product change returns to a fresh S7 review.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Diagnostics and compatibility")
	fmt.Fprintln(w, "  Read-only status: `s7 status`, `runtime investigation status`, `runtime repair status`, `s10 status`, `health`, `doctor`, `validate --all`.")
	fmt.Fprintln(w, "  Direct `runtime review-plan`, `runtime review-result submit`, `runtime investigation hypothesis result`, `runtime repair session open`, `runtime repair plan compile`, `s10 manifest validate/render`, `runtime evidence add`, and direct S9 create/submit/commit verbs are diagnostic, compatibility, or recovery interfaces—not a second delivery after saving a Hook-provided answer.")
	fmt.Fprintln(w, "  `runtime investigation consume` is used only when the current Case route explicitly requires that original route consumer. `runtime transition` is legacy/low-level; normal continuation follows the current stage instructions.")
	fmt.Fprintln(w, "  Help is non-mutating: `loop-harness s7 --help`, `s10 --help`, or `runtime repair dispatch --help` exits 0 and points back here.")
}

func compactHelpName(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

// These are recovery operations, not alternate normal authoring steps.
func printReviewPlanRecoveryHelp(w io.Writer) {
	printCommandHelp(w, "loop-harness runtime review-plan [revive|revise] --root <project-root>", "Normal registration saves the supplied Main questionnaire. revive rechecks the unchanged pinned baseline and restores a stale plan only when its sources match; it does not restore files or move the baseline. revise makes a controlled professional plan revision from an actual consumed Result/Finding; it cannot replace the pinned S6 product baseline.")
	fmt.Fprintln(w, "Example: loop-harness runtime review-plan revive --root /path/to/project")
	fmt.Fprintln(w, "Example: loop-harness runtime review-plan revise --root /path/to/project --file /path/to/ReviewPlan-v2.json --source-ref finding-actual-id --affected-surface internal/example")
	fmt.Fprintln(w, "--file is a path to a complete revised ReviewPlan JSON file, not inline JSON or Markdown. --source-ref names the actual consumed Result/Finding that motivates the revision. --affected-surface bounds the changed Claims; unchanged Claims retain their observations. Replace example paths/ID with original current materials.")
	fmt.Fprintln(w, "Before source restoration, preserve current edits and added/untracked files separately. Inspect the exact pinned commit and affected paths; never use blanket reset/clean/restore or delete files absent from that commit. A new product baseline requires the original new-round/rework path.")
}

func s7PinnedSourceRecovery(root string) string {
	return "Run `loop-harness runtime review-plan frozen-diff --root " + s7CaptureShellQuote(root) + "` first for the read-only restore/preserve list; pinned SHAs come from the plan, never hand-supply a hash. Restore only listed drifted/missing pinned sources; preserve current edits and added/untracked files and never delete files absent from the pinned commit or run blanket reset/clean. After actual sources match, use `loop-harness runtime review-plan revive --root " + s7CaptureShellQuote(root) + "`. Controlled Claim changes use `loop-harness runtime review-plan revise --help` with a JSON file path and consumed Result/Finding; it cannot move the pinned S6 baseline. Real product changes require the original rework/new-round path."
}
