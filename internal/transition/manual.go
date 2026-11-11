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
}

// RenderManual produces the full markdown manual.
func RenderManual(def *LoopDefinition, opts ManualOptions) string {
	if def == nil {
		return ""
	}
	var b strings.Builder
	writeHeader(&b, opts)
	writeControlPlaneRecovery(&b)
	writeRequirementAmendment(&b)
	writeS11HumanGateway(&b)
	writeTOC(&b, def)
	writeTopLevelTransitions(&b, def)
	writePhaseTransitions(&b, def)
	writeGlobalTransitions(&b, def)
	return b.String()
}

// writeRequirementAmendment is the single normal-path amendment guide shared
// by the generated manual and the requirement-funnel link.
func writeRequirementAmendment(b *strings.Builder) {
	fmt.Fprint(b, `## Approved requirement amendment {#approved-requirement-amendment}

Reuse the actual conversation decision approving the specific correction; do not ask for the same approval again or require the human to type a command verbatim. Record the actual decision attribution with --approved-by, not Git identity. An undecided amendment still waits for the human decision.

If the bound Runtime is not already paused, record its checkpoint with the existing command (skip this step when already paused):

`+"```sh"+`
loop-harness runtime pause --root <project-root> --approved-by <actual-human> --reason <approved-correction>
`+"```"+`

Apply only the approved correction to the same REQ ID, keep its top status locked, raise its semantic version above the bound version, and commit the amended REQ on the bound development branch (for example develop). Then use the existing domain receiver:

`+"```sh"+`
loop-harness req amend --root <project-root> --req docs/requirements/REQ-<id>.md --approved-by <actual-human>
`+"```"+`

Read the exact current S2 questionnaire view/answer paths returned by the original amendment outlet and continue its professional work. The original transaction records the decision, new generation and downstream invalidation; do not assemble evidence bindings, hand-run TR-020, resume the old checkpoint after amendment, or guess a generation directory. If the transaction is recorded but questionnaire preparation fails, retain that fact and follow its actual preparation error and original listener continuation; do not resubmit an already accepted amendment or invent a path.

`)
}

func writeS11HumanGateway(b *strings.Builder) {
	fmt.Fprintf(b, "## S11 Human decision gateway\n\n")
	fmt.Fprintf(b, "`awaiting_human_release` is a non-terminal human gateway. The Controller has no automatic candidate or decision at this cursor. Present the actual materials; after a real explicit human choice, Main fills disposition and optional reason in the exact current application and saves normally. Empty disposition remains waiting. The explicit Runtime command below is compatibility/recovery only:\n\n")
	fmt.Fprintf(b, "```bash\n")
	fmt.Fprintln(b, "loop-harness runtime human-decision \\")
	fmt.Fprintln(b, "  --disposition <approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort> \\")
	fmt.Fprintln(b, "  --actor <user|orchestrator> \\")
	fmt.Fprintln(b, "  --decision-evidence <human-decision-reference>")
	fmt.Fprintf(b, "```\n\n")
	fmt.Fprintf(b, "Disposition mapping is fixed: `approve` → TR-025 `release_authorized`; `defer` → TR-026 `paused` (the command binds generated `pause_record=generated:pause_checkpoint`); `reject_defect` → TR-027 S8 investigation (also requires `--finding-evidence`); `reject_acceptance` → TR-028 acceptance; `reject_release_audit` → TR-029 release audit; `abort` → TR-030 `aborted`. Arbitrary target states and transition IDs are not accepted.\n\n")
	fmt.Fprintf(b, "Human approval records release authorization only. Harness has no squash merge, publication, deployment, or formal release permission. Runtime rollover is eligible only from `release_authorized` or `aborted`.\n\n")
}

