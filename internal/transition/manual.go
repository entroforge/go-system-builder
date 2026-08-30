// Manual generator — renders `loop-harness manual` markdown and the per-
// transition `loop-harness explain <TR-xxx>` text from loop-definition.json +
// the guard spec registry. Output is the agent-facing checklist of what each
// transition checks.
//
// Format is deliberately minimal: one sentence per guard, no What/Fail/
// Recovery structure. The agent reads the list before calling `forward` and
// self-verifies each item. If a guard fails, the harness error names the
// guard ID; the agent re-reads that one line to understand the gap.
package transition

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

// ManualOptions controls markdown rendering.
type ManualOptions struct {
	// TargetPath is where the manual will live in the target project.
	TargetPath string
	// HarnessVersion labels the binary that produced this manual.
	HarnessVersion string
	// LoopDefinitionSHA256 pins the manual to the loop-definition.json it was
	// generated from. doctor compares this against the on-disk definition.
	LoopDefinitionSHA256 string
}

// RenderManual produces the full markdown manual.
func RenderManual(def *LoopDefinition, opts ManualOptions) string {
	if def == nil {
		return ""
	}
	var b strings.Builder
	writeHeader(&b, opts)
	writeControlPlaneRecovery(&b)
	writeS11HumanGateway(&b)
	writeTOC(&b, def)
	writeTopLevelTransitions(&b, def)
	writePhaseTransitions(&b, def)
	writeGlobalTransitions(&b, def)
	writeLegacyPTRBUGSection(&b, def)
	return b.String()
}

func writeS11HumanGateway(b *strings.Builder) {
	fmt.Fprintf(b, "## S11 Human decision gateway\n\n")
	fmt.Fprintf(b, "`awaiting_human_release` is a non-terminal human gateway. The Controller has no automatic candidate or decision at this cursor. Submit exactly one finite disposition with the explicit Runtime command:\n\n")
	fmt.Fprintf(b, "```bash\n")
	fmt.Fprintln(b, "loop-harness runtime human-decision \\")
	fmt.Fprintln(b, "  --disposition <approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort> \\")
	fmt.Fprintln(b, "  --expected-revision <N> --actor <user|orchestrator> \\")
	fmt.Fprintln(b, "  --decision-evidence <human-decision-reference>")
	fmt.Fprintf(b, "```\n\n")
	fmt.Fprintf(b, "Disposition mapping is fixed: `approve` → TR-025 `release_authorized`; `defer` → TR-026 `paused` (the command binds generated `pause_record=generated:pause_checkpoint`); `reject_defect` → TR-027 S8 investigation (also requires `--finding-evidence`); `reject_acceptance` → TR-028 acceptance; `reject_release_audit` → TR-029 release audit; `abort` → TR-030 `aborted`. Arbitrary target states and transition IDs are not accepted.\n\n")
	fmt.Fprintf(b, "Human approval records release authorization only. Harness has no squash merge, publication, deployment, or formal release permission. Runtime rollover is eligible only from `release_authorized` or `aborted`.\n\n")
}

