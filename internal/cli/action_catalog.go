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
	fmt.Fprintln(w, "Use the exact current task brief or questionnaire. Main saves its own professional answers through their original consumers. For S7/S8/S9, Workers save their original answers for PreToolUse observation, with successful PostToolUse and Stop/SubagentStop fallback through the original consumers. S5/S6 Workers use their original SubagentStop reception. Main owns real dispatch, integration, required checks and faithfully records actual human decisions. Do not use `runtime transition` directly.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S6 Builder implementation")
	fmt.Fprintln(w, "  1. Main selects an eligible reviewed TASK, runs `s6 prepare --task <TASK> --role <Builder-role>` and passes its exact task brief into Claude Code native Agent with worktree isolation (`worktree.baseRef: head`). No platform IDs, Team or framework launcher.")
	fmt.Fprintln(w, "  2. Builder saves its professional plan, continues implementation and actual TASK self-checks, commits products and saves Result in its own worktree. No generic begin, activation or per-tool receipt. Return the exact brief and absolute Result reference; state actual blockers in the native return. SubagentStart supplies short context; SubagentStop calls the original Result consumer.")
	fmt.Fprintln(w, "  3. Main runs `runtime task-integrate --assignment-id <id>` for actual scope/commit validation, merge, required Main checks, ACK and cleanup. The exact verified batch passes the original Gate/TR-006 into S7.")
	fmt.Fprintln(w, "  4. Code/report correction uses `s6 prepare --task <TASK> --role <role> --revise-assignment <id>` through the original revision Writer, followed by native execution.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S7 complete verification")
	fmt.Fprintln(w, "  1. Main completes the Hook-provided ReviewPlan questionnaire and saves it for the original RegisterPlan consumer.")
	fmt.Fprintln(w, "  2. Main creates manifests for actual responsibilities with `s7 manifest-draft --assignment <id>` and registers them with `runtime register-workgroup`, then actually dispatches native Agents with the exact view/answer paths. Each Reviewer saves its professional answer; PreToolUse observes the ready original, with successful PostToolUse and Stop/SubagentStop fallback through the original ReviewResult consumer. No extra reference return or submission is required for reception.")
	fmt.Fprintln(w, "  3. Main follows current Hook and original receipt guidance for Claim coverage, blockers, and the next action; `s7 status` is optional diagnosis. The original consumer seals Findings or records a clean round.")
	fmt.Fprintln(w, "  If review work is blocked, record the actual limitation in the original answer and tell Main; continue the same responsibility naturally after the condition is resolved.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S8 root-cause investigation")
	fmt.Fprintln(w, "  1. After S7 seals an ObservationBatch, the next Main PreToolUse commits TR-008 and supplies the exact S8 intake path. Main fills its provisional grouping rationale without asserting a common root cause and saves it; the original ingest consumer creates the Case. `runtime investigation ingest --root <root> --grouping-rationale <evidence-based grouping>` remains an explicit recovery entry.")
	fmt.Fprintln(w, "  2. Main uses the supplied hypothesis/dispatch questions or the original `runtime investigation hypothesis register` and `runtime investigation dispatch` commands to record real responsibilities, then actually dispatches native Investigators with exact references. Each Investigator saves its answer; natural events invoke the original Case consumer.")
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
	fmt.Fprintln(w, "  S7 recovery: continue the current ReviewPlan questionnaire naturally. Accepted results with an unfinished exit use `runtime review-continue`; missing S10 materials use `runtime audit-prepare`.")
	fmt.Fprintln(w, "S10/S11")
	fmt.Fprintln(w, "  S10: use `s10 status` for read-only progress. Fill the Hook-provided acceptance/audit manifest and save it once; the original consumer validates it, derives machine metadata, and records the evidence.")
	fmt.Fprintln(w, "  S11: present the actual materials and wait for the human's explicit choice. Main faithfully fills disposition and optional reason in the exact pending application and saves normally. An empty choice remains a waiting draft; no release command or second approval is required.")
	fmt.Fprintln(w, "  S9 never goes directly to S10; every product change returns to a fresh S7 review.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Diagnostics and recovery")
	fmt.Fprintln(w, "  Read-only status: `s7 status`, `runtime investigation status`, `runtime repair status`, `s10 status`, `health`, `doctor`, `validate --all`.")
	fmt.Fprintln(w, "  Direct `runtime investigation hypothesis result`, `runtime repair session open`, `runtime repair plan compile`, `s10 manifest validate/render`, `runtime evidence add`, and direct S9 create/submit/commit verbs are diagnostic or recovery interfaces—not a second delivery after saving a Hook-provided answer.")
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
func s7PinnedSourceRecovery(root string) string {
	return "Main uses the current ReviewPlan questionnaire and available materials to express any actual revision; normal saves are observed by the original consumer."
}