func writeControlPlaneRecovery(b *strings.Builder) {
	fmt.Fprintf(b, "## Controller recovery protocol\n\n")
	fmt.Fprintf(b, "Hooks observe current responsibilities and connect natural delivery boundaries. "+
		"Native Start gives bounded context; native Stop invokes the original professional consumer. S7/S8/S9 PreToolUse listens for ready original answers, with successful PostToolUse and Stop/SubagentStop fallback. "+
		"Main saves its own questionnaires through their existing consumers. S5/S6 retain their original ordinary-tool reception contract. Ordinary tools do not "+
		"activate work or run every stage gate. Milestone is an existing recovery cache, not proof that each event committed or context was delivered.\n\n")
	fmt.Fprintf(b, "### Bootstrap binding boundary\n\n")
	fmt.Fprintf(b, "S6 owners can query `runtime task-receipt --agent-id <owner> --assignment-id <id>`; registered Workers use the exact builder_receipt_command returned by prepare. The read-only receipt distinguishes current qualified delivery, own pending checks and Main integration; checks_source=original_report does not claim new check execution. Lost registered evidence and changed TASK input go to Main recovery/rework. The receipt neither advances TR-006 nor replaces S7 or S9 RepairResult completion.\n\n")
	fmt.Fprintf(b, "`revision` has no global maximum. A Hook/controller checkpoint may advance the inactive bootstrap runtime before `TR-001`; binding lets the Writer read and commit the current Runtime under lock, archives the complete inactive state/journal pair, and installs a new `loop-REQ-*` runtime at revision `0` with an empty active journal. The `binding_receipt` carries `event=req_bound`, the approved REQ and the original source runtime identity. Do not edit revision by hand or reuse a pre-bind runtime snapshot after binding; the runtime identity changes and stale identities are rejected.\n\n")
	fmt.Fprintf(b, "1. Read the `Next` action and current `Stage` from the Hook packet, then follow its `Read in order` list.\n")
	fmt.Fprintf(b, "2. Read the linked `docs/control/agent-protocol.md#sN` section before acting.\n")
	fmt.Fprintf(b, "3. If blocked or the Runtime is unclear, read this Manual. Use `runtime reconcile` only for a provable journal/state integrity gap, not an ordinary CAS conflict; status is optional diagnostics, never a per-tool prerequisite. When the live Quality Gate checklist is unclear, run `loop-harness ready` (diagnostics; never hand-push a Transition from it). `doctor` is structural schema/manual/policy_ref only — not stage readiness or runtime health; use `loop-harness health` for cumulative runtime signals. For the S7→S8→S9→S7→S10 action sequence, run `loop-harness actions`.\n")
	fmt.Fprintf(b, "4. Execute the one missing deliverable/evidence named by Hook/`ready` `missing[]`; do not invent a parallel lifecycle.\n")
	fmt.Fprintf(b, "5. Main reads the native return and original receipt. S6 uses the supplied `runtime task-integrate --assignment-id <id> --root <main-root>`; S9 Main integrates actual contributions and runs necessary checks, then saves its original Impact judgment. No particular integration command, Git commit, Final or proof qualifies S9 answer or Impact reception. Do not re-wake a completed Worker for Main integration. No develop fallback or automatic Main checkout.\n")
	fmt.Fprintf(b, "6. In new native S6, Main runs `s6 prepare --root <authority-root> --task <TASK> --role <Builder-role>` and passes its exact brief into Claude Code native Agent with worktree isolation (`worktree.baseRef: head`). No preselected platform IDs, Team, workspace prepare/launch, generic begin or activation. Builder saves its professional plan before implementation, continues with real TASK self-checks, commits products, saves Result and returns its exact brief and absolute Result reference. SubagentStart supplies short engineering context; SubagentStop invokes the original Result consumer. Ordinary save hooks do not receive native S6 delivery. Main explicitly runs `runtime task-integrate --assignment-id <id>` for the actual merge, required Main checks, ACK and cleanup. Rework uses `s6 prepare --revise-assignment <id> --revision-intent-key <stable-key>` through the original revision Writer. The exact TR-003 batch and TR-006 gate remain. Never launch the same responsibility twice. See `docs/examples/s6-builder/README.md` and `docs/control/agent-protocol.md#s6`.\n")
	fmt.Fprintf(b, "7. In S7, Main fills and saves the exact supplied original ReviewPlan and actually dispatches each registered responsibility with its exact view/answer. Each Reviewer saves its own original professional answer once. PreToolUse observes it; successful PostToolUse and Stop/SubagentStop provide fallback through the same consumer. The consumer checks parsing, necessary fields, readiness and the direct conclusion once and saves once. No returned reference, re-save, done flag, Git/evidence/hash proof or parallel coverage calculation qualifies reception. Necessary responsibilities complete without a Finding continue TR-009 and prepare S10; any accepted Finding hands received observations and unfinished work to S8 through TR-008, without a new CleanRound/ObservationBatch proof or another Main Pre. Already dispatched S7 responsibilities remain receivable after S8 begins; late Findings go to Main without automatically changing approval. Preserve accepted facts and follow the named original continuation for unfinished preparation. Main owns professional coverage; every Reviewer owns its assigned actual checks.\n\n")
	fmt.Fprintf(b, "The supplied ReviewPlan includes its `coverage_inventory` and `e2e_assets`; inspect their current content and answer the professional questions without recreating machine fields. Record actual observations and cite useful sources in the original answer. Follow the specific feedback for unfinished work and recovery.\n")
	fmt.Fprintf(b, "S7 preparation supplies current scenario material and exact original plan questions. Main chooses actual browser/API/command/state coverage, reusable assets and any needed isolated verification workspace; script matches and field counts do not prove coverage. Reviewers cite actual observations and readable source locations without recapture or hash admission requirements. S8 Main reads accepted Findings or the actual human report, registers actual questions and dispatches Investigators. Each Investigator saves its original result for Pre/successful Post/Stop reception. new_hypotheses is only a suggestion; Main judges cause, unknowns and route, and actual approval remains with the real approver and accurate Contract. Unsupported new-question source preparation stays a named Main/system gap, without invented aliases or protocols.\n\n")
	fmt.Fprintf(b, "8. Main follows the approved RepairContract and records actual professional division. Carry each original prepared brief into native Agent with worktree isolation. Current native S9 responsibility selection accepts its registered absolute control-root path, complete control-root-relative path, or ./ plus that relative path; this does not locate materials under Worker cwd. Main supplies readable absolute control-root material paths and carries actual task restrictions into every delegated task. No basename, arbitrary ../, other root or role guessing is allowed; original responsibility selection, actual integration/checks, other stages and frozen paths remain. Reception does not revalidate Git, evidence or historical hashes. Private outputs stay in the actual worktree. Each Builder saves its private PlanReport before implementation, continues repair/self-checks, then saves its private Result at the exact original path in the actual worktree. Applicable PreToolUse listens for ready original Plan/Result answers, with successful PostToolUse and Stop/SubagentStop fallback through the original consumers; there is no separate plan acceptance wait. Ordinary tools remain available. Report actual results and unfinished work to Main; reception requires no extra submission, re-save or returned reference. An accepted contribution can prepare the original ChangeImpact question. Main owns actual integration, necessary checks and professional Impact judgment and arranges independent Targeted verification per the approved Contract. Impact and Targeted preparation can occur earlier; the framework does not require proof of prior Main completion. Impact reception does not certify that actual integration or checks occurred and requires no new Final, commit or preservation proof. Empty changes and empty dispositions are legitimate even when indistinguishable from the default template; they may be received early through the original natural events. Reception does not prove actual integration, checks, all Builders completed or independent verification. Main may naturally update the same original Impact as further contributions arrive; no new completion signal, re-save or Final prerequisite is required. Optional explanations do not become admission fields. The verifier saves its exact original Targeted answer for applicable PreToolUse reception, with successful PostToolUse and Stop/SubagentStop fallback through its original independent consumer, without a Builder PlanReport or product writes. A final message or returned reference is not a reception prerequisite. First incomplete native returns give Main the actual unfinished work and exact gap without claiming completion. An absent judgment normally stays draft; the original empty Impact exception remains receivable as stated above. A comprehensible blocked or unfinished conclusion is received as stated and handed to Main. After accepted passing conclusions and the applicable responsibilities, the original outlet attempts Handoff and prepares the fresh S7 seed; actual work remains the respective professional responsibility, with no completion proof, extra Handoff report, next-Hook wait or duplicate delivery. Targeted reception requires only a comprehensible overall conclusion and necessary association; missing nonessential observation or evidence fields do not reject it. A fail without classification remains an unknown cause, never an inferred same cause. If Targeted was accepted but Handoff or S7 preparation failed, preserve the accepted result and follow the specific system continuation supplied by the actual feedback; do not repeat the independent work or add Final/Changeset proof. The completed outlet supplies the original S7 ReviewPlan questionnaire and guide; Main completes and saves that questionnaire. Missing paths or preparation belong to Main/system. See `docs/control/agent-protocol.md#s9`.\n")
	fmt.Fprint(b, "For S7/S8/S9, an accepted blocked or unfinished handoff does not end the original responsibility. Its author can naturally continue actual unfinished work without another Main approval, submit or resume proof. Receive a new comprehensible conclusion as a new handoff while retaining prior accepted facts. Do not replay unchanged content or treat arbitrary private-file changes as a new responsibility. Accepted completed work and unfinished successor preparation remain separate; continue only the named missing part without re-saving an old answer, re-reading evidence or rechecking Git/hash history.\n\n")
	fmt.Fprintf(b, "9. Continue the original S7/S8/S9 responsibility and professional findings without a default iteration budget or count-based human gate. Follow actual user constraints and existing real decisions through their original consumers; no review-count budget or decision application is required.\n")
	fmt.Fprintf(b, "10. Main fills the actual acceptance or release-audit Manifest at the supplied exact path and saves once. Explain requirement dispositions, applicable observations, counterevidence, unknowns and actual risks. Eight audit themes are relevance prompts, not a row quota or mandatory new work. The original consumer derives its envelope and metrics; do not run a second validate/render/evidence submission. Unknowns and blockers remain unfinished, while explicit outcomes may be recorded without authorizing advance. S10 does not repair product code; changed products return through investigation/repair and fresh complete S7. Actual human choice remains in S11.\n")
	fmt.Fprintf(b, "11. Some authority transactions carry runtime-issued ids that are not TRs and therefore do not appear in the Contents index above: `S8-REPAIR-CONTRACT-APPROVAL` (runtime investigation contract approve — the S8→S9 authority), plus the entity/record CAS ids (REVIEW-RESULT, AGENT-LIFECYCLE, BUG-LIFECYCLE, EVIDENCE-RECORD). They are driven by their runtime verbs, never by `runtime transition`.\n")
	fmt.Fprintf(b, "12. A native Worker returns its actual artifacts or honest blocker and ends its own execution. Main continues its remaining duties; an unknown human decision or external wait is explicit, not a reason to invent more Worker work.\n\n")
	fmt.Fprintf(b, "The persisted `.claude/loop-state.json` `milestone` is a recovery cache, not a second state machine. "+
		"`docs/control/loop-definition.json` and the Transition Engine remain the authority for legal lifecycle changes.\n\n")
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
			fmt.Fprintf(b, "- [`%s`](#%s) %s → %s — %s\n", t.ID, anchor(t.ID), t.From, t.To, oneLineDescription(t.Description))
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
			writeTransition(b, t)
		}
	}
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
	if t.ID == "TR-020" {
		fmt.Fprint(b, "Low-level diagnostic evidence slots only; not the normal amendment entry. Follow [Approved requirement amendment](#approved-requirement-amendment); the original receiver binds these facts, not the author.\n\n")
	} else if t.ID == "TR-009" {
		fmt.Fprint(b, "These legacy diagnostic slots do not define new answer proof. The original outlet uses necessary responsibility completion and accepted no-Finding conclusions to invoke TR-009 and prepare S10; authors do not copy old proof bindings.\n\n")
	} else if t.ID == "TR-008" {
		fmt.Fprint(b, "These legacy diagnostic slots do not define new answer proof. An accepted Finding hands the received observations and unfinished work to S8 through the original outlet; no Batch proof, next Main Pre or repeated Result is required.\n\n")
	} else {
		fmt.Fprintf(b, "Evidence bindings (copy into `runtime transition`):\n\n")
	}
	for _, slot := range t.RequiredEvidence {
		if generator, generated := catalog.Generator(slot); generated {
			fmt.Fprintf(b, "- `%s`: `--evidence %s=%s` (%s)\n", slot, slot, generator.Reference, generator.Description)
		} else {
			fmt.Fprintf(b, "- `%s`: `--evidence %s=<reference>`\n", slot, slot)
		}
		fmt.Fprintf(b, "  Accepted kinds: %s\n", joinBack(catalog.RegisteredAcceptedKinds(slot)))
	}
	if t.ID == "TR-020" {
		fmt.Fprint(b, "\nThese slots describe the original transaction for diagnosis; use the normal amendment entry above rather than composing or retrying a transition.\n\n")
	} else if t.ID == "TR-009" {
		fmt.Fprint(b, "\nReception, transition and material preparation are separate facts. After resolving a named original Gate failure, resume with `loop-harness runtime review-continue --root <root>`; after entering S10, restore missing current materials with `loop-harness runtime audit-prepare --root <root>`. Existing reports and accepted Reviewer answers are preserved; material recovery does not repeat TR-009.\n\n")
	} else if t.ID == "TR-008" {
		fmt.Fprint(b, "\nIf the handoff is incomplete, Main inspects the received original Findings, unfinished responsibilities and `loop-harness explain TR-008` feedback. Preserve accepted Reviewer answers. After the transition, fill and save the supplied S8 intake; its original consumer creates the Case.\n\n")
	} else {
		fmt.Fprintf(b, "\nIf a binding is missing, retry with the command above; run `loop-harness explain %s` to inspect current candidates.\n\n", t.ID)
	}
}

