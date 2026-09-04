---
name: ui-prototyping
description: Use when a requirement changes frontend screens, interactions, states, or visual behavior, and the final UI design package must reach contract lock
category: best-practice
version: 3.3.1
---
# UI Prototyping

## Authority

The current module UI/scenario package is prepared after REQ lock and before
development contracts. Stage requirements live in `docs/agent-protocol.md`;
UI gate legality lives in `docs/loop-definition.json`; package shape lives in
`docs/rules/ui-prototype.md`; project-level language lives in
`docs/rules/design-foundation.md`. HTML prototype quality criteria below are the
canonical shape a Document Verifier checks before contract lock. Story and
flow content quality criteria live in `skills/user-story-design/SKILL.md` and
`skills/user-flow-design/SKILL.md`; the present Skill binds them together
into one per-module package.

## Applicability

Apply to the `ui` risk tag and to any S2 design action where `UI impact =
changed`. The complementary skills `user-story-design` and `user-flow-design`
are co-required for UI-impacting work; this Skill defines the container,
header, sidebar, layout, and token contracts that stories and flows plug
into.

## Required Inputs

- Locked REQ acceptance criteria.
- Current implementation facts and the affected module's complete existing package at
  `docs/design/prototypes/{module}/` (if any), including the scenario four-pack.
