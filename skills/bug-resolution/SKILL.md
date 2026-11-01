---
name: bug-resolution
description: Use when investigating accepted S7 Findings or an original S11 defect decision in S8, or executing an approved RepairContract in S9
category: methodology
version: 2.0.0
---
# Finding Investigation and Approved Repair

Use the S8 methods below for investigation. Read the S9 sections only when
performing an approved repair, impact judgment or independent targeted check.

## Authority

Runtime legality belongs to `docs/control/loop-definition.json`; stage duties and
normal questionnaire shapes belong to [agent-protocol.md](../../docs/control/agent-protocol.md#s8).

The chain is accepted S7 Findings or original S11 defect decision
→ InvestigationCase → HypothesisResults and CausalModel → approved RepairContract
→ S9. A Finding is an immutable observation; the Case owns causal reasoning and
route. A canonical BUG is only an optional post-approval projection.

## Required Inputs

Read the exact current view and answer paths supplied for your responsibility.
Main fills its intake, hypothesis array, route and Contract professional objects;
the assigned Investigator fills only its issued `answer`. Main performs actual
native Agent dispatch with each person's exact view/answer references. Saving a
Main question uses its original consumer; the Investigator saves its own original
answer once. PreToolUse observes the current original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. Reception checks parsing, the stated necessary fields and the routing conclusion once, then saves the accepted fact once. An unanswered conclusion stays a draft. No return reference, re-save, done flag, Git check or evidence/hash revalidation qualifies reception.
Known identities, versions and original responsibility paths remain system-owned.
Actual approval and the object it approves retain their original authority.

## Entry Conditions

Read the accepted S7 answers, Findings, encounter and unfinished work, or the actual
S11 defect decision consumed by TR-027. Historical ObservationBatch material may be
read for an existing responsibility; new intake does not require a Batch or a new
source proof. A human-source Case needs no invented Finding, Claim or Batch.
With no Findings, explanation and hypothesis source_finding_ids arrays stay [].
Current Runtime maintains one active Case. Main handles causal grouping and actual
routes; do not invent split/merge scheduling. After S7 has entered S8, already
issued S7 responsibilities can still deliver their first answer. Main receives new
Findings and decides whether to supplement the investigation or arrange later work.
For the current investigation, select an accepted late Finding by its actual ID in
the original new hypothesis; the Case Writer carries it with that question. Existing
source facts and approvals are not automatically rewritten.

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
  or refutation. Explain how inconclusive and historical observations affect the
  current judgment; these fields are professional reasoning, not a machine closure
  proof or a requirement for at least one refuted result. Put a contrast
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

Preserve original evidence references. Existing IDs, `runtime:<id>`, Finding
references and actual file paths keep their meanings. Describe new observations
with real readable locations and actual commands, conditions and outcomes. The
receiver records the professional answer; it does not prove execution, recapture
sources, register aliases or revalidate historical evidence and hashes. Missing
material that prevents professional judgment is an honest gap for Main.

`new_hypotheses` is a suggestion array in the original result. Saving it does not
register Hypotheses or Assignments and does not add stage exit conditions. Main
judges whether a suggested question would change the causal explanation, scope or
route, then registers and actually dispatches adopted work through the original
Main questionnaire. Existing issued source forms stay readable; a new question
whose original preparation is unsupported remains Main's named preparation gap.

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
partial answer. Save the original answer and report the actual unfinished work;
Main receives unfinished work and diagnostics, while the original consumer leaves drafts unreceived and reports concrete structural gaps.
A native return does not establish acceptance or stage advancement.

Main builds `trigger → violated invariant → faulty mechanism → propagation →
symptoms`, full impact and detection gap. Every source retains an explicit
disposition. Choose the original route: `s9_repair`, `s2_spec_rework`,
`human_req_change`, `s7_no_change`, `investigate_more` or `duplicate`. No-change
requires evidence that no product/spec/test/config artifact needs changing;
requirement changes retain human authority. Unsupported causes remain open.

Continue investigation through the original Main hypotheses file and actually
dispatch appropriate follow-ups, preserving earlier questions and results.
Continuing work does not require an `investigate_more` route registration.
Main may revise the original route answer after `investigate_more` as professional
judgment develops, including with the same material. Replaying an unchanged answer
is idempotent. Concrete dispositions and actual repair-contract approval retain
their original protections; different contract content needs its actual approval.
Choose an outlet when its evidence and authority hold. Investigation counts and
new-evidence fingerprints do not qualify or limit Main's judgment.

For missing/stale sources, ambiguous references or revision conflicts, Main
restores the original responsibility and preserves received sibling work. For
lost acknowledgement, Main reads the original receipt; accepted facts are read from the original Case/receipt without relying on surviving
private bytes. Do not recreate an accepted answer or re-run its review.
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
brief and natural-event reception. The current brief or original Targeted request selects the
responsibility; a test-builder may implement tests or independently verify, but
must not verify work it implemented or merged. Main reads its own division/recovery
questions and accepted artifacts. It integrates Candidates and runs actual checks
before handing its Impact to the next responsibility; an accepted Worker does not wait for Main or refill Result.
S8 retains investigation and approval.

## Issued S9 PlanReport adapter

Main reads the approved Contract and RepairPlan, selects actual specialized
Builders and independent verification work, and performs real native spawn/resume.
The system supplies exact personal view/answer paths and the current-step brief;
Main carries these into the native task without authoring or saving Worker answers.
Read the Main-root question as input. The brief, question and method materials belong to the Main control root; Main supplies their readable absolute paths and passes the actual task restrictions to each Worker. A complete control-root-relative reference or its `./` form can identify the same original native S9 responsibility as its absolute reference, without implying that the material exists in the Worker cwd. Follow [the S9 reference contract](../agent-dispatch/SKILL.md#native-s9); private output paths remain relative to the actual worktree. The native brief supplies private
PlanReport and Result paths relative to the actual native worktree. The Builder
personally saves its top-level `plan` and applicable real `red_checks` and `proposed_paths` there
without an `answer` wrapper before implementation, then continues without a save receipt or another startup step. A missing nonessential precheck field, evidence reference or proposed path does not prevent reception.
Each precheck is `{name,command,result,evidence_refs}` with the actual attempted
command and honest `fail/pass/blocked` result. Cite real logs or existing
references; a blocked attempt or passing precheck does not require invented red
evidence. These commands record past work, not Main's later check selection.
The actual prechecks may cover a subset of the assigned assertions; their results,
or Main's selection of RED experiments, do not reduce the Result assertion set.

Report actual results and unfinished work to Main when finished. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. Reception does not wait for a final message or returned reference. The original consumer receives the same personal Plan independently, then the author continues work.
The original Plan and Result are each read and received once from their supplied
personal paths. No Git/evidence/hash preservation proof or flat-to-typed author
copy qualifies reception. Actual product delivery, real self-checks and Main's
integration/checks remain professional work. Accepted facts do not depend on
private files surviving cleanup; an incomplete successor follows its original
continuation without another author save or Stop.
The Main question is not the native output path. Main owns division, dependencies and necessary
integration; Claude Code owns
worktree creation and execution. Do not write Main, wait for peer plans, manually
issue/consume, copy machine bindings or add a generic activation report. Missing
paths/environment go to Main as concrete blockers.

Main-owned reception recovery preserves these authored facts. Natural-event reception needs no extra submission, re-save or returned path. If the current diagnostic names an exact original private flat input, saved typed answer or Targeted answer, Main may run the supplied `runtime stage-answer consume --root <Main-root> --path <exact-original-input>` through the same adapter and consumer. Use the named original bytes and responsibility; do not reconstruct an answer, infer a path from role, or wake the Builder solely to return saved work. Accepted PlanReport remains accepted. An accepted candidate requires Main integration/checks, and accepted Targeted with unfinished Handoff retains its accepted conclusion while Main/system follows the actual failure feedback for the original continuation. A Worker continues only actual unfinished professional work; source/preparation/transaction failures stay with Main/system.

## Issued S9 RepairResult after implementation

Cite actual readable evidence locations and observations in the original Result.
Preserve their original meaning and source context. Reception saves the personal
answer; it does not capture or hash evidence bytes, register aliases or require
Git commits. Authors and Main judge whether the evidence supports the work and
remains usable for actual checks. Missing evidence is an honest professional gap,
not a reason to fabricate references or repeat an already accepted answer.

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
and after work. Save the ready original Result; PreToolUse listens, with successful PostToolUse and Stop/SubagentStop fallback. Report actual results and unfinished work to Main; reception needs no returned Result reference or second generic completion prerequisite. Actual contributions still require Main
integration and necessary Main checks. Main preserves received work and owns any system reception gap; the Worker continues only unfinished professional work.
Main integrates actual contributions and runs necessary checks, and naturally saves its
original Impact judgment. Impact may be prepared or received early; the empty
original arrays are receivable even when indistinguishable from the default template.
Reception does not prove integration or checks and does not close in-flight Builder
work; Main may naturally update the same original judgment as work continues. No particular integration command, commit, Final or
proof artifact qualifies Impact reception. Empty changes and empty dispositions
are legitimate; do not invent a diff, commit, change type or decision to advance.
No-change work still needs the applicable independent observations and a fresh
complete S7. If the repair premise is invalid, return it to Main for the original
S8 disposition. Save with any supported editor, script or extension tool.

## S9 impact, independent verification and handoff

Actual Contract approval prepares the original Session/Plan and Main division
drafts. Explicit session startup followed by plan compile also prepares those
existing division drafts. Natural events observe the original responsibility and may continue the appropriate
existing preparation or handoff. An accepted contribution can prepare the original ChangeImpact question. Main
performs actual integration and checks and owns the timing and substance of its
professional judgment; preparation or reception does not prove those acts.

Main saves the exact ChangeImpact questionnaire with professional change types,
per-change decisions/rationale and evidence dispositions; known references come from the original accepted contribution. Empty
change types and empty decisions are valid statements of no change or no disposition;
optional explanation is not an admission field. No physical Session rescan, exact-set, freshness, registered-evidence or hash
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
real observations, saves the original answer and reports actual results to Main. A returned reference is not an additional reception prerequisite. PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. Record the overall `result` and any actual `assertion_results` (`assertion_id,result,evidence_refs`) or applicable `failure_class` at the exact path. Missing nonessential observation or evidence fields do not reject a comprehensible overall conclusion; `fail` without a classification remains a failure of unknown cause, not an inferred same-cause failure. `continuity_reason` and
`scope_compliance` are optional legacy metadata; do not add them to obtain
acceptance. Machine times and the Contract-level required reverification ID come
from the original consumer, including same-answer recovery after interruption.
Assertion results are `pass/fail/blocked`; overall result is
`pass/fail/blocked/scope_changed`. Use actual evidence, not the Builder's conclusion;
the verifier must not have implemented or merged the work being verified. The verifier owns the necessary assertion observations and the overall
conclusion. Missing observations stay explicit for Main; reception does not
recompute assertion coverage or override the stated overall conclusion. Extra observations and ID spelling/count/aliases, owner pools, issue
chains or evidence registration are not report qualifications. Honest fail/blocked
reports can be recorded without pretending that unfinished checks passed.

For blockers, save actual attempts/gaps and use the accurate current resolution
question. Do not fabricate PASS or manually issue a successor answer. The system
attempts the existing Handoff in the original completion outlet after the final
successful Targeted reception. If that followup fails, preserve accepted Targeted
and follow the exact original continuation supplied by feedback; do not repeat
independent observations, add a report or create a second review responsibility. An unchanged accepted answer is not re-received by ordinary Pre or status. Continue the original professional responsibility without a default iteration budget or count-based approval; follow actual user instructions and existing real decisions. Missing
preparation receives its concrete recovery entry. Successful TR-012 supplies actual
Handoff/seed and the original S7 ReviewPlan questionnaire paths. Main fills and saves
that questionnaire; do not add manual review-plan registration or interpret seed
existence as PASS. No new Handoff form is required.
Hook listening adds no tool/identity/scope ACL; actual checks, independence,
actual pending user decisions under their existing authorization and the original
Writer’s atomicity/CAS/idempotency remain. Candidate reception, Main integration/checks, Impact and independent verification
are distinct responsibilities. Historical Final records remain readable; a new
Final or preservation proof is not an Impact prerequisite.


An accepted blocked or unfinished handoff does not end the original responsibility.
The same author can naturally continue its unfinished work without another Main
approval or submit/resume action. A new comprehensible conclusion is a new handoff;
retain the earlier accepted facts and do not treat changed private bytes as a new
responsibility or as a revision of historical facts.

## Contract stop conditions in independent verification

Read the original approved Contract stop/escalation conditions in the supplied Targeted context. Judge them against actual observations alongside the necessary symptom/root/gap assertions. Record a triggered scope or Contract change as `scope_changed` with real reason/evidence through the original S8 reassessment and approval route; an environmental inability is `blocked`. Residual-risk wording cannot replace required authority or justify PASS. Use the existing Targeted fields; do not author stop IDs, hashes or another assessment array. Missing or changed Contract material belongs to Main preparation.