func writeS10ArtifactGuidance(b *strings.Builder, transitionID string) {
	switch transitionID {
	case "TR-009", "TR-015":
		fmt.Fprintf(b, "For normal S10, save the supplied professional Manifest; its original consumer records the artifact. Explicit unpacketized/recovery commands only:\n\n")
		fmt.Fprintf(b, "```text\n")
		fmt.Fprintf(b, "loop-harness s10 manifest validate --root <root> \\\n")
		fmt.Fprintf(b, "  --file <acceptance-manifest.json> --type acceptance\n")
		fmt.Fprintf(b, "loop-harness runtime evidence add --root <root> \\\n")
		fmt.Fprintf(b, "  --id <id> --kind acceptance \\\n")
		fmt.Fprintf(b, "  --path <envelope.json> --produced-by <agent> --responsibility <role>\n")
		fmt.Fprintf(b, "```\n\n")
		fmt.Fprintf(b, "The original consumer supplies the actual `audit_manifest_path`. Use the current professional Manifest to explain actual coverage and counterevidence; a path or row count is not proof of product behavior. Resolve unknowns and blocking findings and identify the owner/disposition of remaining risks. `s10 status` is optional read-only progress; do not call `runtime transition` manually.\n\n")
	case "TR-017":
		fmt.Fprintf(b, "Normal Main save validates the current release-audit Manifest and records its evidence. The commands below are explicit unpacketized/recovery examples:\n\n")
		fmt.Fprintf(b, "```text\n")
		fmt.Fprintf(b, "loop-harness s10 manifest validate --root <root> \\\n")
		fmt.Fprintf(b, "  --file <release-audit-manifest.json> --type release_audit\n")
		fmt.Fprintf(b, "loop-harness runtime evidence add --root <root> \\\n")
		fmt.Fprintf(b, "  --id <id> --kind release_audit \\\n")
		fmt.Fprintf(b, "  --path <envelope.json> --produced-by <agent> --responsibility \"Release Auditor\"\n")
		fmt.Fprintf(b, "```\n\n")
		fmt.Fprintf(b, "Eight audit themes guide relevance; use applicable actual observations rather than manufacturing eight rows. The supplied professional Manifest carries the outcome, evidence and risk disposition; Markdown prose alone is not its accepted record.\n\n")
	case "TR-018":
		fmt.Fprintf(b, "A blocked audit preserves actual observations and blockers in the current Manifest. Main normally saves that exact answer for original TR-018. Explicit unpacketized/recovery examples:\n\n")
		fmt.Fprintf(b, "```text\n")
		fmt.Fprintf(b, "loop-harness s10 manifest validate --root <root> \\\n")
		fmt.Fprintf(b, "  --file <release-audit-manifest.json> --type release_audit --outcome blocked\n")
		fmt.Fprintf(b, "loop-harness runtime evidence add --root <root> \\\n")
		fmt.Fprintf(b, "  --id <id> --kind release_audit \\\n")
		fmt.Fprintf(b, "  --path <envelope.json> --produced-by <agent> --responsibility \"Release Auditor\"\n")
		fmt.Fprintf(b, "```\n\n")
		fmt.Fprintf(b, "Keep actual blockers, their evidence and next responsibility in the Manifest. Original TR-018 records the pause; do not call `runtime transition` or manufacture audit rows.\n\n")
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