func writeControlPlaneRecovery(b *strings.Builder) {
	fmt.Fprintf(b, "## Controller recovery protocol\n\n")
	fmt.Fprintf(b, "The Hook is an event trigger for the Loop Controller, not only a guard. "+
		"On `SessionStart`, `PreCompact`, `SubagentStart`, `SubagentStop`, and `TeammateIdle`, "+
		"the Controller reads the Runtime, refreshes the resumable Milestone through CAS, "+
		"and emits a positive `LOOP RECOVERY` packet.\n\n")
	fmt.Fprintf(b, "### Bootstrap binding boundary\n\n")
	fmt.Fprintf(b, "`revision` has no global maximum. A Hook/controller checkpoint may advance the inactive bootstrap runtime before `TR-001`; binding uses the current revision as its CAS value, archives the complete inactive state/journal pair, and installs a new `loop-REQ-*` runtime at revision `0` with an empty active journal. The `binding_receipt` carries `event=req_bound`, the approved REQ, and source runtime hashes. Do not edit revision by hand or reuse a pre-bind runtime snapshot after binding; the runtime identity changes and stale identities are rejected.\n\n")
	fmt.Fprintf(b, "1. Read the `Next` action and current `Stage` from the Hook packet, then follow its `Read in order` list.\n")
	fmt.Fprintf(b, "2. Read the linked `docs/agent-protocol.md#sN` section before acting.\n")
	fmt.Fprintf(b, "3. If blocked or the Runtime is unclear, read this Manual. Use `runtime reconcile` only when the Hook reports an integrity/CAS recovery condition; do not call `status`/`next` during normal continuation. When the live Quality Gate checklist is unclear, run `loop-harness ready` (diagnostics; never hand-push a Transition from it). `doctor` is structural schema/manual/policy_ref only — not stage readiness or runtime health; use `loop-harness health` for cumulative runtime signals. For the complete S7→S8→S9→S7→S10 action sequence and compatibility notes, run `loop-harness actions`.\n")
	fmt.Fprintf(b, "4. Execute the one missing deliverable/evidence named by Hook/`ready` `missing[]`; do not invent a parallel lifecycle.\n")
	fmt.Fprintf(b, "5. For `SubagentStop`, complete the report, worktree review, merge-back to the current `develop` integration branch and `completion_ack` checklist before acknowledging the stop. For `TeammateIdle`, re-wake the same teammate. The identical integration chain is available explicitly via `runtime task-integrate --assignment-id <id>` when the automatic SubagentStop payload cannot identify the assignment.\n")
	fmt.Fprintf(b, "6. Builder completion is registered with `runtime task-complete` — one atomic command (message validation + evidence envelope derivation + Agent/TASK advance + evidence registration); the legacy `agent-event completion_reported` + `runtime evidence add` dual write still works but produces a thinner envelope. Before the Builder writes, create its worktree (`git worktree add .worktrees/<assignment-id> -b wt/<assignment-id> develop`) and record `worktree_path`/`branch`/`target_branch` on the manifest row — SubagentStop integration requires them.\n")
	fmt.Fprintf(b, "7. In S7 (verification) the round is driven by runtime verbs, not by hand-pushed transitions: scaffold and register the ReviewPlan (`s7 draft`, inspect `coverage_inventory`/`e2e_assets`, then `runtime review-plan --file <plan.json>`), dispatch reviewers (`s7 manifest-draft`, `runtime register-workgroup`), consume each Assignment's Canonical ReviewResult (`runtime review-result submit --assignment-id <id> --result <result.json>`; required evidence refs are typed, and observation steps land via `capture step --finding <id> --claim <id>` / `--captures`), and revise a running plan once per round (`runtime review-plan revise`). `loop-harness s7 status` is the board: it prints the plan line, the round counter (current / `max_full_review_rounds`), the `round_entry` block (which TR re-entered the round and its seed/handoff/impact refs) plus the `seed_projection` line when a plan is registered from the S9 seed, the `subject_digest` every result must bind, claim dispositions, any blocked assignment's `blocker_ref` and the recovery verb `runtime agent-event --event blocker_resolved --agent-id <id> --message <file>`, and the single next action; a cold-start E2E round also has `loop-harness s7 workspace-digest` for the `verification_artifact_digest` the E2E result must bind. Re-entering S7 via TR-012 (post S9 repair) re-runs the same verbs from a generated seed: `.claude/review/repair/s7-seeds/review-plan-s9-round-<N>.json` is baseline-complete (it carries the changed-artifact `frozen_subjects`, the change-impact source_refs, and the current-generation TASK coverage) but not a finished plan — the Planner still refines Claims, Assignments and `non_overlap_boundary` per `docs/agent-protocol.md` §s7s9-control-plane-map; refresh the frozen shas if the tree moved, refine the Claim set if needed, then `runtime review-plan --file <seed>`. the change-impact evidence is the source of truth: the registered plan must derive `frozen_subjects`, `coverage_inventory`, and a Claim source_ref from every `changed_artifacts` path/SHA it carries; QA reports additionally carry the §5 Targeted Re-verification table alongside §2–§4, and the round counter tells you which round you are on. A no-repair batch returns via TR-022 (`findings_resolved_without_repair`) instead of TR-012 and runs the same S7 verbs without a seed. A rejected command includes the missing facts, repair action, next command, verification command and protocol ref; fix those facts and resubmit the same artifact. The machine exits are automatic — a sealed ObservationBatch or a machine CleanRound commits TR-008/TR-009 on the next PreToolUse; do not invoke the transition CLI for them. If the PostToolUse auto-activation chain did not fire for a dispatched Worker, recover with `runtime agent-begin --agent-id <id> --plan <plan-report.json>`. Full verb walkthrough: `docs/agent-protocol.md#s7`.\n")
	fmt.Fprintf(b, "Before registering an S7 draft, inspect its CASE-level E2E Assignments. `s7 draft` projects required browser CASEs from `docs/design/prototypes/<module>/cases.json`; a complete CASE→Playwright spec mapping produces `regression_available` and SHA-pinned `e2e_assets`, while any missing mapping produces `cold_start`, an `e2e-workspace/<round>` write surface, and one behavior Assignment per CASE. If no readable CASE inventory exists, the remaining `TODO(planner)` is intentional and registration explains the missing S2 input. Typed path evidence may use `path:<repo-relative>#sha256=<64-hex>` for drift detection; bare `path:` is compatibility-only existence evidence.\n")
	fmt.Fprintf(b, "8. In S9 (bug_resolution), consume only the approved RepairContract: open the RepairSession, compile the RepairPlan, dispatch each Assignment with `runtime repair dispatch --assignment-id <assignment> --agent-id <agent>`, send the generic PLAN_REPORT and submit the S9 domain PlanReport, then wait for `runtime repair execution begin` before product writes. Use `runtime repair status` for each Assignment's owner/report/result, `queue_reason`, `lock_state` and next action; do not create a second scheduler or edit Runtime state by hand.\n")
	fmt.Fprintf(b, "9. If `s7 status` reports `round N of M` with N >= M, finish draining the current round but do not open another one. Submit the human artifact through `runtime s7-budget-decision --file <decision.json> --expected-revision <N> --actor <user>`; `increase_budget` atomically raises `max_full_review_rounds` and leaves the pending round-opening transition to retry, while `return_to_governance` records the decision, invalidates downstream review evidence, resets the review projection, and routes through GTR-006 to planning. The decision file is persisted as scoped `human_decision` evidence, and CAS rejects stale revisions, mismatched runtime/round values, and non-increasing limits.\n")
	fmt.Fprintf(b, "10. In S10 (acceptance_and_audit), treat the stage as a read-only audit, not a final shortcut: run `s10 status`, freeze the finite coverage inventory and responsibility matrix, record one counterevidence check per item, and validate the machine manifest before registering the human-readable ACC or release-audit envelope. The manifest must prove 100%% requirement/contract/changed-path/audit-area coverage with zero UNKNOWN, unsupported PASS, unowned risk, untracked debt, or blocking finding. If any product or architecture defect appears, return through S8 → S9 → a fresh complete S7; never use S9 → S10 or edit product code in S10. Only a current clean package may reach S11.\n")
	fmt.Fprintf(b, "11. Some authority transactions carry runtime-issued ids that are not TRs and therefore do not appear in the Contents index above: `S8-REPAIR-CONTRACT-APPROVAL` (runtime investigation contract approve — the S8→S9 authority; PTR-BUG-08 is its legacy-catalog alias), plus the entity/record CAS ids (REVIEW-RESULT, REVIEW-PLAN-STALE, S7-BUDGET-DECISION, AGENT-LIFECYCLE, BUG-LIFECYCLE, EVIDENCE-RECORD). They are driven by their runtime verbs, never by `runtime transition`.\n")
	fmt.Fprintf(b, "12. Stop only at a human Gateway, an external asynchronous wait, or the end of the current turn.\n\n")
	fmt.Fprintf(b, "The persisted `.claude/loop-state.json` `milestone` is a recovery cache, not a second state machine. "+
		"`docs/loop-definition.json` and the Transition Engine remain the authority for legal lifecycle changes.\n\n")
	fmt.Fprintf(b, "During BUG investigation, answer why E2E did not cover or fail the gap "+
		"(`skills/bug-resolution/SKILL.md`; `loop-harness e2e-coverage`). A contracted behavior that broke without a red CT/AC requires a coverage-gap Closing Contract item.\n\n")
	fmt.Fprintf(b, "---\n\n")
}

