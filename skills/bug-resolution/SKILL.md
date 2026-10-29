---
name: bug-resolution
description: Use when investigating a sealed S7 Finding batch or an original S11 defect decision in S8, or executing an approved RepairContract in S9
category: methodology
version: 2.0.0
---
# Finding Investigation and Approved Repair

Use the S8 methods below for investigation. Read the S9 sections only when
performing an approved repair, impact judgment or independent targeted check.

## Authority

Runtime legality belongs to `docs/control/loop-definition.json`; stage duties and
normal questionnaire shapes belong to [agent-protocol.md](../../docs/control/agent-protocol.md#s8).

The chain is sealed S7 Finding/ObservationBatch or original S11 defect decision
→ InvestigationCase → HypothesisResults and CausalModel → approved RepairContract
→ S9. A Finding is an immutable observation; the Case owns causal reasoning and
route. A canonical BUG is only an optional post-approval projection.

## Required Inputs

Read the exact current view and answer paths supplied for your responsibility.
Main fills its intake, hypothesis array, route and Contract professional objects;
the assigned Investigator fills only its issued `answer`. Main performs actual
native Agent dispatch with each person's exact view/answer references. Saving a
Main question uses its original consumer; Investigator saving and returning the
reference invokes the original Case consumer at SubagentStop. Ordinary Worker
tools do not activate or receive work. Known IDs, hashes, versions and source
bindings are system-owned, without duplicate reports or manual issue/consume.

## Entry Conditions

Intake requires the actual sealed batch or original S11 decision consumed by
TR-027. Preserve exact Finding coverage, original evidence, capture gaps and P0
unobserved Claims. A human-source Case uses the original decision and frozen
ReviewPlan product baseline; do not fabricate a Batch/Finding or run S7 for IDs.
With no Findings, explanation and hypothesis source_finding_ids arrays stay [].
A missing, changed or ambiguous source is Main/system preparation or recovery.

Current Runtime has one active Case with an immutable source set. Grouping by a
candidate shared mechanism is revisable professional reasoning; arbitrary Case
split/merge and parallel mixed-route aggregation are not implemented. Distinguish
mixed mechanisms, explained sets and unresolved items within the original Case.
If independent Cases or incompatible routes are needed, Main explicitly handles
the authority/recovery boundary; do not delete sources or invent Case pointers.
Existing canonical Case/Contract references support the original duplicate route.

## Procedure

### Causal investigation methods

- Consume the frozen `last_good → wall_action → first_bad`, timeline, state delta,
  side effects, terminal state or static inspection trail before reproducing a
  symptom. S8 does not repeat S7 completeness/capture work by default.
- Group candidate explanations by invariant, contract, propagation path and
  compatible repair boundary. Similar errors, files or owners do not prove a
  common cause. Map every source symptom to the mechanism or its unresolved gap.
- Form falsifiable statements and compare important alternatives. Record what
  concrete observation supports/refutes each explanation. When no credible
  competing hypothesis exists, Main explains the search and absence in the
  supplied `no_competing_hypothesis` field rather than inventing a weak alternative.
  When credible alternatives were handled by an accepted discriminator, Main uses
  `competing_hypothesis_assessment` with `hypothesis_ids` for current accepted
  supported/refuted results and a `conclusion` explaining their actual treatment.
  This may retain a contrast while locating the gap; it does not assert absence
  or refutation. Inconclusive and historical-only results cannot close this
  assessment; do not combine it with `no_competing_hypothesis`. Put a contrast
  in `discriminator` when the same experiment distinguishes mechanisms; register
  another Hypothesis only for an independently falsifiable question needing its
  own actual result.
- Put the particular experiment's read/command scope, stopping and unresolved
  conditions in the existing `discriminator`; put support/refute predictions in
  `expected_outcomes`. Do not add unsupported scope/stop fields. General product,
  formal-test, locked-specification and formal-configuration read-only limits
  remain part of the issued responsibility.
- Choose methods from the actual boundary: same-call value comparison, contract
  and type comparison, log/state correlation, read/write propagation, existing
  test predicates or safe isolated controls. Do not modify the product and then
  use its changed behavior to prove the original cause.
- Clarify existing evidence before collecting more. A new observation requires a
  named discriminator and safe evidence gap. FindingSupplement is only for
  supplementing or clarifying the original Finding scene; Main coordinates that
  append-only work with the finder or an authorized replacement. The
  Investigator's own new logs, comparisons and controlled-experiment observations
  go directly into the original HypothesisResult with real file references;
  they require neither a Supplement nor a handoff to the finder.
  Preserve baseline or demonstrably equivalent snapshot conditions, including
  data, account, cache, timing and external dependencies. Unsafe capture remains
  a stated gap, including P0 gaps.

Preserve original evidence references: `<id>` and `runtime:<id>` resolve the same
indexed artifact with its original validity/generation/round/SHA checks. Do not
register aliases or rewrite accepted Findings. A `runtime:` reference never
proves execution. Use the issued evidence instructions to distinguish original
source boundaries, frozen/registered artifacts and new observations. For new
observations, save real files in the author's source checkout (normally beside
the answer) and put `path:<path-relative-to-checkout-root>` in
`answer.evidence_refs`; describe actual commands, conditions and observations.
The original receiver reads and preserves those files with fixed digests in
Main, without author hashes or manual evidence registration. Missing or
out-of-root files are errors. New questions in the same answer may also cite
real local files in `new_hypotheses.evidence_refs`; the original consumer fixes
their bytes before Main dispatches the question. Main reuses the fixed references
in the accepted Case for later questions, detection gaps and technical review.
Main may also create its first or later hypothesis using the exact frozen
references in that question's selected source Findings; no evidence-index alias
or prior S8 result is required. The original consumer validates their Case,
batch, selected-Finding binding and actual content. Missing necessary
source/instructions goes to Main; do not invent URI evidence.

