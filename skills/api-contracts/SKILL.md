---
name: api-contracts
description: Use when APIs, events, shared schemas, integration payloads, or compatibility promises change
category: best-practice
version: 1.1.1
---
# API Contracts
## Authority
S3 uses the locked REQ and registered S2 design to define contracts. Locked contracts own implementation semantics. Stage routing lives in `docs/control/agent-protocol.md`; work dispatched through an Assignment uses its manifest and Agent message.
## Applicability
Apply to `api` risk and shared component boundaries.
## Required Inputs
For S3, read the frozen REQ/S2 inputs and supplied contract templates. For changes to an existing boundary, read its FE/BE/SYNC contracts, schemas, consumers, version policy and error model.
## Quality Criteria
Check field meaning, validation, compatibility, defaults, error codes, idempotency, pagination, ordering, retries, and side effects.
## Outputs
S3 contract design and its professional answer, or contract-aligned implementation and compatibility evidence for an implementation Assignment.
## N/A Criteria
N/A when no machine-consumed boundary changes.
## Stop Conditions
Resolve consumer disagreement, unspecified compatibility and ambiguous side
effects within the authorized S3 design. Return through formal design rework
when frozen S2 inputs must change; use the human gateway only for actual REQ
semantic changes or unresolved decisions outside the existing authorization.
An implementation responsibility reports a locked-contract gap to Main rather
than silently changing its baseline.
## Non-Goals
Do not silently extend accepted contracts or frozen upstream baselines. A
current unreceived S3 document marked `locked` is an author readiness statement
and can still be corrected before normal submission.

## Inlined Methodology

For implementation, trace the assigned TASK's exact clauses through shared
definitions, producer and consumer behavior. Check serialization, validation,
compatibility and failures against those sources, and retain actual evidence in
the supplied professional outputs. Native delivery follows
[agent-dispatch](../agent-dispatch/SKILL.md#native-s6-s6-native-v1);
this technical method adds no lifecycle or approval step.

For S3 Main, follow the frozen REQ/S2 input view and
[S3 stage instructions](../../docs/control/agent-protocol.md#s3).
Use the exact answer, output and template paths supplied by the Hook.
Create the current REQ's index and applicable contracts; fill the professional
answer once and save. The Hook names the bound development branch and any
contract outputs read from Git: commit those completed professional files and
their Git-sourced model/example dependencies there before saving the answer.
S3 outputs can change during this entry; REQ/S2 inputs remain frozen. New
models can be refined within that established meaning; missing structure alone
does not require repeating S2. Use `contracts check` and `scenario bridge` for
autonomous self-review or diagnosis when useful, without making them duplicate
submission procedures.
The original consumer captures model dependencies and
runs checks and registration. S3 needs no Assignment, second envelope or
mandatory repeated manual checks. If an answer remains unreceived, finish
required Git delivery and save that original answer. After reception, retain it
and use the feedback-supplied `runtime stage-answer recover` command for pending gate followup; do not refill or resave the accepted answer.
If transition has already committed but next-stage preparation failed, repair
its named source gap and execute the complete original `runtime stage-answer issue` command supplied by feedback; `recover` continues a pending gate outlet.
Ordinary reads or arbitrary material saves do not promise automatic recovery.

For S3/S4, tool access uses the exact supplied absolute filesystem paths;
answer fields `index_path`, `contract_paths` and `task_paths` contain
repository-relative values. For example, read/write the same contracts file
under its supplied absolute path with a tool, but put its repository-relative
path (`docs/dev/contracts/CONTRACTS-{id}.md`) in `index_path`. Save the
questionnaire at its exact supplied absolute answer path. Markdown links remain
relative to the containing document; do not convert them to answer-field
root-relative paths.

Professional S3/S4 rework is separate from technical continuation. Within the
same bound REQ, use
`.claude/bin/loop-harness runtime stage-answer rework --target design --reason "<concrete design correction>" --root . --agent Main`
when frozen S2 inputs must change. Use
`.claude/bin/loop-harness runtime stage-answer rework --target contracts --reason "<concrete contract correction>" --root . --agent Main`
when accepted contracts must change while S2 stays valid. This explicit route
preserves the accepted history, invalidates affected downstream planning, and
supplies the new current view/answer paths. If preparation fails after the
return is recorded, follow the supplied `runtime stage-answer issue` command.
No packet/entry ID or authored evidence envelope is required. Current
unreceived outputs are ordinary professional corrections, followed by required
Git delivery and saving the original answer, even if marked `locked`.
`issue` repairs missing issuance/preparation; `recover` resumes an accepted
pending outlet. Neither changes an accepted professional judgment or authorizes
editing frozen sources. `revise` is not an S3/S4 professional rework route.
REQ semantic changes retain the actual human amendment boundary; ordinary
contract gaps and compatibility choices within authorization remain professional work.
Any questionnaire reminder about human decisions means an actual unresolved
REQ amendment or decision outside existing authorization. It does not require
a fresh approval for every ADR, compatibility choice, N/A rationale or S3 run;
reuse applicable existing decisions.

Keep the current REQ's original index and complete applicable contract set.
Module behavior spans requirements; retain historical CASEs and real sources.
Another REQ's contract text cannot close a missing current CASE mapping.
Bare FR IDs refer to the current REQ; use the existing explicit `REQ/FR` form
when citing another requirement. Do not copy or relabel a source to satisfy the
check. Missing or ambiguous original sources belong to Main recovery. The normal
consumer uses the original delivery paths; no second scope file or manual check
is required. An unbound all-project diagnostic does not prove this batch complete.

PreToolUse observes graph progress and supplies the current questionnaire's
exact path, missing duties and next action. It does not authorize or deny tools.
Fill the professional answers and save at the supplied path; the original
consumer and stage gates determine reception and progression. Missing input,
conflicts or unspecified compatibility must be reported explicitly. Human
release decisions and independent reviews remain in their original stages.
Prompts reference the relevant contract clauses, role, inputs and expected
outputs; do not duplicate the full methodology or ask the author to copy
machine-known identity and evidence metadata.

## Shared source and reading order

Apply [shared-model contract rules in the factory](../../docs/rules/shared-model-contracts.md) (after installation: [project rule](../../docs/rules/shared-model-contracts.md)).
Decide FE, BE and SYNC applicability independently. For a real shared operation,
converge the applicable shared model and SYNC behavior before finalizing FE/BE
contracts. FE-only local behavior and BE-only internal work may need no SYNC;
SYNC may describe actual participants without separate FE/BE contracts. No
shared data means `Shared model policy: none` with a concrete reason, without
manufacturing a Schema or examples. Applicable CASEs, clauses and the complete
current-REQ batch remain required.
Use native definitions and exact references; do not copy field tables. Check
actual consumer/provider behavior and Mock when applicable, including structurally valid business
rejections. Read only the operation and model slices named by the current TASK
or the exact S3 Hook view.
