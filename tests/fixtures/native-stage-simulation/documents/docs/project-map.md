# Project Map

> Project: 模拟交付来源台账（SIMULATION-ONLY 模拟夹具 / disposable simulation fixture）
> Updated: 2026-10-21
> PM / Architect: Main（PM / Architect）；人类拍板：simulation-human（test-only）
> Runtime: `.claude/loop-state.json`

> 本项目是一次性隔离工程循环夹具。Project Map 中的"产品事实"均为测试夹具事实，不是真实产品资产。

## 1. Project Facts

| Fact | Value | Status |
|:---|:---|:---|
| goal | 建立"模拟交付来源台账"：登记本轮模拟交付来源并强制产物标记 `SIMULATION_ONLY`，用于验证 S0–S11 流转与原生派发/答卷接收（simulation/test-only） | confirmed |
| users | 无真实用户面。唯一人机交互是 `simulation-human`（test-only）在隔离 Runtime 内作出模拟决定 | confirmed |
| domain | 交付产物标记与来源登记（纯文档/NFR 域，无领域运行时） | confirmed |
| product surfaces | none | confirmed |
| stack | 无产品代码栈。仅 Markdown 文档 + 一个预置文本夹具 + 既有 Harness/Hook 控制面 | confirmed |
| constraints | 隔离项目；禁止真实产品实现、push、merge、publish、deploy、release；全部结论必须显式标注 simulation/test-only | confirmed |

## 2. Human Stage Summary

This section is a human-facing summary. It must not override the Loop runtime.
Status values in the baseline and stage-check tables describe those artifacts
or gates; responsibility attempts use the separate five-state contract in
[state-machine.md](rules/state-machine.md#21-loop-responsibility-status).

| Field | Value |
|:---|:---|
| project stage | S2（planning.design） |
| basis | `docs/requirements/REQ-001.md`（locked，v1.0.0，绑定回执 TR-001）；`docs/architecture/ARCHITECTURE-001.md`；`docs/design/decisions/ADR-001.md` |
| next human milestone | S11 模拟 approve 决定（test-only） |
| stage check | pass |
| runtime identity | `loop-REQ-001` |
| runtime lifecycle | planning / design |
| bound REQ | REQ-001 |

## 3. Baseline Index

| Artifact | Path | Status | Fingerprint / Version |
|:---|:---|:---|:---|
| rules | `docs/rules/README.md` | locked | 仓库既有 |
| engineering conventions | `docs/rules/project-conventions.json` | absent | 无既有约定需索引；本夹具不新建决策索引 |
| REQ | `docs/requirements/REQ-001.md` | locked | v1.0.0 / sha256 62e7c1c1d32067fde0bf0316718a953889f6ce3a76f2534d10d6e88433df31b4（绑定时记录） |
| design investment | N/A | N/A | 依据：REQ-001 顶部 `UI impact=none`，本产品无用户可见界面，无 Surface、无共享 token/组件、无 UI 风险面可复用或换手；纯文档夹具不需要项目级 Design Foundation（AGENTS.md：纯后端/文档工作在本图记 N/A） |
| design foundation | N/A | N/A | 无 UI 表面可覆盖，故不发布 `docs/design/DESIGN.md` |
| design/UI | N/A | N/A | §A4-1 无 UI |
| architecture | `docs/architecture/ARCHITECTURE-001.md` | locked | v1.0.0（SIMULATION/test-only） |
| ADR | `docs/design/decisions/ADR-001.md` | accepted | 方向由 simulation-human（test-only）背书 |
| contracts | `docs/dev/contracts/CONTRACTS-001.md` | draft | S3 产出 |
| tasks | `docs/dev/tasks/index-REQ-001.md` | draft | S4 产出 |
| runtime | `.claude/loop-state.json` | authoritative | `loop-REQ-001` / planning.design |

## 4. PM Todo

| REQ | Human stage | Accepted evidence refs | Next action | Owner | Blocker | Acceptance remaining | Runtime ref |
|:---|:---|:---|:---|:---|:---|:---|---|
| REQ-001 | S2 | `docs/requirements/REQ-001.md`（locked）；`docs/architecture/ARCHITECTURE-001.md`；`docs/design/decisions/ADR-001.md` | 完成 S2 自审并提交设计问卷，进入 S3 合同 | Main（PM / Architect） | none | AC-001 唯一验收标准将由 S6/S7 证据覆盖 | `loop-REQ-001` |

## 5. Unknowns

| ID | Question | Affected artifact | Status | Decision |
|:---|:---|:---|:---|:---|
| U-001 | 本夹具 REQ 的场景别名 REQ-901 与文档编号 REQ-001 如何对应 | `docs/requirements/REQ-001.md` | closed | 以 `docs/requirements/REQ-001.md` 为唯一权威文件；REQ-901 仅作场景内部别名（REQ §D Q-001） |
| U-002 | S6 是否需要真实源码改动 | TASK-001 关闭合同 | closed | 不需要。受管交付物为预置夹具；本轮为模拟测试，禁止真实产品实现（CLAUDE.md 覆盖指令） |
| U-003 | S8 的 RepairContract 若被批准，实际改动范围为何 | S9 Builder 责任 | closed | 范围仅限 `simulation/products/artifact.txt`（DECISIONS.md 第 3 条）；实际执行仍受模拟覆盖指令约束 |

## 6. Stage Check

| Check | Result | Evidence |
|:---|:---|:---|
| required artifacts exist | pass | REQ-001（locked）、ARCHITECTURE-001（locked）、ADR-001（accepted） |
| baseline changes are approved | pass | REQ §E 锁定记录；ADR-001 方向背书；全部由 simulation-human（test-only）决定 |
| runtime and journal are healthy | pass | `loop-harness doctor --root .` / `validate --all --root .`（S5 机器检查阶段正式执行） |
| next action is legal in Loop Definition | pass | 当前 cursor `planning.design` → PTR-PLAN-01（design → contracts） |
