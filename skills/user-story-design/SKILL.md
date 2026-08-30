---
name: user-story-design
description: Use when an S2 UI design package needs result-led user stories written into a module's stories.md before contracts lock
category: best-practice
version: 1.1.0
---
# User Story Design

## Authority

User stories explain intended product behavior; they do not change the locked
REQ or decide whether a stage transition is legal. Stage requirements live in
`docs/agent-protocol.md`; UI gate legality lives in
`docs/loop-definition.json`; the required package shape lives in
`docs/rules/ui-prototype.md`.

## Applicability

Apply when `UI impact = changed` and S2 creates or updates
`docs/design/prototypes/{module}/stories.md`. Use alongside `ui-prototyping`
to keep stories anchored to inspectable prototype HTML; use `user-flow-design`
after stories establish the outcomes that must be walked in a real browser.

A module's `stories.md` is the complete current story set for every behavior in
the module. Each entry uses `source_refs` for REQ traceability; no entry owns a
REQ copy and no per-REQ/per-round story file exists.

## Required Inputs

- Locked REQ acceptance criteria, scope boundaries, and non-goals.
- Current implementation facts and the affected module's `index.html` plus
  page HTML files in `docs/design/prototypes/{module}/`.
- Known roles, permissions, business data, upstream and downstream screens.
- Applicable UX, accessibility, security, and domain rules.
- Existing complete `stories.md`, `scenario-model.json`, `cases.json`, and
  `scenario-coverage.json` so stable S-NNN, CASE/branch, and prototype-region
  references stay consistent.

## Required Format

The canonical structure lives in `docs/design/prototypes/USER-STORY-template.md`
and is preserved verbatim. Each story entry in `stories.md` MUST carry:

- `S-NNN` zero-padded ID (per-module sequence, not per-REQ)
- `source_refs` listing the REQ/decision/rule sources for the current behavior
- scenario branch and CASE references, including positive and negative outcomes
- `actor`, `trigger`, `goal`, `context`, `consequence-of-not-completing`
- `result` block: user-visible terminal outcome the feature produces
- Page transitions with rationale ("why this page exists for this journey")
- Prototype regions cited by HTML file plus zone or section
- Edge outcomes (success / empty / validation / error / permission / cancel /
  retry / destructive / boundary) when applicable, each with N/A rationale if
  omitted
- Mapping to source refs and to one or more `F-NNN` flows

## Quality Criteria

### Start With The Result

For each material scenario, first state the user-visible result that makes
the feature worthwhile: what has changed, what the user can now decide or do,
and how they can tell it succeeded. Derive screens, data, controls, feedback,
and recovery behavior from that result. Do not start by listing pages or
fields.

### Establish A Real Scenario

Identify the actor, trigger, goal, relevant context, and consequence of not
completing the goal. A story is useful only when it explains why a real
person would enter this flow now. Separate stories by distinct goal, role,
or business context, not merely because the UI changes screen.

### Test The Journey Logic

For every transition, answer: why does the user leave this state, what intent
does the next state serve, what context must remain visible or recoverable,
and where can the user go next? Check success, empty, validation, error,
permission, cancel, retry, destructive, and boundary outcomes when applicable.
Resolve a confusing label, dead end, irreversible surprise, lost context, or
unexplained permission result before contracts lock.

### Keep The Story Set Traceable

Maintain a module-level current story set inside `stories.md`. Each story has a stable
`S-NNN`, `source_refs`, an actor, a trigger, an intended outcome, branch/CASE
references, prototype regions, and links to one or more `F-NNN` flows. Narrative
depth is flexible: record only the context needed to justify the design and
enable review. Do not force unrelated roles or scenarios into one story just
to fit a fixed form.

### Review From The User's Point Of View

Confirm that the prototype makes each promised outcome inspectable and that
the story does not rely on undocumented data, permissions, page jumps, or
system knowledge. Surface unresolved UI/UX decisions explicitly; a material
unresolved decision blocks S3 rather than becoming a Builder assumption.

## Outputs

- Updated `docs/design/prototypes/{module}/stories.md` with one entry per
  material story; each entry follows the canonical structure from
  `USER-STORY-template.md`, carries `S-NNN` plus `source_refs` and branch/CASE
  references, and maps to at least one `F-NNN` flow and one prototype HTML region.
- Explicit UI/UX decisions or blockers for S2/S3.
- Story IDs and outcome references usable by `user-flow-design` and S3
  contracts.

## N/A Criteria

N/A only when the module has no user-visible interface impact. A small visual
change is not N/A when it changes what a user understands, chooses, or can
do. Update the module current story when a new REQ introduces a material outcome;
do not create a REQ-specific copy.

## Stop Conditions

Stop and request REQ clarification or human design decision when a user
outcome, actor, permission rule, data meaning, recovery expectation, or
required page transition cannot be justified from the available facts, or
when the existing `stories.md` cannot be extended without breaking an
established `S-NNN` reference.

## Non-Goals

Do not prescribe a fixed number of stories, design visual layout details,
replace the static HTML prototype, define API contracts, record E2E results,
or duplicate REQ acceptance criteria that are already tracked elsewhere.