// RenderTransition renders a single transition for `explain`. Returns empty
// string if no transition matches.
func RenderTransition(def *LoopDefinition, id string) string {
	if def == nil {
		return ""
	}
	spec, ok := findTransitionSpec(def, id)
	if !ok {
		return ""
	}
	var b strings.Builder
	writeTransition(&b, spec)
	return b.String()
}

// internal helpers -------------------------------------------------------

// legacyPTRBUG marks the deprecated legacy-compatibility transitions (RC-11
// C-8). They stay in the loop definition for journal-replay compatibility,
// but the Manual collapses them into a Legacy (PTR-BUG) section at the end so
// the main Contents index only surfaces the paths an agent should consider.
var legacyPTRBUG = map[string]bool{
	"PTR-BUG-01": true,
	"PTR-BUG-02": true,
	"PTR-BUG-03": true,
	"PTR-BUG-04": true,
	"PTR-BUG-08": true,
}

func writeHeader(b *strings.Builder, opts ManualOptions) {
	target := opts.TargetPath
	if target == "" {
		target = ".claude/bin/loop-harness.md"
	}
	fmt.Fprintf(b, "# loop-harness — Transition Checklist\n\n")
	fmt.Fprintf(b, "> What `loop-harness` checks at every transition. Read the\n")
	fmt.Fprintf(b, "> relevant section before calling `forward`; verify each bullet\n")
	fmt.Fprintf(b, "> before requesting the harness to advance.\n\n")
	fmt.Fprintf(b, "- **Path**: `%s`\n", target)
	if opts.HarnessVersion != "" {
		fmt.Fprintf(b, "- **Harness version**: %s\n", opts.HarnessVersion)
	}
	if opts.LoopDefinitionSHA256 != "" {
		fmt.Fprintf(b, "- **Loop definition SHA-256**: `%s`\n", opts.LoopDefinitionSHA256)
	}
	fmt.Fprintf(b, "\n---\n\n")
}

