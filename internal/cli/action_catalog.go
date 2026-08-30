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
	fmt.Fprintln(w, "Use the first applicable action; command output names the next action. Mutations are CAS-owned and must not use `runtime transition` directly.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S7 complete verification")
	fmt.Fprintln(w, "  1. `loop-harness s7 draft --out <plan.json>`")
	fmt.Fprintln(w, "  2. `loop-harness runtime review-plan --file <plan.json>`")
	fmt.Fprintln(w, "  3. `loop-harness s7 manifest-draft --assignment <id> --out <manifest.json>`")
	fmt.Fprintln(w, "  4. `loop-harness runtime register-workgroup --manifest <manifest.json> --task-id <TASK> --task <task.md>` (compatibility: runtime register-workgroup remains the binding verb)")
	fmt.Fprintln(w, "  5. `loop-harness runtime review-result submit --assignment-id <id> --result <result.json>`")
	fmt.Fprintln(w, "  6. `loop-harness s7 status` — inspect exact Claim coverage, blockers and the next action")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S8 root-cause investigation")
	fmt.Fprintln(w, "  All S8 mutations go through `runtime investigation ...` — there is no `s8` subcommand (do not run `s8 intake`; the real CLI verb is `runtime investigation ingest --grouping-rationale <reason>`)")
	fmt.Fprintln(w, "  `loop-harness runtime investigation ingest --grouping-rationale <reason> [--emit-template <path>]` → `runtime investigation hypothesis register` → `runtime investigation dispatch`")
	fmt.Fprintln(w, "  `loop-harness runtime investigation hypothesis result` → `runtime investigation consume` → `runtime investigation contract approve`")
	fmt.Fprintln(w, "  Hypothesis route JSON examples: `docs/examples/s7-s9/`; use the command's named `--*-file` flags, then follow the returned next action.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S9 complete repair and return")
	fmt.Fprintln(w, "  `loop-harness runtime repair session open --session-id <id> --req-id <REQ> --created-by <agent> --expected-revision <N> --actor <agent>` → `runtime repair plan compile --plan-id <id> --created-by <agent> --expected-revision <N> --actor <agent>`")
	fmt.Fprintln(w, "  `loop-harness runtime repair dispatch --assignment-id <id> --agent-id <agent> --role-family <role> --agent-definition <path> --expected-revision <N> --actor <agent>`")
	fmt.Fprintln(w, "  `loop-harness runtime repair plan-report submit --file <plan-report.json> --expected-revision <N> --actor <agent>` → `runtime repair execution begin --expected-revision <N> --actor <agent>`")
	fmt.Fprintln(w, "  `loop-harness runtime repair result submit --file <result.json> --expected-revision <N> --actor <agent>`")
	fmt.Fprintln(w, "  `loop-harness runtime repair changeset compute --session-id <session> [--base-ref <ref> --head-ref <ref> --paths <p1,p2>]` → `runtime repair impact create|commit --file <impact.json>` → `runtime repair targeted create|commit|resume`")
	fmt.Fprintln(w, "  `loop-harness runtime repair handoff create --file <handoff-request.json>` → `runtime repair handoff commit --file <handoff.json> --expected-revision <N> --actor <agent>` — the only repair exit; return to a fresh S7 round")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "S10/S11")
	fmt.Fprintln(w, "  `loop-harness s10 status` → `s10 manifest validate --file <manifest.json>` → register evidence → human decision gateway")
	fmt.Fprintln(w, "  S9 never goes directly to S10; every product change returns to a fresh S7 review.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Diagnostics and compatibility")
	fmt.Fprintln(w, "  `loop-harness s7 status`, `runtime repair status`, `runtime investigation status`, `health`, `doctor`, `validate --all`")
	fmt.Fprintln(w, "  `runtime transition` is a legacy/low-level diagnostic path; normal continuation must follow the stage-specific verbs above and do not call runtime transition.")
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
