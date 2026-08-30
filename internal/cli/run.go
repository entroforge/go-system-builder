package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/adapter"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/audit"
	"github.com/entroforge/go-system-builder/internal/change"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	impactanalysis "github.com/entroforge/go-system-builder/internal/impact"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/releasegraph"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/verification"
)

// formatFailure renders a CLI command's failure to stderr in the form
// `<cmd>: <err>` and, when the error wraps a transition-engine guard failure,
// appends ` See .claude/bin/loop-harness.md#<lowercase(rule_id)>.` so a caller
// hitting a gate failure can deep-link straight to the spec. Mirrors the
// convention already enforced for hook payloads in
// `internal/hook/adapter.go` `message()`.
//
// A `runtime.ErrStaleRevision` (or any error wrapping it) is enriched with a
// concrete next-action so the caller can recover instead of doing
// half-experiments: read the current revision with `loop-harness status --root
// <root>` and retry with `--expected-revision <N>`. Repeated divergence means
// an actor is committing concurrently; the durable cure is
// `loop-harness runtime reconcile`.
//
// Recognized rule-id sources (in order of preference):
//   - `guard <NAME> failed: ...`         → id = NAME.
//   - `guard <NAME> is not registered`   → id = NAME.
//
// Returns the formatted line; caller is responsible for writing to stderr.
func formatFailure(cmd string, err error) string {
	msg := err.Error()
	if id := extractRuleID(msg); id != "" {
		return fmt.Sprintf("%s: %s See %s#%s.", cmd, msg, transition.ManualTargetPath(), strings.ToLower(id))
	}
	if errors.Is(err, runtime.ErrStaleRevision) {
		return fmt.Sprintf("%s: %s. Next: run `loop-harness status --root <root>` to read the current revision and retry with `--expected-revision <N>`. If revisions diverge repeatedly, an actor is committing concurrently; resolve with `loop-harness runtime reconcile`.", cmd, msg)
	}
	if errors.Is(err, runtime.ErrStaleRuntimeIdentity) {
		return fmt.Sprintf("%s: %s. The runtime identity changed at a lifecycle boundary; reread status and rebuild the transition request against the current runtime.", cmd, msg)
	}
	return fmt.Sprintf("%s: %s", cmd, msg)
}

// extractRuleID scans a transition-engine error string for a known
// guard-failure pattern and returns the rule id (the guard name). Returns ""
// when no rule id can be located — callers fall back to plain error output.
func extractRuleID(msg string) string {
	for _, prefix := range []string{"guard ", "guard\t"} {
		idx := strings.Index(msg, prefix)
		if idx < 0 {
			continue
		}
		rest := msg[idx+len(prefix):]
		// "guard NAME failed:" or "guard NAME is not registered"
		for _, sep := range []string{" failed:", " is not registered"} {
			if end := strings.Index(rest, sep); end > 0 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}
	return ""
}

// bindUsage wires a flag.FlagSet's Usage function so that --help / -h prints
// the canonical flag defaults plus a pointer to the gate-level manual.
// Invoked once per subcommand right after SetOutput so that every `flag -h`
// path is consistent. Mirrors the manual-anchor convention enforced for hook
// payloads in `internal/hook/adapter.go`.
func bindUsage(flags *flag.FlagSet, label string) {
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage of %s:\n", label)
		flags.PrintDefaults()
		fmt.Fprintf(flags.Output(), "\nSee %s (gate-level specification).\n", transition.ManualTargetPath())
	}
}

