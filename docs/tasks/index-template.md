# 任务看板

> 需求：REQ-{id}  
> 合同基线：`docs/contracts/CONTRACTS-{id}.md`
> 状态：draft / locked / executing / completed  
> 更新日期：YYYY-MM-DD

## 里程碑

> Runtime ref: `{runtime-id}@{revision}`. Legal task-lock and activation guards are defined by the Loop Definition.

## 文档链接

| 关系 | 文档 | 用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | 需求批次 |
| upstream | `docs/contracts/CONTRACTS-{id}.md` | 合同基线 |
| downstream | `docs/tasks/TASK-{id}.md` | 任务单 |
| evidence | `docs/reports/acceptance/ACC-{id}.md` | 批次完成索引 |
| runtime | `.claude/loop-state.json` | 当前任务与 workgroup 状态 |
| assignment | `{team-manifest-path}` | 任务分配、依赖和 Agent 数量依据 |

| 里程碑 | 目标日期 | 包含任务 | 状态 |
|:---|:---|:---|:---|
| M1 合同锁定 | YYYY-MM-DD | TASK-001 | pending |
| M2 开发完成 | YYYY-MM-DD | TASK-002~TASK-010 | pending |

## 任务矩阵

| 任务ID | 合同来源 | 负责人 | 预估 | 依赖 | 状态 | 阻塞 |
|:---|:---|:---|:---|:---|:---|:---|
| TASK-001 | BE-001 | Builder-BE-01 | 1d | - | pending | - |

本表「状态」列是看板**进程**状态（pending/in-progress/done），与 TASK 文件头部的文档 Status 三词（draft/complete/cancelled）是两个正交词汇表——不要把 pending 写进 TASK 文件。

## 关键路径

```text
TASK-001 -> TASK-002 -> TASK-003
```

## 变更记录

| 日期 | 变更 | 申请人 | 审批人 | 影响 |
|:---|:---|:---|:---|:---|
| | | | | |
