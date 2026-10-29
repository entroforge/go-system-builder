---
name: scenario-model-design
description: Use when S2 defines or changes a module's facts, business-rule branches, scenario cases, stories, flows, prototypes, fixtures, or module-scoped Playwright coverage.
---
# Scenario Model Design

## Authority

Use `docs/rules/scenario-model.md` and the generic templates under
`docs/design/prototypes/`. The locked business rule is the oracle source. A REQ
is a `source_refs` input, never the owner of a case, story, flow, prototype,
fixture, or Playwright copy. Stage legality stays with the Loop Definition; this
Skill only defines the scenario design practice.

## Applicability

Apply when planning or reviewing a module change that affects business rules,
branch coverage, scenario generation inputs, current cases, stories, user
flows, prototype states, synthetic fixtures, or module Playwright coverage.
Apply it to S2 scenario design and the scenario-design inputs handed to S5,
S6, and S7. Do not use it to define a role or a parallel methodology.

## Required Inputs

- The complete existing package under `docs/design/prototypes/<module>/`.
- Locked REQ, decision, state, rule, and contract sources that determine the
  business facts and expected outcomes.
- Existing module specs under `web/e2e/<module>/`, when browser behavior applies.
  Read existing definitions during S2; new production specs are implemented by
  the later assigned TASK, not an S2 authoring prerequisite.
- `docs/rules/scenario-model.md`, the three authoring JSON templates and two generated-output shape examples, and the
  applicable UI, naming, security, API, state-machine, and error rules.
- Current material paths and the assigned module boundary supplied by the
  original responsibility entry. The system binds source versions and digests;
  do not calculate, copy, or register a separate fingerprint list.

## Current Module Package

S2 authors maintain the complete current behavior package under
`docs/design/prototypes/<module>/`: five authored sources and two generated
outputs. Add HTML assets only for applicable UI behavior:

```text
stories.md             (hand-written)
flows.md               (hand-written)
scenario-model.json    (hand-written)
cross-matrix.json      (hand-written convergence-1 carrier)
fixture-contract.json  (hand-written)
cases.json             (generated — do not hand-edit)
scenario-coverage.json (generated — do not hand-edit)
index.html             (UI only: current module entry)
*.html                 (UI only: applicable pages and states)
```

Later implementation maintains permanent browser definitions under `web/e2e/<module>/**/*.spec.ts`.
S7 reads these assets and the locked package without rewriting either. A permitted
cold-start responsibility writes missing scripts/fixtures only in its declared
`verification_artifact_workspace` under `e2e-workspace/`.
Service-only branches use an API caller/worker story, F-NNN operation flow and
`browser_required:false`; they require CASEs without HTML, PATH or browser specs.
Mixed modules retain existing UI behavior and its applicable regression scope.
Do not create per-REQ, per-round, v1/v2, generation, or addendum copies. Reports
may identify a review round because they record an execution fact.

## Workflow

1. S2 authors read the complete existing module package before editing. S7 Reviewers
   read the supplied package to check the assigned Claims/CASEs, preserving all
   locked sources and generated outputs; Main owns the complete-round coverage. Read every current
   rule, branch, CASE, story, PATH, prototype state, fixture, and module spec.
2. S2 authors resolve locked business sources and put REQ/decision/state/contract references
   in `source_refs`; stop for clarification when an oracle cannot be derived.
3. S2 authors author `scenario-model.json`, `cross-matrix.json`, `fixture-contract.json`,
   `stories.md`, and `flows.md`. The original `scenario generate` command writes
   `cases.json` and `scenario-coverage.json`, including their derived counts and
   digests. Do not copy output examples or hand-build generated files.
4. S2 authors model facts as stable IDs and non-empty partitions using synthetic values.
   S7 checks those definitions against the assigned oracle and actual observations;
   a missing or contradictory source is reported, not rewritten to make the check pass.
5. For S2 authoring, model `risk` on each parent rule. Model every required allow and reject
   branch with its oracle (written with the branch — polarity forces
   outcome thinking). Fill `cross-matrix.json` as the hunt's carrier: every
   meaningful fact×FR×story cell names its covering branch or a no-branch
   reason — silence is not N/A. Cite `REQ-<id>/FR-<id>` in `source_refs`.
