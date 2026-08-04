# Change Control Rule

---
rule_id: R-CHANGE-01
category: Process
status: locked
owner: Project Manager / Architect
scope: requirements, design, contracts, tasks, quality, release
---

## 1. Rule

Locked baselines change only through documented request, approval, implementation, and verification.

Chat never changes a baseline.

## 2. Baselines

| Baseline | Path | Approval |
|:---|:---|:---|
| Requirements | `docs/requirements/` | user + PM / Architect |
| Design | `docs/design/` | PM / Architect |
| UI Prototypes | `docs/design/prototypes/` | PM / Architect |
| Contracts | `docs/contracts/` | PM / Architect, affected Builders informed |
| Tasks | `docs/tasks/` | PM / Architect |
| Quality | `docs/reports/` | assigned Verifier/QA evidence + PM / Architect |
| Acceptance / Release | `docs/reports/acceptance/`, `docs/release_audits/` | Release Owner + PM / Architect |

## 3. Runtime Effects

Legal state transitions and guards are defined by the Loop Definition. An
approved baseline change must update fingerprints, run impact analysis, and
invalidate affected assignments, activations, PASS evidence, ACC, and release
audit evidence.

## 4. Change Request Fields

| Field | Content |
|:---|:---|
| title | one-line change |
| requester | person or Agent |
| affected docs | REQ / module prototypes (`docs/design/prototypes/<module>/`) / ADR / FE / BE / SYNC / TASK / REV / QA / BUG / ACC / release audit |
| reason | why change is needed |
| change | scope, fields, flow, status, data, tests |
| compatibility | compatible / breaking |
| test impact | tests to add or update |
| release impact | none / audit / migration |
| decision | pending / approved / rejected |

## 5. Forbidden

- implement first, document later
- change APIs or fields privately
- use defaults to hide missing call-site or schema changes
- lock contracts before requirement lock
- lock FE/BE/SYNC contracts before required UI Prototype Gate passes
- bypass a Loop Definition guard or Hook decision
- change sync/release commits with unrelated work
- mark a task complete without a valid complete review round
- fix code while leaving related TASK/REV/QA/BUG/ACC/release evidence stale
- use Engineering Loop automation to lock or change a requirement (REQ lock and amendment are human-only)
- treat release architecture audit as human release approval

## 6. Loop Changes

Inside an active loop:

- contract and task changes that do not alter the locked REQ may be documented and approved by the PM / Architect
- UI prototype changes that do not alter the locked REQ may be documented and approved by the PM / Architect before contract lock
- any change to REQ goal, scope, priority, or acceptance pauses the loop
- `REQ_CHANGE_REQUIRED` remains blocked until human approval updates the requirement baseline
- every automatic change must update affected links, versions, tasks, and verification evidence
