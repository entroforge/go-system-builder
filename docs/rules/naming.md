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
| UI design package directory | `docs/design/prototypes/{module}/` | `docs/design/prototypes/fund/` |
| UI prototype HTML | `<feature>.html` (one per page/dialog/wizard) | `fund-list.html`, `wizard.html` |
| UI module entry hub | `index.html` (card-grid linking to every page HTML file) | `docs/design/prototypes/fund/index.html` |
| UI stories (per-module file) | `stories.md` (one per module; S-NNN entries each carry REQ-id) | `docs/design/prototypes/investor/stories.md` |
| UI flows (per-module file) | `flows.md` (one per module; F-NNN entries each carry REQ-id + PATH-* steps) | `docs/design/prototypes/investor/flows.md` |
| UI user story template | `USER-STORY-{REQ-NNN}-{module}.md` (canonical format reference; output lives in `stories.md`) | `USER-STORY-REQ-001-user-list.md` |
| UI user flow template | `USER-FLOW-{REQ-NNN}-{module}.md` (canonical format reference; output lives in `flows.md`) | `USER-FLOW-REQ-001-user-list.md` |
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
