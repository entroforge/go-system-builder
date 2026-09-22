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

`<release_upstream>` stores release snapshots only.

Daily work goes through `<dev_branch>`. Release from `<dev_branch>` to `<release_upstream>` uses squash merge. After release, merge `<release_upstream>` back into `<dev_branch>`.

Main stays in the authority checkout. A mismatch with the REQ-bound branch must be resolved explicitly; never automatically switch branches. See [workspace integration](../workspace-integration.md).

## 2. Branch Model

| Branch | Purpose | Protection |
|:---|:---|:---|
| `<release_upstream>` | production release snapshot | no direct daily work; release/hotfix only |
| `<dev_branch>` | daily integration | REQ explicitly declared development branch |

REQ binding explicitly declares both destinations, including the remote for a remote release target. Neither field has a default.

## 3. Short-Lived Branches

| Type | Name | Source | Target |
|:---|:---|:---|:---|
| docs/process | `docs/<topic>` | `<dev_branch>` | `<dev_branch>` |
| feature | `feature/<task-id>-<topic>` | `<dev_branch>` | `<dev_branch>` |
| bugfix | `bugfix/<bug-id>-<topic>` | `<dev_branch>` | `<dev_branch>` |
| tech debt | `td/<id>-<topic>` | `<dev_branch>` | `<dev_branch>` |
| release candidate | `release/<version-or-date>` | `<dev_branch>` | `<release_upstream>` |
| production hotfix | `hotfix/<bug-id>-<topic>` | `<release_upstream>` | `<release_upstream>` + `<dev_branch>` |

## 4. Stage To Branch

These are optional naming conventions for separately authorized branches, not
instructions to switch Main at each stage. Registered Worker branches follow
the workspace execution record; Main retains its bound checkout and branch.

| Stage | Output | Branch |
|:---|:---|:---|
| S0/S1 requirement design and initialization | `AGENTS.md`, `project.yaml`, `project-map.md`, `REQ-*.md` | `docs/req-<id>-<topic>` or `docs/bootstrap-project` |
| S2 design | architecture, state, model, ADR, UI design packages | `docs/design-<req-id>-<topic>` |
| S3 contracts | FE/BE/SYNC contracts | `docs/dev/contracts-<req-id>-<topic>` |
| S4 tasks | task board and task files | `docs/dev/tasks-<req-id>-<topic>` |
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
| `<dev_branch>` -> `<release_upstream>` | squash merge only |
| `<release_upstream>` -> `<dev_branch>` after release | normal merge |
| `hotfix/*` -> `<release_upstream>` | squash merge, then merge back to `<dev_branch>` |
| `release/*` -> `<release_upstream>` | squash merge |
| `feature/*` / `bugfix/*` -> `<dev_branch>` | normal merge commit; retain task evidence, verify, acknowledge and clean the temporary worktree |

## 7. Release Gates

Before merge to `<release_upstream>`:

- release audit exists in `docs/reports/release-audits/`
- audit result is not `BLOCKED`
- TASK, REV, and QA evidence exists
- locked contract quality gate evidence exists
- release audit includes release changes, migration, and rollback
- migrations, data repair, and rollback plan are documented if needed

## 8. Forbidden

- feature branch directly into `<release_upstream>`
- normal merge commit into `<release_upstream>`
- daily work on `<release_upstream>`
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
`<release_upstream>`.

## 10. Human Release Handoff

The handoff identifies the actual bound integration branch, tested commit,
project release branch and release evidence. The human release owner performs
release and post-release synchronization through the project’s approved process.
Do not place automatic checkout, pull, squash or push commands in Main’s
engineering continuation. A protected release target requires its own human
release decision even when a Worker has passed integration checks.
## 临时 worktree 与阶段交付

REQ 绑定必须显式提供 `--dev-branch <开发主分支>` 和 `--release-upstream <最终发布上游>`，远程发布目标包含 remote；没有 develop 默认值。项目根目录是当前 REQ 唯一权威。

派发前将上游正式 Markdown、代码和测试产出整理提交；不要自动提交用户无关变更。新 worktree 不含主会话未提交或仅暂存的文件。主会话用 `runtime worktree-create --assignment-id <id> --root <项目根目录>` 从绑定开发分支的明确 commit 创建；不要依赖平台默认分支或复制整个目录。明确不入 Git 的 evidence 单独按依赖交接，不能复制整个控制面。

子会话在子分支提交成果并报告，主会话在项目根目录执行 `runtime task-integrate --assignment-id <id>`，生成合并提交、校验、接收并及时清理。集成不是 release；临时 worktree 不是交付终点。未回收、分支偏离、积压只提醒，不新增 Stop 或普通工具硬门禁。

Gate 的输入来源由 loop-definition 的 file_sources 契约声明；正式交付读固定 Git tree，明确运行输入读磁盘，Runtime 读权威快照。未提交产出不能帮助阶段通过。允许汇总的 evidence 按 mutable_evidence_kinds 自动同步对应 SHA256；同步不改变结论或代际，也不抹除产品基线漂移。