6. S2 authors check the design before handing the package to S3: run
   `loop-harness scenario bridge --root .` right after convergence-1 (the
   AC source check needs no generated outputs — fix FR gaps now), then
   `scenario generate` + `scenario validate` at close for source and generated
   consistency. The original S3 receiver checks the full AC→CASE bridge;
   module validation alone does not run that REQ-wide check. Endorsed N/A is a
   declared NFR id or an explicit §A4 pointer — free text is rejected.
7. S2 authors reconcile the full `stories.md`, `flows.md`, and applicable current `*.html` set —
   by dual-track order stories are written BEFORE rules (they seed the
   hunt and branches cite them); flows/prototypes converge stories with
   branches and bind PATH for browser-required cases.
8. S2 authors hand off Rule/CASE/Story/flow references to contracts and TASKs, adding PATH
   for browser-required cases. Later implementation and verification bind specs
   and actual evidence. Any module package change requires the full module
   regression sweep in the later verification scope.

## Quality Criteria

Require every parent rule to declare its `risk` (a short free-text phrase
naming what makes this rule risky, e.g. `"money is involved"`,
`"third-party API flakiness"` — it is orthogonal to `coverage_profile`,
do not copy profile names into it), and every branch to declare
`case_id`, `title`, `polarity`, `required`, `witness`, `oracle`, `fixture_id`,
`story_refs`, `flow_refs`, and `browser_required`.

Oracle field semantics (what each field asserts, and who consumes it — the
S5 oracle-independence check and S7 Playwright assertions):

| Field | Write what | Applies to |
|:--|:--|:--|
| `visible` | the observable checkpoint(s) on the page/output that prove the outcome (UI selector or any observable output for non-UI modules) | all branches |
| `terminal_state` | the end state the system must be in (e.g. `"submitted"`) | all branches |
| `persisted_effects` | the business results that must exist afterwards (e.g. `"filing-created"`) — assertions, not narrations | all branches |
| `forbidden_side_effects` | what must provably NOT have happened (e.g. `"duplicate-filing"`) | all branches |
| `rejection` | the stable rejection reason/code the user/system sees | negative branches |
| `expected_state` | the state the system settles into after the rejection (e.g. `"draft"`), consistent with that branch's `terminal_state` | negative branches |
| `recovery` | how the user recovers from the rejection (e.g. `"select-institutional"`); `recovery: "N/A"` must carry `recovery_source_refs` + `recovery_reason` | negative branches |

Also require deterministic
current outputs, explicit positive and negative witnesses, 100% required
allow/reject branch coverage, polarity ratios as risk references only, complete
CASE/Story/flow traceability, applicable browser PATHs, synthetic isolated
fixtures, and full-module regression scope. S2 designs these obligations; later
TASKs implement new specs and S7 records actual execution. Treat every missing
link or assertion required by the current responsibility as fail-closed.

### JSON Contract

The three authoring JSON files and two generated outputs use strict JSON: UTF-8,
two-space indentation, trailing newline and unknown fields rejected. Follow the
original generators for output ordering; do not hand-edit generated statistics.

`scenario-model.json` has only `module`, `coverage_profile`, `facts`, and
`rules` at the top level. Each parent rule owns its `risk`; branch objects own
the CASE, witness, oracle, fixture, Story/PATH references, and
`browser_required` fields listed above, and must not carry a separate `risk`.
`fixture-contract.json` has only `module` and `fixtures`. `cases.json` and
`scenario-coverage.json` are generated outputs and must not contain version,
generation, status, REQ-owner, or round fields.

Use the generic templates in `docs/design/prototypes/` as shape references:

- `scenario-model-template.json`
- `cases-template.json`
- `scenario-coverage-template.json`
- `fixture-contract-template.json`
- `cross-matrix-template.json` (authoring carrier; choose `branch` or `no_branch_reason`, omitting the unused field)

The templates are not module data and must not be copied as an unreviewed
module package.

### Fixture / Browser Boundary

