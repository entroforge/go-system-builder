---
name: frontend-engineering
description: Use when frontend components, browser behavior, accessibility, or responsive presentation changes
category: best-practice
version: 1.1.0
---
# Frontend Engineering
## Authority
Quality guidance only. Stage routing lives in `docs/agent-protocol.md`; Skill routing authority comes from the current assignment and risk tags; the reusable method summary is inlined below.
## Applicability
Apply to frontend paths and the `frontend` risk tag.
## Required Inputs
Read the UI design package, FE contract, states, target browsers, affected components, Project Design Foundation (`docs/design/DESIGN.md` §0 Next-agent card + current `SUR-*` diff + `derivation/REQ-{id}.md`; the cold-start packet is ≤120 lines / ≤12 KB), `design-language.md` only if the card cannot answer Must not, `packages/design-tokens/tokens.css` (generated from `tokens.json`), and — when the target app has Storybook — live component docs via `tools/ui-lab/README.md` (`docs-list` / `docs-show` first).
## Quality Criteria
Check component ownership, state transitions, loading/error/empty states, accessibility, responsive constraints, keyboard behavior, and regression risk. **Before creating any UI**: query UI Lab for a live component covering the same semantic role; reuse it when present — do not re-implement Button, Dialog, or similar from `docs/design/proof/portable/DESIGN.md` or from a library demo. After F6, pages and proof HTML must consume only `semantic_token_only` values via `tokens.css`/`var(--color-*)` — direct use of primitive tokens or raw hex/rgb/hsl is an advisory finding (`primitive_consumption`); style-tile candidates are the only exception. A second module that needs the same relationship must share the component or file `docs/design/decisions/CP-*.md` (legacy `docs/design/components/` still recognized) before adding a parallel control. When `design-checks.json` declares project rules (`max_role_count`/`forbid_binding`/`token_scope`), components must expose `data-design-role` (or the UI Lab role mapping) so the rule can be verified; missing markers make the check `unverifiable`. Unregistered hex and near-duplicate names are advisory findings from `loop-harness design-foundation check`.
## Outputs
Implementation choices or one scoped review conclusion with evidence.
## N/A Criteria
N/A only when no browser-delivered behavior or presentation changes.
## Stop Conditions
Stop on missing UI authority, inaccessible required behavior, or contract conflict.
## Non-Goals
Do not invent product behavior or replace integration testing.

## Inlined Methodology

Loop Engineering uses two Skill categories: Methodology (reusable procedure for an engineering event, including entry, steps, output and stop conditions) and Best Practice (professional quality criteria and review techniques for one technical concern). Role identity is not a Skill; Builder, Document Verifier, Delivery Verifier, QA and E2E Browser identities belong to Agent Definitions. An Agent gains capability by combining one role definition, one task instance and the smallest applicable Skill set. Skill files must reference source documents by path. They must not copy REQ, contract, TASK, BUG or report bodies. Best-practice selection maps the `frontend` risk tag to the `frontend-engineering` Skill. Routing order: current runtime state and phase -> pending legal event -> one primary Methodology Skill -> Agent Definition kind -> task artifact and risk tags -> smallest applicable Best-practice set -> two-phase activation when a subagent is involved. Composition rules: one workflow event that requires reusable procedure has exactly one primary Methodology Skill; at most two secondary Methodology dependencies should be active for one action. Anti-patterns include `builder`, `verifier` or `qa` as a general-purpose role Skill; a Skill that reads or writes current Loop state as its own authority; a Skill containing a second state machine; copied REQ/contract/TASK/BUG content; and Best Practices loaded globally without applicability.