func writeTOC(b *strings.Builder, def *LoopDefinition) {
	fmt.Fprintf(b, "## Contents\n\n")
	for _, t := range def.Transitions {
		fmt.Fprintf(b, "- [`%s`](#%s) %s → %s — %s\n", t.ID, anchor(t.ID), t.From, t.To, oneLineDescription(t.Description))
	}
	phaseOwners := make([]string, 0, len(def.PhaseMachines))
	for owner := range def.PhaseMachines {
		phaseOwners = append(phaseOwners, owner)
	}
	sort.Strings(phaseOwners)
	for _, owner := range phaseOwners {
		machine := def.PhaseMachines[owner]
		if len(machine.Transitions) == 0 {
			continue
		}
		fmt.Fprintf(b, "\n_Phase: %s_\n\n", owner)
		for _, t := range machine.Transitions {
			if legacyPTRBUG[t.ID] {
				// C-8: legacy compatibility transitions are collapsed into
				// the Legacy (PTR-BUG) section at the end of the Manual.
				continue
			}
			fmt.Fprintf(b, "- [`%s`](#%s) %s → %s — %s\n", t.ID, anchor(t.ID), t.From, t.To, oneLineDescription(t.Description))
		}
		if owner == "bug_resolution" {
			fmt.Fprintf(b, "\nSee [Legacy (PTR-BUG)](#legacy-ptr-bug) at the end of this Manual for the legacy compatibility transitions (PTR-BUG-01..04, PTR-BUG-08).\n")
		}
	}
	if len(def.GlobalTransitions) > 0 {
		fmt.Fprintf(b, "\n_Global_\n\n")
		for _, t := range def.GlobalTransitions {
			fmt.Fprintf(b, "- [`%s`](#%s) → %s — %s\n", t.ID, anchor(t.ID), t.To, oneLineDescription(t.Description))
		}
	}
	fmt.Fprintf(b, "\n---\n\n")
}