- Design Derivation Note at `docs/design/derivation/REQ-{id}.md` (when `UI impact = changed`; `docs/design/design-language.md` is **not** a default Required Input — open Grammar only when the Derivation's `Active constraints → GR-*` column actually cites a `GR-*` that the page must compile, otherwise the §0 card + Surface diff + Derivation drives the page).
- Applicable UX, accessibility, security, and domain rules.
- `docs/rules/design-foundation.md` and `docs/rules/ui-prototype.md`.
- Applicable `docs/design/prototypes/*-convention.md` siblings (storage key
  layout, naming) when the prototype references them.

## Quality Criteria

Cover target interactions, responsive states, validation, loading, empty,
error, permission, and destructive actions. Pass the module layout, HTML
header, sidebar, stories/flows integration, and cross-REQ regression
contracts in §Module Layout through §Cross-REQ Regression below.

## Outputs

Current UI/scenario package per affected module:
`docs/design/prototypes/{module}/{index.html, *.html, stories.md, flows.md,
scenario-model.json, cases.json, scenario-coverage.json, fixture-contract.json}`
plus optional `*-convention.md` siblings.

`stories.md` and `flows.md` content is produced via `user-story-design` and
`user-flow-design`; their internal `S-NNN` / `F-NNN` IDs are per-module, and
each entry carries `source_refs` plus CASE/branch links. The present Skill guarantees that the
referenced outcomes, controls, and states are inspectable in the HTML
prototype.

## N/A Criteria

N/A only when no user-visible frontend interface changes. A small visual
change is not N/A when it changes what a user understands, chooses, or can
do; in that case `stories.md` / `flows.md` must still be extended or
explicitly N/A'd per the Interaction Coverage Map.

## Stop Conditions

Stop when target behavior is ambiguous, a `PATH-*` step cannot be tied to a
visible prototype control or state, or a `stories.md` / `flows.md` entry
cannot be made inspectable in the prototype HTML.

## Non-Goals

Do not use prose alone as a frontend prototype. Do not maintain version,
generation, REQ, round history or addendum files — the package describes the current target only;
git history provides retrospective audit. Do not capture a separate UI
baseline — the running code IS the baseline. Do not redefine `S-NNN` /
`F-NNN` ID rules (those belong to `user-story-design` /
`user-flow-design`).

## Inlined Methodology

Loop Engineering uses two Skill categories: Methodology (reusable procedure
for an engineering event, including entry, steps, output and stop conditions)
and Best Practice (professional quality criteria and review techniques for
one technical concern). Role identity is not a Skill; Builder, Document
Verifier, Delivery Verifier, QA and E2E Browser identities belong to Agent
Definitions. An Agent gains capability by combining one role definition, one
task instance and the smallest applicable Skill set. Skill files must
reference source documents by path. They must not copy REQ, contract, TASK,
BUG or report bodies. The `ui` risk tag selects the complementary
`ui-prototyping`, `user-story-design`, and `user-flow-design` Skills.
Routing order: current runtime state and phase -> pending legal event -> one
primary Methodology Skill -> Agent Definition kind -> task artifact and risk
tags -> smallest applicable Best-practice set -> two-phase activation when a
subagent is involved. Composition rules: one workflow event that requires
reusable procedure has exactly one primary Methodology Skill; at most two
secondary Methodology dependencies should be active for one action.
Anti-patterns include `builder`, `verifier` or `qa` as a general-purpose
role Skill; a Skill that reads or writes current Loop state as its own
authority; a Skill containing a second state machine; copied REQ/contract/
TASK/BUG content; and Best Practices loaded globally without applicability.

## Core Principle

A prototype describes **the target** — what we are about to build. The
current implementation is the live baseline; it lives in the running code,
not in a captured document. Therefore:

- **Current truth only.** No version history, generation label, addendum, REQ copy,
  or round copy is maintained in the package. Git and round evidence preserve history.
- **Module-scoped, not REQ-scoped.** A module's package evolves across multiple REQs;
  every REQ touching it is merged into the same complete current directory. REQ appears
  only in `source_refs`.
- **Scenario + HTML + stories + flows are co-equal.** A module package is incomplete
  without the scenario four-pack and the UI set. Stories validate design rationality;
  flows become E2E scripts; cases and coverage prove branch completeness.
- **Modifiable, not locked.** Prototype files evolve as the target
  clarifies. Contract lock references the current fingerprint via
  `impact-analysis`; the prototype file itself is never sealed.

## Module Layout

Every UI-impacting module ships a package under
`docs/design/prototypes/{module}/`:

```text
docs/design/prototypes/{module}/
├── index.html         # module entry hub (cards link to each prototype file)
├── stories.md         # complete current module story set, see user-story-design
├── flows.md           # complete current module flow set, see user-flow-design
├── scenario-model.json       # current rules and explicit branch witnesses
├── cases.json                 # generated current cases
├── scenario-coverage.json     # generated current coverage output
├── fixture-contract.json      # synthetic setup/cleanup contract
├── *.html             # one HTML file per page / dialog / wizard / component mockup
└── *-convention.md    # optional cross-cutting design conventions
```

Examples:

```text
docs/design/prototypes/fund/
├── index.html
├── stories.md
├── flows.md
├── fund-list.html
├── fund-detail.html
├── fund-detail-quotation.html
├── fund-detail-compliance.html
├── fund-quotation-list.html
├── fund-compliance-list.html
└── wizard.html

docs/design/prototypes/investor/
├── index.html
├── stories.md
├── flows.md
├── investor-list.html
├── investor-detail.html
├── kyc-case-tab.html
├── entity-structure-tab.html
├── kyc-files-tab.html
├── subscription-tab.html
├── review-tab.html
├── add-investor-dialog.html
└── mindmap-legend-and-contextmenu.html
```

Folder naming: `proto/` and `prototypes/` are unified as `prototypes/`.
Legacy `proto/` paths must be migrated.

## HTML Header Contract

Every prototype HTML file carries a 4-field current-truth header in a dark
gradient `.proto-meta` bar. **No other fields.** Version, REQ ID, lock status,
round, owner, and related contracts are excluded; the prototype describes the
current target only, and contracts live elsewhere.

```html
<header class="proto-meta">
  <span>设计代数: v2</span>
  <span>更新: 2026-07-09</span>
  <span>路由: <strong>/layout/fund — overview</strong></span>
  <span><a href="index.html">↩ 模块入口</a></span>
</header>
```

| # | Field | Format | Example | Purpose |
|---|---|---|---|---|
| 1 | 设计代数 | `v{n}` | `v2` | Design generation; bumps when the S2 package is re-converged. Machine-checked. |
| 2 | 更新 | `YYYY-MM-DD` | `2026-07-09` | Last edit date. |
| 3 | 路由 | `<strong>...</strong>` | `/layout/fund — overview`, `/layout/fund/:id — 父详情` | Page route plus slot label. Optional on dialogs / wizards / component mockups (omit if no route). |
| 4 | index 链接 | `<a href="index.html">↩ 模块入口</a>` | link back | Present when an `index.html` hub exists for the module. |

REQ source refs live inside the module scenario/story/flow mappings, not in the
HTML header, because the package is module-scoped. Version, lock status, round,
and owner fields are not part of current-truth HTML.
Related contracts belong in `CONTRACTS-{id}.md`, not in every prototype
file.

## Sidebar `proto-notes`

The right-pane HTML prototype carries a sticky left sidebar
`aside.proto-notes` with per-screen design rationale. Sections are ordered;
the order is part of the contract.

### Mandatory sections (every page)

| # | Heading | Content shape | What it answers |
|---|---|---|---|
| 1 | `本原型目标` | `<p>` — sidebar slot + main-UI intent in one paragraph | Why does this page exist? Which sidebar slot does it occupy? |
| 2 | `设计推导` | `<ul>` of Foundation version (`DESIGN.md@version` or `local`), `SUR-*@version` + profile, Experience role, Active constraints by stable ID (`LAW/ANTI/INV` → `GR-*`), `Must not` by source ID, `Bindings` by `ROLE/PATTERN`/component, Exception (`EX-*`) | Why does this page grow this way from the Next-agent card? Cite `docs/design/derivation/REQ-{id}.md` (the cold-start handoff packet is only §0 + SUR diff + Derivation, ≤120 lines / ≤12 KB). Construction hex or library Primary from a prior page is never the brand; only `semantic_token_only` values are legal. |
| 3 | `PM 决策 (D-1 / D-2 / ...)` | `<ul>` of `<li><b>D1</b> ...` | What product decisions shaped this layout? Each decision gets a stable code (D1, D2, R1…) so contracts and BUG reports can cite it. |
| 4 | `FR coverage` | 3-col `<table>`: FR ref (`§3.1`) · UI affordance · `状态` badge | Which REQ functional requirements does this page cover? Status uses `.badge-status`: `covered` / `partial` / `open`. |
| 5 | `API endpoints` | 3-col `<table>`: Method · Path (`<code>`) · UI 用途 | Which backend endpoints does this page call? |
| 6 | `Edge cases (N)` | `<ul>` of fallback states | Empty, error, permission-denied, terminal-state, confirm-required, etc. |
| 7 | `Q&A (N)` | `<ul>` of `<li><b>Q1 (topic)</b> — DECIDED: ...` or `OPEN: ...` | Open / decided design questions with stable IDs. |

### Layout-specific section (pick one, between PM 决策 and FR coverage)

| Layout | Section | Content |
|---|---|---|
| list | `<N> 列主表 (§X.Y)` | 3-col `<table>`: column # · column name · role / source |
| detail | `zone map` or `5 区纵向顺序` | zone ordering table |
| tab | `子组件 ↔ 原型 zone 映射` | sub-component ↔ zone mapping |
| wizard | `step list` | step sequence |
| dialog | `state-grid (N 态)` | enumeration of dialog view states |

## Stories And Flows Integration

`stories.md` and `flows.md` are produced by `user-story-design` and
`user-flow-design`. Their canonical structure is preserved verbatim; this
Skill only enforces cross-document consistency:

- Every `S-NNN` story outcome must be visually inspectable in at least one
  prototype HTML file.
- Every `PATH-*` step inside an `F-NNN` flow must name a visible control,
  zone, or state in at least one prototype HTML file.
- The module interaction-coverage map in `flows.md` must enumerate every
  declared actionable control in the module's HTML and assign it to at
  least one `PATH-*` step (or an explicit N/A rationale).
- Each `S-NNN` and `F-NNN` entry carries `source_refs`, branch/CASE references,
  and the §8 module regression sweep uses the complete current set.

## Cross-REQ Regression

> **Module regression rule** (`docs/rules/ui-prototype.md §8`): when a REQ
> modifies any file in `docs/design/prototypes/{module}/`, the resulting
> test plan MUST execute **every current flow and required CASE/PATH** listed in `{module}/flows.md`
> end-to-end, not only the flows directly touched by this REQ. This
> protects prior REQs' shipped behavior from silent regression.

Rationale: a module's prototype set is shared across all REQs that touch
it. REQ-031 might add a new tab to the investor module; REQ-032 might
refactor its data loading. Both must verify the module's existing flows
(capture, review, decide) still pass.

This rule propagates to `skills/e2e-browser-testing/SKILL.md`: the
module-level block in a test plan must include a
"module-flow regression sweep" sub-block when the change touches an
existing module. The sweep enumerates every `F-NNN` from the affected
module's `flows.md` and required scenario files, then walks them via Playwright + CDP.

## Layout Variants

The right-side `main.tab-pane-mock` takes one of five canonical shapes.
Each variant has layout-specific conventions.

| Variant | Right-pane skeleton | Mandatory sidebar extras | Notes |
|---|---|---|---|
| **list** | `.design-intent` banner → `.card` (filter chips + `<table>`) | column-by-column spec table | Filter chips often use a 2-row pattern (primary status + secondary dimension) |
| **detail** | `.el-tabs-fake` → `.tab-meta` crumb → `.zone` blocks (each with `.zone-title` + `.zone-tag`) | zone ordering table | `.zone-tag` variants: default (info), `.fr` (primary blue), `.endpoint` (success green) |
| **tab** | `.el-tabs-fake` + `.el-tabs-header` (`.tab.active` blue bottom-border) + `.el-tabs-content` + optional nested `.sub-tabs` | sub-component ↔ zone mapping | One HTML file per tab is the canonical granularity |
| **wizard** | `.page` (max-width 1400px) + `.wizard-header` (dark gradient card) + `.wizard-stepper` (numbered circles; `.active` blue, `.done` green) | step list + state-grid for each step | Wizards do NOT use `.proto-shell`; they get their own dark-gradient header |
| **dialog** | `.tab-meta` crumb + `.page-wrap` containing a grid of `.state-card`s | state-grid enumeration | Each `.state-card` shows one view state (empty / valid / error / submitting / success) |

## Visual Tokens

CSS variables come from `packages/design-tokens/tokens.css`, generated from
`tokens.json`. Do not invent a second palette per module and do not add a
hex that is absent from `tokens.json`. F2 Style Tiles may still use
*candidate* hex while comparing design worlds; published Anchor / Stress /
module HTML after F6 must use the variables.

From `docs/design/prototypes/<module>/`:

```html
<link rel="stylesheet" href="../../../../packages/design-tokens/tokens.css">
```

Canonical names (see `packages/design-tokens/README.md`):

`--color-surface-page`, `--color-surface-raised`, `--color-content-ink`,
`--color-content-meta`, `--color-content-border`,
`--color-content-border-strong`, `--color-action-promise`,
`--color-status-success`, `--color-status-warning`,
`--color-status-blocking`, `--color-status-info`, `--color-brand-mark`,
`--space-*`, `--rounded-*`, `--font-*`.

P1 aliases, if an older page still uses them, map locally — do not put hex
back into the page:

```css
:root {
  --color-surface-card: var(--color-surface-raised);
  --color-border-default: var(--color-content-border);
  --color-content-primary: var(--color-content-ink);
  --color-content-muted: var(--color-content-meta);
  --radius: var(--rounded-md);
  --radius-lg: var(--rounded-lg);
}
```

The legacy `--c-bg` / `--c-primary` names stay retired. After changing
primitives, run `loop-harness design-foundation emit-css --root .`.
Unregistered hex is reported by `loop-harness design-foundation check`.

Badge system — `.badge-status` (FR coverage status):

- `.covered` — green
- `.partial` — warning
- `.open` — info
- `.new` — purple (sparingly, for newly-added FRs)

Domain badges (entity state, type, severity) use solid color bg + white
text. Maintain a per-module palette but keep the visual language identical
across pages.

## Index Hub

Every module ships `index.html` as the entry hub. Card-grid layout (3-col
on desktop). Each card carries:

- `<h3>§N.M title <span class="badge">类型</span></h3>` — REQ section ref +
  page type (list / detail / tab / wizard / dialog)
- `<p>` — one-sentence description
- `<code>` meta chips (route, related stories/flows)
- Footer: `filename · 关联 stories · 关联 flows` plus
  `<a href="X.html">查看 →</a>`

## Sibling Artifacts

A module directory MAY co-host `*-convention.md` design-convention docs
alongside the HTML prototypes. These are NOT prototypes themselves; they
are cross-cutting technical rules (storage key layout, naming conventions,
encoding rules) that multiple prototype files reference.

Required `.md` convention shape:

```markdown
# {module}-{topic}-convention
## Problem
## Research
## Recommendation
## Implementation (FE path / BE path / SYNC ref)
## Risk
```

A Document Verifier treats the `.md` convention as part of the module's
prototype set: it gets fingerprinted alongside the HTML files and cited
from contracts via path + section anchor.

## Quality Bar (Document Verifier checklist)

A module prototype set is ready for contract lock when ALL of these hold:

- [ ] `docs/design/prototypes/{module}/` exists with `index.html`,
      `stories.md`, `flows.md`, the scenario four-pack, and ≥1 page HTML file
- [ ] Every HTML file carries the 4-field header (设计代数 / 更新 / 路由 /
      index 链接); no version, REQ ID, round, lock status, no owner, no
      related-contracts field
- [ ] Every HTML file's `aside.proto-notes` has the 7 mandatory sections
      in canonical order (`本原型目标` → `设计推导` → `PM 决策` → layout-specific
      → `FR coverage` → `API endpoints` → `Edge cases` → `Q&A`)
- [ ] `设计推导` cites Foundation version, Next-agent card / active laws, Must not, experience role, and
      exception; each active law maps to a visible region on the page; construction hex is not cited as brand
- [ ] The layout-appropriate layout-specific section is present (column
      spec / zone map / sub-component map / step list / state-grid)
- [ ] `stories.md` carries the complete current `S-NNN` set; every story cites
      `source_refs`, a branch/CASE, ≥1 prototype file, and ≥1 `F-NNN` flow
- [ ] `flows.md` carries the complete current `F-NNN` set with `PATH-*` steps;
      every flow cites `source_refs`, a CASE/branch, a triggering story ID, and ≥1 prototype file per
      step
- [ ] The module interaction-coverage map assigns every declared control
      in the prototype HTML to at least one `PATH-*` step or an explicit
      N/A rationale
- [ ] Every FR in `FR coverage` has `covered` or `partial` status; no
      `open` without a tracked Q&A ID
- [ ] Every API endpoint in `API endpoints` is closed by an existing or
      pending FE/BE/SYNC contract
- [ ] Edge cases cover at minimum: empty, error, permission-denied,
      terminal state
- [ ] If the REQ modifies an existing module: the test plan includes a
      full-module regression sweep enumerating every `F-NNN` and required CASE/PATH
- [ ] The scenario four-pack is present; generated cases and coverage are current;
      required allow/reject branch coverage is 100%; and the module ratio gate passes

Failing any item is a `DV-SPEC-CONSISTENCY` finding; the prototype set
returns to S2 rework.
