# UI Design Package Rule

---
rule_id: R-UI-PROTOTYPE-01
category: Design
status: locked
owner: Project Manager / Architect
scope: frontend UI, screens, components, interactions, visible states, responsive behavior, user stories, user flows
---

## 1. Rule

Frontend UI changes require a **module-organized current-truth package**
(`index.html` + `stories.md` + `flows.md` + the four scenario JSON files + page
HTML files) under `docs/design/prototypes/<module>/` before FE/BE/SYNC contracts
are locked.

The prototype describes the **target** behavior. The current implementation IS the baseline; no separate `UI-BASELINE-*` capture is required.

## 2. Trigger

Use this rule when a requirement changes any of:

- page layout or navigation
- visible component, form, table, modal, drawer, toolbar, or dashboard
- user interaction or state transition visible in the UI
- loading, empty, disabled, error, success, or permission state
- responsive behavior or accessibility behavior
- frontend-visible API field, error code, status, or side effect
- a module's user story or flow (even without visual change)

## 3. Required Evidence

When `UI impact = changed`:

1. Locate the affected module directory `docs/design/prototypes/<module>/`. Create it if missing.
2. Read the complete existing module package and merge the REQ behavior into its current truth.
   HTML pages link `packages/design-tokens/tokens.css` (see `docs/rules/design-foundation.md`);
   do not introduce unregistered hex.
3. Ensure the module ships `index.html`, `stories.md`, `flows.md`, `scenario-model.json`,
   `cases.json`, `scenario-coverage.json`, `fixture-contract.json`, and ≥1 page HTML file.
4. Update the complete module set; REQ is a `source_refs` value, not the owner of a copy.
5. Confirm every page HTML file carries the current 4-field header (see §5).
6. Confirm scenario JSON, `stories.md`, and `flows.md` satisfy §6 and §7, including
   positive/negative and 100% required branch gates.
7. Link the module current-truth package into FE/BE/SYNC contracts before contract lock
   (by directory path + fingerprint of the current contents).

When `UI impact = unknown`, contract lock is blocked until the REQ is clarified.

When `UI impact = none`, record `N/A` in REQ and contract templates.

## 4. Module Layout Minimum

`docs/design/prototypes/<module>/` MUST contain:

| File | Purpose |
|:---|:---|
| `index.html` | module entry hub; card-grid linking to every page HTML file with type badges |
| `stories.md` | persona list + user stories (`S-NNN` IDs) |
| `flows.md` | user flows / journey maps (`F-NNN` IDs); dual-purpose: design review + E2E test script |
| `scenario-model.json` | current facts, rules, explicit allow/reject branches, witnesses, and oracles |
| `cases.json` | generated current case catalog |
| `scenario-coverage.json` | generated current branch/polarity coverage output |
| `fixture-contract.json` | synthetic setup and cleanup contract |
| `*.html` | one HTML file per page / dialog / wizard / component mockup |
| `*-convention.md` (optional) | cross-cutting design conventions (storage key layout, naming) referenced by the HTML files |

Folder naming: use `prototypes/` (not `proto/`). The two are unified; legacy `proto/` paths must be migrated.

## 5. HTML Header Minimum

Every page HTML file carries a 4-field current-truth header in a dark gradient
`.proto-meta` bar. **No version, REQ, round, or owner fields.**

| # | Field | Format | Example |
|:---|:---|:---|:---|
| 1 | 设计代数 | `v{n}` design generation | `v2` — bumps when the S2 package is re-converged |
| 2 | 更新 | `YYYY-MM-DD` | `2026-07-09` — last edit date |
| 3 | 路由 | route + slot label | `/layout/fund — overview` (optional on dialogs/wizards) |
| 4 | index 链接 | `<a href="index.html">↩ 模块入口</a>` | link back to module hub |

The four tokens (`设计代数 / 更新 / 路由 / index 链接`) are machine-checked as fixed substrings in every page HTML — a page missing any of them fails the UI prototype gate with the missing tokens named.

REQ ID, lock status, owner, and related contracts are explicitly **dropped** — they are noise given the "only final version" rule, and they live elsewhere (REQ file, `CONTRACTS-{id}.md` index, git history).

Below the header, each page carries a sticky `aside.proto-notes` sidebar with 7 mandatory sections in canonical order: `本原型目标` → `设计推导` → `PM 决策` → layout-specific section → `FR coverage` → `API endpoints` → `Edge cases` → `Q&A`. See `skills/ui-prototyping/SKILL.md` §Sidebar for full contract.

## 6. Stories Minimum

`stories.md` MUST contain:

- **Personas** section: lowercase-kebab IDs with role description, daily goal, and permissions
- **Stories** section: `S-NNN` zero-padded IDs, each with:
  - "作为 X, 我想要 Y, 以便 Z" statement
  - 入口 (route or trigger)
  - 目标 (measurable outcome)
  - 关联原型 (≥1 page HTML file)
  - E2E 锚点 (≥1 flow ID from `flows.md`)

## 7. Flows Minimum

`flows.md` MUST contain:

- One section per flow with `F-NNN` zero-padded ID
- Each flow carries: 触发 / 角色 / 前置 / 关联 story
- Numbered steps with action → expected → prototype reference / route
- 异常分支: edge cases with fallback paths
- E2E 转录 note: identifies which steps map to Playwright `data-test` hooks

Flows are dual-purpose: they validate design rationality at review time, and they become the E2E test script after dev complete.

## 8. Cross-REQ Module Regression

When a REQ modifies any file in `docs/design/prototypes/<module>/`, the resulting test plan MUST execute **every flow** listed in `<module>/flows.md` end-to-end, not only the flows directly touched by this REQ.

Rationale: a module's prototype set is shared across REQs. REQ-A adds a tab; REQ-B refines data loading. Both must verify the module's existing flows still pass. This rule prevents silent regression of prior REQs' shipped behavior.

The test plan owner (Builder for unit, DV Team for spec verification, QA Team for e2e) includes a "module-flow regression sweep" sub-block listing every `F-NNN` from the affected module.

## 9. Procedure Ownership

The prototype design procedure belongs to `.claude/skills/specification-planning/SKILL.md` (planning.design — design choices including the optional UI prototype artifact). Professional prototype criteria — the HTML shell, sidebar sections, visual tokens, stories/flows templates, and the Document Verifier quality bar — belong to `.claude/skills/ui-prototyping/SKILL.md`. The Loop Definition owns the planning_complete guard that fires TR-002; the prototype gate is now a manual checklist inside the planning skill, not a state-machine phase.

## 10. Forbidden

- locking FE/BE/SYNC contracts for UI changes before the module prototype set passes the §3 evidence gate
- shipping a prototype HTML file without the 4-field header
- shipping a module directory without `stories.md` or `flows.md`
- capturing or maintaining a separate `UI-BASELINE-*` document (the current code IS the baseline)
- tracking prototype file versions, design generations, addenda, REQ copies, or round copies
- creating target UI directly from a prompt without checking the module's existing stories/flows
- using chat screenshots or summaries as the only prototype evidence
- hiding UI-driven API/data requirements until Builder implementation
- treating lightweight mode as permission to skip the module prototype set gate
- skipping the §8 module-flow regression sweep when modifying an existing module