func writeTopLevelTransitions(b *strings.Builder, def *LoopDefinition) {
	fmt.Fprintf(b, "## Transitions\n\n")
	for _, t := range def.Transitions {
		writeTransition(b, t)
	}
}

func writePhaseTransitions(b *strings.Builder, def *LoopDefinition) {
	phaseOwners := make([]string, 0, len(def.PhaseMachines))
	for owner := range def.PhaseMachines {
		phaseOwners = append(phaseOwners, owner)
	}
	sort.Strings(phaseOwners)
	for _, owner := range phaseOwners {
		machine := def.PhaseMachines[owner]
		if len(machine.Transitions) == 0 {
			continue
		}
		fmt.Fprintf(b, "## Phase transitions: %s\n\n", owner)
		for _, t := range machine.Transitions {
			if legacyPTRBUG[t.ID] {
				// C-8: rendered in writeLegacyPTRBUGSection instead.
				continue
			}
			writeTransition(b, t)
		}
	}
}

// writeLegacyPTRBUGSection appends the collapsed Legacy (PTR-BUG) section
// (RC-11 C-8) after the global transitions. It re-renders the deprecated
// legacy-compatibility transitions verbatim so anchors keep resolving, but
// keeps them out of the main Contents index and phase-transition sections.
func writeLegacyPTRBUGSection(b *strings.Builder, def *LoopDefinition) {
	fmt.Fprintf(b, "\n## Legacy (PTR-BUG)\n\n")
	fmt.Fprintf(b, "_These four transitions are legacy compatibility paths only. They exist so\n")
	fmt.Fprintf(b, "pre-S9 journals and synthetic fixtures still replay; new code must use the\n")
	fmt.Fprintf(b, "S8 InvestigationCase / S9 repair machinery above instead. See also the\n")
	fmt.Fprintf(b, "authority-transaction note in the Contents preamble (S8-REPAIR-CONTRACT-APPROVAL\n")
	fmt.Fprintf(b, "is the real S8→S9 authority; PTR-BUG-08 is its legacy-catalog alias)._ \n\n")
	wrote := false
	for _, owner := range sortedPhaseOwners(def) {
		for _, t := range def.PhaseMachines[owner].Transitions {
			if legacyPTRBUG[t.ID] {
				writeTransition(b, t)
				wrote = true
			}
		}
	}
	if !wrote {
		fmt.Fprintf(b, "_No legacy transitions declared._\n\n")
	}
}