// printTopLevelUsage renders the top-level help text to stdout. Triggered by
// `loop-harness --help` / `-h` / `help`; mirrors the inline usage printed on
// `len(args) == 0` (which goes to stderr because that path indicates a usage
// error).
func printTopLevelUsage(stdout io.Writer) {
	fmt.Fprintln(stdout, "Usage: loop-harness <command> [flags]")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Top-level commands:")
	fmt.Fprintln(stdout, "  init        Initialize .claude/ harness state for a repository")
	fmt.Fprintln(stdout, "  req         Locked REQ operations (req bind)")
	fmt.Fprintln(stdout, "  status      Render current stage projection (read-only; coarse)")
	fmt.Fprintln(stdout, "  next        Render next transition + missing preconditions (coarse)")
	fmt.Fprintln(stdout, "  ready       Dry-run current Quality Gate checklist (diagnostics)")
	fmt.Fprintln(stdout, "  validate    Validate runtime + journal against schema")
	fmt.Fprintln(stdout, "  dry-run     Render an applied transition without writing")
	fmt.Fprintln(stdout, "  hook        Hook adapter entrypoints (PreToolUse, Stop, etc.)")
	fmt.Fprintln(stdout, "  doctor      Structural schema / manual / policy_ref checks (not runtime health)")
	fmt.Fprintln(stdout, "  health      Runtime history signals and Hook timing (use --fail-on-degraded in CI)")
	fmt.Fprintln(stdout, "  actions     Canonical Agent action catalog and compatibility notes")
	fmt.Fprintln(stdout, "  runtime     Runtime helpers (including investigation intake, S9 repair transactions and terminal rollover)")
	fmt.Fprintln(stdout, "  team        Team manifest + responsibility checks")
	fmt.Fprintln(stdout, "  s6          S6 workgroup scaffolding + TASK generation")
	fmt.Fprintln(stdout, "  s7          S7 ReviewPlan drafting, manifest scaffold and status (read-only)")
	fmt.Fprintln(stdout, "  s10         Acceptance/release-audit manifest validation and status (read-only)")
	fmt.Fprintln(stdout, "  tasks       TASK discovery/coverage listing (read-only)")
	fmt.Fprintln(stdout, "  contracts   Locked contract set inspection (read-only)")
	fmt.Fprintln(stdout, "  capture     Observation capture buffer (console/network/step evidence)")
	fmt.Fprintln(stdout, "  impact      Evidence invalidation analysis")
	fmt.Fprintln(stdout, "  verification Verification round evaluators")
	fmt.Fprintln(stdout, "  release-graph Release-graph topological assertions")
	fmt.Fprintln(stdout, "  e2e-coverage  Score E2E scenario inventory fidelity (REQ-039)")
	fmt.Fprintln(stdout, "  scenario      Generate and validate module fact-driven scenario packages")
	fmt.Fprintln(stdout, "  manual      Render the gate-level manual")
	fmt.Fprintln(stdout, "  explain     Per-transition details (explain <TR-xxx>)")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Manual: see %s (gate-level specification).\n", transition.ManualTargetPath())
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness <init|req|status|next|ready|validate|dry-run|hook|doctor|health|actions|runtime|team|s6|s7|tasks|contracts|capture|impact|verification|release-graph|e2e-coverage|scenario|s10|manual|explain>")
		fmt.Fprintln(stderr, "manual:  see .claude/bin/loop-harness.md (gate-level specification)")
		fmt.Fprintln(stderr, "explain: loop-harness explain <TR-xxx> (per-transition details)")
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printTopLevelUsage(stdout)
		return 0
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "req":
		return runREQ(args[1:], stdout, stderr)
	case "status":
		return runProjection(args[1:], false, stdout, stderr)
	case "next":
		return runProjection(args[1:], true, stdout, stderr)
	case "ready":
		return runReady(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "dry-run":
		return runDryRun(args[1:], stdout, stderr)
	case "hook":
		return runHook(args[1:], stdin, stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "health":
		return runHealth(args[1:], stdout, stderr)
	case "actions":
		return runActions(args[1:], stdout, stderr)
	case "runtime":
		return runRuntime(args[1:], stdout, stderr)
	case "team":
		return runTeam(args[1:], stdout, stderr)
	case "impact":
		return runImpact(args[1:], stdout, stderr)
	case "verification":
		return runVerification(args[1:], stdout, stderr)
	case "release-graph":
		return runReleaseGraph(args[1:], stdout, stderr)
	case "manual":
		return runManual(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "e2e-coverage":
		return runE2ECoverage(args[1:], stdout, stderr)
	case "scenario":
		return runScenario(args[1:], stdout, stderr)
	case "contracts":
		return runContracts(args[1:], stdout, stderr)
	case "s6":
		return runS6Command(args[1:], stdout, stderr)
	case "capture":
		return runCapture(args[1:], stdin, stdout, stderr)
	case "s7":
		return runS7Command(args[1:], stdout, stderr)
	case "s10":
		return runS10Command(args[1:], stdout, stderr)
	case "tasks":
		return runTasks(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runREQ(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "bind" && args[0] != "list" && args[0] != "unbind" && args[0] != "amend") {
		fmt.Fprintln(stderr, "req requires <bind|list|unbind|amend>")
		return 2
	}
	if args[0] == "list" {
		return runREQList(args[1:], stdout, stderr)
	}
	if args[0] == "unbind" {
		return runREQUnbind(args[1:], stdout, stderr)
	}
	if args[0] == "amend" {
		return runREQAmend(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("req bind", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "req bind")
	root := flags.String("root", ".", "repository root")
	reqPath := flags.String("req", "", "locked REQ path (default: auto-discover the sole bindable REQ)")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	asJSON := flags.Bool("json", false, "machine-readable state output")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *approvedBy == "" {
		if identity := detectGitIdentity(*root); identity != "" {
			fmt.Fprintf(stderr, "req bind requires --approved-by (detected git identity %q; rerun with --approved-by %q)\n", identity, identity)
		} else {
			fmt.Fprintln(stderr, "req bind requires --approved-by <human identity>")
		}
		return 2
	}
	// Info lines go to stderr in --json mode so stdout stays a single valid
	// JSON document for scripts.
	infoW := io.Writer(stdout)
	if *asJSON {
		infoW = stderr
	}
	// Auto-init: a missing runtime is not an error state to route around —
	// binding is the first mutating command a human runs on a fresh project.
	if _, err := os.Stat(filepath.Join(*root, ".claude", "loop-state.json")); os.IsNotExist(err) {
		if err := writeInactiveRuntime(*root); err != nil {
			fmt.Fprintln(stderr, formatFailure("req bind", fmt.Errorf("auto-init runtime: %w", err)))
			return 1
		}
		fmt.Fprintln(infoW, "initialized fresh runtime at .claude/loop-state.json")
	} else if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", fmt.Errorf("inspect runtime: %w", err)))
		return 1
	}
	if *reqPath == "" {
		candidates := bindableOnly(*root)
		switch len(candidates) {
		case 1:
			*reqPath = candidates[0].Path
			fmt.Fprintf(infoW, "discovered sole bindable REQ: %s\n", *reqPath)
		case 0:
			for _, s := range classifyRequirements(*root) {
				fmt.Fprintf(stderr, "  %-11s %-8s %s\n", s.ID, s.Status, s.Note)
			}
			fmt.Fprintln(stderr, "req bind: no bindable REQ (status must be locked and lifecycle open); lock one in S0 first, see `req list`")
			return 1
		default:
			fmt.Fprintln(stderr, "req bind: multiple bindable REQs — uniqueness is a human decision:")
			for _, s := range candidates {
				fmt.Fprintf(stderr, "  %s\n", s.Path)
			}
			fmt.Fprintln(stderr, "rerun with --req <path>")
			return 2
		}
	}
	data, err := os.ReadFile(filepath.Join(*root, *reqPath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", err))
		return 1
	}
	status := markdownField(string(data), "状态", "Status")
	version := markdownField(string(data), "版本", "Version")
	if status != "locked" || version == "" {
		fmt.Fprintln(stderr, "req bind: the REQ top blockquote must declare `状态：locked` (or `Status: locked`) and `版本：<semver>` — see docs/requirements/REQ-template.md")
		return 1
	}
	id := strings.TrimSuffix(filepath.Base(*reqPath), filepath.Ext(*reqPath))
	if !strings.HasPrefix(id, "REQ-") {
		fmt.Fprintln(stderr, "req bind: filename must start with REQ-")
		return 1
	}
	statePath := filepath.Join(*root, ".claude/loop-state.json")
	journalPath := filepath.Join(*root, ".claude/loop-events.jsonl")
	// REQ bind is an explicit mutation command. Its writer is therefore the
	// recovery boundary for a pending rollover/commit; read-only projections
	// must report the marker instead of repairing it implicitly.
	snapshot, err := runtime.NewWriter(statePath, journalPath, *root, semantic.RuntimeCandidateValidator{}).Snapshot()
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", fmt.Errorf("read runtime revision: %w", err)))
		return 1
	}
	// Preflight: a REQ is already bound — TR-001 would refuse on source
	// state; name the two legal routes instead of the raw rejection. (An
	// inactive runtime still carrying a bound_req is the rollover-pending
	// case, handled by the recovery preflight below with its own wording.)
	if lifecycleState, _ := snapshot.State["lifecycle"].(map[string]any); lifecycleState != nil {
		if state, _ := lifecycleState["state"].(string); state != "" && state != "inactive" {
			if bound, _ := snapshot.State["bound_req"].(map[string]any); bound != nil {
				if boundID, _ := bound["id"].(string); boundID != "" {
					fmt.Fprintf(stderr, "req bind: %s is already bound (TR-001 requires an inactive runtime) — to change the requirement: `runtime pause` then `req amend --req <new version of %s>`; to abandon it: `req unbind`\n", boundID, boundID)
					return 1
				}
			}
		}
	}
	// Preflight: refuse to burn a drifted control-plane fingerprint into a
	// fresh baseline. Parse-level drift already fails closed above (catalog
	// load); this catches a valid-but-changed definition or policy file.
	if hint := controlPlaneDrift(*root, snapshot.State); hint != "" {
		fmt.Fprintln(stderr, formatFailure("req bind", fmt.Errorf("control plane drifted: %s", hint)))
		return 1
	}
	now := time.Now().UTC()
	shaHex := fmt.Sprintf("%x", sha256.Sum256(data))
	next, err := transition.Apply(*root, statePath, journalPath, transition.Request{
		TransitionID: "TR-001", ExpectedRevision: snapshot.Revision, ExpectedRuntimeID: "loop-inactive", Actor: "user",
		Evidence: map[string]string{
			"req_lock_record":           *reqPath + "@" + shaHex,
			"loop_authorization_record": "approved-by:" + *approvedBy,
		},
		REQ: &transition.LockedREQ{ID: id, Path: *reqPath, Version: version, SHA256: shaHex, ApprovedBy: *approvedBy, ApprovedAt: now.Format(time.RFC3339Nano)}, OccurredAt: now,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("req bind", err))
		return 1
	}
	if *asJSON {
		return encodeJSON(stdout, next.State)
	}
	printBindConfirmation(stdout, next.State)
	return 0
}

func runREQList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("req list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "req list")
	root := flags.String("root", ".", "repository root")
	asJSON := flags.Bool("json", false, "machine-readable output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	summaries := classifyRequirements(*root)
	if *asJSON {
		return encodeJSON(stdout, summaries)
	}
	if len(summaries) == 0 {
		fmt.Fprintln(stdout, "no REQ files under docs/requirements/ (draft one in S0 from the REQ template)")
		return 0
	}
	fmt.Fprintf(stdout, "%-12s %-9s %-10s %s\n", "REQ", "STATUS", "VERSION", "NOTE")
	bindable := 0
	for _, s := range summaries {
		mark := " "
		if s.Bindable {
			mark = "*"
			bindable++
		}
		fmt.Fprintf(stdout, "%s %-11s %-9s %-10s %s\n", mark, s.ID, s.Status, s.Version, s.Note)
	}
	switch {
	case bindable == 1:
		fmt.Fprintln(stdout, "\nready to bind:")
		fmt.Fprintf(stdout, "  %s\n", soleBindableCommand(*root))
	case bindable > 1:
		fmt.Fprintln(stdout, "\nmultiple bindable REQs: uniqueness is a human decision; rerun req bind with --req <path>")
	}
	return 0
}

// controlPlaneDrift compares the runtime-recorded definition/policy
// fingerprints with the on-disk files; empty string means consistent.
func controlPlaneDrift(root string, state map[string]any) string {
	checks := []struct {
		stateKey, rel string
	}{
		{"definition", "docs/loop-definition.json"},
		{"hook_control", "docs/hook-policy.json"},
	}
	for _, check := range checks {
		block, _ := state[check.stateKey].(map[string]any)
		recorded, _ := block["sha256"].(string)
		if recorded == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.rel)))
		if err != nil {
			return fmt.Sprintf("%s unreadable (%v) — run doctor", check.rel, err)
		}
		if actual := fmt.Sprintf("%x", sha256.Sum256(data)); actual != recorded {
			return fmt.Sprintf("%s changed since the runtime was initialized — run `loop-harness doctor --root .` first; if it reports a policy_ref drift, reconcile with `runtime reconcile-policy-ref`, otherwise the control-plane change must be re-baselined (bind preflight refuses stale fingerprints)", check.rel)
		}
	}
	return ""
}

func markdownField(content string, names ...string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, sep := range []string{"：", ":"} {
			parts := strings.SplitN(line, sep, 2)
			if len(parts) != 2 {
				continue
			}
			for _, name := range names {
				if strings.EqualFold(strings.TrimSpace(parts[0]), name) {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

func runProjection(args []string, nextOnly bool, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("projection", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "projection")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	snapshot, err := runtime.NewStore(
		filepath.Join(*root, ".claude/loop-state.json"),
		filepath.Join(*root, ".claude/loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "projection: read runtime: %v\n", err)
		return 1
	}
	state := snapshot.State
	lifecycle, _ := state["lifecycle"].(map[string]any)
	machine, _ := lifecycle["state"].(string)
	phase, _ := lifecycle["phase"].(string)
	stage, skill, action := projectNext(machine, phase, *root)
	var projection any
	if nextOnly {
		projection = buildNextProjection(state, stage, skill, action, *root)
	} else {
		projection = buildStatusProjection(state, stage, machine, lifecycle["phase"], *root)
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		fmt.Fprintf(stderr, "projection: encode: %v\n", err)
		return 1
	}
	schemaName := "status.schema.json"
	if nextOnly {
		schemaName = "next.schema.json"
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes(schemaName, encoded); err != nil {
		fmt.Fprintf(stderr, "projection: internal contract violation: %v\n", err)
		return 1
	}
	return encodeJSON(stdout, projection)
}

// runReady dry-runs the current cursor Quality Gate and prints the live
// missing checklist. It never commits a Transition or mutates Runtime.
// Usage:
//
//	loop-harness ready --root .
func runReady(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ready", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "ready")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	report, err := controller.EvaluateReady(context.Background(), *root)
	if err != nil {
		fmt.Fprintf(stderr, "ready: %v\n", err)
		return 1
	}
	return encodeJSON(stdout, report)
}

func projectNext(state, phase, root string) (string, string, string) {
	cursor, _ := runtime.StageFor(state, phase, root)
	switch state {
	case "inactive":
		action := "produce one human-locked REQ (docs/requirements/REQ-template.md + skills: requirement-funnel), then bind it"
		skill := "requirement-funnel"
		if cmd := soleBindableCommand(root); cmd != "" {
			action = "bind the human-locked REQ: " + cmd + " (or tell the main session to bind it for you)"
			skill = "loop-orchestration"
		}
		return "S0", skill, action
	case "planning":
		return cursor, "specification-planning", "complete the planning phase for " + phase
	case "document_verification":
		return "S5", "document-verification", "complete independent document verification"
	case "building":
		return "S6", "agent-dispatch", "complete Builder assignments (register each result via `runtime task-complete`; SubagentStop integrates the worktree)"
	case "verification":
		switch phase {
		case "planned":
			return "S7", PrimarySkillS7, "scaffold the ReviewPlan via `loop-harness s7 draft --out plan.json`, fill the TODO oracles, and register via `runtime review-plan --file plan.json`"
		case "running", "cannot_clean", "discovery_draining":
			return "S7", "team-planning", "read `loop-harness s7 status`, scaffold each Assignment with `loop-harness s7 manifest-draft --assignment <id>`, register via `runtime register-workgroup`, and consume each Canonical ReviewResult via `runtime review-result submit`"
		case "observation_sealed":
			return "S7", "bug-resolution", "ObservationBatch sealed; the next PreToolUse auto-commits TR-008 to hand the batch to S8 — do not call the transition CLI"
		case "clean":
			return "S7", "acceptance-and-handoff", "machine CleanRound recorded; the next PreToolUse auto-commits TR-009 to advance into S10 — do not call the transition CLI"
		}
		return "S7", PrimarySkillS7, "recover the verification round with `loop-harness s7 status`; if no plan is registered, run `s7 draft`, otherwise scaffold the exact Assignment with `s7 manifest-draft` and register it"
	case "bug_resolution":
		switch phase {
		case "investigation":
			return "S8", "bug-resolution", "ingest or continue the InvestigationCase from the sealed ObservationBatch; do not create a BUG or reproduce the symptom"
		case "bug_report_review":
			return "S8", "bug-resolution", "reconcile the legacy BUG projection into its InvestigationCase; new S8 work must not accept a BUG as the authority"
		case "repair_readback":
			return "S9", "bug-resolution", "open or recover the RepairSession with `runtime repair status` / `runtime repair session open`, then compile the bounded RepairPlan"
		case "planning":
			return "S9", "bug-resolution", "dispatch each RepairAssignment with `runtime repair dispatch --assignment-id <assignment> --agent-id <agent>`, then each Builder submits an immutable PlanReport with `runtime repair plan-report submit --file <report.json>` (bind Session/Plan/Assignment, include at least one failing red pre-fix check); product writes stay denied until `runtime repair execution begin`"
		case "reproducing":
			return "S9", "bug-resolution", "the red pre-fix checks are recorded in the PlanReport; when every Assignment has reported, release implementation writes with `runtime repair execution begin`; inspect `runtime repair status` for missing reports"
		case "fixing":
			return "S9", "bug-resolution", "continue the already-dispatched bounded repair Builder(s) and submit one exact-unit result per Assignment with `runtime repair result submit --file <result.json>`"
		case "targeted_reverification":
			return "S9", "bug-resolution", "commit ChangeImpact and an independent TargetedReverification; follow `runtime repair status`"
		case "ready_for_full_review":
			return "S9", "bug-resolution", "create and commit the complete RepairHandoff with `runtime repair handoff create/commit`; then S7 starts a fresh full round"
		}
		return "S9", "bug-resolution", "recover the S9 RepairSession with `runtime repair status` and follow its next_action"
	case "acceptance", "release_audit":
		if state == "acceptance" {
			return "S10", "acceptance-and-handoff", "freeze the finite coverage_inventory and responsibility matrix, answer one counterevidence question per item, validate the acceptance manifest with `loop-harness s10 manifest validate --file <path> --type acceptance`, then register the fingerprinted acceptance evidence; do not modify product code or jump to S11"
		}
		return "S10", "acceptance-and-handoff", "complete all 8 release-audit areas and their counterevidence, validate the release-audit manifest with `loop-harness s10 manifest validate --file <path> --type release_audit`, then register the fingerprinted audit evidence; if any finding is blocking, route back through S7 or pause instead of forcing S11"
	case "awaiting_human_release":
		return "S11", "acceptance-and-handoff", "stop automation and submit one explicit runtime human-decision (approve, defer, reject_defect, reject_acceptance, reject_release_audit, or abort)"
	case "release_authorized":
		return "S11", "acceptance-and-handoff", "S11 human-authorized terminal; Harness performs no merge, publication, deployment, or formal release"
	case "aborted":
		return "aborted", "loop-orchestration", "aborted terminal; stop automation and use only an eligible human-authorized rollover for a new Runtime"
	case "paused":
		return "paused", "loop-orchestration", "resolve the recorded pause condition"
	default:
		return "cross-stage", "loop-orchestration", "recover the runtime checkpoint"
	}
}

func encodeJSON(w io.Writer, value any) int {
	enc := json.NewEncoder(w)
	if err := enc.Encode(value); err != nil {
		return 1
	}
	return 0
}

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "init")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if err := writeInactiveRuntime(*root); err != nil {
		fmt.Fprintf(stderr, "init failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "initialized .claude/loop-state.json")
	// Best-effort: regenerate the agent-facing Manual next to the binary so it
	// matches the on-disk loop-definition.json. Failure is non-fatal — the
	// Manual can be regenerated later via `loop-harness manual`. REQ-005 FR-015.
	if err := regenerateManualBestEffort(*root); err != nil {
		fmt.Fprintf(stderr, "init: warning: manual not regenerated: %v\n", err)
	} else {
		fmt.Fprintf(stdout, "manual regenerated at %s\n", transition.ManualTargetPath())
	}
	return 0
}

// regenerateManualBestEffort regenerates `.claude/bin/loop-harness.md` from
// the on-disk loop-definition.json + the spec registry compiled into this
// binary. Used by `init` so a freshly-initialized project has a current
// Manual alongside its binary without a separate `manual` invocation.
func regenerateManualBestEffort(root string) error {
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	defData, err := os.ReadFile(filepath.Join(root, "docs", "loop-definition.json"))
	if err != nil {
		return fmt.Errorf("read loop-definition.json: %w", err)
	}
	target := transition.ManualTargetPath()
	markdown := transition.RenderManual(catalog.Definition, transition.ManualOptions{
		TargetPath:           target,
		HarnessVersion:       "dev",
		LoopDefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(defData)),
	})
	fullTarget := filepath.Join(root, target)
	if err := os.MkdirAll(filepath.Dir(fullTarget), 0o755); err != nil {
		return fmt.Errorf("create manual dir: %w", err)
	}
	if err := os.WriteFile(fullTarget, []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write manual: %w", err)
	}
	return nil
}

// writeInactiveRuntime writes a schema-valid inactive runtime whose Loop
// Definition and Hook policy fingerprints match the local files. It is the
// standard way to seed a freshly bootstrapped project.
func writeInactiveRuntime(root string) error {
	markerPath := filepath.Join(root, ".claude/loop-init-pending.json")
	if _, err := os.Lstat(markerPath); err == nil {
		return completePendingInitialization(root, markerPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect pending initialization: %w", err)
	}
	full, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		return err
	}
	paths := []string{
		filepath.Join(root, ".claude/loop-state.json"),
		filepath.Join(root, ".claude/loop-events.jsonl"),
		filepath.Join(root, ".claude/hook-decisions.jsonl"),
		filepath.Join(root, ".claude/loop-state.json.rollover-pending.json"),
		filepath.Join(root, ".claude/loop-state.json.lock"),
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("refusing to initialize over existing runtime file %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect runtime path %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	pendingData, err := json.MarshalIndent(initPending{SchemaVersion: "1.0.0", FreshState: full}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pending initialization: %w", err)
	}
	if err := writeNewFile(markerPath, append(pendingData, '\n')); err != nil {
		return fmt.Errorf("record pending initialization: %w", err)
	}
	return completePendingInitialization(root, markerPath)
}

type initPending struct {
	SchemaVersion string         `json:"schema_version"`
	FreshState    map[string]any `json:"fresh_state"`
}

// completePendingInitialization makes a bootstrap retry-safe: every file is
// checked against the pending marker before it is accepted, and the marker is
// removed only after the complete runtime triplet exists.
func completePendingInitialization(root, markerPath string) error {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("read pending initialization: %w", err)
	}
	var pending initPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("decode pending initialization: %w", err)
	}
	if pending.SchemaVersion != "1.0.0" {
		return fmt.Errorf("unsupported pending initialization schema %q", pending.SchemaVersion)
	}
	stateData, err := json.MarshalIndent(pending.FreshState, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pending runtime state: %w", err)
	}
	files := []struct {
		path string
		data []byte
	}{
		{filepath.Join(root, ".claude/loop-state.json"), append(stateData, '\n')},
		{filepath.Join(root, ".claude/loop-events.jsonl"), nil},
		{filepath.Join(root, ".claude/hook-decisions.jsonl"), nil},
	}
	for _, file := range files {
		if err := ensurePendingInitFile(file.path, file.data); err != nil {
			return fmt.Errorf("complete pending initialization: %w", err)
		}
	}
	if err := os.Remove(markerPath); err != nil {
		return fmt.Errorf("clear pending initialization: %w", err)
	}
	if err := syncDirectory(filepath.Dir(markerPath)); err != nil {
		return fmt.Errorf("sync cleared pending initialization: %w", err)
	}
	return nil
}

func ensurePendingInitFile(path string, want []byte) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := writeNewFile(path, want); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(data, want) {
		return fmt.Errorf("existing file conflicts with pending bootstrap: %s", path)
	}
	return nil
}

func writeNewFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".loop-init-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Link publishes the fully written inode without replacing an existing
	// runtime file. A retry can therefore distinguish a completed file from a
	// missing one without ever observing a partially written destination.
	if err := os.Link(tempPath, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func inactiveRuntimeState(root string, occurredAt time.Time) (map[string]any, error) {
	defPath := filepath.Join(root, "docs/loop-definition.json")
	policyPath := filepath.Join(root, "docs/hook-policy.json")
	defData, err := os.ReadFile(defPath)
	if err != nil {
		return nil, fmt.Errorf("read Loop Definition: %w", err)
	}
	policyData, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("read Hook policy: %w", err)
	}
	var policyMetadata struct {
		Version string `json:"version"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(policyData, &policyMetadata); err != nil {
		return nil, fmt.Errorf("decode Hook policy metadata: %w", err)
	}
	if policyMetadata.Version == "" || policyMetadata.Mode == "" {
		return nil, fmt.Errorf("Hook policy version and mode are required")
	}
	type placeholder struct {
		SchemaVersion string         `json:"schema_version"`
		RuntimeID     string         `json:"runtime_id"`
		Definition    map[string]any `json:"definition"`
		Revision      int            `json:"revision"`
		Lifecycle     map[string]any `json:"lifecycle"`
		HookControl   map[string]any `json:"hook_control"`
	}
	defVersion := "1.1.0"
	if v, ok := extractDefinitionVersion(defData); ok {
		defVersion = v
	}
	state := placeholder{
		SchemaVersion: "1.1.0",
		RuntimeID:     "loop-inactive",
		Definition: map[string]any{
			"path":    "docs/loop-definition.json",
			"version": defVersion,
			"sha256":  fmt.Sprintf("%x", sha256.Sum256(defData)),
		},
		Revision: 0,
		Lifecycle: map[string]any{
			"state":          "inactive",
			"phase":          nil,
			"phase_revision": 0,
		},
		HookControl: map[string]any{
			"policy_ref": map[string]any{
				"path":    "docs/hook-policy.json",
				"version": policyMetadata.Version,
				"sha256":  fmt.Sprintf("%x", sha256.Sum256(policyData)),
			},
			"mode":                 policyMetadata.Mode,
			"health":               "healthy",
			"consecutive_failures": 0,
			"last_checked_at":      nil,
		},
	}
	return map[string]any{
		"schema_version": state.SchemaVersion,
		"runtime_id":     state.RuntimeID,
		"definition":     state.Definition,
		"revision":       state.Revision,
		"lifecycle":      state.Lifecycle,
		"milestone": map[string]any{
			"stage":           "S0",
			"lifecycle_state": "inactive",
			"lifecycle_phase": nil,
			"objective":       "produce one human-locked requirement (binding is the S1 action)",
			"action":          "produce one human-locked REQ (docs/requirements/REQ-template.md + skills: requirement-funnel), then bind it",
			"protocol_ref":    "docs/agent-protocol.md#s0",
			"manual_ref":      loopManualRef,
			"primary_skill":   "requirement-funnel",
			"read":            []any{"docs/requirements/"},
			"missing":         []any{"human_locked_req"},
			"done_when":       []any{"a locked REQ exists in docs/requirements/ — `req bind` (S1) initializes the runtime and fingerprints it"},
			"human_required":  false,
			"blocked":         false,
			"blocker":         nil,
			"event":           "init",
			"instruction":     "LOOP RECOVERY: bind one human-locked REQ.",
			"recovery":        []any{"read docs/agent-protocol.md#s0", "if blocked read .claude/bin/loop-harness.md"},
			"source_revision": 0,
			"updated_at":      occurredAt.UTC().Format(time.RFC3339Nano),
		},
		"authorization": map[string]any{
			"mode":        "none",
			"command":     "",
			"actor":       "",
			"occurred_at": "1970-01-01T00:00:00Z",
		},
		"bound_req": nil,
		"baseline": map[string]any{
			"generation":  0,
			"captured_at": nil,
		},
		"review": map[string]any{
			"round":       0,
			"clean_round": nil,
		},
		"configuration": map[string]any{
			"repair": map[string]any{
				"max_attempts_per_bug":       3,
				"max_same_contract_failures": 2,
				"max_full_review_rounds":     5,
			},
		},
		"hook_control": state.HookControl,
		"documents":    []any{},
		"entities": map[string]any{
			"agents": []any{},
			"tasks":  []any{},
			"bugs":   []any{},
			"teams":  []any{},
		},
		"evidence": []any{},
		"blockers": []any{},
		"pause":    nil,
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": 0,
			"last_event_id": nil,
		},
		"last_transition": nil,
		"updated_at":      occurredAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func extractDefinitionVersion(data []byte) (string, bool) {
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.SchemaVersion != "" {
		return probe.SchemaVersion, true
	}
	return "", false
}

func runTeam(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "launch" {
		fmt.Fprintln(stderr, "team requires <launch>")
		return 2
	}
	flags := flag.NewFlagSet("team launch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "team launch")
	root := flags.String("root", ".", "repository root")
	manifestPath := flags.String("manifest", "", "team manifest path relative to root")
	templatePath := flags.String("request-template", "", "readback request template path relative to root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *manifestPath == "" || *templatePath == "" {
		fmt.Fprintln(stderr, "team launch requires --manifest and --request-template")
		return 2
	}
	manifestData, err := os.ReadFile(filepath.Join(*root, *manifestPath))
	if err != nil {
		fmt.Fprintf(stderr, "read team manifest: %v\n", err)
		return 1
	}
	templateData, err := os.ReadFile(filepath.Join(*root, *templatePath))
	if err != nil {
		fmt.Fprintf(stderr, "read request template: %v\n", err)
		return 1
	}
	var request team.ReadbackRequest
	if err := json.Unmarshal(templateData, &request); err != nil {
		fmt.Fprintf(stderr, "decode request template: %v\n", err)
		return 1
	}
	if err := schemaValidate(*root, "readback-request.schema.json", templateData); err != nil {
		fmt.Fprintf(stderr, "validate request template: %v\n", err)
		return 1
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, request.OccurredAt)
	if err != nil {
		fmt.Fprintf(stderr, "parse request occurred_at: %v\n", err)
		return 1
	}
	requests, err := team.GenerateReadbackRequests(*root, manifestData, team.LaunchOptions{
		TaskID:                  request.TaskID,
		BugID:                   request.BugID,
		ExpectedRuntimeRevision: request.ExpectedRuntimeRevision,
		Documents:               request.Documents,
		OccurredAt:              occurredAt,
	})
	if err != nil {
		fmt.Fprintf(stderr, "generate launch packages: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(requests); err != nil {
		fmt.Fprintf(stderr, "encode launch packages: %v\n", err)
		return 1
	}
	return 0
}

func runRuntime(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		name := compactHelpName(args)
		if name == "" {
			name = "<stage-specific verb>"
		}
		printCommandHelp(stdout, "loop-harness runtime "+name, "Runtime actions are the CAS-owned mutation surface. Choose the stage-specific verb shown by `loop-harness actions`; diagnostics expose the next recovery action.")
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime requires <recover|reconcile|migrate-planning|reconcile-policy-ref|rollover|human-decision|s7-budget-decision|pause|resume|transition|change|evidence|register-workgroup|agent-begin|agent-event|task-complete|task-integrate|review-plan|review-result|finding-supplement|investigation|repair|bug-event|fingerprint>")
		return 2
	}
	switch args[0] {
	case "recover":
		return runRuntimeRecover(args[1:], stdout, stderr)
	case "rollover":
		return runRuntimeRollover(args[1:], stdout, stderr)
	case "human-decision":
		return runRuntimeHumanDecision(args[1:], stdout, stderr)
	case "s7-budget-decision":
		return runRuntimeS7BudgetDecision(args[1:], stdout, stderr)
	case "pause":
		return runRuntimePause(args[1:], stdout, stderr)
	case "resume":
		return runRuntimeResume(args[1:], stdout, stderr)
	case "reconcile":
		flags := flag.NewFlagSet("runtime reconcile", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime reconcile")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		resolvedState := resolveRootPath(*root, *statePath)
		resolvedJournal := resolveRootPath(*root, *journalPath)
		reconciler := runtime.NewWriter(resolvedState, resolvedJournal, *root, semantic.RuntimeCandidateValidator{})
		reconciled, err := reconciler.Reconcile()
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime reconcile", err))
			return 1
		}
		if reconciled {
			fmt.Fprintln(stdout, "journal reconciled")
		} else {
			fmt.Fprintln(stdout, "journal already consistent")
		}
		return 0
	case "migrate-planning":
		flags := flag.NewFlagSet("runtime migrate-planning", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime migrate-planning")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		resolvedState := *statePath
		if !filepath.IsAbs(resolvedState) {
			resolvedState = filepath.Join(*root, resolvedState)
		}
		resolvedJournal := *journalPath
		if !filepath.IsAbs(resolvedJournal) {
			resolvedJournal = filepath.Join(*root, resolvedJournal)
		}
		migrated, err := runtime.NewWriter(resolvedState, resolvedJournal, *root, semantic.RuntimeCandidateValidator{}).MigrateLegacyPlanning(*root)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime migrate-planning", err))
			return 1
		}
		if migrated {
			fmt.Fprintln(stdout, "planning phase migrated")
		} else {
			fmt.Fprintln(stdout, "planning phase already current")
		}
		return 0
	case "reconcile-policy-ref":
		return runRuntimeReconcilePolicyRef(args[1:], stdout, stderr)
	case "transition":
		flags := flag.NewFlagSet("runtime transition", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime transition")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		transitionID := flags.String("id", "", "Loop Definition transition ID")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		actor := flags.String("actor", "", "transition actor")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 transition time")
		reqID := flags.String("req-id", "", "locked REQ ID for TR-001")
		reqPath := flags.String("req-path", "", "locked REQ path for TR-001")
		reqVersion := flags.String("req-version", "", "locked REQ version for TR-001")
		reqSHA256 := flags.String("req-sha256", "", "locked REQ SHA-256 for TR-001")
		reqApprovedBy := flags.String("req-approved-by", "", "locked REQ approver for TR-001")
		reqApprovedAt := flags.String("req-approved-at", "", "locked REQ approval time for TR-001")
		paramsRaw := flags.String("params", "", "JSON object of guard params (used by generated-evidence transitions like PTR-BUG-02)") // deprecated: legacy compatibility
		var evidence stringListFlag
		flags.Var(&evidence, "evidence", "required evidence kind=reference; repeatable")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		var params map[string]any
		if *paramsRaw != "" {
			if err := json.Unmarshal([]byte(*paramsRaw), &params); err != nil {
				fmt.Fprintf(stderr, "runtime transition: invalid --params JSON: %v\n", err)
				return 2
			}
		}
		if *transitionID == "" || *actor == "" {
			fmt.Fprintln(stderr, "runtime transition requires --id and --actor")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime transition", err))
			return 1
		}
		resolvedStatePath := resolveRootPath(*root, *statePath)
		resolvedJournalPath := resolveRootPath(*root, *journalPath)
		currentSnapshot, err := runtime.NewStore(resolvedStatePath, resolvedJournalPath).Snapshot()
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime transition", err))
			return 1
		}
		currentRuntimeID, _ := currentSnapshot.State["runtime_id"].(string)
		evidenceMap, err := parseEvidence(evidence)
		if err != nil {
			fmt.Fprintf(stderr, "runtime transition: %v\n", err)
			return 2
		}
		var occurredAt time.Time
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime transition: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		var req *transition.LockedREQ
		if *reqID != "" {
			req = &transition.LockedREQ{
				ID: *reqID, Path: *reqPath, Version: *reqVersion, SHA256: *reqSHA256,
				ApprovedBy: *reqApprovedBy, ApprovedAt: *reqApprovedAt,
			}
		}
		next, err := transition.Apply(*root, resolvedStatePath, resolvedJournalPath, transition.Request{
			TransitionID: *transitionID, ExpectedRevision: resolvedRevision, ExpectedRuntimeID: currentRuntimeID,
			Actor: *actor, Evidence: evidenceMap, REQ: req, OccurredAt: occurredAt,
			Params: params,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime transition", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode runtime transition: %v\n", err)
			return 1
		}
		return 0
	case "change":
		return runRuntimeChange(args[1:], stdout, stderr)
	case "evidence":
		return runRuntimeEvidence(args[1:], stdout, stderr)
	case "register-workgroup":
		flags := flag.NewFlagSet("runtime register-workgroup", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime register-workgroup")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		manifestPath := flags.String("manifest", "", "team manifest path")
		taskID := flags.String("task-id", "", "TASK ID")
		taskPath := flags.String("task", "", "TASK path")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 registration time")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *manifestPath == "" || *taskID == "" || *taskPath == "" {
			fmt.Fprintln(stderr, "runtime register-workgroup requires --manifest, --task-id and --task")
			return 2
		}
		// Anchor --state / --journal relative paths against --root so the
		// verb works from any cwd (e.g. a sandboxed shell whose cwd is not
		// the project root). resolveExpectedRevision must read the same
		// resolved state file as assignment.Register, otherwise the
		// resolved revision diverges from the file the writer opens and
		// Register aborts with a stale-revision error.
		resolvedState := resolveRootPath(*root, *statePath)
		resolvedJournal := resolveRootPath(*root, *journalPath)
		resolvedRevision, err := resolveExpectedRevision(*root, resolvedState, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime register-workgroup", err))
			return 1
		}
		var occurredAt time.Time
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime register-workgroup: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		next, err := assignment.Register(*root, resolvedState, resolvedJournal, assignment.Request{
			ExpectedRevision: resolvedRevision,
			ManifestPath:     resolveRootPath(*root, *manifestPath),
			TaskID:           *taskID,
			TaskPath:         resolveRootPath(*root, *taskPath),
			OccurredAt:       occurredAt,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime register-workgroup", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode workgroup registration: %v\n", err)
			return 1
		}
		return 0
	case "agent-begin":
		// L4 §3.3 plan_checkpoint recovery verb. Performs the same
		// readback_submitted -> activation_sent -> work_started chain as
		// the PostToolUse(SendMessage) auto-chain, driven explicitly when
		// the auto-chain could not (e.g. Worker omitted plan_ref, hook
		// failed). One CAS-bound AdvanceAgent call per step so the
		// existing dispatch-mode / state / hash-chain guards stay in force.
		flags := flag.NewFlagSet("runtime agent-begin", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime agent-begin")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		agentID := flags.String("agent-id", "", "Agent ID")
		planPath := flags.String("plan", "", "plan_report message path")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *agentID == "" || *planPath == "" {
			fmt.Fprintln(stderr, "runtime agent-begin requires --agent-id and --plan")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime agent-begin", err))
			return 1
		}
		var occurredAt time.Time
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime agent-begin: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		next, outcome, err := assignment.AgentBegin(*root, *statePath, *journalPath, assignment.AgentBeginRequest{
			ExpectedRevision: resolvedRevision,
			AgentID:          *agentID,
			PlanPath:         resolveRootPath(*root, *planPath),
			OccurredAt:       occurredAt,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime agent-begin", err))
			return 1
		}
		if outcome.Chained {
			fmt.Fprintf(stderr, "auto-chain: %s advanced to %s (activation_id=%s)\n", outcome.AgentID, outcome.FinalState, outcome.ActivationID)
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode agent-begin snapshot: %v\n", err)
			return 1
		}
		return 0
	case "agent-event":
		flags := flag.NewFlagSet("runtime agent-event", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime agent-event")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		agentID := flags.String("agent-id", "", "Agent ID")
		event := flags.String("event", "", "readback_submitted, understanding_approved, or activated")
		messagePath := flags.String("message", "", "Agent message path")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *agentID == "" || *event == "" || *messagePath == "" {
			fmt.Fprintln(stderr, "runtime agent-event requires --agent-id, --event and --message")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime agent-event", err))
			return 1
		}
		var occurredAt time.Time
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime agent-event: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		next, err := assignment.AdvanceAgent(*root, *statePath, *journalPath, assignment.AgentEventRequest{
			ExpectedRevision: resolvedRevision,
			AgentID:          *agentID,
			Event:            *event,
			MessagePath:      resolveRootPath(*root, *messagePath),
			OccurredAt:       occurredAt,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime agent-event", err))
			return 1
		}
		// The activation moment is where the next-step discipline is needed;
		// print role-aware next actions instead of letting the agent discover
		// them from a late failure (L3-S6/S7 complexity passes). Reviewers
		// have no worktree and submit via review-result; Builders integrate
		// via worktree + task-complete.
		if *event == "activation_sent" {
			if reviewerRole(next.State, *agentID) {
				fmt.Fprintln(stderr, "activated. next: (1) advance `work_started` when you begin; (2) write the Canonical ReviewResult (claim_results must equal the assignment's Claim set exactly; every fail Claim needs one Finding with a real encounter — see review-result.example.json) and submit via `runtime review-result submit --assignment-id <id> --result <file>`")
			} else {
				fmt.Fprintln(stderr, "activated. next: (1) create the worktree if absent — `git worktree add .worktrees/<assignment-id> -b wt/<assignment-id> develop` — and record worktree_path/branch/target_branch on the assignment's workgroup manifest row (or .claude/assignments/<assignment-id>.json); (2) advance `work_started` when the Builder begins writing; (3) register completion with `runtime task-complete`")
			}
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode Agent event: %v\n", err)
			return 1
		}
		return 0
	case "task-complete":
		// Canonical S6 Builder Result registration (L3-S6 §7.3): one
		// command validates the completion message, derives the
		// completion_report evidence envelope, advances the Agent and
		// TASK, and registers the evidence — atomically. This replaces
		// the agent-event + evidence-add dual write.
		flags := flag.NewFlagSet("runtime task-complete", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime task-complete")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		agentID := flags.String("agent-id", "", "Builder Agent ID")
		messagePath := flags.String("message", "", "completion_report message path")
		occurredAtValue := flags.String("occurred-at", "", "RFC3339 event time")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *agentID == "" || *messagePath == "" {
			fmt.Fprintln(stderr, "runtime task-complete requires --agent-id and --message")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime task-complete", err))
			return 1
		}
		var occurredAt time.Time
		if *occurredAtValue != "" {
			occurredAt, err = time.Parse(time.RFC3339Nano, *occurredAtValue)
			if err != nil {
				fmt.Fprintf(stderr, "runtime task-complete: invalid --occurred-at: %v\n", err)
				return 2
			}
		}
		next, err := assignment.CompleteTask(*root, *statePath, *journalPath, assignment.CompletionRequest{
			ExpectedRevision: resolvedRevision,
			AgentID:          *agentID,
			MessagePath:      resolveRootPath(*root, *messagePath),
			OccurredAt:       occurredAt,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime task-complete", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode Builder Result: %v\n", err)
			return 1
		}
		return 0
	case "task-integrate":
		// Explicit S6 integration verb (L3-S6 §7.4 / N1 complexity pass):
		// runs the same Inspect → non-squash merge → verified checkpoint
		// chain as the SubagentStop hook, without depending on the
		// platform payload carrying the assignment identification.
		flags := flag.NewFlagSet("runtime task-integrate", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime task-integrate")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		assignmentID := flags.String("assignment-id", "", "assignment to integrate")
		agentID := flags.String("agent-id", "", "owning agent ID (optional, aids lookup)")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *assignmentID == "" {
			fmt.Fprintln(stderr, "runtime task-integrate requires --assignment-id")
			return 2
		}
		resolvedRoot := *root
		if !filepath.IsAbs(resolvedRoot) {
			if abs, err := filepath.Abs(resolvedRoot); err == nil {
				resolvedRoot = abs
			}
		}
		resolvedState := resolveRootPath(resolvedRoot, *statePath)
		resolvedJournal := resolveRootPath(resolvedRoot, *journalPath)
		snapshot, err := runtime.NewStore(resolvedState, resolvedJournal).Snapshot()
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime task-integrate", err))
			return 1
		}
		loaded, err := hookctx.LoadFull(resolvedRoot, *agentID)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime task-integrate", err))
			return 1
		}
		matched := false
		known := make([]string, 0, len(loaded.Assignments))
		for i := range loaded.Assignments {
			row := loaded.Assignments[i]
			if row.AssignmentID != "" {
				known = append(known, row.AssignmentID)
			}
			if row.AssignmentID == *assignmentID || (*agentID != "" && row.OwnerAgentID == *agentID) {
				matched = true
			}
		}
		if !matched {
			fmt.Fprintf(stderr, "runtime task-integrate: assignment %q is not registered (worktree coordinates or agent row missing); known assignments: %s\n",
				*assignmentID, strings.Join(known, ", "))
			return 1
		}
		input := policy.Input{
			Event:    "SubagentStop",
			AgentID:  *agentID,
			TargetID: *assignmentID,
			Facts:    map[string]bool{"agent_report_complete": true},
		}
		guidance, updated, err := HandleSubagentStopForController(context.Background(), resolvedRoot, snapshot, loaded, "SubagentStop", input)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime task-integrate", err))
			return 1
		}
		state := ""
		if guidance.Blocked {
			state = "preserved/blocked"
		} else {
			state = "integrated"
		}
		fmt.Fprintf(stderr, "task-integrate: %s — %s\n", state, guidance.Action)
		if len(guidance.Integration) > 0 {
			fmt.Fprintf(stderr, "integration facts: %s\n", strings.Join(guidance.Integration, "; "))
		}
		payload := map[string]any{
			"assignment_id": *assignmentID,
			"state":         state,
			"blocked":       guidance.Blocked,
			"blocker":       guidance.Blocker,
			"integration":   guidance.Integration,
			"revision":      updated.Revision,
		}
		if err := json.NewEncoder(stdout).Encode(payload); err != nil {
			fmt.Fprintf(stderr, "encode task-integrate result: %v\n", err)
			return 1
		}
		return 0
	case "review-plan":
		// S7 entry verb (L3-S7 §4.1): validates and pins the ReviewPlan,
		// initializes claim/assignment projections, phase -> running.
		// `revise` is the one controlled revision per round (§5.3).
		revise := len(args) > 1 && args[1] == "revise"
		flags := flag.NewFlagSet("runtime review-plan", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime review-plan")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		planPath := flags.String("file", "", "ReviewPlan JSON path")
		sourceRef := flags.String("source-ref", "", "revise: triggering Result/Finding evidence id")
		affectedSurface := flags.String("affected-surface", "", "revise: path surface the revision may touch")
		parseArgs := args[1:]
		if revise {
			parseArgs = args[2:]
		}
		if err := flags.Parse(parseArgs); err != nil {
			return 2
		}
		if revise {
			resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
			if err != nil {
				fmt.Fprintln(stderr, formatFailure("runtime review-plan revise", err))
				return 1
			}
			next, err := review.RevisePlan(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), review.ReviseRequest{
				ExpectedRevision: resolvedRevision,
				PlanPath:         resolveRootPath(*root, *planPath),
				SourceRef:        *sourceRef,
				AffectedSurface:  *affectedSurface,
			})
			if err != nil {
				fmt.Fprintln(stderr, formatFailure("runtime review-plan revise", err))
				return 1
			}
			ptr := review.PlanPointerFromState(next.State)
			fmt.Fprintf(stderr, "review-plan revise: %s now at revision %d (status %s); changed claims returned to planned\n", ptr.PlanID, ptr.Revision, ptr.Status)
			return encodeJSON(stdout, map[string]any{
				"plan_id":  ptr.PlanID,
				"revision": ptr.Revision,
				"status":   ptr.Status,
			})
		}
		if *planPath == "" {
			fmt.Fprintln(stderr, "runtime review-plan requires --file <plan.json>")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime review-plan", err))
			return 1
		}
		next, err := review.RegisterPlan(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), review.PlanRequest{
			ExpectedRevision: resolvedRevision,
			PlanPath:         resolveRootPath(*root, *planPath),
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime review-plan", err))
			return 1
		}
		ptr := review.PlanPointerFromState(next.State)
		fmt.Fprintf(stderr, "review-plan: registered %s for round %d (status %s); dispatch reviewers via `runtime register-workgroup`, then consume results via `runtime review-result submit`\n",
			ptr.PlanID, ptr.ReviewRound, ptr.Status)
		return encodeJSON(stdout, map[string]any{
			"plan_id":      ptr.PlanID,
			"review_round": ptr.ReviewRound,
			"status":       ptr.Status,
			"revision":     next.Revision,
		})
	case "review-result":
		// S7 Canonical ReviewResult submit (L3-S7 §9.1): one CAS consumes
		// the result, registers immutable Findings, updates claim
		// dispositions, and runs the round consumer (seal / clean / pause).
		// The documented invocation carries the `submit` verb word.
		verbArgs := args[1:]
		if len(verbArgs) > 0 && verbArgs[0] == "submit" {
			verbArgs = verbArgs[1:]
		}
		flags := flag.NewFlagSet("runtime review-result", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime review-result")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		assignmentID := flags.String("assignment-id", "", "plan assignment the result answers")
		resultPath := flags.String("result", "", "ReviewResult JSON path")
		captureDir := flags.String("captures", "", "capture buffer dir (or the steps.jsonl file itself); empty encounter timelines absorb buffered steps")
		if err := flags.Parse(verbArgs); err != nil {
			return 2
		}
		if *assignmentID == "" || *resultPath == "" {
			fmt.Fprintln(stderr, "runtime review-result requires --assignment-id <id> and --result <result.json>")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime review-result", err))
			return 1
		}
		resolvedCaptures := ""
		if *captureDir != "" {
			resolvedCaptures = resolveRootPath(*root, *captureDir)
			// Accept both the buffer directory and the steps.jsonl file; a
			// directory passed through silently loaded zero steps otherwise.
			if info, statErr := os.Stat(resolvedCaptures); statErr == nil && info.IsDir() {
				resolvedCaptures = filepath.Join(resolvedCaptures, "steps.jsonl")
			}
			if info, statErr := os.Stat(resolvedCaptures); statErr != nil || info.IsDir() {
				fmt.Fprintf(stderr, "note: --captures buffer not found at %s; findings keep their own timelines\n", resolvedCaptures)
			}
		}
		next, err := review.SubmitResult(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), review.SubmitRequest{
			ExpectedRevision: resolvedRevision,
			AssignmentID:     *assignmentID,
			ResultPath:       resolveRootPath(*root, *resultPath),
			CaptureDir:       resolvedCaptures,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime review-result", err))
			return 1
		}
		ptr := review.PlanPointerFromState(next.State)
		status := ""
		if ptr != nil {
			status = ptr.Status
		}
		switch status {
		case "observation_sealed":
			fmt.Fprintln(stderr, "review-result: consumed; ObservationBatch sealed — the next PreToolUse will auto-commit TR-008 to hand off to S8 (do not invoke the transition CLI)")
		case "clean":
			fmt.Fprintln(stderr, "review-result: consumed; machine CleanRound generated — the next PreToolUse will auto-commit TR-009 to advance into S10 (do not invoke the transition CLI)")
		case "paused":
			fmt.Fprintln(stderr, "review-result: consumed; pause checkpoint recorded — route via TR-010 (req change) or TR-011 (release blocked)")
		case "cannot_clean", "discovery_draining":
			fmt.Fprintf(stderr, "review-result: consumed; round is %s — %d required claim(s) still need results before the batch seals\n",
				status, len(review.UndispositionedRequired(next.State)))
		default:
			fmt.Fprintf(stderr, "review-result: consumed; round running — %d required claim(s) remaining\n",
				len(review.UndispositionedRequired(next.State)))
		}
		pending := review.UndispositionedRequired(next.State)
		if pending == nil {
			pending = []string{}
		}
		return encodeJSON(stdout, map[string]any{
			"assignment_id":  *assignmentID,
			"plan_status":    status,
			"pending_claims": pending,
			"revision":       next.Revision,
		})
	case "finding-supplement":
		// S7/S8 FindingSupplement append (L3-S7 §3.6, L3-S8 §2.2): the
		// original finder — or a scheduler-authorized replacement — appends
		// new observation/evidence/correlation refs under an immutable
		// Finding without rewriting it or the sealed ObservationBatch. The
		// discriminator gate (L3-S7 §14.1) requires hypothesis_id +
		// discriminator + expected_outcomes unless the submission is an S7
		// in-round note declared with --in-round-note.
		flags := flag.NewFlagSet("runtime finding-supplement", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime finding-supplement")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		findingID := flags.String("finding", "", "finding id the supplement extends")
		filePath := flags.String("file", "", "FindingSupplement JSON path")
		authorizedBy := flags.String("authorized-by", "", "scheduler identity authorizing a replacement finder (required when author != original finder)")
		inRoundNote := flags.Bool("in-round-note", false, "declare an S7 in-round note from the original finder (exempt from the hypothesis_id + discriminator + expected_outcomes gate; must not carry hypothesis_id)")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *findingID == "" || *filePath == "" {
			fmt.Fprintln(stderr, "runtime finding-supplement requires --finding <id> and --file <supplement.json>")
			return 2
		}
		receipt, err := review.SubmitSupplement(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), review.SupplementRequest{
			FindingID:    *findingID,
			FilePath:     resolveRootPath(*root, *filePath),
			AuthorizedBy: *authorizedBy,
			InRoundNote:  *inRoundNote,
		})
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime finding-supplement", err))
			return 1
		}
		fmt.Fprintf(stderr, "finding-supplement: %s appended to %s (state revision %d); the Finding and ObservationBatch are unchanged\n",
			receipt.SupplementID, receipt.FindingID, receipt.Revision)
		return encodeJSON(stdout, map[string]any{
			"supplement_id":          receipt.SupplementID,
			"supplements_finding_id": receipt.FindingID,
			"path":                   receipt.Path,
			"sha256":                 receipt.SHA256,
			"revision":               receipt.Revision,
		})
	case "investigation":
		return runRuntimeInvestigation(args[1:], stdout, stderr)
	case "repair":
		return runRuntimeRepair(args[1:], stdout, stderr)
	case "bug-event":
		flags := flag.NewFlagSet("runtime bug-event", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime bug-event")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
		bugID := flags.String("bug-id", "", "BUG ID")
		event := flags.String("event", "", "BUG lifecycle event (e.g. bug_accepted)")
		messagePath := flags.String("message", "", "BUG message evidence path")
		paramsRaw := flags.String("params", "", "JSON object of guard params")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *bugID == "" || *event == "" {
			fmt.Fprintln(stderr, "runtime bug-event requires --bug-id and --event")
			return 2
		}
		resolvedRevision, err := resolveExpectedRevision(*root, *statePath, *expectedRevision)
		if err != nil {
			fmt.Fprintln(stderr, formatFailure("runtime bug-event", err))
			return 1
		}
		var params map[string]any
		if *paramsRaw != "" {
			if err := json.Unmarshal([]byte(*paramsRaw), &params); err != nil {
				fmt.Fprintf(stderr, "runtime bug-event: invalid --params JSON: %v\n", err)
				return 2
			}
		}
		next, err := assignment.AdvanceBug(*root, *statePath, *journalPath, assignment.BugEventRequest{
			ExpectedRevision: resolvedRevision,
			BugID:            *bugID,
			Event:            *event,
			MessagePath:      resolveRootPath(*root, *messagePath),
			Params:           params,
		})
		if err != nil {
			// RC-15 (S9-M5/T1): a typed *transition.RepairLimitError from the
			// BUG lifecycle is bridged through adapter.DispatchRepairLimitExceeded
			// (GTR-004) so the runtime enters paused with a real pause_record
			// instead of only printing the limit failure. The failed AdvanceBug
			// never committed, so the Runtime revision is still resolvedRevision
			// — the CAS must pin that revision, not revision+1. The dispatch
			// error, if any, is reported; the original limit error is otherwise
			// surfaced after the pause is committed.
			nextSnapshot, dispatchErr := adapter.DispatchRepairLimitExceeded(*root, *statePath, *journalPath, resolvedRevision, err)
			if dispatchErr == nil {
				if encodeErr := json.NewEncoder(stdout).Encode(nextSnapshot); encodeErr != nil {
					fmt.Fprintf(stderr, "encode paused snapshot: %v\n", encodeErr)
					return 1
				}
				fmt.Fprintln(stderr, formatFailure("runtime bug-event", err)+"; runtime paused via GTR-004")
				return 1
			}
			fmt.Fprintln(stderr, formatFailure("runtime bug-event", err))
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(next); err != nil {
			fmt.Fprintf(stderr, "encode BUG event: %v\n", err)
			return 1
		}
		return 0
	case "fingerprint":
		flags := flag.NewFlagSet("runtime fingerprint", flag.ContinueOnError)
		flags.SetOutput(stderr)
		bindUsage(flags, "runtime fingerprint")
		root := flags.String("root", ".", "repository root")
		statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
		journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		// Anchor --state / --journal against --root so the verb works
		// from any cwd (L3-S7 sandbox contract).
		resolvedState := resolveRootPath(*root, *statePath)
		resolvedJournal := resolveRootPath(*root, *journalPath)
		result, err := runtime.NewWriter(resolvedState, resolvedJournal, *root, semantic.RuntimeCandidateValidator{}).RefreshFingerprints(*root)
		if err != nil {
			fmt.Fprintf(stderr, "runtime fingerprint failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "updated=%d unchanged=%d missing=%d\n", len(result.Updated), len(result.Unchanged), len(result.Missing))
		for _, p := range result.Updated {
			fmt.Fprintf(stdout, "updated  %s\n", p)
		}
		for _, p := range result.Missing {
			fmt.Fprintf(stdout, "missing  %s\n", p)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown runtime command %q\n", args[0])
		return 2
	}
}

// runRuntimeHumanDecision is the only CLI entrypoint for S11 human decisions.
// The disposition is mapped to a fixed transition by the Runtime package; the
// caller cannot supply a target state or transition ID. This makes the command
// usable for both legacy S11 snapshots and the current human gateway while
// keeping missing evidence, actor, and CAS revision fail-closed.
func runRuntimeHumanDecision(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime human-decision", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime human-decision")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	disposition := flags.String("disposition", "", "one of approve, defer, reject_defect, reject_acceptance, reject_release_audit, abort")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	actor := flags.String("actor", "", "human decision actor")
	decisionEvidence := flags.String("decision-evidence", "", "human_decision_record evidence reference")
	findingEvidence := flags.String("finding-evidence", "", "finding_record evidence reference; required for reject_defect")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	missing := make([]string, 0, 4)
	if strings.TrimSpace(*disposition) == "" {
		missing = append(missing, "--disposition")
	}
	if *expectedRevision < 0 {
		missing = append(missing, "--expected-revision")
	}
	if strings.TrimSpace(*actor) == "" {
		missing = append(missing, "--actor")
	}
	if strings.TrimSpace(*decisionEvidence) == "" {
		missing = append(missing, "--decision-evidence")
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "runtime human-decision requires %s; choose --disposition approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort\n", strings.Join(missing, ", "))
		return 2
	}

	transitionID, err := runtime.HumanReleaseTransitionID(strings.TrimSpace(*disposition))
	if err != nil {
		fmt.Fprintf(stderr, "runtime human-decision: %v; choose approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort\n", err)
		return 2
	}
	if strings.TrimSpace(*disposition) == string(runtime.HumanReleaseDispositionRejectDefect) && strings.TrimSpace(*findingEvidence) == "" {
		fmt.Fprintln(stderr, "runtime human-decision reject_defect requires --finding-evidence for finding_record")
		return 2
	}

	evidence := map[string]string{"human_decision_record": strings.TrimSpace(*decisionEvidence)}
	if strings.TrimSpace(*disposition) == string(runtime.HumanReleaseDispositionDefer) {
		evidence["pause_record"] = "generated:pause_checkpoint"
	}
	if strings.TrimSpace(*disposition) == string(runtime.HumanReleaseDispositionRejectDefect) {
		evidence["finding_record"] = strings.TrimSpace(*findingEvidence)
	}

	next, err := transition.Apply(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), transition.Request{
		TransitionID:     transitionID,
		ExpectedRevision: *expectedRevision,
		Actor:            strings.TrimSpace(*actor),
		Evidence:         evidence,
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime human-decision", err))
		return 1
	}
	return encodeJSON(stdout, next)
}

// runRuntimeReconcilePolicyRef realigns `hook_control.policy_ref` with the
// Hook policy document on disk (BUG-039-12; REQ-039 §11, SYNC-039 §6-7).
//
// This is the fix path that `loop-harness doctor` names when it detects policy
// reference drift. It deliberately reuses Store.RefreshFingerprints rather than
// writing policy_ref directly: fingerprint refresh is the single canonical
// non-semantic housekeeping writer, it goes through the runtime lock, the
// the mandatory semantic validator and atomic write, and it does not bump the revision or
// append a journal entry. Realigning an audit snapshot is not a Loop transition,
// so it must not look like one in the journal.
//
// --check reports drift without writing, for use in CI or a pre-flight probe.
func runRuntimeReconcilePolicyRef(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime reconcile-policy-ref", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime reconcile-policy-ref")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	checkOnly := flags.Bool("check", false, "report drift without writing state")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	// Anchor --state / --journal against --root so the verb works
	// from any cwd (L3-S7 sandbox contract).
	resolvedState := resolveRootPath(*root, *statePath)
	resolvedJournal := resolveRootPath(*root, *journalPath)
	store := runtime.NewStore(resolvedState, resolvedJournal)
	before, err := store.InspectPolicyRef(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime reconcile-policy-ref", err))
		return 1
	}
	if before.Missing {
		fmt.Fprintln(stderr, "runtime reconcile-policy-ref failed: runtime state has no hook_control.policy_ref; re-bind the runtime")
		return 1
	}
	if before.FileMissing {
		fmt.Fprintf(stderr, "runtime reconcile-policy-ref failed: hook policy %s does not exist on disk\n", before.Path)
		return 1
	}
	if !before.Drifted() {
		fmt.Fprintf(stdout, "policy_ref already consistent path=%s version=%s\n", before.Path, before.RecordedVersion)
		return 0
	}
	if *checkOnly {
		fmt.Fprintf(stderr, "policy_ref drifted path=%s version recorded=%s on-disk=%s sha256 recorded=%s on-disk=%s\n",
			before.Path, before.RecordedVersion, before.OnDiskVersion, before.RecordedSHA256, before.OnDiskSHA256)
		return 1
	}
	if _, err := runtime.NewWriter(resolvedState, resolvedJournal, *root, semantic.RuntimeCandidateValidator{}).RefreshFingerprints(*root); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime reconcile-policy-ref", err))
		return 1
	}
	after, err := store.InspectPolicyRef(*root)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime reconcile-policy-ref", err))
		return 1
	}
	if after.Drifted() {
		fmt.Fprintf(stderr, "runtime reconcile-policy-ref failed: policy_ref still drifted after refresh (version=%s sha256=%s)\n",
			after.RecordedVersion, after.RecordedSHA256)
		return 1
	}
	fmt.Fprintf(stdout, "policy_ref reconciled path=%s version %s -> %s\n", after.Path, before.RecordedVersion, after.RecordedVersion)
	if before.RecordedSHA256 != after.RecordedSHA256 {
		fmt.Fprintf(stdout, "policy_ref sha256 %s -> %s\n", before.RecordedSHA256, after.RecordedSHA256)
	}
	return 0
}

func runRuntimeRollover(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("runtime rollover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime rollover")
	root := flags.String("root", ".", "repository root")
	archive := flags.String("archive-dir", ".claude/runtime-archive", "archive directory relative to repository root")
	approvedBy := flags.String("approved-by", "", "human approver identity")
	approvalEvidence := flags.String("approval-evidence", "", "valid human_decision evidence ID produced by --approved-by")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*approvedBy) == "" {
		fmt.Fprintln(stderr, "runtime rollover requires --approved-by")
		return 2
	}
	if strings.TrimSpace(*approvalEvidence) == "" {
		fmt.Fprintln(stderr, "runtime rollover requires --approval-evidence")
		return 2
	}
	now := time.Now().UTC()
	freshState, err := inactiveRuntimeState(*root, now)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", err))
		return 1
	}
	encoded, err := json.Marshal(freshState)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", fmt.Errorf("encode fresh runtime: %w", err)))
		return 1
	}
	if err := semantic.ValidateRuntimeBytes(*root, encoded); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", fmt.Errorf("validate fresh runtime: %w", err)))
		return 1
	}
	record, err := runtime.NewWriter(rootedPath(*root, ".claude/loop-state.json"), rootedPath(*root, ".claude/loop-events.jsonl"), *root, semantic.RuntimeCandidateValidator{}).Rollover(
		freshState, rootedPath(*root, *archive), runtime.RolloverApproval{ApprovedBy: *approvedBy, EvidenceID: *approvalEvidence}, now,
	)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime rollover", err))
		return 1
	}
	fmt.Fprintf(stdout, "runtime rolled over: archived %s revision %d at %s\n", record.RuntimeID, record.Revision, record.ArchiveDir)
	if note, err := archiveREQOnRollover(*root, record.ArchiveDir, *approvedBy, now); err != nil {
		fmt.Fprintf(stderr, "runtime rollover: warning: REQ archival skipped: %v\n", err)
	} else if note != "" {
		fmt.Fprintf(stdout, "REQ %s (dual-fingerprint receipt: %s)\n", note, filepath.Join(record.ArchiveDir, "req-archive.json"))
	}
	return 0
}

// archiveREQOnRollover closes the REQ file's lifecycle at the rollover
// moment: the status line flips locked → archived (baseline content is
// never touched) and a dual-fingerprint receipt lands beside the sealed
// journal in the archive directory. The manifest's sealed hashes stay
// intact — the receipt is a separate, self-describing record.
func archiveREQOnRollover(root, archiveDir, approvedBy string, occurredAt time.Time) (string, error) {
	stateData, err := os.ReadFile(filepath.Join(archiveDir, "loop-state.json"))
	if err != nil {
		return "", fmt.Errorf("read archived runtime: %w", err)
	}
	var archived map[string]any
	if err := json.Unmarshal(stateData, &archived); err != nil {
		return "", fmt.Errorf("decode archived runtime: %w", err)
	}
	bound, _ := archived["bound_req"].(map[string]any)
	reqPath, _ := bound["path"].(string)
	reqID, _ := bound["id"].(string)
	if reqPath == "" || reqID == "" {
		return "", nil // nothing bound in the archived period; nothing to close
	}
	cleanRel, err := containedRelPath(reqPath)
	if err != nil {
		return "", fmt.Errorf("bound REQ path %q: %w", reqPath, err)
	}
	reqData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cleanRel)))
	if err != nil {
		return "", fmt.Errorf("read bound REQ %s: %w", cleanRel, err)
	}
	shaBefore := fmt.Sprintf("%x", sha256.Sum256(reqData))
	flipped, ok := flipStatusLineToArchived(string(reqData))
	if !ok {
		return "", nil // already archived or not locked; leave untouched
	}
	if err := atomicWriteREQFile(filepath.Join(root, filepath.FromSlash(cleanRel)), []byte(flipped)); err != nil {
		return "", fmt.Errorf("write archived REQ %s: %w", cleanRel, err)
	}
	shaAfter := fmt.Sprintf("%x", sha256.Sum256([]byte(flipped)))
	receipt := map[string]any{
		"schema_version": "1.0.0",
		"event":          "req_archived",
		"disposition":    "lifecycle_closed",
		"req": map[string]any{
			"id": reqID, "path": reqPath,
			"status_before": "locked", "status_after": "archived",
			"sha256_before": shaBefore, "sha256_after": shaAfter,
		},
		"approved_by": approvedBy,
		"occurred_at": occurredAt.UTC().Format(time.RFC3339Nano),
	}
	receiptData, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicWriteREQFile(filepath.Join(archiveDir, "req-archive.json"), append(receiptData, '\n')); err != nil {
		return "", fmt.Errorf("write REQ archive receipt: %w", err)
	}
	short := func(s string) string {
		if len(s) > 12 {
			return s[:12]
		}
		return s
	}
	return fmt.Sprintf("%s status locked → archived (sha %s… → %s…)", reqID, short(shaBefore), short(shaAfter)), nil
}

// flipStatusLineToArchived rewrites the first top-of-file 状态/Status line
// whose value is exactly "locked" to "archived". It touches nothing else —
// baseline content is immutable (L2 first-principle refinement).
// containedRelPath enforces that a runtime-recorded document path stays
// inside the repository before it is used for a write.
func containedRelPath(rel string) (string, error) {
	clean := filepath.Clean(filepath.ToSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("must stay within the repository")
	}
	return clean, nil
}

// atomicWriteREQFile writes via temp-file + rename so a crash cannot
// truncate a human-authored REQ or a receipt mid-write. The original file
// mode is preserved (CreateTemp defaults to 0600, which would strip read
// access for other identities on a git-tracked REQ).
func atomicWriteREQFile(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".req-archive-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		dir.Sync()
		dir.Close()
	}
	return nil
}

func flipStatusLineToArchived(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, sep := range []string{"：", ":"} {
			parts := strings.SplitN(trimmed, sep, 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			if key != "状态" && key != "status" {
				continue
			}
			if strings.TrimSpace(parts[1]) != "locked" {
				return strings.Join(lines, "\n"), false
			}
			idx := strings.LastIndex(line, parts[1])
			if idx < 0 {
				return content, false
			}
			suffix := line[idx+len(parts[1]):]
			lines[i] = line[:idx] + "archived" + suffix
			return strings.Join(lines, "\n"), true
		}
	}
	return content, false
}

func rootedPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func runRuntimeEvidence(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(stderr, "runtime evidence requires <add>")
		return 2
	}
	flags := flag.NewFlagSet("runtime evidence add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime evidence add")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	id := flags.String("id", "", "evidence ID")
	kind := flags.String("kind", "", "evidence kind")
	path := flags.String("path", "", "evidence artifact path relative to repository root")
	responsibility := flags.String("responsibility", "", "owning responsibility ID")
	reviewRound := flags.Int("review-round", 0, "review round; omit for non-round evidence")
	var producedBy, scopeRefs stringListFlag
	flags.Var(&producedBy, "produced-by", "evidence producer; repeatable")
	flags.Var(&scopeRefs, "scope-ref", "evidence scope reference; repeatable")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *expectedRevision < 0 || *id == "" || *kind == "" || *path == "" || len(producedBy) == 0 {
		fmt.Fprintln(stderr, "runtime evidence add requires --expected-revision, --id, --kind, --path and --produced-by")
		return 2
	}
	var round *int
	if *reviewRound != 0 {
		round = reviewRound
	}
	next, err := runtime.RecordEvidence(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), runtime.EvidenceRequest{
		ExpectedRevision: *expectedRevision,
		ID:               *id,
		Kind:             *kind,
		Path:             *path,
		ProducedBy:       append([]string(nil), producedBy...),
		ResponsibilityID: *responsibility,
		ReviewRound:      round,
		ScopeRefs:        append([]string(nil), scopeRefs...),
		Validator:        semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime evidence add", err))
		return 1
	}
	entry := map[string]any{}
	for _, raw := range next.State["evidence"].([]any) {
		if item, ok := raw.(map[string]any); ok && item["id"] == *id {
			entry = item
			break
		}
	}
	// One-line receipt: the full snapshot stays readable via `s10 status` /
	// loop-state; dumping it here buried the actionable fields (2026-08-28
	// walkthrough UX finding).
	return encodeJSON(stdout, map[string]any{
		"recorded":            true,
		"id":                  *id,
		"kind":                *kind,
		"revision":            next.Revision,
		"path":                entry["path"],
		"sha256":              entry["sha256"],
		"review_round":        entry["review_round"],
		"baseline_generation": entry["baseline_generation"],
	})
}

func runRuntimeChange(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprintln(stderr, "runtime change requires <create>")
		return 2
	}
	flags := flag.NewFlagSet("runtime change create", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "runtime change create")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	expectedRevision := flags.Int("expected-revision", -1, "expected runtime revision")
	inputPath := flags.String("input", "", "JSON Change Record input path")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *expectedRevision < 0 || *inputPath == "" {
		fmt.Fprintln(stderr, "runtime change create requires --expected-revision and --input")
		return 2
	}
	data, err := os.ReadFile(resolveRootPath(*root, *inputPath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	var input change.Input
	if err := json.Unmarshal(data, &input); err != nil {
		fmt.Fprintf(stderr, "runtime change create: invalid input JSON: %v\n", err)
		return 2
	}
	stateData, err := os.ReadFile(resolveRootPath(*root, *statePath))
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	if bound, ok := state["bound_req"].(map[string]any); ok {
		if input.REQRef == "" {
			input.REQRef, _ = bound["id"].(string)
		}
		if input.REQSHA == "" {
			input.REQSHA, _ = bound["sha256"].(string)
		}
	}
	record, err := change.BuildRecord(input)
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	next, err := runtime.CreateChange(*root, resolveRootPath(*root, *statePath), resolveRootPath(*root, *journalPath), runtime.ChangeRequest{
		ExpectedRevision: *expectedRevision,
		Record:           record,
		Validator:        semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		fmt.Fprintln(stderr, formatFailure("runtime change create", err))
		return 1
	}
	return encodeJSON(stdout, next)
}

func resolveRootPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

// resolveExpectedRevision reads the current runtime revision when the
// caller omitted `--expected-revision` (value -1). CAS still applies to
// the write itself — the flag just removes the hand-shake step of reading
// the state first (L3-S6 E2E pass).
func resolveExpectedRevision(root, statePath string, provided int) (int, error) {
	if provided >= 0 {
		return provided, nil
	}
	snapshot, err := runtime.NewStore(resolveRootPath(root, statePath), resolveRootPath(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return 0, fmt.Errorf("read current runtime revision (omit --expected-revision or read it with `status`): %w", err)
	}
	return snapshot.Revision, nil
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func parseEvidence(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		kind, reference, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(reference) == "" {
			return nil, fmt.Errorf("--evidence must use kind=reference")
		}
		result[strings.TrimSpace(kind)] = strings.TrimSpace(reference)
	}
	return result, nil
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "validate")
	all := flags.Bool("all", false, "validate all Harness artifacts")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if !*all {
		fmt.Fprintln(stderr, "validate requires --all")
		return 2
	}
	if err := semantic.ValidateRepository(*root); err != nil {
		fmt.Fprintln(stderr, formatFailure("validation", err))
		return 1
	}
	fmt.Fprintln(stdout, "validation passed")
	return 0
}

func runDryRun(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dry-run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "dry-run")
	root := flags.String("root", ".", "repository root")
	fixture := flags.String("fixture", "", "Hook input fixture")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *fixture == "" {
		fmt.Fprintln(stderr, "dry-run requires --fixture")
		return 2
	}
	file, err := os.Open(*fixture)
	if err != nil {
		fmt.Fprintf(stderr, "open fixture: %v\n", err)
		return 1
	}
	defer file.Close()
	return evaluate(*root, "", file, stdout, stderr, false)
}

func runHook(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hook", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "hook")
	event := flags.String("event", "", "Claude Code Hook event")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *event == "" {
		fmt.Fprintln(stderr, "hook requires --event")
		return 2
	}
	return evaluate(*root, *event, stdin, stdout, stderr, true)
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "doctor")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	journalPath := flags.String("journal", ".claude/loop-events.jsonl", "runtime journal path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if err := semantic.ValidateManualAgreement(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: %v\n", err)
		return 1
	}
	if err := semantic.ValidateRepository(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: %v\n", err)
		return 1
	}
	if err := qualitygate.ValidateEvidenceCatalog(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: %v\n", err)
		return 1
	}
	if code := reportPolicyRefDrift(*root, *statePath, *journalPath, stdout, stderr); code != 0 {
		return code
	}
	if out, err := metrics.FormatDoctor(*root); err != nil {
		fmt.Fprintf(stderr, "doctor failed: read loop metrics: %v\n", err)
		return 1
	} else {
		fmt.Fprintln(stdout, out)
	}
	fmt.Fprintln(stdout, "doctor passed: structural schemas, examples, semantic links valid; manual current")
	fmt.Fprintln(stdout, "doctor note: runtime health is reported separately by `loop-harness health --root .`")
	return 0
}

// runHealth reports cumulative runtime signals without re-running the
// repository's structural doctor. This separation prevents a large historical
// counter from being mistaken for a current schema failure, while still
// giving CI/operators an explicit --fail-on-degraded choice.
func runHealth(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("health", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "health")
	root := flags.String("root", ".", "repository root")
	failOnDegraded := flags.Bool("fail-on-degraded", false, "return exit 1 when historical runtime signals require inspection")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	out, err := metrics.FormatHealth(*root)
	if err != nil {
		fmt.Fprintf(stderr, "health failed: read loop metrics: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, out)
	if *failOnDegraded {
		degraded, err := metrics.HealthDegraded(*root)
		if err != nil {
			fmt.Fprintf(stderr, "health failed: classify runtime signals: %v\n", err)
			return 1
		}
		if degraded {
			return 1
		}
	}
	return 0
}

// reportPolicyRefDrift surfaces divergence between the Hook policy reference
// recorded in `hook_control.policy_ref` and the policy document on disk
// (BUG-039-12; REQ-039 §11, SYNC-039 §6-7).
//
// `policy_ref` is a bind-time snapshot of the enforced safety boundary. When
// `docs/hook-policy.json` is rewritten in place the snapshot goes stale, and
// the runtime keeps attributing Hook decisions to a policy version/digest that
// is no longer what the Hook actually loads — an audit inconsistency rather
// than a runtime failure. doctor is the detector; the fix path it names is
// `runtime reconcile-policy-ref`.
//
// A repository with no runtime state yet is not a finding: doctor also runs on
// unbound checkouts, so a missing state file is skipped silently.
func reportPolicyRefDrift(root, statePath, journalPath string, stdout, stderr io.Writer) int {
	resolvedState := statePath
	if !filepath.IsAbs(resolvedState) {
		resolvedState = filepath.Join(root, resolvedState)
	}
	if _, err := os.Stat(resolvedState); err != nil {
		return 0
	}
	resolvedJournal := journalPath
	if !filepath.IsAbs(resolvedJournal) {
		resolvedJournal = filepath.Join(root, resolvedJournal)
	}
	drift, err := runtime.NewStore(resolvedState, resolvedJournal).InspectPolicyRef(root)
	if err != nil {
		fmt.Fprintf(stderr, "doctor failed: inspect hook policy reference: %v\n", err)
		return 1
	}
	if !drift.Drifted() {
		fmt.Fprintf(stdout, "doctor: hook_control.policy_ref consistent (version=%s)\n", drift.RecordedVersion)
		return 0
	}
	switch {
	case drift.Missing:
		fmt.Fprintln(stderr, "doctor failed: runtime state has no hook_control.policy_ref; re-bind the runtime or run: loop-harness runtime reconcile-policy-ref --root .")
	case drift.FileMissing:
		fmt.Fprintf(stderr, "doctor failed: hook policy %s recorded in hook_control.policy_ref does not exist on disk\n", drift.Path)
	default:
		fmt.Fprintf(stderr, "doctor failed: hook_control.policy_ref drifted from %s\n", drift.Path)
		if drift.VersionDrifted() {
			fmt.Fprintf(stderr, "  version recorded=%s on-disk=%s\n", drift.RecordedVersion, drift.OnDiskVersion)
		}
		if drift.SHADrifted() {
			fmt.Fprintf(stderr, "  sha256  recorded=%s on-disk=%s\n", drift.RecordedSHA256, drift.OnDiskSHA256)
		}
		fmt.Fprintln(stderr, "  fix: loop-harness runtime reconcile-policy-ref --root .")
	}
	return 1
}

func evaluate(root, expectedEvent string, input io.Reader, stdout, stderr io.Writer, renderHook bool) int {
	evaluationStarted := time.Now()
	var request policy.Input
	if err := json.NewDecoder(input).Decode(&request); err != nil {
		fmt.Fprintf(stderr, "decode Hook input: %v\n", err)
		return 1
	}
	if expectedEvent != "" && request.Event != expectedEvent {
		fmt.Fprintf(stderr, "Hook argument event %q does not match input event %q\n", expectedEvent, request.Event)
		return 1
	}
	// Official TeammateIdle payloads carry teammate_name instead of agent_id
	// (Claude Code 2.1.218). Normalize once so hookctx resolution, the
	// Controller and the audit envelope all identify the same teammate
	// instead of guessing (L4 §15.2 P0-1); the original payload fields stay
	// on the Input untouched.
	if request.AgentID == "" {
		request.AgentID = request.TeammateName
	}
	// PostToolUse(SendMessage) is a pure observer (L3-S7 §8, L4 §7.4): it
	// never runs the Quality Gate, never persists a gate milestone, and
	// never denies. It short-circuits here so no control-cycle machinery
	// runs for it.
	if request.Event == "PostToolUse" {
		return runPostToolUseHook(root, request, stdout, stderr)
	}
	if request.Event == "PostToolUseFailure" || request.Event == "ConfigChange" {
		return runNativeObserverHook(root, request, stdout, stderr, evaluationStarted)
	}
	if request.Runtime.RuntimeID == "" {
		context, err := hookctx.Load(root, request.AgentID)
		if err != nil {
			// Keep the error until after the controller projection. A
			// mutating PreToolUse must fail closed when the runtime facts
			// needed to determine its write surface are unavailable.
			request.Runtime = policy.RuntimeContext{}
		} else {
			request.Runtime = context
		}
	}
	// The Hook policy document is the minimal-safety authority. If it
	// cannot be loaded, evaluate() cannot render a safety decision and
	// the Hook output is meaningless — return the load error instead of
	// silently falling through. The controller cycle (run below) does
	// NOT depend on the policy document; it only needs the catalog and
	// the runtime store.
	if _, err := policy.Load(filepath.Join(root, "docs", "hook-policy.json")); err != nil {
		fmt.Fprintf(stderr, "load policy: %v\n", err)
		return 1
	}

	// Main Stop is a preflight gate. It must run before the Controller because
	// the Controller is allowed to commit one automatic transition and refresh
	// the Runtime milestone; a Stop that is already known to be illegal must not
	// mutate the cursor while discovering that fact (HOOK-B03).
	var controlResult controller.ControlResult
	var decision policy.Decision
	mainStopBlocked := false
	if request.Event == "Stop" {
		if stopDecision, blocked := hook.MainStopDecision(root, request); blocked {
			decision = stopDecision
			mainStopBlocked = true
		}
	}

	if !mainStopBlocked {
		// The Hook entrypoint delegates the canonical control cycle to the
		// internal/controller package. That cycle runs the eleven steps of
		// BUG-039-02 §4.1 (snapshot → gate → optional one Transition →
		// committed snapshot → milestone refresh → final safety →
		// ControlResult). The minimal safety policy still produces the
		// `Decision` consumed downstream by the envelope and renderer; the
		// controller only adds Quality Gate progress and (when applicable)
		// auto-commits a single Transition before the safety verdict.
		controlResult = runControlCycleForHook(root, request)
		decision = projectControlDecision(controlResult)
		if request.Event == "PreToolUse" && hookInputMayMutate(request) && request.Runtime.RuntimeID == "" && controlResult.Error != "" && runtimeCheckpointMissing(root) {
			decision = policy.Decision{
				Decision:       "deny",
				RuleID:         policy.RuleRuntimeUnreadable,
				Reason:         "runtime facts are unreadable; mutating tools are blocked until the loop runtime is restored",
				Recovery:       []string{"restore .claude/loop-state.json and .claude/loop-events.jsonl", "run `loop-harness runtime inspect --root .`", "retry the tool after the runtime becomes readable"},
				Retry:          policy.RetryAfterRecoveryValidation,
				HumanRequired:  false,
				MatchedRuleIDs: []string{policy.RuleRuntimeUnreadable},
			}
		}
		refreshGuidanceFromController(root, &request, &decision, controlResult)
	}
	// Lifecycle hooks are the agent's re-entry points. Inject only a bounded
	// native context packet: SessionStart gets the current stage/next action;
	// SubagentStart additionally gets an Assignment brief when the platform
	// payload maps to exactly one runtime assignment. Ambiguity stays
	// fail-open and leaves the full Guidance packet as the source of truth.
	if request.Event == "SessionStart" || request.Event == "SubagentStart" {
		var assignments []hookctx.AssignmentContext
		if request.Event == "SubagentStart" {
			if loaded, err := hookctx.LoadFull(root, request.AgentID); err == nil && loaded != nil {
				assignments = loaded.Assignments
			}
		}
		decision.AdditionalContext = hook.BuildLifecycleAdditionalContext(request.Event, request, decision, assignments)
	}
	// L4 §15.2 P0-5: the PreToolUse(TaskUpdate) self-claim guard needs an
	// identified agent; the Controller cycle's safety input carries no Agent
	// context, so the agent-scoped rule is evaluated here against the
	// hookctx-resolved runtime.
	if decision.Decision == "allow" && request.Event == "PreToolUse" {
		if agentDecision, blocked := policy.EvaluateAgentScoped(request); blocked {
			decision = agentDecision
		}
	}
	// L4 §15.2 P0-2: TeammateIdle/SubagentStop use the real platform
	// control — a block decision exits 2 with the feedback on stderr so the
	// platform continues the same agent (render branch below).
	//
	// Order contract: StopIdleDecision must always run when the event is a
	// stop/idle event AND the controller cycle did NOT return a real
	// block. The legacy gate `decision.Decision == "allow"` was correct on
	// the verification lifecycle (where the Controller's projection also
	// surfaces the S7 report-complete check), but it falls open on every
	// non-verification lifecycle — S8 bug_resolution.investigation,
	// acceptance, paused, etc. The Controller cycle on those phases
	// returns StatusSatisfied + allow, and the stop/idle gate is the only
	// authority that can block the platform from letting an agent go
	// idle before it has registered its PLAN_REPORT or Result. We
	// therefore always call StopIdleDecision on stop/idle events here; a
	// real controller block above is preserved unchanged.
	if hook.IsStopIdleEvent(request.Event) && !isDenyingHookDecision(decision.Decision) {
		if stopDecision, blocked := hook.StopIdleDecision(root, request); blocked {
			decision = stopDecision
		}
	}
	// Stop is the Main-session counterpart to the Worker stop/idle gate. The
	// preflight above handles a known pending review assignment before any
	// Controller mutation. If the preflight allowed, re-check after the normal
	// cycle because a concurrent worker may have submitted a Result meanwhile;
	// an already-blocked preflight decision remains authoritative.
	if request.Event == "Stop" && !isDenyingHookDecision(decision.Decision) {
		if stopDecision, blocked := hook.MainStopDecision(root, request); blocked {
			decision = stopDecision
		}
	}
	// Record the measured controller/policy path before the envelope is
	// persisted. A platform timeout kills the process before this point, so a
	// missing record remains a useful timeout signal rather than a fabricated
	// timed_out=true value.
	decision.ElapsedMS = time.Since(evaluationStarted).Milliseconds()
	envelope := buildEnvelopeFromController(root, request, decision, controlResult, time.Now())
	// envelopeWithQualityGate carries the layered Controller projection
	// alongside the legacy hook-policy envelope fields. On PreToolUse the
	// outbox record AND the stdout wire payload must both expose
	// quality_gate so an external auditor and the Agent see the same
	// status (BUG-039-03 §4.1).
	envelopeWithQualityGate := any(envelope)
	if request.Event == "PreToolUse" {
		envelopeWithQualityGate = envelopeWithQualityGateMap(envelope, controlResult)
	}
	if !renderHook {
		if err := json.NewEncoder(stdout).Encode(envelopeWithQualityGate); err != nil {
			fmt.Fprintf(stderr, "encode decision: %v\n", err)
			return 1
		}
		return 0
	}
	if err := audit.NewOutbox(filepath.Join(root, ".claude", "hook-decisions.jsonl")).Append(envelopeWithQualityGate); err != nil {
		// For denying decisions the deny payload is more important than the
		// an audit-write error here would mask the deny. For all other decisions
		// the audit trail is the only durable record, so its failure is fatal.
		fmt.Fprintf(stderr, "append Hook audit: %v\n", err)
		if !isDenyingHookDecision(decision.Decision) {
			return 1
		}
		// Audit write failed but block payload is intact; fall through to write stdout.
	}
	// Audit-classification decisions are observation-only — no user-visible
	// hookSpecificOutput/systemMessage payload and no exit-code signal. The
	// full DecisionEnvelope already landed in the outbox above, so skip the
	// Render call (whose audit branch writes a SECOND, sparse record via
	// appendAuditLine — the dual-write defect DV-1 / QA-1 §4 T2).
	if decision.Decision == "audit" {
		return 0
	}
	if request.Event == "PreToolUse" || decision.Guidance != nil {
		_ = metrics.RecordRecoveryPacket(root)
	}
	// TeammateIdle/SubagentStop block: the official Claude Code control is
	// exit code 2 with the feedback on stderr (routed back to that same
	// agent); no stdout payload is emitted for the blocked stop/idle.
	if hook.IsStopIdleEvent(request.Event) && isDenyingHookDecision(decision.Decision) {
		fmt.Fprintln(stderr, hook.RenderStopBlockFeedback(decision))
		return 2
	}
	if request.Event == "Stop" && isDenyingHookDecision(decision.Decision) {
		fmt.Fprintln(stderr, hook.RenderStopBlockFeedback(decision))
		return 2
	}
	// PreToolUse uses the layered Controller-driven render path
	// (PreToolUseWithQualityGate) so the wire envelope carries the
	// `quality_gate` object alongside permissionDecision. Lifecycle events
	// (SessionStart, SubagentStart, ...) continue to flow through the legacy
	// hook-policy renderer. BUG-039-03 §4.1.
	var output []byte
	var code int
	var err error
	if request.Event == "PreToolUse" {
		output, code, err = hook.PreToolUseWithQualityGate(decision, controlResult)
	} else {
		output, code, err = hook.RenderWithAdditionalContext(root, request.Event, decision, request.Runtime, decision.AdditionalContext)
	}
	if err != nil {
		if isDenyingHookDecision(decision.Decision) {
			return 2
		}
		fmt.Fprintf(stderr, "render Hook output: %v\n", err)
		return 1
	}
	if len(output) > 0 {
		if _, err := stdout.Write(append(output, '\n')); err != nil {
			if isDenyingHookDecision(decision.Decision) {
				return 2
			}
			fmt.Fprintf(stderr, "write Hook output: %v\n", err)
			return 1
		}
	}
	if isDenyingHookDecision(decision.Decision) {
		return 2
	}
	return code
}

func hookInputMayMutate(request policy.Input) bool {
	switch request.ToolName {
	case "Write", "Edit", "MultiEdit", "NotebookEdit", "Bash":
		return true
	default:
		return policy.IsMCPTool(request.ToolName)
	}
}

// runNativeObserverHook consumes platform-native observation events without
// entering the lifecycle Controller. These events cannot safely veto the
// originating operation; their value is a durable, deduplicated audit signal
// that can be correlated with the existing wrapper and runtime evidence.
func runNativeObserverHook(root string, request policy.Input, stdout, stderr io.Writer, started time.Time) int {
	engine, err := policy.Load(filepath.Join(root, "docs", "hook-policy.json"))
	if err != nil {
		fmt.Fprintf(stderr, "load policy for %s observer: %v\n", hook.NativeObserverSummary(request), err)
		return 0
	}
	decision := hook.NativeObserverDecision(request, time.Since(started))
	envelope := engine.Envelope(request, decision, time.Now())
	if err := audit.NewOutbox(filepath.Join(root, ".claude", "hook-decisions.jsonl")).Append(envelope); err != nil {
		fmt.Fprintf(stderr, "append %s audit: %v\n", hook.NativeObserverSummary(request), err)
		// This event is observation-only. Losing an audit row is reported on
		// stderr, but must not turn a non-vetoing observer into an accidental
		// tool failure or permission gate.
		return 0
	}
	return 0
}

func runtimeCheckpointMissing(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".claude", "loop-state.json"))
	return os.IsNotExist(err)
}

// runPostToolUseHook handles the PostToolUse(SendMessage) observation path:
// identify the sender, and when a PLAN_REPORT is observed for the first
// time, CAS-write agent.plan_reported_ref so the first-write barrier has a
// durable fact. Everything about this path is fail-open: identification
// gaps produce a silent observation, never a block and never an error.
//
// plan_checkpoint dispatch_mode triggers the L4 §3.3 auto-activation
// chain: readback_submitted -> activation_sent -> work_started, with the
// activation envelope's hash chain bound to the plan_report file bytes
// (assignable.AutoAdvanceToWorking). The chain is driven by the plan
// SendMessage payload's `plan_ref` field; if the field is absent the
// observation degrades to the legacy plan_reported_ref-only behavior and
// the recovery verb is named in the stderr note.
func runPostToolUseHook(root string, request policy.Input, stdout, stderr io.Writer) int {
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		// No runtime → nothing to observe into; still emit the envelope.
		fmt.Fprintln(stdout, hook.RenderPostToolUseEnvelope(hook.PostToolUseObservation{Reason: "runtime unreadable"}))
		return 0
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rawAgents, _ := entities["agents"].([]any)
	rows := make([]hook.AgentRow, 0, len(rawAgents))
	for _, raw := range rawAgents {
		agent, _ := raw.(map[string]any)
		if agent == nil {
			continue
		}
		id, _ := agent["id"].(string)
		state, _ := agent["state"].(string)
		mode, _ := agent["dispatch_mode"].(string)
		rows = append(rows, hook.AgentRow{ID: id, State: state, DispatchMode: mode})
	}
	obs := hook.HandlePostToolUse(request, rows)
	if obs.Recorded && obs.Message == "plan_report" {
		planRef := planReportRef(request)
		if err := validatePlanReportCheckpoint(root, snapshot, obs.AgentID, planRef); err != nil {
			obs.Recorded = false
			obs.Reason = "plan_report rejected: " + err.Error()
			fmt.Fprintf(stderr, "note: %s\n", obs.Reason)
			fmt.Fprintln(stdout, hook.RenderPostToolUseEnvelope(obs))
			return 0
		}
		recordPlanCheckpoint(root, statePath, journalPath, snapshot, obs.AgentID, request, stderr)
		// Auto-chain for plan_checkpoint agents. plan_ref is the plan file
		// path the Worker wrote before SendMessage. Gating happens twice:
		// once here (avoid the call entirely for plan_approval_required /
		// one_shot), and again inside AutoAdvanceToWorking as defense in
		// depth.
		dispatchMode := dispatchModeOf(rows, obs.AgentID)
		planRef = planReportRef(request)
		if dispatchMode == "plan_checkpoint" && planRef != "" {
			outcome, err := assignment.AutoAdvanceToWorking(assignment.AutoChainInput{
				Root:        root,
				StatePath:   statePath,
				JournalPath: journalPath,
				AgentID:     obs.AgentID,
				PlanPath:    planRef,
			})
			if err != nil {
				fmt.Fprintf(stderr, "note: plan_checkpoint auto-chain failed (%v); fall back to `runtime agent-begin --agent-id %s --plan %s`\n", err, obs.AgentID, planRef)
			} else if outcome.Chained {
				fmt.Fprintf(stderr, "auto-chain: %s advanced to %s (activation_id=%s)\n", outcome.AgentID, outcome.FinalState, outcome.ActivationID)
			} else if outcome.Reason != "" {
				// Skip is informational (e.g. dispatch_mode changed between
				// rows read and AutoAdvanceToWorking's snapshot). Surface
				// only when the agent is still in reading so the agent-begin
				// fallback verb is called out as the next step.
				fmt.Fprintf(stderr, "note: plan_checkpoint auto-chain skipped for %s: %s\n", obs.AgentID, outcome.Reason)
			}
		}
	}
	fmt.Fprintln(stdout, hook.RenderPostToolUseEnvelope(obs))
	return 0
}

// dispatchModeOf returns the dispatch_mode for the given agent from the rows
// the observer already loaded. Returns "" when the agent is not in the row
// set (the caller already recorded a silent observation in that case).
func dispatchModeOf(rows []hook.AgentRow, agentID string) string {
	for _, r := range rows {
		if r.ID == agentID {
			return r.DispatchMode
		}
	}
	return ""
}

func planReportRef(request policy.Input) string {
	ref, _ := request.ToolInput["plan_ref"].(string)
	if strings.TrimSpace(ref) == "" {
		ref, _ = request.ToolInput["plan_path"].(string)
	}
	return strings.TrimSpace(ref)
}

// validatePlanReportCheckpoint makes the PostToolUse observer's durable
// checkpoint correspond to the current dispatched Assignment. The observer
// remains non-blocking, but a malformed or unrelated plan report must not
// clear the first-write barrier merely because it says message_type=plan_report.
func validatePlanReportCheckpoint(root string, snapshot runtime.Snapshot, agentID, ref string) error {
	if ref == "" {
		return fmt.Errorf("plan_ref is required")
	}
	abs := ref
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(ref))
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return fmt.Errorf("resolve plan_ref: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("plan_ref %q is outside the repository", ref)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read plan_ref %q: %w", ref, err)
	}
	if err := schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data); err != nil {
		return fmt.Errorf("plan_ref schema: %w", err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return fmt.Errorf("decode plan_ref: %w", err)
	}
	if message["message_type"] != "plan_report" || message["agent_id"] != agentID {
		return fmt.Errorf("plan_ref must be a plan_report for Agent %s", agentID)
	}
	if runtimeID, _ := snapshot.State["runtime_id"].(string); runtimeID != "" && message["runtime_id"] != runtimeID {
		return fmt.Errorf("plan_ref runtime_id does not match the current runtime")
	}
	ptr := review.PlanPointerFromState(snapshot.State)
	assignmentID, _ := message["assignment_id"].(string)
	if ptr != nil {
		if revision := integerValue(message["assignment_revision"]); revision != ptr.Revision {
			// A non-S7 workgroup may be active while the previous S7 plan is
			// still present in the projection. Only enforce the ReviewPlan
			// revision when the submitted Assignment is actually one of its
			// rows; S8/S9 assignments are bound by their manifest below.
			reviewMap, _ := snapshot.State["review"].(map[string]any)
			assignments, _ := reviewMap["assignments"].(map[string]any)
			if _, exists := assignments[assignmentID]; exists {
				return fmt.Errorf("plan_ref assignment_revision %d does not match ReviewPlan revision %d", revision, ptr.Revision)
			}
		}
		reviewMap, _ := snapshot.State["review"].(map[string]any)
		assignments, _ := reviewMap["assignments"].(map[string]any)
		row, _ := assignments[assignmentID].(map[string]any)
		if row != nil {
			if row["agent_id"] != agentID {
				return fmt.Errorf("plan_ref Assignment %s is not dispatched to Agent %s", assignmentID, agentID)
			}
			if revision := integerValue(message["assignment_revision"]); revision != ptr.Revision {
				return fmt.Errorf("plan_ref assignment_revision %d does not match ReviewPlan revision %d", revision, ptr.Revision)
			}
			return nil
		}
	}
	return validateManifestPlanReportCheckpoint(root, snapshot, message, agentID)
}

// validateManifestPlanReportCheckpoint validates the generic L4 checkpoint
// for Builder/Investigator workgroups that are not S7 ReviewPlan rows. Their
// Assignment identity lives in the fingerprinted team manifest; the manifest
// assignment is immutable for the lifetime of that dispatch, so its generic
// checkpoint revision is deliberately 1. S9 keeps a separate domain
// RepairAssignment (repair-assignment-*) and maps it to the platform-safe
// manifest id (assignment-s9-*); this function validates the latter while the
// S9 domain PlanReport validates the former.
func validateManifestPlanReportCheckpoint(root string, snapshot runtime.Snapshot, message map[string]any, agentID string) error {
	assignmentID := stringValue(message["assignment_id"])
	teamID := stringValue(message["team_id"])
	taskID := stringValue(message["task_id"])
	if assignmentID == "" || teamID == "" || taskID == "" {
		return fmt.Errorf("manifest-bound plan_ref requires assignment_id, team_id, and task_id")
	}
	if revision := integerValue(message["assignment_revision"]); revision != 1 {
		return fmt.Errorf("manifest-bound Assignment %s uses assignment_revision=1; got %d", assignmentID, revision)
	}

	entities, _ := snapshot.State["entities"].(map[string]any)
	var agent map[string]any
	rawAgents, _ := entities["agents"].([]any)
	for _, raw := range rawAgents {
		candidate, _ := raw.(map[string]any)
		if stringValue(candidate["id"]) == agentID {
			agent = candidate
			break
		}
	}
	if agent == nil {
		return fmt.Errorf("manifest-bound plan_ref Agent %s is not registered", agentID)
	}
	if recordedTeam := stringValue(agent["team_id"]); recordedTeam != "" && recordedTeam != teamID {
		return fmt.Errorf("plan_ref team_id %s does not match Agent %s team %s", teamID, agentID, recordedTeam)
	}
	if !containsStringValue(stringSliceAny(agent["task_ids"]), taskID) {
		return fmt.Errorf("plan_ref TASK %s is outside Agent %s assignment", taskID, agentID)
	}

	var teamRow map[string]any
	rawTeams, _ := entities["teams"].([]any)
	for _, raw := range rawTeams {
		candidate, _ := raw.(map[string]any)
		if stringValue(candidate["id"]) == teamID {
			teamRow = candidate
			break
		}
	}
	if teamRow == nil {
		return fmt.Errorf("manifest-bound plan_ref team %s is not registered", teamID)
	}
	manifestRef := stringValue(teamRow["manifest_ref"])
	if manifestRef == "" {
		return fmt.Errorf("team %s has no manifest_ref for the plan checkpoint", teamID)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root for manifest: %w", err)
	}
	manifestPath := resolveRootPath(root, manifestRef)
	manifestAbs, err := filepath.Abs(manifestPath)
	if err != nil {
		return fmt.Errorf("resolve team manifest %q: %w", manifestRef, err)
	}
	manifestRel, err := filepath.Rel(rootAbs, manifestAbs)
	if err != nil || manifestRel == ".." || strings.HasPrefix(manifestRel, ".."+string(filepath.Separator)) || filepath.IsAbs(manifestRel) {
		return fmt.Errorf("team manifest %q is outside the repository", manifestRef)
	}
	manifestBytes, err := os.ReadFile(manifestAbs)
	if err != nil {
		return fmt.Errorf("read dispatched team manifest %s: %w", manifestRef, err)
	}
	if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		return fmt.Errorf("dispatched team manifest schema: %w", err)
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		return fmt.Errorf("dispatched team manifest semantics: %w", err)
	}
	var manifest struct {
		RuntimeID   string `json:"runtime_id"`
		WorkgroupID string `json:"workgroup_id"`
		Assignments []struct {
			AssignmentID       string `json:"assignment_id"`
			AgentID            string `json:"agent_id"`
			AgentDefinitionRef string `json:"agent_definition_ref"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("decode dispatched team manifest: %w", err)
	}
	if manifest.RuntimeID != stringValue(snapshot.State["runtime_id"]) {
		return fmt.Errorf("plan_ref manifest runtime_id does not match the current runtime")
	}
	if manifest.WorkgroupID != teamID {
		return fmt.Errorf("plan_ref team_id %s does not match manifest workgroup_id %s", teamID, manifest.WorkgroupID)
	}
	for _, assignment := range manifest.Assignments {
		if assignment.AssignmentID != assignmentID {
			continue
		}
		if assignment.AgentID != agentID {
			return fmt.Errorf("plan_ref Assignment %s is owned by Agent %s, not %s", assignmentID, assignment.AgentID, agentID)
		}
		return nil
	}
	return fmt.Errorf("plan_ref Assignment %s is not declared by manifest %s", assignmentID, manifestRef)
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// recordPlanCheckpoint writes agent.plan_reported_ref once (idempotent).
// The ref is symbolic (the SendMessage message id) because the platform
// payload does not carry a verifiable file path; the authoritative,
// schema-validated registration remains `runtime agent-event
// --event readback_submitted` with the plan file.
func recordPlanCheckpoint(root, statePath, journalPath string, snapshot runtime.Snapshot, agentID string, request policy.Input, stderr io.Writer) {
	ref := planReportRef(request)
	if ref == "" {
		return
	}
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	_, err := store.Update(snapshot.Revision, runtime.Mutation{
		EventID:        fmt.Sprintf("evt-plan-observed-%s-r%d", agentID, snapshot.Revision+1),
		TransitionID:   "PLAN-OBSERVATION",
		Event:          "plan_report_observed",
		Actor:          "hook_controller",
		IdempotencyKey: fmt.Sprintf("hook:plan-observed:%s", agentID),
		RuntimeID:      runtimeIDString(snapshot.State),
		OccurredAt:     time.Now().UTC(),
		Apply: func(state map[string]any) error {
			entities, _ := state["entities"].(map[string]any)
			for _, raw := range entities["agents"].([]any) {
				agent, _ := raw.(map[string]any)
				if agent == nil || agent["id"] != agentID {
					continue
				}
				if existing, _ := agent["plan_reported_ref"].(string); existing != "" {
					return nil // already recorded — idempotent
				}
				agent["plan_reported_ref"] = ref
				return nil
			}
			return fmt.Errorf("agent %s not found", agentID)
		},
	})
	if err != nil {
		// Observation must never fail the tool call.
		fmt.Fprintf(stderr, "note: plan_report observation not persisted (%v)\n", err)
	}
}

func runtimeIDString(state map[string]any) string {
	id, _ := state["runtime_id"].(string)
	return id
}

// runControlCycleForHook adapts the policy.Input the Hook transport emits
// to the ControlRequest the internal/controller package expects, runs the
// cycle, and returns the resulting ControlResult. The cycle itself is the
// single authority for Quality Gate progress and auto-Transition commits
// (BUG-039-02 §4.1).
func runControlCycleForHook(root string, request policy.Input) controller.ControlResult {
	controlReq := controller.ControlRequest{
		Root:        root,
		Event:       request.Event,
		ToolName:    request.ToolName,
		ToolInput:   request.ToolInput,
		TargetID:    request.TargetID,
		AgentID:     request.AgentID,
		SessionID:   request.SessionID,
		Runtime:     request.Runtime,
		HookPayload: map[string]any{},
	}
	result, err := controller.RunControlCycle(contextForHook(request), controlReq)
	if err != nil {
		// RunControlCycle surfaces user-visible errors via the result
		// struct; reaching here is reserved for programmer errors (e.g.
		// missing root). Surface them as an unknown gate verdict.
		return controller.ControlResult{
			Decision:  policy.Decision{Decision: "allow", Reason: "controller cycle failed: " + err.Error()},
			Error:     err.Error(),
			ErrorCode: "LOOP_RUNTIME_INVALID",
			QualityGate: controller.QualityGateResult{
				Status:       controller.StatusUnknown,
				Missing:      []string{},
				EvidenceRefs: []string{},
			},
		}
	}
	return result
}

func contextForHook(request policy.Input) context.Context {
	if request.SessionID == "" {
		return context.Background()
	}
	type sessionKey struct{}
	return context.WithValue(context.Background(), sessionKey{}, request.SessionID)
}

// projectControlDecision folds a ControlResult back into a policy.Decision
// for the existing envelope + renderer path. Quality Gate `not_ready`,
// `satisfied`, and `unknown` all map to safety `allow` (BE-039 §3.2 /
// §5.2 / REQ-039 §10.2); only an actual safety block is projected as
// `block` and only `advanced` keeps the tool-default `allow` after a
// successful transition.
func projectControlDecision(result controller.ControlResult) policy.Decision {
	if result.Decision.Decision == "block" || result.Decision.Decision == "deny" {
		return result.Decision
	}
	// A warning is still a policy result even though the tool remains
	// allowed. Preserve its rule, reason, recovery and retry fields so the
	// Agent sees the classification guidance instead of an indistinguishable
	// allow verdict (unknown MCP tools are the canonical example).
	if result.Decision.Decision == "warn" {
		return result.Decision
	}
	switch result.QualityGate.Status {
	case controller.StatusBlocked:
		// Quality gate was projected to blocked because the final safety
		// layer denied. The Decision field already carries the block
		// payload; we just ensure the gate status survives the round-trip.
		decision := result.Decision
		if !isDenyingHookDecision(decision.Decision) {
			decision.Decision = "block"
			decision.RuleID = policy.RuleLockedArtifactWrite
			decision.Reason = "final safety block"
		}
		return decision
	case controller.StatusAdvanced, controller.StatusSatisfied, controller.StatusNotReady, controller.StatusUnknown:
		// Always allow the tool; the Quality Gate verdict is the
		// positive side (Guidance), not a permission verdict.
		decision := result.Decision
		decision.Decision = "allow"
		decision.RuleID = ""
		decision.Reason = "no policy rule blocked this action"
		decision.Retry = "not_applicable"
		decision.HumanRequired = false
		decision.MatchedRuleIDs = nil
		return decision
	}
	return result.Decision
}

// refreshGuidanceFromController attaches the positive Guidance packet
// the controller produced (or the legacy helper when the cycle fell back
// to read-only projection) to the Decision so the renderer can include
// the recovery / next step in the systemMessage.
//
// For PreToolUse, when the Controller produced a non-empty QualityGate,
// this helper also persists the gate into the Runtime Milestone via
// refreshMilestoneWithGate so the milestone projection matches what the
// hook emitted on the wire (BUG-039-07 wiring).
func refreshGuidanceFromController(root string, request *policy.Input, decision *policy.Decision, result controller.ControlResult) {
	persistGateForPreToolUse(root, request, decision, result)
	if decision.Guidance != nil {
		return
	}
	if request.Event == "PreToolUse" && result.Snapshot.State != nil {
		guidance := BuildGuidanceForState(root, result.Snapshot.State, request.Event, *request)
		decision.Guidance = &guidance
		return
	}
	if isGuidanceEvent(request.Event) {
		guidance, _, err := ReconcileGuidanceForController(root, request.Event, *request)
		if err == nil {
			decision.Guidance = &guidance
			return
		}
	}
	if result.Snapshot.State != nil {
		guidance := BuildGuidanceForState(root, result.Snapshot.State, request.Event, *request)
		decision.Guidance = &guidance
		return
	}
	if runtimeStateMissing(root) {
		decision.Guidance = FreshStartGuidanceForController(root, request.Event)
		return
	}
	decision.Guidance = FallbackGuidanceForController(request.Event)
}

// persistGateForPreToolUse threads the Controller's QualityGate into the
// Runtime Milestone for PreToolUse events. The gate is the source of truth
// for the milestone.quality_gate projection; without this wiring the field
// would be empty in production even though the schema and helpers support it.
// Other event types (SessionStart/PreCompact/SubagentStop/TeammateIdle) do
// not run the control cycle and therefore have no gate to persist; their
// milestone refresh continues to use the zero-gate legacy path.
//
// The presence test is `Status`, not `GateID`/`Fingerprint` (BUG-039-12
// repair). A gate that the Controller could not resolve to a single candidate
// still carries a real observation — e.g. `status=unknown` with
// `error_code=LOOP_TRIGGER_CONFLICT` has no gate ID and no fingerprint, yet it
// is precisely the state the Agent needs to see in the Recovery Packet after a
// compact. Gating on GateID/Fingerprint silently dropped every unknown gate and
// left milestone.quality_gate absent on exactly the runtimes that needed it.
// Only a wholly zero-valued gate (no Status at all, i.e. the cycle never ran)
// is skipped.
func persistGateForPreToolUse(root string, request *policy.Input, decision *policy.Decision, result controller.ControlResult) {
	if request == nil || request.Event != "PreToolUse" {
		return
	}
	if result.QualityGate.Status == "" {
		return
	}
	if decision.Guidance == nil {
		guidance := BuildGuidanceForState(root, result.Snapshot.State, request.Event, *request)
		decision.Guidance = &guidance
	}
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if _, _, err := refreshMilestoneWithGate(root, statePath, journalPath, result.Snapshot, *decision.Guidance, request.Event, result.QualityGate); err != nil {
		// Persistence failure is non-fatal for the hook verdict: the
		// wire envelope still carries quality_gate. Expose the bounded
		// reason and the next action in the same packet so the Agent does
		// not mistake a missing milestone for permission to improvise.
		reason := milestoneRefreshFailureReason(err)
		decision.Guidance.Automation = append(decision.Guidance.Automation,
			fmt.Sprintf("milestone refresh deferred [%s]; the quality_gate in this Hook packet remains authoritative; retry on the next Hook", reason),
		)
		if reason != "stale_revision" {
			decision.Guidance.Automation = append(decision.Guidance.Automation,
				"if the refresh failure repeats, stop normal work and run `loop-harness runtime reconcile --root .` to inspect/recover the Runtime pair",
			)
		}
	}
}

// buildEnvelopeFromController wraps the engine.Envelope call so the
// evaluate() entrypoint continues to render the same JSON schema. The
// minimal safety engine is still required to populate the policy metadata
// fields (policy_id/policy_version/policy_sha256) used by the outbox and
// audit pipeline.
//
// For PreToolUse the envelope is augmented with the Controller's layered
// `quality_gate` projection so the outbox record mirrors the wire payload
// (BUG-039-03 §4.1). The Controller-produced quality_gate is the single
// source of truth; this helper never fabricates status="advanced".
func buildEnvelopeFromController(root string, request policy.Input, decision policy.Decision, controlResult controller.ControlResult, evaluatedAt time.Time) policy.DecisionEnvelope {
	engine, err := policy.Load(filepath.Join(root, "docs", "hook-policy.json"))
	if err != nil {
		// The minimal safety policy load failure is not fatal: the
		// controller's verdict is authoritative. Synthesize an envelope
		// with empty policy metadata.
		envelope := policy.DecisionEnvelope{
			SchemaVersion: "1.1.0",
			DecisionID:    fmt.Sprintf("hook-decision-%d", evaluatedAt.UnixNano()),
			HookEvent:     request.Event,
			SessionID:     request.SessionID,
			Decision:      decision.Decision,
			Reason:        decision.Reason,
			Recovery:      decision.Recovery,
			Retry:         decision.Retry,
			HumanRequired: decision.HumanRequired,
			EvaluatedAt:   evaluatedAt.UTC().Format(time.RFC3339Nano),
			ElapsedMS:     decision.ElapsedMS,
		}
		if decision.Guidance != nil {
			envelope.Guidance = decision.Guidance
		}
		return envelope
	}
	return engine.Envelope(request, decision, evaluatedAt)
}

// qualityGateEnvelopeFields converts the Controller's Quality Gate
// projection into the wire shape required by hook-decision.schema.json
// (status, gate_id, candidate_transition, observed_revision, fingerprint,
// missing, evidence_refs, transition_committed, next_cursor). The map is
// intended to be merged into a serialised DecisionEnvelope as the
// `quality_gate` top-level property so the outbox and the wire payload
// agree on what the Controller observed (BUG-039-03 §4.1).
func qualityGateEnvelopeFields(qg controller.QualityGateResult) map[string]any {
	missing := qg.Missing
	if missing == nil {
		missing = []string{}
	}
	evidenceRefs := qg.EvidenceRefs
	if evidenceRefs == nil {
		evidenceRefs = []string{}
	}
	conflicts := qg.Conflicts
	if conflicts == nil {
		conflicts = []string{}
	}
	return map[string]any{
		"quality_gate": map[string]any{
			"status":               string(qg.Status),
			"gate_id":              qg.GateID,
			"candidate_transition": qg.CandidateTransition,
			"observed_revision":    qg.ObservedRevision,
			"fingerprint":          qg.Fingerprint,
			"missing":              missing,
			"evidence_refs":        evidenceRefs,
			"error_code":           qg.ErrorCode,
			"conflicts":            conflicts,
			"transition_committed": qg.TransitionCommitted,
			"next_cursor":          qg.NextCursor,
		},
	}
}

// envelopeWithQualityGateMap returns a map[string]any copy of the supplied
// DecisionEnvelope with the Controller's quality_gate block merged in. The
// map is what the audit outbox + stdout serialiser consume so the layered
// projection lands in both channels without adding a field to
// policy.DecisionEnvelope (which would create a controller -> policy
// import cycle per BUG-039-03 §4.2).
//
// Returns the bare envelope when the marshal round-trip fails so the
// evaluation pipeline never fails because of an audit-shape defect.
func envelopeWithQualityGateMap(envelope policy.DecisionEnvelope, result controller.ControlResult) any {
	data, err := json.Marshal(envelope)
	if err != nil {
		return envelope
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return envelope
	}
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range qualityGateEnvelopeFields(result.QualityGate) {
		out[k] = v
	}
	return out
}

func isDenyingHookDecision(decision string) bool {
	return decision == "deny" || decision == "block"
}

// runImpact exposes the evidence-impact analysis as a CLI subcommand. It reads
// the current runtime state, computes which historical evidence is affected by
// the supplied changed paths, and prints the result as JSON.
//
// Usage:
//
//	loop-harness impact analyze --root . --changed docs/contracts/CONTRACTS-002.md
func runImpact(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness impact analyze --root . --changed <path>...")
		return 2
	}
	if args[0] != "analyze" {
		fmt.Fprintf(stderr, "unknown impact subcommand %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("impact analyze", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "impact analyze")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	var changed changedPaths
	flags.Var(&changed, "changed", "changed path (repeatable)")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if len(changed) == 0 {
		fmt.Fprintln(stderr, "impact analyze: at least one --changed path is required")
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, *statePath))
	if err != nil {
		fmt.Fprintf(stderr, "read state: %v\n", err)
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(stderr, "parse state: %v\n", err)
		return 1
	}
	impacts := impactanalysis.ComputeImpact(state, changed)
	result := map[string]any{
		"changed_paths":   changed,
		"impacted_count":  len(impacts),
		"newly_affected":  countNewlyAffected(impacts),
		"already_invalid": countAlreadyInvalid(impacts),
		"impacts":         impacts,
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	return 0
}

type changedPaths []string

func (c *changedPaths) String() string     { return strings.Join(*c, ", ") }
func (c *changedPaths) Set(v string) error { *c = append(*c, v); return nil }

func countNewlyAffected(impacts []impactanalysis.EvidenceImpact) int {
	n := 0
	for _, item := range impacts {
		if !item.AlreadyInvalid {
			n++
		}
	}
	return n
}

func countAlreadyInvalid(impacts []impactanalysis.EvidenceImpact) int {
	n := 0
	for _, item := range impacts {
		if item.AlreadyInvalid {
			n++
		}
	}
	return n
}

// runVerification exposes the clean-round evaluation as a CLI subcommand.
//
// Usage:
//
//	loop-harness verification clean-round --root .
func runVerification(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness verification clean-round --root .")
		return 2
	}
	if args[0] != "clean-round" {
		fmt.Fprintf(stderr, "unknown verification subcommand %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("verification clean-round", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "verification clean-round")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "runtime state path")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	data, err := os.ReadFile(filepath.Join(*root, *statePath))
	if err != nil {
		fmt.Fprintf(stderr, "read state: %v\n", err)
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(stderr, "parse state: %v\n", err)
		return 1
	}
	result := verification.EvaluateCleanRound(state)
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(out))
	if !result.Passed {
		return 1
	}
	return 0
}

func hookTargetPath(input map[string]any) string {
	for _, key := range []string{"file_path", "path", "notebook_path"} {
		if value, _ := input[key].(string); value != "" {
			return value
		}
	}
	return ""
}

// hasProtoMetaHeader enforces the 4-field header mandate from
// docs/rules/ui-prototype.md §5: 设计代数 / 更新 / 路由 / index 链接.
// All four tokens must appear in the file (any ordering); each is matched as a
// fixed substring so the check stays decoupled from HTML structure.
func hasProtoMetaHeader(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	body := string(data)
	for _, marker := range []string{"设计代数", "更新", "路由", "index.html"} {
		if !strings.Contains(body, marker) {
			return false
		}
	}
	return true
}

// hasStoryIDWithReqID verifies stories.md carries at least one S-NNN entry
// referencing a REQ-id, per docs/rules/ui-prototype.md §6.
func hasStoryIDWithReqID(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	body := string(data)
	storyRe := regexp.MustCompile(`(?m)^#+\s*S-\d{3,}\b`)
	reqRe := regexp.MustCompile(`REQ-\d{3,}`)
	loc := storyRe.FindStringIndex(body)
	if loc == nil {
		return false
	}
	return reqRe.MatchString(body[loc[0]:])
}

// hasFlowIDWithReqID verifies flows.md carries at least one F-NNN entry
// referencing a REQ-id, per docs/rules/ui-prototype.md §7.
func hasFlowIDWithReqID(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	body := string(data)
	flowRe := regexp.MustCompile(`(?m)^#+\s*F-\d{3,}\b`)
	reqRe := regexp.MustCompile(`REQ-\d{3,}`)
	loc := flowRe.FindStringIndex(body)
	if loc == nil {
		return false
	}
	return reqRe.MatchString(body[loc[0]:])
}

// runReleaseGraph validates a staged release tree by walking its Skill
// references and asserting each resolves to a file in the tree.
//
// Usage:
//
//	loop-harness release-graph validate --root <staged-tree-path>
func runReleaseGraph(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(stderr, "usage: loop-harness release-graph validate --root <path>")
		return 2
	}
	flags := flag.NewFlagSet("release-graph validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "release-graph validate")
	root := flags.String("root", ".", "staged release tree root")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if err := releasegraph.ValidateStagedRelease(*root); err != nil {
		fmt.Fprintf(stderr, "release-graph validation failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "release-graph validation passed")
	return 0
}

// runManual renders the agent-facing gate specification markdown from
// docs/loop-definition.json plus the guard/action spec registries. Output goes
// to --target (default .claude/bin/loop-harness.md, sitting beside the binary)
// or to stdout when --stdout is set.
//
// Usage:
//
//	loop-harness manual --root . [--target <path>] [--stdout]
func runManual(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("manual", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "manual")
	root := flags.String("root", ".", "repository root")
	target := flags.String("target", transition.ManualTargetPath(), "output path relative to root; defaults to .claude/bin/loop-harness.md next to the binary")
	toStdout := flags.Bool("stdout", false, "write to stdout instead of --target")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	catalog, err := transition.LoadCatalog(*root)
	if err != nil {
		fmt.Fprintf(stderr, "manual: load catalog: %v\n", err)
		return 1
	}
	defData, err := os.ReadFile(filepath.Join(*root, "docs", "loop-definition.json"))
	if err != nil {
		fmt.Fprintf(stderr, "manual: read loop-definition.json: %v\n", err)
		return 1
	}
	markdown := transition.RenderManual(catalog.Definition, transition.ManualOptions{
		TargetPath:           filepath.ToSlash(*target),
		HarnessVersion:       "dev",
		LoopDefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(defData)),
	})
	if *toStdout {
		fmt.Fprint(stdout, markdown)
		return 0
	}
	fullTarget := filepath.Join(*root, *target)
	if err := os.MkdirAll(filepath.Dir(fullTarget), 0o755); err != nil {
		fmt.Fprintf(stderr, "manual: create target dir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(fullTarget, []byte(markdown), 0o644); err != nil {
		fmt.Fprintf(stderr, "manual: write target: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "manual written to %s\n", filepath.ToSlash(*target))
	return 0
}

// runExplain renders the per-transition details for one transition ID. The
// output is the same shape as one entry in the manual, but without the manual
// header or TOC. Used by agents that have just hit a gate failure and want to
// understand one transition quickly.
//
// Usage:
//
//	loop-harness explain <TR-xxx> --root .
func runExplain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: loop-harness explain <TR-xxx> [--root <path>] [--state <path>]")
		return 2
	}
	id := args[0]
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindUsage(flags, "explain")
	root := flags.String("root", ".", "repository root")
	statePath := flags.String("state", ".claude/loop-state.json", "current Runtime state path; read-only")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	catalog, err := transition.LoadCatalog(*root)
	if err != nil {
		fmt.Fprintf(stderr, "explain: load catalog: %v\n", err)
		return 1
	}
	body := transition.RenderTransition(catalog.Definition, id)
	if body == "" {
		fmt.Fprintf(stderr, "explain: transition %q not found in top-level, phase, or global scope\n", id)
		return 1
	}
	stateData, err := os.ReadFile(resolveRootPath(*root, *statePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprint(stdout, body)
			fmt.Fprintf(stdout, "\nCurrent Runtime evidence candidates unavailable: state file %q does not exist.\n", filepath.ToSlash(*statePath))
			return 0
		}
		fmt.Fprintf(stderr, "explain: read current Runtime state: %v\n", err)
		return 1
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		fmt.Fprintf(stderr, "explain: decode current Runtime state: %v\n", err)
		return 1
	}
	body = transition.RenderTransitionWithCandidates(catalog.Definition, id, *root, state)
	fmt.Fprint(stdout, body)
	return 0
}

// schemaValidate validates in-memory bytes against an embedded schema by
// basename. It is a thin wrapper over schema.NewValidator to keep CLI call
// sites terse.
func schemaValidate(root, schemaName string, data []byte) error {
	return schema.NewValidator(root).ValidateBytes(schemaName, data)
}

// reviewerRole reports whether the agent's registered role is one of the S7
// reviewer families (delivery-verifier / qa / e2e-tester role_family).
func reviewerRole(state map[string]any, agentID string) bool {
	entities, _ := state["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		agent, _ := raw.(map[string]any)
		if agent == nil || agent["id"] != agentID {
			continue
		}
		role, _ := agent["role"].(string)
		switch role {
		case "delivery-verifier", "qa", "e2e-tester":
			return true
		}
		return false
	}
	return false
}
