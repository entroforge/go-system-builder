# Naming Rule

---
rule_id: R-NAME-01
category: Process
status: locked
owner: Project Manager / Architect
scope: docs, branches, contracts, tasks, APIs, events, code names
---

## 1. Rule

One concept has one name across requirements, design, contracts, code, tests, and reports.

Names must make traceability obvious.

## 2. Document Names

| Type | Format | Example |
|:---|:---|:---|
| requirement | `REQ-{NNN}.md` | `REQ-001.md` |
| ADR | `ADR-{NNN}.md` | `ADR-001.md` |
| design foundation | `docs/design/DESIGN.md` | `docs/design/DESIGN.md` |
| design grammar | `docs/design/design-language.md` | `docs/design/design-language.md` |
| surface profile | `docs/design/surface-profiles/{surface}.md` | `docs/design/surface-profiles/consumer.md` |
| design derivation | `docs/design/derivation/REQ-{id}.md` | `docs/design/derivation/REQ-014.md` |
| design exception | `docs/design/decisions/EX-{id}.md` (legacy `docs/design/exceptions/EX-{id}.md` still recognized) | `docs/design/decisions/EX-001.md` |
| design tokens | `packages/design-tokens/tokens.json` (+ generated `tokens.css`) | `packages/design-tokens/tokens.json` |
| portable DESIGN snapshot | `docs/design/proof/portable/DESIGN.md` (derived, not authority) | `docs/design/proof/portable/DESIGN.md` |
| component proposal | `docs/design/decisions/CP-{id}.md` (legacy `docs/design/components/CP-{id}.md` still recognized) | `docs/design/decisions/CP-001.md` |
| foundation replay record | `docs/reports/design-foundation/` | `docs/reports/design-foundation/FOUNDATION-REPLAY-template.md` |
| UI design package directory | `docs/design/prototypes/{module}/` | `docs/design/prototypes/fund/` |
| UI prototype HTML | `<feature>.html` (one per page/dialog/wizard) | `fund-list.html`, `wizard.html` |
| UI module entry hub | `index.html` (card-grid linking to every page HTML file) | `docs/design/prototypes/fund/index.html` |
| UI stories (per-module current truth) | `stories.md` (one complete module set; source REQs appear only in `source_refs`) | `docs/design/prototypes/investor/stories.md` |
| UI flows (per-module current truth) | `flows.md` (one complete module set; `F-NNN` + `PATH-*` steps) | `docs/design/prototypes/investor/flows.md` |
| Scenario model | `scenario-model.json` (module rules and branch witnesses) | `docs/design/prototypes/investor/scenario-model.json` |
| Generated cases | `cases.json` (current generated output) | `docs/design/prototypes/investor/cases.json` |
| Scenario coverage | `scenario-coverage.json` (current generated output) | `docs/design/prototypes/investor/scenario-coverage.json` |
| Fixture contract | `fixture-contract.json` (synthetic setup and cleanup) | `docs/design/prototypes/investor/fixture-contract.json` |
| UI user story template | `USER-STORY-template.md` (canonical structure; output lives in module `stories.md`) | `docs/design/prototypes/USER-STORY-template.md` |
| UI user flow template | `USER-FLOW-template.md` (canonical structure; output lives in module `flows.md`) | `docs/design/prototypes/USER-FLOW-template.md` |
| contract overview | `CONTRACTS-{NNN}.md` | `CONTRACTS-001.md` |
| frontend contract | `FE-{NNN}.md` | `FE-001.md` |
| backend contract | `BE-{NNN}.md` | `BE-001.md` |
| sync contract | `SYNC-{NNN}.md` | `SYNC-001.md` |
| task | `TASK-{NNN}.md` | `TASK-001.md` |
| bug | `BUG-{NNN}.md` | `BUG-001.md` |
| review | `REV-{NNN}.md` | `REV-001.md` |
| acceptance | `ACC-{NNN}.md` | `ACC-001.md` |
| QA report | `QA-{NNN}.md` | `QA-001.md` |

## 3. Branch Names

| Type | Format | Example |
|:---|:---|:---|
| docs | `docs/<topic>` | `docs/req-001-onboarding` |
| feature | `feature/<task-id>-<topic>` | `feature/TASK-001-login` |
| bugfix | `bugfix/<bug-id>-<topic>` | `bugfix/BUG-007-retry` |
| tech debt | `td/<id>-<topic>` | `td/TD-003-repository-cleanup` |
| release | `release/<version-or-date>` | `release/2026-05-20` |
| hotfix | `hotfix/<bug-id>-<topic>` | `hotfix/BUG-009-payment-timeout` |

## 4. Code And API Names

| Object | Rule |
|:---|:---|
| directories | follow project ecosystem; keep cross-doc paths stable |
| types/classes | use domain names; avoid vague `Manager` or `Helper` |
| API fields | match `SYNC-*` exactly |
| states | match `docs/design/state/*.md` exactly |
| events | `{domain}.{entity}.{action}`, e.g. `order.payment.succeeded` |
| errors | stable short code, e.g. `E1001`; never reuse meaning |

## 5. Forbidden

- core domain names like `data`, `info`, `tmp`
- multiple names for the same concept
- API field rename without contract approval
- default values that hide unknown business identity
- legacy `proto/` paths for the UI design package directory; the canonical name is `prototypes/` (per `docs/rules/ui-prototype.md` §4). Existing `proto/` paths must be migrated before the next UI-impacting REQ locks
- per-REQ, per-round, `v1/v2`, addendum, or generation copies of module stories, flows, cases, prototypes, fixtures, or Playwright specs
