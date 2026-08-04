# Git Branch And Release Rule

---
rule_id: R-P01
legacy_id: "005"
category: Process
status: locked
owner: Project Manager / Architect / Release Owner
scope: branches, merges, release workflow, master/main gates
---

## 1. Rule

`master/main` stores release snapshots only.

Daily work goes through `develop`. Release from `develop` to `master/main` uses squash merge. After release, merge `master/main` back into `develop`.

## 2. Branch Model

| Branch | Purpose | Protection |
|:---|:---|:---|
| `master` / `main` | production release snapshot | no direct daily work; release/hotfix only |
| `develop` | daily integration | project default integration branch |

Project chooses either `master` or `main` as release branch. This rule uses `master/main` for both.

## 3. Short-Lived Branches

| Type | Name | Source | Target |
|:---|:---|:---|:---|
| docs/process | `docs/<topic>` | `develop` | `develop` |
| feature | `feature/<task-id>-<topic>` | `develop` | `develop` |
| bugfix | `bugfix/<bug-id>-<topic>` | `develop` | `develop` |
| tech debt | `td/<id>-<topic>` | `develop` | `develop` |
| release candidate | `release/<version-or-date>` | `develop` | `master/main` |
| production hotfix | `hotfix/<bug-id>-<topic>` | `master/main` | `master/main` + `develop` |

## 4. Stage To Branch

| Stage | Output | Branch |
|:---|:---|:---|
| S0/S1 requirement design and initialization | `AGENTS.md`, `project.yaml`, `project-map.md`, `REQ-*.md` | `docs/req-<id>-<topic>` or `docs/bootstrap-project` |
| S2 design | architecture, state, model, ADR, UI design packages | `docs/design-<req-id>-<topic>` |
| S3 contracts | FE/BE/SYNC contracts | `docs/contracts-<req-id>-<topic>` |
| S4 tasks | task board and task files | `docs/tasks-<req-id>-<topic>` |
| S5 document verification | REV/document-verification evidence | `docs/document-verification-<req-id>` |
| S6 build | code and tests | `feature/<task-id>-<topic>` |
| S7 full verification round | REV/QA/E2E evidence | `docs/review-<req-id>-round-<n>` |
| S8 finding investigation | BUG reports and root-cause evidence | `docs/bug-investigation-<req-id>-round-<n>` |
| S9 bug resolution | bugfix, targeted re-verification, impact evidence | `bugfix/<bug-id>-<topic>` |
| S10 acceptance and audit | ACC, release changes, release architecture audit | `release/<version-or-date>` or `docs/release-<topic>` |
| S11 human release gateway | release-ready handoff only; no automation branch action | n/a |

## 5. Implementation Branch Authority

Branch creation requires a permitted Loop transition and an activated assignment
whose scope includes the branch operation. This rule defines branch shape; the
Loop Definition, runtime, activation, and Hooks enforce timing.

## 6. Merge Rules

| Operation | Rule |
|:---|:---|
| `develop` -> `master/main` | squash merge only |
| `master/main` -> `develop` after release | normal merge |
| `hotfix/*` -> `master/main` | squash merge, then merge back to `develop` |
| `release/*` -> `master/main` | squash merge |
| `feature/*` / `bugfix/*` -> `develop` | project convention; must keep task evidence |

## 7. Release Gates

Before merge to `master/main`:

- release audit exists in `docs/release_audits/`
- audit result is not `BLOCKED`
- TASK, REV, and QA evidence exists
- locked contract quality gate evidence exists
- release audit includes release changes, migration, and rollback
- migrations, data repair, and rollback plan are documented if needed

## 8. Forbidden

- feature branch directly into `master/main`
- normal merge commit into `master/main`
- daily work on `master/main`
- release without release audit
- release with `BLOCKED` audit
- sync commit with unrelated changes
- implementation branch outside an activated assignment
- loop-created squash merge or formal release without human approval
- implementation branch before locked contract
- implementation branch before required UI Design Package Gate
- release branch containing next-batch features

## 9. Loop Release Boundary

Loop mode may create and manage docs, feature, bugfix, and release-candidate branches for its bound REQ.

After required reports and release audit are complete, it must stop at:

```text
awaiting_human_release
```

Only an explicit human release approval may authorize squash merge to
`master/main`.

## 10. Command Sketch

```bash
git checkout develop
git pull origin develop

# platform performs squash merge: develop -> master/main

git checkout develop
git pull origin develop
git fetch origin
git merge origin/master
git push origin develop
```

For `main`, replace `master` with `main`.
