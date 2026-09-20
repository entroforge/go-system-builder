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

Main stays in the user’s selected project checkout and its bound current branch.
There is no universal integration branch name: if Main is on `test2`, registered
Workers start from its frozen base and integrate back into `test2`. Neither
project configuration nor this rule authorizes switching Main to another branch.
See [Workspace integration](../workspace-integration.md) for input snapshots,
commit boundaries and recovery.

The release model below assumes a separate protected release branch selected by
the project. Binding a branch never overrides its release protections; if Main
is on a protected release branch, resolve that setup before dispatching writes.
Human release uses squash merge to `master/main`, followed by the project’s
normal synchronization back to its integration branch.

## 2. Branch Model

| Branch | Purpose | Protection |
|:---|:---|:---|
| `master` / `main` | production release snapshot | no direct daily work; release/hotfix only |
| `<bound-integration-branch>` | daily integration | actual branch captured by Main workspace binding |

Project chooses either `master` or `main` as release branch. This rule uses `master/main` for both.

## 3. Short-Lived Branches

| Type | Name | Source | Target |
|:---|:---|:---|:---|
| docs/process | `docs/<topic>` | `<bound-integration-branch>` | `<bound-integration-branch>` |
| feature | `feature/<task-id>-<topic>` | `<bound-integration-branch>` | `<bound-integration-branch>` |
| bugfix | `bugfix/<bug-id>-<topic>` | `<bound-integration-branch>` | `<bound-integration-branch>` |
| tech debt | `td/<id>-<topic>` | `<bound-integration-branch>` | `<bound-integration-branch>` |
| release candidate | `release/<version-or-date>` | `<bound-integration-branch>` | `master/main` |
| production hotfix | `hotfix/<bug-id>-<topic>` | `master/main` | `master/main` + `<bound-integration-branch>` |

## 4. Stage To Branch

These are optional naming conventions for separately authorized branches, not
instructions to switch Main at each stage. Registered Worker branches follow
the workspace execution record; Main retains its bound checkout and branch.

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
| `<bound-integration-branch>` -> `master/main` | squash merge only |
| `master/main` -> `<bound-integration-branch>` after release | normal merge |
| `hotfix/*` -> `master/main` | squash merge, then merge back to `<bound-integration-branch>` |
| `release/*` -> `master/main` | squash merge |
| `feature/*` / `bugfix/*` -> `<bound-integration-branch>` | registered Worker integration uses non-squash merge and verified checks; keep task evidence |

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

## 10. Human Release Handoff

The handoff identifies the actual bound integration branch, tested commit,
project release branch and release evidence. The human release owner performs
release and post-release synchronization through the project’s approved process.
Do not place automatic checkout, pull, squash or push commands in Main’s
engineering continuation. A protected release target requires its own human
release decision even when a Worker has passed integration checks.