For an S8 Investigator, automatically loaded code-quality and testing-strategy
methods serve only the assigned hypothesis and discriminator. They do not impose
ReviewPlan Claims, module positive/negative ratios, full-chain validation or test
rewriting on this read-only responsibility. S6 implementation and S7 review
retain their original stage-specific obligations; Main owns Case-wide closure.

## Results, causal synthesis and recovery

Each Investigator reports its own method, actual observations, evidence,
supported/refuted/inconclusive result and explained/unexplained symptoms in the
original answer. Main does not author that result. If blocked, use
`result="inconclusive"` when a complete truthful answer is possible; record actual
attempts in `answer.method`, and the blocker, missing material and Main's next
action in `answer.observed`. Mandatory missing material can leave a truthful
partial answer. Save and return its exact reference on the first native return;
Main receives unfinished work and diagnostics, while the original consumer still
rejects incomplete/invalid answers. Return is not acceptance or stage advancement.

Main builds `trigger → violated invariant → faulty mechanism → propagation →
symptoms`, full impact and detection gap. Every source retains an explicit
disposition. Choose the original route: `s9_repair`, `s2_spec_rework`,
`human_req_change`, `s7_no_change`, `investigate_more` or `duplicate`. No-change
requires evidence that no product/spec/test/config artifact needs changing;
requirement changes retain human authority. Unsupported causes remain open.

Continue investigation with genuinely new discriminating facts or accepted
current results. Preserve old questions/results and add distinct follow-ups in
the original Main hypotheses file, then actually dispatch them. Continuing work
does not require another `investigate_more` route registration, including when
the route budget is exhausted; retain the exact original hypotheses-file path.
More questions, reordered text or a changed route reason alone is not progress;
actual new observations may reuse the same valid source reference. Main explains
how later results resolve earlier truthful inconclusive gaps. Follow the current
re-entry feedback rather than manufacturing certainty to bypass a budget.
`configuration.repair.max_investigation_reentries` bounds consecutive
`investigate_more` route registrations, independently of S7 full-review rounds;
absent configuration uses S8's default of 5. At the limit, stay in the same Case
and obtain real discriminators/results as needed. The limit does not forbid
investigation or require a forced alternative route or human approval; stop
repeating `investigate_more` registrations and choose an outlet only when its
evidence and authority actually hold.

For missing/stale sources, ambiguous references or revision conflicts, Main
restores the original responsibility and preserves received sibling work. For
lost acknowledgement, Main reads the original receipt; actual answer replay
compares its saved bytes. Do not recreate an accepted answer or re-run its review.
After an actual non-blocked S9 targeted failure, Main reopens the same Case with
`causal_reassessment_refs` and dispatches a new discriminator. Environment blockers
remain S9 recovery; failure to execute does not itself disprove the causal model.

## Complete repair intent and actual approval

For `s9_repair`, Main prepares the complete invariant/intent, prospective and
forbidden scope, repair units/assertions, relevant compatibility/migration/
rollback needs and stop/escalation conditions. Review real entry-to-effect paths
and unit dependencies so required wiring, shared types, persistence and tests are
not omitted. Do not shrink a scope by copying a shared business rule locally.

