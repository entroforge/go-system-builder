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
- Existing module specs under `web/e2e/<module>/`.
- `docs/rules/scenario-model.md`, the four generic JSON templates, and the
  applicable UI, naming, security, API, state-machine, and error rules.
- Current fingerprints and the assigned module boundary.

## Current Module Package

Maintain exactly these files under `docs/design/prototypes/<module>/`:

```text
index.html
stories.md
flows.md
scenario-model.json
cross-matrix.json      (hand-written convergence-1 carrier)
fixture-contract.json  (hand-written)
cases.json             (generated — do not hand-edit)
scenario-coverage.json (generated — do not hand-edit)
*.html
```

Maintain permanent browser definitions only under `web/e2e/<module>/**/*.spec.ts`.
Do not create per-REQ, per-round, v1/v2, generation, or addendum copies. Reports
may identify a review round because they record an execution fact.

## Workflow

1. Read the complete existing module package before editing. Read every current
   rule, branch, CASE, story, PATH, prototype state, fixture, and module spec.
2. Resolve locked business sources. Put REQ/decision/state/contract references
   in `source_refs`; stop for clarification when an oracle cannot be derived.
3. Edit only `scenario-model.json`, `cross-matrix.json`, and `fixture-contract.json` as human inputs (cases/coverage are engine-generated).
   Keep `cases.json` and `scenario-coverage.json` as generated current outputs.
4. Model facts as stable IDs and non-empty partitions. Use synthetic values only.
5. Model `risk` on each parent rule. Model every required allow and reject
   branch with its oracle (written with the branch — polarity forces
   outcome thinking). Fill `cross-matrix.json` as the hunt's carrier: every
   meaningful fact×FR×story cell names its covering branch or a no-branch
   reason — silence is not N/A. Cite `REQ-<id>/FR-<id>` in `source_refs`.
6. Validate the gates before handing the package to S5: run
   `loop-harness scenario bridge --root .` right after convergence-1 (the
   AC source check needs no generated outputs — fix FR gaps now), then
   `scenario generate` + `scenario validate` at close (full bridge
   included). Endorsed N/A is a declared NFR id or an explicit §A4
   pointer — free text is rejected.
7. Reconcile the full `stories.md`, `flows.md`, and current `*.html` set —
   by dual-track order stories are written BEFORE rules (they seed the
   hunt and branches cite them); flows/prototypes converge stories with
   branches and bind PATH for browser-required cases.
8. Hand off `Rule → CASE → Story → PATH → Spec → Evidence` references to
   contracts, TASKs, DV, QA, and S7. Any module package change requires the full
   module regression sweep.

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
| `expected_state` | the state the system settles into after the rejection (e.g. `"draft"`) — distinct from `terminal_state`, which describes the positive path | negative branches |
| `recovery` | how the user recovers from the rejection (e.g. `"select-institutional"`); `recovery: "N/A"` must carry `recovery_source_refs` + `recovery_reason` | negative branches |

Also require deterministic
current outputs, explicit positive and negative witnesses, 100% required
allow/reject branch coverage, the configured polarity ratio, complete
CASE/Story/PATH/spec traceability, synthetic isolated fixtures, and full-module
regression. Treat every missing required link or assertion as fail-closed.

### JSON Contract

All four JSON files are strict: UTF-8, two-space indentation, trailing newline,
stable ordering, and unknown fields rejected.

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

Use the repository harness when available:

```bash
go run ./cmd/loop-harness scenario generate --module <module> --root .
go run ./cmd/loop-harness scenario validate --module <module> --root .
go run ./cmd/loop-harness scenario validate --all --root .
go run ./cmd/loop-harness scenario validate --module <module> --root . --require-specs
```

Do not replace a failed command with a manual PASS. If the scenario command is
not yet implemented, report the missing engine as a dependency and preserve
the fail-closed design checks.

## Outputs

- The complete current module package with reviewed human inputs and current
  generated `cases.json` and `scenario-coverage.json` outputs. Each rule keeps
  its parent-level `risk`, while each branch exposes its complete CASE and
  Story/PATH binding.
- Stable `Rule (risk) → CASE → Story → PATH → Spec → Evidence` references for
  contracts, TASKs, DV, QA, and E2E execution.
- An explicit PASS-ready gate result or a fail-closed list of missing,
  conflicting, stale, or unresolvable scenario obligations.
- A full-module regression scope whenever any current module truth changes.

## N/A Criteria

N/A is allowed only when the assigned change affects no module business rule,
scenario, user-visible journey, fixture, or browser-required behavior, and the
reason is traceable to locked sources. A missing reject branch, unknown oracle,
unimplemented spec, or unavailable fixture is not N/A.

## Stop Conditions

Stop and report a document finding when any of these occurs:

- the module package is missing or split into REQ/round/version copies;
- a rule source or expected outcome is unknown;
- a parent rule lacks `risk`, or a branch carries `risk` instead of the
  parent rule owning it;
- a branch, fact partition, fixture, CASE, Story, PATH, or spec reference is
  missing, duplicated, or unresolvable;
- required allow/reject coverage is below 100% or the ratio gate fails;
- a negative oracle lacks any common field, rejection, expected-state, recovery,
  or sourced N/A semantics;
- fixture setup performs the user journey or cannot clean its namespace;
- a generated output is manually patched to hide a gap;
- a requested change needs Runtime, Transition, Evidence, Go, or test-runner
  implementation outside this Skill's documentation/design scope.

## Non-Goals

Do not invent business rules, implement the scenario engine, modify Runtime or
Loop Definition, create real business data, write production/test code, decide
stage transitions, or author DV/QA/E2E conclusions.
