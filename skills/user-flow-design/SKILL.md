---
name: user-flow-design
description: Use when an S2 UI design package needs result-led, checklist-style F-NNN flows written into a module's flows.md before contracts lock, and when S7 plans E2E-USER-FLOW responsibilities
category: best-practice
version: 1.1.0
---
# User Flow Design

## Authority

User flows are the locked execution plan for real-browser testing; E2E
evidence records what happened during a particular run. S2 and S7 obligations
live in `docs/agent-protocol.md`; UI gate legality lives in
`docs/loop-definition.json`; package requirements live in
`docs/rules/ui-prototype.md`.

## Applicability

Apply when `UI impact = changed` and S2 creates or updates
`docs/design/prototypes/{module}/flows.md`, and when S7 plans
`E2E-USER-FLOW` responsibilities. Use after `user-story-design` identifies
the user outcomes that must be walked in a real browser.

A module's `flows.md` is the complete current flow set for the module. Each
entry uses `source_refs` for REQ traceability and binds a CASE/branch; there is
no per-REQ or per-round flow file. The `PATH-*` steps become the executable
checklist for S7 E2E Browser dispatch.

## Required Inputs

- Locked REQ criteria and the reviewed user-story outcomes in
  `docs/design/prototypes/{module}/stories.md`.
- Final module HTML prototypes (`index.html` plus page HTML files), including
  visible controls and states.
- Roles, permissions, test data, entry surfaces, and contract-relevant
  effects.
- Existing complete `flows.md`, `cases.json`, `scenario-coverage.json`, and
  `stories.md` so `source_refs`, `F-NNN`, `PATH-*`, CASE, and branch references
  stay consistent.

## Required Format

The canonical structure lives in `docs/design/prototypes/USER-FLOW-template.md`
and is preserved verbatim. Each flow entry in `flows.md` MUST carry:

- `F-NNN` zero-padded ID (per-module sequence, not per-REQ)
- `source_refs` listing the REQ/decision/rule sources for the current behavior
- CASE and branch references, with positive/negative polarity where applicable
- Header: 触发 / 角色 / 前置 / 关联 story (`S-NNN`)
- `PATH-*` ordered checklist of atomic steps; each step names one human
  action, the visible control or stable selector hint, any input, and the
  immediate visible assertion
- 异常分支 (validation / permission / empty / error / cancel / retry /
  destructive / boundary) where applicable, as separate paths when the
  journey diverges materially
- E2E transcription note that names `data-test` hooks an E2E Browser must
  use; missing hooks surface as frontend-engineering findings, never as
  brittle-selector workarounds
- A module interaction-coverage map entry that assigns every declared
  control in the affected prototype HTML to at least one `PATH-*` step,
  with explicit N/A rationale for unassigned controls

## Quality Criteria

### Derive Routes From Observable Outcomes

Start with a user-visible terminal result from a story, then work backward to
the real entry point and forward again to validate the route. A route is
valid only when a person can recognize its success, failure, or recovery
without reading source code or inspecting hidden state.

### Make Each Route A Linear Checklist

Use the per-flow `PATH-*` step identifier. Give each route one user goal,
one declared entry, explicit preconditions, an ordered sequence of atomic
steps, and one terminal outcome. Each step names one human action, the
visible control or stable selector hint, any input, and the immediate
visible assertion. Do not combine several clicks, a dialog decision, and a
result check into one step.

Split a route when the user goal, entry condition, operation sequence,
recovery choice, or terminal result differs materially. Validation,
permission, empty, error, cancel, retry, destructive, and boundary behavior
are separate paths when they require a different human journey. Share setup
data by reference instead of hiding navigation or state mutations in test
setup.

### Preserve Real Human Navigation

Begin from the route's declared visible entry. Do not substitute direct
URLs, API calls, hidden browser state, or manually mutated application
state for normal navigation unless the entry itself is a documented deep
link or the action is an explicit precondition. A recommended execution
order may optimize manual testing, but every route must still be
independently repeatable without an unstated dependency on a previous
route.

### Design For Dispatch And Evidence

Provide a per-module flow directory that lets S7 assign explicit flow IDs
to an `E2E-USER-FLOW` responsibility. Include flow goal, role, entry,
  terminal state, data/permission requirements, linked story, CASE and source
  refs, prototype area, priority, and required / N-A status. Keep the current
  `flows.md` as the planned checklist; the E2E report mirrors
assigned flow and `PATH-*` IDs and adds PASS/FAIL/BLOCKED plus
screenshots, traces, and observations.

### Audit Interaction Coverage

Maintain a module-level interaction map from every declared actionable
control (button, menu, tab, link, dialog action, and meaningful keyboard
action) to at least one `PATH-*` step. Give a reason for every N/A item.
This prevents a green route suite from silently leaving controls
unvisited.

### Cross-Check Before Lock

Confirm that every step points to a visible prototype control or state,
every flow terminal outcome realizes a user-story outcome, and every
UI-driven field, status, error, permission, or side effect is available
to S3 contracts.

### Respect The Cross-REQ Module Regression Sweep

`docs/rules/ui-prototype.md §8` requires that any REQ modifying a module's
current-truth package trigger a regression sweep of every `F-NNN` and required
CASE/PATH in that module's `flows.md`. Reconcile new behavior into the current
flow set; do not create a REQ-owned flow copy.

## Outputs

- Updated `docs/design/prototypes/{module}/flows.md` with one entry per
  material flow; each entry follows the canonical structure from
  `USER-FLOW-template.md`, carries `F-NNN` plus `source_refs` and CASE/branch references, and produces
  per-flow `PATH-*` steps that are dispatchable to S7 E2E Browser.
- A module interaction-coverage map that names every declared control in
  the module's prototype HTML and the `PATH-*` step that exercises it.
- Stable flow and step IDs for E2E assignment and step-level evidence.
- Explicit gaps or UI/UX blockers requiring S2 resolution.

## N/A Criteria

N/A only when the REQ has no user-visible interface impact. A flow may be
N/A only with an approved reason in the module interaction-coverage map;
omitted controls and undeclared URL shortcuts are never implicit N/A.

## Stop Conditions

Stop when a flow lacks a real entry, a visible expected result, enough data
or permission information to run, a prototype control/state reference, a
traceable user-story outcome, or `data-test` selectors that the prototype
HTML must carry before S7 can dispatch it.

## Non-Goals

Do not turn the design file into a mutable test-result log, invent
test-only product behavior, replace contract tests, decide S7 completion,
or duplicate REQ acceptance criteria that are already tracked elsewhere.