Select verification for the repaired boundary. For a wiring defect, an actual
entry-point check and a wiring-removal negative control may distinguish reachability
from helper correctness. For several promised variants (such as slot families),
cover the relevant variants rather than extrapolating from one helper. For an
atomic operation (such as save+clear), preserve the required transaction boundary.
These are conditional examples, not mandatory work for unrelated contracts.
A historical baseline comparison must use a coherent pre-repair baseline;
matching failure names/counts alone does not prove matching causes.

The system fixes and supplies the final real Contract draft and exact original
decision application. Technical review follows its actual assigned responsibility;
Architect review is conditional on cross-module, source-of-truth or contract-boundary
changes. Under a pinned project policy or valid bounded grant, the designated
Driver supplies actual review judgments/evidence. That Driver may be Main when
Main is designated, in which case Main fills and saves the original application.
Otherwise Main supplies the designated Driver the exact fixed final draft and
review fields; that Driver returns their own four judgments, evidence and reason,
and Main faithfully records them and saves the original application. A Worker's
save event does not consume Main's application. Do not add a compulsory second
reviewer, new Worker questionnaire or second report.

Without valid bounded authority, Main obtains and faithfully records the real
human's `approve_contract`, `reject_contract` or `defer` choice and necessary
reason. A finite grant is optional and requires actual human reviewer/scope/
budget/expiry choices. See [bounded repair autonomy](../../docs/bounded-repair-autonomy.md).
Saving a Contract, review or empty decision does not approve repair. Only the
original `S8-REPAIR-CONTRACT-APPROVAL` success enters S9 `repair_readback`.
Approval binds the final bytes; changed intent needs its original new review and
authority. Projection or S9 preparation failures resume from that same approval,
without a second approval or rollback to an uninvestigated state.

## Outputs

The original Investigator answer and its receipt or concrete unfinished-work
diagnostic; Main's versioned Case disposition and, for repair, the approved
Contract and original S9 handoff. Keep optional BUG projections downstream of
approval.

## Exit Conditions

Each source has an explicit evidence-backed disposition. Repair enters S9 only
with the original approved Contract; other routes retain their reason and next
owner. A personal accepted answer completes that responsibility, not the Case.

## Stop Conditions

Return missing sources, unsafe or unavailable discriminators, unexplained
symptoms, incomplete repair scope and insufficient authority to the actual
responsible owner. Preserve truthful partial work; do not force a conclusion,
route or approval to finish native execution.

## Non-Goals

S8 does not modify product/formal tests/locked specifications or reconstruct S7
by default. This Skill adds no arbitrary Case split/merge capability, generic
report, tool permission gate or substitute for actual human authority. S9 does
not reinterpret the approved cause; targeted verification does not replace a
fresh complete S7 round.

## Native S9 dispatch

