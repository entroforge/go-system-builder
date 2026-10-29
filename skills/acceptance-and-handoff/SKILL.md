---
name: acceptance-and-handoff
description: Use when a current complete clean round enters acceptance or release audit and prepares the existing human handoff
category: methodology
version: 1.3.0
---
# Acceptance And Handoff

## Authority

Follow the installed [S10 stage contract](../../docs/control/agent-protocol.md#s10) and `docs/control/loop-definition.json`.

Acceptance checks the actual REQ commitments and applicable Closing Contracts.
Release audit checks the system and operational concerns relevant to this change.
They are two professional steps, using the existing Manifest at the exact path
supplied by the system.

## Entry Conditions

The current responsibility is acceptance or release audit after a complete S7
clean round. A targeted check or fresh S7 seed is not a complete clean round.

## Required Inputs

Read the current S7 results, commitment text, existing expectations and material
paths supplied for this responsibility.

The system supplies known responsibility/round associations and machine
summaries. Do not copy IDs, hashes, receipts, metrics or source certificates.
Responsibility progress uses the existing five states `pending/active/blocked/done/cancelled`;
professional outcomes, stage cursors and the human decision remain distinct.

## Procedure

1. Personally check the commitments and relevant counterexamples. Record actual
   observations, professional conclusions, evidence locations/output, risks and
   unfinished reasons in the existing fields. The existing oracle field may
   explain an actual observation; it is not another proof format.
2. Reuse real S7 findings, checks and other existing observations when they apply.
   One observation may support a main conclusion and its related counterexample;
   do not invent another test, task, evidence artifact or summary for each row.
3. Review actual delivery, compatibility, migration, rollback and operations
   concerns. The eight audit themes are useful questions, not mandatory counts.
   No applicable object means no invented N/A row, citation or task. Not checked
   is unfinished, not automatically N/A or PASS.
4. Main arranges and actually notifies any necessary independent reviewer.
   Actual independence concerns participation in the work, not the spelling of
   an owner/role field. Report real risk impact, who handles it and a usable
   follow-up location; existing local work records are valid locations.
5. If a review exposes a product, architecture or REQ problem, report it and
   follow the existing return or human-decision route. Do not patch product or
   locked requirement/specification files inside S10. Any genuine change that
   invalidates the current verification needs the original S7 work again.

## Outputs

Fill the explicit professional outcome for the current acceptance/release-audit
step and save the supplied Manifest. Unknown, failed or incomplete reports are
recorded honestly and do not imply that the stage is complete. Leave actual
missing work visible in the same file; no new report/state is required.

The normal receiver reads existing field types and current responsibility,
records through the original atomic Writer and projects the same content to the
human-readable ACC/audit report. You do not maintain a second envelope, summary,
Markdown report or registration transaction. Do not manually issue/consume or
run `runtime evidence add` as a normal author step. Explicit validate/render
commands remain diagnostic tools when addressing a concrete reported problem.

## Stop Conditions

Actual unfinished work prevents completion and is reported to its current owner;
it does not block ordinary tools or prevent recording an honest report.
The Hook can show current recorded outcomes, unanswered/unknown/failed rows,
reported blockers, material paths and the next responsibility. It cannot prove
all natural-language promises were exhaustively checked, authenticate evidence
or judge actual independence. Do not turn counts, fixed categories, evidence
array size, tracking-ticket spelling or source hashes into acceptance tests.
Machine metrics describe reported progress, not reality or release approval.

## Exit Conditions

A complete acceptance save follows the original TR-015 path to the next accurate
audit questionnaire. A complete relevant audit follows TR-017 to the existing
S11 handoff. The current complete S7 clean-round condition still applies; the
normal completion exit does not wait for another Pre to repeat source proofs.
The existing `review_required`/`blocked` routes preserve actual unfinished work.

An explicit acceptance `review_required` returns to a complete S7 round through
TR-016. The system derives its original impact input from the received decision
and the current ReviewPlan's frozen scope; this does not assert product changes.
Do not create a Changeset, RepairSession, Finding or duplicate impact report to
obtain a return. Main restores any specifically reported missing original plan
or scope. The received acceptance stays received; normal Main completion resumes
that original consumer after source recovery. Do not refill or resave unchanged
acceptance. The original full-review budget still applies, and the new S7 plan
requires actual professional judgment and independent review.

## Non-Goals

S11 reuses accepted S10 readable reports, real residual risks and exact paths; no duplicate human package or metadata form. TR-017 prepares materials once; ordinary Pre/Stop only reads. The human explicitly chooses one of the six dispositions. Main faithfully fills the supplied application’s original `disposition` and optional `reason` and saves; PostToolUse automatically records/consumes through the existing Writer and fixed outlet. Without a real human choice leave disposition empty and wait: drafts do not write Runtime or default to approval. The Hook does not authenticate identity or demand extra confirmation/proof fields; the system supplies metadata/caller attribution. The original human-decision command is compatibility/recovery, not normal author work or a second submission. Return routes carry actual reason/paths to S8/S10; retain observations but clear the returned S10 step’s old final conclusion. No consumed old application is reused. S10 does not merge, publish, deploy or formally release
on the human's behalf. Removing tool blocking does not grant release approval,
and recording an audit does not stand in for a human decision.

If the review identifies a Design Foundation issue, report the actual affected
constraint and use its existing owner/decision route. This does not require a
second receipt, a new task matrix or an unrelated replay to receive the S10
report. A professional report cannot authorize a Kernel or REQ change.