Fixtures may seed facts that precede the journey: synthetic persona and
permissions, draft records, reference dictionaries, fixed clock, isolated
namespace, and controlled external responses. Fixtures must have idempotent
cleanup and may not perform login, navigation, field entry, save, submit,
approve, cancel, retry, or other actions under test.

Playwright starts at the declared visible entry, uses semantic or `data-test`
selectors, performs every human action in the PATH, and asserts visible
checkpoints plus terminal persistence and forbidden side effects. Missing
selectors, missing cleanup, direct URL substitution, hidden API completion, or
shared mutable data is a fail-closed finding.

## Validation Commands

S2 authors use the repository harness for generation and validation. S7 Reviewers
may run applicable read-only validation; they do not run generation to rewrite
the current package or add missing FE contracts. Use the actual installed harness:

```bash
.claude/bin/loop-harness scenario generate --module <module> --root .
.claude/bin/loop-harness scenario validate --module <module> --root .
.claude/bin/loop-harness scenario validate --all --root .
```

For S2, run module generation and validation for applicable packages; use `--all` only
when the current responsibility calls for that scope. `--require-specs` belongs
to later browser test implementation/verification work, not ordinary S2 close.
The generator owns CASE/coverage output; the Main design receiver does not run
the generator automatically on answer save.

Do not replace a failed command with a manual PASS. If the scenario command is
unavailable in the installed harness, report that concrete installation gap
and preserve the required design work; do not fabricate generated output.

## Outputs

S7 Reviewers instead record the assigned Claim observations and real evidence in
the issued answer, with a Finding only for observed failure. A passing negative
CASE records state, side effects, rejection and recovery in `ClaimResult.observed`
and evidence references. Missing applicability or environment facts go to Main
with the affected Claim and exact gap. Ordinary Findings continue safe assigned
checks; a real P0 or dangerous action stops with actual observations and gaps.
The following authored outputs belong to S2:

- The complete current module package with reviewed human inputs and current
  generated `cases.json` and `scenario-coverage.json` outputs. Each rule keeps
  its parent-level `risk`, while each branch exposes its complete CASE and
  Story/PATH binding.
- Stable Rule/CASE/Story/flow references for contracts and TASKs, with PATHs for
  browser behavior. Later DV, QA and E2E responsibilities consume the designed
  obligations, implemented specs and actual evidence.
- An explicit PASS-ready gate result or a fail-closed list of missing,
  conflicting, stale, or unresolvable scenario obligations.
- A full-module regression scope whenever any current module truth changes.

## N/A Criteria

N/A is allowed only when the assigned change affects no module business rule,
scenario, user-visible journey, fixture, or browser-required behavior, and the
reason is traceable to locked sources. A missing reject branch, unknown oracle,
unimplemented spec, or unavailable fixture is not N/A.

## Stop Conditions

For S2, stop authoring and report the concrete design gap when any of these occurs.
For S7, report an observed source inconsistency as the assigned Finding, continue
other safe checks, and return missing prerequisites/applicability to Main without
inventing a product failure or rewriting the design:

- the module package is missing or split into REQ/round/version copies;
- a rule source or expected outcome is unknown;
- a parent rule lacks `risk`, or a branch carries `risk` instead of the
  parent rule owning it;
- a required branch, fact partition, fixture, CASE, Story, or flow reference is
  missing, duplicated, or unresolvable; a browser-required branch lacks a PATH,
  or a spec required by the current implementation/verification responsibility
  is missing;
- required allow/reject coverage is below 100%; polarity ratios alone never reject a package;
- a negative oracle lacks any common field, rejection, expected-state, recovery,
  or sourced N/A semantics;
- fixture setup performs the user journey or cannot clean its namespace;
- a generated output is manually patched to hide a gap;
- a requested change needs Runtime, Transition, Evidence, Go, or test-runner
  implementation outside this Skill's documentation/design scope.

## Non-Goals

Do not invent business rules, implement the scenario engine, modify Runtime or
Loop Definition, create real business data, write production/test code, or decide
stage transitions. S2 authoring does not produce DV/QA/E2E conclusions; S7
Reviewers record only their original assigned conclusions and evidence. Permitted
cold-start verification scripts remain within the declared workspace.