Use [agent-dispatch](../agent-dispatch/SKILL.md#native-s9) for exact native
brief/return handling. The current brief or original Targeted request selects the
responsibility; a test-builder may implement tests or independently verify, but
must not verify work it implemented or merged. Main reads its own division/recovery
questions and accepted artifacts. It integrates Candidates and runs actual checks
before Final/Impact; an accepted Worker does not wait for Final or refill Result.
S8 retains investigation and approval. Only registered frozen execution retains
its original [recovery contract](../agent-dispatch/frozen-execution.md).

## Issued S9 PlanReport adapter

Main reads the approved Contract and RepairPlan, selects actual specialized
Builders and independent verification work, and performs real native spawn/resume.
The system supplies exact personal view/answer paths and the current-step brief;
Main carries these into the native task without authoring or saving Worker answers.
Read the Main-root question as input. The native brief supplies private
PlanReport and Result paths relative to the actual native worktree. The Builder
personally saves top-level `plan`, real `red_checks` and `proposed_paths` there
without an `answer` wrapper before implementation, then continues without a save receipt or another startup step.
Each precheck is `{name,command,result,evidence_refs}` with the actual attempted
command and honest `fail/pass/blocked` result. Cite real logs or existing
references; a blocked attempt or passing precheck does not require invented red
evidence. These commands record past work, not Main's later check selection.
The actual prechecks may cover a subset of the assigned assertions; their results,
or Main's selection of RED experiments, do not reduce the Result assertion set.

Report actual results and unfinished work to Main when finished. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. Reception does not wait for a final message or returned reference. The adapter imports the private PlanReport into the original questionnaire and invokes the existing consumers.
For newly prepared completed no-change S9 work, the system first preserves exact
authored Plan/flat Result bytes, required local evidence and the real Git source
at that natural reception boundary. A successfully registered immutable technical source supports the
original consumers after cleanup; it is not acceptance or PASS. Missing or failed
preservation cannot recover lost bytes. PreToolUse listens for ready original S9 work, with successful PostToolUse and Stop/SubagentStop fallback.
Platform 2.1.311 isolated runs verified files surviving through Stop and subsequent
cleanup; full harness preservation/reception acceptance remains pending.
The Main question is not the native output path; registered frozen execution keeps
its original answer shape and binding. Main owns division, dependencies and necessary
integration; Claude Code owns
worktree creation and execution. Do not write Main, wait for peer plans, manually
issue/consume, copy machine bindings or add a generic activation report. Missing
paths/environment go to Main as concrete blockers. Existing frozen executions
use their original [recovery contract](../agent-dispatch/frozen-execution.md).

Main-owned reception recovery preserves these authored facts. Normal SubagentStop needs no extra submission. If the current diagnostic names an exact original private flat input, saved typed answer or Targeted answer, Main may run the supplied `runtime stage-answer consume --root <Main-root> --path <exact-original-input>` through the same adapter and consumer. Use the named original bytes and responsibility; do not reconstruct an answer, infer a path from role, or wake the Builder solely to return saved work. Accepted PlanReport remains accepted. An accepted candidate requires Main integration/checks, and accepted Targeted with unfinished Handoff follows `runtime repair handoff complete`. A Worker continues only actual unfinished professional work; source/preparation/transaction failures stay with Main/system.

## Issued S9 RepairResult after implementation

For new native delivery, local files in Result `evidence_refs` (top level and
nested observations) use the registered Builder root, never Main's root.
Prefer `path:logs/check.txt` (no authored hash needed); slash-containing relative
file references and absolute files within that Builder root are also accepted.
The original Result receiver captures these actual bytes into Main and keeps the
original authored references and source root in its immutable provenance. These
referenced local check outputs need no extra evidence commit; product deliverables
keep their original commit requirement. A missing local file is a source handoff gap: restore the original file and retry
reception, without inventing observations. URI references retain their meaning;
bare tokens are opaque, unarchived identifiers. For a filename without a directory,
use `path:check.log` or `./check.log`. Migration, rollback and detection-gap artifact
references keep their original meaning and are not included in this capture. This
Result capture does not claim to archive Plan-only evidence: PlanReport retains
its original references and Builder source context. Registered frozen executions
keep their original reception requirements and unarchived source references.

At the supplied Worker root, implement and self-check, or complete assigned
no-change tests, then personally save flat top-level professional fields at the private Result path
in the native brief. Do not add an `answer` wrapper or write Main. Fill the
professional verdict, actual assertion observations, unit results, evidence,
scope deviations, risks and applicable migration/rollback fields. Known machine
and Git/check facts come from the system; green checks do not replace judgment.
In `before_fix_results`, record one truthful pre-repair observation for every
assertion in the original RepairAssignment supplied by the brief, in its original
order, including assertions already satisfied, unreproduced or blocked. The
`observed_red` field retains its historical name; describe the actual observation
and evidence without manufacturing a failure. A RED-only experiment subset in a
parent task does not replace this original Result responsibility.
No-change testing requires no new commit. Existing Contract or Git references may
support actual migration/rollback needs; nonempty “No migration” text does not
force two references or another report.

PlanReport and RepairResult record the two different professional steps before
and after work. Save the ready original Result; PreToolUse listens, with successful PostToolUse and Stop/SubagentStop fallback. Return the same saved Result reference,
without a second generic completion prerequisite. Actual changes still require real Git delivery, Main
integration and necessary Main checks; retain the Worker until Main receives it.
Main uses the supplied `runtime workspace integrate --root <Main-root> --assignment <execution-assignment-id> --agent <owner-agent-id> --check-command '<actual necessary Main check command>'`; root/assignment/owner come from the current execution, while Main selects actual necessary checks (repeat `--check-command` for multiple checks). This typed S9 path records real integration and Final; do not substitute generic `runtime task-integrate`.
No-change results still receive real required checks. If every accepted Final has no product change, Main keeps the supplied empty artifact set and change_types=[], explains the original Case/Contract disposition in the existing Impact decisions, and still arranges all necessary independent assertion observations and a fresh full S7 round. If the repair premise is no longer valid, return to the original S8 disposition instead. Do not invent a diff, commit or change type to advance. Save through any supported
editor, script or extension tool; do not send a separate completion report or
copy identities, hashes, revisions or check receipts into professional answers.

## S9 impact, independent verification and handoff

Actual Contract approval prepares the original Session/Plan and Main division
drafts. Explicit session startup followed by plan compile also prepares those
existing division drafts. Ordinary tools only read current responsibilities and
paths; they do not perform this preparation. After real Main integration, the
system prepares ChangeImpact only when the current repair facts reach that step;
otherwise it reports the actual remaining assignment work.

Main saves the exact ChangeImpact questionnaire with professional change types,
per-change decisions/rationale and evidence dispositions; system-known diff and
bindings come from accepted Final batch facts, merged with the existing last-wins
rule. No physical Session rescan, exact-set, freshness, registered-evidence or hash
proof qualifies this judgment. Actual file/output paths are valid evidence. Use the
professional field shapes/enums in that questionnaire,
including `decision` in `invalidate/supersede/retain/reverify`.
Successful Impact saving prepares the existing Targeted questionnaire for the
actually assigned independent verifier. If no verifier is selected yet, use Main's
current independent verifier division path; saving that selection at the
targeted step prepares that person's answer.
Main actually spawns/notifies the independent verifier. For new responsibilities,
use an independent native context sharing the actual final Main checkout. Its
original registered TASK fixes the normal authoring source; the system supplies
the exact ordinary `.loop-submissions/s9/.../targeted-....json` original. Existing
responsibilities keep their original source and path. Do not select a protocol by
file existence, move old answers or substitute Main's conclusions. An actual
native worktree without its original source binding remains unreceived.
That person uses only the
original Targeted request, with no Builder PlanReport/RepairResult, product writes
or manufactured commit. The questionnaire supplies
all necessary Contract assertion text and system slots; the verifier performs the
real observations, saves the original answer and returns that exact reference. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. Fill
`assertion_results` (`assertion_id,result,evidence_refs`), `result` and the
applicable `failure_class` at the exact path. `continuity_reason` and
`scope_compliance` are optional legacy metadata; do not add them to obtain
acceptance. Machine times and the Contract-level required reverification ID come
from the original consumer, including same-answer recovery after interruption.
Assertion results are `pass/fail/blocked`; overall result is
`pass/fail/blocked/scope_changed`. Use actual evidence, not the Builder's conclusion;
the verifier must not have implemented or merged the work being verified. Every
required assertion needs an actual result; specific missing observations prevent
advancement. Extra observations and ID spelling/count/aliases, owner pools, issue
chains or evidence registration are not report qualifications. Honest fail/blocked
reports can be recorded without pretending that unfinished checks passed.

For blockers, save actual attempts/gaps and use the accurate current resolution
question. Do not fabricate PASS or manually issue a successor answer. The system
attempts the existing Handoff in the original completion outlet after the final
successful Targeted reception. If that followup fails, preserve accepted Targeted
and follow the exact original continuation supplied by feedback; do not repeat
independent observations, add a report or increase the round twice. Ordinary Pre
and status stay read-only. A full-round budget condition uses the original prepared
budget question and real decision; an empty choice grants nothing. Missing
preparation receives its concrete recovery entry. Successful TR-012 supplies actual
Handoff/seed and the original S7 ReviewPlan questionnaire paths. Main fills and saves
that questionnaire; do not add manual review-plan registration or interpret seed
existence as PASS. No new Handoff form is required.
Hook listening adds no tool/identity/scope ACL; actual checks, independence,
actual pending user decisions under their existing authorization and the original
Writer’s atomicity/CAS/idempotency remain. Candidate, Main integration and Final
are distinct real steps; removing duplicate proof does not remove integration.

## Contract stop conditions in independent verification

Read the original approved Contract stop/escalation conditions in the supplied Targeted context. Judge them against actual observations alongside the necessary symptom/root/gap assertions. Record a triggered scope or Contract change as `scope_changed` with real reason/evidence through the original S8 reassessment and approval route; an environmental inability is `blocked`. Residual-risk wording cannot replace required authority or justify PASS. Use the existing Targeted fields; do not author stop IDs, hashes or another assessment array. Missing or changed Contract material belongs to Main preparation.