func sortedPhaseOwners(def *LoopDefinition) []string {
	owners := make([]string, 0, len(def.PhaseMachines))
	for owner := range def.PhaseMachines {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	return owners
}

func writeGlobalTransitions(b *strings.Builder, def *LoopDefinition) {
	if len(def.GlobalTransitions) == 0 {
		return
	}
	fmt.Fprintf(b, "## Global transitions\n\n")
	for _, t := range def.GlobalTransitions {
		from := strings.Join(t.FromStates, "|")
		spec := TransitionSpec{
			ID:               t.ID,
			From:             from,
			Event:            t.Event,
			To:               t.To,
			Actors:           t.Actors,
			Guards:           t.Guards,
			RequiredEvidence: t.RequiredEvidence,
			Description:      t.Description,
		}
		writeTransition(b, spec)
	}
}

func writeTransition(b *strings.Builder, t TransitionSpec) {
	fmt.Fprintf(b, "### `%s` {#%s}\n\n", t.ID, anchor(t.ID))
	fmt.Fprintf(b, "_%s → %s_\n\n", t.From, t.To)
	fmt.Fprintf(b, "%s\n\n", t.Description)
	if len(t.Guards) == 0 {
		fmt.Fprintf(b, "_No guards._\n\n")
	} else {
		for _, g := range t.Guards {
			writeGuard(b, g)
		}
		fmt.Fprintf(b, "\n")
	}
	if len(t.RequiredEvidence) > 0 {
		fmt.Fprintf(b, "Evidence: %s\n\n", joinBack(t.RequiredEvidence))
		writeEvidenceBindings(b, t)
		writeS10ArtifactGuidance(b, t.ID)
	}
}

func writeEvidenceBindings(b *strings.Builder, t TransitionSpec) {
	catalog := evidence.DefaultCatalog()
	fmt.Fprintf(b, "Evidence bindings (copy into `runtime transition`):\n\n")
	for _, slot := range t.RequiredEvidence {
		if generator, generated := catalog.Generator(slot); generated {
			fmt.Fprintf(b, "- `%s`: `--evidence %s=%s` (%s)\n", slot, slot, generator.Reference, generator.Description)
		} else {
			fmt.Fprintf(b, "- `%s`: `--evidence %s=<reference>`\n", slot, slot)
		}
		fmt.Fprintf(b, "  Accepted kinds: %s\n", joinBack(catalog.RegisteredAcceptedKinds(slot)))
	}
	fmt.Fprintf(b, "\nIf a binding is missing, retry with the command above; run `loop-harness explain %s` to inspect current candidates.\n\n", t.ID)
}

func writeS10ArtifactGuidance(b *strings.Builder, transitionID string) {
	switch transitionID {
	case "TR-009", "TR-015":
		fmt.Fprintf(b, "S10 acceptance machine artifact (required before the Controller can consume the acceptance evidence):\n\n")
		fmt.Fprintf(b, "```text\n")
		fmt.Fprintf(b, "loop-harness s10 manifest validate --root <root> \\\n")
		fmt.Fprintf(b, "  --file <acceptance-manifest.json> --type acceptance\n")
		fmt.Fprintf(b, "loop-harness runtime evidence add --root <root> \\\n")
		fmt.Fprintf(b, "  --expected-revision <N> --id <id> --kind acceptance \\\n")
		fmt.Fprintf(b, "  --path <envelope.json> --produced-by <agent> --responsibility <role>\n")
		fmt.Fprintf(b, "```\n\n")
		fmt.Fprintf(b, "The envelope must contain `audit_manifest_path` and `audit_manifest_sha256`. The manifest must have a frozen `coverage_inventory` with explicit `requirement`, `contract`, and `changed_path` rows (use evidence-backed `not_applicable`, never an omitted hard category), exactly one counterevidence row per item, and zero UNKNOWN/unsupported PASS/unowned risk/untracked debt/blocking finding metrics. Start from the copyable shape `docs/examples/s10/acceptance-manifest.json` when useful. Run `loop-harness s10 status --root <root>` to see the current round and the next recovery action. Do not call `runtime transition` manually.\n\n")
	case "TR-017":
		fmt.Fprintf(b, "The release-audit evidence must point to a separately validated manifest:\n\n")
		fmt.Fprintf(b, "```text\n")
		fmt.Fprintf(b, "loop-harness s10 manifest validate --root <root> \\\n")
		fmt.Fprintf(b, "  --file <release-audit-manifest.json> --type release_audit\n")
		fmt.Fprintf(b, "loop-harness runtime evidence add --root <root> \\\n")
		fmt.Fprintf(b, "  --expected-revision <N> --id <id> --kind release_audit \\\n")
		fmt.Fprintf(b, "  --path <envelope.json> --produced-by <agent> --responsibility \"Release Auditor\"\n")
		fmt.Fprintf(b, "```\n\n")
		fmt.Fprintf(b, "The manifest must include all eight audit areas from `internal/schema/assets/s10-audit-manifest.schema.json`; use `docs/examples/s10/release-audit-manifest.json` as the copyable shape. Markdown audit prose does not replace this machine-checked ledger.\n\n")
	case "TR-018":
		fmt.Fprintf(b, "The BLOCKED release-audit evidence must preserve its machine-readable blocker ledger:\n\n")
		fmt.Fprintf(b, "```text\n")
		fmt.Fprintf(b, "loop-harness s10 manifest validate --root <root> \\\n")
		fmt.Fprintf(b, "  --file <release-audit-manifest.json> --type release_audit --outcome blocked\n")
		fmt.Fprintf(b, "loop-harness runtime evidence add --root <root> \\\n")
		fmt.Fprintf(b, "  --expected-revision <N> --id <id> --kind release_audit \\\n")
		fmt.Fprintf(b, "  --path <envelope.json> --produced-by <agent> --responsibility \"Release Auditor\"\n")
		fmt.Fprintf(b, "```\n\n")
		fmt.Fprintf(b, "Keep the blocking finding, route, evidence references, and all eight audit areas in the manifest. Let the Controller take TR-018 to `paused`; do not call `runtime transition` manually.\n\n")
	}
}

// ValidateManualEvidenceBindings verifies that a generated Manual contains an
// actionable binding template for every required evidence slot.
func ValidateManualEvidenceBindings(def *LoopDefinition, markdown string) error {
	if def == nil {
		return nil
	}
	for _, spec := range allTransitionSpecs(def) {
		for _, slot := range spec.RequiredEvidence {
			prefix := "--evidence " + slot + "="
			if !strings.Contains(markdown, prefix) {
				return fmt.Errorf("manual missing evidence binding guidance for transition %s slot %s; run `loop-harness manual --root .` to regenerate; expected %s<reference>", spec.ID, slot, prefix)
			}
		}
	}
	return nil
}

func allTransitionSpecs(def *LoopDefinition) []TransitionSpec {
	var specs []TransitionSpec
	specs = append(specs, def.Transitions...)
	owners := make([]string, 0, len(def.PhaseMachines))
	for owner := range def.PhaseMachines {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		specs = append(specs, def.PhaseMachines[owner].Transitions...)
	}
	for _, global := range def.GlobalTransitions {
		specs = append(specs, TransitionSpec{
			ID:               global.ID,
			RequiredEvidence: global.RequiredEvidence,
		})
	}
	for _, lifecycle := range def.EntityLifecycles {
		specs = append(specs, lifecycle.Transitions...)
	}
	return specs
}

func writeGuard(b *strings.Builder, guardID string) {
	registration, registered := LookupGuardRegistration(guardID)
	enforcement := "unregistered"
	if registered {
		enforcement = string(registration.Enforcement)
	}
	spec, ok := LookupGuardSpec(guardID)
	if !ok {
		fmt.Fprintf(b, "- `%s` [%s] — _no spec_\n", guardID, enforcement)
		return
	}
	fmt.Fprintf(b, "- `%s` [%s] — %s\n", guardID, enforcement, spec.Check)
}

// findTransitionSpec returns the transition spec by ID across all scopes.
func findTransitionSpec(def *LoopDefinition, id string) (TransitionSpec, bool) {
	for _, t := range def.Transitions {
		if t.ID == id {
			return t, true
		}
	}
	for _, machine := range def.PhaseMachines {
		for _, t := range machine.Transitions {
			if t.ID == id {
				return t, true
			}
		}
	}
	for _, g := range def.GlobalTransitions {
		if g.ID == id {
			return TransitionSpec{
				ID:               g.ID,
				From:             strings.Join(g.FromStates, "|"),
				Event:            g.Event,
				To:               g.To,
				Actors:           g.Actors,
				Guards:           g.Guards,
				RequiredEvidence: g.RequiredEvidence,
				Description:      g.Description,
			}, true
		}
	}
	return TransitionSpec{}, false
}

func anchor(id string) string { return strings.ToLower(id) }

func joinBack(values []string) string {
	if len(values) == 0 {
		return "_(none)_"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = "`" + v + "`"
	}
	return strings.Join(parts, ", ")
}

func oneLineDescription(s string) string {
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		return s[:idx+1]
	}
	return s
}

// ManualFilename returns the conventional on-disk filename for the manual.
func ManualFilename() string { return "loop-harness.md" }

// ManualTargetPath returns the conventional target-project path.
// Both this and the repo-root placement (ManualFilename) are valid Manual
// locations; semantic.ValidateManualAgreement checks them in that order
// (RC-11 F-5: guidance names the root copy primary, this one is the fallback).
func ManualTargetPath() string {
	return filepath.ToSlash(filepath.Join(".claude", "bin", ManualFilename()))
}
