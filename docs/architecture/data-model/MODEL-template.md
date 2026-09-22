# 项目共享模型入口

先阅读 [共享模型与合同规则](../../rules/shared-model-contracts.md)。本文件定位项目当前权威源，不按 REQ 建副本，不手抄 Schema 字段表。

## 权威定位与领域边界

| 领域/概念 | 原生模型链接 | 上游业务设计 | 语义与消费者 |
|:---|:---|:---|:---|
| {领域} | [权威 Schema]({schema}.json) | [架构](../ARCHITECTURE-{id}.md) | {含义及 FE/BE/SYNC 消费入口} |

## 转换与演进

仅记录实际存在的持久化/展示转换、迁移和兼容决策；同构使用同一定义。状态引用既有状态机。未知业务含义返回 S2，需求变化按 [变更控制](../../rules/change-control.md) 处理。

## 下一步

在 [合同总览](../../dev/contracts/CONTRACTS-{id}.md#shared-model-baseline) 声明操作与源关系，与 SYNC 一起收敛数据和交互，再派生分端责任。历史由 Git 保存，精确指纹与冻结状态由 runtime 管理。
