---
name: frontend-engineering
description: Use when frontend components, browser behavior, accessibility, or responsive presentation changes
category: best-practice
version: 1.1.0
---
# Frontend Engineering
## Authority
Quality guidance only. Stage routing lives in `docs/control/agent-protocol.md`. Apply this method to the actual assigned technical concern.
## Applicability
Apply to frontend paths and the `frontend` risk tag.
## Required Inputs
Read the UI design package, FE contract, states, target browsers, affected components, Project Design Foundation (`docs/design/DESIGN.md` §0 Next-agent card + current `SUR-*` diff + `derivation/REQ-{id}.md`; the cold-start packet is ≤120 lines / ≤12 KB), `design-language.md` only if the card cannot answer Must not, `docs/design/tokens/tokens.css` (generated from `tokens.json`), and — when the target app has Storybook — live component docs via `tools/ui-lab/README.md` (`docs-list` / `docs-show` first).
## Quality Criteria
Check component ownership, state transitions, loading/error/empty states, accessibility, responsive constraints, keyboard behavior, and regression risk. **Before creating any UI**: query UI Lab for a live component covering the same semantic role; reuse it when present — do not re-implement Button, Dialog, or similar from `docs/design/proof/portable/DESIGN.md` or from a library demo. After F6, pages and proof HTML must consume only `semantic_token_only` values via `tokens.css`/`var(--color-*)` — direct use of primitive tokens or raw hex/rgb/hsl is an advisory finding (`primitive_consumption`); style-tile candidates are the only exception. A second module that needs the same relationship must share the component or file `docs/design/decisions/CP-*.md` (legacy `docs/design/components/` still recognized) before adding a parallel control. When `design-checks.json` declares project rules (`max_role_count`/`forbid_binding`/`token_scope`), components must expose `data-design-role` (or the UI Lab role mapping) so the rule can be verified; missing markers make the check `unverifiable`. Unregistered hex and near-duplicate names are advisory findings from `loop-harness design-foundation check`.
## Outputs
Implementation choices or one scoped review conclusion with evidence.
## N/A Criteria
N/A only when no browser-delivered behavior or presentation changes.
## Stop Conditions
Resolve component, fixture and reversible diagnostic choices within the accepted UI/FE design. Report missing authoritative design, a required behavior that cannot be implemented within scope, or a necessary contract change to Main; ordinary implementation uncertainty needs no extra approval.
## Non-Goals
Do not invent product behavior or replace integration testing.

## Implementation method
Trace each assigned behavior from the accepted UI/FE design to its component,
state owner and API boundary. Reuse applicable components and semantic tokens.
Implement loading, empty, error and permission states, keyboard interaction and
responsive behavior, then check the affected components and integration paths.
Record actual observations and remaining gaps in the supplied professional outputs.
